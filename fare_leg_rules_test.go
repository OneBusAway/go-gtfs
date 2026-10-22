package gtfs

import (
	"fmt"
	"testing"

	"github.com/OneBusAway/go-gtfs/warnings"
	"github.com/google/go-cmp/cmp"
)

// legRuleFiles are the files fare_leg_rules.txt can reference: service
// service_id (from the default feed), network n1, areas a1 and a2, timeframe
// group peak, and fare product Bus.
func legRuleFiles(extra map[string][]string) map[string][]string {
	files := map[string][]string{
		"routes.txt":        {"route_id,route_type,network_id", "route_id,3,n1"},
		"areas.txt":         {"area_id", "a1", "a2"},
		"timeframes.txt":    {"timeframe_group_id,start_time,end_time,service_id", "peak,07:00:00,09:00:00,service_id"},
		"fare_products.txt": {"fare_product_id,amount,currency", "Bus,2.75,USD", "Bus2,3,USD"},
	}
	for name, lines := range extra {
		files[name] = lines
	}
	return files
}

func TestParseTimeframes(t *testing.T) {
	const header = "timeframe_group_id,start_time,end_time,service_id"
	for _, tc := range []struct {
		desc         string
		lines        []string
		want         []string
		wantWarnings []warnings.StaticWarningKind
	}{
		{
			desc:  "with and without times",
			lines: []string{header, "peak,07:00:00,09:30:00,service_id", "peak,16:00:00,24:00:00,service_id", "allday,,,service_id"},
			want:  []string{"peak 7h0m0s-9h30m0s service_id", "peak 16h0m0s-24h0m0s service_id", "allday 0s-24h0m0s service_id"},
		},
		{
			desc:         "only start_time",
			lines:        []string{header, "peak,07:00:00,,service_id"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TimeframeInvalid{TimeframeGroupID: "peak", Reason: "start_time and end_time must both be set or both be empty"}},
		},
		{
			desc:         "after 24:00:00",
			lines:        []string{header, "late,23:00:00,25:00:00,service_id"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TimeframeInvalid{TimeframeGroupID: "late", Reason: `invalid end_time "25:00:00"`}},
		},
		{
			desc:         "unparsable time",
			lines:        []string{header, "peak,7am,09:00:00,service_id"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TimeframeInvalid{TimeframeGroupID: "peak", Reason: `invalid start_time "7am"`}},
		},
		{
			desc:         "minutes out of range",
			lines:        []string{header, "peak,07:99:00,09:00:00,service_id"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TimeframeInvalid{TimeframeGroupID: "peak", Reason: `invalid start_time "07:99:00"`}},
		},
		{
			desc:         "missing seconds",
			lines:        []string{header, "peak,07:00:00,09:00,service_id"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TimeframeInvalid{TimeframeGroupID: "peak", Reason: `invalid end_time "09:00"`}},
		},
		{
			desc:         "overflowing hours",
			lines:        []string{header, "peak,9999999999:00:00,09:00:00,service_id"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TimeframeInvalid{TimeframeGroupID: "peak", Reason: `invalid start_time "9999999999:00:00"`}},
		},
		{
			desc:         "unknown service_id",
			lines:        []string{header, "peak,,,nope"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TimeframeInvalid{TimeframeGroupID: "peak", Reason: `unknown service_id "nope"`}},
		},
		{
			desc:         "missing service_id",
			lines:        []string{header, "peak,,,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TimeframeInvalid{TimeframeGroupID: "peak", Reason: "missing values [service_id]"}},
		},
		{
			desc:         "unreadable timeframes.txt is dropped",
			lines:        []string{header, "peak,,,service_id", "broken"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FileUnreadable{Reason: "record on line 3: wrong number of fields"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			static := parseFaresV2Feed(t, map[string][]string{"timeframes.txt": tc.lines})
			var got []string
			for _, tf := range static.Timeframes {
				got = append(got, fmt.Sprintf("%s %s-%s %s", tf.GroupId, tf.StartTime, tf.EndTime, tf.Service.Id))
			}
			if diff := cmp.Diff(got, tc.want); diff != "" {
				t.Errorf("Timeframes mismatch (-got +want):\n%s", diff)
			}
			if diff := cmp.Diff(warningKinds(static.Warnings), tc.wantWarnings); diff != "" {
				t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

// summarizeFareLegRules renders each rule as one line.
func summarizeFareLegRules(rules []FareLegRule) []string {
	var s []string
	for _, r := range rules {
		network, from, to, priority := "", "", "", "none"
		if r.Network != nil {
			network = r.Network.Id
		}
		if r.FromArea != nil {
			from = r.FromArea.Id
		}
		if r.ToArea != nil {
			to = r.ToArea.Id
		}
		if r.RulePriority != nil {
			priority = fmt.Sprint(*r.RulePriority)
		}
		s = append(s, fmt.Sprintf("group=%s network=%s from=%s to=%s fromTime=%s toTime=%s product=%s priority=%s",
			r.LegGroupId, network, from, to, r.FromTimeframeGroupId, r.ToTimeframeGroupId, r.FareProductId, priority))
	}
	return s
}

func TestParseFareLegRules(t *testing.T) {
	const header = "leg_group_id,network_id,from_area_id,to_area_id,from_timeframe_group_id,to_timeframe_group_id,fare_product_id"
	const withPriority = header + ",rule_priority"
	for _, tc := range []struct {
		desc         string
		lines        []string
		want         []string
		wantWarnings []warnings.StaticWarningKind
	}{
		{
			desc:  "without rule_priority column",
			lines: []string{header, "g1,n1,a1,a2,peak,,Bus", "g2,,,,,,Bus2"},
			want: []string{
				"group=g1 network=n1 from=a1 to=a2 fromTime=peak toTime= product=Bus priority=none",
				"group=g2 network= from= to= fromTime= toTime= product=Bus2 priority=none",
			},
		},
		{
			desc:  "with rule_priority column",
			lines: []string{withPriority, "g1,n1,,,,,Bus,2", "g2,,,,,,Bus2,"},
			want: []string{
				"group=g1 network=n1 from= to= fromTime= toTime= product=Bus priority=2",
				"group=g2 network= from= to= fromTime= toTime= product=Bus2 priority=0",
			},
		},
		{
			desc:         "unknown fare_product_id",
			lines:        []string{header, "g,,,,,,Nope"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareLegRuleInvalid{LegGroupID: "g", Reason: `unknown fare_product_id "Nope"`}},
		},
		{
			desc:         "unknown network_id",
			lines:        []string{header, "g,nope,,,,,Bus"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareLegRuleInvalid{LegGroupID: "g", Reason: `unknown network_id "nope"`}},
		},
		{
			desc:         "unknown to_area_id",
			lines:        []string{header, "g,,,nope,,,Bus"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareLegRuleInvalid{LegGroupID: "g", Reason: `unknown to_area_id "nope"`}},
		},
		{
			desc:         "unknown timeframe group",
			lines:        []string{header, "g,,,,,offpeak,Bus"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareLegRuleInvalid{LegGroupID: "g", Reason: `unknown to_timeframe_group_id "offpeak"`}},
		},
		{
			desc:         "negative rule_priority",
			lines:        []string{withPriority, "g,,,,,,Bus,-1"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareLegRuleInvalid{LegGroupID: "g", Reason: `invalid rule_priority "-1"`}},
		},
		{
			desc:         "missing fare_product_id",
			lines:        []string{header, "g,,,,,,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareLegRuleInvalid{LegGroupID: "g", Reason: "missing values [fare_product_id]"}},
		},
		{
			desc:         "repeated key in another leg group",
			lines:        []string{header, "g1,n1,,,,,Bus", "g2,n1,,,,,Bus"},
			want:         []string{"group=g1 network=n1 from= to= fromTime= toTime= product=Bus priority=none"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareLegRuleInvalid{LegGroupID: "g2", Reason: "duplicate network_id, from_area_id, to_area_id, from_timeframe_group_id, to_timeframe_group_id and fare_product_id"}},
		},
		{
			desc:         "unreadable fare_leg_rules.txt is dropped",
			lines:        []string{header, "g1,n1,,,,,Bus", "broken"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FileUnreadable{Reason: "record on line 3: wrong number of fields"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			static := parseFaresV2Feed(t, legRuleFiles(map[string][]string{"fare_leg_rules.txt": tc.lines}))
			if diff := cmp.Diff(summarizeFareLegRules(static.FareLegRules), tc.want); diff != "" {
				t.Errorf("FareLegRules mismatch (-got +want):\n%s", diff)
			}
			if diff := cmp.Diff(warningKinds(static.Warnings), tc.wantWarnings); diff != "" {
				t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

// fare_leg_rules.txt can name a network from networks.txt as well as from
// routes.txt.
func TestParseFareLegRules_NetworksFile(t *testing.T) {
	static := parseFaresV2Feed(t, legRuleFiles(map[string][]string{
		"routes.txt":         {"route_id,route_type", "route_id,3"},
		"networks.txt":       {"network_id", "n2"},
		"fare_leg_rules.txt": {"network_id,fare_product_id", "n2,Bus"},
	}))
	if len(static.FareLegRules) != 1 || static.FareLegRules[0].Network != &static.Networks[0] {
		t.Errorf("got rules %v, want one pointing at Static.Networks[0]", summarizeFareLegRules(static.FareLegRules))
	}
}

func TestParseTimeframes_ServiceIsShared(t *testing.T) {
	static := parseFaresV2Feed(t, map[string][]string{"timeframes.txt": {"timeframe_group_id,service_id", "g,service_id"}})
	if len(static.Timeframes) != 1 || static.Timeframes[0].Service != &static.Services[0] {
		t.Error("Timeframe.Service does not point into Static.Services")
	}
}
