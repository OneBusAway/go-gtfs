package gtfs

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files under testdata/")

// syntheticFeed builds a deterministic GTFS static zip with nTrips trips of
// stopsPerTrip stop times each. Besides the bulk rows, it exercises the stop
// time edge cases the parser has to handle: non-contiguous trips, rows out of
// stop_sequence order, missing times that need interpolating, unknown stops
// and trips, invalid stop sequences, and non-empty stop headsigns.
func syntheticFeed(nTrips, stopsPerTrip int) []byte {
	const nRoutes = 5
	nStops := stopsPerTrip * 2

	var stops strings.Builder
	stops.WriteString("stop_id,stop_name,stop_lat,stop_lon,location_type,parent_station\n")
	for s := 0; s < nStops; s += 10 {
		fmt.Fprintf(&stops, "station_%d,Station %d,47.%04d,-122.%04d,1,\n", s, s, s, s)
	}
	for s := 0; s < nStops; s++ {
		parent := ""
		if s%10 == 0 {
			parent = fmt.Sprintf("station_%d", s)
		}
		fmt.Fprintf(&stops, "stop_%d,Stop %d,47.%04d,-122.%04d,,%s\n", s, s, s, s, parent)
	}

	var routes strings.Builder
	routes.WriteString("route_id,agency_id,route_short_name,route_type\n")
	for r := 0; r < nRoutes; r++ {
		fmt.Fprintf(&routes, "route_%d,agency,%d,3\n", r, r)
	}

	var shapes strings.Builder
	shapes.WriteString("shape_id,shape_pt_lat,shape_pt_lon,shape_pt_sequence,shape_dist_traveled\n")
	for r := 0; r < nRoutes; r++ {
		for p := 0; p < 20; p++ {
			fmt.Fprintf(&shapes, "shape_%d,47.%04d,-122.%04d,%d,%d.5\n", r, p, r, p, p*100)
		}
	}

	var trips strings.Builder
	trips.WriteString("route_id,service_id,trip_id,trip_headsign,direction_id,block_id,shape_id\n")
	for t := 0; t < nTrips; t++ {
		fmt.Fprintf(&trips, "route_%d,service_%d,trip_%d,Headsign %d,%d,block_%d,shape_%d\n",
			t%nRoutes, t%2, t, t%nRoutes, t%2, t/4, t%nRoutes)
	}

	stopTimeRow := func(t, i int) string {
		arrival := fmt.Sprintf("%02d:%02d:00", 6+(t+i)/60%18, (t+i)%60)
		departure := arrival
		timepoint := "1"
		if i%4 == 2 && i != stopsPerTrip-1 {
			arrival, departure, timepoint = "", "", "0"
		}
		headsign := ""
		if i%7 == 3 {
			headsign = fmt.Sprintf("To stop %d", stopsPerTrip-1)
		}
		return fmt.Sprintf("trip_%d,stop_%d,%s,%s,%s,%d,%s,%d,%d,%d.25\n",
			t, (t+i)%nStops, arrival, departure, timepoint, i+1, headsign, i%2, i%3%2, i*150)
	}
	var stopTimes strings.Builder
	stopTimes.WriteString("trip_id,stop_id,arrival_time,departure_time,timepoint,stop_sequence,stop_headsign,pickup_type,drop_off_type,shape_dist_traveled\n")
	stopTimes.WriteString("no_such_trip,stop_0,08:00:00,08:00:00,1,1,,0,0,0\n")
	var deferred []string
	for t := 0; t < nTrips; t++ {
		var rows []string
		for i := 0; i < stopsPerTrip; i++ {
			rows = append(rows, stopTimeRow(t, i))
		}
		switch t {
		case 1:
			// Second half of trip 1 appears after trip 2.
			deferred = rows[stopsPerTrip/2:]
			rows = rows[:stopsPerTrip/2]
		case 2:
			rows = append(rows, deferred...)
		case 3:
			// Trip 3 is written in reverse stop_sequence order.
			for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
		for _, row := range rows {
			stopTimes.WriteString(row)
		}
	}
	stopTimes.WriteString("trip_0,no_such_stop,08:00:00,08:00:00,1,999,,0,0,0\n")
	stopTimes.WriteString("trip_0,stop_0,08:00:00,08:00:00,1,not_a_number,,0,0,0\n")

	return newZipBuilder().add(
		"agency.txt",
		"agency_id,agency_name,agency_url,agency_timezone\nagency,Agency,https://example.com,America/Los_Angeles",
	).add(
		"routes.txt", routes.String(),
	).add(
		"stops.txt", stops.String(),
	).add(
		"calendar.txt",
		"service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date\n"+
			"service_0,1,1,1,1,1,0,0,20220504,20220507\n"+
			"service_1,0,0,0,0,0,1,1,20220504,20220507",
	).add(
		"calendar_dates.txt", "service_id,date,exception_type\nservice_0,20220505,2",
	).add(
		"shapes.txt", shapes.String(),
	).add(
		"trips.txt", trips.String(),
	).add(
		"frequencies.txt", "trip_id,start_time,end_time,headway_secs\ntrip_0,06:00:00,07:00:00,600",
	).add(
		"stop_times.txt", stopTimes.String(),
	).build()
}

// dumpStatic renders the parse result as deterministic text, following
// pointers by ID, so two parses can be compared field by field.
func dumpStatic(s *Static) string {
	var b strings.Builder
	w := func(format string, a ...interface{}) { fmt.Fprintf(&b, format+"\n", a...) }
	f := func(p *float64) string {
		if p == nil {
			return "nil"
		}
		return fmt.Sprint(*p)
	}
	for _, a := range s.Agencies {
		w("agency %+v", a)
	}
	for _, r := range s.Routes {
		w("route %s agency=%s %s %s %s %s %s %v %s %v %v %v", r.Id, r.Agency.Id, r.Color, r.TextColor, r.ShortName,
			r.LongName, r.Description, r.Type, r.Url, r.SortOrder != nil, r.ContinuousPickup, r.ContinuousDropOff)
	}
	for _, stop := range s.Stops {
		parent := ""
		if stop.Parent != nil {
			parent = stop.Parent.Id
		}
		w("stop %s %q %s %s %v parent=%s %v", stop.Id, stop.Name, f(stop.Latitude), f(stop.Longitude), stop.Type, parent, stop.WheelchairBoarding)
	}
	services := append([]Service(nil), s.Services...)
	sort.Slice(services, func(i, j int) bool { return services[i].Id < services[j].Id })
	for _, service := range services {
		w("service %+v", service)
	}
	for _, shape := range s.Shapes {
		w("shape %s", shape.ID)
		for _, p := range shape.Points {
			w("  point %v %v %s", p.Latitude, p.Longitude, f(p.Distance))
		}
	}
	for _, trip := range s.Trips {
		shape := ""
		if trip.Shape != nil {
			shape = trip.Shape.ID
		}
		w("trip %s route=%s service=%s %q %q %v %s %v %v shape=%s freq=%+v", trip.ID, trip.Route.Id, trip.Service.Id, trip.Headsign,
			trip.ShortName, trip.DirectionId, trip.BlockID, trip.WheelchairAccessible, trip.BikesAllowed, shape, trip.Frequencies)
		for _, st := range trip.StopTimes {
			w("  stop_time trip=%v stop=%s arr=%v dep=%v seq=%d %q %v %v %v %v %s %v", st.Trip != nil, st.Stop.Id,
				st.ArrivalTime, st.DepartureTime, st.StopSequence, st.Headsign, st.PickupType, st.DropOffType,
				st.ContinuousPickup, st.ContinuousDropOff, f(st.ShapeDistanceTraveled), st.ExactTimes)
		}
	}
	for _, t := range s.Transfers {
		w("transfer %s %s %v", t.From.Id, t.To.Id, t.Type)
	}
	w("warnings %d", len(s.Warnings))
	return b.String()
}

// TestParseStatic_SyntheticGolden pins the full parse result of the synthetic
// feed, so internal memory optimizations can be checked to leave the output
// unchanged. Regenerate with `go test -run SyntheticGolden -update`.
func TestParseStatic_SyntheticGolden(t *testing.T) {
	const goldenFile = "testdata/static_synthetic_golden.txt"
	result, err := ParseStatic(syntheticFeed(8, 9), ParseStaticOptions{})
	if err != nil {
		t.Fatalf("ParseStatic: %s", err)
	}
	got := dumpStatic(result)
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenFile, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenFile)
	if err != nil {
		t.Fatalf("reading golden file (run with -update to create it): %s", err)
	}
	if got != string(want) {
		t.Errorf("parse result differs from %s; diff (-want +got):\n%s", goldenFile, lineDiff(string(want), got))
	}
}

func lineDiff(want, got string) string {
	wantLines, gotLines := strings.Split(want, "\n"), strings.Split(got, "\n")
	var b strings.Builder
	for i := 0; i < len(wantLines) || i < len(gotLines); i++ {
		var w, g string
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w != g {
			fmt.Fprintf(&b, "line %d:\n- %s\n+ %s\n", i+1, w, g)
		}
	}
	return b.String()
}

type parseMemory struct {
	// retained is the heap still reachable from the result after a GC.
	retained uint64
	// allocated is the total bytes allocated during the parse. Unlike a
	// sampled peak it is deterministic, and it bounds the garbage the parse
	// hands the GC on top of what it retains.
	allocated uint64
}

func measureParseStatic(t *testing.T, feed []byte, opts ParseStaticOptions) (*Static, parseMemory) {
	t.Helper()
	defer debug.SetGCPercent(debug.SetGCPercent(10))
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	result, err := ParseStatic(feed, opts)
	if err != nil {
		t.Fatalf("ParseStatic: %s", err)
	}
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	runtime.GC()
	runtime.ReadMemStats(&after)
	var retained uint64
	if after.HeapAlloc > before.HeapAlloc {
		retained = after.HeapAlloc - before.HeapAlloc
	}
	runtime.KeepAlive(result)
	return result, parseMemory{retained: retained, allocated: allocated}
}

func countStopTimes(s *Static) int {
	n := 0
	for i := range s.Trips {
		n += len(s.Trips[i].StopTimes)
	}
	return n
}

// TestParseStatic_StopTimesMemory bounds the heap ParseStatic needs per stop
// time. Stop times dominate large feeds (2.2M rows in Seattle's), so these
// per-row costs decide whether a parse fits in memory.
func TestParseStatic_StopTimesMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory measurement in short mode")
	}
	const nTrips, stopsPerTrip = 2000, 50
	feed := syntheticFeed(nTrips, stopsPerTrip)
	result, mem := measureParseStatic(t, feed, ParseStaticOptions{})
	n := countStopTimes(result)
	if n < nTrips*stopsPerTrip-10 {
		t.Fatalf("parsed %d stop times, want about %d", n, nTrips*stopsPerTrip)
	}
	retainedPerRow := float64(mem.retained) / float64(n)
	allocatedPerRow := float64(mem.allocated) / float64(n)
	t.Logf("retained %.0f B/stop time, allocated %.0f B/stop time", retainedPerRow, allocatedPerRow)

	// A ScheduledStopTime is 88 bytes plus an 8-byte shape distance. Anything
	// much above that means the result pins CSV row buffers or slack capacity.
	if retainedPerRow > 115 {
		t.Errorf("retained %.0f bytes per stop time, want <= 115", retainedPerRow)
	}
	// One CSV row string and the stop time itself are unavoidable; repeated
	// copies of the stop time slice are not.
	if allocatedPerRow > 200 {
		t.Errorf("allocated %.0f bytes per stop time, want <= 200", allocatedPerRow)
	}
}
