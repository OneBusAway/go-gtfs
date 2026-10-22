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

// FileUnreadable is raised when an optional file that only adds to the feed,
// such as feed_info.txt, cannot be read as CSV, for example because a row has
// the wrong number of fields. Rows after the error are ignored, and the rest
// of the feed is parsed as usual.
type FileUnreadable struct {
	Reason string
}

func (w FileUnreadable) Error() string {
	return fmt.Sprintf("file could not be fully read: %s", w.Reason)
}

// FeedInfoExtraRow is raised for each feed_info.txt row after the first.
// GTFS allows one row. The extra rows are ignored.
type FeedInfoExtraRow struct{}

func (w FeedInfoExtraRow) Error() string {
	return "feed_info.txt has more than one row; only the first is read"
}

// FeedInfoMissingValues is raised when the feed_info.txt row leaves required
// values empty. The row is still read.
type FeedInfoMissingValues struct {
	Columns []string
}

func (w FeedInfoMissingValues) Error() string {
	return fmt.Sprintf("feed info is missing values %s", w.Columns)
}

// FeedInfoInvalidDate is raised when feed_start_date or feed_end_date is not
// a YYYYMMDD date. The date is read as unset.
type FeedInfoInvalidDate struct {
	Column string
	Value  string
}

func (w FeedInfoInvalidDate) Error() string {
	return fmt.Sprintf("feed info %s %q is not a YYYYMMDD date", w.Column, w.Value)
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

// TransferMissingTrip is raised when a transfers.txt row links trips
// (transfer_type 4 or 5) but leaves from_trip_id or to_trip_id empty. GTFS
// requires both for linked trips. The row is skipped.
type TransferMissingTrip struct {
	Column string
}

func (w TransferMissingTrip) Error() string {
	return fmt.Sprintf("linked-trip transfer is missing %s", w.Column)
}

// StopInvalidLocationType is raised when a stops.txt row has a location_type
// that GTFS does not define. Only empty and 0 through 4 are valid. The stop is
// kept and read as a stop (location_type 0).
type StopInvalidLocationType struct {
	StopID string
	Value  string
}

func (w StopInvalidLocationType) Error() string {
	return fmt.Sprintf("stop %q has invalid location_type %q; read as a stop", w.StopID, w.Value)
}

// StopUnknownLevel is raised when a stops.txt row names a level_id that
// levels.txt does not define. The stop is kept without a level.
type StopUnknownLevel struct {
	StopID  string
	LevelID string
}

func (w StopUnknownLevel) Error() string {
	return fmt.Sprintf("stop %q references unknown level %q", w.StopID, w.LevelID)
}

// LevelInvalid is raised when a levels.txt row is skipped.
type LevelInvalid struct {
	LevelID string
	Reason  string
}

func (w LevelInvalid) Error() string {
	return fmt.Sprintf("level %q is invalid: %s", w.LevelID, w.Reason)
}

// StopAccessInvalid is raised when a stops.txt row sets a stop_access that
// is not a valid value, or sets one where GTFS forbids it. The stop is kept
// with no stop_access.
type StopAccessInvalid struct {
	StopID string
	Value  string
	Reason string
}

func (w StopAccessInvalid) Error() string {
	return fmt.Sprintf("stop %q stop_access %q is ignored: %s", w.StopID, w.Value, w.Reason)
}

// PathwayInvalid is raised when a pathways.txt row is skipped.
type PathwayInvalid struct {
	PathwayID string
	Reason    string
}

func (w PathwayInvalid) Error() string {
	return fmt.Sprintf("pathway %q is invalid: %s", w.PathwayID, w.Reason)
}

// PathwayInvalidValue is raised when an optional pathways.txt value does not
// parse or is out of the range GTFS gives for it. The pathway is kept and
// the value is read as unset.
type PathwayInvalidValue struct {
	PathwayID string
	Column    string
	Value     string
}

func (w PathwayInvalidValue) Error() string {
	return fmt.Sprintf("pathway %q has invalid %s %q; read as unset", w.PathwayID, w.Column, w.Value)
}

// TranslationInvalid is raised when a translations.txt row is skipped.
type TranslationInvalid struct {
	Reason string
}

func (w TranslationInvalid) Error() string {
	return fmt.Sprintf("translation is invalid: %s", w.Reason)
}

// TranslationsWithoutFeedInfo is raised when a feed has translations.txt but
// no usable feed_info.txt, which GTFS requires alongside it. The translations
// are still read.
type TranslationsWithoutFeedInfo struct{}

func (w TranslationsWithoutFeedInfo) Error() string {
	return "translations.txt is present but feed_info.txt is missing or unusable"
}

// AttributionInvalid is raised when an attributions.txt row is skipped.
type AttributionInvalid struct {
	AttributionID string
	Reason        string
}

func (w AttributionInvalid) Error() string {
	return fmt.Sprintf("attribution %q is invalid: %s", w.AttributionID, w.Reason)
}

// AttributionWithoutRole is raised when an attributions.txt row sets none
// of is_producer, is_operator and is_authority to 1, which GTFS recommends.
// The attribution is kept.
type AttributionWithoutRole struct {
	AttributionID string
}

func (w AttributionWithoutRole) Error() string {
	return fmt.Sprintf("attribution %q has no role; set is_producer, is_operator or is_authority", w.AttributionID)
}

// NetworksDefinedTwice is raised when routes.txt sets network_id and the
// feed also has networks.txt or route_networks.txt, which GTFS forbids. The
// network files are used and routes.txt network_id is ignored.
type NetworksDefinedTwice struct{}

func (w NetworksDefinedTwice) Error() string {
	return "routes.txt network_id is ignored because networks.txt or route_networks.txt is present"
}

// NetworkInvalid is raised when a networks.txt row is skipped.
type NetworkInvalid struct {
	NetworkID string
	Reason    string
}

func (w NetworkInvalid) Error() string {
	return fmt.Sprintf("network %q is invalid: %s", w.NetworkID, w.Reason)
}

// RouteNetworkInvalid is raised when a route_networks.txt row is skipped.
type RouteNetworkInvalid struct {
	RouteID   string
	NetworkID string
	Reason    string
}

func (w RouteNetworkInvalid) Error() string {
	return fmt.Sprintf("route %q in network %q is skipped: %s", w.RouteID, w.NetworkID, w.Reason)
}

// AreaInvalid is raised when an areas.txt row is skipped.
type AreaInvalid struct {
	AreaID string
	Reason string
}

func (w AreaInvalid) Error() string {
	return fmt.Sprintf("area %q is invalid: %s", w.AreaID, w.Reason)
}

// StopAreaInvalid is raised when a stop_areas.txt row is skipped.
type StopAreaInvalid struct {
	AreaID string
	StopID string
	Reason string
}

func (w StopAreaInvalid) Error() string {
	return fmt.Sprintf("stop %q in area %q is skipped: %s", w.StopID, w.AreaID, w.Reason)
}

// FareAttributeInvalid is raised when a fare_attributes.txt row is skipped.
type FareAttributeInvalid struct {
	FareID string
	Reason string
}

func (w FareAttributeInvalid) Error() string {
	return fmt.Sprintf("fare %q is invalid: %s", w.FareID, w.Reason)
}

// FareAttributeInvalidValue is raised when an optional fare_attributes.txt
// value is invalid. The fare is kept and the value is read as unset.
type FareAttributeInvalidValue struct {
	FareID string
	Column string
	Value  string
}

func (w FareAttributeInvalidValue) Error() string {
	return fmt.Sprintf("fare %q has invalid %s %q; read as unset", w.FareID, w.Column, w.Value)
}

// FareAttributeAgencyAmbiguous is raised when a fare_attributes.txt row gives
// no agency_id in a feed with several agencies, where GTFS requires one. The
// fare is kept with no agency.
type FareAttributeAgencyAmbiguous struct {
	FareID string
}

func (w FareAttributeAgencyAmbiguous) Error() string {
	return fmt.Sprintf("fare %q has no agency_id but the feed has several agencies", w.FareID)
}

// FareRuleInvalid is raised when a fare_rules.txt row is skipped.
type FareRuleInvalid struct {
	FareID string
	Reason string
}

func (w FareRuleInvalid) Error() string {
	return fmt.Sprintf("fare rule for fare %q is invalid: %s", w.FareID, w.Reason)
}

// RiderCategoryInvalid is raised when a rider_categories.txt row is skipped.
type RiderCategoryInvalid struct {
	RiderCategoryID string
	Reason          string
}

func (w RiderCategoryInvalid) Error() string {
	return fmt.Sprintf("rider category %q is invalid: %s", w.RiderCategoryID, w.Reason)
}

// FareMediaInvalid is raised when a fare_media.txt row is skipped.
type FareMediaInvalid struct {
	FareMediaID string
	Reason      string
}

func (w FareMediaInvalid) Error() string {
	return fmt.Sprintf("fare media %q is invalid: %s", w.FareMediaID, w.Reason)
}

// FareProductInvalid is raised when a fare_products.txt row is skipped.
type FareProductInvalid struct {
	FareProductID string
	Reason        string
}

func (w FareProductInvalid) Error() string {
	return fmt.Sprintf("fare product %q is invalid: %s", w.FareProductID, w.Reason)
}

// FareProductDefaultCategory is raised when several rider categories are
// eligible for a fare product but Defaults of them, not exactly one, are the
// default, which GTFS requires. The product is kept.
type FareProductDefaultCategory struct {
	FareProductID string
	Defaults      int
}

func (w FareProductDefaultCategory) Error() string {
	return fmt.Sprintf("fare product %q has %d default rider categories among its eligible ones; want exactly 1", w.FareProductID, w.Defaults)
}

// TimeframeInvalid is raised when a timeframes.txt row is skipped.
type TimeframeInvalid struct {
	TimeframeGroupID string
	Reason           string
}

func (w TimeframeInvalid) Error() string {
	return fmt.Sprintf("timeframe in group %q is invalid: %s", w.TimeframeGroupID, w.Reason)
}

// FareLegRuleInvalid is raised when a fare_leg_rules.txt row is skipped.
// Since an unset field in a leg rule matches what the other rules leave out,
// a skipped rule can change what the remaining rules match.
type FareLegRuleInvalid struct {
	LegGroupID string
	Reason     string
}

func (w FareLegRuleInvalid) Error() string {
	return fmt.Sprintf("fare leg rule in group %q is invalid: %s", w.LegGroupID, w.Reason)
}

// TransferMissingStop is raised when a transfers.txt row that does not link
// trips (transfer_type empty or 0 to 3) leaves from_stop_id or to_stop_id
// empty. GTFS requires both for these types. The row is skipped.
type TransferMissingStop struct {
	Column string
}

func (w TransferMissingStop) Error() string {
	return fmt.Sprintf("transfer is missing %s", w.Column)
}

// TransferInvalidType is raised when a transfers.txt row has a transfer_type
// that GTFS does not define. Only empty and 0 through 5 are valid. The row is
// skipped, because reading the value as some other type could change what
// the rule means.
type TransferInvalidType struct {
	Value string
}

func (w TransferInvalidType) Error() string {
	return fmt.Sprintf("transfer has invalid transfer_type %q", w.Value)
}

// TransferUnknownReference is raised when a transfers.txt row names a stop,
// route or trip that is not in the parsed feed: either stops.txt, routes.txt
// or trips.txt does not define it, or its row there was skipped. The
// transfer row is skipped too. Reading an unknown route or trip ID as unset
// would make the rule apply more widely than the feed says.
type TransferUnknownReference struct {
	Column string
	ID     string
}

func (w TransferUnknownReference) Error() string {
	return fmt.Sprintf("transfer references unknown %s %q", w.Column, w.ID)
}

// TransferStopNotAllowed is raised when a transfers.txt row names a
// from_stop_id or to_stop_id whose location_type the transfer does not allow.
// GTFS allows a stop or a station (location_type 0 or 1), and only a stop for
// linked trips (transfer_type 4 or 5). Allowed describes what the row needed.
// The row is skipped.
type TransferStopNotAllowed struct {
	Column  string
	StopID  string
	Allowed string
}

func (w TransferStopNotAllowed) Error() string {
	return fmt.Sprintf("transfer %s %q is not %s", w.Column, w.StopID, w.Allowed)
}

// TransferTripNotOnRoute is raised when a transfers.txt row sets a trip and a
// route on the same side (from_ or to_) but the trip runs on a different
// route. GTFS requires the trip to belong to the route. The row is skipped.
type TransferTripNotOnRoute struct {
	TripColumn string
	TripID     string
	RouteID    string
}

func (w TransferTripNotOnRoute) Error() string {
	return fmt.Sprintf("transfer %s %q does not run on route %q", w.TripColumn, w.TripID, w.RouteID)
}
