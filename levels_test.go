package gtfs

import (
	"testing"

	"github.com/OneBusAway/go-gtfs/warnings"
	"github.com/google/go-cmp/cmp"
)

// parseLevelsFeed parses the default feed with the given stops.txt and, if
// levelLines is not nil, levels.txt.
func parseLevelsFeed(t *testing.T, stopLines []string, levelLines ...string) *Static {
	t.Helper()
	b := newZipBuilderWithDefaults().add("stops.txt", stopLines...)
	if levelLines != nil {
		b.add("levels.txt", levelLines...)
	}
	static, err := ParseStatic(b.build(), ParseStaticOptions{})
	if err != nil {
		t.Fatalf("ParseStatic: %s", err)
	}
	return static
}

// stopLevels maps each stop ID to its level's ID, or "" for no level.
func stopLevels(static *Static) map[string]string {
	m := map[string]string{}
	for _, stop := range static.Stops {
		m[stop.Id] = ""
		if stop.Level != nil {
			m[stop.Id] = stop.Level.Id
		}
	}
	return m
}

func TestParseLevels(t *testing.T) {
	defaultStops := []string{"stop_id,level_id", "stop_id,"}
	for _, tc := range []struct {
		desc           string
		stops          []string
		levels         []string
		wantLevels     []Level
		wantStopLevels map[string]string
		wantWarnings   []warnings.StaticWarningKind
	}{
		{
			desc:           "no levels.txt",
			stops:          defaultStops,
			wantStopLevels: map[string]string{"stop_id": ""},
		},
		{
			desc:  "levels above, at and below ground",
			stops: defaultStops,
			levels: []string{
				"level_id,level_index,level_name",
				"mezz,1,Mezzanine",
				"street,0,Street",
				"red,-3.0000000,Red Line platforms",
				"half,-0.5,",
			},
			wantLevels: []Level{
				{Id: "mezz", Index: 1, Name: "Mezzanine"},
				{Id: "street", Index: 0, Name: "Street"},
				{Id: "red", Index: -3, Name: "Red Line platforms"},
				{Id: "half", Index: -0.5},
			},
			wantStopLevels: map[string]string{"stop_id": ""},
		},
		{
			desc:           "stops point at their level",
			stops:          []string{"stop_id,level_id", "stop_id,", "platform,red", "entrance,street", "lobby,street"},
			levels:         []string{"level_id,level_index", "street,0", "red,-3"},
			wantLevels:     []Level{{Id: "street", Index: 0}, {Id: "red", Index: -3}},
			wantStopLevels: map[string]string{"stop_id": "", "platform": "red", "entrance": "street", "lobby": "street"},
		},
		{
			desc:           "unknown level_id keeps the stop without a level",
			stops:          []string{"stop_id,level_id", "stop_id,", "platform,nope"},
			levels:         []string{"level_id,level_index", "street,0"},
			wantLevels:     []Level{{Id: "street", Index: 0}},
			wantStopLevels: map[string]string{"stop_id": "", "platform": ""},
			wantWarnings:   []warnings.StaticWarningKind{warnings.StopUnknownLevel{StopID: "platform", LevelID: "nope"}},
		},
		{
			desc:           "level_id without levels.txt",
			stops:          []string{"stop_id,level_id", "stop_id,", "platform,red"},
			wantStopLevels: map[string]string{"stop_id": "", "platform": ""},
			wantWarnings:   []warnings.StaticWarningKind{warnings.StopUnknownLevel{StopID: "platform", LevelID: "red"}},
		},
		{
			desc:           "invalid level_index skips the level",
			stops:          []string{"stop_id,level_id", "stop_id,", "platform,bad"},
			levels:         []string{"level_id,level_index", "bad,first", "street,0"},
			wantLevels:     []Level{{Id: "street", Index: 0}},
			wantStopLevels: map[string]string{"stop_id": "", "platform": ""},
			wantWarnings: []warnings.StaticWarningKind{
				warnings.LevelInvalid{LevelID: "bad", Reason: `invalid level_index "first"`},
				warnings.StopUnknownLevel{StopID: "platform", LevelID: "bad"},
			},
		},
		{
			desc:           "non-finite level_index skips the level",
			stops:          defaultStops,
			levels:         []string{"level_id,level_index", "nan,NaN", "inf,-Inf", "street,0"},
			wantLevels:     []Level{{Id: "street", Index: 0}},
			wantStopLevels: map[string]string{"stop_id": ""},
			wantWarnings: []warnings.StaticWarningKind{
				warnings.LevelInvalid{LevelID: "nan", Reason: `invalid level_index "NaN"`},
				warnings.LevelInvalid{LevelID: "inf", Reason: `invalid level_index "-Inf"`},
			},
		},
		{
			desc:           "empty level_index skips the level",
			stops:          defaultStops,
			levels:         []string{"level_id,level_index", "street,"},
			wantStopLevels: map[string]string{"stop_id": ""},
			wantWarnings:   []warnings.StaticWarningKind{warnings.LevelInvalid{LevelID: "street", Reason: "missing values [level_index]"}},
		},
		{
			desc:           "empty level_id skips the level",
			stops:          defaultStops,
			levels:         []string{"level_id,level_index", ",0"},
			wantStopLevels: map[string]string{"stop_id": ""},
			wantWarnings:   []warnings.StaticWarningKind{warnings.LevelInvalid{Reason: "missing values [level_id]"}},
		},
		{
			desc:           "duplicate level_id keeps the first",
			stops:          []string{"stop_id,level_id", "stop_id,street"},
			levels:         []string{"level_id,level_index,level_name", "street,0,First", "street,1,Second"},
			wantLevels:     []Level{{Id: "street", Index: 0, Name: "First"}},
			wantStopLevels: map[string]string{"stop_id": "street"},
			wantWarnings:   []warnings.StaticWarningKind{warnings.LevelInvalid{LevelID: "street", Reason: "duplicate level_id"}},
		},
		{
			desc:           "missing level_index column",
			stops:          defaultStops,
			levels:         []string{"level_id,level_name", "street,Street"},
			wantStopLevels: map[string]string{"stop_id": ""},
			wantWarnings:   []warnings.StaticWarningKind{warnings.MissingColumns{Columns: []string{"level_index"}}},
		},
		{
			desc:           "unreadable levels.txt keeps the levels read before the error",
			stops:          []string{"stop_id,level_id", "stop_id,street"},
			levels:         []string{"level_id,level_index", "street,0", "broken"},
			wantLevels:     []Level{{Id: "street", Index: 0}},
			wantStopLevels: map[string]string{"stop_id": "street"},
			wantWarnings:   []warnings.StaticWarningKind{warnings.FileUnreadable{Reason: "record on line 3: wrong number of fields"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			static := parseLevelsFeed(t, tc.stops, tc.levels...)
			if diff := cmp.Diff(static.Levels, tc.wantLevels); diff != "" {
				t.Errorf("Levels mismatch (-got +want):\n%s", diff)
			}
			if diff := cmp.Diff(stopLevels(static), tc.wantStopLevels); diff != "" {
				t.Errorf("stop levels mismatch (-got +want):\n%s", diff)
			}
			if diff := cmp.Diff(warningKinds(static.Warnings), tc.wantWarnings); diff != "" {
				t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

// A stop's Level must point into Static.Levels, not at a copy.
func TestParseLevels_StopLevelIsShared(t *testing.T) {
	static := parseLevelsFeed(t,
		[]string{"stop_id,level_id", "stop_id,", "a,street", "b,street"},
		"level_id,level_index", "street,0",
	)
	want := &static.Levels[0]
	for _, stop := range static.Stops[1:] {
		if stop.Level != want {
			t.Errorf("stop %s Level = %p, want %p (&Static.Levels[0])", stop.Id, stop.Level, want)
		}
	}
}
