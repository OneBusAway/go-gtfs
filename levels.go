package gtfs

import (
	"fmt"
	"math"

	"github.com/OneBusAway/go-gtfs/csv"
	"github.com/OneBusAway/go-gtfs/warnings"
)

// Level is a floor of a station, from levels.txt.
type Level struct {
	Id string
	// Index orders the levels: 0 is ground level, positive is above ground
	// and negative is below.
	Index float64
	Name  string
}

// parseLevels reads levels.txt. A row with a missing or unparsable value, or
// a repeated level_id, is skipped with a warning.
func parseLevels(csv *csv.File) ([]Level, []warnings.StaticWarning) {
	idColumn := csv.RequiredColumn("level_id")
	indexColumn := csv.RequiredColumn("level_index")
	nameColumn := csv.OptionalColumn("level_name")

	if w := checkForMissingColumns(csv); len(w) > 0 {
		return nil, w
	}

	var levels []Level
	var w []warnings.StaticWarning
	seen := map[string]bool{}
	invalid := func(id, reason string) {
		w = append(w, warnings.NewStaticWarning(csv, warnings.LevelInvalid{LevelID: id, Reason: reason}))
	}
	for csv.NextRow() {
		id := idColumn.Read()
		rawIndex := indexColumn.Read()
		if missingKeys := csv.MissingRowKeys(); len(missingKeys) > 0 {
			invalid(id, fmt.Sprintf("missing values %s", missingKeys))
			continue
		}
		index := parseFloat64(rawIndex)
		if index == nil || math.IsNaN(*index) || math.IsInf(*index, 0) {
			invalid(id, fmt.Sprintf("invalid level_index %q", rawIndex))
			continue
		}
		if seen[id] {
			invalid(id, "duplicate level_id")
			continue
		}
		seen[id] = true
		levels = append(levels, Level{Id: id, Index: *index, Name: nameColumn.Read()})
	}
	return levels, w
}
