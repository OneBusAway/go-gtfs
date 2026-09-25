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
