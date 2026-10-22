package gtfs

import (
	"fmt"

	"github.com/OneBusAway/go-gtfs/csv"
	"github.com/OneBusAway/go-gtfs/warnings"
)

// Network is a group of routes, such as for fare rules. It comes from
// networks.txt, or from routes.txt network_id, in which case Name is empty.
type Network struct {
	Id   string
	Name string
}

// parseNetworks reads networks.txt. A row with an empty or repeated
// network_id is skipped with a warning.
func parseNetworks(csv *csv.File) ([]Network, []warnings.StaticWarning) {
	idColumn := csv.RequiredColumn("network_id")
	nameColumn := csv.OptionalColumn("network_name")

	if w := checkForMissingColumns(csv); len(w) > 0 {
		return nil, w
	}

	var networks []Network
	var w []warnings.StaticWarning
	seen := map[string]bool{}
	for csv.NextRow() {
		id := idColumn.Read()
		reason := ""
		if missingKeys := csv.MissingRowKeys(); len(missingKeys) > 0 {
			reason = fmt.Sprintf("missing values %s", missingKeys)
		} else if seen[id] {
			reason = "duplicate network_id"
		}
		if reason != "" {
			w = append(w, warnings.NewStaticWarning(csv, warnings.NetworkInvalid{NetworkID: id, Reason: reason}))
			continue
		}
		seen[id] = true
		networks = append(networks, Network{Id: id, Name: nameColumn.Read()})
	}
	return networks, w
}

// parseRouteNetworks reads route_networks.txt and sets each listed route's
// Network. A row with a missing value or an unknown ID is skipped with a
// warning, as is a row for a route already in a network: GTFS allows a route
// in only one network, so the first row wins.
func parseRouteNetworks(csv *csv.File, networks []Network, routeIdToRoute map[string]*Route) []warnings.StaticWarning {
	networkIDColumn := csv.RequiredColumn("network_id")
	routeIDColumn := csv.RequiredColumn("route_id")

	if w := checkForMissingColumns(csv); len(w) > 0 {
		return w
	}

	networkIdToNetwork := networksByID(networks)
	var w []warnings.StaticWarning
	for csv.NextRow() {
		networkID := networkIDColumn.Read()
		routeID := routeIDColumn.Read()
		network := networkIdToNetwork[networkID]
		route := routeIdToRoute[routeID]
		reason := ""
		switch missingKeys := csv.MissingRowKeys(); {
		case len(missingKeys) > 0:
			reason = fmt.Sprintf("missing values %s", missingKeys)
		case network == nil:
			reason = "unknown network_id"
		case route == nil:
			reason = "unknown route_id"
		case route.Network != nil:
			reason = fmt.Sprintf("route is already in network %q", route.Network.Id)
		default:
			route.Network = network
			continue
		}
		w = append(w, warnings.NewStaticWarning(csv, warnings.RouteNetworkInvalid{RouteID: routeID, NetworkID: networkID, Reason: reason}))
	}
	return w
}

// networksFromRoutes builds one Network for each distinct routes.txt
// network_id, in order of first use, and points the routes at them.
// networkIDs holds each route's network_id, or is nil if routes.txt has none.
func networksFromRoutes(routes []Route, networkIDs []string) []Network {
	var networks []Network
	index := map[string]int{}
	for _, id := range networkIDs {
		if _, ok := index[id]; id != "" && !ok {
			index[id] = len(networks)
			networks = append(networks, Network{Id: id})
		}
	}
	for i, id := range networkIDs {
		if id != "" {
			routes[i].Network = &networks[index[id]]
		}
	}
	return networks
}
