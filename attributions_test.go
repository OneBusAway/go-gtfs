package gtfs

import (
	"fmt"
	"testing"

	"github.com/OneBusAway/go-gtfs/warnings"
	"github.com/google/go-cmp/cmp"
)

// parseAttributionsFeed parses the default feed (agency a, route route_id,
// trip trip_id) plus attributions.txt if lines are given.
func parseAttributionsFeed(t *testing.T, lines ...string) *Static {
	t.Helper()
	b := newZipBuilderWithDefaults()
	if lines != nil {
		b.add("attributions.txt", lines...)
	}
	static, err := ParseStatic(b.build(), ParseStaticOptions{})
	if err != nil {
		t.Fatalf("ParseStatic: %s", err)
	}
	return static
}

// summarizeAttributions renders each attribution as one line, so a test can
// state the expected result without building linked values.
func summarizeAttributions(attributions []Attribution) []string {
	var s []string
	for _, a := range attributions {
		agency := ""
		if a.Agency != nil {
			agency = a.Agency.Id
		}
		s = append(s, fmt.Sprintf("id=%s agency=%s route=%s trip=%s org=%q producer=%t operator=%t authority=%t url=%s email=%s phone=%s",
			a.Id, agency, routeID(a.Route), tripID(a.Trip), a.OrganizationName, a.IsProducer, a.IsOperator, a.IsAuthority, a.Url, a.Email, a.Phone))
	}
	return s
}

func TestParseAttributions(t *testing.T) {
	const header = "attribution_id,agency_id,route_id,trip_id,organization_name,is_producer,is_operator,is_authority,attribution_url,attribution_email,attribution_phone"
	for _, tc := range []struct {
		desc         string
		lines        []string
		want         []string
		wantWarnings []warnings.StaticWarningKind
	}{
		{
			desc: "no attributions.txt",
		},
		{
			desc:  "whole dataset",
			lines: []string{"attribution_id,is_producer,organization_name,attribution_url", "rp,1,Rejseplanen,https://www.rejseplanen.dk"},
			want:  []string{`id=rp agency= route= trip= org="Rejseplanen" producer=true operator=false authority=false url=https://www.rejseplanen.dk email= phone=`},
		},
		{
			desc: "scoped to an agency, a route and a trip",
			lines: []string{
				header,
				"A,a,,,Agency Org,0,1,,,ops@example.com,",
				"R,,route_id,,Route Org,,,1,,,555-0100",
				"T,,,trip_id,Trip Org,1,1,1,https://example.com,,",
			},
			want: []string{
				`id=A agency=a route= trip= org="Agency Org" producer=false operator=true authority=false url= email=ops@example.com phone=`,
				`id=R agency= route=route_id trip= org="Route Org" producer=false operator=false authority=true url= email= phone=555-0100`,
				`id=T agency= route= trip=trip_id org="Trip Org" producer=true operator=true authority=true url=https://example.com email= phone=`,
			},
		},
		{
			desc:  "no attribution_id",
			lines: []string{"organization_name,is_operator", "Org,1"},
			want:  []string{`id= agency= route= trip= org="Org" producer=false operator=true authority=false url= email= phone=`},
		},
		{
			desc:         "route and trip both set",
			lines:        []string{header, "X,,route_id,trip_id,Org,1,,,,,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.AttributionInvalid{AttributionID: "X", Reason: "more than one of agency_id, route_id and trip_id is set"}},
		},
		{
			desc:         "unknown route_id",
			lines:        []string{header, "X,,nope,,Org,1,,,,,", "Y,,route_id,,Org,1,,,,,"},
			want:         []string{`id=Y agency= route=route_id trip= org="Org" producer=true operator=false authority=false url= email= phone=`},
			wantWarnings: []warnings.StaticWarningKind{warnings.AttributionInvalid{AttributionID: "X", Reason: `unknown route_id "nope"`}},
		},
		{
			desc:         "unknown agency_id",
			lines:        []string{header, "X,nope,,,Org,1,,,,,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.AttributionInvalid{AttributionID: "X", Reason: `unknown agency_id "nope"`}},
		},
		{
			desc:         "unknown trip_id",
			lines:        []string{header, "X,,,nope,Org,1,,,,,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.AttributionInvalid{AttributionID: "X", Reason: `unknown trip_id "nope"`}},
		},
		{
			desc:         "empty organization_name",
			lines:        []string{header, "X,,,,,1,,,,,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.AttributionInvalid{AttributionID: "X", Reason: "missing values [organization_name]"}},
		},
		{
			desc:         "invalid role",
			lines:        []string{header, "X,,,,Org,yes,,,,,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.AttributionInvalid{AttributionID: "X", Reason: `invalid is_producer "yes"`}},
		},
		{
			desc:  "padded role",
			lines: []string{header, "X,,,,Org, 1,0 ,,,,"},
			want:  []string{`id=X agency= route= trip= org="Org" producer=true operator=false authority=false url= email= phone=`},
		},
		{
			desc:         "duplicate attribution_id keeps the first",
			lines:        []string{header, "X,,,,First,1,,,,,", "X,,,,Second,1,,,,,"},
			want:         []string{`id=X agency= route= trip= org="First" producer=true operator=false authority=false url= email= phone=`},
			wantWarnings: []warnings.StaticWarningKind{warnings.AttributionInvalid{AttributionID: "X", Reason: "duplicate attribution_id"}},
		},
		{
			desc:         "missing organization_name column",
			lines:        []string{"attribution_id,is_producer", "X,1"},
			wantWarnings: []warnings.StaticWarningKind{warnings.MissingColumns{Columns: []string{"organization_name"}}},
		},
		{
			desc:         "unreadable attributions.txt keeps the rows read before the error",
			lines:        []string{"organization_name,is_producer", "Org,1", "broken,1,1"},
			want:         []string{`id= agency= route= trip= org="Org" producer=true operator=false authority=false url= email= phone=`},
			wantWarnings: []warnings.StaticWarningKind{warnings.FileUnreadable{Reason: "record on line 3: wrong number of fields"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			static := parseAttributionsFeed(t, tc.lines...)
			if diff := cmp.Diff(summarizeAttributions(static.Attributions), tc.want); diff != "" {
				t.Errorf("Attributions mismatch (-got +want):\n%s", diff)
			}
			if diff := cmp.Diff(warningKinds(static.Warnings), tc.wantWarnings); diff != "" {
				t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

// Scope pointers must point into Static's slices, not at copies.
func TestParseAttributions_ScopesAreShared(t *testing.T) {
	static := parseAttributionsFeed(t,
		"agency_id,route_id,trip_id,organization_name",
		"a,,,Org", ",route_id,,Org", ",,trip_id,Org",
	)
	if got := static.Attributions; got[0].Agency != &static.Agencies[0] || got[1].Route != &static.Routes[0] || got[2].Trip != &static.Trips[0] {
		t.Errorf("attribution scopes do not point into Static.Agencies, Routes and Trips")
	}
}
