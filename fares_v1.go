package gtfs

import (
	"fmt"
	"strings"

	"github.com/OneBusAway/go-gtfs/csv"
	"github.com/OneBusAway/go-gtfs/warnings"
)

// FareAttribute is a GTFS-Fares V1 fare, from fare_attributes.txt. GTFS
// recommends that consumers use Fares V2 instead when a feed has both.
type FareAttribute struct {
	Id            string
	Price         CurrencyAmount
	PaymentMethod PaymentMethod
	// Transfers is the number of transfers allowed, or nil for unlimited.
	Transfers *int32
	// Agency is nil when the feed has several agencies and the row gives
	// no agency_id.
	Agency *Agency
	// TransferDuration is how long, in seconds, a transfer or ticket stays
	// valid. Nil when not given.
	TransferDuration *int32
}

// PaymentMethod is when a fare is paid.
type PaymentMethod int32

const (
	PaymentMethod_OnBoard        PaymentMethod = 0
	PaymentMethod_BeforeBoarding PaymentMethod = 1
)

func (m PaymentMethod) String() string {
	switch m {
	case PaymentMethod_OnBoard:
		return "ON_BOARD"
	case PaymentMethod_BeforeBoarding:
		return "BEFORE_BOARDING"
	default:
		return "UNKNOWN"
	}
}

// FareRule says where a GTFS-Fares V1 fare applies, from fare_rules.txt.
// Zone IDs refer to stops.txt zone_id. A fare with no rules applies to every
// trip.
type FareRule struct {
	Fare              *FareAttribute
	Route             *Route
	OriginZoneId      string
	DestinationZoneId string
	// ContainsZoneId is one zone a trip must pass through. A fare with
	// several such rules needs all of their zones.
	ContainsZoneId string
}

// parseFareAttributes reads fare_attributes.txt. A row is skipped with a
// warning if a required value is missing or invalid, its agency_id is
// unknown, or its fare_id repeats an earlier one. A row without agency_id
// uses the feed's agency if there is just one, and otherwise gets a warning.
func parseFareAttributes(csv *csv.File, agencies []Agency) ([]FareAttribute, []warnings.StaticWarning) {
	idColumn := csv.RequiredColumn("fare_id")
	priceColumn := csv.RequiredColumn("price")
	currencyColumn := csv.RequiredColumn("currency_type")
	paymentMethodColumn := csv.RequiredColumn("payment_method")
	// transfers is a required column whose value may be empty.
	transfersColumn := csv.OptionalColumn("transfers")
	agencyIDColumn := csv.OptionalColumn("agency_id")
	transferDurationColumn := csv.OptionalColumn("transfer_duration")

	missing := csv.MissingRequiredColumns()
	if !transfersColumn.Exists() {
		missing = append(missing, "transfers")
	}
	if len(missing) > 0 {
		return nil, missingColumnsWarning(csv, missing)
	}

	agencyIdToAgency := agenciesByID(agencies)
	var fares []FareAttribute
	var w []warnings.StaticWarning
	seen := map[string]bool{}
	for csv.NextRow() {
		id := idColumn.Read()
		rawPrice := priceColumn.Read()
		currency := currencyColumn.Read()
		rawPaymentMethod := paymentMethodColumn.Read()
		invalid := func(format string, a ...any) {
			w = append(w, warnings.NewStaticWarning(csv, warnings.FareAttributeInvalid{FareID: id, Reason: fmt.Sprintf(format, a...)}))
		}
		if missingKeys := csv.MissingRowKeys(); len(missingKeys) > 0 {
			invalid("missing values %s", missingKeys)
			continue
		}
		price, ok := parseCurrencyAmount(rawPrice, currency)
		if !ok || price.Units < 0 {
			invalid("invalid price %q", rawPrice)
			continue
		}
		fare := FareAttribute{Id: id, Price: price}
		paymentMethod := parseInt32(strings.TrimSpace(rawPaymentMethod))
		if paymentMethod == nil || *paymentMethod < 0 || *paymentMethod > 1 {
			invalid("invalid payment_method %q", rawPaymentMethod)
			continue
		}
		fare.PaymentMethod = PaymentMethod(*paymentMethod)
		// Empty means unlimited, which Transfers leaves nil.
		if rawTransfers := transfersColumn.Read(); strings.TrimSpace(rawTransfers) != "" {
			fare.Transfers = parseInt32(strings.TrimSpace(rawTransfers))
			if fare.Transfers == nil || *fare.Transfers < 0 || *fare.Transfers > 2 {
				invalid("invalid transfers %q", rawTransfers)
				continue
			}
		}
		agencyID := agencyIDColumn.Read()
		switch {
		case agencyID != "":
			if fare.Agency = agencyIdToAgency[agencyID]; fare.Agency == nil {
				invalid("unknown agency_id %q", agencyID)
				continue
			}
		case len(agencies) == 1:
			fare.Agency = &agencies[0]
		}
		if seen[id] {
			invalid("duplicate fare_id")
			continue
		}
		seen[id] = true
		if agencyID == "" && len(agencies) > 1 {
			w = append(w, warnings.NewStaticWarning(csv, warnings.FareAttributeAgencyAmbiguous{FareID: id}))
		}
		if raw := transferDurationColumn.Read(); raw != "" {
			if d := parseInt32(strings.TrimSpace(raw)); d != nil && *d >= 0 {
				fare.TransferDuration = d
			} else {
				w = append(w, warnings.NewStaticWarning(csv, warnings.FareAttributeInvalidValue{FareID: id, Column: "transfer_duration", Value: raw}))
			}
		}
		fares = append(fares, fare)
	}
	return fares, w
}

// parseFareRules reads fare_rules.txt. A row with a missing or unknown
// fare_id, or an unknown route_id, is skipped with a warning.
func parseFareRules(csv *csv.File, fares []FareAttribute, routeIdToRoute map[string]*Route) ([]FareRule, []warnings.StaticWarning) {
	fareIDColumn := csv.RequiredColumn("fare_id")
	routeIDColumn := csv.OptionalColumn("route_id")
	originColumn := csv.OptionalColumn("origin_id")
	destinationColumn := csv.OptionalColumn("destination_id")
	containsColumn := csv.OptionalColumn("contains_id")

	if w := checkForMissingColumns(csv); len(w) > 0 {
		return nil, w
	}

	fareIdToFare := faresByID(fares)
	var rules []FareRule
	var w []warnings.StaticWarning
	for csv.NextRow() {
		fareID := fareIDColumn.Read()
		routeID := routeIDColumn.Read()
		rule := FareRule{
			Fare:              fareIdToFare[fareID],
			Route:             routeIdToRoute[routeID],
			OriginZoneId:      originColumn.Read(),
			DestinationZoneId: destinationColumn.Read(),
			ContainsZoneId:    containsColumn.Read(),
		}
		reason := ""
		switch missingKeys := csv.MissingRowKeys(); {
		case len(missingKeys) > 0:
			reason = fmt.Sprintf("missing values %s", missingKeys)
		case rule.Fare == nil:
			reason = "unknown fare_id"
		case routeID != "" && rule.Route == nil:
			// Reading it as unset would make the rule apply to every route.
			reason = fmt.Sprintf("unknown route_id %q", routeID)
		default:
			rules = append(rules, rule)
			continue
		}
		w = append(w, warnings.NewStaticWarning(csv, warnings.FareRuleInvalid{FareID: fareID, Reason: reason}))
	}
	return rules, w
}
