package gtfs

import (
	"testing"

	"github.com/OneBusAway/go-gtfs/constants"
	"github.com/OneBusAway/go-gtfs/warnings"
	"github.com/google/go-cmp/cmp"
)

// parseNetworksFeed parses the default feed with the given routes.txt and,
// when given, networks.txt and route_networks.txt.
func parseNetworksFeed(t *testing.T, routes, networks, routeNetworks []string) *Static {
	t.Helper()
	b := newZipBuilderWithDefaults().add("routes.txt", routes...)
	if networks != nil {
		b.add("networks.txt", networks...)
	}
	if routeNetworks != nil {
		b.add("route_networks.txt", routeNetworks...)
	}
	static, err := ParseStatic(b.build(), ParseStaticOptions{})
	if err != nil {
		t.Fatalf("ParseStatic: %s", err)
	}
	return static
}

// routeNetworks maps each route ID to its network's ID, or "" for none.
func routeNetworks(static *Static) map[string]string {
	m := map[string]string{}
	for _, r := range static.Routes {
		m[r.Id] = ""
		if r.Network != nil {
			m[r.Id] = r.Network.Id
		}
	}
	return m
}

func TestParseNetworks(t *testing.T) {
	plainRoutes := []string{"route_id,route_type", "route_id,3", "100340,0", "100336,4"}
	for _, tc := range []struct {
		desc              string
		routes            []string
		networks          []string
		routeNetworks     []string
		wantNetworks      []Network
		wantRouteNetworks map[string]string
		wantWarnings      []warnings.StaticWarningKind
	}{
		{
			desc:              "no networks",
			routes:            plainRoutes,
			wantRouteNetworks: map[string]string{"route_id": "", "100340": "", "100336": ""},
		},
		{
			desc:              "routes.txt network_id",
			routes:            []string{"route_id,route_type,network_id", "route_id,3,", "Red,1,rapid_transit", "1,3,local_bus", "Blue,1,rapid_transit"},
			wantNetworks:      []Network{{Id: "rapid_transit"}, {Id: "local_bus"}},
			wantRouteNetworks: map[string]string{"route_id": "", "Red": "rapid_transit", "1": "local_bus", "Blue": "rapid_transit"},
		},
		{
			desc:   "networks.txt and route_networks.txt",
			routes: plainRoutes,
			networks: []string{
				"network_id,network_name",
				"free_shuttle,Free Shuttle",
				"streetcar,Street Car",
				"west_seattle_water_taxi,West Seattle Water Taxi",
			},
			routeNetworks: []string{"network_id,route_id", "streetcar,100340", "west_seattle_water_taxi,100336"},
			wantNetworks: []Network{
				{Id: "free_shuttle", Name: "Free Shuttle"},
				{Id: "streetcar", Name: "Street Car"},
				{Id: "west_seattle_water_taxi", Name: "West Seattle Water Taxi"},
			},
			wantRouteNetworks: map[string]string{"route_id": "", "100340": "streetcar", "100336": "west_seattle_water_taxi"},
		},
		{
			desc:              "networks.txt alone",
			routes:            plainRoutes,
			networks:          []string{"network_id,network_name", "streetcar,Street Car"},
			wantNetworks:      []Network{{Id: "streetcar", Name: "Street Car"}},
			wantRouteNetworks: map[string]string{"route_id": "", "100340": "", "100336": ""},
		},
		{
			desc:              "both methods uses the network files",
			routes:            []string{"route_id,route_type,network_id", "route_id,3,", "100340,0,rail", "100336,4,"},
			networks:          []string{"network_id,network_name", "streetcar,Street Car"},
			routeNetworks:     []string{"network_id,route_id", "streetcar,100340"},
			wantNetworks:      []Network{{Id: "streetcar", Name: "Street Car"}},
			wantRouteNetworks: map[string]string{"route_id": "", "100340": "streetcar", "100336": ""},
			wantWarnings:      []warnings.StaticWarningKind{warnings.NetworksDefinedTwice{}},
		},
		{
			desc:              "empty routes.txt network_id column does not conflict",
			routes:            []string{"route_id,route_type,network_id", "route_id,3,", "100340,0,"},
			networks:          []string{"network_id", "streetcar"},
			routeNetworks:     []string{"network_id,route_id", "streetcar,100340"},
			wantNetworks:      []Network{{Id: "streetcar"}},
			wantRouteNetworks: map[string]string{"route_id": "", "100340": "streetcar"},
		},
		{
			desc:              "route listed twice keeps the first",
			routes:            plainRoutes,
			networks:          []string{"network_id", "streetcar", "ferry"},
			routeNetworks:     []string{"network_id,route_id", "streetcar,100340", "ferry,100340"},
			wantNetworks:      []Network{{Id: "streetcar"}, {Id: "ferry"}},
			wantRouteNetworks: map[string]string{"route_id": "", "100340": "streetcar", "100336": ""},
			wantWarnings:      []warnings.StaticWarningKind{warnings.RouteNetworkInvalid{RouteID: "100340", NetworkID: "ferry", Reason: "route is already in network \"streetcar\""}},
		},
		{
			desc:              "unknown network_id",
			routes:            plainRoutes,
			networks:          []string{"network_id", "streetcar"},
			routeNetworks:     []string{"network_id,route_id", "nope,100340"},
			wantNetworks:      []Network{{Id: "streetcar"}},
			wantRouteNetworks: map[string]string{"route_id": "", "100340": "", "100336": ""},
			wantWarnings:      []warnings.StaticWarningKind{warnings.RouteNetworkInvalid{RouteID: "100340", NetworkID: "nope", Reason: "unknown network_id"}},
		},
		{
			desc:              "route_networks.txt without networks.txt",
			routes:            plainRoutes,
			routeNetworks:     []string{"network_id,route_id", "streetcar,100340"},
			wantRouteNetworks: map[string]string{"route_id": "", "100340": "", "100336": ""},
			wantWarnings:      []warnings.StaticWarningKind{warnings.RouteNetworkInvalid{RouteID: "100340", NetworkID: "streetcar", Reason: "unknown network_id"}},
		},
		{
			desc:              "unknown route_id",
			routes:            plainRoutes,
			networks:          []string{"network_id", "streetcar"},
			routeNetworks:     []string{"network_id,route_id", "streetcar,nope"},
			wantNetworks:      []Network{{Id: "streetcar"}},
			wantRouteNetworks: map[string]string{"route_id": "", "100340": "", "100336": ""},
			wantWarnings:      []warnings.StaticWarningKind{warnings.RouteNetworkInvalid{RouteID: "nope", NetworkID: "streetcar", Reason: "unknown route_id"}},
		},
		{
			desc:              "route_networks.txt row with missing values",
			routes:            plainRoutes,
			networks:          []string{"network_id", "streetcar"},
			routeNetworks:     []string{"network_id,route_id", "streetcar,"},
			wantNetworks:      []Network{{Id: "streetcar"}},
			wantRouteNetworks: map[string]string{"route_id": "", "100340": "", "100336": ""},
			wantWarnings:      []warnings.StaticWarningKind{warnings.RouteNetworkInvalid{NetworkID: "streetcar", Reason: "missing values [route_id]"}},
		},
		{
			desc:              "networks.txt duplicate and empty network_id",
			routes:            plainRoutes,
			networks:          []string{"network_id,network_name", "streetcar,First", "streetcar,Second", ",Nameless"},
			wantNetworks:      []Network{{Id: "streetcar", Name: "First"}},
			wantRouteNetworks: map[string]string{"route_id": "", "100340": "", "100336": ""},
			wantWarnings: []warnings.StaticWarningKind{
				warnings.NetworkInvalid{NetworkID: "streetcar", Reason: "duplicate network_id"},
				warnings.NetworkInvalid{Reason: "missing values [network_id]"},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			static := parseNetworksFeed(t, tc.routes, tc.networks, tc.routeNetworks)
			if diff := cmp.Diff(static.Networks, tc.wantNetworks); diff != "" {
				t.Errorf("Networks mismatch (-got +want):\n%s", diff)
			}
			if diff := cmp.Diff(routeNetworks(static), tc.wantRouteNetworks); diff != "" {
				t.Errorf("route networks mismatch (-got +want):\n%s", diff)
			}
			if diff := cmp.Diff(warningKinds(static.Warnings), tc.wantWarnings); diff != "" {
				t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

// The table only compares warning kinds; this checks the warning is about
// routes.txt as a whole, not a row.
func TestParseNetworks_DefinedTwiceIsAFileWarning(t *testing.T) {
	static := parseNetworksFeed(t,
		[]string{"route_id,route_type,network_id", "route_id,3,rail"},
		[]string{"network_id", "streetcar"}, nil)
	want := []warnings.StaticWarning{warnings.NewFileWarning(constants.RoutesFile, warnings.NetworksDefinedTwice{})}
	if diff := cmp.Diff(static.Warnings, want); diff != "" {
		t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
	}
}

// A route's Network must point into Static.Networks, not at a copy.
func TestParseNetworks_RouteNetworkIsShared(t *testing.T) {
	for _, tc := range []struct {
		desc                    string
		routes, networks, links []string
	}{
		{"routes.txt", []string{"route_id,route_type,network_id", "route_id,3,n"}, nil, nil},
		{"network files", []string{"route_id,route_type", "route_id,3"}, []string{"network_id", "n"}, []string{"network_id,route_id", "n,route_id"}},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			static := parseNetworksFeed(t, tc.routes, tc.networks, tc.links)
			if static.Routes[0].Network != &static.Networks[0] {
				t.Errorf("route Network %p is not &Static.Networks[0] %p", static.Routes[0].Network, &static.Networks[0])
			}
		})
	}
}
