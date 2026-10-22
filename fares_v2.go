package gtfs

import (
	"fmt"
	"strings"

	"github.com/OneBusAway/go-gtfs/csv"
	"github.com/OneBusAway/go-gtfs/warnings"
)

// RiderCategory is a group of riders, such as adults or seniors, from
// rider_categories.txt. It is part of GTFS-Fares V2.
type RiderCategory struct {
	Id   string
	Name string
	// IsDefault marks the category to show riders by default, such as
	// "Adult".
	IsDefault      bool
	EligibilityUrl string
}

// FareMedia is a way to pay or hold a fare, from fare_media.txt.
type FareMedia struct {
	Id   string
	Name string
	Type FareMediaType
}

// FareMediaType is the kind of a fare media. Its values match GTFS.
type FareMediaType int32

const (
	// FareMediaType_None is no media, such as cash paid to a driver.
	FareMediaType_None        FareMediaType = 0
	FareMediaType_PaperTicket FareMediaType = 1
	FareMediaType_TransitCard FareMediaType = 2
	// FareMediaType_Cemv is a contactless bank card or mobile device.
	FareMediaType_Cemv      FareMediaType = 3
	FareMediaType_MobileApp FareMediaType = 4
)

func (t FareMediaType) String() string {
	switch t {
	case FareMediaType_None:
		return "NONE"
	case FareMediaType_PaperTicket:
		return "PAPER_TICKET"
	case FareMediaType_TransitCard:
		return "TRANSIT_CARD"
	case FareMediaType_Cemv:
		return "CEMV"
	case FareMediaType_MobileApp:
		return "MOBILE_APP"
	default:
		return "UNKNOWN"
	}
}

// FareProduct is a priced fare product, from fare_products.txt. Several
// FareProducts may share an Id, one for each rider category and fare media
// it can be bought for.
type FareProduct struct {
	Id   string
	Name string
	// RiderCategory is nil when any rider category is eligible.
	RiderCategory *RiderCategory
	// FareMedia is nil when the media is unknown.
	FareMedia *FareMedia
	// Amount may be negative, to represent a transfer discount.
	Amount CurrencyAmount
}

// parseRiderCategories reads rider_categories.txt. A row with a missing or
// invalid value, or a repeated rider_category_id, is skipped with a warning.
func parseRiderCategories(csv *csv.File) ([]RiderCategory, []warnings.StaticWarning) {
	idColumn := csv.RequiredColumn("rider_category_id")
	nameColumn := csv.RequiredColumn("rider_category_name")
	// A required column whose value may be empty.
	isDefaultColumn := csv.OptionalColumn("is_default_fare_category")
	eligibilityUrlColumn := csv.OptionalColumn("eligibility_url")

	missing := csv.MissingRequiredColumns()
	if !isDefaultColumn.Exists() {
		missing = append(missing, "is_default_fare_category")
	}
	if len(missing) > 0 {
		return nil, missingColumnsWarning(csv, missing)
	}

	var categories []RiderCategory
	var w []warnings.StaticWarning
	seen := map[string]bool{}
	for csv.NextRow() {
		c := RiderCategory{Id: idColumn.Read(), Name: nameColumn.Read(), EligibilityUrl: eligibilityUrlColumn.Read()}
		reason := ""
		switch rawDefault := isDefaultColumn.Read(); {
		case len(csv.MissingRowKeys()) > 0:
			reason = fmt.Sprintf("missing values %s", csv.MissingRowKeys())
		case !parseZeroOrOne(rawDefault, &c.IsDefault):
			reason = fmt.Sprintf("invalid is_default_fare_category %q", rawDefault)
		case seen[c.Id]:
			reason = "duplicate rider_category_id"
		default:
			seen[c.Id] = true
			categories = append(categories, c)
			continue
		}
		w = append(w, warnings.NewStaticWarning(csv, warnings.RiderCategoryInvalid{RiderCategoryID: c.Id, Reason: reason}))
	}
	return categories, w
}

// parseFareMedia reads fare_media.txt. A row with a missing or invalid value,
// or a repeated fare_media_id, is skipped with a warning.
func parseFareMedia(csv *csv.File) ([]FareMedia, []warnings.StaticWarning) {
	idColumn := csv.RequiredColumn("fare_media_id")
	nameColumn := csv.OptionalColumn("fare_media_name")
	typeColumn := csv.RequiredColumn("fare_media_type")

	if w := checkForMissingColumns(csv); len(w) > 0 {
		return nil, w
	}

	var media []FareMedia
	var w []warnings.StaticWarning
	seen := map[string]bool{}
	for csv.NextRow() {
		m := FareMedia{Id: idColumn.Read(), Name: nameColumn.Read()}
		rawType := typeColumn.Read()
		t := parseInt32(strings.TrimSpace(rawType))
		reason := ""
		switch {
		case len(csv.MissingRowKeys()) > 0:
			reason = fmt.Sprintf("missing values %s", csv.MissingRowKeys())
		case t == nil || *t < 0 || *t > 4:
			reason = fmt.Sprintf("invalid fare_media_type %q", rawType)
		case seen[m.Id]:
			reason = "duplicate fare_media_id"
		default:
			m.Type = FareMediaType(*t)
			seen[m.Id] = true
			media = append(media, m)
			continue
		}
		w = append(w, warnings.NewStaticWarning(csv, warnings.FareMediaInvalid{FareMediaID: m.Id, Reason: reason}))
	}
	return media, w
}

// parseFareProducts reads fare_products.txt. A row is skipped with a warning
// if a required value is missing or invalid, it names an unknown rider
// category or fare media, or it repeats an earlier row's fare_product_id,
// rider_category_id and fare_media_id. Reading an unknown ID as unset would
// widen the product to every category or media.
//
// When several rider categories are eligible for one fare_product_id, GTFS
// requires exactly one of them to be the default; a product that breaks this
// is kept, with a warning.
func parseFareProducts(csv *csv.File, categories []RiderCategory, media []FareMedia) ([]FareProduct, []warnings.StaticWarning) {
	idColumn := csv.RequiredColumn("fare_product_id")
	nameColumn := csv.OptionalColumn("fare_product_name")
	categoryIDColumn := csv.OptionalColumn("rider_category_id")
	mediaIDColumn := csv.OptionalColumn("fare_media_id")
	amountColumn := csv.RequiredColumn("amount")
	currencyColumn := csv.RequiredColumn("currency")

	if w := checkForMissingColumns(csv); len(w) > 0 {
		return nil, w
	}

	categoryIdToCategory := make(map[string]*RiderCategory, len(categories))
	for i := range categories {
		categoryIdToCategory[categories[i].Id] = &categories[i]
	}
	mediaIdToMedia := make(map[string]*FareMedia, len(media))
	for i := range media {
		mediaIdToMedia[media[i].Id] = &media[i]
	}
	type key struct{ product, category, media string }
	seen := map[key]bool{}
	var products []FareProduct
	var w []warnings.StaticWarning
	for csv.NextRow() {
		p := FareProduct{Id: idColumn.Read(), Name: nameColumn.Read()}
		categoryID, mediaID := categoryIDColumn.Read(), mediaIDColumn.Read()
		rawAmount := amountColumn.Read()
		currency := strings.TrimSpace(currencyColumn.Read())
		p.RiderCategory = categoryIdToCategory[categoryID]
		p.FareMedia = mediaIdToMedia[mediaID]
		var ok bool
		p.Amount, ok = parseCurrencyAmount(rawAmount, currency)
		reason := ""
		switch k := (key{p.Id, categoryID, mediaID}); {
		case len(csv.MissingRowKeys()) > 0 || currency == "":
			reason = fmt.Sprintf("missing values %s", csv.MissingRowKeys())
		case !ok:
			reason = fmt.Sprintf("invalid amount %q", rawAmount)
		case categoryID != "" && p.RiderCategory == nil:
			reason = fmt.Sprintf("unknown rider_category_id %q", categoryID)
		case mediaID != "" && p.FareMedia == nil:
			reason = fmt.Sprintf("unknown fare_media_id %q", mediaID)
		case seen[k]:
			reason = "duplicate fare_product_id, rider_category_id and fare_media_id"
		default:
			seen[k] = true
			products = append(products, p)
			continue
		}
		w = append(w, warnings.NewStaticWarning(csv, warnings.FareProductInvalid{FareProductID: p.Id, Reason: reason}))
	}

	// Check the default rider category of each product, in file order.
	var order []string
	eligible := map[string]map[*RiderCategory]bool{}
	for _, p := range products {
		if p.RiderCategory == nil {
			continue
		}
		if eligible[p.Id] == nil {
			eligible[p.Id] = map[*RiderCategory]bool{}
			order = append(order, p.Id)
		}
		eligible[p.Id][p.RiderCategory] = true
	}
	for _, id := range order {
		if len(eligible[id]) < 2 {
			continue
		}
		defaults := 0
		for c := range eligible[id] {
			if c.IsDefault {
				defaults++
			}
		}
		if defaults != 1 {
			w = append(w, warnings.NewFileWarning(csv.Name(), warnings.FareProductDefaultCategory{FareProductID: id, Defaults: defaults}))
		}
	}
	return products, w
}

// parseZeroOrOne reads an enum that is 1 for true and 0 or empty for false,
// ignoring surrounding whitespace, into v. It reports whether s was valid.
func parseZeroOrOne(s string, v *bool) bool {
	switch strings.TrimSpace(s) {
	case "", "0":
		*v = false
	case "1":
		*v = true
	default:
		return false
	}
	return true
}
