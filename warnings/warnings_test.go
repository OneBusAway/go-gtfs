package warnings

import (
	"testing"

	"github.com/OneBusAway/go-gtfs/constants"
	"github.com/google/go-cmp/cmp"
)

// Verify that StaticWarningKind satisfies the error interface.
var _ error = StaticWarningKind(nil)

func TestFlexWarningKindsHaveMessages(t *testing.T) {
	for _, tc := range []struct {
		kind StaticWarningKind
		want string
	}{
		{StopTimeInvalidReference{Reason: "r"}, "stop time has an invalid reference: r"},
		{StopTimeInvalidWindow{Reason: "r"}, "stop time has an invalid pickup/drop-off window: r"},
		{LocationGroupUnknownStop{GroupID: "g", StopID: "s"}, `location group "g" references unknown stop "s"`},
		{LocationInvalidGeometry{LocationID: "l", Reason: "r"}, `location "l" has invalid geometry: r`},
		{BookingRuleInvalid{BookingRuleID: "b", Reason: "r"}, `booking rule "b" is invalid: r`},
		{LocationsFileInvalid{Reason: "r"}, "locations.geojson is invalid and was ignored: r"},
		{TransferMissingTrip{Column: "from_trip_id"}, "linked-trip transfer is missing from_trip_id"},
		{TransferUnknownReference{Column: "to_trip_id", ID: "t"}, `transfer references unknown to_trip_id "t"`},
		{TransferUnknownReference{Column: "from_route_id", ID: "r"}, `transfer references unknown from_route_id "r"`},
		{TransferTripNotOnRoute{TripColumn: "from_trip_id", TripID: "t", RouteID: "r"}, `transfer from_trip_id "t" does not run on route "r"`},
		{TransferStopNotAllowed{Column: "from_stop_id", StopID: "s", Allowed: "a stop (location_type 0)"}, `transfer from_stop_id "s" is not a stop (location_type 0)`},
		{TransferInvalidType{Value: "6"}, `transfer has invalid transfer_type "6"`},
		{TransferMissingStop{Column: "from_stop_id"}, "transfer is missing from_stop_id"},
		{StopInvalidLocationType{StopID: "s", Value: "5"}, `stop "s" has invalid location_type "5"; read as a stop`},
		{FileUnreadable{Reason: "r"}, "file could not be fully read: r"},
		{StopUnknownLevel{StopID: "s", LevelID: "l"}, `stop "s" references unknown level "l"`},
		{LevelInvalid{LevelID: "l", Reason: "r"}, `level "l" is invalid: r`},
		{StopAccessInvalid{StopID: "s", Value: "2", Reason: "r"}, `stop "s" stop_access "2" is ignored: r`},
		{PathwayInvalid{PathwayID: "p", Reason: "r"}, `pathway "p" is invalid: r`},
		{TranslationInvalid{Reason: "r"}, "translation is invalid: r"},
		{AttributionInvalid{AttributionID: "a", Reason: "r"}, `attribution "a" is invalid: r`},
		{NetworksDefinedTwice{}, "routes.txt network_id is ignored because networks.txt or route_networks.txt is present"},
		{AreaInvalid{AreaID: "a", Reason: "r"}, `area "a" is invalid: r`},
		{FareAttributeInvalid{FareID: "f", Reason: "r"}, `fare "f" is invalid: r`},
		{FareAttributeInvalidValue{FareID: "f", Column: "c", Value: "v"}, `fare "f" has invalid c "v"; read as unset`},
		{FareAttributeAgencyAmbiguous{FareID: "f"}, `fare "f" has no agency_id but the feed has several agencies`},
		{FareRuleInvalid{FareID: "f", Reason: "r"}, `fare rule for fare "f" is invalid: r`},
		{RiderCategoryInvalid{RiderCategoryID: "c", Reason: "r"}, `rider category "c" is invalid: r`},
		{FareMediaInvalid{FareMediaID: "m", Reason: "r"}, `fare media "m" is invalid: r`},
		{FareProductInvalid{FareProductID: "p", Reason: "r"}, `fare product "p" is invalid: r`},
		{FareProductDefaultCategory{FareProductID: "p", Defaults: 2}, `fare product "p" has 2 default rider categories among its eligible ones; want exactly 1`},
		{StopAreaInvalid{AreaID: "a", StopID: "s", Reason: "r"}, `stop "s" in area "a" is skipped: r`},
		{NetworkInvalid{NetworkID: "n", Reason: "r"}, `network "n" is invalid: r`},
		{RouteNetworkInvalid{RouteID: "r", NetworkID: "n", Reason: "x"}, `route "r" in network "n" is skipped: x`},
		{AttributionWithoutRole{AttributionID: "a"}, `attribution "a" has no role; set is_producer, is_operator or is_authority`},
		{TranslationsWithoutFeedInfo{}, "translations.txt is present but feed_info.txt is missing or unusable"},
		{PathwayInvalidValue{PathwayID: "p", Column: "length", Value: "-1"}, `pathway "p" has invalid length "-1"; read as unset`},
		{FeedInfoExtraRow{}, "feed_info.txt has more than one row; only the first is read"},
		{FeedInfoMissingValues{Columns: []string{"feed_lang"}}, "feed info is missing values [feed_lang]"},
		{FeedInfoInvalidDate{Column: "feed_end_date", Value: "x"}, `feed info feed_end_date "x" is not a YYYYMMDD date`},
	} {
		if got := tc.kind.Error(); got != tc.want {
			t.Errorf("%T.Error() = %q, want %q", tc.kind, got, tc.want)
		}
	}
}

func TestNewFileWarning(t *testing.T) {
	kind := LocationInvalidGeometry{LocationID: "l", Reason: "r"}
	got := NewFileWarning(constants.LocationsGeoJSONFile, kind)
	want := StaticWarning{Kind: kind, File: constants.LocationsGeoJSONFile}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("NewFileWarning() mismatch (-got +want):\n%s", diff)
	}
}
