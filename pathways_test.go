package gtfs

import (
	"fmt"
	"testing"

	"github.com/OneBusAway/go-gtfs/warnings"
	"github.com/google/go-cmp/cmp"
)

// pathwayStops is a station ST with entrance E, generic node N, boarding
// area B, platform P and platform D, which riders reach directly from the
// street (stop_access 1).
var pathwayStops = []string{
	"stop_id,location_type,parent_station,stop_access",
	"stop_id,,,",
	"ST,1,,",
	"E,2,ST,",
	"N,3,ST,",
	"P,0,ST,0",
	"B,4,P,",
	"D,0,ST,1",
}

func parsePathwaysFeed(t *testing.T, stopLines []string, pathwayLines ...string) *Static {
	t.Helper()
	b := newZipBuilderWithDefaults().add("stops.txt", stopLines...)
	if pathwayLines != nil {
		b.add("pathways.txt", pathwayLines...)
	}
	static, err := ParseStatic(b.build(), ParseStaticOptions{})
	if err != nil {
		t.Fatalf("ParseStatic: %s", err)
	}
	return static
}

// summarizePathways renders each pathway as one line of the fields that are
// set, so a test can state the expected result without building linked Stop
// values.
func summarizePathways(pathways []Pathway) []string {
	var s []string
	for _, p := range pathways {
		line := fmt.Sprintf("%s %s->%s %s", p.Id, stopID(p.From), stopID(p.To), p.Mode)
		if p.IsBidirectional {
			line += " bidirectional"
		}
		for _, f := range []struct {
			name  string
			value any
		}{
			{"length", p.Length}, {"time", p.TraversalTime}, {"stairs", p.StairCount},
			{"slope", p.MaxSlope}, {"width", p.MinWidth},
		} {
			switch v := f.value.(type) {
			case *float64:
				if v != nil {
					line += fmt.Sprintf(" %s=%g", f.name, *v)
				}
			case *int32:
				if v != nil {
					line += fmt.Sprintf(" %s=%d", f.name, *v)
				}
			}
		}
		if p.SignpostedAs != "" {
			line += fmt.Sprintf(" sign=%q", p.SignpostedAs)
		}
		if p.ReversedSignpostedAs != "" {
			line += fmt.Sprintf(" reversed=%q", p.ReversedSignpostedAs)
		}
		s = append(s, line)
	}
	return s
}

func TestParsePathways(t *testing.T) {
	const header = "pathway_id,from_stop_id,to_stop_id,pathway_mode,is_bidirectional"
	const fullHeader = header + ",length,traversal_time,stair_count,max_slope,min_width,signposted_as,reversed_signposted_as"
	for _, tc := range []struct {
		desc         string
		pathways     []string
		wantSummary  []string
		wantWarnings []warnings.StaticWarningKind
	}{
		{
			desc: "no pathways.txt",
		},
		{
			desc:     "all fields",
			pathways: []string{fullHeader, "W1,E,N,1,1,12.5,30,-20,0.083,0.9,To trains,To street"},
			wantSummary: []string{
				`W1 E->N WALKWAY bidirectional length=12.5 time=30 stairs=-20 slope=0.083 width=0.9 sign="To trains" reversed="To street"`,
			},
		},
		{
			desc: "every mode",
			pathways: []string{
				header,
				"1,E,N,1,0", "2,E,N,2,0", "3,E,N,3,0", "4,E,N,4,0", "5,E,N,5,0", "6,E,N,6,0", "7,N,E,7,0",
			},
			wantSummary: []string{
				"1 E->N WALKWAY",
				"2 E->N STAIRS",
				"3 E->N MOVING_SIDEWALK",
				"4 E->N ESCALATOR",
				"5 E->N ELEVATOR",
				"6 E->N FARE_GATE",
				"7 N->E EXIT_GATE",
			},
		},
		{
			desc:     "platform and boarding area",
			pathways: []string{header + ",stair_count", "S,N,B,2,1,12", "S2,B,P,1,1,"},
			wantSummary: []string{
				`S N->B STAIRS bidirectional stairs=12`,
				`S2 B->P WALKWAY bidirectional`,
			},
		},
		{
			desc:         "unknown to_stop_id",
			pathways:     []string{header, "W,E,NOPE,1,0", "W2,E,N,1,0"},
			wantSummary:  []string{`W2 E->N WALKWAY`},
			wantWarnings: []warnings.StaticWarningKind{warnings.PathwayInvalid{PathwayID: "W", Reason: `unknown to_stop_id "NOPE"`}},
		},
		{
			desc:         "unknown from_stop_id",
			pathways:     []string{header, "W,NOPE,N,1,0"},
			wantWarnings: []warnings.StaticWarningKind{warnings.PathwayInvalid{PathwayID: "W", Reason: `unknown from_stop_id "NOPE"`}},
		},
		{
			desc:         "from a station",
			pathways:     []string{header, "W,ST,N,1,0"},
			wantWarnings: []warnings.StaticWarningKind{warnings.PathwayInvalid{PathwayID: "W", Reason: `from_stop_id "ST" is a station`}},
		},
		{
			desc:         "to a stop reached directly from the street",
			pathways:     []string{header, "W,E,D,1,0"},
			wantWarnings: []warnings.StaticWarningKind{warnings.PathwayInvalid{PathwayID: "W", Reason: `to_stop_id "D" has stop_access 1`}},
		},
		{
			desc:         "invalid pathway_mode",
			pathways:     []string{header, "W,E,N,8,0"},
			wantWarnings: []warnings.StaticWarningKind{warnings.PathwayInvalid{PathwayID: "W", Reason: `invalid pathway_mode "8"`}},
		},
		{
			desc:         "invalid is_bidirectional",
			pathways:     []string{header, "W,E,N,1,2"},
			wantWarnings: []warnings.StaticWarningKind{warnings.PathwayInvalid{PathwayID: "W", Reason: `invalid is_bidirectional "2"`}},
		},
		{
			desc:         "missing pathway_mode and is_bidirectional",
			pathways:     []string{header, "W,E,N,,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.PathwayInvalid{PathwayID: "W", Reason: "missing values [pathway_mode is_bidirectional]"}},
		},
		{
			desc:        "padded enums are trimmed",
			pathways:    []string{header, "W,E,N, 5,1 "},
			wantSummary: []string{`W E->N ELEVATOR bidirectional`},
		},
		{
			desc:         "duplicate pathway_id keeps the first",
			pathways:     []string{header, "W,E,N,1,0", "W,N,E,2,0"},
			wantSummary:  []string{`W E->N WALKWAY`},
			wantWarnings: []warnings.StaticWarningKind{warnings.PathwayInvalid{PathwayID: "W", Reason: "duplicate pathway_id"}},
		},
		{
			desc:     "out-of-range optional values are dropped with a warning",
			pathways: []string{fullHeader, "W,E,N,1,0,-1,0,0,steep,0,,"},
			wantSummary: []string{
				`W E->N WALKWAY`,
			},
			wantWarnings: []warnings.StaticWarningKind{
				warnings.PathwayInvalidValue{PathwayID: "W", Column: "length", Value: "-1"},
				warnings.PathwayInvalidValue{PathwayID: "W", Column: "traversal_time", Value: "0"},
				warnings.PathwayInvalidValue{PathwayID: "W", Column: "stair_count", Value: "0"},
				warnings.PathwayInvalidValue{PathwayID: "W", Column: "max_slope", Value: "steep"},
				warnings.PathwayInvalidValue{PathwayID: "W", Column: "min_width", Value: "0"},
			},
		},
		{
			desc:     "zero length and slope are valid",
			pathways: []string{fullHeader, "W,E,N,6,0,0,5,,0,1.5,,"},
			wantSummary: []string{
				`W E->N FARE_GATE length=0 time=5 slope=0 width=1.5`,
			},
		},
		{
			desc:         "missing required column",
			pathways:     []string{"pathway_id,from_stop_id,to_stop_id,pathway_mode", "W,E,N,1"},
			wantWarnings: []warnings.StaticWarningKind{warnings.MissingColumns{Columns: []string{"is_bidirectional"}}},
		},
		{
			desc:         "unreadable pathways.txt is dropped",
			pathways:     []string{header, "W,E,N,1,0", "broken"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FileUnreadable{Reason: "record on line 3: wrong number of fields"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			static := parsePathwaysFeed(t, pathwayStops, tc.pathways...)
			if diff := cmp.Diff(summarizePathways(static.Pathways), tc.wantSummary); diff != "" {
				t.Errorf("Pathways mismatch (-got +want):\n%s", diff)
			}
			if diff := cmp.Diff(warningKinds(static.Warnings), tc.wantWarnings); diff != "" {
				t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

// Pathway ends must point into Static.Stops, not at copies.
func TestParsePathways_EndsAreShared(t *testing.T) {
	static := parsePathwaysFeed(t, pathwayStops, "pathway_id,from_stop_id,to_stop_id,pathway_mode,is_bidirectional", "W,E,N,1,0")
	stops := stopsByID(static.Stops)
	if got := static.Pathways[0]; got.From != stops["E"] || got.To != stops["N"] {
		t.Errorf("pathway ends %p, %p are not &Static.Stops entries %p, %p", got.From, got.To, stops["E"], stops["N"])
	}
}

func TestParseStopAccess(t *testing.T) {
	for _, tc := range []struct {
		desc         string
		row          string
		want         StopAccess
		wantWarnings []warnings.StaticWarningKind
	}{
		{desc: "empty", row: "P,0,ST,", want: StopAccess_NotSpecified},
		{desc: "0", row: "P,0,ST,0", want: StopAccess_ThroughStation},
		{desc: "1", row: "P,,ST,1", want: StopAccess_Direct},
		{desc: "padded", row: "P,0,ST, 1", want: StopAccess_Direct},
		{
			desc:         "invalid value",
			row:          "P,0,ST,2",
			want:         StopAccess_NotSpecified,
			wantWarnings: []warnings.StaticWarningKind{warnings.StopAccessInvalid{StopID: "P", Value: "2", Reason: "not a valid value"}},
		},
		{
			desc:         "on an entrance",
			row:          "P,2,ST,1",
			want:         StopAccess_NotSpecified,
			wantWarnings: []warnings.StaticWarningKind{warnings.StopAccessInvalid{StopID: "P", Value: "1", Reason: "only a stop (location_type 0) may set it"}},
		},
		{
			desc:         "without a parent station",
			row:          "P,0,,0",
			want:         StopAccess_NotSpecified,
			wantWarnings: []warnings.StaticWarningKind{warnings.StopAccessInvalid{StopID: "P", Value: "0", Reason: "the stop has no parent_station"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			static := parsePathwaysFeed(t, []string{"stop_id,location_type,parent_station,stop_access", "stop_id,,,", "ST,1,,", tc.row})
			if got := stopsByID(static.Stops)["P"].Access; got != tc.want {
				t.Errorf("Access = %s, want %s", got, tc.want)
			}
			if diff := cmp.Diff(warningKinds(static.Warnings), tc.wantWarnings); diff != "" {
				t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
			}
		})
	}
}
