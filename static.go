// Package gtfs contains parsers for GTFS static and realtime feeds.
package gtfs

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"log"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/OneBusAway/go-gtfs/constants"
	"github.com/OneBusAway/go-gtfs/csv"
	"github.com/OneBusAway/go-gtfs/warnings"
)

// Static contains the parsed content for a single GTFS static message.
type Static struct {
	Agencies  []Agency
	Routes    []Route
	Networks  []Network
	Stops     []Stop
	Levels    []Level
	Areas     []Area
	Pathways  []Pathway
	Transfers []Transfer
	Services  []Service
	Trips     []ScheduledTrip
	Shapes    []Shape

	// Locations is nil when locations.geojson is absent.
	Locations []Location
	// LocationGroups is nil when location_groups.txt is absent.
	LocationGroups []LocationGroup
	// BookingRules is nil when booking_rules.txt is absent.
	BookingRules []BookingRule
	// Translations is nil when translations.txt is absent. Use NewTranslator
	// to look them up.
	Translations []Translation
	// FareAttributes and FareRules are GTFS-Fares V1. Both are nil if either
	// file cannot be read as CSV, since a fare with no rules applies to
	// every trip.
	FareAttributes []FareAttribute
	FareRules      []FareRule
	// RiderCategories, FareMedia and FareProducts are GTFS-Fares V2, which
	// GTFS recommends using instead of V1 when a feed has both.
	RiderCategories []RiderCategory
	FareMedia       []FareMedia
	FareProducts    []FareProduct
	// Timeframes and FareLegRules are dropped entirely if their file cannot
	// be read as CSV: an unset field in a leg rule matches what the other
	// rules leave out, so a partial set would change what the rest match.
	Timeframes   []Timeframe
	FareLegRules []FareLegRule
	// Attributions with no Agency, Route or Trip apply to the whole feed.
	Attributions []Attribution

	// FeedInfo is nil when the feed has no feed_info.txt, or when that file
	// lacks a required column or has no data row that can be read.
	FeedInfo *FeedInfo

	// Warnings raised during GTFS static parsing.
	Warnings []warnings.StaticWarning
}

// Agency corresponds to a single row in the agency.txt file.
type Agency struct {
	Id       string
	Name     string
	Url      string
	Timezone string
	Language string
	Phone    string
	FareUrl  string
	Email    string
}

// Route corresponds to a single row in the routes.txt file.
type Route struct {
	Id                string
	Agency            *Agency
	Color             string
	TextColor         string
	ShortName         string
	LongName          string
	Description       string
	Type              RouteType
	Url               string
	SortOrder         *int32
	ContinuousPickup  PickupDropOffPolicy
	ContinuousDropOff PickupDropOffPolicy
	// Network is nil when the route is in no network.
	Network *Network
}

type Stop struct {
	Id                 string
	Code               string
	Name               string
	TtsName            string
	Description        string
	ZoneId             string
	Longitude          *float64
	Latitude           *float64
	Url                string
	Type               StopType
	Parent             *Stop
	Timezone           string
	WheelchairBoarding WheelchairBoarding
	PlatformCode       string
	// Level is nil when stops.txt gives no level_id or names an unknown one.
	Level  *Level
	Access StopAccess
	// Areas are the areas stop_areas.txt lists the stop in. A platform not
	// listed in any is also in its station's areas; see Area.Contains.
	Areas []*Area
}

// Root returns the root stop.
func (stop *Stop) Root() *Stop {
	for {
		if stop.Parent == nil {
			return stop
		}
		stop = stop.Parent
	}
}

type Transfer struct {
	// From and To are nil when a linked-trip transfer (TransferType_InSeat
	// or TransferType_InSeatNotAllowed) omits from_stop_id or to_stop_id.
	From            *Stop
	To              *Stop
	Type            TransferType
	MinTransferTime *int32

	FromRoute *Route
	ToRoute   *Route

	FromTrip *ScheduledTrip
	ToTrip   *ScheduledTrip
}

type Service struct {
	Id           string
	Monday       bool
	Tuesday      bool
	Wednesday    bool
	Thursday     bool
	Friday       bool
	Saturday     bool
	Sunday       bool
	StartDate    time.Time
	EndDate      time.Time
	AddedDates   []time.Time
	RemovedDates []time.Time
}

type ScheduledTrip struct {
	Route                *Route
	Service              *Service
	ID                   string
	Headsign             string
	ShortName            string
	DirectionId          DirectionID
	BlockID              string
	WheelchairAccessible WheelchairBoarding
	BikesAllowed         BikesAllowed
	StopTimes            []ScheduledStopTime
	Shape                *Shape
	Frequencies          []Frequency

	// Stops lists the stops the trip visits, in stop_sequence order. It is
	// populated only when ParseStaticOptions.StopTimes is StopTimesStopsOnly,
	// which leaves StopTimes empty; otherwise use StopTimes[i].Stop.
	Stops []*Stop

	// SafeDurationFactor and SafeDurationOffset (GTFS-Flex) scale the
	// scheduled travel time into a rider-facing upper bound. Nil when absent.
	SafeDurationFactor *float64
	SafeDurationOffset *float64
}

type ScheduledStopTime struct {
	Trip                  *ScheduledTrip
	Stop                  *Stop // nil on GTFS-Flex rows that reference a Location or LocationGroup
	ArrivalTime           time.Duration
	DepartureTime         time.Duration
	StopSequence          int
	Headsign              string
	PickupType            PickupDropOffPolicy
	DropOffType           PickupDropOffPolicy
	ContinuousPickup      PickupDropOffPolicy
	ContinuousDropOff     PickupDropOffPolicy
	ShapeDistanceTraveled *float64
	ExactTimes            bool
	// Flex holds the row's GTFS-Flex columns, or is nil when it sets none.
	// Most feeds have no Flex rows, so keeping these fields out of line saves
	// memory on every stop time.
	Flex *FlexStopTime
}

// FlexStopTime holds the GTFS-Flex columns of a stop_times.txt row. Exactly
// one of ScheduledStopTime.Stop, Location and LocationGroup is set.
type FlexStopTime struct {
	Location                 *Location
	LocationGroup            *LocationGroup
	StartPickupDropOffWindow *time.Duration
	EndPickupDropOffWindow   *time.Duration
	PickupBookingRule        *BookingRule
	DropOffBookingRule       *BookingRule
	// Draft-era placement tolerated for real feeds; the adopted spec puts
	// these on trips.txt (see ScheduledTrip).
	SafeDurationFactor *float64
	SafeDurationOffset *float64
}

// IsWindowed reports whether the record carries a pickup/drop-off window
// instead of arrival/departure times.
func (st ScheduledStopTime) IsWindowed() bool {
	return st.StartPickupDropOffWindow() != nil && st.EndPickupDropOffWindow() != nil
}

// IsFlex reports whether the record is an on-demand (GTFS-Flex) record.
func (st ScheduledStopTime) IsFlex() bool {
	return st.IsWindowed() || st.Location() != nil || st.LocationGroup() != nil
}

// The accessors below return a GTFS-Flex field, or nil when the record has
// no Flex columns.

// Location returns the GTFS-Flex zone the record serves, or nil.
func (st ScheduledStopTime) Location() *Location {
	if st.Flex == nil {
		return nil
	}
	return st.Flex.Location
}

// LocationGroup returns the location group the record serves, or nil.
func (st ScheduledStopTime) LocationGroup() *LocationGroup {
	if st.Flex == nil {
		return nil
	}
	return st.Flex.LocationGroup
}

// StartPickupDropOffWindow returns the start of the record's pickup/drop-off
// window, or nil.
func (st ScheduledStopTime) StartPickupDropOffWindow() *time.Duration {
	if st.Flex == nil {
		return nil
	}
	return st.Flex.StartPickupDropOffWindow
}

// EndPickupDropOffWindow returns the end of the record's pickup/drop-off
// window, or nil.
func (st ScheduledStopTime) EndPickupDropOffWindow() *time.Duration {
	if st.Flex == nil {
		return nil
	}
	return st.Flex.EndPickupDropOffWindow
}

// PickupBookingRule returns the booking rule for boarding, or nil.
func (st ScheduledStopTime) PickupBookingRule() *BookingRule {
	if st.Flex == nil {
		return nil
	}
	return st.Flex.PickupBookingRule
}

// DropOffBookingRule returns the booking rule for alighting, or nil.
func (st ScheduledStopTime) DropOffBookingRule() *BookingRule {
	if st.Flex == nil {
		return nil
	}
	return st.Flex.DropOffBookingRule
}

// SafeDurationFactor returns the draft-era stop_times.txt
// safe_duration_factor, or nil.
func (st ScheduledStopTime) SafeDurationFactor() *float64 {
	if st.Flex == nil {
		return nil
	}
	return st.Flex.SafeDurationFactor
}

// SafeDurationOffset returns the draft-era stop_times.txt
// safe_duration_offset, or nil.
func (st ScheduledStopTime) SafeDurationOffset() *float64 {
	if st.Flex == nil {
		return nil
	}
	return st.Flex.SafeDurationOffset
}

type ShapePoint struct {
	Latitude  float64
	Longitude float64
	Distance  *float64
}

type Shape struct {
	ID     string
	Points []ShapePoint
}

type Frequency struct {
	StartTime  time.Duration
	EndTime    time.Duration
	Headway    time.Duration
	ExactTimes ExactTimes
}

// BookingRule corresponds to a single row in the booking_rules.txt file.
//
// Fields that GTFS marks conditionally required are pointers and stay nil when
// the feed omits them; real feeds do omit them, and consumers decide how to
// treat the gap.
type BookingRule struct {
	Id                     string
	Type                   BookingType
	PriorNoticeDurationMin *int32 // minutes
	PriorNoticeDurationMax *int32 // minutes
	PriorNoticeLastDay     *int32
	PriorNoticeLastTime    *time.Duration
	PriorNoticeStartDay    *int32
	PriorNoticeStartTime   *time.Duration
	PriorNoticeServiceId   string
	Message                string
	PickupMessage          string
	DropOffMessage         string
	PhoneNumber            string
	InfoUrl                string
	BookingUrl             string
}

// LocationGroup corresponds to a single row in the location_groups.txt file
// with its location_group_stops.txt members resolved.
type LocationGroup struct {
	Id    string
	Name  string
	Stops []*Stop // resolved members; unknown stop ids are skipped with a warning
}

type ParseStaticOptions struct {
	// If true, wheelchair boarding information is inherited from parent station
	// when unspecified for a child stop/platform, entrance, or exit.
	InheritWheelchairBoarding bool

	// If true, shapes.txt is not parsed: Static.Shapes and every
	// ScheduledTrip.Shape are nil.
	SkipShapes bool

	// StopTimes controls how much of stop_times.txt is kept. Stop times
	// dominate the memory of a parsed feed, so callers that do not need
	// arrival and departure times can save most of it.
	StopTimes StopTimesMode
}

// StopTimesMode controls how ParseStatic loads stop_times.txt.
type StopTimesMode int

const (
	// StopTimesFull populates ScheduledTrip.StopTimes. This is the default.
	StopTimesFull StopTimesMode = iota

	// StopTimesStopsOnly populates ScheduledTrip.Stops, the trip's stops in
	// stop_sequence order, and leaves ScheduledTrip.StopTimes nil. It keeps
	// one pointer per stop time instead of a full ScheduledStopTime.
	StopTimesStopsOnly

	// StopTimesNone skips stop_times.txt entirely, which then need not exist.
	StopTimesNone
)

// ParseStatic parses the content as a GTFS static feed.
func ParseStatic(content []byte, opts ParseStaticOptions) (*Static, error) {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, err
	}
	return parseStatic(staticZipFiles(reader), opts, nil)
}

// staticZipFiles indexes the files of a GTFS zip by name.
func staticZipFiles(reader *zip.Reader) map[constants.StaticFile]*zip.File {
	fileNameToFile := map[constants.StaticFile]*zip.File{}
	for _, file := range reader.File {
		fileNameToFile[constants.StaticFile(file.Name)] = file
	}
	return fileNameToFile
}

// parseStatic parses the feed's files. When stream is non-nil, it only counts
// the rows of shapes.txt and stop_times.txt, for stream to read later.
func parseStatic(fileNameToFile map[constants.StaticFile]*zip.File, opts ParseStaticOptions, stream *StaticStream) (*Static, error) {
	result := &Static{}
	locationsPresent, err := parseLocationsFile(fileNameToFile[constants.LocationsGeoJSONFile], result)
	if err != nil {
		return nil, err
	}
	serviceIdToService := map[string]Service{}
	// With SkipShapes the map stays nil, which tells parseScheduledTrips not
	// to look up (and warn about) shape IDs that were never loaded.
	var shapeIdToShape map[string]*Shape
	if !opts.SkipShapes {
		shapeIdToShape = map[string]*Shape{}
	}
	tripIdToScheduledTrip := map[string]*ScheduledTrip{}
	// Each is built right after its file is read and shared by the parsers
	// that resolve stop or route IDs. stopIdToStop stays nil when stops.txt
	// is absent.
	var stopIdToStop map[string]*Stop
	var routeIdToRoute map[string]*Route
	// GTFS forbids routes.txt network_id when either network file exists.
	networkFilesPresent := !isAbsentOrEmpty(fileNameToFile[constants.NetworksFile]) || !isAbsentOrEmpty(fileNameToFile[constants.RouteNetworksFile])
	// Set when a Fares V1 file cannot be read or lacks a required column, so
	// that both are dropped.
	faresV1Unreadable := false
	dropFaresV1 := func() {
		faresV1Unreadable = true
		result.FareAttributes, result.FareRules = nil, nil
	}
	timezone := time.UTC
	for _, table := range []struct {
		File        constants.StaticFile
		Action      func(file *csv.File) []warnings.StaticWarning
		PostProcess func()
		Optional    bool
		Skip        bool
		// Discard, when set, makes an unreadable file a warning rather than
		// an error. It runs when reading stops at a malformed row, and must
		// undo whatever Action stored that the missing rows make unreliable.
		// Use it for files that only add to the feed, whose problems should
		// not stop the rest from parsing.
		Discard func()
	}{
		{
			File: constants.AgencyFile,
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				result.Agencies, w = parseAgencies(file)
				if len(result.Agencies) > 0 {
					var err error
					timezone, err = time.LoadLocation(result.Agencies[0].Timezone)
					if err != nil {
						timezone = time.UTC
					}
				}
				return
			},
		},
		{
			File: constants.FeedInfoFile,
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				result.FeedInfo, w = parseFeedInfo(file, timezone)
				return
			},
			Optional: true,
			// Only the first row is used, and any row read before the
			// error parsed cleanly, so there is nothing to undo.
			Discard: func() {},
		},
		{
			File: constants.RoutesFile,
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				var networkIDs []string
				result.Routes, networkIDs = parseRoutes(file, result.Agencies)
				routeIdToRoute = routesByID(result.Routes)
				if !networkFilesPresent {
					result.Networks = networksFromRoutes(result.Routes, networkIDs)
				} else if slices.ContainsFunc(networkIDs, func(id string) bool { return id != "" }) {
					w = append(w, warnings.NewFileWarning(constants.RoutesFile, warnings.NetworksDefinedTwice{}))
				}
				return
			},
		},
		{
			File: constants.NetworksFile,
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				result.Networks, w = parseNetworks(file)
				return
			},
			Optional: true,
			// Each row stands alone, so rows read before the error are kept.
			Discard: func() {},
		},
		{
			File: constants.FareAttributesFile,
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				var ok bool
				result.FareAttributes, w, ok = parseFareAttributes(file, result.Agencies)
				if !ok {
					dropFaresV1()
				}
				return
			},
			Optional: true,
			Discard:  dropFaresV1,
		},
		{
			File: constants.FareRulesFile,
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				// Without usable fares, every rule would only report an
				// unknown fare_id.
				if faresV1Unreadable {
					return
				}
				var ok bool
				result.FareAttributes, result.FareRules, w, ok = parseFareRules(file, result.FareAttributes, routeIdToRoute)
				if !ok {
					// With no rules, every fare would apply everywhere.
					dropFaresV1()
				}
				return
			},
			Optional: true,
			Discard:  dropFaresV1,
		},
		{
			File: constants.RiderCategoriesFile,
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				result.RiderCategories, w = parseRiderCategories(file)
				return
			},
			Optional: true,
			// Each row stands alone, so rows read before the error are kept.
			Discard: func() {},
		},
		{
			File: constants.FareMediaFile,
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				result.FareMedia, w = parseFareMedia(file)
				return
			},
			Optional: true,
			Discard:  func() {},
		},
		{
			File: constants.FareProductsFile,
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				result.FareProducts, w = parseFareProducts(file, result.RiderCategories, result.FareMedia)
				return
			},
			Optional: true,
			Discard:  func() {},
		},
		{
			File: constants.RouteNetworksFile,
			Action: func(file *csv.File) []warnings.StaticWarning {
				return parseRouteNetworks(file, result.Networks, routeIdToRoute)
			},
			Optional: true,
			// Each row stands alone, so assignments made before the error
			// are kept.
			Discard: func() {},
		},
		{
			File: constants.LevelsFile,
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				result.Levels, w = parseLevels(file)
				return
			},
			Optional: true,
			// Levels read before the error parsed cleanly. Keeping them
			// spares stops on those levels an unknown-level warning.
			Discard: func() {},
		},
		{
			File: constants.StopsFile,
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				result.Stops, w = parseStops(file, opts.InheritWheelchairBoarding, levelsByID(result.Levels))
				stopIdToStop = stopsByID(result.Stops)
				return
			},
			// GTFS makes stops.txt optional when locations.geojson defines zones.
			Optional: locationsPresent,
		},
		{
			File: constants.PathwaysFile,
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				result.Pathways, w = parsePathways(file, stopIdToStop)
				return
			},
			Optional: true,
			// Pathways describe a station's whole access graph, so part of
			// it would mislead routing.
			Discard: func() { result.Pathways = nil },
		},
		{
			File: constants.AreasFile,
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				result.Areas, w = parseAreas(file)
				return
			},
			Optional: true,
			// Each row stands alone, so rows read before the error are kept.
			Discard: func() {},
		},
		{
			File: constants.StopAreasFile,
			Action: func(file *csv.File) []warnings.StaticWarning {
				return parseStopAreas(file, result.Areas, stopIdToStop)
			},
			Optional: true,
			// Each row stands alone, so rows read before the error are kept.
			Discard: func() {},
		},
		{
			File: constants.LocationGroupsFile,
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				result.LocationGroups, w = parseLocationGroups(file)
				return
			},
			Optional: true,
		},
		{
			File: constants.LocationGroupStopsFile,
			Action: func(file *csv.File) []warnings.StaticWarning {
				return parseLocationGroupStops(file, result.LocationGroups, stopIdToStop)
			},
			Optional: true,
		},
		{
			File: "calendar.txt",
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				parseCalendar(file, serviceIdToService, timezone)
				return
			},
			Optional: true,
		},
		{
			File: "calendar_dates.txt",
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				parseCalendarDates(file, serviceIdToService, timezone)
				return
			},
			PostProcess: func() {
				for _, service := range serviceIdToService {
					result.Services = append(result.Services, service)
				}
			},
			Optional: true,
		},
		{
			File: constants.TimeframesFile,
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				result.Timeframes, w = parseTimeframes(file, servicesByID(result.Services))
				return
			},
			Optional: true,
			Discard:  func() { result.Timeframes = nil },
		},
		{
			File: constants.FareLegRulesFile,
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				result.FareLegRules, w = parseFareLegRules(file, result.Networks, result.Areas, result.Timeframes, result.FareProducts)
				return
			},
			Optional: true,
			Discard:  func() { result.FareLegRules = nil },
		},
		{
			File: "shapes.txt",
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				if stream != nil {
					stream.countShapeRows(file)
					for shapeID := range stream.shapeRowCounts {
						shapeIdToShape[shapeID] = &Shape{ID: shapeID}
					}
					return
				}
				result.Shapes = parseShapes(file)
				for idx, shape := range result.Shapes {
					shapeIdToShape[shape.ID] = &result.Shapes[idx]
				}
				return
			},
			Optional: true,
			Skip:     opts.SkipShapes,
		},
		{
			File: constants.BookingRulesFile,
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				result.BookingRules, w = parseBookingRules(file)
				return
			},
			Optional: true,
		},
		{
			File: "trips.txt",
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				result.Trips = parseScheduledTrips(file, routeIdToRoute, result.Services, shapeIdToShape)
				for idx, trip := range result.Trips {
					tripIdToScheduledTrip[trip.ID] = &result.Trips[idx]
				}
				return
			},
		},
		{
			File: constants.TransfersFile,
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				result.Transfers, w = parseTransfers(file, stopIdToStop, routeIdToRoute, tripIdToScheduledTrip)
				return
			},
			Optional: true,
		},
		{
			File: constants.AttributionsFile,
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				result.Attributions, w = parseAttributions(file, result.Agencies, routeIdToRoute, tripIdToScheduledTrip)
				return
			},
			Optional: true,
			// Each row stands alone, so rows read before the error are kept.
			Discard: func() {},
		},
		{
			File: "frequencies.txt",
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				parseFrequencies(file, tripIdToScheduledTrip)
				return
			},
			Optional: true,
		},
		{
			File: constants.TranslationsFile,
			Action: func(file *csv.File) (w []warnings.StaticWarning) {
				result.Translations, w = parseTranslations(file)
				// GTFS requires feed_info.txt alongside translations.txt,
				// since feed_lang says what language the originals are in.
				// feed_info.txt is read earlier, so a nil FeedInfo also
				// covers a file that is present but has no usable row.
				if result.FeedInfo == nil {
					w = append(w, warnings.NewFileWarning(constants.TranslationsFile, warnings.TranslationsWithoutFeedInfo{}))
				}
				return
			},
			Optional: true,
			// Each row stands alone, so rows read before the error are kept.
			Discard: func() {},
		},
		{
			File: constants.StopTimesFile,
			Action: func(file *csv.File) []warnings.StaticWarning {
				if stream != nil {
					return stream.countStopTimeRows(file, result, stopIdToStop)
				}
				if opts.StopTimes == StopTimesStopsOnly {
					parseTripStops(file, stopIdToStop, result.Trips)
					return nil
				}
				return parseScheduledStopTimes(file, result, stopIdToStop)
			},
			Skip: opts.StopTimes == StopTimesNone,
		},
	} {
		if table.Skip {
			continue
		}
		if table.PostProcess == nil {
			table.PostProcess = func() {}
		}
		zipFile := fileNameToFile[table.File]
		if table.Optional && isAbsentOrEmpty(zipFile) {
			table.PostProcess()
			continue
		}
		if zipFile == nil {
			return nil, fmt.Errorf("no %q file in GTFS static feed", table.File)
		}
		file, err := openCsvFile(table.File, zipFile)
		if err != nil {
			if table.Discard != nil {
				// Discard can undo state shared with other tables, such as
				// the Fares V1 pair, so it runs here too.
				table.Discard()
				result.Warnings = append(result.Warnings, warnings.NewFileWarning(table.File, warnings.FileUnreadable{Reason: err.Error()}))
				table.PostProcess()
				continue
			}
			return nil, fmt.Errorf("failed to read %q: %w", table.File, err)
		}
		w := table.Action(file)
		if err := file.Close(); err != nil {
			if table.Discard == nil {
				return nil, fmt.Errorf("failed to read %q: %w", table.File, err)
			}
			table.Discard()
			w = append(w, warnings.NewFileWarning(table.File, warnings.FileUnreadable{Reason: err.Error()}))
		}
		table.PostProcess()
		result.Warnings = append(result.Warnings, w...)
	}
	return result, nil
}

func openCsvFile(file constants.StaticFile, zipFile *zip.File) (*csv.File, error) {
	content, err := zipFile.Open()
	if err != nil {
		return nil, err
	}
	f, err := csv.New(file, content)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// isAbsentOrEmpty reports whether an optional feed file should be treated as
// not provided. A zero-byte file has no header row and is meaningless.
func isAbsentOrEmpty(zipFile *zip.File) bool {
	return zipFile == nil || zipFile.UncompressedSize64 == 0
}

// parseLocationsFile reads locations.geojson, when present and non-empty, into
// result. It runs before the CSV files because its presence decides whether
// stops.txt is required. Only the exact name locations.geojson is read. A file
// that cannot be used at all is reported as a warning and treated as absent.
func parseLocationsFile(zipFile *zip.File, result *Static) (present bool, err error) {
	if isAbsentOrEmpty(zipFile) {
		return false, nil
	}
	content, err := readZipFile(zipFile)
	if err != nil {
		return false, fmt.Errorf("failed to read %q: %w", constants.LocationsGeoJSONFile, err)
	}
	locations, w, err := parseLocations(content)
	if err != nil {
		// Zones are optional service; an unusable file must not also take
		// down the feed's fixed-route data, so it is reported and ignored.
		result.Warnings = append(result.Warnings, warnings.NewFileWarning(constants.LocationsGeoJSONFile,
			warnings.LocationsFileInvalid{Reason: err.Error()}))
		return false, nil
	}
	result.Locations = locations
	result.Warnings = append(result.Warnings, w...)
	return true, nil
}

func readZipFile(zipFile *zip.File) ([]byte, error) {
	reader, err := zipFile.Open()
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(reader)
	return b, firstError(err, reader.Close())
}

func parseAgencies(csv *csv.File) ([]Agency, []warnings.StaticWarning) {
	var w []warnings.StaticWarning
	idColumn := csv.OptionalColumn("agency_id")
	nameColumn := csv.RequiredColumn("agency_name")
	urlColumn := csv.RequiredColumn("agency_url")
	timezoneColumn := csv.RequiredColumn("agency_timezone")
	languageColumn := csv.OptionalColumn("agency_lang")
	phoneColumn := csv.OptionalColumn("agency_phone")
	fareUrlColumn := csv.OptionalColumn("agency_fare_url")
	emailColumn := csv.OptionalColumn("agency_email")

	if warnings := checkForMissingColumns(csv); len(warnings) > 0 {
		return nil, warnings
	}

	var agencies []Agency
	for csv.NextRow() {
		name := nameColumn.Read()
		agency := Agency{
			// TODO: support specifying the default agency ID in the GTFS static parser settings
			Id:       idColumn.ReadOr(fmt.Sprintf("%s_id", name)),
			Name:     name,
			Url:      urlColumn.Read(),
			Timezone: timezoneColumn.Read(),
			Language: languageColumn.Read(),
			Phone:    phoneColumn.Read(),
			FareUrl:  fareUrlColumn.Read(),
			Email:    emailColumn.Read(),
		}
		if missingKeys := csv.MissingRowKeys(); len(missingKeys) > 0 {
			w = append(w, warnings.NewStaticWarning(csv, warnings.AgencyMissingValues{
				AgencyID: agency.Id,
				Columns:  missingKeys,
			}))
			continue
		}
		agencies = append(agencies, agency)
	}
	return agencies, w
}

// parseRoutes reads routes.txt. networkIDs holds each returned route's
// network_id, or is nil when routes.txt has no network_id column.
func parseRoutes(csv *csv.File, agencies []Agency) (routes []Route, networkIDs []string) {
	idColumn := csv.RequiredColumn("route_id")
	agencyIDColumn := csv.OptionalColumn("agency_id")
	colorColumn := csv.OptionalColumn("route_color")
	textColorColumn := csv.OptionalColumn("route_text_color")
	shortNameColumn := csv.OptionalColumn("route_short_name")
	longNameColumn := csv.OptionalColumn("route_long_name")
	descriptionColumn := csv.OptionalColumn("route_desc")
	routeTypeColumn := csv.RequiredColumn("route_type")
	urlColumn := csv.OptionalColumn("route_url")
	sortOrderColumn := csv.OptionalColumn("route_sort_order")
	continuousPickupColumn := csv.OptionalColumn("continuous_pickup")
	continuousDropOffColumn := csv.OptionalColumn("continuous_drop_off")
	networkIDColumn := csv.OptionalColumn("network_id")

	if err := csv.MissingRequiredColumns(); err != nil {
		fmt.Println(err)
		return nil, nil
	}

	agencyIdToAgency := agenciesByID(agencies)
	for csv.NextRow() {
		routeID := idColumn.Read()
		agencyID := agencyIDColumn.Read()
		var agency *Agency
		if agencyID != "" {
			if agency = agencyIdToAgency[agencyID]; agency == nil {
				log.Printf("skipping route %s: no match for agency ID %s", routeID, agencyID)
				continue
			}
		} else if len(agencies) == 1 {
			// In GTFS static if there is a single agency, a route's agency ID field can be omitted in
			// which case the route's agency is the unique agency in the feed.
			agency = &agencies[0]
		} else {
			log.Printf("skipping route %s: no agency ID provided but no unique agency", routeID)
			continue
		}
		route := Route{
			Id:                routeID,
			Agency:            agency,
			Color:             colorColumn.ReadOr("FFFFFF"),
			TextColor:         textColorColumn.ReadOr("000000"),
			ShortName:         shortNameColumn.Read(),
			LongName:          longNameColumn.Read(),
			Description:       descriptionColumn.Read(),
			Type:              parseRouteType_GTFSStatic(routeTypeColumn.Read()),
			Url:               urlColumn.Read(),
			SortOrder:         parseRouteSortOrder(sortOrderColumn.Read()),
			ContinuousPickup:  parsePickupDropOffPolicy(continuousPickupColumn.ReadOr("")),
			ContinuousDropOff: parsePickupDropOffPolicy(continuousDropOffColumn.ReadOr("")),
		}
		if missingKeys := csv.MissingRowKeys(); len(missingKeys) > 0 {
			log.Printf("Skipping route %+v because of missing keys %s", route, missingKeys)
			continue
		}
		routes = append(routes, route)
		if networkIDColumn.Exists() {
			networkIDs = append(networkIDs, networkIDColumn.Read())
		}
	}
	return routes, networkIDs
}

func parseRouteSortOrder(raw string) *int32 {
	if raw == "" {
		return nil
	}
	i, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	i32 := int32(i)
	return &i32
}

func parseStops(csv *csv.File, inheritWheelchairBoarding bool, levelIdToLevel map[string]*Level) ([]Stop, []warnings.StaticWarning) {
	idColumn := csv.RequiredColumn("stop_id")
	codeColumn := csv.OptionalColumn("stop_code")
	nameColumn := csv.OptionalColumn("stop_name")
	ttsNameColumn := csv.OptionalColumn("tts_stop_name")
	descriptionColumn := csv.OptionalColumn("stop_desc")
	zoneIdColumn := csv.OptionalColumn("zone_id")
	longitudeColumn := csv.OptionalColumn("stop_lon")
	latitudeColumn := csv.OptionalColumn("stop_lat")
	urlColumn := csv.OptionalColumn("stop_url")
	typeColumn := csv.OptionalColumn("location_type")
	timezoneColumn := csv.OptionalColumn("stop_timezone")
	wheelchairBoardingColumn := csv.OptionalColumn("wheelchair_boarding")
	platformCodeColumn := csv.OptionalColumn("platform_code")
	parentStationColumn := csv.OptionalColumn("parent_station")
	levelIDColumn := csv.OptionalColumn("level_id")
	accessColumn := csv.OptionalColumn("stop_access")

	if err := csv.MissingRequiredColumns(); err != nil {
		fmt.Println(err)
		return nil, nil
	}

	var stops []Stop
	var w []warnings.StaticWarning
	stopIdToIndex := map[string]int{}
	stopIdToParent := map[string]string{}
	for csv.NextRow() {
		stopID := idColumn.Read()
		parentStopId := parentStationColumn.Read()
		rawType := typeColumn.Read()
		stopType, ok := parseStopType(rawType)
		stop := Stop{
			Id:                 stopID,
			Code:               codeColumn.Read(),
			Name:               nameColumn.Read(),
			TtsName:            ttsNameColumn.Read(),
			Description:        descriptionColumn.Read(),
			ZoneId:             zoneIdColumn.Read(),
			Longitude:          parseFloat64(longitudeColumn.Read()),
			Latitude:           parseFloat64(latitudeColumn.Read()),
			Url:                urlColumn.Read(),
			Type:               stopType,
			Timezone:           timezoneColumn.Read(),
			WheelchairBoarding: parseWheelchairBoarding(wheelchairBoardingColumn.Read()),
			PlatformCode:       platformCodeColumn.Read(),
		}
		if missingKeys := csv.MissingRowKeys(); len(missingKeys) > 0 {
			log.Printf("Skipping stop %+v because of missing keys %s", stop, missingKeys)
			continue
		}
		if parentStopId != "" {
			stopIdToParent[stopID] = parentStopId
		}
		if !ok {
			w = append(w, warnings.NewStaticWarning(csv, warnings.StopInvalidLocationType{StopID: stopID, Value: rawType}))
		}
		if rawAccess := accessColumn.Read(); rawAccess != "" {
			access, ok := parseStopAccess(rawAccess)
			// GTFS forbids stop_access on anything but a stop in a station.
			reason := ""
			switch {
			case !ok:
				reason = "not a valid value"
			case stop.Type != StopType_Stop:
				reason = "only a stop (location_type 0) may set it"
			case parentStopId == "":
				reason = "the stop has no parent_station"
			default:
				stop.Access = access
			}
			if reason != "" {
				w = append(w, warnings.NewStaticWarning(csv, warnings.StopAccessInvalid{StopID: stopID, Value: rawAccess, Reason: reason}))
			}
		}
		if levelID := levelIDColumn.Read(); levelID != "" {
			stop.Level = levelIdToLevel[levelID]
			if stop.Level == nil {
				w = append(w, warnings.NewStaticWarning(csv, warnings.StopUnknownLevel{StopID: stopID, LevelID: levelID}))
			}
		}
		stopIdToIndex[stop.Id] = len(stops)
		stops = append(stops, stop)
	}
	for stopId, parentStopId := range stopIdToParent {
		parentStopIndex, ok := stopIdToIndex[parentStopId]
		if !ok {
			continue
		}
		stops[stopIdToIndex[stopId]].Parent = &stops[parentStopIndex]
	}

	// Inherit wheelchair boarding from parent stops if specified.
	if inheritWheelchairBoarding {
		for i := range stops {
			stop := &stops[i]
			if stop.Parent != nil && stop.Parent.Type == StopType_Station && stop.WheelchairBoarding == WheelchairBoarding_NotSpecified {
				stop.WheelchairBoarding = stop.Parent.WheelchairBoarding
			}
		}
	}

	return stops, w
}

func parseLocationGroups(csv *csv.File) ([]LocationGroup, []warnings.StaticWarning) {
	idColumn := csv.RequiredColumn("location_group_id")
	nameColumn := csv.OptionalColumn("location_group_name")

	if w := checkForMissingColumns(csv); len(w) > 0 {
		return nil, w
	}

	var groups []LocationGroup
	for csv.NextRow() {
		id := idColumn.Read()
		if missingKeys := csv.MissingRowKeys(); len(missingKeys) > 0 {
			log.Printf("Skipping location group because of missing keys %s", missingKeys)
			continue
		}
		groups = append(groups, LocationGroup{Id: id, Name: nameColumn.Read()})
	}
	return groups, nil
}

// parseLocationGroupStops appends each row's stop to its group's Stops. A row
// naming an unknown group is logged and skipped (matching how transfers.txt
// handles dangling ids); a row naming an unknown stop raises a warning.
func parseLocationGroupStops(csv *csv.File, groups []LocationGroup, stopIdToStop map[string]*Stop) []warnings.StaticWarning {
	groupIDColumn := csv.RequiredColumn("location_group_id")
	stopIDColumn := csv.RequiredColumn("stop_id")

	if w := checkForMissingColumns(csv); len(w) > 0 {
		return w
	}

	idToGroup := map[string]*LocationGroup{}
	for i := range groups {
		idToGroup[groups[i].Id] = &groups[i]
	}
	var w []warnings.StaticWarning
	for csv.NextRow() {
		groupID := groupIDColumn.Read()
		stopID := stopIDColumn.Read()
		if missingKeys := csv.MissingRowKeys(); len(missingKeys) > 0 {
			log.Printf("Skipping location group stop because of missing keys %s", missingKeys)
			continue
		}
		group := idToGroup[groupID]
		if group == nil {
			log.Printf("Skipping location group stop because location group %q is unknown", groupID)
			continue
		}
		stop := stopIdToStop[stopID]
		if stop == nil {
			w = append(w, warnings.NewStaticWarning(csv, warnings.LocationGroupUnknownStop{
				GroupID: groupID,
				StopID:  stopID,
			}))
			continue
		}
		group.Stops = append(group.Stops, stop)
	}
	return w
}

func parseFloat64(s string) *float64 {
	if s == "" {
		return nil
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return nil
	}
	return &f
}

// parseTransfers reads transfers.txt. from_stop_id and to_stop_id are required
// unless the row links trips (transfer_type 4 or 5), in which case
// from_trip_id and to_trip_id are required instead and a linked trip's stop
// may be nil.
func parseTransfers(csv *csv.File, stopIdToStop map[string]*Stop, routeIdToRoute map[string]*Route, tripIdToTrip map[string]*ScheduledTrip) ([]Transfer, []warnings.StaticWarning) {
	fromStopIDColumn := csv.OptionalColumn("from_stop_id")
	toStopIDColumn := csv.OptionalColumn("to_stop_id")
	fromRouteIDColumn := csv.OptionalColumn("from_route_id")
	toRouteIDColumn := csv.OptionalColumn("to_route_id")
	fromTripIDColumn := csv.OptionalColumn("from_trip_id")
	toTripIDColumn := csv.OptionalColumn("to_trip_id")
	typeColumn := csv.OptionalColumn("transfer_type")
	transferTimeColumn := csv.OptionalColumn("min_transfer_time")

	// The stop columns are optional for linked trips only. If one is absent,
	// a single warning is added on the first row that needs it.
	var missingStopColumns []string
	if !fromStopIDColumn.Exists() {
		missingStopColumns = append(missingStopColumns, "from_stop_id")
	}
	if !toStopIDColumn.Exists() {
		missingStopColumns = append(missingStopColumns, "to_stop_id")
	}
	reportedMissingStopColumns := false

	var transfers []Transfer
	var w []warnings.StaticWarning
	// resolveStop looks up an optional stop id. ok is false, and a warning is
	// added, if the id is given but unknown.
	resolveStop := func(column, id string) (stop *Stop, ok bool) {
		if id == "" {
			return nil, true
		}
		stop = stopIdToStop[id]
		if stop == nil {
			w = append(w, warnings.NewStaticWarning(csv, warnings.TransferUnknownReference{Column: column, ID: id}))
			return nil, false
		}
		return stop, true
	}
	// checkTripOnRoute warns if a trip and a route are both given on one side
	// of the transfer but the trip runs on another route. GTFS requires the
	// trip to belong to the route.
	checkTripOnRoute := func(tripColumn string, trip *ScheduledTrip, route *Route) {
		if trip != nil && route != nil && trip.Route.Id != route.Id {
			w = append(w, warnings.NewStaticWarning(csv, warnings.TransferTripNotOnRoute{
				TripColumn: tripColumn,
				TripID:     trip.ID,
				RouteID:    route.Id,
			}))
		}
	}
	// checkStopType warns if a transfer names a stop whose location_type GTFS
	// does not allow: only a stop or a station, and only a stop for linked
	// trips, which run on one vehicle.
	checkStopType := func(column string, stop *Stop, linkedTrips bool) {
		switch {
		case stop == nil, stop.Type == StopType_Stop:
		case linkedTrips:
			w = append(w, warnings.NewStaticWarning(csv, warnings.TransferStopNotAllowed{Column: column, StopID: stop.Id, Allowed: "a stop (location_type 0)"}))
		case stop.Type != StopType_Station:
			w = append(w, warnings.NewStaticWarning(csv, warnings.TransferStopNotAllowed{Column: column, StopID: stop.Id, Allowed: "a stop or station (location_type 0 or 1)"}))
		}
	}
	for csv.NextRow() {
		rawType := typeColumn.Read()
		transferType, ok := parseTransferType(rawType)
		if !ok {
			w = append(w, warnings.NewStaticWarning(csv, warnings.TransferInvalidType{Value: rawType}))
			continue
		}
		linkedTrips := transferType == TransferType_InSeat || transferType == TransferType_InSeatNotAllowed

		fromStopID := fromStopIDColumn.Read()
		toStopID := toStopIDColumn.Read()
		fromRouteID := fromRouteIDColumn.Read()
		toRouteID := toRouteIDColumn.Read()
		fromTripID := fromTripIDColumn.Read()
		toTripID := toTripIDColumn.Read()
		fromRoute := routeIdToRoute[fromRouteID]
		toRoute := routeIdToRoute[toRouteID]
		fromTrip := tripIdToTrip[fromTripID]
		toTrip := tripIdToTrip[toTripID]
		// An unknown route or trip cannot be treated as unset: that would turn
		// the rule into a less specific one that applies to more trips.
		warningsBefore := len(w)
		for _, ref := range []struct {
			column   string
			id       string
			known    bool
			required bool
		}{
			{"from_route_id", fromRouteID, fromRoute != nil, false},
			{"to_route_id", toRouteID, toRoute != nil, false},
			{"from_trip_id", fromTripID, fromTrip != nil, linkedTrips},
			{"to_trip_id", toTripID, toTrip != nil, linkedTrips},
		} {
			switch {
			case ref.id == "":
				if ref.required {
					w = append(w, warnings.NewStaticWarning(csv, warnings.TransferMissingTrip{Column: ref.column}))
				}
			case !ref.known:
				w = append(w, warnings.NewStaticWarning(csv, warnings.TransferUnknownReference{Column: ref.column, ID: ref.id}))
			}
		}
		if len(w) > warningsBefore {
			continue
		}
		checkTripOnRoute("from_trip_id", fromTrip, fromRoute)
		checkTripOnRoute("to_trip_id", toTrip, toRoute)
		if len(w) > warningsBefore {
			continue
		}

		if !linkedTrips {
			if len(missingStopColumns) > 0 {
				if !reportedMissingStopColumns {
					w = append(w, missingColumnsWarning(csv, missingStopColumns)...)
					reportedMissingStopColumns = true
				}
				continue
			}
			for _, ref := range []struct{ column, id string }{
				{"from_stop_id", fromStopID},
				{"to_stop_id", toStopID},
			} {
				if ref.id == "" {
					w = append(w, warnings.NewStaticWarning(csv, warnings.TransferMissingStop{Column: ref.column}))
				}
			}
			if len(w) > warningsBefore {
				continue
			}
		}
		fromStop, ok := resolveStop("from_stop_id", fromStopID)
		if !ok {
			continue
		}
		toStop, ok := resolveStop("to_stop_id", toStopID)
		if !ok {
			continue
		}
		checkStopType("from_stop_id", fromStop, linkedTrips)
		checkStopType("to_stop_id", toStop, linkedTrips)
		if len(w) > warningsBefore {
			continue
		}
		transfers = append(transfers, Transfer{
			From:            fromStop,
			To:              toStop,
			Type:            transferType,
			MinTransferTime: parseInt32(transferTimeColumn.Read()),
			FromRoute:       fromRoute,
			ToRoute:         toRoute,
			FromTrip:        fromTrip,
			ToTrip:          toTrip,
		})
	}
	return transfers, w
}

// parseFiniteFloat64 is parseFloat64 that also rejects NaN and infinities,
// which strconv accepts but GTFS float fields do not allow.
func parseFiniteFloat64(s string) *float64 {
	f := parseFloat64(s)
	if f == nil || math.IsNaN(*f) || math.IsInf(*f, 0) {
		return nil
	}
	return f
}

func parseInt32(s string) *int32 {
	if s == "" {
		return nil
	}
	i, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return nil
	}
	i32 := int32(i)
	return &i32
}

func parseCalendar(f *csv.File, m map[string]Service, timezone *time.Location) {
	startDateColumn := f.RequiredColumn("start_date")
	endDateColumn := f.RequiredColumn("end_date")
	serviceIDColumn := f.RequiredColumn("service_id")
	var dayColumns [7]csv.RequiredColumn
	for i, days := range []string{
		"monday",
		"tuesday",
		"wednesday",
		"thursday",
		"friday",
		"saturday",
		"sunday",
	} {
		dayColumns[i] = f.RequiredColumn(days)
	}

	if err := f.MissingRequiredColumns(); err != nil {
		fmt.Println(err)
		return
	}

	parseBool := func(s string) bool {
		return s == "1"
	}
	for f.NextRow() {
		startDate, err := parseTime(startDateColumn.Read(), timezone)
		if err != nil {
			continue
		}
		endDate, err := parseTime(endDateColumn.Read(), timezone)
		if err != nil {
			continue
		}
		service := Service{
			Id:        serviceIDColumn.Read(),
			Monday:    parseBool(dayColumns[0].Read()),
			Tuesday:   parseBool(dayColumns[1].Read()),
			Wednesday: parseBool(dayColumns[2].Read()),
			Thursday:  parseBool(dayColumns[3].Read()),
			Friday:    parseBool(dayColumns[4].Read()),
			Saturday:  parseBool(dayColumns[5].Read()),
			Sunday:    parseBool(dayColumns[6].Read()),
			StartDate: startDate,
			EndDate:   endDate,
		}
		if missingKeys := f.MissingRowKeys(); len(missingKeys) > 0 {
			log.Printf("Skipping calendar because of missing keys %s", missingKeys)
			continue
		}
		m[service.Id] = service
	}
}

func parseCalendarDates(csv *csv.File, m map[string]Service, timezone *time.Location) {
	serviceIDColumn := csv.RequiredColumn("service_id")
	dateColumn := csv.RequiredColumn("date")
	exceptionTypeColumn := csv.RequiredColumn("exception_type")

	if err := csv.MissingRequiredColumns(); err != nil {
		fmt.Println(err)
		return
	}

	for csv.NextRow() {
		serviceId := serviceIDColumn.Read()
		date, err := parseTime(dateColumn.Read(), timezone)
		if err != nil {
			continue
		}
		exceptionType := exceptionTypeColumn.Read()
		if missingKeys := csv.MissingRowKeys(); len(missingKeys) > 0 {
			log.Printf("Skipping calendar because of missing keys %s", missingKeys)
			continue
		}
		service, ok := m[serviceId]
		service.Id = serviceId
		if !ok {
			service.StartDate = date
			service.EndDate = date
		} else {
			if date.Before(service.StartDate) {
				service.StartDate = date
			}
			if service.EndDate.Before(date) {
				service.EndDate = date
			}
		}
		switch exceptionType {
		case "1":
			service.AddedDates = append(service.AddedDates, date)
		case "2":
			service.RemovedDates = append(service.RemovedDates, date)
		default:
			continue
		}
		m[service.Id] = service
	}
}

func parseTime(s string, timezone *time.Location) (time.Time, error) {
	return time.ParseInLocation("20060102", s, timezone)
}

func parseScheduledTrips(csv *csv.File, routeIdToRoute map[string]*Route, services []Service, shapeIDToShape map[string]*Shape) []ScheduledTrip {
	routeIDColumn := csv.RequiredColumn("route_id")
	serviceIDColumn := csv.RequiredColumn("service_id")
	tripIDColumn := csv.RequiredColumn("trip_id")
	tripHeadsignColumn := csv.OptionalColumn("trip_headsign")
	tripShortNameColumn := csv.OptionalColumn("trip_short_name")
	directionIDColumn := csv.OptionalColumn("direction_id")
	blockIDColumn := csv.OptionalColumn("block_id")
	wheelchairAccessibleColumn := csv.OptionalColumn("wheelchair_accessible")
	bikesAllowedColumn := csv.OptionalColumn("bikes_allowed")
	shapeIDColumn := csv.OptionalColumn("shape_id")
	safeDurationFactorColumn := csv.OptionalColumn("safe_duration_factor")
	safeDurationOffsetColumn := csv.OptionalColumn("safe_duration_offset")

	if err := csv.MissingRequiredColumns(); err != nil {
		fmt.Println(err)
		return nil
	}

	idToService := servicesByID(services)
	var trips []ScheduledTrip
	for csv.NextRow() {
		trip := ScheduledTrip{
			Route:                routeIdToRoute[routeIDColumn.Read()],
			Service:              idToService[serviceIDColumn.Read()],
			ID:                   tripIDColumn.Read(),
			Headsign:             tripHeadsignColumn.Read(),
			ShortName:            tripShortNameColumn.Read(),
			DirectionId:          parseDirectionID_GTFSStatic(directionIDColumn.ReadOr("")),
			BlockID:              blockIDColumn.Read(),
			WheelchairAccessible: parseWheelchairBoarding(wheelchairAccessibleColumn.Read()),
			BikesAllowed:         parseBikesAllowed(bikesAllowedColumn.ReadOr("")),
			SafeDurationFactor:   parseFloat64(safeDurationFactorColumn.Read()),
			SafeDurationOffset:   parseFloat64(safeDurationOffsetColumn.Read()),
		}

		shapeIDOrNil := shapeIDColumn.Read()
		if shapeIDOrNil != "" && shapeIDToShape != nil {
			if shape, ok := shapeIDToShape[shapeIDOrNil]; ok {
				trip.Shape = shape
			} else {
				log.Printf("Shape %s not found for trip %s", shapeIDOrNil, trip.ID)
			}
		}

		if missingKeys := csv.MissingRowKeys(); len(missingKeys) > 0 {
			log.Printf("Skipping trip because of missing keys %s", missingKeys)
			continue
		}
		if trip.Route == nil {
			log.Print("Skipping trip because of missing route")
			continue
		}
		if trip.Service == nil {
			log.Print("Skipping trip because of missing service")
			continue
		}
		trips = append(trips, trip)
	}
	return trips
}

// stopTimeReferences resolves the ids a stop_times.txt row may point at.
type stopTimeReferences struct {
	tripIndexes  map[string]int
	stops        map[string]*Stop
	locations    map[string]*Location
	groups       map[string]*LocationGroup
	bookingRules map[string]*BookingRule
}

func newStopTimeReferences(static *Static, stopIdToStop map[string]*Stop) stopTimeReferences {
	refs := stopTimeReferences{
		tripIndexes:  tripIndexByID(static.Trips),
		stops:        stopIdToStop,
		locations:    map[string]*Location{},
		groups:       map[string]*LocationGroup{},
		bookingRules: map[string]*BookingRule{},
	}
	for i := range static.Locations {
		refs.locations[static.Locations[i].Id] = &static.Locations[i]
	}
	for i := range static.LocationGroups {
		refs.groups[static.LocationGroups[i].Id] = &static.LocationGroups[i]
	}
	for i := range static.BookingRules {
		refs.bookingRules[static.BookingRules[i].Id] = &static.BookingRules[i]
	}
	return refs
}

// stopTimePlace is the exactly-one-of stop / location / location group that a
// stop_times.txt row serves.
type stopTimePlace struct {
	stop     *Stop
	location *Location
	group    *LocationGroup
}

// resolvePlace enforces GTFS's exactly-one-of rule for stop_id, location_id
// and location_group_id and resolves the one id given. A non-empty reason
// means the row must be skipped.
func (refs stopTimeReferences) resolvePlace(stopID, locationID, groupID string) (stopTimePlace, string) {
	referenced := 0
	for _, id := range []string{stopID, locationID, groupID} {
		if id != "" {
			referenced++
		}
	}
	if referenced != 1 {
		return stopTimePlace{}, fmt.Sprintf(
			"row references %d of stop_id, location_id and location_group_id; exactly one is required", referenced)
	}
	var place stopTimePlace
	switch {
	case stopID != "":
		place.stop = refs.stops[stopID]
		if place.stop == nil {
			return stopTimePlace{}, fmt.Sprintf("unknown stop_id %q", stopID)
		}
	case locationID != "":
		place.location = refs.locations[locationID]
		if place.location == nil {
			return stopTimePlace{}, fmt.Sprintf("unknown location_id %q", locationID)
		}
	default:
		place.group = refs.groups[groupID]
		if place.group == nil {
			return stopTimePlace{}, fmt.Sprintf("unknown location_group_id %q", groupID)
		}
	}
	return place, ""
}

// resolveBookingRule resolves an optional booking rule id. A non-empty reason
// means the id was given but does not exist.
func (refs stopTimeReferences) resolveBookingRule(column, id string) (*BookingRule, string) {
	if id == "" {
		return nil, ""
	}
	rule := refs.bookingRules[id]
	if rule == nil {
		return nil, fmt.Sprintf("unknown %s %q", column, id)
	}
	return rule, ""
}

// parsePickupDropOffWindow parses the two window cells of a row. Both empty
// means "not windowed" (nil pointers, empty reason). Any other invalid
// combination, including an end before the start, returns the reason the row
// must be skipped. A zero-length window (start == end) is allowed.
func parsePickupDropOffWindow(startRaw, endRaw string) (start, end *time.Duration, reason string) {
	if startRaw == "" && endRaw == "" {
		return nil, nil, ""
	}
	if startRaw == "" || endRaw == "" {
		return nil, nil, "start_pickup_drop_off_window and end_pickup_drop_off_window must be set together"
	}
	startWindow, startOk := parseGtfsTimeToDuration(startRaw)
	endWindow, endOk := parseGtfsTimeToDuration(endRaw)
	if !startOk || !endOk {
		return nil, nil, fmt.Sprintf("unparsable pickup/drop-off window %q-%q", startRaw, endRaw)
	}
	if endWindow < startWindow {
		return nil, nil, fmt.Sprintf("pickup/drop-off window %q-%q ends before it starts", startRaw, endRaw)
	}
	return &startWindow, &endWindow, ""
}

// checkWindowUsage applies the GTFS presence rules that tie windows to the
// kind of place a row references and to arrival/departure times.
func checkWindowUsage(windowed bool, place stopTimePlace, arrivalRaw, departureRaw string) string {
	if windowed && (arrivalRaw != "" || departureRaw != "") {
		return "arrival_time/departure_time are forbidden on a row with a pickup/drop-off window"
	}
	if !windowed && place.stop == nil {
		return "location_id and location_group_id rows require start/end_pickup_drop_off_window"
	}
	return ""
}

// parseArrivalDeparture parses the two time cells, copying whichever is
// present into the one that is missing.
func parseArrivalDeparture(arrivalRaw, departureRaw string) (arrival, departure time.Duration) {
	arrival, arrivalOk := parseGtfsTimeToDuration(arrivalRaw)
	departure, departureOk := parseGtfsTimeToDuration(departureRaw)
	if !departureOk {
		departure = arrival
	}
	if !arrivalOk {
		arrival = departure
	}
	return arrival, departure
}

func parseScheduledStopTimes(csv *csv.File, static *Static, stopIdToStop map[string]*Stop) []warnings.StaticWarning {
	reader, missing := newStopTimeReader(csv, newStopTimeReferences(static, stopIdToStop))
	if missing != nil {
		return missingColumnsWarning(csv, missing)
	}
	stopTimesByTrip := readTripRows(csv, reader.tripIDColumn, len(static.Trips), reader.refs.tripIndexes, reader.read)
	for i, stopTimes := range stopTimesByTrip {
		if len(stopTimes) == 0 {
			continue
		}
		static.Trips[i].StopTimes = finishTripStopTimes(stopTimes, reader.hasShapeDistances)
	}
	return reader.warnings
}

// stopTimeReader builds a ScheduledStopTime from each row of stop_times.txt.
type stopTimeReader struct {
	file                        *csv.File
	tripIDColumn                csv.RequiredColumn
	stopSequenceColumn          csv.RequiredColumn
	stopIDColumn                csv.OptionalColumn
	locationIDColumn            csv.OptionalColumn
	locationGroupIDColumn       csv.OptionalColumn
	arrivalTimeColumn           csv.OptionalColumn
	departureTimeColumn         csv.OptionalColumn
	startWindowColumn           csv.OptionalColumn
	endWindowColumn             csv.OptionalColumn
	stopHeadsignColumn          csv.OptionalColumn
	pickupTypeColumn            csv.OptionalColumn
	dropOffTypeColumn           csv.OptionalColumn
	continuousPickupColumn      csv.OptionalColumn
	continuousDropOffColumn     csv.OptionalColumn
	shapeDistanceTraveledColumn csv.OptionalColumn
	timepointColumn             csv.OptionalColumn
	pickupBookingRuleIDColumn   csv.OptionalColumn
	dropOffBookingRuleIDColumn  csv.OptionalColumn
	safeDurationFactorColumn    csv.OptionalColumn
	safeDurationOffsetColumn    csv.OptionalColumn

	refs      stopTimeReferences
	headsigns stringInterner

	// hasShapeDistances is set once any row read so far has a
	// shape_dist_traveled value. It picks how a trip's missing times are
	// interpolated, for the whole feed.
	hasShapeDistances bool
	// warnings holds a warning for each row read so far that breaks a
	// GTFS-Flex rule, unless discardWarnings is set.
	warnings []warnings.StaticWarning
	// discardWarnings is set by readers whose warnings were already
	// reported by an earlier pass over the same file.
	discardWarnings bool
}

// newStopTimeReader returns nil and the names of any missing required columns.
func newStopTimeReader(f *csv.File, refs stopTimeReferences) (*stopTimeReader, []string) {
	reader := &stopTimeReader{
		file:                        f,
		tripIDColumn:                f.RequiredColumn("trip_id"),
		stopSequenceColumn:          f.RequiredColumn("stop_sequence"),
		stopIDColumn:                f.OptionalColumn("stop_id"),
		locationIDColumn:            f.OptionalColumn("location_id"),
		locationGroupIDColumn:       f.OptionalColumn("location_group_id"),
		arrivalTimeColumn:           f.OptionalColumn("arrival_time"),
		departureTimeColumn:         f.OptionalColumn("departure_time"),
		startWindowColumn:           f.OptionalColumn("start_pickup_drop_off_window"),
		endWindowColumn:             f.OptionalColumn("end_pickup_drop_off_window"),
		stopHeadsignColumn:          f.OptionalColumn("stop_headsign"),
		pickupTypeColumn:            f.OptionalColumn("pickup_type"),
		dropOffTypeColumn:           f.OptionalColumn("drop_off_type"),
		continuousPickupColumn:      f.OptionalColumn("continuous_pickup"),
		continuousDropOffColumn:     f.OptionalColumn("continuous_drop_off"),
		shapeDistanceTraveledColumn: f.OptionalColumn("shape_dist_traveled"),
		timepointColumn:             f.OptionalColumn("timepoint"),
		pickupBookingRuleIDColumn:   f.OptionalColumn("pickup_booking_rule_id"),
		dropOffBookingRuleIDColumn:  f.OptionalColumn("drop_off_booking_rule_id"),
		safeDurationFactorColumn:    f.OptionalColumn("safe_duration_factor"),
		safeDurationOffsetColumn:    f.OptionalColumn("safe_duration_offset"),
	}
	if missing := f.MissingRequiredColumns(); missing != nil {
		return nil, missing
	}
	// Each of the three place columns is optional, but a file with none of
	// them can't place any row, so report stop_id as missing rather than
	// warning on every row.
	if !reader.stopIDColumn.Exists() && !reader.locationIDColumn.Exists() && !reader.locationGroupIDColumn.Exists() {
		return nil, []string{"stop_id"}
	}
	reader.refs = refs
	reader.headsigns = stringInterner{}
	return reader, nil
}

// read builds the stop time in the current row. It reports false for a row
// with an invalid stop sequence and, with a warning, for a row that breaks a
// GTFS-Flex rule.
func (r *stopTimeReader) read() (ScheduledStopTime, bool) {
	rawStopSequence := r.stopSequenceColumn.Read()
	stopSequence, err := strconv.Atoi(rawStopSequence)
	if err != nil {
		log.Printf("Skipping stop time because stop_sequence %q is not an integer", rawStopSequence)
		return ScheduledStopTime{}, false
	}

	place, reason := r.refs.resolvePlace(r.stopIDColumn.Read(), r.locationIDColumn.Read(), r.locationGroupIDColumn.Read())
	if reason != "" {
		r.warn(warnings.StopTimeInvalidReference{Reason: reason})
		return ScheduledStopTime{}, false
	}
	startWindow, endWindow, reason := parsePickupDropOffWindow(r.startWindowColumn.Read(), r.endWindowColumn.Read())
	if reason != "" {
		r.warn(warnings.StopTimeInvalidWindow{Reason: reason})
		return ScheduledStopTime{}, false
	}
	windowed := startWindow != nil
	arrivalRaw := r.arrivalTimeColumn.Read()
	departureRaw := r.departureTimeColumn.Read()
	if reason := checkWindowUsage(windowed, place, arrivalRaw, departureRaw); reason != "" {
		r.warn(warnings.StopTimeInvalidWindow{Reason: reason})
		return ScheduledStopTime{}, false
	}
	// A booking rule only adds rider information, so an unresolvable id
	// keeps the row (with no rule) rather than dropping the service.
	pickupRule, reason := r.refs.resolveBookingRule("pickup_booking_rule_id", r.pickupBookingRuleIDColumn.Read())
	if reason != "" {
		r.warn(warnings.StopTimeInvalidReference{Reason: reason})
	}
	dropOffRule, reason := r.refs.resolveBookingRule("drop_off_booking_rule_id", r.dropOffBookingRuleIDColumn.Read())
	if reason != "" {
		r.warn(warnings.StopTimeInvalidReference{Reason: reason})
	}

	arrival, departure := parseArrivalDeparture(arrivalRaw, departureRaw)
	if r.shapeDistanceTraveledColumn.Read() != "" {
		r.hasShapeDistances = true
	}
	stopTime := ScheduledStopTime{
		Stop:                  place.stop,
		Headsign:              r.headsigns.intern(r.stopHeadsignColumn.Read()),
		ArrivalTime:           arrival,
		DepartureTime:         departure,
		StopSequence:          stopSequence,
		PickupType:            parsePickupDropOffPolicyOrYes(r.pickupTypeColumn.Read()),
		DropOffType:           parsePickupDropOffPolicyOrYes(r.dropOffTypeColumn.Read()),
		ContinuousPickup:      parsePickupDropOffPolicy(r.continuousPickupColumn.ReadOr("")),
		ContinuousDropOff:     parsePickupDropOffPolicy(r.continuousDropOffColumn.ReadOr("")),
		ShapeDistanceTraveled: parseFloat64(r.shapeDistanceTraveledColumn.Read()),
		// A windowed record has no exact time whatever timepoint says.
		ExactTimes: !windowed && r.timepointColumn.ReadOr("1") != "0",
		Flex: newFlexStopTime(FlexStopTime{
			Location:                 place.location,
			LocationGroup:            place.group,
			StartPickupDropOffWindow: startWindow,
			EndPickupDropOffWindow:   endWindow,
			PickupBookingRule:        pickupRule,
			DropOffBookingRule:       dropOffRule,
			SafeDurationFactor:       parseFloat64(r.safeDurationFactorColumn.Read()),
			SafeDurationOffset:       parseFloat64(r.safeDurationOffsetColumn.Read()),
		}),
	}
	return stopTime, true
}

func (r *stopTimeReader) warn(kind warnings.StaticWarningKind) {
	if r.discardWarnings {
		return
	}
	r.warnings = append(r.warnings, warnings.NewStaticWarning(r.file, kind))
}

// finishTripStopTimes orders one trip's stop times by stop_sequence and fills
// in missing times. hasShapeDistances must be stopTimeReader's value after it
// has read every row of the file, because it decides how times are
// interpolated for the whole feed.
func finishTripStopTimes(stopTimes []ScheduledStopTime, hasShapeDistances bool) []ScheduledStopTime {
	sort.Slice(stopTimes, func(a, b int) bool {
		return stopTimes[a].StopSequence < stopTimes[b].StopSequence
	})
	return interpolateTimedStopTimes(stopTimes, hasShapeDistances)
}

type stopVisit struct {
	sequence int
	stop     *Stop
}

// parseTripStops is the StopTimesStopsOnly counterpart of
// parseScheduledStopTimes: it keeps only each trip's sequence of stops, so
// rows without a known stop (including every GTFS-Flex location or location
// group row) are dropped. stop_id is optional because a feed with only Flex
// service may leave it out.
func parseTripStops(csv *csv.File, stopIdToStop map[string]*Stop, trips []ScheduledTrip) {
	stopIDColumn := csv.OptionalColumn("stop_id")
	stopSequenceColumn := csv.RequiredColumn("stop_sequence")
	tripIDColumn := csv.RequiredColumn("trip_id")
	if err := csv.MissingRequiredColumns(); err != nil {
		fmt.Println(err)
		return
	}

	visitsByTrip := readTripRows(csv, tripIDColumn, len(trips), tripIndexByID(trips), func() (stopVisit, bool) {
		stopSequence, err := strconv.Atoi(stopSequenceColumn.Read())
		if err != nil {
			return stopVisit{}, false
		}
		stop := stopIdToStop[stopIDColumn.Read()]
		return stopVisit{sequence: stopSequence, stop: stop}, stop != nil
	})
	for i, visits := range visitsByTrip {
		if len(visits) == 0 {
			continue
		}
		sort.Slice(visits, func(a, b int) bool {
			return visits[a].sequence < visits[b].sequence
		})
		tripStops := make([]*Stop, len(visits))
		for j := range visits {
			tripStops[j] = visits[j].stop
		}
		trips[i].Stops = tripStops
		visitsByTrip[i] = nil
	}
}

// agenciesByID maps each agency ID to its first agency, so a repeated
// agency_id resolves the same way for every file that references it.
func agenciesByID(agencies []Agency) map[string]*Agency {
	idToAgency := make(map[string]*Agency, len(agencies))
	for i := range agencies {
		if _, ok := idToAgency[agencies[i].Id]; !ok {
			idToAgency[agencies[i].Id] = &agencies[i]
		}
	}
	return idToAgency
}

func faresByID(fares []FareAttribute) map[string]*FareAttribute {
	idToFare := make(map[string]*FareAttribute, len(fares))
	for i := range fares {
		idToFare[fares[i].Id] = &fares[i]
	}
	return idToFare
}

// idSet returns the set of the items' IDs, for references to IDs that several
// items may share, such as a fare_product_id.
func idSet[T any](items []T, id func(T) string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, item := range items {
		set[id(item)] = true
	}
	return set
}

func servicesByID(services []Service) map[string]*Service {
	idToService := make(map[string]*Service, len(services))
	for i := range services {
		idToService[services[i].Id] = &services[i]
	}
	return idToService
}

func riderCategoriesByID(categories []RiderCategory) map[string]*RiderCategory {
	idToCategory := make(map[string]*RiderCategory, len(categories))
	for i := range categories {
		idToCategory[categories[i].Id] = &categories[i]
	}
	return idToCategory
}

func fareMediaByID(media []FareMedia) map[string]*FareMedia {
	idToMedia := make(map[string]*FareMedia, len(media))
	for i := range media {
		idToMedia[media[i].Id] = &media[i]
	}
	return idToMedia
}

func areasByID(areas []Area) map[string]*Area {
	idToArea := make(map[string]*Area, len(areas))
	for i := range areas {
		idToArea[areas[i].Id] = &areas[i]
	}
	return idToArea
}

func networksByID(networks []Network) map[string]*Network {
	idToNetwork := make(map[string]*Network, len(networks))
	for i := range networks {
		idToNetwork[networks[i].Id] = &networks[i]
	}
	return idToNetwork
}

func levelsByID(levels []Level) map[string]*Level {
	idToLevel := make(map[string]*Level, len(levels))
	for i := range levels {
		idToLevel[levels[i].Id] = &levels[i]
	}
	return idToLevel
}

func routesByID(routes []Route) map[string]*Route {
	idToRoute := make(map[string]*Route, len(routes))
	for i := range routes {
		idToRoute[routes[i].Id] = &routes[i]
	}
	return idToRoute
}

func stopsByID(stops []Stop) map[string]*Stop {
	idToStop := make(map[string]*Stop, len(stops))
	for i := range stops {
		idToStop[stops[i].Id] = &stops[i]
	}
	return idToStop
}

// readTripRows groups the rows eachTripRow accepts by trip, indexed by trip
// index. idToTripIndex must map trip IDs to indexes below tripCount.
//
// Each trip's rows are buffered while they are contiguous among the accepted
// rows and then copied into a slice of exactly that size, so the result
// carries no slack capacity. A trip whose rows resume after another trip's
// grows by append instead, and is trimmed to size once the file is read.
func readTripRows[T any](f *csv.File, tripIDColumn csv.RequiredColumn, tripCount int, idToTripIndex map[string]int, readRow func() (T, bool)) [][]T {
	rowsByTrip := make([][]T, tripCount)
	resumed := map[int]bool{}
	var pending []T
	currentTrip := -1
	flush := func() {
		if currentTrip < 0 || len(pending) == 0 {
			return
		}
		if rowsByTrip[currentTrip] == nil {
			rowsByTrip[currentTrip] = make([]T, len(pending))
			copy(rowsByTrip[currentTrip], pending)
		} else {
			rowsByTrip[currentTrip] = append(rowsByTrip[currentTrip], pending...)
			resumed[currentTrip] = true
		}
		pending = pending[:0]
	}
	eachTripRow(f, tripIDColumn, idToTripIndex, readRow, func(tripIndex int, row T) bool {
		if tripIndex != currentTrip {
			flush()
			currentTrip = tripIndex
		}
		pending = append(pending, row)
		return true
	})
	flush()
	for i := range resumed {
		rowsByTrip[i] = append([]T(nil), rowsByTrip[i]...)
	}
	return rowsByTrip
}

// eachTripRow reads every row of a stop_times-shaped file and passes the trip
// index and the value readRow builds to accept, in file order, until accept
// returns false. Rows for which readRow returns false, rows with missing
// required values, and rows for unknown trips are dropped.
func eachTripRow[T any](f *csv.File, tripIDColumn csv.RequiredColumn, idToTripIndex map[string]int, readRow func() (T, bool), accept func(tripIndex int, row T) bool) {
	// Rows of one trip are usually contiguous, so the trip of the previous row
	// saves a map lookup on most rows.
	var tripID string
	tripIndex, known := 0, false
	for f.NextRow() {
		if id := tripIDColumn.Read(); id != tripID {
			tripID = id
			tripIndex, known = idToTripIndex[id]
		}
		// readRow runs only for rows of known trips, so it never warns about
		// a row that would be dropped anyway.
		if !known {
			continue
		}
		row, ok := readRow()
		if !ok {
			continue
		}
		if missingKeys := f.MissingRowKeys(); len(missingKeys) > 0 {
			log.Printf("Skipping stop time because of missing keys %s", missingKeys)
			continue
		}
		if !accept(tripIndex, row) {
			return
		}
	}
}

// tripIndexByID maps each trip ID to its index in trips. When an ID repeats,
// the last trip with it wins.
func tripIndexByID(trips []ScheduledTrip) map[string]int {
	idToTripIndex := make(map[string]int, len(trips))
	for i := range trips {
		idToTripIndex[trips[i].ID] = i
	}
	return idToTripIndex
}

// stringInterner deduplicates strings read from CSV rows. Beyond saving
// memory on repeated values, it copies each value out of its row: a string
// sliced from a row keeps the whole row alive.
type stringInterner map[string]string

func (m stringInterner) intern(s string) string {
	if s == "" {
		return ""
	}
	if v, ok := m[s]; ok {
		return v
	}
	s = strings.Clone(s)
	m[s] = s
	return s
}

// newFlexStopTime returns a copy of flex on the heap, or nil when it sets no
// field. Copying into new storage, rather than returning &flex, keeps rows
// without Flex columns from allocating.
func newFlexStopTime(flex FlexStopTime) *FlexStopTime {
	if flex == (FlexStopTime{}) {
		return nil
	}
	heapCopy := new(FlexStopTime)
	*heapCopy = flex
	return heapCopy
}

func parseGtfsTimeToDuration(s string) (time.Duration, bool) {
	if s == "" {
		return 0, false
	}
	var pieces [3]int
	var i int
	for _, c := range s {
		if '0' <= c && c <= '9' {
			pieces[i] = 10*pieces[i] + int(c-'0')
		} else if c == ':' {
			i++
			if i > 2 {
				return 0, false
			}
		} else if unicode.IsSpace(c) {
			continue
		} else {
			return 0, false
		}
	}
	hours := pieces[0]
	minutes := pieces[1]
	seconds := pieces[2]
	return time.Duration((hours*60+minutes)*60+seconds) * time.Second, true
}

// parseOptionalGtfsTime parses an optional HH:MM:SS cell; nil when the cell is
// empty or unparsable.
func parseOptionalGtfsTime(s string) *time.Duration {
	d, ok := parseGtfsTimeToDuration(s)
	if !ok {
		return nil
	}
	return &d
}

func parseBookingRules(csv *csv.File) ([]BookingRule, []warnings.StaticWarning) {
	idColumn := csv.RequiredColumn("booking_rule_id")
	typeColumn := csv.RequiredColumn("booking_type")
	durationMinColumn := csv.OptionalColumn("prior_notice_duration_min")
	durationMaxColumn := csv.OptionalColumn("prior_notice_duration_max")
	lastDayColumn := csv.OptionalColumn("prior_notice_last_day")
	lastTimeColumn := csv.OptionalColumn("prior_notice_last_time")
	startDayColumn := csv.OptionalColumn("prior_notice_start_day")
	startTimeColumn := csv.OptionalColumn("prior_notice_start_time")
	serviceIDColumn := csv.OptionalColumn("prior_notice_service_id")
	messageColumn := csv.OptionalColumn("message")
	pickupMessageColumn := csv.OptionalColumn("pickup_message")
	dropOffMessageColumn := csv.OptionalColumn("drop_off_message")
	phoneNumberColumn := csv.OptionalColumn("phone_number")
	infoUrlColumn := csv.OptionalColumn("info_url")
	bookingUrlColumn := csv.OptionalColumn("booking_url")

	if w := checkForMissingColumns(csv); len(w) > 0 {
		return nil, w
	}

	var w []warnings.StaticWarning
	var rules []BookingRule
	for csv.NextRow() {
		id := idColumn.Read()
		rawType := typeColumn.Read()
		if missingKeys := csv.MissingRowKeys(); len(missingKeys) > 0 {
			w = append(w, warnings.NewStaticWarning(csv, warnings.BookingRuleInvalid{
				BookingRuleID: id,
				Reason:        fmt.Sprintf("missing values %s", missingKeys),
			}))
			continue
		}
		bookingType, ok := parseBookingType(rawType)
		if !ok {
			w = append(w, warnings.NewStaticWarning(csv, warnings.BookingRuleInvalid{
				BookingRuleID: id,
				Reason:        fmt.Sprintf("unparsable booking_type %q", rawType),
			}))
			continue
		}
		rules = append(rules, BookingRule{
			Id:                     id,
			Type:                   bookingType,
			PriorNoticeDurationMin: parseInt32(durationMinColumn.Read()),
			PriorNoticeDurationMax: parseInt32(durationMaxColumn.Read()),
			PriorNoticeLastDay:     parseInt32(lastDayColumn.Read()),
			PriorNoticeLastTime:    parseOptionalGtfsTime(lastTimeColumn.Read()),
			PriorNoticeStartDay:    parseInt32(startDayColumn.Read()),
			PriorNoticeStartTime:   parseOptionalGtfsTime(startTimeColumn.Read()),
			PriorNoticeServiceId:   serviceIDColumn.Read(),
			Message:                messageColumn.Read(),
			PickupMessage:          pickupMessageColumn.Read(),
			DropOffMessage:         dropOffMessageColumn.Read(),
			PhoneNumber:            phoneNumberColumn.Read(),
			InfoUrl:                infoUrlColumn.Read(),
			BookingUrl:             bookingUrlColumn.Read(),
		})
	}
	return rules, w
}

type ShapeRow struct {
	ShapePtLat        float64
	ShapePtLon        float64
	ShapePtSequence   int32
	ShapeDistTraveled *float64
}

func parseShapes(csv *csv.File) []Shape {
	reader, missing := newShapeReader(csv)
	if missing != nil {
		fmt.Println(missing)
		return nil
	}

	shapeIDToRowData := map[string][]ShapeRow{}
	reader.eachRow(func(shapeID string, row ShapeRow) bool {
		shapeIDToRowData[shapeID] = append(shapeIDToRowData[shapeID], row)
		return true
	})

	shapes := make([]Shape, 0, len(shapeIDToRowData))
	for shapeID, rows := range shapeIDToRowData {
		shapes = append(shapes, finishShape(shapeID, rows))
	}

	// Sort the shapes by ID
	sort.Slice(shapes, func(i, j int) bool {
		return shapes[i].ID < shapes[j].ID
	})

	return shapes
}

// shapeReader reads the rows of shapes.txt.
type shapeReader struct {
	file                    *csv.File
	shapeIDColumn           csv.RequiredColumn
	shapePtLatColumn        csv.RequiredColumn
	shapePtLonColumn        csv.RequiredColumn
	shapePtSequenceColumn   csv.RequiredColumn
	shapeDistTraveledColumn csv.OptionalColumn
}

// newShapeReader returns nil and the names of any missing required columns.
func newShapeReader(f *csv.File) (*shapeReader, []string) {
	reader := &shapeReader{
		file:                    f,
		shapeIDColumn:           f.RequiredColumn("shape_id"),
		shapePtLatColumn:        f.RequiredColumn("shape_pt_lat"),
		shapePtLonColumn:        f.RequiredColumn("shape_pt_lon"),
		shapePtSequenceColumn:   f.RequiredColumn("shape_pt_sequence"),
		shapeDistTraveledColumn: f.OptionalColumn("shape_dist_traveled"),
	}
	if missing := f.MissingRequiredColumns(); missing != nil {
		return nil, missing
	}
	return reader, nil
}

// eachRow passes each row that has its required values to accept, in file
// order, until accept returns false.
func (r *shapeReader) eachRow(accept func(shapeID string, row ShapeRow) bool) {
	for r.file.NextRow() {
		shapeID := r.shapeIDColumn.Read()
		shapePtLat := parseFloat64(r.shapePtLatColumn.Read())
		shapePtLon := parseFloat64(r.shapePtLonColumn.Read())
		shapePtSequence := parseInt32(r.shapePtSequenceColumn.Read())
		shapeDistTraveled := parseFloat64(r.shapeDistTraveledColumn.Read())

		if missingKeys := r.file.MissingRowKeys(); len(missingKeys) > 0 {
			log.Printf("Skipping shape because of missing keys %s", missingKeys)
			continue
		}

		row := ShapeRow{
			ShapePtLat:        *shapePtLat,
			ShapePtLon:        *shapePtLon,
			ShapePtSequence:   *shapePtSequence,
			ShapeDistTraveled: shapeDistTraveled,
		}
		if !accept(shapeID, row) {
			return
		}
	}
}

// finishShape orders one shape's rows by shape_pt_sequence and builds the
// shape.
func finishShape(shapeID string, rows []ShapeRow) Shape {
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].ShapePtSequence < rows[j].ShapePtSequence
	})

	points := make([]ShapePoint, 0, len(rows))
	for _, row := range rows {
		points = append(points, ShapePoint{
			Latitude:  row.ShapePtLat,
			Longitude: row.ShapePtLon,
			Distance:  row.ShapeDistTraveled,
		})
	}
	return Shape{ID: shapeID, Points: points}
}

func parseFrequencies(csv *csv.File, tripIDToScheduledTrip map[string]*ScheduledTrip) {
	tripIDColumn := csv.RequiredColumn("trip_id")
	startTimeColumn := csv.RequiredColumn("start_time")
	endTimeColumn := csv.RequiredColumn("end_time")
	headwaySecsColumn := csv.RequiredColumn("headway_secs")
	exactTimesColumn := csv.OptionalColumn("exact_times")

	if err := csv.MissingRequiredColumns(); err != nil {
		fmt.Println(err)
		return
	}

	for csv.NextRow() {
		tripID := tripIDColumn.Read()
		startTime := startTimeColumn.Read()
		endTime := endTimeColumn.Read()
		headwaySecs := headwaySecsColumn.Read()

		if missingKeys := csv.MissingRowKeys(); len(missingKeys) > 0 {
			log.Printf("Skipping frequency because of missing keys %s", missingKeys)
			continue
		}
		scheduledTripOrNil := tripIDToScheduledTrip[tripID]
		if scheduledTripOrNil == nil {
			log.Printf("Skipping frequency because of missing trip %s", tripID)
			continue
		}
		headwaySecsOrNil := parseInt32(headwaySecs)
		if headwaySecsOrNil == nil {
			log.Print("Skipping frequency because of invalid headway_secs")
			continue
		}
		startTimeDuration, startTimeDurationOk := parseGtfsTimeToDuration(startTime)
		if !startTimeDurationOk {
			log.Print("Skipping frequency because of invalid start_time")
			continue
		}
		endTimeDuration, endTimeDurationOk := parseGtfsTimeToDuration(endTime)
		if !endTimeDurationOk {
			log.Print("Skipping frequency because of invalid end_time")
			continue
		}

		frequency := Frequency{
			StartTime:  startTimeDuration,
			EndTime:    endTimeDuration,
			Headway:    time.Duration(*headwaySecsOrNil) * time.Second,
			ExactTimes: parseExactTimes(exactTimesColumn.Read()),
		}

		scheduledTripOrNil.Frequencies = append(scheduledTripOrNil.Frequencies, frequency)
	}
}

func checkForMissingColumns(csv *csv.File) []warnings.StaticWarning {
	missing := csv.MissingRequiredColumns()
	if len(missing) == 0 {
		return nil
	}
	return missingColumnsWarning(csv, missing)
}

func missingColumnsWarning(csv *csv.File, missing []string) []warnings.StaticWarning {
	return []warnings.StaticWarning{
		warnings.NewStaticWarning(csv, warnings.MissingColumns{Columns: missing}),
	}
}
