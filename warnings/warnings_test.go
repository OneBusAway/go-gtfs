package warnings

import (
	"testing"

	"github.com/OneBusAway/go-gtfs/constants"
	"github.com/google/go-cmp/cmp"
)

// Verify that StaticWarningKind satisfies the error interface.
var (
	w StaticWarningKind = nil
	e error             = w
)

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
