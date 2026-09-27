package gtfs

import (
	"math"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// Simple helper: parse "08:00:00" to time.Duration
func dur(s string) time.Duration {
	t, _ := time.Parse("15:04:05", s)
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute + time.Duration(t.Second())*time.Second
}
func almostEq(a, b time.Duration) bool {
	return math.Abs(float64(a-b)) < float64(time.Second)
}

// --- Tests for interpolateStopTimes (equal, no distance) ---

func TestInterpolateStopTimes_Normal(t *testing.T) {
	st := []ScheduledStopTime{
		{StopSequence: 1, ArrivalTime: dur("08:00:00"), DepartureTime: dur("08:05:00"), ExactTimes: true},
		{StopSequence: 2, ArrivalTime: 0, DepartureTime: 0},
		{StopSequence: 3, ArrivalTime: 0, DepartureTime: 0},
		{StopSequence: 4, ArrivalTime: dur("08:30:00"), DepartureTime: dur("08:35:00"), ExactTimes: true},
	}
	wantArr := []time.Duration{dur("08:00:00"), dur("08:10:00"), dur("08:20:00"), dur("08:30:00")}
	wantDep := []time.Duration{dur("08:05:00"), dur("08:15:00"), dur("08:25:00"), dur("08:35:00")}
	wantTimePoint := []bool{true, false, false, true}

	got := interpolateStopTimes(st)
	for i := range got {
		if !almostEq(got[i].ArrivalTime, wantArr[i]) {
			t.Errorf("normal: arrival %d: want %v got %v", i, wantArr[i], got[i].ArrivalTime)
		}
		if !almostEq(got[i].DepartureTime, wantDep[i]) {
			t.Errorf("normal: depart %d: want %v got %v", i, wantDep[i], got[i].DepartureTime)
		}
		if got[i].ExactTimes != wantTimePoint[i] {
			t.Errorf("timepoint %d: want %v got %v", i, wantTimePoint[i], got[i].ExactTimes)
		}
	}
}

func TestInterpolateStopTimes_MissingFirst(t *testing.T) {
	st := []ScheduledStopTime{
		{StopSequence: 1, ArrivalTime: 0, DepartureTime: 0},
		{StopSequence: 2, ArrivalTime: 0, DepartureTime: 0},
		{StopSequence: 3, ArrivalTime: dur("08:30:00"), DepartureTime: dur("08:35:00")},
	}
	got := interpolateStopTimes(st)
	// The first two should remain zero, last should be as input
	if got[0].ArrivalTime != 0 || got[1].ArrivalTime != 0 {
		t.Errorf("first missing: arrivals should remain zero, got: %v %v", got[0].ArrivalTime, got[1].ArrivalTime)
	}
	if got[0].DepartureTime != 0 || got[1].DepartureTime != 0 {
		t.Errorf("first missing: departures should remain zero, got: %v %v", got[0].DepartureTime, got[1].DepartureTime)
	}
	if !almostEq(got[2].ArrivalTime, dur("08:30:00")) {
		t.Errorf("first missing: last arrival wrong, got %v", got[2].ArrivalTime)
	}
	if !almostEq(got[2].DepartureTime, dur("08:35:00")) {
		t.Errorf("first missing: last depart wrong, got %v", got[2].DepartureTime)
	}
}

func TestInterpolateStopTimes_MissingLast(t *testing.T) {
	st := []ScheduledStopTime{
		{StopSequence: 1, ArrivalTime: dur("08:00:00"), DepartureTime: dur("08:05:00")},
		{StopSequence: 2, ArrivalTime: 0, DepartureTime: 0},
		{StopSequence: 3, ArrivalTime: 0, DepartureTime: 0},
	}
	got := interpolateStopTimes(st)
	// The first should be as input, the last two should remain zero
	if !almostEq(got[0].ArrivalTime, dur("08:00:00")) {
		t.Errorf("last missing: first arrival wrong, got %v", got[0].ArrivalTime)
	}
	if !almostEq(got[0].DepartureTime, dur("08:05:00")) {
		t.Errorf("last missing: first depart wrong, got %v", got[0].DepartureTime)
	}
	if got[1].ArrivalTime != 0 || got[2].ArrivalTime != 0 {
		t.Errorf("last missing: arrivals should remain zero, got: %v %v", got[1].ArrivalTime, got[2].ArrivalTime)
	}
	if got[1].DepartureTime != 0 || got[2].DepartureTime != 0 {
		t.Errorf("last missing: departures should remain zero, got: %v %v", got[1].DepartureTime, got[2].DepartureTime)
	}
}

// --- Tests for interpolateStopTimesByShapeDist (distance-weighted) ---

func TestInterpolateStopTimesByShapeDist_Normal(t *testing.T) {
	st := []ScheduledStopTime{
		{StopSequence: 1, ArrivalTime: dur("08:00:00"), DepartureTime: dur("08:05:00"), ShapeDistanceTraveled: ptr(0.0), ExactTimes: true},
		{StopSequence: 2, ArrivalTime: 0, DepartureTime: 0, ShapeDistanceTraveled: ptr(3.5)},
		{StopSequence: 3, ArrivalTime: 0, DepartureTime: 0, ShapeDistanceTraveled: ptr(7.0)},
		{StopSequence: 4, ArrivalTime: dur("08:30:00"), DepartureTime: dur("08:35:00"), ShapeDistanceTraveled: ptr(10.5), ExactTimes: true},
	}
	wantArr := []time.Duration{dur("08:00:00"), dur("08:10:00"), dur("08:20:00"), dur("08:30:00")}
	wantDep := []time.Duration{dur("08:05:00"), dur("08:15:00"), dur("08:25:00"), dur("08:35:00")}
	wantTimePoint := []bool{true, false, false, true}

	got := interpolateStopTimesByShapeDist(st)
	for i := range got {
		if !almostEq(got[i].ArrivalTime, wantArr[i]) {
			t.Errorf("shape normal: arrival %d: want %v got %v", i, wantArr[i], got[i].ArrivalTime)
		}
		if !almostEq(got[i].DepartureTime, wantDep[i]) {
			t.Errorf("shape normal: depart %d: want %v got %v", i, wantDep[i], got[i].DepartureTime)
		}
		if got[i].ExactTimes != wantTimePoint[i] {
			t.Errorf("timepoint %d: want %v got %v", i, wantTimePoint[i], got[i].ExactTimes)
		}
	}
}

func TestInterpolateStopTimesByShapeDist_MissingFirst(t *testing.T) {
	st := []ScheduledStopTime{
		{StopSequence: 1, ArrivalTime: 0, DepartureTime: 0, ShapeDistanceTraveled: ptr(0.0)},
		{StopSequence: 2, ArrivalTime: 0, DepartureTime: 0, ShapeDistanceTraveled: ptr(3.5)},
		{StopSequence: 3, ArrivalTime: dur("08:30:00"), DepartureTime: dur("08:35:00"), ShapeDistanceTraveled: ptr(10.5)},
	}
	got := interpolateStopTimesByShapeDist(st)
	// The first two should remain zero, last should be as input
	if got[0].ArrivalTime != 0 || got[1].ArrivalTime != 0 {
		t.Errorf("shape first missing: arrivals should remain zero, got: %v %v", got[0].ArrivalTime, got[1].ArrivalTime)
	}
	if got[0].DepartureTime != 0 || got[1].DepartureTime != 0 {
		t.Errorf("shape first missing: departures should remain zero, got: %v %v", got[0].DepartureTime, got[1].DepartureTime)
	}
	if !almostEq(got[2].ArrivalTime, dur("08:30:00")) {
		t.Errorf("shape first missing: last arrival wrong, got %v", got[2].ArrivalTime)
	}
	if !almostEq(got[2].DepartureTime, dur("08:35:00")) {
		t.Errorf("shape first missing: last depart wrong, got %v", got[2].DepartureTime)
	}
}

func TestInterpolateStopTimesByShapeDist_MissingLast(t *testing.T) {
	st := []ScheduledStopTime{
		{StopSequence: 1, ArrivalTime: dur("08:00:00"), DepartureTime: dur("08:05:00"), ShapeDistanceTraveled: ptr(0.0)},
		{StopSequence: 2, ArrivalTime: 0, DepartureTime: 0, ShapeDistanceTraveled: ptr(3.5)},
		{StopSequence: 3, ArrivalTime: 0, DepartureTime: 0, ShapeDistanceTraveled: ptr(10.5)},
	}
	got := interpolateStopTimesByShapeDist(st)
	// The first should be as input, the last two should remain zero
	if !almostEq(got[0].ArrivalTime, dur("08:00:00")) {
		t.Errorf("shape last missing: first arrival wrong, got %v", got[0].ArrivalTime)
	}
	if !almostEq(got[0].DepartureTime, dur("08:05:00")) {
		t.Errorf("shape last missing: first depart wrong, got %v", got[0].DepartureTime)
	}
	if got[1].ArrivalTime != 0 || got[2].ArrivalTime != 0 {
		t.Errorf("shape last missing: arrivals should remain zero, got: %v %v", got[1].ArrivalTime, got[2].ArrivalTime)
	}
	if got[1].DepartureTime != 0 || got[2].DepartureTime != 0 {
		t.Errorf("shape last missing: departures should remain zero, got: %v %v", got[1].DepartureTime, got[2].DepartureTime)
	}
}

func TestInterpolateStopTimesByShapeDist_NilDistanceFallsBackToEven(t *testing.T) {
	st := []ScheduledStopTime{
		{StopSequence: 1, ArrivalTime: dur("08:00:00"), DepartureTime: dur("08:00:00"), ShapeDistanceTraveled: ptr(0.0), ExactTimes: true},
		{StopSequence: 2},
		{StopSequence: 3, ShapeDistanceTraveled: ptr(9.0)},
		{StopSequence: 4, ArrivalTime: dur("08:30:00"), DepartureTime: dur("08:30:00"), ShapeDistanceTraveled: ptr(10.0), ExactTimes: true},
	}
	wantArr := []time.Duration{dur("08:00:00"), dur("08:10:00"), dur("08:20:00"), dur("08:30:00")}

	got := interpolateStopTimesByShapeDist(st)
	for i := range got {
		if !almostEq(got[i].ArrivalTime, wantArr[i]) {
			t.Errorf("nil distance: arrival %d: want %v got %v", i, wantArr[i], got[i].ArrivalTime)
		}
		if !almostEq(got[i].DepartureTime, wantArr[i]) {
			t.Errorf("nil distance: depart %d: want %v got %v", i, wantArr[i], got[i].DepartureTime)
		}
	}
}

func TestInterpolateStopTimesByShapeDist_NilEndpointDistanceFallsBackToEven(t *testing.T) {
	st := []ScheduledStopTime{
		{StopSequence: 1, ArrivalTime: dur("08:00:00"), DepartureTime: dur("08:00:00")},
		{StopSequence: 2, ShapeDistanceTraveled: ptr(9.0)},
		{StopSequence: 3, ArrivalTime: dur("08:20:00"), DepartureTime: dur("08:20:00"), ShapeDistanceTraveled: ptr(10.0)},
	}
	got := interpolateStopTimesByShapeDist(st)
	if !almostEq(got[1].ArrivalTime, dur("08:10:00")) {
		t.Errorf("nil endpoint distance: want 08:10:00 got %v", got[1].ArrivalTime)
	}
}

func TestScheduledStopTime_IsWindowedAndIsFlex(t *testing.T) {
	window := dur("08:00:00")
	for _, tc := range []struct {
		desc         string
		st           ScheduledStopTime
		wantWindowed bool
		wantFlex     bool
	}{
		{desc: "timed stop", st: ScheduledStopTime{Stop: &Stop{Id: "s"}}, wantWindowed: false, wantFlex: false},
		{desc: "windowed stop", st: ScheduledStopTime{Stop: &Stop{Id: "s"}, StartPickupDropOffWindow: &window, EndPickupDropOffWindow: &window}, wantWindowed: true, wantFlex: true},
		{desc: "only start window", st: ScheduledStopTime{StartPickupDropOffWindow: &window}, wantWindowed: false, wantFlex: false},
		{desc: "location without windows", st: ScheduledStopTime{Location: &Location{Id: "l"}}, wantWindowed: false, wantFlex: true},
		{desc: "group without windows", st: ScheduledStopTime{LocationGroup: &LocationGroup{Id: "g"}}, wantWindowed: false, wantFlex: true},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			if got := tc.st.IsWindowed(); got != tc.wantWindowed {
				t.Errorf("IsWindowed() = %v, want %v", got, tc.wantWindowed)
			}
			if got := tc.st.IsFlex(); got != tc.wantFlex {
				t.Errorf("IsFlex() = %v, want %v", got, tc.wantFlex)
			}
		})
	}
}

func TestInterpolateTimedStopTimes_SkipsWindowedRecords(t *testing.T) {
	window := dur("08:00:00")
	st := []ScheduledStopTime{
		{StopSequence: 1, ArrivalTime: dur("08:00:00"), DepartureTime: dur("08:00:00"), ExactTimes: true},
		{StopSequence: 2, StartPickupDropOffWindow: &window, EndPickupDropOffWindow: &window},
		{StopSequence: 3},
		{StopSequence: 4, StartPickupDropOffWindow: &window, EndPickupDropOffWindow: &window},
		{StopSequence: 5, ArrivalTime: dur("08:40:00"), DepartureTime: dur("08:40:00"), ExactTimes: true},
	}
	got := interpolateTimedStopTimes(st, false)

	if len(got) != 5 {
		t.Fatalf("got %d records, want 5", len(got))
	}
	for i, want := range []int{1, 2, 3, 4, 5} {
		if got[i].StopSequence != want {
			t.Errorf("record %d has stop_sequence %d, want %d (stop_sequence order must be preserved)", i, got[i].StopSequence, want)
		}
	}
	if !almostEq(got[2].ArrivalTime, dur("08:20:00")) || !almostEq(got[2].DepartureTime, dur("08:20:00")) {
		t.Errorf("timed record 3 = %v/%v, want 08:20:00 (even interpolation over the timed records only)", got[2].ArrivalTime, got[2].DepartureTime)
	}
	for _, i := range []int{1, 3} {
		if got[i].ArrivalTime != 0 || got[i].DepartureTime != 0 {
			t.Errorf("windowed record %d got times %v/%v, want none", i+1, got[i].ArrivalTime, got[i].DepartureTime)
		}
		if !got[i].IsWindowed() {
			t.Errorf("windowed record %d lost its window", i+1)
		}
	}
}

func TestInterpolateTimedStopTimes_ByShapeDistanceIgnoresWindowedRecordWithoutDistance(t *testing.T) {
	// A windowed record has no shape_dist_traveled. Before partitioning, the
	// shape-distance interpolation dereferenced it and panicked.
	window := dur("08:00:00")
	st := []ScheduledStopTime{
		{StopSequence: 1, ArrivalTime: dur("08:00:00"), DepartureTime: dur("08:00:00"), ShapeDistanceTraveled: ptr(0.0)},
		{StopSequence: 2, StartPickupDropOffWindow: &window, EndPickupDropOffWindow: &window},
		{StopSequence: 3, ShapeDistanceTraveled: ptr(7.5)},
		{StopSequence: 4, ArrivalTime: dur("08:40:00"), DepartureTime: dur("08:40:00"), ShapeDistanceTraveled: ptr(10.0)},
	}
	got := interpolateTimedStopTimes(st, true)
	if !almostEq(got[2].ArrivalTime, dur("08:30:00")) {
		t.Errorf("timed record 3 = %v, want 08:30:00 (distance-weighted: 7.5 of 10)", got[2].ArrivalTime)
	}
	if got[1].ArrivalTime != 0 {
		t.Errorf("windowed record got time %v, want none", got[1].ArrivalTime)
	}
}

func TestInterpolateTimedStopTimes_AllWindowedIsUnchanged(t *testing.T) {
	window := dur("08:00:00")
	st := []ScheduledStopTime{
		{StopSequence: 1, StartPickupDropOffWindow: &window, EndPickupDropOffWindow: &window},
		{StopSequence: 2, StartPickupDropOffWindow: &window, EndPickupDropOffWindow: &window},
	}
	got := interpolateTimedStopTimes(st, true)
	if len(got) != 2 || got[0].StopSequence != 1 || got[1].StopSequence != 2 {
		t.Errorf("all-windowed trip changed: %+v", got)
	}
}

func TestInterpolateTimedStopTimes_EmptyIsNil(t *testing.T) {
	if got := interpolateTimedStopTimes(nil, false); got != nil {
		t.Errorf("got %v, want nil so cmp.Diff on Static keeps matching", got)
	}
}

func TestInterpolateTimedStopTimes_AllTimedMatchesInterpolator(t *testing.T) {
	newTrip := func() []ScheduledStopTime {
		return []ScheduledStopTime{
			{StopSequence: 1, ArrivalTime: dur("08:00:00"), DepartureTime: dur("08:00:00"), ShapeDistanceTraveled: ptr(0.0)},
			{StopSequence: 2, ShapeDistanceTraveled: ptr(2.5)},
			{StopSequence: 3, ShapeDistanceTraveled: ptr(5.0)},
			{StopSequence: 4, ArrivalTime: dur("08:40:00"), DepartureTime: dur("08:40:00"), ShapeDistanceTraveled: ptr(10.0)},
		}
	}
	for _, tc := range []struct {
		desc         string
		byShapeDist  bool
		interpolator func([]ScheduledStopTime) []ScheduledStopTime
	}{
		{desc: "even", byShapeDist: false, interpolator: interpolateStopTimes},
		{desc: "by shape distance", byShapeDist: true, interpolator: interpolateStopTimesByShapeDist},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			input := newTrip()
			got := interpolateTimedStopTimes(input, tc.byShapeDist)
			if diff := cmp.Diff(got, tc.interpolator(newTrip())); diff != "" {
				t.Errorf("interpolateTimedStopTimes mismatch (-got +want):\n%s", diff)
			}
			if diff := cmp.Diff(input, newTrip()); diff != "" {
				t.Errorf("input was mutated (-got +want):\n%s", diff)
			}
		})
	}
}

func TestInterpolateTimedStopTimes_AllTimedCopiesOnce(t *testing.T) {
	st := []ScheduledStopTime{
		{StopSequence: 1, ArrivalTime: dur("08:00:00"), DepartureTime: dur("08:00:00")},
		{StopSequence: 2},
		{StopSequence: 3},
		{StopSequence: 4, ArrivalTime: dur("08:40:00"), DepartureTime: dur("08:40:00")},
	}
	// interpolateStopTimes makes the single result copy; partitioning an
	// all-timed trip must not add a second one.
	allocs := testing.AllocsPerRun(100, func() {
		interpolateTimedStopTimes(st, false)
	})
	if allocs != 1 {
		t.Errorf("got %v allocations per call, want 1", allocs)
	}
}
