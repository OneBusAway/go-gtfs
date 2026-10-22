package gtfs

import (
	"fmt"
	"testing"

	"github.com/OneBusAway/go-gtfs/warnings"
	"github.com/google/go-cmp/cmp"
)

// transferRuleFiles adds, to legRuleFiles, a stop and a station with its
// platform, networks n1 and n2, and leg groups g1 and g2.
func transferRuleFiles(extra map[string][]string) map[string][]string {
	files := legRuleFiles(map[string][]string{
		"stops.txt":          {"stop_id,location_type,parent_station", "stop_id,,", "ST,1,", "P,0,ST", "E,2,ST"},
		"routes.txt":         {"route_id,route_type,network_id", "route_id,3,n1", "r2,3,n2"},
		"fare_leg_rules.txt": {"leg_group_id,network_id,fare_product_id", "g1,n1,Bus", "g2,n2,Bus2"},
	})
	for name, lines := range extra {
		files[name] = lines
	}
	return files
}

func summarizeFareLegJoinRules(rules []FareLegJoinRule) []string {
	var s []string
	for _, r := range rules {
		s = append(s, fmt.Sprintf("%s->%s at %s->%s", r.FromNetwork.Id, r.ToNetwork.Id, stopID(r.FromStop), stopID(r.ToStop)))
	}
	return s
}

func TestParseFareLegJoinRules(t *testing.T) {
	const header = "from_network_id,to_network_id,from_stop_id,to_stop_id"
	for _, tc := range []struct {
		desc         string
		lines        []string
		want         []string
		wantWarnings []warnings.StaticWarningKind
	}{
		{
			desc:  "networks only, and with stops",
			lines: []string{header, "n1,n2,,", "n1,n1,ST,P"},
			want:  []string{"n1->n2 at ->", "n1->n1 at ST->P"},
		},
		{
			desc:         "unknown network",
			lines:        []string{header, "n1,nope,,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareLegJoinRuleInvalid{Reason: `unknown to_network_id "nope"`}},
		},
		{
			desc:         "missing network",
			lines:        []string{header, "n1,,,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareLegJoinRuleInvalid{Reason: "missing values [to_network_id]"}},
		},
		{
			desc:         "only one stop",
			lines:        []string{header, "n1,n2,ST,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareLegJoinRuleInvalid{Reason: "from_stop_id and to_stop_id must both be set or both be empty"}},
		},
		{
			desc:         "unknown stop",
			lines:        []string{header, "n1,n2,ST,nope"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareLegJoinRuleInvalid{Reason: `unknown to_stop_id "nope"`}},
		},
		{
			desc:         "entrance as a stop",
			lines:        []string{header, "n1,n2,E,P"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareLegJoinRuleInvalid{Reason: `from_stop_id "E" is not a stop or station`}},
		},
		{
			desc:         "duplicate",
			lines:        []string{header, "n1,n2,,", "n1,n2,,"},
			want:         []string{"n1->n2 at ->"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareLegJoinRuleInvalid{Reason: "duplicate row"}},
		},
		{
			desc:         "unreadable file is dropped",
			lines:        []string{header, "n1,n2,,", "broken"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FileUnreadable{Reason: "record on line 3: wrong number of fields"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			static := parseFaresV2Feed(t, transferRuleFiles(map[string][]string{"fare_leg_join_rules.txt": tc.lines}))
			if diff := cmp.Diff(summarizeFareLegJoinRules(static.FareLegJoinRules), tc.want); diff != "" {
				t.Errorf("FareLegJoinRules mismatch (-got +want):\n%s", diff)
			}
			if diff := cmp.Diff(warningKinds(static.Warnings), tc.wantWarnings); diff != "" {
				t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

func summarizeFareTransferRules(rules []FareTransferRule) []string {
	opt := func(p *int32) string {
		if p == nil {
			return "-"
		}
		return fmt.Sprint(*p)
	}
	var s []string
	for _, r := range rules {
		limitType := "-"
		if r.DurationLimit != nil {
			limitType = r.DurationLimitType.String()
		}
		s = append(s, fmt.Sprintf("%s->%s %s product=%s count=%s limit=%s %s",
			r.FromLegGroupId, r.ToLegGroupId, r.FareTransferType, r.FareProductId, opt(r.TransferCount), opt(r.DurationLimit), limitType))
	}
	return s
}

func TestParseFareTransferRules(t *testing.T) {
	const header = "from_leg_group_id,to_leg_group_id,transfer_count,duration_limit,duration_limit_type,fare_transfer_type,fare_product_id"
	for _, tc := range []struct {
		desc         string
		lines        []string
		want         []string
		wantWarnings []warnings.StaticWarningKind
	}{
		{
			desc: "rules",
			lines: []string{
				header,
				"g1,g1,-1,7200,1,0,",
				"g1,g2,,5400,3,1,Bus",
				"g2,g1,,,,2,Bus2",
				",,,,,0,",
				"g2,g2,2,600,0,0,",
				"g1,,,60,2,0,",
			},
			want: []string{
				"g1->g1 A_PLUS_AB product= count=-1 limit=7200 DEPARTURE_TO_DEPARTURE",
				"g1->g2 A_PLUS_AB_PLUS_B product=Bus count=- limit=5400 ARRIVAL_TO_ARRIVAL",
				"g2->g1 AB product=Bus2 count=- limit=- -",
				"-> A_PLUS_AB product= count=- limit=- -",
				"g2->g2 A_PLUS_AB product= count=2 limit=600 DEPARTURE_TO_ARRIVAL",
				"g1-> A_PLUS_AB product= count=- limit=60 ARRIVAL_TO_DEPARTURE",
			},
		},
		{
			desc:         "unknown leg group",
			lines:        []string{header, "nope,g1,,,,0,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareTransferRuleInvalid{Reason: `unknown from_leg_group_id "nope"`}},
		},
		{
			desc:         "unknown fare_product_id",
			lines:        []string{header, "g1,g2,,,,0,Nope"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareTransferRuleInvalid{Reason: `unknown fare_product_id "Nope"`}},
		},
		{
			desc:         "invalid fare_transfer_type",
			lines:        []string{header, "g1,g2,,,,3,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareTransferRuleInvalid{Reason: `invalid fare_transfer_type "3"`}},
		},
		{
			desc:         "missing fare_transfer_type",
			lines:        []string{header, "g1,g2,,,,,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareTransferRuleInvalid{Reason: "missing values [fare_transfer_type]"}},
		},
		{
			desc:         "transfer_count between different groups",
			lines:        []string{header, "g1,g2,1,,,0,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareTransferRuleInvalid{Reason: "transfer_count is set but from_leg_group_id differs from to_leg_group_id"}},
		},
		{
			desc:         "no transfer_count within a group",
			lines:        []string{header, "g1,g1,,,,0,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareTransferRuleInvalid{Reason: "transfer_count is required when from_leg_group_id equals to_leg_group_id"}},
		},
		{
			desc:         "zero transfer_count",
			lines:        []string{header, "g1,g1,0,,,0,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareTransferRuleInvalid{Reason: `invalid transfer_count "0"`}},
		},
		{
			desc:         "duration_limit without type",
			lines:        []string{header, "g1,g2,,600,,0,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareTransferRuleInvalid{Reason: "duration_limit_type must be set exactly when duration_limit is"}},
		},
		{
			desc:         "type without duration_limit",
			lines:        []string{header, "g1,g2,,,1,0,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareTransferRuleInvalid{Reason: "duration_limit_type must be set exactly when duration_limit is"}},
		},
		{
			desc:         "non-positive duration_limit",
			lines:        []string{header, "g1,g2,,0,1,0,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareTransferRuleInvalid{Reason: `invalid duration_limit "0"`}},
		},
		{
			desc:         "invalid duration_limit_type",
			lines:        []string{header, "g1,g2,,60,4,0,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareTransferRuleInvalid{Reason: `invalid duration_limit_type "4"`}},
		},
		{
			desc:         "duplicate key",
			lines:        []string{header, "g1,g2,,60,1,0,", "g1,g2,,60,2,1,"},
			want:         []string{"g1->g2 A_PLUS_AB product= count=- limit=60 DEPARTURE_TO_DEPARTURE"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareTransferRuleInvalid{Reason: "duplicate from_leg_group_id, to_leg_group_id, fare_product_id, transfer_count and duration_limit"}},
		},
		{
			desc:         "duplicate key written differently",
			lines:        []string{header, "g1,g1,1,60,1,0,", "g1,g1,01,060,1,0,"},
			want:         []string{"g1->g1 A_PLUS_AB product= count=1 limit=60 DEPARTURE_TO_DEPARTURE"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareTransferRuleInvalid{Reason: "duplicate from_leg_group_id, to_leg_group_id, fare_product_id, transfer_count and duration_limit"}},
		},
		{
			desc:         "unreadable file is dropped",
			lines:        []string{header, ",,,,,0,", "broken"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FileUnreadable{Reason: "record on line 3: wrong number of fields"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			static := parseFaresV2Feed(t, transferRuleFiles(map[string][]string{"fare_transfer_rules.txt": tc.lines}))
			if diff := cmp.Diff(summarizeFareTransferRules(static.FareTransferRules), tc.want); diff != "" {
				t.Errorf("FareTransferRules mismatch (-got +want):\n%s", diff)
			}
			if diff := cmp.Diff(warningKinds(static.Warnings), tc.wantWarnings); diff != "" {
				t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

// An empty leg group in a transfer rule matches the groups the other rules
// leave out, so the rules cannot be kept when the leg rules were dropped.
func TestParseFareTransferRules_LegRulesUnreadable(t *testing.T) {
	static := parseFaresV2Feed(t, transferRuleFiles(map[string][]string{
		"fare_leg_rules.txt":      {"leg_group_id,network_id,fare_product_id", "g1,n1,Bus", "broken"},
		"fare_transfer_rules.txt": {"from_leg_group_id,to_leg_group_id,fare_transfer_type", ",,0", "g1,g2,0"},
	}))
	if len(static.FareTransferRules) != 0 {
		t.Errorf("got rules %v, want none", summarizeFareTransferRules(static.FareTransferRules))
	}
	want := []warnings.StaticWarningKind{warnings.FileUnreadable{Reason: "record on line 3: wrong number of fields"}}
	if diff := cmp.Diff(warningKinds(static.Warnings), want); diff != "" {
		t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
	}
}

// A leg rules file without a required column has no leg groups, so the
// transfer rules are dropped as when it cannot be read.
func TestParseFareTransferRules_LegRulesMissingColumn(t *testing.T) {
	static := parseFaresV2Feed(t, transferRuleFiles(map[string][]string{
		"fare_leg_rules.txt":      {"leg_group_id,network_id", "g1,n1"},
		"fare_transfer_rules.txt": {"from_leg_group_id,to_leg_group_id,fare_transfer_type", ",,0", "g1,g2,0"},
	}))
	if len(static.FareTransferRules) != 0 {
		t.Errorf("got rules %v, want none", summarizeFareTransferRules(static.FareTransferRules))
	}
	want := []warnings.StaticWarningKind{warnings.MissingColumns{Columns: []string{"fare_product_id"}}}
	if diff := cmp.Diff(warningKinds(static.Warnings), want); diff != "" {
		t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
	}
}
