package gtfs

import (
	"fmt"
	"strings"

	"github.com/OneBusAway/go-gtfs/csv"
	"github.com/OneBusAway/go-gtfs/warnings"
)

// FareLegJoinRule says when two consecutive legs count as one effective fare
// leg, from fare_leg_join_rules.txt.
type FareLegJoinRule struct {
	FromNetwork *Network
	ToNetwork   *Network
	// FromStop and ToStop, both set or both nil, restrict the rule to legs
	// that end and start at these stops or stations.
	FromStop *Stop
	ToStop   *Stop
}

// FareTransferRule gives the cost of transferring between legs, from
// fare_transfer_rules.txt. An empty leg group matches the groups that the
// other rules do not name.
type FareTransferRule struct {
	FromLegGroupId string
	ToLegGroupId   string
	// TransferCount is how many consecutive transfers the rule covers, or
	// -1 for no limit. It is nil when the leg groups differ.
	TransferCount *int32
	// DurationLimit is the transfer's time limit in seconds, or nil for
	// none. DurationLimitType says what it is measured between, and is
	// meaningful only when DurationLimit is set.
	DurationLimit     *int32
	DurationLimitType DurationLimitType
	FareTransferType  FareTransferType
	// FareProductId is the product needed to transfer, or empty when the
	// transfer is free. It refers to every FareProduct with this Id.
	FareProductId string
}

// FareTransferType is how a transfer's cost combines with its legs'. Its
// values match GTFS.
type FareTransferType int32

const (
	// FareTransferType_APlusAB is the from-leg's product plus the
	// transfer's.
	FareTransferType_APlusAB FareTransferType = 0
	// FareTransferType_APlusABPlusB adds the to-leg's product too.
	FareTransferType_APlusABPlusB FareTransferType = 1
	// FareTransferType_AB is the transfer's product alone.
	FareTransferType_AB FareTransferType = 2
)

func (t FareTransferType) String() string {
	switch t {
	case FareTransferType_APlusAB:
		return "A_PLUS_AB"
	case FareTransferType_APlusABPlusB:
		return "A_PLUS_AB_PLUS_B"
	case FareTransferType_AB:
		return "AB"
	default:
		return "UNKNOWN"
	}
}

// DurationLimitType is what a transfer's duration limit is measured between:
// the fare validation at the departure or arrival of the first leg, and at
// the departure or arrival of the last. Its values match GTFS.
type DurationLimitType int32

const (
	DurationLimitType_DepartureToArrival   DurationLimitType = 0
	DurationLimitType_DepartureToDeparture DurationLimitType = 1
	DurationLimitType_ArrivalToDeparture   DurationLimitType = 2
	DurationLimitType_ArrivalToArrival     DurationLimitType = 3
)

func (t DurationLimitType) String() string {
	switch t {
	case DurationLimitType_DepartureToArrival:
		return "DEPARTURE_TO_ARRIVAL"
	case DurationLimitType_DepartureToDeparture:
		return "DEPARTURE_TO_DEPARTURE"
	case DurationLimitType_ArrivalToDeparture:
		return "ARRIVAL_TO_DEPARTURE"
	case DurationLimitType_ArrivalToArrival:
		return "ARRIVAL_TO_ARRIVAL"
	default:
		return "UNKNOWN"
	}
}

// parseFareLegJoinRules reads fare_leg_join_rules.txt. A row is skipped with
// a warning if a network is missing or unknown, it sets only one stop, a stop
// is unknown or is not a stop or station, or it repeats an earlier row.
func parseFareLegJoinRules(csv *csv.File, networks []Network, stopIdToStop map[string]*Stop) ([]FareLegJoinRule, []warnings.StaticWarning) {
	fromNetworkColumn := csv.RequiredColumn("from_network_id")
	toNetworkColumn := csv.RequiredColumn("to_network_id")
	fromStopColumn := csv.OptionalColumn("from_stop_id")
	toStopColumn := csv.OptionalColumn("to_stop_id")

	if w := checkForMissingColumns(csv); len(w) > 0 {
		return nil, w
	}

	networkIdToNetwork := networksByID(networks)
	type key struct{ fromNetwork, toNetwork, fromStop, toStop string }
	seen := map[key]bool{}
	var rules []FareLegJoinRule
	var w []warnings.StaticWarning
	for csv.NextRow() {
		k := key{fromNetworkColumn.Read(), toNetworkColumn.Read(), fromStopColumn.Read(), toStopColumn.Read()}
		r := FareLegJoinRule{
			FromNetwork: networkIdToNetwork[k.fromNetwork],
			ToNetwork:   networkIdToNetwork[k.toNetwork],
			FromStop:    stopIdToStop[k.fromStop],
			ToStop:      stopIdToStop[k.toStop],
		}
		missing := csv.MissingRowKeys()
		reason := ""
		switch {
		case len(missing) > 0:
			reason = fmt.Sprintf("missing values %s", missing)
		case r.FromNetwork == nil:
			reason = fmt.Sprintf("unknown from_network_id %q", k.fromNetwork)
		case r.ToNetwork == nil:
			reason = fmt.Sprintf("unknown to_network_id %q", k.toNetwork)
		case (k.fromStop == "") != (k.toStop == ""):
			reason = "from_stop_id and to_stop_id must both be set or both be empty"
		case k.fromStop != "" && r.FromStop == nil:
			reason = fmt.Sprintf("unknown from_stop_id %q", k.fromStop)
		case k.toStop != "" && r.ToStop == nil:
			reason = fmt.Sprintf("unknown to_stop_id %q", k.toStop)
		case r.FromStop != nil && !isStopOrStation(r.FromStop):
			reason = fmt.Sprintf("from_stop_id %q is not a stop or station", k.fromStop)
		case r.ToStop != nil && !isStopOrStation(r.ToStop):
			reason = fmt.Sprintf("to_stop_id %q is not a stop or station", k.toStop)
		case seen[k]:
			reason = "duplicate row"
		default:
			seen[k] = true
			rules = append(rules, r)
			continue
		}
		w = append(w, warnings.NewStaticWarning(csv, warnings.FareLegJoinRuleInvalid{Reason: reason}))
	}
	return rules, w
}

// parseFareTransferRules reads fare_transfer_rules.txt. A row is skipped
// with a warning if a value is missing, invalid or unknown, it breaks the
// GTFS rules for when transfer_count and duration_limit_type are set, or it
// repeats an earlier row's primary key. GTFS requires transfer_count when the
// two leg groups are the same and forbids it when they differ; two empty
// groups match different groups, so they need none.
func parseFareTransferRules(csv *csv.File, legRules []FareLegRule, products []FareProduct) ([]FareTransferRule, []warnings.StaticWarning) {
	fromGroupColumn := csv.OptionalColumn("from_leg_group_id")
	toGroupColumn := csv.OptionalColumn("to_leg_group_id")
	transferCountColumn := csv.OptionalColumn("transfer_count")
	durationLimitColumn := csv.OptionalColumn("duration_limit")
	durationLimitTypeColumn := csv.OptionalColumn("duration_limit_type")
	transferTypeColumn := csv.RequiredColumn("fare_transfer_type")
	productIDColumn := csv.OptionalColumn("fare_product_id")

	if w := checkForMissingColumns(csv); len(w) > 0 {
		return nil, w
	}

	legGroups := idSet(legRules, func(r FareLegRule) string { return r.LegGroupId })
	productIDs := idSet(products, func(p FareProduct) string { return p.Id })
	type key struct{ fromGroup, toGroup, product, transferCount, durationLimit string }
	seen := map[key]bool{}
	var rules []FareTransferRule
	var w []warnings.StaticWarning
	for csv.NextRow() {
		r := FareTransferRule{
			FromLegGroupId: fromGroupColumn.Read(),
			ToLegGroupId:   toGroupColumn.Read(),
			FareProductId:  productIDColumn.Read(),
		}
		rawCount := strings.TrimSpace(transferCountColumn.Read())
		rawLimit := strings.TrimSpace(durationLimitColumn.Read())
		rawLimitType := strings.TrimSpace(durationLimitTypeColumn.Read())
		rawTransferType := strings.TrimSpace(transferTypeColumn.Read())
		var transferTypeOK, limitTypeOK bool
		r.FareTransferType, transferTypeOK = parseEnum(rawTransferType, FareTransferType_APlusAB, FareTransferType_AB)
		r.DurationLimitType, limitTypeOK = parseEnum(rawLimitType, DurationLimitType_DepartureToArrival, DurationLimitType_ArrivalToArrival)
		r.TransferCount = parseInt32(rawCount)
		r.DurationLimit = parseInt32(rawLimit)
		sameGroup := r.FromLegGroupId != "" && r.FromLegGroupId == r.ToLegGroupId
		k := key{r.FromLegGroupId, r.ToLegGroupId, r.FareProductId, rawCount, rawLimit}
		missing := csv.MissingRowKeys()
		reason := ""
		switch {
		case len(missing) > 0:
			reason = fmt.Sprintf("missing values %s", missing)
		case !transferTypeOK:
			reason = fmt.Sprintf("invalid fare_transfer_type %q", rawTransferType)
		case r.FromLegGroupId != "" && !legGroups[r.FromLegGroupId]:
			reason = fmt.Sprintf("unknown from_leg_group_id %q", r.FromLegGroupId)
		case r.ToLegGroupId != "" && !legGroups[r.ToLegGroupId]:
			reason = fmt.Sprintf("unknown to_leg_group_id %q", r.ToLegGroupId)
		case r.FareProductId != "" && !productIDs[r.FareProductId]:
			reason = fmt.Sprintf("unknown fare_product_id %q", r.FareProductId)
		case rawCount != "" && r.FromLegGroupId != r.ToLegGroupId:
			reason = "transfer_count is set but from_leg_group_id differs from to_leg_group_id"
		case rawCount == "" && sameGroup:
			reason = "transfer_count is required when from_leg_group_id equals to_leg_group_id"
		case rawCount != "" && (r.TransferCount == nil || *r.TransferCount == 0 || *r.TransferCount < -1):
			reason = fmt.Sprintf("invalid transfer_count %q", rawCount)
		case (rawLimit == "") != (rawLimitType == ""):
			reason = "duration_limit_type must be set exactly when duration_limit is"
		case rawLimit != "" && (r.DurationLimit == nil || *r.DurationLimit <= 0):
			reason = fmt.Sprintf("invalid duration_limit %q", rawLimit)
		case rawLimitType != "" && !limitTypeOK:
			reason = fmt.Sprintf("invalid duration_limit_type %q", rawLimitType)
		case seen[k]:
			reason = "duplicate from_leg_group_id, to_leg_group_id, fare_product_id, transfer_count and duration_limit"
		default:
			seen[k] = true
			rules = append(rules, r)
			continue
		}
		w = append(w, warnings.NewStaticWarning(csv, warnings.FareTransferRuleInvalid{Reason: reason}))
	}
	return rules, w
}
