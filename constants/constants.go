package constants

type StaticFile string

const (
	AgencyFile             StaticFile = "agency.txt"
	FeedInfoFile           StaticFile = "feed_info.txt"
	StopsFile              StaticFile = "stops.txt"
	StopTimesFile          StaticFile = "stop_times.txt"
	BookingRulesFile       StaticFile = "booking_rules.txt"
	LocationGroupsFile     StaticFile = "location_groups.txt"
	LocationGroupStopsFile StaticFile = "location_group_stops.txt"
	LevelsFile             StaticFile = "levels.txt"
	LocationsGeoJSONFile   StaticFile = "locations.geojson"
	PathwaysFile           StaticFile = "pathways.txt"
	TransfersFile          StaticFile = "transfers.txt"
	TranslationsFile       StaticFile = "translations.txt"
)
