package gtfs

import (
	"fmt"
	"testing"

	"github.com/OneBusAway/go-gtfs/constants"
	"github.com/OneBusAway/go-gtfs/warnings"
	"github.com/google/go-cmp/cmp"
)

// parseTransfersFeed parses a feed with stops S1, S2 and station ST, routes R1
// and R2, and trips T1 (on R1) and T2 (on R2), plus the given transfers.txt.
func parseTransfersFeed(t *testing.T, transfers ...string) *Static {
	t.Helper()
	feed := newZipBuilder().add(
		"agency.txt",
		"agency_id,agency_name,agency_url,agency_timezone",
		"a,b,c,d",
	).add(
		"routes.txt",
		"route_id,route_type",
		"R1,3",
		"R2,3",
	).add(
		"stops.txt",
		"stop_id,location_type,parent_station",
		"S1,0,ST",
		"S2,0,ST",
		"ST,1,",
	).add(
		"calendar.txt",
		"service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date",
		"service_id,1,1,1,1,1,1,1,20220504,20220507",
	).add(
		"trips.txt",
		"route_id,service_id,trip_id",
		"R1,service_id,T1",
		"R2,service_id,T2",
	).add(
		"transfers.txt",
		transfers...,
	).build()
	static, err := ParseStatic(feed, ParseStaticOptions{})
	if err != nil {
		t.Fatalf("ParseStatic: %s", err)
	}
	return static
}

// summarizeTransfers renders each transfer as one line of IDs, so a test can
// state the expected result without building linked Stop/Route/Trip values.
func summarizeTransfers(transfers []Transfer) []string {
	var s []string
	for _, tr := range transfers {
		min := "<nil>"
		if tr.MinTransferTime != nil {
			min = fmt.Sprint(*tr.MinTransferTime)
		}
		s = append(s, fmt.Sprintf("from=%s to=%s fromRoute=%s toRoute=%s fromTrip=%s toTrip=%s type=%s min=%s",
			stopID(tr.From), stopID(tr.To), routeID(tr.FromRoute), routeID(tr.ToRoute),
			tripID(tr.FromTrip), tripID(tr.ToTrip), tr.Type, min))
	}
	return s
}

func warningKinds(ws []warnings.StaticWarning) []warnings.StaticWarningKind {
	var kinds []warnings.StaticWarningKind
	for _, w := range ws {
		kinds = append(kinds, w.Kind)
	}
	return kinds
}

func TestParseTransfers(t *testing.T) {
	const header = "from_stop_id,to_stop_id,from_route_id,to_route_id,from_trip_id,to_trip_id,transfer_type,min_transfer_time"
	for _, tc := range []struct {
		desc         string
		transfers    []string
		wantSummary  []string
		wantWarnings []warnings.StaticWarningKind
	}{
		{
			desc:        "in-seat transfer without stop ids",
			transfers:   []string{header, ",,,,T1,T2,4,"},
			wantSummary: []string{"from= to= fromRoute= toRoute= fromTrip=T1 toTrip=T2 type=IN_SEAT min=<nil>"},
		},
		{
			desc:        "in-seat transfer with stop ids",
			transfers:   []string{header, "S1,S2,,,T1,T2,4,"},
			wantSummary: []string{"from=S1 to=S2 fromRoute= toRoute= fromTrip=T1 toTrip=T2 type=IN_SEAT min=<nil>"},
		},
		{
			desc:        "in-seat transfer with only to_stop_id",
			transfers:   []string{header, ",S2,,,T1,T2,4,"},
			wantSummary: []string{"from= to=S2 fromRoute= toRoute= fromTrip=T1 toTrip=T2 type=IN_SEAT min=<nil>"},
		},
		{
			desc:        "in-seat transfer not allowed",
			transfers:   []string{header, "S1,S2,,,T1,T2,5,"},
			wantSummary: []string{"from=S1 to=S2 fromRoute= toRoute= fromTrip=T1 toTrip=T2 type=IN_SEAT_NOT_ALLOWED min=<nil>"},
		},
		{
			desc:        "in-seat transfer not allowed without stop ids",
			transfers:   []string{header, ",,,,T1,T2,5,"},
			wantSummary: []string{"from= to= fromRoute= toRoute= fromTrip=T1 toTrip=T2 type=IN_SEAT_NOT_ALLOWED min=<nil>"},
		},
		{
			desc:        "linked trips file without stop id columns",
			transfers:   []string{"from_trip_id,to_trip_id,transfer_type", "T1,T2,4"},
			wantSummary: []string{"from= to= fromRoute= toRoute= fromTrip=T1 toTrip=T2 type=IN_SEAT min=<nil>"},
		},
		{
			desc:         "in-seat transfer without from_trip_id",
			transfers:    []string{header, ",,,,,T2,4,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TransferMissingTrip{Column: "from_trip_id"}},
		},
		{
			desc:         "in-seat transfer not allowed without to_trip_id",
			transfers:    []string{header, "S1,S2,,,T1,,5,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TransferMissingTrip{Column: "to_trip_id"}},
		},
		{
			desc:         "in-seat transfer with unknown from_trip_id",
			transfers:    []string{header, ",,,,UNKNOWN,T2,4,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TransferUnknownReference{Column: "from_trip_id", ID: "UNKNOWN"}},
		},
		{
			desc:         "in-seat transfer with unknown to_trip_id",
			transfers:    []string{header, ",,,,T1,UNKNOWN,4,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TransferUnknownReference{Column: "to_trip_id", ID: "UNKNOWN"}},
		},
		{
			desc:        "timed transfer between routes at one stop",
			transfers:   []string{header, "S1,S1,R1,R2,,,1,"},
			wantSummary: []string{"from=S1 to=S1 fromRoute=R1 toRoute=R2 fromTrip= toTrip= type=TIMED min=<nil>"},
		},
		{
			desc:        "minimum time transfer at one stop",
			transfers:   []string{header, "S1,S1,,,,,2,180"},
			wantSummary: []string{"from=S1 to=S1 fromRoute= toRoute= fromTrip= toTrip= type=REQUIRES_TIME min=180"},
		},
		{
			desc:        "minimum time transfer within one station",
			transfers:   []string{header, "ST,ST,,,,,2,300"},
			wantSummary: []string{"from=ST to=ST fromRoute= toRoute= fromTrip= toTrip= type=REQUIRES_TIME min=300"},
		},
		{
			desc:        "transfer not possible between trips at one stop",
			transfers:   []string{header, "S1,S1,,,T1,T2,3,"},
			wantSummary: []string{"from=S1 to=S1 fromRoute= toRoute= fromTrip=T1 toTrip=T2 type=NOT_POSSIBLE min=<nil>"},
		},
		{
			desc:        "transfer between routes and trips",
			transfers:   []string{header, "S1,S2,R1,R2,T1,T2,2,120"},
			wantSummary: []string{"from=S1 to=S2 fromRoute=R1 toRoute=R2 fromTrip=T1 toTrip=T2 type=REQUIRES_TIME min=120"},
		},
		{
			desc:         "transfer with unknown from_trip_id",
			transfers:    []string{header, "S1,S2,,,UNKNOWN,,3,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TransferUnknownReference{Column: "from_trip_id", ID: "UNKNOWN"}},
		},
		{
			desc:         "transfer with unknown to_trip_id",
			transfers:    []string{header, "S1,S2,,,T1,UNKNOWN,3,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TransferUnknownReference{Column: "to_trip_id", ID: "UNKNOWN"}},
		},
		{
			desc:         "transfer with unknown from_route_id",
			transfers:    []string{header, "S1,S2,UNKNOWN,R2,,,1,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TransferUnknownReference{Column: "from_route_id", ID: "UNKNOWN"}},
		},
		{
			desc:         "transfer with unknown to_route_id",
			transfers:    []string{header, "S1,S2,R1,UNKNOWN,,,1,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TransferUnknownReference{Column: "to_route_id", ID: "UNKNOWN"}},
		},
		{
			desc:      "transfer with two unknown ids warns for each",
			transfers: []string{header, "S1,S2,NOPE,R2,,GONE,2,60"},
			wantWarnings: []warnings.StaticWarningKind{
				warnings.TransferUnknownReference{Column: "from_route_id", ID: "NOPE"},
				warnings.TransferUnknownReference{Column: "to_trip_id", ID: "GONE"},
			},
		},
		{
			desc:         "in-seat transfer with unknown from_route_id",
			transfers:    []string{header, ",,UNKNOWN,,T1,T2,4,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TransferUnknownReference{Column: "from_route_id", ID: "UNKNOWN"}},
		},
		{
			desc:      "unknown id skips only its own row",
			transfers: []string{header, "S1,S2,,,UNKNOWN,,3,", "S1,S2,,,T1,,3,"},
			wantSummary: []string{
				"from=S1 to=S2 fromRoute= toRoute= fromTrip=T1 toTrip= type=NOT_POSSIBLE min=<nil>",
			},
			wantWarnings: []warnings.StaticWarningKind{warnings.TransferUnknownReference{Column: "from_trip_id", ID: "UNKNOWN"}},
		},
		{
			desc:         "in-seat transfer from a station",
			transfers:    []string{header, "ST,S2,,,T1,T2,4,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TransferStopNotAllowed{Column: "from_stop_id", StopID: "ST"}},
		},
		{
			desc:         "in-seat transfer not allowed to a station",
			transfers:    []string{header, "S1,ST,,,T1,T2,5,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TransferStopNotAllowed{Column: "to_stop_id", StopID: "ST"}},
		},
		{
			desc:         "transfer whose from_trip_id is not on from_route_id",
			transfers:    []string{header, "S1,S2,R2,,T1,,1,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TransferTripNotOnRoute{TripColumn: "from_trip_id", TripID: "T1", RouteID: "R2"}},
		},
		{
			desc:         "transfer whose to_trip_id is not on to_route_id",
			transfers:    []string{header, "S1,S2,,R1,,T2,1,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TransferTripNotOnRoute{TripColumn: "to_trip_id", TripID: "T2", RouteID: "R1"}},
		},
		{
			desc:         "in-seat transfer whose trip is not on its route",
			transfers:    []string{header, ",,R1,R1,T1,T2,4,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TransferTripNotOnRoute{TripColumn: "to_trip_id", TripID: "T2", RouteID: "R1"}},
		},
		{
			desc:        "trip on a route in only the from direction",
			transfers:   []string{header, "S1,S2,R1,,T1,T2,2,60"},
			wantSummary: []string{"from=S1 to=S2 fromRoute=R1 toRoute= fromTrip=T1 toTrip=T2 type=REQUIRES_TIME min=60"},
		},
		{
			desc:      "minimum time transfer without from_stop_id is skipped",
			transfers: []string{header, ",S2,,,,,2,180"},
		},
		{
			desc:      "recommended transfer without to_stop_id is skipped",
			transfers: []string{header, "S1,,,,,,0,"},
		},
		{
			desc:      "empty transfer_type without stop ids is skipped",
			transfers: []string{header, ",,,,T1,T2,,"},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			static := parseTransfersFeed(t, tc.transfers...)
			if diff := cmp.Diff(summarizeTransfers(static.Transfers), tc.wantSummary); diff != "" {
				t.Errorf("Transfers mismatch (-got +want):\n%s", diff)
			}
			if diff := cmp.Diff(warningKinds(static.Warnings), tc.wantWarnings); diff != "" {
				t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

func TestParseTransfers_WarningPointsAtRow(t *testing.T) {
	const header = "from_stop_id,to_stop_id,from_trip_id,to_trip_id,transfer_type"
	static := parseTransfersFeed(t, header, "S1,S2,T1,T2,4", ",,,T2,5")
	want := []warnings.StaticWarning{{
		Kind:          warnings.TransferMissingTrip{Column: "from_trip_id"},
		File:          constants.TransfersFile,
		RowNumber:     2,
		RowContent:    []string{"", "", "", "T2", "5"},
		HeaderContent: []string{"from_stop_id", "to_stop_id", "from_trip_id", "to_trip_id", "transfer_type"},
	}}
	if diff := cmp.Diff(static.Warnings, want); diff != "" {
		t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
	}
}

func TestTransferTypeString(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"", "RECOMMENDED"},
		{"0", "RECOMMENDED"},
		{"1", "TIMED"},
		{"2", "REQUIRES_TIME"},
		{"3", "NOT_POSSIBLE"},
		{"4", "IN_SEAT"},
		{"5", "IN_SEAT_NOT_ALLOWED"},
	} {
		if got := parseTransferType(tc.in).String(); got != tc.want {
			t.Errorf("parseTransferType(%q).String() = %q, want %q", tc.in, got, tc.want)
		}
	}
}
