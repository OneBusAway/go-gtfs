package gtfs

import (
	"fmt"
	"testing"

	"github.com/OneBusAway/go-gtfs/constants"
	"github.com/OneBusAway/go-gtfs/warnings"
	"github.com/google/go-cmp/cmp"
)

// parseFaresV2Feed parses the default feed plus the given files, by name.
func parseFaresV2Feed(t *testing.T, files map[string][]string) *Static {
	t.Helper()
	b := newZipBuilderWithDefaults()
	for name, lines := range files {
		b.add(name, lines...)
	}
	static, err := ParseStatic(b.build(), ParseStaticOptions{})
	if err != nil {
		t.Fatalf("ParseStatic: %s", err)
	}
	return static
}

func TestParseRiderCategories(t *testing.T) {
	const header = "rider_category_id,rider_category_name,is_default_fare_category,eligibility_url"
	for _, tc := range []struct {
		desc         string
		lines        []string
		want         []RiderCategory
		wantWarnings []warnings.StaticWarningKind
	}{
		{
			desc: "categories",
			lines: []string{
				header,
				"Adult,Adult,1,https://example.com/adult",
				"Youth,Youth,0,",
				"Senior,Senior,,",
			},
			want: []RiderCategory{
				{Id: "Adult", Name: "Adult", IsDefault: true, EligibilityUrl: "https://example.com/adult"},
				{Id: "Youth", Name: "Youth"},
				{Id: "Senior", Name: "Senior"},
			},
		},
		{
			desc:         "invalid is_default_fare_category",
			lines:        []string{header, "Adult,Adult,yes,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.RiderCategoryInvalid{RiderCategoryID: "Adult", Reason: `invalid is_default_fare_category "yes"`}},
		},
		{
			desc:         "missing name",
			lines:        []string{header, "Adult,,1,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.RiderCategoryInvalid{RiderCategoryID: "Adult", Reason: "missing values [rider_category_name]"}},
		},
		{
			desc:         "duplicate id",
			lines:        []string{header, "Adult,Adult,1,", "Adult,Other,0,"},
			want:         []RiderCategory{{Id: "Adult", Name: "Adult", IsDefault: true}},
			wantWarnings: []warnings.StaticWarningKind{warnings.RiderCategoryInvalid{RiderCategoryID: "Adult", Reason: "duplicate rider_category_id"}},
		},
		{
			// BART publishes an older layout.
			desc:         "pre-spec layout",
			lines:        []string{"rider_category_id,rider_category_description", "5,Youth Clipper"},
			wantWarnings: []warnings.StaticWarningKind{warnings.MissingColumns{Columns: []string{"rider_category_name", "is_default_fare_category"}}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			static := parseFaresV2Feed(t, map[string][]string{"rider_categories.txt": tc.lines})
			if diff := cmp.Diff(static.RiderCategories, tc.want); diff != "" {
				t.Errorf("RiderCategories mismatch (-got +want):\n%s", diff)
			}
			if diff := cmp.Diff(warningKinds(static.Warnings), tc.wantWarnings); diff != "" {
				t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

func TestParseFareMedia(t *testing.T) {
	const header = "fare_media_id,fare_media_name,fare_media_type"
	for _, tc := range []struct {
		desc         string
		lines        []string
		want         []FareMedia
		wantWarnings []warnings.StaticWarningKind
	}{
		{
			desc: "every type",
			lines: []string{
				header,
				"cash,Cash,0", "paper,CharlieTicket,1", "card,ORCA,2", "cemv,Contactless,3", "app,Transit app,4",
			},
			want: []FareMedia{
				{Id: "cash", Name: "Cash", Type: FareMediaType_None},
				{Id: "paper", Name: "CharlieTicket", Type: FareMediaType_PaperTicket},
				{Id: "card", Name: "ORCA", Type: FareMediaType_TransitCard},
				{Id: "cemv", Name: "Contactless", Type: FareMediaType_Cemv},
				{Id: "app", Name: "Transit app", Type: FareMediaType_MobileApp},
			},
		},
		{
			desc:         "invalid type",
			lines:        []string{header, "x,X,5"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareMediaInvalid{FareMediaID: "x", Reason: `invalid fare_media_type "5"`}},
		},
		{
			desc:         "missing type",
			lines:        []string{header, "x,X,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareMediaInvalid{FareMediaID: "x", Reason: "missing values [fare_media_type]"}},
		},
		{
			desc:         "duplicate id",
			lines:        []string{header, "x,X,0", "x,Y,1"},
			want:         []FareMedia{{Id: "x", Name: "X", Type: FareMediaType_None}},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareMediaInvalid{FareMediaID: "x", Reason: "duplicate fare_media_id"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			static := parseFaresV2Feed(t, map[string][]string{"fare_media.txt": tc.lines})
			if diff := cmp.Diff(static.FareMedia, tc.want); diff != "" {
				t.Errorf("FareMedia mismatch (-got +want):\n%s", diff)
			}
			if diff := cmp.Diff(warningKinds(static.Warnings), tc.wantWarnings); diff != "" {
				t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

// summarizeFareProducts renders each product as one line.
func summarizeFareProducts(products []FareProduct) []string {
	var s []string
	for _, p := range products {
		category, media := "", ""
		if p.RiderCategory != nil {
			category = p.RiderCategory.Id
		}
		if p.FareMedia != nil {
			media = p.FareMedia.Id
		}
		s = append(s, fmt.Sprintf("%s %q category=%s media=%s %s", p.Id, p.Name, category, media, p.Amount))
	}
	return s
}

func TestParseFareProducts(t *testing.T) {
	categories := []string{"rider_category_id,rider_category_name,is_default_fare_category", "Adult,Adult,1", "Youth,Youth,0"}
	media := []string{"fare_media_id,fare_media_type", "cash,0", "card,2"}
	const header = "fare_product_id,fare_product_name,rider_category_id,fare_media_id,amount,currency"
	for _, tc := range []struct {
		desc         string
		products     []string
		want         []string
		wantWarnings []warnings.StaticWarningKind
	}{
		{
			desc: "products",
			products: []string{
				header,
				"Bus,Single Ride,Adult,cash,3.00,USD",
				"Bus,Single Ride,Adult,card,2.75,USD",
				"Bus,Single Ride,Youth,card,0,USD",
				"transfer_discount,Transfer discount,,,-1.25,USD",
				"yen,Fare,,,210,JPY",
			},
			want: []string{
				`Bus "Single Ride" category=Adult media=cash 3.00 USD`,
				`Bus "Single Ride" category=Adult media=card 2.75 USD`,
				`Bus "Single Ride" category=Youth media=card 0 USD`,
				`transfer_discount "Transfer discount" category= media= -1.25 USD`,
				`yen "Fare" category= media= 210 JPY`,
			},
		},
		{
			desc:         "unknown rider_category_id",
			products:     []string{header, "Bus,,Nope,,1,USD"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareProductInvalid{FareProductID: "Bus", Reason: `unknown rider_category_id "Nope"`}},
		},
		{
			desc:         "unknown fare_media_id",
			products:     []string{header, "Bus,,,nope,1,USD"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareProductInvalid{FareProductID: "Bus", Reason: `unknown fare_media_id "nope"`}},
		},
		{
			desc:         "invalid amount",
			products:     []string{header, "Bus,,,,1.2.3,USD"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareProductInvalid{FareProductID: "Bus", Reason: `invalid amount "1.2.3"`}},
		},
		{
			desc:         "missing amount and currency",
			products:     []string{header, "Bus,,,,,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareProductInvalid{FareProductID: "Bus", Reason: "missing values [amount currency]"}},
		},
		{
			desc:         "duplicate key",
			products:     []string{header, "Bus,,Adult,cash,3,USD", "Bus,,Adult,cash,4,USD"},
			want:         []string{`Bus "" category=Adult media=cash 3 USD`},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareProductInvalid{FareProductID: "Bus", Reason: "duplicate fare_product_id, rider_category_id and fare_media_id"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			static := parseFaresV2Feed(t, map[string][]string{
				"rider_categories.txt": categories,
				"fare_media.txt":       media,
				"fare_products.txt":    tc.products,
			})
			if diff := cmp.Diff(summarizeFareProducts(static.FareProducts), tc.want); diff != "" {
				t.Errorf("FareProducts mismatch (-got +want):\n%s", diff)
			}
			if diff := cmp.Diff(warningKinds(static.Warnings), tc.wantWarnings); diff != "" {
				t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

// When several rider categories are eligible for one fare_product_id, GTFS
// requires exactly one of them to be the default.
func TestParseFareProducts_DefaultCategory(t *testing.T) {
	const header = "fare_product_id,rider_category_id,amount,currency"
	for _, tc := range []struct {
		desc         string
		categories   []string
		products     []string
		wantWarnings []warnings.StaticWarningKind
	}{
		{
			desc:       "one default",
			categories: []string{"rider_category_id,rider_category_name,is_default_fare_category", "Adult,Adult,1", "Youth,Youth,0"},
			products:   []string{header, "Bus,Adult,3,USD", "Bus,Youth,1,USD"},
		},
		{
			desc:       "a single category needs no default",
			categories: []string{"rider_category_id,rider_category_name,is_default_fare_category", "Youth,Youth,0"},
			products:   []string{header, "Bus,Youth,1,USD"},
		},
		{
			desc:         "no default",
			categories:   []string{"rider_category_id,rider_category_name,is_default_fare_category", "Senior,Senior,0", "Youth,Youth,0"},
			products:     []string{header, "Bus,Senior,1,USD", "Bus,Youth,1,USD"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareProductDefaultCategory{FareProductID: "Bus", Defaults: 0}},
		},
		{
			desc:         "two defaults",
			categories:   []string{"rider_category_id,rider_category_name,is_default_fare_category", "Adult,Adult,1", "Regular,Regular,1"},
			products:     []string{header, "Bus,Adult,3,USD", "Bus,Regular,3,USD"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareProductDefaultCategory{FareProductID: "Bus", Defaults: 2}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			static := parseFaresV2Feed(t, map[string][]string{"rider_categories.txt": tc.categories, "fare_products.txt": tc.products})
			if len(static.FareProducts) != len(tc.products)-1 {
				t.Errorf("got %d products, want all %d kept", len(static.FareProducts), len(tc.products)-1)
			}
			if diff := cmp.Diff(warningKinds(static.Warnings), tc.wantWarnings); diff != "" {
				t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
			}
			for _, w := range static.Warnings {
				if w.File != constants.FareProductsFile {
					t.Errorf("warning %v is about %s, want %s", w.Kind, w.File, constants.FareProductsFile)
				}
			}
		})
	}
}

// Product references must point into Static's slices, not at copies.
func TestParseFareProducts_ReferencesAreShared(t *testing.T) {
	static := parseFaresV2Feed(t, map[string][]string{
		"rider_categories.txt": {"rider_category_id,rider_category_name,is_default_fare_category", "Adult,Adult,1"},
		"fare_media.txt":       {"fare_media_id,fare_media_type", "cash,0"},
		"fare_products.txt":    {"fare_product_id,rider_category_id,fare_media_id,amount,currency", "Bus,Adult,cash,3,USD"},
	})
	p := static.FareProducts[0]
	if p.RiderCategory != &static.RiderCategories[0] || p.FareMedia != &static.FareMedia[0] {
		t.Error("product references do not point into Static.RiderCategories and Static.FareMedia")
	}
}
