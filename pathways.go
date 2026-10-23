package gtfs

import (
	"fmt"
	"strings"

	"github.com/OneBusAway/go-gtfs/csv"
	"github.com/OneBusAway/go-gtfs/warnings"
)

// Pathway is an edge of a station's internal access graph, from pathways.txt.
type Pathway struct {
	Id   string
	From *Stop
	To   *Stop
	Mode PathwayMode
	// IsBidirectional is false when the pathway can only be used from From
	// to To.
	IsBidirectional bool
	// Length is the horizontal length in meters.
	Length *float64
	// TraversalTime is the average time in seconds to walk the pathway.
	TraversalTime *int32
	// StairCount is positive when the rider walks up from From to To and
	// negative when they walk down.
	StairCount *int32
	// MaxSlope is the maximum slope ratio, positive for upwards.
	MaxSlope *float64
	// MinWidth is the minimum width in meters.
	MinWidth             *float64
	SignpostedAs         string
	ReversedSignpostedAs string
}

// PathwayMode is the kind of a pathway.
type PathwayMode int32

const (
	PathwayMode_Walkway        PathwayMode = 1
	PathwayMode_Stairs         PathwayMode = 2
	PathwayMode_MovingSidewalk PathwayMode = 3
	PathwayMode_Escalator      PathwayMode = 4
	PathwayMode_Elevator       PathwayMode = 5
	// PathwayMode_FareGate crosses into an area where proof of payment is
	// required.
	PathwayMode_FareGate PathwayMode = 6
	// PathwayMode_ExitGate leaves a paid area into an unpaid one.
	PathwayMode_ExitGate PathwayMode = 7
)

func (m PathwayMode) String() string {
	switch m {
	case PathwayMode_Walkway:
		return "WALKWAY"
	case PathwayMode_Stairs:
		return "STAIRS"
	case PathwayMode_MovingSidewalk:
		return "MOVING_SIDEWALK"
	case PathwayMode_Escalator:
		return "ESCALATOR"
	case PathwayMode_Elevator:
		return "ELEVATOR"
	case PathwayMode_FareGate:
		return "FARE_GATE"
	case PathwayMode_ExitGate:
		return "EXIT_GATE"
	default:
		return "UNKNOWN"
	}
}

// parsePathwayMode reads a pathway_mode, ignoring surrounding whitespace. ok
// is false for any value GTFS does not define.
func parsePathwayMode(s string) (m PathwayMode, ok bool) {
	switch strings.TrimSpace(s) {
	case "1":
		return PathwayMode_Walkway, true
	case "2":
		return PathwayMode_Stairs, true
	case "3":
		return PathwayMode_MovingSidewalk, true
	case "4":
		return PathwayMode_Escalator, true
	case "5":
		return PathwayMode_Elevator, true
	case "6":
		return PathwayMode_FareGate, true
	case "7":
		return PathwayMode_ExitGate, true
	default:
		return 0, false
	}
}

// StopAccess is how riders reach a stop in a station, from stops.txt
// stop_access. Its numbering differs from GTFS, which uses 0 and 1 for
// StopAccess_ThroughStation and StopAccess_Direct.
type StopAccess int32

const (
	StopAccess_NotSpecified StopAccess = 0
	// StopAccess_ThroughStation means the stop cannot be reached from the
	// street directly, only through the station's entrances or pathways.
	StopAccess_ThroughStation StopAccess = 1
	// StopAccess_Direct means directions should lead straight to the stop,
	// not through the station's entrances or pathways.
	StopAccess_Direct StopAccess = 2
)

func (a StopAccess) String() string {
	switch a {
	case StopAccess_NotSpecified:
		return "NOT_SPECIFIED"
	case StopAccess_ThroughStation:
		return "THROUGH_STATION"
	case StopAccess_Direct:
		return "DIRECT"
	default:
		return "UNKNOWN"
	}
}

// parseStopAccess reads a stop_access, ignoring surrounding whitespace. Empty
// is StopAccess_NotSpecified. ok is false for any value GTFS does not define.
func parseStopAccess(s string) (a StopAccess, ok bool) {
	switch strings.TrimSpace(s) {
	case "":
		return StopAccess_NotSpecified, true
	case "0":
		return StopAccess_ThroughStation, true
	case "1":
		return StopAccess_Direct, true
	default:
		return StopAccess_NotSpecified, false
	}
}

// parsePathways reads pathways.txt. A row is skipped with a warning if a
// required value is missing or invalid, if it names an unknown stop or one
// GTFS forbids as a pathway end, if it is a bidirectional exit gate, or if
// its pathway_id repeats an earlier one. An invalid optional value is read as unset with a warning.
func parsePathways(csv *csv.File, stopIdToStop map[string]*Stop) ([]Pathway, []warnings.StaticWarning) {
	idColumn := csv.RequiredColumn("pathway_id")
	fromColumn := csv.RequiredColumn("from_stop_id")
	toColumn := csv.RequiredColumn("to_stop_id")
	modeColumn := csv.RequiredColumn("pathway_mode")
	bidirectionalColumn := csv.RequiredColumn("is_bidirectional")
	lengthColumn := csv.OptionalColumn("length")
	traversalTimeColumn := csv.OptionalColumn("traversal_time")
	stairCountColumn := csv.OptionalColumn("stair_count")
	maxSlopeColumn := csv.OptionalColumn("max_slope")
	minWidthColumn := csv.OptionalColumn("min_width")
	signpostedAsColumn := csv.OptionalColumn("signposted_as")
	reversedSignpostedAsColumn := csv.OptionalColumn("reversed_signposted_as")

	if w := checkForMissingColumns(csv); len(w) > 0 {
		return nil, w
	}

	var pathways []Pathway
	var w []warnings.StaticWarning
	invalid := func(id, format string, a ...any) {
		w = append(w, warnings.NewStaticWarning(csv, warnings.PathwayInvalid{PathwayID: id, Reason: fmt.Sprintf(format, a...)}))
	}
	// resolve looks up a pathway end. GTFS forbids a station, or a stop
	// riders reach directly from the street, as either end.
	resolve := func(id, column, stopID string) *Stop {
		stop := stopIdToStop[stopID]
		switch {
		case stop == nil:
			invalid(id, "unknown %s %q", column, stopID)
		case stop.Type == StopType_Station:
			invalid(id, "%s %q is a station", column, stopID)
		case stop.Access == StopAccess_Direct:
			invalid(id, "%s %q has stop_access 1", column, stopID)
		default:
			return stop
		}
		return nil
	}
	// Each optional number is read as unset, with a warning, when it does
	// not parse or is out of the range GTFS gives for it.
	checked := func(id, column, raw string, valid bool) bool {
		if raw != "" && !valid {
			w = append(w, warnings.NewStaticWarning(csv, warnings.PathwayInvalidValue{PathwayID: id, Column: column, Value: raw}))
		}
		return raw != "" && valid
	}
	// A nil inRange accepts any value.
	optionalFloat := func(id, column, raw string, inRange func(float64) bool) *float64 {
		f := parseFiniteFloat64(raw)
		if !checked(id, column, raw, f != nil && (inRange == nil || inRange(*f))) {
			return nil
		}
		return f
	}
	optionalInt := func(id, column, raw string, inRange func(int32) bool) *int32 {
		i := parseInt32(strings.TrimSpace(raw))
		if !checked(id, column, raw, i != nil && inRange(*i)) {
			return nil
		}
		return i
	}
	seen := map[string]bool{}
	for csv.NextRow() {
		id := idColumn.Read()
		fromID := fromColumn.Read()
		toID := toColumn.Read()
		rawMode := modeColumn.Read()
		rawBidirectional := bidirectionalColumn.Read()
		if missingKeys := csv.MissingRowKeys(); len(missingKeys) > 0 {
			invalid(id, "missing values %s", missingKeys)
			continue
		}
		mode, ok := parsePathwayMode(rawMode)
		if !ok {
			invalid(id, "invalid pathway_mode %q", rawMode)
			continue
		}
		var bidirectional bool
		switch strings.TrimSpace(rawBidirectional) {
		case "0":
		case "1":
			bidirectional = true
		default:
			invalid(id, "invalid is_bidirectional %q", rawBidirectional)
			continue
		}
		if bidirectional && mode == PathwayMode_ExitGate {
			invalid(id, "exit gate (pathway_mode 7) is bidirectional")
			continue
		}
		from := resolve(id, "from_stop_id", fromID)
		if from == nil {
			continue
		}
		to := resolve(id, "to_stop_id", toID)
		if to == nil {
			continue
		}
		if seen[id] {
			invalid(id, "duplicate pathway_id")
			continue
		}
		seen[id] = true
		pathways = append(pathways, Pathway{
			Id:                   id,
			From:                 from,
			To:                   to,
			Mode:                 mode,
			IsBidirectional:      bidirectional,
			Length:               optionalFloat(id, "length", lengthColumn.Read(), func(f float64) bool { return f >= 0 }),
			TraversalTime:        optionalInt(id, "traversal_time", traversalTimeColumn.Read(), func(i int32) bool { return i > 0 }),
			StairCount:           optionalInt(id, "stair_count", stairCountColumn.Read(), func(i int32) bool { return i != 0 }),
			MaxSlope:             optionalFloat(id, "max_slope", maxSlopeColumn.Read(), nil),
			MinWidth:             optionalFloat(id, "min_width", minWidthColumn.Read(), func(f float64) bool { return f > 0 }),
			SignpostedAs:         signpostedAsColumn.Read(),
			ReversedSignpostedAs: reversedSignpostedAsColumn.Read(),
		})
	}
	return pathways, w
}
