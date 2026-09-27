package constants

type StaticFile string

const (
	AgencyFile             StaticFile = "agency.txt"
	StopTimesFile          StaticFile = "stop_times.txt"
	BookingRulesFile       StaticFile = "booking_rules.txt"
	LocationGroupsFile     StaticFile = "location_groups.txt"
	LocationGroupStopsFile StaticFile = "location_group_stops.txt"
	LocationsGeoJSONFile   StaticFile = "locations.geojson"
)
