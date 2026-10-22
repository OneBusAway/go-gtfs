package gtfs

import (
	"fmt"
	"slices"

	"github.com/OneBusAway/go-gtfs/csv"
	"github.com/OneBusAway/go-gtfs/warnings"
)

// Area is a group of stops, such as a fare zone, from areas.txt.
type Area struct {
	Id   string
	Name string
	// Stops are the stops stop_areas.txt lists for the area. A listed
	// station is not expanded into its platforms; use Contains to apply
	// that rule.
	Stops []*Stop
}

// Contains reports whether stop is in the area. A stop is in it if
// stop_areas.txt lists it there. A platform (a stop whose parent is a
// station) is also in the area if its station is, unless stop_areas.txt
// lists the platform itself, in any area, which overrides its station.
func (a *Area) Contains(stop *Stop) bool {
	if stop == nil {
		return false
	}
	if a.listed(stop) {
		return true
	}
	parent := stop.Parent
	return stop.Type == StopType_Stop && len(stop.Areas) == 0 &&
		parent != nil && parent.Type == StopType_Station && a.listed(parent)
}

// listed reports whether stop_areas.txt lists stop in the area. It matches
// by ID, not pointer, so a copy of the Area (such as a range loop variable
// over Static.Areas) still works.
func (a *Area) listed(stop *Stop) bool {
	return slices.ContainsFunc(stop.Areas, func(b *Area) bool { return b.Id == a.Id })
}

// parseAreas reads areas.txt. A row with an empty or repeated area_id is
// skipped with a warning.
func parseAreas(csv *csv.File) ([]Area, []warnings.StaticWarning) {
	idColumn := csv.RequiredColumn("area_id")
	nameColumn := csv.OptionalColumn("area_name")

	if w := checkForMissingColumns(csv); len(w) > 0 {
		return nil, w
	}

	var areas []Area
	var w []warnings.StaticWarning
	seen := map[string]bool{}
	for csv.NextRow() {
		id := idColumn.Read()
		reason := ""
		if missingKeys := csv.MissingRowKeys(); len(missingKeys) > 0 {
			reason = fmt.Sprintf("missing values %s", missingKeys)
		} else if seen[id] {
			reason = "duplicate area_id"
		}
		if reason != "" {
			w = append(w, warnings.NewStaticWarning(csv, warnings.AreaInvalid{AreaID: id, Reason: reason}))
			continue
		}
		seen[id] = true
		areas = append(areas, Area{Id: id, Name: nameColumn.Read()})
	}
	return areas, w
}

// parseStopAreas reads stop_areas.txt into the areas' Stops and the stops'
// Areas. A row with a missing value, an unknown ID, or that repeats an
// earlier row is skipped with a warning.
func parseStopAreas(csv *csv.File, areas []Area, stopIdToStop map[string]*Stop) []warnings.StaticWarning {
	areaIDColumn := csv.RequiredColumn("area_id")
	stopIDColumn := csv.RequiredColumn("stop_id")

	if w := checkForMissingColumns(csv); len(w) > 0 {
		return w
	}

	areaIdToArea := areasByID(areas)
	type pair struct {
		area *Area
		stop *Stop
	}
	seen := map[pair]bool{}
	var w []warnings.StaticWarning
	for csv.NextRow() {
		areaID := areaIDColumn.Read()
		stopID := stopIDColumn.Read()
		area := areaIdToArea[areaID]
		stop := stopIdToStop[stopID]
		reason := ""
		switch missingKeys := csv.MissingRowKeys(); {
		case len(missingKeys) > 0:
			reason = fmt.Sprintf("missing values %s", missingKeys)
		case area == nil:
			reason = "unknown area_id"
		case stop == nil:
			reason = "unknown stop_id"
		case seen[pair{area, stop}]:
			reason = "duplicate row"
		default:
			seen[pair{area, stop}] = true
			area.Stops = append(area.Stops, stop)
			stop.Areas = append(stop.Areas, area)
			continue
		}
		w = append(w, warnings.NewStaticWarning(csv, warnings.StopAreaInvalid{AreaID: areaID, StopID: stopID, Reason: reason}))
	}
	return w
}
