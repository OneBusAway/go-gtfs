package gtfs

import (
	"bytes"
	"errors"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// streamedStatic parses feed with ParseStaticStream and fills in the stop
// times and shapes, so the result can be compared with ParseStatic's.
func streamedStatic(t *testing.T, feed []byte, opts ParseStaticOptions) *Static {
	t.Helper()
	stream, err := ParseStaticStream(bytes.NewReader(feed), int64(len(feed)), opts)
	if err != nil {
		t.Fatalf("ParseStaticStream: %s", err)
	}
	s := stream.Static
	yielded := map[int]bool{}
	err = stream.EachTripStopTimes(func(i int, stopTimes []ScheduledStopTime) error {
		if yielded[i] {
			t.Errorf("trip %d yielded twice", i)
		}
		yielded[i] = true
		if len(stopTimes) != stream.StopTimeCount(i) || cap(stopTimes) != len(stopTimes) {
			t.Errorf("trip %d: len %d cap %d, StopTimeCount says %d", i, len(stopTimes), cap(stopTimes), stream.StopTimeCount(i))
		}
		s.Trips[i].StopTimes = stopTimes
		return nil
	})
	if err != nil {
		t.Fatalf("EachTripStopTimes: %s", err)
	}
	err = stream.EachShape(func(shape Shape) error {
		s.Shapes = append(s.Shapes, shape)
		return nil
	})
	if err != nil {
		t.Fatalf("EachShape: %s", err)
	}
	sort.Slice(s.Shapes, func(i, j int) bool { return s.Shapes[i].ID < s.Shapes[j].ID })
	return s
}

func TestParseStaticStream_MatchesParseStatic(t *testing.T) {
	noShapeDistances := newZipBuilderWithDefaults().add(
		"stops.txt", "stop_id\ns1\ns2\ns3",
	).add(
		"trips.txt", "route_id,service_id,trip_id\nroute_id,service_id,t1\nroute_id,service_id,t2\nroute_id,service_id,t3",
	).add(
		// t1 and t2 interleave, t1 misses a time to interpolate, one row names
		// an unknown stop, one an unknown trip, one has no stop_sequence, and
		// t3 has no valid rows at all.
		"stop_times.txt", "trip_id,stop_id,arrival_time,departure_time,stop_sequence\n"+
			"t1,s1,08:00:00,08:00:00,1\n"+
			"t2,s3,09:00:00,09:00:00,2\n"+
			"t1,s2,,,2\n"+
			"t2,s1,08:50:00,08:50:00,1\n"+
			"t1,s3,08:20:00,08:20:00,3\n"+
			"t1,nowhere,08:30:00,08:30:00,4\n"+
			"t9,s1,08:30:00,08:30:00,1\n"+
			"t3,nowhere,07:00:00,07:00:00,1\n"+
			"t2,s2,09:10:00,09:10:00,\n",
	).build()

	// Two trips share an ID (the last one wins), one names an unknown route
	// and is dropped though its stop times remain, and shape points
	// interleave.
	awkward := newZipBuilderWithDefaults().add(
		"stops.txt", "stop_id\ns1\ns2",
	).add(
		"trips.txt", "route_id,service_id,trip_id,shape_id\n"+
			"route_id,service_id,dup,sh1\n"+
			"route_id,service_id,dup,sh2\n"+
			"no_route,service_id,orphan,sh1\n"+
			"route_id,service_id,t1,missing_shape",
	).add(
		"shapes.txt", "shape_id,shape_pt_lat,shape_pt_lon,shape_pt_sequence\n"+
			"sh1,1,1,2\nsh2,2,2,1\nsh1,1,0,1\nsh2,2,3,2\nsh1,1,2,3",
	).add(
		"stop_times.txt", "trip_id,stop_id,arrival_time,departure_time,stop_sequence\n"+
			"dup,s1,08:00:00,08:00:00,1\n"+
			"orphan,s1,08:00:00,08:00:00,1\n"+
			"t1,s2,09:30:00,09:30:00,2\n"+
			"dup,s2,08:10:00,08:10:00,2\n"+
			"t1,s1,09:00:00,09:00:00,1",
	).build()
	withoutShapesFile := newZipBuilderWithDefaults().build()

	tests := []struct {
		name string
		feed []byte
		opts ParseStaticOptions
	}{
		{"duplicate trips, dropped trips, interleaved shapes", awkward, ParseStaticOptions{}},
		{"no shapes.txt", withoutShapesFile, ParseStaticOptions{}},
		{"synthetic", syntheticFeed(8, 9), ParseStaticOptions{}},
		{"synthetic without shapes", syntheticFeed(8, 9), ParseStaticOptions{SkipShapes: true}},
		{"no shape distances", noShapeDistances, ParseStaticOptions{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want, err := ParseStatic(tt.feed, tt.opts)
			if err != nil {
				t.Fatalf("ParseStatic: %s", err)
			}
			got := streamedStatic(t, tt.feed, tt.opts)
			if diff := cmp.Diff(dumpStatic(want), dumpStatic(got)); diff != "" {
				t.Errorf("streamed feed differs from ParseStatic (-want +got):\n%s", diff)
			}
		})
	}
}

// TestParseStaticStream_MatchesParseStaticForFlex checks that GTFS-Flex stop
// times stream as ParseStatic parses them, and that the rows dropped for
// breaking a GTFS-Flex rule raise the same warnings, once.
func TestParseStaticStream_MatchesParseStaticForFlex(t *testing.T) {
	// A deviated route: timed stops around a zone and a location group, an
	// untimed stop to interpolate, an unknown booking rule (kept, with a
	// warning), a zone row with no window and a row naming an unknown zone
	// (both dropped, with warnings).
	feed := newFlexZipBuilder().add(
		"stop_times.txt",
		"trip_id,stop_sequence,stop_id,location_id,location_group_id,arrival_time,departure_time,"+
			"start_pickup_drop_off_window,end_pickup_drop_off_window,pickup_booking_rule_id",
		"trip_id,1,stop_1,,,08:00:00,08:00:00,,,br_1",
		"trip_id,2,,zone_a,,,,08:00:00,08:40:00,br_1",
		"trip_id,3,stop_2,,,,,,,",
		"trip_id,4,,,group_1,,,08:00:00,08:40:00,no_such_rule",
		"trip_id,5,stop_1,,,08:40:00,08:40:00,,,",
		"trip_id,6,,zone_b,,,,,,",
		"trip_id,7,,nowhere,,,,08:00:00,08:40:00,",
	).build()

	want, err := ParseStatic(feed, ParseStaticOptions{})
	if err != nil {
		t.Fatalf("ParseStatic: %s", err)
	}
	if n := len(want.Trips[0].StopTimes); n != 5 {
		t.Fatalf("ParseStatic kept %d stop times, want 5", n)
	}
	if n := len(want.Warnings); n != 3 {
		t.Fatalf("ParseStatic raised %d warnings, want 3: %v", n, want.Warnings)
	}
	got := streamedStatic(t, feed, ParseStaticOptions{})
	if diff := cmp.Diff(want.Trips, got.Trips); diff != "" {
		t.Errorf("streamed trips differ from ParseStatic (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(want.Warnings, got.Warnings); diff != "" {
		t.Errorf("streamed warnings differ from ParseStatic (-want +got):\n%s", diff)
	}
}

func TestParseStaticStream_TripShapesCarryOnlyTheirID(t *testing.T) {
	feed := syntheticFeed(2, 3)
	stream, err := ParseStaticStream(bytes.NewReader(feed), int64(len(feed)), ParseStaticOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(stream.Static.Shapes) != 0 {
		t.Errorf("Static.Shapes has %d shapes, want none", len(stream.Static.Shapes))
	}
	for _, trip := range stream.Static.Trips {
		if trip.Shape == nil || trip.Shape.ID == "" || trip.Shape.Points != nil {
			t.Errorf("trip %s: want a Shape with an ID and no points, got %+v", trip.ID, trip.Shape)
		}
	}
}

func TestParseStaticStream_StopsAtCallbackError(t *testing.T) {
	feed := syntheticFeed(4, 3)
	stream, err := ParseStaticStream(bytes.NewReader(feed), int64(len(feed)), ParseStaticOptions{})
	if err != nil {
		t.Fatal(err)
	}
	stop := errors.New("stop")

	calls := 0
	err = stream.EachTripStopTimes(func(int, []ScheduledStopTime) error {
		calls++
		return stop
	})
	if !errors.Is(err, stop) || calls != 1 {
		t.Errorf("EachTripStopTimes: got err %v after %d calls, want %v after 1", err, calls, stop)
	}

	calls = 0
	err = stream.EachShape(func(Shape) error {
		calls++
		return stop
	})
	if !errors.Is(err, stop) || calls != 1 {
		t.Errorf("EachShape: got err %v after %d calls, want %v after 1", err, calls, stop)
	}
}

func TestParseStaticStream_RequiresFullStopTimes(t *testing.T) {
	feed := syntheticFeed(1, 2)
	_, err := ParseStaticStream(bytes.NewReader(feed), int64(len(feed)), ParseStaticOptions{StopTimes: StopTimesNone})
	if err == nil {
		t.Error("want an error for StopTimesNone")
	}
}

// TestParseStaticStream_SurvivesReplacedTrips replaces Static.Trips with a
// filtered copy, as a caller validating the feed might, and checks that the
// stream still yields the parsed trips' stop times.
func TestParseStaticStream_SurvivesReplacedTrips(t *testing.T) {
	feed := syntheticFeed(4, 3)
	stream, err := ParseStaticStream(bytes.NewReader(feed), int64(len(feed)), ParseStaticOptions{})
	if err != nil {
		t.Fatal(err)
	}
	stream.Static.Trips = append([]ScheduledTrip(nil), stream.Static.Trips[1:]...)

	want, err := ParseStatic(feed, ParseStaticOptions{})
	if err != nil {
		t.Fatal(err)
	}
	wantStopTimes := map[string]int{}
	for _, trip := range want.Trips {
		wantStopTimes[trip.ID] = len(trip.StopTimes)
	}
	err = stream.EachTripStopTimes(func(i int, stopTimes []ScheduledStopTime) error {
		trip := stream.Trip(i)
		if len(stopTimes) != wantStopTimes[trip.ID] {
			t.Errorf("trip %s: %d stop times, want %d", trip.ID, len(stopTimes), wantStopTimes[trip.ID])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
