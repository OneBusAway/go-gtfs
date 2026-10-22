package gtfs

import (
	"fmt"
	"strings"
	"time"

	"github.com/OneBusAway/go-gtfs/csv"
	"github.com/OneBusAway/go-gtfs/warnings"
)

// Timeframe is a time of day on the days of a service, from timeframes.txt.
// Several timeframes may share a GroupId, which fare leg rules refer to.
type Timeframe struct {
	GroupId string
	// StartTime is included in the timeframe and EndTime is not. They are
	// 0 and 24h when the row gives no times.
	StartTime time.Duration
	EndTime   time.Duration
	Service   *Service
}

// FareLegRule gives the fare product for a leg, from fare_leg_rules.txt.
//
// An unset Network, FromArea, ToArea or timeframe group has a special
// meaning that depends on RulePriority: see the GTFS reference for
// fare_leg_rules.txt.
type FareLegRule struct {
	LegGroupId           string
	Network              *Network
	FromArea             *Area
	ToArea               *Area
	FromTimeframeGroupId string
	ToTimeframeGroupId   string
	// FareProductId refers to every FareProduct with this Id.
	FareProductId string
	// RulePriority is nil when fare_leg_rules.txt has no rule_priority
	// column, in which case an unset field matches only when no rule
	// matches exactly. When the column exists, an unset field does not
	// affect matching, and an empty value is 0.
	RulePriority *int32
}

// parseTimeframes reads timeframes.txt. A row is skipped with a warning if
// a required value is missing, it sets only one of start_time and end_time,
// a time is invalid or after 24:00:00, or its service_id is unknown.
func parseTimeframes(csv *csv.File, serviceIdToService map[string]*Service) ([]Timeframe, []warnings.StaticWarning) {
	groupIDColumn := csv.RequiredColumn("timeframe_group_id")
	startColumn := csv.OptionalColumn("start_time")
	endColumn := csv.OptionalColumn("end_time")
	serviceIDColumn := csv.RequiredColumn("service_id")

	if w := checkForMissingColumns(csv); len(w) > 0 {
		return nil, w
	}

	var timeframes []Timeframe
	var w []warnings.StaticWarning
	for csv.NextRow() {
		tf := Timeframe{GroupId: groupIDColumn.Read()}
		var timeReason string
		tf.StartTime, tf.EndTime, timeReason = parseTimeframeTimes(startColumn.Read(), endColumn.Read())
		serviceID := serviceIDColumn.Read()
		tf.Service = serviceIdToService[serviceID]
		missing := csv.MissingRowKeys()
		reason := ""
		switch {
		case len(missing) > 0:
			reason = fmt.Sprintf("missing values %s", missing)
		case timeReason != "":
			reason = timeReason
		case tf.Service == nil:
			reason = fmt.Sprintf("unknown service_id %q", serviceID)
		}
		if reason != "" {
			w = append(w, warnings.NewStaticWarning(csv, warnings.TimeframeInvalid{TimeframeGroupID: tf.GroupId, Reason: reason}))
			continue
		}
		timeframes = append(timeframes, tf)
	}
	return timeframes, w
}

// parseTimeframeTimes reads a timeframe's start_time and end_time, which
// must both be set or both be empty, in which case they are 0 and 24h. A
// non-empty reason means the row must be skipped.
func parseTimeframeTimes(rawStart, rawEnd string) (start, end time.Duration, reason string) {
	if rawStart == "" && rawEnd == "" {
		return 0, 24 * time.Hour, ""
	}
	if rawStart == "" || rawEnd == "" {
		return 0, 0, "start_time and end_time must both be set or both be empty"
	}
	// GTFS forbids times after 24:00:00 here.
	start, ok := parseGtfsTimeToDuration(rawStart)
	if !ok || start > 24*time.Hour {
		return 0, 0, fmt.Sprintf("invalid start_time %q", rawStart)
	}
	end, ok = parseGtfsTimeToDuration(rawEnd)
	if !ok || end > 24*time.Hour {
		return 0, 0, fmt.Sprintf("invalid end_time %q", rawEnd)
	}
	return start, end, ""
}

// parseFareLegRules reads fare_leg_rules.txt. A row is skipped with a
// warning if fare_product_id is missing, any ID is unknown, rule_priority is
// not a non-negative integer, or it repeats an earlier row's primary key.
// Reading an unknown ID as unset would change what the rule matches.
func parseFareLegRules(csv *csv.File, networks []Network, areas []Area, timeframes []Timeframe, products []FareProduct) ([]FareLegRule, []warnings.StaticWarning) {
	legGroupIDColumn := csv.OptionalColumn("leg_group_id")
	networkIDColumn := csv.OptionalColumn("network_id")
	fromAreaIDColumn := csv.OptionalColumn("from_area_id")
	toAreaIDColumn := csv.OptionalColumn("to_area_id")
	fromTimeframeColumn := csv.OptionalColumn("from_timeframe_group_id")
	toTimeframeColumn := csv.OptionalColumn("to_timeframe_group_id")
	productIDColumn := csv.RequiredColumn("fare_product_id")
	rulePriorityColumn := csv.OptionalColumn("rule_priority")

	if w := checkForMissingColumns(csv); len(w) > 0 {
		return nil, w
	}

	networkIdToNetwork := networksByID(networks)
	areaIdToArea := areasByID(areas)
	timeframeGroups := idSet(timeframes, func(tf Timeframe) string { return tf.GroupId })
	productIDs := idSet(products, func(p FareProduct) string { return p.Id })
	type key struct{ network, fromArea, toArea, fromTimeframe, toTimeframe, product string }
	seen := map[key]bool{}

	var rules []FareLegRule
	var w []warnings.StaticWarning
	for csv.NextRow() {
		r := FareLegRule{
			LegGroupId:           legGroupIDColumn.Read(),
			FromTimeframeGroupId: fromTimeframeColumn.Read(),
			ToTimeframeGroupId:   toTimeframeColumn.Read(),
			FareProductId:        productIDColumn.Read(),
		}
		networkID, fromAreaID, toAreaID := networkIDColumn.Read(), fromAreaIDColumn.Read(), toAreaIDColumn.Read()
		r.Network = networkIdToNetwork[networkID]
		r.FromArea = areaIdToArea[fromAreaID]
		r.ToArea = areaIdToArea[toAreaID]
		k := key{networkID, fromAreaID, toAreaID, r.FromTimeframeGroupId, r.ToTimeframeGroupId, r.FareProductId}
		rawPriority := rulePriorityColumn.Read()
		if rulePriorityColumn.Exists() {
			p := strings.TrimSpace(rawPriority)
			if p == "" {
				p = "0"
			}
			r.RulePriority = parseInt32(p)
		}
		missing := csv.MissingRowKeys()
		reason := ""
		switch {
		case len(missing) > 0:
			reason = fmt.Sprintf("missing values %s", missing)
		case !productIDs[r.FareProductId]:
			reason = fmt.Sprintf("unknown fare_product_id %q", r.FareProductId)
		case networkID != "" && r.Network == nil:
			reason = fmt.Sprintf("unknown network_id %q", networkID)
		case fromAreaID != "" && r.FromArea == nil:
			reason = fmt.Sprintf("unknown from_area_id %q", fromAreaID)
		case toAreaID != "" && r.ToArea == nil:
			reason = fmt.Sprintf("unknown to_area_id %q", toAreaID)
		case r.FromTimeframeGroupId != "" && !timeframeGroups[r.FromTimeframeGroupId]:
			reason = fmt.Sprintf("unknown from_timeframe_group_id %q", r.FromTimeframeGroupId)
		case r.ToTimeframeGroupId != "" && !timeframeGroups[r.ToTimeframeGroupId]:
			reason = fmt.Sprintf("unknown to_timeframe_group_id %q", r.ToTimeframeGroupId)
		case rulePriorityColumn.Exists() && (r.RulePriority == nil || *r.RulePriority < 0):
			reason = fmt.Sprintf("invalid rule_priority %q", rawPriority)
		case seen[k]:
			// GTFS also forbids one rule in several leg groups, which
			// this catches, since leg_group_id is not part of the key.
			reason = "duplicate network_id, from_area_id, to_area_id, from_timeframe_group_id, to_timeframe_group_id and fare_product_id"
		default:
			seen[k] = true
			rules = append(rules, r)
			continue
		}
		w = append(w, warnings.NewStaticWarning(csv, warnings.FareLegRuleInvalid{LegGroupID: r.LegGroupId, Reason: reason}))
	}
	return rules, w
}
