package warnings

import (
	"fmt"

	"github.com/OneBusAway/go-gtfs/constants"
	"github.com/OneBusAway/go-gtfs/csv"
)

type StaticWarning struct {
	// Kind of warning
	Kind StaticWarningKind
	// File in the warning comes from
	File constants.StaticFile
	// Row number of the problematic row
	//
	// This is not necessarily the line number in the file.
	// For example, if the CSV file contains empty lines these won't be
	// counted towards the row number.
	RowNumber int
	// Content of the problematic row
	RowContent []string
	// Content of the header of the csv file.
	// If the warning is for the header this will be identical to LineContent.
	HeaderContent []string
}

func NewStaticWarning(csvFile *csv.File, kind StaticWarningKind) StaticWarning {
	return StaticWarning{
		Kind:          kind,
		File:          csvFile.Name(),
		RowNumber:     csvFile.RowNumber(),
		RowContent:    copyRow(csvFile.RowContent()),
		HeaderContent: csvFile.HeaderContent(),
	}
}

// copyRow detaches a row from the csv.File, which reuses its row storage
// when it reads the next row.
func copyRow(row []string) []string {
	if row == nil {
		return nil
	}
	copied := make([]string, len(row))
	copy(copied, row)
	return copied
}

// NewFileWarning builds a warning for a file that is not CSV, such as
// locations.geojson, where there is no row to point at.
func NewFileWarning(file constants.StaticFile, kind StaticWarningKind) StaticWarning {
	return StaticWarning{
		Kind: kind,
		File: file,
	}
}

// StaticWarningKind represents the kind of warning raised during GTFS static parsing.
//
// StaticWarningKind satisfies the error interface.
type StaticWarningKind interface {
	// Text of the warning message.
	Error() string // TODO: Message()

	// TODO: Fatal() ? And convert all parsing errors into warnings
}

type MissingColumns struct {
	Columns []string
}

func (w MissingColumns) Error() string {
	return fmt.Sprintf("csv file is missing columns %s", w.Columns)
}

type AgencyMissingValues struct {
	AgencyID string
	Columns  []string
}

func (w AgencyMissingValues) Error() string {
	return fmt.Sprintf("agency %q is missing values %s", w.AgencyID, w.Columns)
}

// StopTimeInvalidReference is raised when a stop_times.txt row does not
// reference exactly one of stop_id, location_id and location_group_id, or
// references an id (including a booking rule id) that does not resolve.
type StopTimeInvalidReference struct {
	Reason string
}

func (w StopTimeInvalidReference) Error() string {
	return fmt.Sprintf("stop time has an invalid reference: %s", w.Reason)
}

// StopTimeInvalidWindow is raised when a stop_times.txt row's
// start/end_pickup_drop_off_window pair violates the GTFS presence rules.
type StopTimeInvalidWindow struct {
	Reason string
}

func (w StopTimeInvalidWindow) Error() string {
	return fmt.Sprintf("stop time has an invalid pickup/drop-off window: %s", w.Reason)
}

// LocationGroupUnknownStop is raised when location_group_stops.txt names a
// stop that stops.txt does not define.
type LocationGroupUnknownStop struct {
	GroupID string
	StopID  string
}

func (w LocationGroupUnknownStop) Error() string {
	return fmt.Sprintf("location group %q references unknown stop %q", w.GroupID, w.StopID)
}

// LocationInvalidGeometry is raised when a locations.geojson Feature cannot be
// used: no id, no geometry, an unsupported geometry type or malformed
// coordinates.
type LocationInvalidGeometry struct {
	LocationID string
	Reason     string
}

func (w LocationInvalidGeometry) Error() string {
	return fmt.Sprintf("location %q has invalid geometry: %s", w.LocationID, w.Reason)
}

// LocationsFileInvalid is raised when locations.geojson as a whole cannot be
// used (malformed JSON, or not a FeatureCollection). The file is ignored.
type LocationsFileInvalid struct {
	Reason string
}

func (w LocationsFileInvalid) Error() string {
	return fmt.Sprintf("locations.geojson is invalid and was ignored: %s", w.Reason)
}

// BookingRuleInvalid is raised when a booking_rules.txt row is skipped.
type BookingRuleInvalid struct {
	BookingRuleID string
	Reason        string
}

func (w BookingRuleInvalid) Error() string {
	return fmt.Sprintf("booking rule %q is invalid: %s", w.BookingRuleID, w.Reason)
}
