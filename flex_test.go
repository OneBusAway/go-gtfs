package gtfs

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/OneBusAway/go-gtfs/constants"
	"github.com/OneBusAway/go-gtfs/warnings"
	"github.com/google/go-cmp/cmp"
)

// Shared flex fixture: two stops, two zones, one group of both stops, one
// booking rule. Tests add the stop_times.txt they exercise.

const (
	flexZoneAGeometry = `{"type":"Polygon","coordinates":[[[-85.0,45.0],[-84.9,45.0],[-84.9,45.1],[-85.0,45.0]]]}`
	flexZoneBGeometry = `{"type":"MultiPolygon","coordinates":[[[[-86.0,44.0],[-85.9,44.0],[-85.9,44.1],[-86.0,44.0]]],[[[-86.5,44.5],[-86.4,44.5],[-86.4,44.6],[-86.5,44.5]]]]}`
	flexZonesGeoJSON  = `{"type":"FeatureCollection","features":[` +
		`{"type":"Feature","id":"zone_a","properties":{"stop_name":"Zone A"},"geometry":` + flexZoneAGeometry + `},` +
		`{"type":"Feature","id":"zone_b","properties":{"stop_name":"Zone B"},"geometry":` + flexZoneBGeometry + `}]}`

	flexStopTimesHeader = "trip_id,stop_sequence,stop_id,location_id,location_group_id," +
		"arrival_time,departure_time,start_pickup_drop_off_window,end_pickup_drop_off_window," +
		"pickup_type,drop_off_type,pickup_booking_rule_id,drop_off_booking_rule_id,timepoint"
)

func newFlexZipBuilder() *zipBuilder {
	return newZipBuilder().add(
		"agency.txt", "agency_id,agency_name,agency_url,agency_timezone\na,b,c,d",
	).add(
		"routes.txt", "route_id,route_type\nroute_id,3",
	).add(
		"stops.txt", "stop_id,stop_lat,stop_lon\nstop_1,45.0,-85.0\nstop_2,45.1,-85.1",
	).add(
		"calendar.txt",
		"service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date\n"+
			"service_id,0,0,0,0,0,0,0,20220504,20220507",
	).add(
		"trips.txt", "route_id,service_id,trip_id\nroute_id,service_id,trip_id",
	).add(
		"booking_rules.txt", "booking_rule_id,booking_type,prior_notice_duration_min\nbr_1,1,60",
	).add(
		"locations.geojson", flexZonesGeoJSON,
	).add(
		"location_groups.txt", "location_group_id,location_group_name\ngroup_1,Group One",
	).add(
		"location_group_stops.txt", "location_group_id,stop_id\ngroup_1,stop_1\ngroup_1,stop_2",
	)
}

func flexStop1() *Stop { return &Stop{Id: "stop_1", Latitude: ptr(45.0), Longitude: ptr(-85.0)} }
func flexStop2() *Stop { return &Stop{Id: "stop_2", Latitude: ptr(45.1), Longitude: ptr(-85.1)} }

func flexZoneA() *Location {
	return &Location{
		Id:   "zone_a",
		Name: "Zone A",
		Geometry: LocationGeometry{
			Type:     "Polygon",
			Polygons: [][][][2]float64{{{{-85.0, 45.0}, {-84.9, 45.0}, {-84.9, 45.1}, {-85.0, 45.0}}}},
			Raw:      json.RawMessage(flexZoneAGeometry),
		},
	}
}

func flexZoneB() *Location {
	return &Location{
		Id:   "zone_b",
		Name: "Zone B",
		Geometry: LocationGeometry{
			Type: "MultiPolygon",
			Polygons: [][][][2]float64{
				{{{-86.0, 44.0}, {-85.9, 44.0}, {-85.9, 44.1}, {-86.0, 44.0}}},
				{{{-86.5, 44.5}, {-86.4, 44.5}, {-86.4, 44.6}, {-86.5, 44.5}}},
			},
			Raw: json.RawMessage(flexZoneBGeometry),
		},
	}
}

func flexGroup1() *LocationGroup {
	return &LocationGroup{Id: "group_1", Name: "Group One", Stops: []*Stop{flexStop1(), flexStop2()}}
}

func flexBookingRule1() *BookingRule {
	return &BookingRule{Id: "br_1", Type: BookingType_SameDay, PriorNoticeDurationMin: ptr(int32(60))}
}

func cells(row string) []string { return strings.Split(row, ",") }

func hhmm(h, m int) *time.Duration {
	d := time.Duration(h)*time.Hour + time.Duration(m)*time.Minute
	return &d
}

func TestParseStatic_FlexStopTimeValidation(t *testing.T) {
	for _, tc := range []struct {
		desc          string
		row           string
		wantStopTimes []ScheduledStopTime
		wantWarning   warnings.StaticWarningKind // nil means no warning
	}{
		{
			desc: "zone record",
			row:  "trip_id,1,,zone_a,,,,08:00:00,17:00:00,2,1,br_1,br_1,",
			wantStopTimes: []ScheduledStopTime{{
				Location:                 flexZoneA(),
				StopSequence:             1,
				StartPickupDropOffWindow: hhmm(8, 0),
				EndPickupDropOffWindow:   hhmm(17, 0),
				PickupType:               PickupDropOffPolicy_PhoneAgency,
				DropOffType:              PickupDropOffPolicy_No,
				ContinuousPickup:         PickupDropOffPolicy_No,
				ContinuousDropOff:        PickupDropOffPolicy_No,
				PickupBookingRule:        flexBookingRule1(),
				DropOffBookingRule:       flexBookingRule1(),
				ExactTimes:               false,
			}},
		},
		{
			desc: "group record",
			row:  "trip_id,1,,,group_1,,,08:00:00,17:00:00,1,2,,br_1,",
			wantStopTimes: []ScheduledStopTime{{
				LocationGroup:            flexGroup1(),
				StopSequence:             1,
				StartPickupDropOffWindow: hhmm(8, 0),
				EndPickupDropOffWindow:   hhmm(17, 0),
				PickupType:               PickupDropOffPolicy_No,
				DropOffType:              PickupDropOffPolicy_PhoneAgency,
				ContinuousPickup:         PickupDropOffPolicy_No,
				ContinuousDropOff:        PickupDropOffPolicy_No,
				DropOffBookingRule:       flexBookingRule1(),
			}},
		},
		{
			// Spec §9.1: a stop_id record may carry windows. timepoint=1 is
			// ignored because a windowed record has no exact time.
			desc: "windowed stop record ignores timepoint",
			row:  "trip_id,1,stop_1,,,,,08:00:00,17:00:00,2,2,,,1",
			wantStopTimes: []ScheduledStopTime{{
				Stop:                     flexStop1(),
				StopSequence:             1,
				StartPickupDropOffWindow: hhmm(8, 0),
				EndPickupDropOffWindow:   hhmm(17, 0),
				PickupType:               PickupDropOffPolicy_PhoneAgency,
				DropOffType:              PickupDropOffPolicy_PhoneAgency,
				ContinuousPickup:         PickupDropOffPolicy_No,
				ContinuousDropOff:        PickupDropOffPolicy_No,
				ExactTimes:               false,
			}},
		},
		{
			desc: "timed stop record is unchanged by the flex columns",
			row:  "trip_id,1,stop_1,,,08:00:00,08:00:00,,,,,,,1",
			wantStopTimes: []ScheduledStopTime{{
				Stop:              flexStop1(),
				StopSequence:      1,
				ArrivalTime:       8 * time.Hour,
				DepartureTime:     8 * time.Hour,
				ContinuousPickup:  PickupDropOffPolicy_No,
				ContinuousDropOff: PickupDropOffPolicy_No,
				ExactTimes:        true,
			}},
		},
		{
			desc:        "no stop, location or group",
			row:         "trip_id,1,,,,,,08:00:00,17:00:00,2,1,,,",
			wantWarning: warnings.StopTimeInvalidReference{Reason: "row references 0 of stop_id, location_id and location_group_id; exactly one is required"},
		},
		{
			desc:        "both stop and location",
			row:         "trip_id,1,stop_1,zone_a,,,,08:00:00,17:00:00,2,1,,,",
			wantWarning: warnings.StopTimeInvalidReference{Reason: "row references 2 of stop_id, location_id and location_group_id; exactly one is required"},
		},
		{
			desc:        "unknown stop",
			row:         "trip_id,1,stop_x,,,08:00:00,08:00:00,,,,,,,",
			wantWarning: warnings.StopTimeInvalidReference{Reason: `unknown stop_id "stop_x"`},
		},
		{
			desc:        "unknown location",
			row:         "trip_id,1,,zone_x,,,,08:00:00,17:00:00,2,1,,,",
			wantWarning: warnings.StopTimeInvalidReference{Reason: `unknown location_id "zone_x"`},
		},
		{
			desc:        "unknown location group",
			row:         "trip_id,1,,,group_x,,,08:00:00,17:00:00,2,1,,,",
			wantWarning: warnings.StopTimeInvalidReference{Reason: `unknown location_group_id "group_x"`},
		},
		{
			desc:        "only start window",
			row:         "trip_id,1,,zone_a,,,,08:00:00,,2,1,,,",
			wantWarning: warnings.StopTimeInvalidWindow{Reason: "start_pickup_drop_off_window and end_pickup_drop_off_window must be set together"},
		},
		{
			desc:        "only end window",
			row:         "trip_id,1,,zone_a,,,,,17:00:00,2,1,,,",
			wantWarning: warnings.StopTimeInvalidWindow{Reason: "start_pickup_drop_off_window and end_pickup_drop_off_window must be set together"},
		},
		{
			desc:        "unparsable window",
			row:         "trip_id,1,,zone_a,,,,soon,17:00:00,2,1,,,",
			wantWarning: warnings.StopTimeInvalidWindow{Reason: `unparsable pickup/drop-off window "soon"-"17:00:00"`},
		},
		{
			desc:        "reversed window",
			row:         "trip_id,1,,zone_a,,,,17:00:00,08:00:00,2,1,,,",
			wantWarning: warnings.StopTimeInvalidWindow{Reason: `pickup/drop-off window "17:00:00"-"08:00:00" ends before it starts`},
		},
		{
			desc: "zero-length window",
			row:  "trip_id,1,,zone_a,,,,08:00:00,08:00:00,2,1,,,",
			wantStopTimes: []ScheduledStopTime{{
				Location:                 flexZoneA(),
				StopSequence:             1,
				StartPickupDropOffWindow: hhmm(8, 0),
				EndPickupDropOffWindow:   hhmm(8, 0),
				PickupType:               PickupDropOffPolicy_PhoneAgency,
				DropOffType:              PickupDropOffPolicy_No,
				ContinuousPickup:         PickupDropOffPolicy_No,
				ContinuousDropOff:        PickupDropOffPolicy_No,
			}},
		},
		{
			desc:        "location without windows",
			row:         "trip_id,1,,zone_a,,,,,,2,1,,,",
			wantWarning: warnings.StopTimeInvalidWindow{Reason: "location_id and location_group_id rows require start/end_pickup_drop_off_window"},
		},
		{
			desc:        "windows with arrival time",
			row:         "trip_id,1,,zone_a,,08:00:00,,08:00:00,17:00:00,2,1,,,",
			wantWarning: warnings.StopTimeInvalidWindow{Reason: "arrival_time/departure_time are forbidden on a row with a pickup/drop-off window"},
		},
		{
			desc:        "unknown pickup booking rule",
			row:         "trip_id,1,,zone_a,,,,08:00:00,17:00:00,2,1,br_x,,",
			wantWarning: warnings.StopTimeInvalidReference{Reason: `unknown pickup_booking_rule_id "br_x"`},
		},
		{
			desc:        "unknown drop-off booking rule",
			row:         "trip_id,1,,zone_a,,,,08:00:00,17:00:00,2,1,,br_x,",
			wantWarning: warnings.StopTimeInvalidReference{Reason: `unknown drop_off_booking_rule_id "br_x"`},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			content := newFlexZipBuilder().add("stop_times.txt", flexStopTimesHeader, tc.row).build()

			static, err := ParseStatic(content, ParseStaticOptions{})
			if err != nil {
				t.Fatalf("ParseStatic() got error %v, want nil", err)
			}
			if len(static.Trips) != 1 {
				t.Fatalf("got %d trips, want 1", len(static.Trips))
			}
			if diff := cmp.Diff(static.Trips[0].StopTimes, tc.wantStopTimes); diff != "" {
				t.Errorf("StopTimes mismatch (-got +want):\n%s", diff)
			}

			var wantWarnings []warnings.StaticWarning
			if tc.wantWarning != nil {
				wantWarnings = []warnings.StaticWarning{{
					Kind:          tc.wantWarning,
					File:          constants.StopTimesFile,
					RowNumber:     1,
					RowContent:    cells(tc.row),
					HeaderContent: cells(flexStopTimesHeader),
				}}
			}
			if diff := cmp.Diff(static.Warnings, wantWarnings); diff != "" {
				t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

func TestParseStatic_FlexStopIDColumnAbsent(t *testing.T) {
	// A pure zone feed may omit the stop_id column entirely.
	content := newFlexZipBuilder().add(
		"stop_times.txt",
		"trip_id,stop_sequence,location_id,start_pickup_drop_off_window,end_pickup_drop_off_window,pickup_type,drop_off_type",
		"trip_id,1,zone_a,08:00:00,17:00:00,2,1",
		"trip_id,2,zone_a,08:00:00,17:00:00,1,2",
	).build()

	static, err := ParseStatic(content, ParseStaticOptions{})
	if err != nil {
		t.Fatalf("ParseStatic() got error %v, want nil", err)
	}
	if len(static.Warnings) != 0 {
		t.Errorf("got warnings %+v, want none", static.Warnings)
	}
	if got := len(static.Trips[0].StopTimes); got != 2 {
		t.Fatalf("got %d stop times, want 2", got)
	}
	if static.Trips[0].StopTimes[0].Location != static.Trips[0].StopTimes[1].Location {
		t.Error("both records must point at the same *Location")
	}
}

func TestParseStatic_DraftSafeDurationOnStopTimes(t *testing.T) {
	content := newFlexZipBuilder().add(
		"stop_times.txt",
		"trip_id,stop_sequence,location_id,start_pickup_drop_off_window,end_pickup_drop_off_window,pickup_type,drop_off_type,mean_duration_factor,mean_duration_offset,safe_duration_factor,safe_duration_offset",
		"trip_id,1,zone_a,08:00:00,17:00:00,2,1,1,0.0,1,0.0",
	).build()

	static, err := ParseStatic(content, ParseStaticOptions{})
	if err != nil {
		t.Fatalf("ParseStatic() got error %v, want nil", err)
	}
	stopTime := static.Trips[0].StopTimes[0]
	if stopTime.SafeDurationFactor == nil || *stopTime.SafeDurationFactor != 1.0 {
		t.Errorf("SafeDurationFactor = %v, want 1.0", stopTime.SafeDurationFactor)
	}
	if stopTime.SafeDurationOffset == nil || *stopTime.SafeDurationOffset != 0.0 {
		t.Errorf("SafeDurationOffset = %v, want 0.0", stopTime.SafeDurationOffset)
	}
	if static.Trips[0].SafeDurationFactor != nil {
		t.Error("trips.txt has no safe_duration_factor column; ScheduledTrip.SafeDurationFactor must stay nil")
	}
}

// The four gtfs.org GTFS-Flex patterns, reduced to one trip each.
func TestParseStatic_FlexFeeds(t *testing.T) {
	t.Run("Heartland-style single zone", func(t *testing.T) {
		content := newFlexZipBuilder().add(
			"stop_times.txt",
			"trip_id,stop_sequence,location_id,start_pickup_drop_off_window,end_pickup_drop_off_window,pickup_type,drop_off_type,pickup_booking_rule_id,drop_off_booking_rule_id",
			"trip_id,1,zone_a,08:00:00,17:00:00,2,1,br_1,br_1",
			"trip_id,2,zone_a,08:00:00,17:00:00,1,2,br_1,br_1",
		).build()

		static := parseFlexFeed(t, content)
		want := []ScheduledStopTime{
			{
				Location: flexZoneA(), StopSequence: 1,
				StartPickupDropOffWindow: hhmm(8, 0), EndPickupDropOffWindow: hhmm(17, 0),
				PickupType: PickupDropOffPolicy_PhoneAgency, DropOffType: PickupDropOffPolicy_No,
				ContinuousPickup: PickupDropOffPolicy_No, ContinuousDropOff: PickupDropOffPolicy_No,
				PickupBookingRule: flexBookingRule1(), DropOffBookingRule: flexBookingRule1(),
			},
			{
				Location: flexZoneA(), StopSequence: 2,
				StartPickupDropOffWindow: hhmm(8, 0), EndPickupDropOffWindow: hhmm(17, 0),
				PickupType: PickupDropOffPolicy_No, DropOffType: PickupDropOffPolicy_PhoneAgency,
				ContinuousPickup: PickupDropOffPolicy_No, ContinuousDropOff: PickupDropOffPolicy_No,
				PickupBookingRule: flexBookingRule1(), DropOffBookingRule: flexBookingRule1(),
			},
		}
		if diff := cmp.Diff(static.Trips[0].StopTimes, want); diff != "" {
			t.Errorf("StopTimes mismatch (-got +want):\n%s", diff)
		}
		stopTimes := static.Trips[0].StopTimes
		if stopTimes[0].Location != stopTimes[1].Location || stopTimes[0].Location != &static.Locations[0] {
			t.Error("records must share the *Location stored in Static.Locations")
		}
		if stopTimes[0].PickupBookingRule != &static.BookingRules[0] {
			t.Error("records must point at the *BookingRule stored in Static.BookingRules")
		}
		for _, st := range stopTimes {
			if !st.IsWindowed() || !st.IsFlex() {
				t.Errorf("record %d: IsWindowed()=%v IsFlex()=%v, want true/true", st.StopSequence, st.IsWindowed(), st.IsFlex())
			}
		}
	})

	t.Run("zone to zone", func(t *testing.T) {
		content := newFlexZipBuilder().add(
			"stop_times.txt",
			"trip_id,stop_sequence,location_id,start_pickup_drop_off_window,end_pickup_drop_off_window,pickup_type,drop_off_type",
			"trip_id,1,zone_a,06:00:00,18:00:00,2,1",
			"trip_id,2,zone_b,06:00:00,18:00:00,1,2",
		).build()

		static := parseFlexFeed(t, content)
		stopTimes := static.Trips[0].StopTimes
		if len(stopTimes) != 2 {
			t.Fatalf("got %d stop times, want 2", len(stopTimes))
		}
		if diff := cmp.Diff(stopTimes[0].Location, flexZoneA()); diff != "" {
			t.Errorf("record 1 location (-got +want):\n%s", diff)
		}
		if diff := cmp.Diff(stopTimes[1].Location, flexZoneB()); diff != "" {
			t.Errorf("record 2 location (-got +want):\n%s", diff)
		}
		if len(stopTimes[1].Location.Geometry.Polygons) != 2 {
			t.Errorf("zone_b is a MultiPolygon with 2 polygons, got %d", len(stopTimes[1].Location.Geometry.Polygons))
		}
	})

	t.Run("RufBus-style location group without pickup_type columns", func(t *testing.T) {
		// gtfs.org's group example omits pickup_type/drop_off_type; the blank
		// default is 0 (regularly scheduled), which maglev's rule compilation
		// depends on.
		content := newFlexZipBuilder().add(
			"stops.txt", "stop_id,stop_lat,stop_lon\nstop_1,45.0,-85.0\nstop_2,45.1,-85.1\nstop_3,45.2,-85.2",
		).add(
			"location_group_stops.txt", "location_group_id,stop_id\ngroup_1,stop_1\ngroup_1,stop_2\ngroup_1,stop_3",
		).add(
			"stop_times.txt",
			"trip_id,stop_sequence,location_group_id,start_pickup_drop_off_window,end_pickup_drop_off_window",
			"trip_id,1,group_1,09:00:00,15:00:00",
			"trip_id,2,group_1,09:00:00,15:00:00",
		).build()

		static := parseFlexFeed(t, content)
		if len(static.LocationGroups) != 1 || len(static.LocationGroups[0].Stops) != 3 {
			t.Fatalf("got groups %+v, want one group of 3 stops", static.LocationGroups)
		}
		stopTimes := static.Trips[0].StopTimes
		if len(stopTimes) != 2 {
			t.Fatalf("got %d stop times, want 2", len(stopTimes))
		}
		for _, st := range stopTimes {
			if st.LocationGroup != &static.LocationGroups[0] {
				t.Errorf("record %d must point at Static.LocationGroups[0]", st.StopSequence)
			}
			if st.PickupType != PickupDropOffPolicy_Yes || st.DropOffType != PickupDropOffPolicy_Yes {
				t.Errorf("record %d pickup/drop-off = %v/%v, want ALLOWED/ALLOWED", st.StopSequence, st.PickupType, st.DropOffType)
			}
			if st.Stop != nil || st.Location != nil {
				t.Errorf("record %d must reference only the group", st.StopSequence)
			}
		}
	})

	t.Run("Hermann-style deviated route", func(t *testing.T) {
		// timed stop, zone, timed stop (untimed, to be interpolated), zone, timed stop.
		content := newFlexZipBuilder().add(
			"stop_times.txt",
			"trip_id,stop_sequence,stop_id,location_id,arrival_time,departure_time,start_pickup_drop_off_window,end_pickup_drop_off_window,pickup_type,drop_off_type,timepoint",
			"trip_id,1,stop_1,,08:00:00,08:00:00,,,0,0,1",
			"trip_id,2,,zone_a,,,08:00:00,08:40:00,1,3,",
			"trip_id,3,stop_2,,,,,,0,0,0",
			"trip_id,4,,zone_b,,,08:00:00,08:40:00,2,1,",
			"trip_id,5,stop_1,,08:40:00,08:40:00,,,0,0,1",
		).build()

		static := parseFlexFeed(t, content)
		stopTimes := static.Trips[0].StopTimes
		if len(stopTimes) != 5 {
			t.Fatalf("got %d stop times, want 5", len(stopTimes))
		}
		for i, want := range []int{1, 2, 3, 4, 5} {
			if stopTimes[i].StopSequence != want {
				t.Errorf("record %d has stop_sequence %d, want %d", i, stopTimes[i].StopSequence, want)
			}
		}
		// Windowed records untouched by interpolation.
		for _, i := range []int{1, 3} {
			st := stopTimes[i]
			if st.ArrivalTime != 0 || st.DepartureTime != 0 {
				t.Errorf("zone record %d got times %v/%v, want none", st.StopSequence, st.ArrivalTime, st.DepartureTime)
			}
			if !st.IsWindowed() || st.ExactTimes {
				t.Errorf("zone record %d: IsWindowed()=%v ExactTimes=%v, want true/false", st.StopSequence, st.IsWindowed(), st.ExactTimes)
			}
		}
		if stopTimes[1].PickupType != PickupDropOffPolicy_No || stopTimes[1].DropOffType != PickupDropOffPolicy_CoordinateWithDriver {
			t.Errorf("zone record 2 pickup/drop-off = %v/%v, want NOT_ALLOWED/COORDINATE_WITH_DRIVER", stopTimes[1].PickupType, stopTimes[1].DropOffType)
		}
		// The untimed timed stop is interpolated between its timed neighbours only.
		if !almostEq(stopTimes[2].ArrivalTime, dur("08:20:00")) || !almostEq(stopTimes[2].DepartureTime, dur("08:20:00")) {
			t.Errorf("stop record 3 = %v/%v, want 08:20:00 (midpoint of 08:00 and 08:40)", stopTimes[2].ArrivalTime, stopTimes[2].DepartureTime)
		}
		if stopTimes[2].ExactTimes {
			t.Error("stop record 3 has timepoint=0 and must not be exact")
		}
		if stopTimes[2].IsFlex() {
			t.Error("stop record 3 is a plain timed stop and must not be flex")
		}
	})
}

func parseFlexFeed(t *testing.T, content []byte) *Static {
	t.Helper()
	static, err := ParseStatic(content, ParseStaticOptions{})
	if err != nil {
		t.Fatalf("ParseStatic() got error %v, want nil", err)
	}
	if len(static.Warnings) != 0 {
		t.Fatalf("got warnings %+v, want none", static.Warnings)
	}
	if len(static.Trips) != 1 {
		t.Fatalf("got %d trips, want 1", len(static.Trips))
	}
	return static
}

// Reduced from the real Alexandria (Trillium) feed: draft-era columns on
// stop_times.txt, header-only location group files, a type 2 booking rule
// with prior_notice_start_*.
func TestParseStatic_AlexandriaExcerpt(t *testing.T) {
	content := newZipBuilder().add(
		"agency.txt", "agency_id,agency_name,agency_url,agency_timezone\n5088,DOT,https://www.alexandriava.gov,America/Los_Angeles",
	).add(
		"routes.txt", "route_id,agency_id,route_short_name,route_long_name,route_type\n77652,5088,,DOT Paratransit,3",
	).add(
		"stops.txt",
		"stop_id,stop_code,platform_code,stop_name,stop_desc,stop_lat,stop_lon,zone_id,stop_url,location_type,parent_station,stop_timezone,position,direction,wheelchair_boarding,tts_stop_name",
		`4258639,,,"Alexandria, VA, USA",,38.836368,-77.049221,,,0,,America/New_York,,,0,`,
	).add(
		"calendar.txt",
		"service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date",
		"c_71675_b_85952_d_63,1,1,1,1,1,1,0,20260101,20261231",
		"c_71675_b_85952_d_64,0,0,0,0,0,0,1,20260101,20261231",
	).add(
		"trips.txt",
		"route_id,service_id,trip_id,trip_short_name,trip_headsign,direction_id,block_id,shape_id,bikes_allowed,wheelchair_accessible,trip_type,continuous_pickup_message,continuous_drop_off_message,tts_trip_headsign,tts_trip_short_name",
		"77652,c_71675_b_85952_d_63,t_6124961_b_85952_tn_0,,,0,,,,,,,,,",
		"77652,c_71675_b_85952_d_64,t_6124409_b_85952_tn_0,,,0,,,,,,,,,",
	).add(
		"stop_times.txt",
		"trip_id,arrival_time,departure_time,stop_id,location_id,location_group_id,stop_sequence,stop_headsign,pickup_type,drop_off_type,shape_dist_traveled,timepoint,continuous_pickup,continuous_drop_off,pickup_booking_rule_id,drop_off_booking_rule_id,start_pickup_drop_off_window,end_pickup_drop_off_window,mean_duration_factor,mean_duration_offset,safe_duration_factor,safe_duration_offset,tts_stop_headsign",
		"t_6124409_b_85952_tn_0,,,,area_1449,,1,,2,1,,0,1,1,booking_route_77652,booking_route_77652,07:00:00,24:50:00,1,0.0,1,0.0,",
		"t_6124409_b_85952_tn_0,,,,area_1449,,2,,1,2,,0,1,1,booking_route_77652,booking_route_77652,07:00:00,25:00:00,1,0.0,1,0.0,",
		"t_6124961_b_85952_tn_0,,,,area_1449,,1,,2,1,,0,1,1,booking_route_77652,booking_route_77652,05:00:00,24:50:00,1,0.0,1,0.0,",
		"t_6124961_b_85952_tn_0,,,,area_1449,,2,,1,2,,0,1,1,booking_route_77652,booking_route_77652,05:00:00,25:00:00,1,0.0,1,0.0,",
	).add(
		"booking_rules.txt",
		"booking_rule_id,booking_type,prior_notice_duration_min,prior_notice_duration_max,prior_notice_start_day,prior_notice_start_time,prior_notice_last_day,prior_notice_last_time,prior_notice_service_id,message,pickup_message,drop_off_message,phone_number,info_url,booking_url",
		`booking_route_77652,2,,,14,00:00:00,1,17:00:00,,"DOT is the City of Alexandria's paratransit program, call 703.746.5222",,,703-746-5222,https://www.alexandriava.gov/Paratransit,https://example.com/book`,
	).add(
		"location_groups.txt", "location_group_id,location_group_name",
	).add(
		"location_group_stops.txt", "location_group_id,stop_id",
	).add(
		"locations.geojson",
		`{"type":"FeatureCollection","features":[{"id":"area_1449","type":"Feature","geometry":{"type":"Polygon","coordinates":[[[-77.0464775,38.8762916],[-77.0449561,38.8765842],[-77.0403763,38.8723894],[-77.0464775,38.8762916]]]},"properties":{}}]}`,
	).build()

	static, err := ParseStatic(content, ParseStaticOptions{})
	if err != nil {
		t.Fatalf("ParseStatic() got error %v, want nil", err)
	}
	if len(static.Warnings) != 0 {
		t.Fatalf("got warnings %+v, want none", static.Warnings)
	}
	if static.LocationGroups != nil {
		t.Errorf("header-only location_groups.txt must yield nil, got %+v", static.LocationGroups)
	}
	if len(static.Locations) != 1 || static.Locations[0].Id != "area_1449" {
		t.Fatalf("got locations %+v, want area_1449 only", static.Locations)
	}
	if len(static.Stops) != 1 || static.Stops[0].Id != "4258639" {
		t.Fatalf("got stops %+v, want 4258639 only", static.Stops)
	}

	wantRule := BookingRule{
		Id:                   "booking_route_77652",
		Type:                 BookingType_PriorDays,
		PriorNoticeStartDay:  ptr(int32(14)),
		PriorNoticeStartTime: ptr(time.Duration(0)),
		PriorNoticeLastDay:   ptr(int32(1)),
		PriorNoticeLastTime:  ptr(17 * time.Hour),
		Message:              "DOT is the City of Alexandria's paratransit program, call 703.746.5222",
		PhoneNumber:          "703-746-5222",
		InfoUrl:              "https://www.alexandriava.gov/Paratransit",
		BookingUrl:           "https://example.com/book",
	}
	if diff := cmp.Diff(static.BookingRules, []BookingRule{wantRule}); diff != "" {
		t.Errorf("BookingRules mismatch (-got +want):\n%s", diff)
	}

	if len(static.Trips) != 2 {
		t.Fatalf("got %d trips, want 2", len(static.Trips))
	}
	for _, trip := range static.Trips {
		if trip.SafeDurationFactor != nil || trip.SafeDurationOffset != nil {
			t.Errorf("trip %s: trips.txt carries no safe_duration_* columns, want nil", trip.ID)
		}
		if len(trip.StopTimes) != 2 {
			t.Fatalf("trip %s: got %d stop times, want 2", trip.ID, len(trip.StopTimes))
		}
		for _, st := range trip.StopTimes {
			if st.Location != &static.Locations[0] {
				t.Errorf("trip %s seq %d: Location must be area_1449", trip.ID, st.StopSequence)
			}
			if st.PickupBookingRule != &static.BookingRules[0] || st.DropOffBookingRule != &static.BookingRules[0] {
				t.Errorf("trip %s seq %d: booking rules must resolve to booking_route_77652", trip.ID, st.StopSequence)
			}
			if st.SafeDurationFactor == nil || *st.SafeDurationFactor != 1.0 || st.SafeDurationOffset == nil || *st.SafeDurationOffset != 0.0 {
				t.Errorf("trip %s seq %d: draft-era safe duration = %v/%v, want 1.0/0.0", trip.ID, st.StopSequence, st.SafeDurationFactor, st.SafeDurationOffset)
			}
			if st.ExactTimes || !st.IsWindowed() {
				t.Errorf("trip %s seq %d: ExactTimes=%v IsWindowed()=%v, want false/true", trip.ID, st.StopSequence, st.ExactTimes, st.IsWindowed())
			}
		}
	}
	// trips.txt file order: t_6124961 (Mon–Sat, 05:00–24:50 then 05:00–25:00)
	// first, then t_6124409 (Sun, 07:00–24:50 then 07:00–25:00).
	weekdays, sunday := static.Trips[0], static.Trips[1]
	if diff := cmp.Diff(weekdays.StopTimes[0].StartPickupDropOffWindow, hhmm(5, 0)); diff != "" {
		t.Errorf("weekday start window (-got +want):\n%s", diff)
	}
	if diff := cmp.Diff(weekdays.StopTimes[0].EndPickupDropOffWindow, hhmm(24, 50)); diff != "" {
		t.Errorf("weekday end window (-got +want):\n%s", diff)
	}
	if diff := cmp.Diff(weekdays.StopTimes[1].EndPickupDropOffWindow, hhmm(25, 0)); diff != "" {
		t.Errorf("weekday drop-off end window (-got +want):\n%s", diff)
	}
	if diff := cmp.Diff(sunday.StopTimes[0].StartPickupDropOffWindow, hhmm(7, 0)); diff != "" {
		t.Errorf("sunday start window (-got +want):\n%s", diff)
	}
}
