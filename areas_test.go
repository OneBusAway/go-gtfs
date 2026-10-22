package gtfs

import (
	"testing"

	"github.com/OneBusAway/go-gtfs/warnings"
	"github.com/google/go-cmp/cmp"
)

// areaStops is a station ST with platforms P1 and P2, entrance E, and a
// standalone stop S.
var areaStops = []string{
	"stop_id,location_type,parent_station",
	"stop_id,,",
	"ST,1,",
	"P1,0,ST",
	"P2,0,ST",
	"E,2,ST",
	"S,0,",
}

func parseAreasFeed(t *testing.T, areas, stopAreas []string) *Static {
	t.Helper()
	b := newZipBuilderWithDefaults().add("stops.txt", areaStops...)
	if areas != nil {
		b.add("areas.txt", areas...)
	}
	if stopAreas != nil {
		b.add("stop_areas.txt", stopAreas...)
	}
	static, err := ParseStatic(b.build(), ParseStaticOptions{})
	if err != nil {
		t.Fatalf("ParseStatic: %s", err)
	}
	return static
}

// areaMembers maps each area ID to the IDs of its listed stops.
func areaMembers(static *Static) map[string][]string {
	m := map[string][]string{}
	for _, a := range static.Areas {
		m[a.Id] = []string{}
		for _, s := range a.Stops {
			m[a.Id] = append(m[a.Id], s.Id)
		}
	}
	return m
}

func TestParseAreas(t *testing.T) {
	for _, tc := range []struct {
		desc         string
		areas        []string
		stopAreas    []string
		wantNames    map[string]string
		wantMembers  map[string][]string
		wantWarnings []warnings.StaticWarningKind
	}{
		{
			desc:        "no areas",
			wantNames:   map[string]string{},
			wantMembers: map[string][]string{},
		},
		{
			desc:        "areas without stops",
			areas:       []string{"area_id,area_name", "zone1,Zone 1", "zone2,"},
			wantNames:   map[string]string{"zone1": "Zone 1", "zone2": ""},
			wantMembers: map[string][]string{"zone1": {}, "zone2": {}},
		},
		{
			desc:        "one stop in two areas",
			areas:       []string{"area_id,area_name", "downtown,Downtown", "route_426,Route 426"},
			stopAreas:   []string{"area_id,stop_id", "downtown,S", "route_426,S", "downtown,ST"},
			wantNames:   map[string]string{"downtown": "Downtown", "route_426": "Route 426"},
			wantMembers: map[string][]string{"downtown": {"S", "ST"}, "route_426": {"S"}},
		},
		{
			desc:         "unknown area_id",
			areas:        []string{"area_id", "zone1"},
			stopAreas:    []string{"area_id,stop_id", "nope,S", "zone1,S"},
			wantNames:    map[string]string{"zone1": ""},
			wantMembers:  map[string][]string{"zone1": {"S"}},
			wantWarnings: []warnings.StaticWarningKind{warnings.StopAreaInvalid{AreaID: "nope", StopID: "S", Reason: "unknown area_id"}},
		},
		{
			desc:         "unknown stop_id",
			areas:        []string{"area_id", "zone1"},
			stopAreas:    []string{"area_id,stop_id", "zone1,nope"},
			wantNames:    map[string]string{"zone1": ""},
			wantMembers:  map[string][]string{"zone1": {}},
			wantWarnings: []warnings.StaticWarningKind{warnings.StopAreaInvalid{AreaID: "zone1", StopID: "nope", Reason: "unknown stop_id"}},
		},
		{
			desc:         "stop_areas.txt without areas.txt",
			stopAreas:    []string{"area_id,stop_id", "zone1,S"},
			wantNames:    map[string]string{},
			wantMembers:  map[string][]string{},
			wantWarnings: []warnings.StaticWarningKind{warnings.StopAreaInvalid{AreaID: "zone1", StopID: "S", Reason: "unknown area_id"}},
		},
		{
			desc:         "repeated row",
			areas:        []string{"area_id", "zone1"},
			stopAreas:    []string{"area_id,stop_id", "zone1,S", "zone1,S"},
			wantNames:    map[string]string{"zone1": ""},
			wantMembers:  map[string][]string{"zone1": {"S"}},
			wantWarnings: []warnings.StaticWarningKind{warnings.StopAreaInvalid{AreaID: "zone1", StopID: "S", Reason: "duplicate row"}},
		},
		{
			desc:         "missing value",
			areas:        []string{"area_id", "zone1"},
			stopAreas:    []string{"area_id,stop_id", "zone1,"},
			wantNames:    map[string]string{"zone1": ""},
			wantMembers:  map[string][]string{"zone1": {}},
			wantWarnings: []warnings.StaticWarningKind{warnings.StopAreaInvalid{AreaID: "zone1", Reason: "missing values [stop_id]"}},
		},
		{
			desc:        "duplicate and empty area_id",
			areas:       []string{"area_id,area_name", "zone1,First", "zone1,Second", ",Nameless"},
			wantNames:   map[string]string{"zone1": "First"},
			wantMembers: map[string][]string{"zone1": {}},
			wantWarnings: []warnings.StaticWarningKind{
				warnings.AreaInvalid{AreaID: "zone1", Reason: "duplicate area_id"},
				warnings.AreaInvalid{Reason: "missing values [area_id]"},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			static := parseAreasFeed(t, tc.areas, tc.stopAreas)
			names := map[string]string{}
			for _, a := range static.Areas {
				names[a.Id] = a.Name
			}
			if diff := cmp.Diff(names, tc.wantNames); diff != "" {
				t.Errorf("area names mismatch (-got +want):\n%s", diff)
			}
			if diff := cmp.Diff(areaMembers(static), tc.wantMembers); diff != "" {
				t.Errorf("area members mismatch (-got +want):\n%s", diff)
			}
			if diff := cmp.Diff(warningKinds(static.Warnings), tc.wantWarnings); diff != "" {
				t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

func TestAreaContains(t *testing.T) {
	// Station ST is in "station_area". Platform P2 is also assigned to
	// "p2_area", which overrides its station's area for it.
	static := parseAreasFeed(t,
		[]string{"area_id", "station_area", "p2_area", "s_area"},
		[]string{"area_id,stop_id", "station_area,ST", "p2_area,P2", "s_area,S"},
	)
	areas := areasByID(static.Areas)
	stops := stopsByID(static.Stops)
	for _, tc := range []struct {
		area, stop string
		want       bool
	}{
		{"station_area", "ST", true},
		{"station_area", "P1", true},  // platform of a station in the area
		{"station_area", "P2", false}, // assigned to another area, which overrides
		{"p2_area", "P2", true},
		{"p2_area", "P1", false},
		{"station_area", "E", false}, // only platforms follow their station
		{"s_area", "S", true},
		{"station_area", "S", false},
	} {
		if got := areas[tc.area].Contains(stops[tc.stop]); got != tc.want {
			t.Errorf("%s.Contains(%s) = %t, want %t", tc.area, tc.stop, got, tc.want)
		}
	}
	if areas["station_area"].Contains(nil) {
		t.Error("Contains(nil) = true, want false")
	}
}

// A platform listed in its own station's area is in it, of course, even
// though being listed somewhere stops it following its station.
func TestAreaContains_PlatformListedWithItsStation(t *testing.T) {
	static := parseAreasFeed(t,
		[]string{"area_id", "a"},
		[]string{"area_id,stop_id", "a,ST", "a,P1"},
	)
	stops := stopsByID(static.Stops)
	a := &static.Areas[0]
	if !a.Contains(stops["P1"]) || !a.Contains(stops["P2"]) {
		t.Errorf("Contains(P1), Contains(P2) = %t, %t, want true, true", a.Contains(stops["P1"]), a.Contains(stops["P2"]))
	}
}

// Stop.Areas lists the areas stop_areas.txt names for each stop.
func TestParseAreas_StopAreas(t *testing.T) {
	static := parseAreasFeed(t,
		[]string{"area_id", "downtown", "route_426"},
		[]string{"area_id,stop_id", "downtown,S", "route_426,S", "downtown,ST"},
	)
	got := map[string][]string{}
	for _, stop := range static.Stops {
		for _, a := range stop.Areas {
			got[stop.Id] = append(got[stop.Id], a.Id)
		}
	}
	want := map[string][]string{"S": {"downtown", "route_426"}, "ST": {"downtown"}}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("stop areas mismatch (-got +want):\n%s", diff)
	}
	areas := areasByID(static.Areas)
	if s := stopsByID(static.Stops)["S"]; s.Areas[0] != areas["downtown"] {
		t.Error("Stop.Areas does not point into Static.Areas")
	}
}

// A parsed feed with areas must stay comparable with cmp, which panics on
// unexported fields.
func TestParseAreas_ComparableWithCmp(t *testing.T) {
	static := parseAreasFeed(t, []string{"area_id", "a"}, []string{"area_id,stop_id", "a,S"})
	if diff := cmp.Diff(static.Areas, static.Areas); diff != "" {
		t.Errorf("Areas differ from themselves:\n%s", diff)
	}
}
