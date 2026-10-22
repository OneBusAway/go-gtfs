package gtfs

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"

	"github.com/OneBusAway/go-gtfs/constants"
	"github.com/OneBusAway/go-gtfs/csv"
	"github.com/OneBusAway/go-gtfs/warnings"
)

// StaticStream is a static feed parsed without its stop times and shape
// points. It reads those, the bulk of a large feed, on demand, so the whole
// feed never has to be in memory at once.
//
// Static holds what ParseStatic returns, except that each trip's StopTimes is
// nil, Static.Shapes is nil, and a trip's Shape carries only its ID: its
// points are nil even when the shape has points. Read the stop times with
// EachTripStopTimes and the shapes with EachShape; both give the same values
// ParseStatic would.
//
// The stream reads from the trips and stops as parsed. A caller may replace
// Static.Trips or Static.Stops, with a filtered copy for example, but must not
// change those slices in place while it still reads from the stream.
type StaticStream struct {
	Static *Static

	files map[constants.StaticFile]*zip.File

	// trips and refs are the trips, and what stop times refer to, as
	// parsed, so the stream still works if the caller replaces Static's
	// slices.
	trips []ScheduledTrip
	refs  stopTimeReferences

	// stopTimeCounts holds, for each trip in trips, how many stop times
	// EachTripStopTimes yields for it.
	stopTimeCounts []int
	// hasShapeDistances is true when any stop time in the feed has a
	// shape_dist_traveled value, which decides how times are interpolated.
	hasShapeDistances bool
	// shapeRowCounts holds how many points each shape has.
	shapeRowCounts map[string]int
}

// ParseStaticStream parses the feed in r, of the given size, as a
// StaticStream. opts.StopTimes must be StopTimesFull. With opts.SkipShapes,
// EachShape yields nothing and trips have no Shape.
//
// It reads stop_times.txt and shapes.txt once here, to count each trip's
// stop times and each shape's points, and again in EachTripStopTimes and
// EachShape.
func ParseStaticStream(r io.ReaderAt, size int64, opts ParseStaticOptions) (*StaticStream, error) {
	if opts.StopTimes != StopTimesFull {
		return nil, errors.New("ParseStaticStream needs StopTimesFull")
	}
	reader, err := zip.NewReader(r, size)
	if err != nil {
		return nil, err
	}
	stream := &StaticStream{files: staticZipFiles(reader), shapeRowCounts: map[string]int{}}
	stream.Static, err = parseStatic(stream.files, opts, stream)
	if err != nil {
		return nil, err
	}
	stream.trips = stream.Static.Trips
	return stream, nil
}

// Trip returns the trip at tripIndex in Static.Trips as ParseStaticStream
// returned it.
func (s *StaticStream) Trip(tripIndex int) *ScheduledTrip {
	return &s.trips[tripIndex]
}

// StopTimeCount returns how many stop times EachTripStopTimes yields for the
// trip at tripIndex in Static.Trips as ParseStaticStream returned it. A trip
// with none is never yielded.
func (s *StaticStream) StopTimeCount(tripIndex int) int {
	return s.stopTimeCounts[tripIndex]
}

// countStopTimeRows counts each trip's stop times and returns the warnings
// for the rows it drops, which EachTripStopTimes doesn't report again.
func (s *StaticStream) countStopTimeRows(file *csv.File, static *Static) []warnings.StaticWarning {
	s.stopTimeCounts = make([]int, len(static.Trips))
	s.refs = newStopTimeReferences(static)
	reader, missing := newStopTimeReader(file, s.refs)
	if missing != nil {
		return missingColumnsWarning(file, missing)
	}
	eachTripRow(file, reader.tripIDColumn, s.refs.tripIndexes, reader.read, func(tripIndex int, _ ScheduledStopTime) bool {
		s.stopTimeCounts[tripIndex]++
		return true
	})
	s.hasShapeDistances = reader.hasShapeDistances
	return reader.warnings
}

func (s *StaticStream) countShapeRows(file *csv.File) {
	reader, missing := newShapeReader(file)
	if missing != nil {
		fmt.Println(missing)
		return
	}
	reader.eachRow(func(shapeID string, _ ShapeRow) bool {
		s.shapeRowCounts[shapeID]++
		return true
	})
}

// EachTripStopTimes calls fn with the index of each trip that has stop times,
// in Static.Trips as ParseStaticStream returned it, and its stop times,
// ordered by stop_sequence and interpolated as ParseStatic does. Trips come in
// the order their last stop time appears in stop_times.txt.
//
// It holds the stop times of the trips it hasn't finished: for a file grouped
// by trip, one trip at a time, but for a file in another order (by stop or by
// time, say) most of the file. It stops at the first error fn returns and
// returns it.
func (s *StaticStream) EachTripStopTimes(fn func(tripIndex int, stopTimes []ScheduledStopTime) error) error {
	file, err := s.open("stop_times.txt")
	if err != nil {
		return err
	}
	reader, missing := newStopTimeReader(file, s.refs)
	if missing != nil {
		return file.Close()
	}

	groups := newGroupBuffer[int, ScheduledStopTime](func(tripIndex int) int { return s.stopTimeCounts[tripIndex] })
	var fnErr error
	eachTripRow(file, reader.tripIDColumn, s.refs.tripIndexes, reader.read, func(tripIndex int, stopTime ScheduledStopTime) bool {
		stopTimes, done := groups.add(tripIndex, stopTime)
		if !done {
			return true
		}
		fnErr = fn(tripIndex, finishTripStopTimes(stopTimes, s.hasShapeDistances))
		return fnErr == nil
	})
	return firstError(fnErr, file.Close())
}

// EachShape calls fn with each shape, its points ordered by
// shape_pt_sequence as ParseStatic does. Shapes come in the order their last
// point appears in shapes.txt. Like EachTripStopTimes, it holds the points of
// the shapes it hasn't finished. It stops at the first error fn returns and
// returns it.
func (s *StaticStream) EachShape(fn func(Shape) error) error {
	if len(s.shapeRowCounts) == 0 {
		return nil
	}
	file, err := s.open("shapes.txt")
	if err != nil {
		return err
	}
	reader, missing := newShapeReader(file)
	if missing != nil {
		return file.Close()
	}

	groups := newGroupBuffer[string, ShapeRow](func(shapeID string) int { return s.shapeRowCounts[shapeID] })
	var fnErr error
	reader.eachRow(func(shapeID string, row ShapeRow) bool {
		rows, done := groups.add(shapeID, row)
		if !done {
			return true
		}
		fnErr = fn(finishShape(shapeID, rows))
		return fnErr == nil
	})
	return firstError(fnErr, file.Close())
}

// groupBuffer collects rows by group until each group has as many rows as
// its size says. The group being read is kept outside the map, so a file
// grouped by key costs no map operation per row.
type groupBuffer[K comparable, T any] struct {
	size        func(K) int
	current     K
	currentSize int
	rows        []T
	active      bool
	pending     map[K][]T
}

func newGroupBuffer[K comparable, T any](size func(K) int) *groupBuffer[K, T] {
	return &groupBuffer[K, T]{size: size, pending: map[K][]T{}}
}

// add adds a row to its group and, once the group is complete, returns its
// rows and true.
func (g *groupBuffer[K, T]) add(key K, row T) ([]T, bool) {
	if !g.active || key != g.current {
		if g.active {
			g.pending[g.current] = g.rows
		}
		g.current, g.currentSize, g.active = key, g.size(key), true
		g.rows = g.pending[key]
		delete(g.pending, key)
		if g.rows == nil {
			g.rows = make([]T, 0, g.currentSize)
		}
	}
	g.rows = append(g.rows, row)
	if len(g.rows) < g.currentSize {
		return nil, false
	}
	rows := g.rows
	g.rows, g.active = nil, false
	return rows, true
}

// open opens one of the feed's files.
func (s *StaticStream) open(name constants.StaticFile) (*csv.File, error) {
	zipFile := s.files[name]
	if zipFile == nil {
		return nil, fmt.Errorf("no %q file in GTFS static feed", name)
	}
	file, err := openCsvFile(name, zipFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read %q: %w", name, err)
	}
	return file, nil
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
