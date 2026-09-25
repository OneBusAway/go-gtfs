package gtfs

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/OneBusAway/go-gtfs/constants"
	"github.com/OneBusAway/go-gtfs/warnings"
	"github.com/google/go-cmp/cmp"
)

var (
	may4 = time.Date(2022, 5, 4, 0, 0, 0, 0, time.UTC)
	may7 = time.Date(2022, 5, 7, 0, 0, 0, 0, time.UTC)
)

func TestParse(t *testing.T) {
	defaultAgency := Agency{
		Id:       "a",
		Name:     "b",
		Url:      "c",
		Timezone: "d",
	}
	otherAgency := Agency{
		Id:       "e",
		Name:     "f",
		Url:      "g",
		Timezone: "h",
	}
	defaultRoute := Route{
		Id:                "route_id",
		Agency:            &defaultAgency,
		Color:             "FFFFFF",
		TextColor:         "000000",
		Type:              RouteType_Bus,
		ContinuousPickup:  PickupDropOffPolicy_No,
		ContinuousDropOff: PickupDropOffPolicy_No,
	}
	otherRoute := Route{
		Id:                "other_route_id",
		Agency:            &defaultAgency,
		Color:             "FFFFFF",
		TextColor:         "000000",
		Type:              RouteType_Bus,
		ContinuousPickup:  PickupDropOffPolicy_No,
		ContinuousDropOff: PickupDropOffPolicy_No,
	}
	defaultStop := Stop{
		Id: "stop_id",
	}
	defaultService := Service{
		Id:        "service_id",
		StartDate: may4,
		EndDate:   may7,
	}
	defaultTrip := ScheduledTrip{
		ID:      "trip_id",
		Route:   &defaultRoute,
		Service: &defaultService,
	}
	otherTrip := ScheduledTrip{
		ID:      "other_trip_id",
		Route:   &otherRoute,
		Service: &defaultService,
	}
	for _, tc := range []struct {
		desc     string
		content  []byte
		opts     ParseStaticOptions
		expected *Static
	}{
		{
			desc: "agency with only required fields",
			content: newZipBuilder().add(
				"agency.txt",
				"agency_id,agency_name,agency_url,agency_timezone\na,b,c,d",
			).build(),
			expected: &Static{
				Agencies: []Agency{
					{
						Id:       "a",
						Name:     "b",
						Url:      "c",
						Timezone: "d",
					},
				},
			},
		},
		{
			desc: "agency file with missing columns",
			content: newZipBuilder().add(
				"agency.txt",
				"agency_id,agency_url,agency_timezone\na,c,d",
			).build(),
			expected: &Static{
				Warnings: []warnings.StaticWarning{
					{
						Kind: warnings.MissingColumns{
							Columns: []string{"agency_name"},
						},
						File:          constants.AgencyFile,
						RowNumber:     0,
						RowContent:    []string{"agency_id", "agency_url", "agency_timezone"},
						HeaderContent: []string{"agency_id", "agency_url", "agency_timezone"},
					},
				},
			},
		},
		{
			desc: "agency with missing values",
			content: newZipBuilder().add(
				"agency.txt",
				"agency_id,agency_name,agency_url,agency_timezone\na,b,c,d\ne,,g,h",
			).build(),
			expected: &Static{
				Agencies: []Agency{
					{
						Id:       "a",
						Name:     "b",
						Url:      "c",
						Timezone: "d",
					},
				},
				Warnings: []warnings.StaticWarning{
					{
						Kind: warnings.AgencyMissingValues{
							AgencyID: "e",
							Columns:  []string{"agency_name"},
						},
						File:          constants.AgencyFile,
						RowNumber:     2,
						RowContent:    []string{"e", "", "g", "h"},
						HeaderContent: []string{"agency_id", "agency_name", "agency_url", "agency_timezone"},
					},
				},
			},
		},
		{
			desc: "agency with all fields",
			content: newZipBuilder().add(
				"agency.txt",
				"agency_id,agency_name,agency_url,agency_timezone,agency_lang,agency_phone,agency_fare_url,agency_email\na,b,c,d,e,f,g,h",
			).build(),
			expected: &Static{
				Agencies: []Agency{
					{
						Id:       "a",
						Name:     "b",
						Url:      "c",
						Timezone: "d",
						Language: "e",
						Phone:    "f",
						FareUrl:  "g",
						Email:    "h",
					},
				},
			},
		},
		{
			desc: "route with only required fields",
			content: newZipBuilder().add(
				"agency.txt",
				"agency_id,agency_name,agency_url,agency_timezone\na,b,c,d",
			).add(
				"routes.txt",
				"route_id,route_type\na,3",
			).build(),
			expected: &Static{
				Agencies: []Agency{defaultAgency},
				Routes: []Route{
					{
						Id:                "a",
						Agency:            &defaultAgency,
						Color:             "FFFFFF",
						TextColor:         "000000",
						Type:              RouteType_Bus,
						ContinuousPickup:  PickupDropOffPolicy_No,
						ContinuousDropOff: PickupDropOffPolicy_No,
					},
				},
			},
		},
		{
			desc: "route with all fields",
			content: newZipBuilder().add(
				"agency.txt",
				"agency_id,agency_name,agency_url,agency_timezone\na,b,c,d",
			).add(
				"routes.txt",
				"route_id,route_color,route_text_color,route_short_name,"+
					"route_long_name,route_desc,route_type,route_url,route_sort_order,continuous_pickup,continuous_drop_off\n"+
					"a,b,c,e,f,g,2,h,5,0,2",
			).build(),
			expected: &Static{
				Agencies: []Agency{defaultAgency},
				Routes: []Route{
					{
						Id:                "a",
						Agency:            &defaultAgency,
						Color:             "b",
						TextColor:         "c",
						ShortName:         "e",
						LongName:          "f",
						Description:       "g",
						Type:              RouteType_Rail,
						Url:               "h",
						SortOrder:         ptr(int32(5)),
						ContinuousPickup:  PickupDropOffPolicy_Yes,
						ContinuousDropOff: PickupDropOffPolicy_PhoneAgency,
					},
				},
			},
		},
		{
			desc: "route with matching specified agency",
			content: newZipBuilder().add(
				"agency.txt",
				"agency_id,agency_name,agency_url,agency_timezone\na,b,c,d\ne,f,g,h",
			).add(
				"routes.txt",
				"route_id,route_type,agency_id\na,3,e",
			).build(),
			expected: &Static{
				Agencies: []Agency{defaultAgency, otherAgency},
				Routes: []Route{
					{
						Id:                "a",
						Agency:            &otherAgency,
						Color:             "FFFFFF",
						TextColor:         "000000",
						Type:              RouteType_Bus,
						ContinuousPickup:  PickupDropOffPolicy_No,
						ContinuousDropOff: PickupDropOffPolicy_No,
					},
				},
			},
		},
		{
			desc: "stop",
			content: newZipBuilder().add(
				"stops.txt",
				"stop_id,stop_code,stop_name,stop_desc,zone_id,stop_lon,stop_lat,"+
					"stop_url,location_type,stop_timezone,wheelchair_boarding,platform_code",
				"a,b,c,d,e,1.5,2.5,f,1,g,1,h",
				"i,j,k,l,m,1.5,2.5,n,1,o,1,p",
			).build(),
			expected: &Static{
				Stops: []Stop{
					{
						Id:                 "a",
						Code:               "b",
						Name:               "c",
						Description:        "d",
						ZoneId:             "e",
						Longitude:          ptr(1.5),
						Latitude:           ptr(2.5),
						Url:                "f",
						Type:               StopType_Station,
						Timezone:           "g",
						WheelchairBoarding: WheelchairBoarding_Possible,
						PlatformCode:       "h",
					},
					{
						Id:                 "i",
						Code:               "j",
						Name:               "k",
						Description:        "l",
						ZoneId:             "m",
						Longitude:          ptr(1.5),
						Latitude:           ptr(2.5),
						Url:                "n",
						Type:               StopType_Station,
						Timezone:           "o",
						WheelchairBoarding: WheelchairBoarding_Possible,
						PlatformCode:       "p",
					},
				},
			},
		},
		{
			desc: "stop with parent",
			content: newZipBuilder().add(
				"stops.txt",
				"stop_id,parent_station\na,b\nb,",
			).build(),
			expected: &Static{
				Stops: []Stop{
					{
						Id:     "a",
						Parent: &Stop{Id: "b"},
						Type:   StopType_Platform,
					},
					{
						Id: "b",
					},
				},
			},
		},
		{
			desc: "transfer",
			content: newZipBuilder().add(
				"stops.txt",
				"stop_id\na\nb",
			).add(
				"transfers.txt",
				"from_stop_id,to_stop_id,transfer_type,min_transfer_time\na,b,2,300",
			).build(),
			expected: &Static{
				Stops: []Stop{
					{Id: "a"},
					{Id: "b"},
				},
				Transfers: []Transfer{
					{
						From:            &Stop{Id: "a"},
						To:              &Stop{Id: "b"},
						Type:            TransferType_RequiresTime,
						MinTransferTime: ptr(int32(300)),
					},
				},
			},
		},
		{
			desc: "same stop transfer",
			content: newZipBuilder().add(
				"stops.txt",
				"stop_id\na",
			).add(
				"transfers.txt",
				"from_stop_id,to_stop_id\na,a",
			).build(),
			expected: &Static{
				Stops: []Stop{{Id: "a"}},
			},
		},
		{
			desc: "transfer unknown to_id",
			content: newZipBuilder().add(
				"stops.txt",
				"stop_id\na",
			).add(
				"transfers.txt",
				"from_stop_id,to_stop_id\na,b",
			).build(),
			expected: &Static{
				Stops: []Stop{{Id: "a"}},
			},
		},
		{
			desc: "transfer unknown from_id",
			content: newZipBuilder().add(
				"stops.txt",
				"stop_id\nb",
			).add(
				"transfers.txt",
				"from_stop_id,to_stop_id\na,b",
			).build(),
			expected: &Static{
				Stops: []Stop{{Id: "b"}},
			},
		},
		{
			desc: "transfer with route",
			content: newZipBuilder().add(
				"agency.txt",
				"agency_id,agency_name,agency_url,agency_timezone\na,b,c,d",
			).add(
				"routes.txt",
				"route_id,route_type\nroute_id,3\nother_route_id,3",
			).add(
				"stops.txt",
				"stop_id\na\nb",
			).add(
				"transfers.txt",
				"from_stop_id,to_stop_id,from_route_id,to_route_id,transfer_type,min_transfer_time\na,b,route_id,other_route_id,2,300",
			).build(),
			expected: &Static{
				Agencies: []Agency{defaultAgency},
				Routes: []Route{
					defaultRoute,
					otherRoute,
				},
				Stops: []Stop{
					{Id: "a"},
					{Id: "b"},
				},
				Transfers: []Transfer{
					{
						From:            &Stop{Id: "a"},
						To:              &Stop{Id: "b"},
						Type:            TransferType_RequiresTime,
						MinTransferTime: ptr(int32(300)),
						FromRoute:       &defaultRoute,
						ToRoute:         &otherRoute,
					},
				},
			},
		},
		{
			desc: "transfer with trip",
			content: newZipBuilder().add(
				"agency.txt",
				"agency_id,agency_name,agency_url,agency_timezone\na,b,c,d",
			).add(
				"routes.txt",
				"route_id,route_type\nroute_id,3\nother_route_id,3",
			).add(
				"stops.txt",
				"stop_id\na\nb",
			).add(
				"trips.txt",
				"route_id,service_id,trip_id,shape_id",
				"route_id,service_id,trip_id,shape_id",
				"other_route_id,service_id,other_trip_id,other_shape_id",
			).add(
				"calendar.txt",
				"service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date\n"+
					"service_id,0,0,0,0,0,0,0,20220504,20220507",
			).add(
				"transfers.txt",
				"from_stop_id,to_stop_id,from_route_id,to_route_id,from_trip_id,to_trip_id,transfer_type,min_transfer_time\na,b,route_id,other_route_id,trip_id,other_trip_id,2,300",
			).build(),
			expected: &Static{
				Agencies: []Agency{defaultAgency},
				Routes: []Route{
					defaultRoute,
					otherRoute,
				},
				Trips: []ScheduledTrip{
					defaultTrip,
					otherTrip,
				},
				Services: []Service{
					defaultService,
				},
				Stops: []Stop{
					{Id: "a"},
					{Id: "b"},
				},
				Transfers: []Transfer{
					{
						From:            &Stop{Id: "a"},
						To:              &Stop{Id: "b"},
						Type:            TransferType_RequiresTime,
						MinTransferTime: ptr(int32(300)),
						FromRoute:       &defaultRoute,
						ToRoute:         &otherRoute,
						FromTrip:        &defaultTrip,
						ToTrip:          &otherTrip,
					},
				},
			},
		},
		{
			desc: "calendar.txt",
			content: newZipBuilder().add(
				"calendar.txt",
				"service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date\n"+
					"a,1,0,1,0,1,0,1,20220504,20220507",
			).build(),
			expected: &Static{
				Services: []Service{
					{
						Id:        "a",
						Monday:    true,
						Tuesday:   false,
						Wednesday: true,
						Thursday:  false,
						Friday:    true,
						Saturday:  false,
						Sunday:    true,
						StartDate: may4,
						EndDate:   may7,
					},
				},
			},
		},
		{
			desc: "calendar_dates.txt",
			content: newZipBuilder().add(
				"calendar_dates.txt",
				"service_id,date,exception_type\na,20220504,1\na,20220507,2",
			).build(),
			expected: &Static{
				Services: []Service{
					{
						Id:           "a",
						StartDate:    may4,
						EndDate:      may7,
						AddedDates:   []time.Time{may4},
						RemovedDates: []time.Time{may7},
					},
				},
			},
		},
		{
			desc: "booking rules with all fields",
			content: newZipBuilder().add(
				"booking_rules.txt",
				"booking_rule_id,booking_type,prior_notice_duration_min,prior_notice_duration_max,"+
					"prior_notice_last_day,prior_notice_last_time,prior_notice_start_day,prior_notice_start_time,"+
					"prior_notice_service_id,message,pickup_message,drop_off_message,phone_number,info_url,booking_url",
				"br_1,2,,,1,17:00:00,14,00:00:00,weekdays,msg,pmsg,dmsg,555-0100,https://info.example,https://book.example",
				"br_2,1,60,1440,,,,,,,,,,,",
			).build(),
			expected: &Static{
				BookingRules: []BookingRule{
					{
						Id:                   "br_1",
						Type:                 BookingType_PriorDays,
						PriorNoticeLastDay:   ptr(int32(1)),
						PriorNoticeLastTime:  ptr(17 * time.Hour),
						PriorNoticeStartDay:  ptr(int32(14)),
						PriorNoticeStartTime: ptr(time.Duration(0)),
						PriorNoticeServiceId: "weekdays",
						Message:              "msg",
						PickupMessage:        "pmsg",
						DropOffMessage:       "dmsg",
						PhoneNumber:          "555-0100",
						InfoUrl:              "https://info.example",
						BookingUrl:           "https://book.example",
					},
					{
						Id:                     "br_2",
						Type:                   BookingType_SameDay,
						PriorNoticeDurationMin: ptr(int32(60)),
						PriorNoticeDurationMax: ptr(int32(1440)),
					},
				},
			},
		},
		{
			// Real Michigan feeds omit prior_notice_duration_min on type 1 and
			// prior_notice_last_time on type 2. Import them with nils (spec §9.4).
			desc: "booking rules missing conditionally required fields",
			content: newZipBuilder().add(
				"booking_rules.txt",
				"booking_rule_id,booking_type,prior_notice_duration_min,prior_notice_last_day,prior_notice_last_time",
				"br_same_day,1,,,",
				"br_prior_days,2,,7,",
			).build(),
			expected: &Static{
				BookingRules: []BookingRule{
					{Id: "br_same_day", Type: BookingType_SameDay},
					{Id: "br_prior_days", Type: BookingType_PriorDays, PriorNoticeLastDay: ptr(int32(7))},
				},
			},
		},
		{
			desc: "booking rule with unparsable time is kept with a nil time",
			content: newZipBuilder().add(
				"booking_rules.txt",
				"booking_rule_id,booking_type,prior_notice_last_time",
				"br_1,2,soon",
			).build(),
			expected: &Static{
				BookingRules: []BookingRule{{Id: "br_1", Type: BookingType_PriorDays}},
			},
		},
		{
			desc: "booking rule with unparsable type is skipped",
			content: newZipBuilder().add(
				"booking_rules.txt",
				"booking_rule_id,booking_type",
				"br_ok,0",
				"br_bad,9",
			).build(),
			expected: &Static{
				BookingRules: []BookingRule{{Id: "br_ok", Type: BookingType_RealTime}},
				Warnings: []warnings.StaticWarning{
					{
						Kind:          warnings.BookingRuleInvalid{BookingRuleID: "br_bad", Reason: `unparsable booking_type "9"`},
						File:          constants.BookingRulesFile,
						RowNumber:     2,
						RowContent:    []string{"br_bad", "9"},
						HeaderContent: []string{"booking_rule_id", "booking_type"},
					},
				},
			},
		},
		{
			desc: "booking rule with missing id is skipped",
			content: newZipBuilder().add(
				"booking_rules.txt",
				"booking_rule_id,booking_type",
				",1",
			).build(),
			expected: &Static{
				Warnings: []warnings.StaticWarning{
					{
						Kind:          warnings.BookingRuleInvalid{BookingRuleID: "", Reason: "missing values [booking_rule_id]"},
						File:          constants.BookingRulesFile,
						RowNumber:     1,
						RowContent:    []string{"", "1"},
						HeaderContent: []string{"booking_rule_id", "booking_type"},
					},
				},
			},
		},
		{
			desc: "booking rules file with missing columns",
			content: newZipBuilder().add(
				"booking_rules.txt",
				"booking_rule_id\nbr_1",
			).build(),
			expected: &Static{
				Warnings: []warnings.StaticWarning{
					{
						Kind:          warnings.MissingColumns{Columns: []string{"booking_type"}},
						File:          constants.BookingRulesFile,
						RowNumber:     0,
						RowContent:    []string{"booking_rule_id"},
						HeaderContent: []string{"booking_rule_id"},
					},
				},
			},
		},
		{
			desc: "header-only booking rules file yields nil",
			content: newZipBuilder().add(
				"booking_rules.txt",
				"booking_rule_id,booking_type",
			).build(),
			expected: &Static{},
		},
		{
			desc: "locations.geojson polygon",
			content: newZipBuilder().add(
				"locations.geojson",
				`{"type":"FeatureCollection","features":[{"type":"Feature","id":"zone_a",`+
					`"properties":{"stop_name":"Zone A","stop_desc":"North side"},`+
					`"geometry":{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}}]}`,
			).build(),
			expected: &Static{
				Locations: []Location{
					{
						Id:          "zone_a",
						Name:        "Zone A",
						Description: "North side",
						Geometry: LocationGeometry{
							Type:     "Polygon",
							Polygons: [][][][2]float64{{{{0, 0}, {1, 0}, {1, 1}, {0, 0}}}},
							Raw:      json.RawMessage(`{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}`),
						},
					},
				},
			},
		},
		{
			desc: "locations.geojson multipolygon with a hole",
			content: newZipBuilder().add(
				"locations.geojson",
				`{"type":"FeatureCollection","features":[{"type":"Feature","id":"zone_m","properties":{},`+
					`"geometry":{"type":"MultiPolygon","coordinates":[`+
					`[[[0,0],[4,0],[4,4],[0,0]],[[1,1],[2,1],[2,2],[1,1]]],`+
					`[[[10,10],[11,10],[11,11],[10,10]]]]}}]}`,
			).build(),
			expected: &Static{
				Locations: []Location{
					{
						Id: "zone_m",
						Geometry: LocationGeometry{
							Type: "MultiPolygon",
							Polygons: [][][][2]float64{
								{{{0, 0}, {4, 0}, {4, 4}, {0, 0}}, {{1, 1}, {2, 1}, {2, 2}, {1, 1}}},
								{{{10, 10}, {11, 10}, {11, 11}, {10, 10}}},
							},
							Raw: json.RawMessage(`{"type":"MultiPolygon","coordinates":[` +
								`[[[0,0],[4,0],[4,4],[0,0]],[[1,1],[2,1],[2,2],[1,1]]],` +
								`[[[10,10],[11,10],[11,11],[10,10]]]]}`),
						},
					},
				},
			},
		},
		{
			desc: "locations.geojson numeric feature id",
			content: newZipBuilder().add(
				"locations.geojson",
				`{"type":"FeatureCollection","features":[{"type":"Feature","id":42,"properties":{},`+
					`"geometry":{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}}]}`,
			).build(),
			expected: &Static{
				Locations: []Location{
					{
						Id: "42",
						Geometry: LocationGeometry{
							Type:     "Polygon",
							Polygons: [][][][2]float64{{{{0, 0}, {1, 0}, {1, 1}, {0, 0}}}},
							Raw:      json.RawMessage(`{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}`),
						},
					},
				},
			},
		},
		{
			desc: "locations.geojson id falls back to properties",
			content: newZipBuilder().add(
				"locations.geojson",
				`{"type":"FeatureCollection","features":[`+
					`{"type":"Feature","properties":{"id":"from_props"},`+
					`"geometry":{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}},`+
					`{"type":"Feature","properties":{"location_id":"from_draft"},`+
					`"geometry":{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}}]}`,
			).build(),
			expected: &Static{
				Locations: []Location{
					{
						Id: "from_props",
						Geometry: LocationGeometry{
							Type:     "Polygon",
							Polygons: [][][][2]float64{{{{0, 0}, {1, 0}, {1, 1}, {0, 0}}}},
							Raw:      json.RawMessage(`{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}`),
						},
					},
					{
						Id: "from_draft",
						Geometry: LocationGeometry{
							Type:     "Polygon",
							Polygons: [][][][2]float64{{{{0, 0}, {1, 0}, {1, 1}, {0, 0}}}},
							Raw:      json.RawMessage(`{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}`),
						},
					},
				},
			},
		},
		{
			desc: "locations.geojson altitude is dropped",
			content: newZipBuilder().add(
				"locations.geojson",
				`{"type":"FeatureCollection","features":[{"type":"Feature","id":"z","properties":{},`+
					`"geometry":{"type":"Polygon","coordinates":[[[0,0,5],[1,0,5],[1,1,5],[0,0,5]]]}}]}`,
			).build(),
			expected: &Static{
				Locations: []Location{
					{
						Id: "z",
						Geometry: LocationGeometry{
							Type:     "Polygon",
							Polygons: [][][][2]float64{{{{0, 0}, {1, 0}, {1, 1}, {0, 0}}}},
							Raw:      json.RawMessage(`{"type":"Polygon","coordinates":[[[0,0,5],[1,0,5],[1,1,5],[0,0,5]]]}`),
						},
					},
				},
			},
		},
		{
			desc: "locations.geojson BOM is stripped",
			content: newZipBuilder().add(
				"locations.geojson",
				"\xef\xbb\xbf"+`{"type":"FeatureCollection","features":[{"type":"Feature","id":"z","properties":{},`+
					`"geometry":{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}}]}`,
			).build(),
			expected: &Static{
				Locations: []Location{
					{
						Id: "z",
						Geometry: LocationGeometry{
							Type:     "Polygon",
							Polygons: [][][][2]float64{{{{0, 0}, {1, 0}, {1, 1}, {0, 0}}}},
							Raw:      json.RawMessage(`{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}`),
						},
					},
				},
			},
		},
		{
			desc: "locations.geojson unsupported geometry warns",
			content: newZipBuilder().add(
				"locations.geojson",
				`{"type":"FeatureCollection","features":[{"type":"Feature","id":"pt","properties":{},`+
					`"geometry":{"type":"Point","coordinates":[0,0]}}]}`,
			).build(),
			expected: &Static{
				Warnings: []warnings.StaticWarning{
					warnings.NewFileWarning(constants.LocationsGeoJSONFile, warnings.LocationInvalidGeometry{
						LocationID: "pt",
						Reason:     `unsupported geometry type "Point"`,
					}),
				},
			},
		},
		{
			desc: "locations.geojson null geometry warns",
			content: newZipBuilder().add(
				"locations.geojson",
				`{"type":"FeatureCollection","features":[`+
					`{"type":"Feature","id":"null_geom","properties":{},"geometry":null},`+
					`{"type":"Feature","id":"no_geom","properties":{}}]}`,
			).build(),
			expected: &Static{
				Warnings: []warnings.StaticWarning{
					warnings.NewFileWarning(constants.LocationsGeoJSONFile, warnings.LocationInvalidGeometry{
						LocationID: "null_geom",
						Reason:     "feature has no geometry",
					}),
					warnings.NewFileWarning(constants.LocationsGeoJSONFile, warnings.LocationInvalidGeometry{
						LocationID: "no_geom",
						Reason:     "feature has no geometry",
					}),
				},
			},
		},
		{
			desc: "locations.geojson polygon with no rings warns",
			content: newZipBuilder().add(
				"locations.geojson",
				`{"type":"FeatureCollection","features":[{"type":"Feature","id":"empty","properties":{},`+
					`"geometry":{"type":"Polygon","coordinates":[]}}]}`,
			).build(),
			expected: &Static{
				Warnings: []warnings.StaticWarning{
					warnings.NewFileWarning(constants.LocationsGeoJSONFile, warnings.LocationInvalidGeometry{
						LocationID: "empty",
						Reason:     "polygon has no rings",
					}),
				},
			},
		},
		{
			desc: "locations.geojson feature without id warns",
			content: newZipBuilder().add(
				"locations.geojson",
				`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},`+
					`"geometry":{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}}]}`,
			).build(),
			expected: &Static{
				Warnings: []warnings.StaticWarning{
					warnings.NewFileWarning(constants.LocationsGeoJSONFile, warnings.LocationInvalidGeometry{
						Reason: "feature has no id",
					}),
				},
			},
		},
		{
			desc: "location.geojson (singular) is ignored",
			content: newZipBuilder().add(
				"location.geojson",
				`{"type":"FeatureCollection","features":[{"type":"Feature","id":"z","properties":{},`+
					`"geometry":{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}}]}`,
			).build(),
			expected: &Static{},
		},
		{
			desc: "zero-byte locations.geojson is treated as absent",
			content: newZipBuilder().add(
				"locations.geojson", "",
			).build(),
			expected: &Static{},
		},
		{
			desc: "stops.txt is optional when locations.geojson is present",
			content: newZipBuilder().remove("stops.txt").add(
				"locations.geojson",
				`{"type":"FeatureCollection","features":[{"type":"Feature","id":"z","properties":{},`+
					`"geometry":{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}}]}`,
			).build(),
			expected: &Static{
				Locations: []Location{
					{
						Id: "z",
						Geometry: LocationGeometry{
							Type:     "Polygon",
							Polygons: [][][][2]float64{{{{0, 0}, {1, 0}, {1, 1}, {0, 0}}}},
							Raw:      json.RawMessage(`{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}`),
						},
					},
				},
			},
		},
		{
			desc: "location groups with resolved members",
			content: newZipBuilder().add(
				"stops.txt",
				"stop_id\nstop_id\nstop_2",
			).add(
				"location_groups.txt",
				"location_group_id,location_group_name",
				"g1,Group One",
				"g2,",
			).add(
				"location_group_stops.txt",
				"location_group_id,stop_id",
				"g1,stop_id",
				"g1,stop_2",
				"g2,stop_2",
			).build(),
			expected: &Static{
				Stops: []Stop{defaultStop, {Id: "stop_2"}},
				LocationGroups: []LocationGroup{
					{Id: "g1", Name: "Group One", Stops: []*Stop{&defaultStop, {Id: "stop_2"}}},
					{Id: "g2", Stops: []*Stop{{Id: "stop_2"}}},
				},
			},
		},
		{
			desc: "location group with unknown stop warns",
			content: newZipBuilder().add(
				"stops.txt",
				"stop_id\nstop_id",
			).add(
				"location_groups.txt",
				"location_group_id\ng1",
			).add(
				"location_group_stops.txt",
				"location_group_id,stop_id",
				"g1,stop_id",
				"g1,nope",
			).build(),
			expected: &Static{
				Stops: []Stop{defaultStop},
				LocationGroups: []LocationGroup{
					{Id: "g1", Stops: []*Stop{&defaultStop}},
				},
				Warnings: []warnings.StaticWarning{
					{
						Kind:          warnings.LocationGroupUnknownStop{GroupID: "g1", StopID: "nope"},
						File:          constants.LocationGroupStopsFile,
						RowNumber:     2,
						RowContent:    []string{"g1", "nope"},
						HeaderContent: []string{"location_group_id", "stop_id"},
					},
				},
			},
		},
		{
			desc: "membership rows for unknown groups are skipped",
			content: newZipBuilder().add(
				"stops.txt",
				"stop_id\nstop_id",
			).add(
				"location_group_stops.txt",
				"location_group_id,stop_id",
				"missing_group,stop_id",
			).build(),
			expected: &Static{
				Stops: []Stop{defaultStop},
			},
		},
		{
			desc: "header-only location group files yield nil",
			content: newZipBuilder().add(
				"location_groups.txt",
				"location_group_id,location_group_name",
			).add(
				"location_group_stops.txt",
				"location_group_id,stop_id",
			).build(),
			expected: &Static{},
		},
		{
			desc: "location groups file with missing columns",
			content: newZipBuilder().add(
				"location_groups.txt",
				"location_group_name\nGroup",
			).build(),
			expected: &Static{
				Warnings: []warnings.StaticWarning{
					{
						Kind:          warnings.MissingColumns{Columns: []string{"location_group_id"}},
						File:          constants.LocationGroupsFile,
						RowNumber:     0,
						RowContent:    []string{"location_group_name"},
						HeaderContent: []string{"location_group_name"},
					},
				},
			},
		},
		{
			desc: "header-only stops.txt parses to zero stops",
			content: newZipBuilder().add(
				"stops.txt", "stop_id,stop_name,stop_lat,stop_lon",
			).build(),
			expected: &Static{},
		},
		{
			desc: "trip",
			content: newZipBuilder().add(
				"agency.txt",
				"agency_id,agency_name,agency_url,agency_timezone\na,b,c,d",
			).add(
				"routes.txt",
				"route_id,route_type\nroute_id,3",
			).add(
				"stops.txt",
				"stop_id\nstop_id",
			).add(
				"calendar.txt",
				"service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date\n"+
					"service_id,0,0,0,0,0,0,0,20220504,20220507",
			).add(
				"trips.txt",
				"route_id,service_id,trip_id,trip_headsign,trip_short_name,direction_id,block_id,wheelchair_accessible,bikes_allowed\n"+
					"route_id,service_id,a,b,c,1,block_id,0,2",
			).add(
				"stop_times.txt",
				"stop_id,trip_id,arrival_time,departure_time,stop_sequence,stop_headsign,pickup_type,drop_off_type,continuous_pickup,continuous_drop_off,shape_dist_traveled,timepoint\n"+
					"stop_id,a,04:05:06,13:14:15,50,b,0,1,2,3,0.25,1",
			).build(),
			expected: &Static{
				Agencies: []Agency{defaultAgency},
				Routes:   []Route{defaultRoute},
				Services: []Service{defaultService},
				Stops:    []Stop{defaultStop},
				Trips: []ScheduledTrip{
					{
						Route:                &defaultRoute,
						Service:              &defaultService,
						ID:                   "a",
						Headsign:             "b",
						ShortName:            "c",
						DirectionId:          DirectionID_True,
						BlockID:              "block_id",
						WheelchairAccessible: WheelchairBoarding_NotSpecified,
						BikesAllowed:         BikesAllowed_NotAllowed,
						StopTimes: []ScheduledStopTime{
							{
								Stop:                  &defaultStop,
								Headsign:              "b",
								StopSequence:          50,
								ArrivalTime:           4*time.Hour + 5*time.Minute + 6*time.Second,
								DepartureTime:         13*time.Hour + 14*time.Minute + 15*time.Second,
								PickupType:            PickupDropOffPolicy_Yes,
								DropOffType:           PickupDropOffPolicy_No,
								ContinuousPickup:      PickupDropOffPolicy_PhoneAgency,
								ContinuousDropOff:     PickupDropOffPolicy_CoordinateWithDriver,
								ShapeDistanceTraveled: ptr(0.25),
								ExactTimes:            true,
							},
						},
					},
				},
			},
		},
		{
			desc: "stop time with only one of arrival and departure time",
			content: newZipBuilder().add(
				"agency.txt",
				"agency_id,agency_name,agency_url,agency_timezone\na,b,c,d",
			).add(
				"routes.txt",
				"route_id,route_type\nroute_id,3",
			).add(
				"stops.txt",
				"stop_id\nstop_id",
			).add(
				"calendar.txt",
				"service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date\n"+
					"service_id,0,0,0,0,0,0,0,20220504,20220507",
			).add(
				"trips.txt",
				"route_id,service_id,trip_id\nroute_id,service_id,a",
			).add(
				"stop_times.txt",
				"stop_id,trip_id,arrival_time,departure_time,stop_sequence",
				"stop_id,a,04:05:06,,1",
				"stop_id,a,,13:14:15,2",
			).build(),
			expected: &Static{
				Agencies: []Agency{defaultAgency},
				Routes:   []Route{defaultRoute},
				Services: []Service{defaultService},
				Stops:    []Stop{defaultStop},
				Trips: []ScheduledTrip{
					{
						Route:   &defaultRoute,
						Service: &defaultService,
						ID:      "a",
						StopTimes: []ScheduledStopTime{
							{
								Stop:              &defaultStop,
								StopSequence:      1,
								ArrivalTime:       4*time.Hour + 5*time.Minute + 6*time.Second,
								DepartureTime:     4*time.Hour + 5*time.Minute + 6*time.Second,
								ContinuousPickup:  PickupDropOffPolicy_No,
								ContinuousDropOff: PickupDropOffPolicy_No,
								ExactTimes:        true,
							},
							{
								Stop:              &defaultStop,
								StopSequence:      2,
								ArrivalTime:       13*time.Hour + 14*time.Minute + 15*time.Second,
								DepartureTime:     13*time.Hour + 14*time.Minute + 15*time.Second,
								ContinuousPickup:  PickupDropOffPolicy_No,
								ContinuousDropOff: PickupDropOffPolicy_No,
								ExactTimes:        true,
							},
						},
					},
				},
			},
		},
		{
			desc: "trip with safe duration",
			content: newZipBuilder().add(
				"agency.txt",
				"agency_id,agency_name,agency_url,agency_timezone\na,b,c,d",
			).add(
				"routes.txt",
				"route_id,route_type\nroute_id,3",
			).add(
				"calendar.txt",
				"service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date\n"+
					"service_id,0,0,0,0,0,0,0,20220504,20220507",
			).add(
				"trips.txt",
				"route_id,service_id,trip_id,safe_duration_factor,safe_duration_offset",
				"route_id,service_id,a,2,30",
				"route_id,service_id,b,,",
			).build(),
			expected: &Static{
				Agencies: []Agency{defaultAgency},
				Routes:   []Route{defaultRoute},
				Services: []Service{defaultService},
				Trips: []ScheduledTrip{
					{
						Route:              &defaultRoute,
						Service:            &defaultService,
						ID:                 "a",
						SafeDurationFactor: ptr(2.0),
						SafeDurationOffset: ptr(30.0),
					},
					{
						Route:   &defaultRoute,
						Service: &defaultService,
						ID:      "b",
					},
				},
			},
		},
		{
			desc: "stop with spaces in lat/lon",
			content: newZipBuilder().add(
				"stops.txt",
				"stop_id,stop_code,stop_name,stop_desc,zone_id,stop_lon,stop_lat,"+
					"stop_url,location_type,stop_timezone,wheelchair_boarding,platform_code\n"+
					"a,b,c,d,e, 1.5 , 2.5 ,f,1,g,1,h",
			).build(),
			expected: &Static{
				Stops: []Stop{
					{
						Id:                 "a",
						Code:               "b",
						Name:               "c",
						Description:        "d",
						ZoneId:             "e",
						Longitude:          ptr(1.5),
						Latitude:           ptr(2.5),
						Url:                "f",
						Type:               StopType_Station,
						Timezone:           "g",
						WheelchairBoarding: WheelchairBoarding_Possible,
						PlatformCode:       "h",
					},
				},
			},
		},
		{
			desc: "data with BOM",
			content: newZipBuilder().add(
				"stops.txt",
				"\xEF\xBB\xBFstop_id,stop_code,stop_name,stop_desc,zone_id,stop_lon,stop_lat,"+
					"stop_url,location_type,stop_timezone,wheelchair_boarding,platform_code\n"+
					"a,b,c,d,e,1.5,2.5,f,1,g,1,h",
			).build(),
			expected: &Static{
				Stops: []Stop{
					{
						Id:                 "a",
						Code:               "b",
						Name:               "c",
						Description:        "d",
						ZoneId:             "e",
						Longitude:          ptr(1.5),
						Latitude:           ptr(2.5),
						Url:                "f",
						Type:               StopType_Station,
						Timezone:           "g",
						WheelchairBoarding: WheelchairBoarding_Possible,
						PlatformCode:       "h",
					},
				},
			},
		},
		{
			desc: "empty shapes file",
			content: newZipBuilderWithDefaults().add(
				"shapes.txt",
				"shape_id,shape_pt_lat,shape_pt_lon,shape_pt_sequence",
			).add(
				"trips.txt",
				"route_id,service_id,trip_id,shape_id",
				"route_id,service_id,trip_id,shape_id",
			).build(),
			expected: &Static{
				Agencies: []Agency{defaultAgency},
				Routes:   []Route{defaultRoute},
				Services: []Service{defaultService},
				Stops:    []Stop{defaultStop},
				Trips: []ScheduledTrip{
					{
						Route:   &defaultRoute,
						Service: &defaultService,
						ID:      "trip_id",
					},
				},
				Shapes: []Shape{},
			},
		},
		{
			desc: "single point shape",
			content: newZipBuilderWithDefaults().add(
				"shapes.txt",
				"shape_id,shape_pt_lat,shape_pt_lon,shape_pt_sequence",
				"shape_1,1.5,2.5,1",
			).add(
				"trips.txt",
				"route_id,service_id,trip_id,shape_id",
				"route_id,service_id,trip_id,shape_1",
			).build(),
			expected: &Static{
				Agencies: []Agency{defaultAgency},
				Routes:   []Route{defaultRoute},
				Services: []Service{defaultService},
				Stops:    []Stop{defaultStop},
				Trips: []ScheduledTrip{
					{
						Route:   &defaultRoute,
						Service: &defaultService,
						ID:      "trip_id",
						Shape: &Shape{
							ID: "shape_1",
							Points: []ShapePoint{
								{
									Latitude:  1.5,
									Longitude: 2.5,
								},
							},
						},
					},
				},
				Shapes: []Shape{
					{
						ID: "shape_1",
						Points: []ShapePoint{
							{
								Latitude:  1.5,
								Longitude: 2.5,
							},
						},
					},
				},
			},
		},
		{
			desc: "multi point shape",
			content: newZipBuilderWithDefaults().add(
				"shapes.txt",
				"shape_id,shape_pt_lat,shape_pt_lon,shape_pt_sequence",
				"shape_1,1.5,2.5,1",
				"shape_1,2.5,3.5,2",
				"shape_1,3.5,4.5,3",
			).add(
				"trips.txt",
				"route_id,service_id,trip_id,shape_id",
				"route_id,service_id,trip_id,shape_1",
			).build(),
			expected: &Static{
				Agencies: []Agency{defaultAgency},
				Routes:   []Route{defaultRoute},
				Services: []Service{defaultService},
				Stops:    []Stop{defaultStop},
				Trips: []ScheduledTrip{
					{
						Route:   &defaultRoute,
						Service: &defaultService,
						ID:      "trip_id",
						Shape: &Shape{
							ID: "shape_1",
							Points: []ShapePoint{
								{
									Latitude:  1.5,
									Longitude: 2.5,
								},
								{
									Latitude:  2.5,
									Longitude: 3.5,
								},
								{
									Latitude:  3.5,
									Longitude: 4.5,
								},
							},
						},
					},
				},
				Shapes: []Shape{
					{
						ID: "shape_1",
						Points: []ShapePoint{
							{
								Latitude:  1.5,
								Longitude: 2.5,
							},
							{
								Latitude:  2.5,
								Longitude: 3.5,
							},
							{
								Latitude:  3.5,
								Longitude: 4.5,
							},
						},
					},
				},
			},
		},
		{
			desc: "points not in row order",
			content: newZipBuilderWithDefaults().add(
				"shapes.txt",
				"shape_id,shape_pt_lat,shape_pt_lon,shape_pt_sequence",
				"shape_1,3.5,4.5,3",
				"shape_1,2.5,3.5,2",
				"shape_1,1.5,2.5,1",
			).add(
				"trips.txt",
				"route_id,service_id,trip_id,shape_id",
				"route_id,service_id,trip_id,shape_1",
			).build(),
			expected: &Static{
				Agencies: []Agency{defaultAgency},
				Routes:   []Route{defaultRoute},
				Services: []Service{defaultService},
				Stops:    []Stop{defaultStop},
				Trips: []ScheduledTrip{
					{
						Route:   &defaultRoute,
						Service: &defaultService,
						ID:      "trip_id",
						Shape: &Shape{
							ID: "shape_1",
							Points: []ShapePoint{
								{
									Latitude:  1.5,
									Longitude: 2.5,
								},
								{
									Latitude:  2.5,
									Longitude: 3.5,
								},
								{
									Latitude:  3.5,
									Longitude: 4.5,
								},
							},
						},
					},
				},
				Shapes: []Shape{
					{
						ID: "shape_1",
						Points: []ShapePoint{
							{
								Latitude:  1.5,
								Longitude: 2.5,
							},
							{
								Latitude:  2.5,
								Longitude: 3.5,
							},
							{
								Latitude:  3.5,
								Longitude: 4.5,
							},
						},
					},
				},
			},
		},
		{
			desc: "multiple shapes",
			content: newZipBuilderWithDefaults().add(
				"shapes.txt",
				"shape_id,shape_pt_lat,shape_pt_lon,shape_pt_sequence",
				"shape_1,1.5,2.5,1",
				"shape_1,2.5,3.5,2",
				"shape_1,3.5,4.5,3",
				"shape_2,4.5,5.5,1",
				"shape_2,5.5,6.5,2",
			).add(
				"trips.txt",
				"route_id,service_id,trip_id,shape_id",
				"route_id,service_id,trip_1,shape_1",
				"route_id,service_id,trip_2,shape_2",
				"route_id,service_id,trip_3,shape_1",
			).build(),
			expected: &Static{
				Agencies: []Agency{defaultAgency},
				Routes:   []Route{defaultRoute},
				Services: []Service{defaultService},
				Stops:    []Stop{defaultStop},
				Trips: []ScheduledTrip{
					{
						Route:   &defaultRoute,
						Service: &defaultService,
						ID:      "trip_1",
						Shape: &Shape{
							ID: "shape_1",
							Points: []ShapePoint{
								{
									Latitude:  1.5,
									Longitude: 2.5,
								},
								{
									Latitude:  2.5,
									Longitude: 3.5,
								},
								{
									Latitude:  3.5,
									Longitude: 4.5,
								},
							},
						},
					},
					{
						Route:   &defaultRoute,
						Service: &defaultService,
						ID:      "trip_2",
						Shape: &Shape{
							ID: "shape_2",
							Points: []ShapePoint{
								{
									Latitude:  4.5,
									Longitude: 5.5,
								},
								{
									Latitude:  5.5,
									Longitude: 6.5,
								},
							},
						},
					},
					{
						Route:   &defaultRoute,
						Service: &defaultService,
						ID:      "trip_3",
						Shape: &Shape{
							ID: "shape_1",
							Points: []ShapePoint{
								{
									Latitude:  1.5,
									Longitude: 2.5,
								},
								{
									Latitude:  2.5,
									Longitude: 3.5,
								},
								{
									Latitude:  3.5,
									Longitude: 4.5,
								},
							},
						},
					},
				},
				Shapes: []Shape{
					{
						ID: "shape_1",
						Points: []ShapePoint{
							{
								Latitude:  1.5,
								Longitude: 2.5,
							},
							{
								Latitude:  2.5,
								Longitude: 3.5,
							},
							{
								Latitude:  3.5,
								Longitude: 4.5,
							},
						},
					},
					{
						ID: "shape_2",
						Points: []ShapePoint{
							{
								Latitude:  4.5,
								Longitude: 5.5,
							},
							{
								Latitude:  5.5,
								Longitude: 6.5,
							},
						},
					},
				},
			},
		},
		{
			desc: "shape dist traveled",
			content: newZipBuilderWithDefaults().add(
				"shapes.txt",
				"shape_id,shape_pt_lat,shape_pt_lon,shape_pt_sequence,shape_dist_traveled",
				"shape_1,1.5,2.5,1,0",
				"shape_1,2.5,3.5,2,",
				"shape_1,3.5,4.5,3,20",
			).add(
				"trips.txt",
				"route_id,service_id,trip_id,shape_id",
				"route_id,service_id,trip_1,shape_1",
			).build(),
			expected: &Static{
				Agencies: []Agency{defaultAgency},
				Routes:   []Route{defaultRoute},
				Services: []Service{defaultService},
				Stops:    []Stop{defaultStop},
				Trips: []ScheduledTrip{
					{
						Route:   &defaultRoute,
						Service: &defaultService,
						ID:      "trip_1",
						Shape: &Shape{
							ID: "shape_1",
							Points: []ShapePoint{
								{
									Latitude:  1.5,
									Longitude: 2.5,
									Distance:  ptr(float64(0)),
								},
								{
									Latitude:  2.5,
									Longitude: 3.5,
								},
								{
									Latitude:  3.5,
									Longitude: 4.5,
									Distance:  ptr(float64(20)),
								},
							},
						},
					},
				},
				Shapes: []Shape{
					{
						ID: "shape_1",
						Points: []ShapePoint{
							{
								Latitude:  1.5,
								Longitude: 2.5,
								Distance:  ptr(float64(0)),
							},
							{
								Latitude:  2.5,
								Longitude: 3.5,
							},
							{
								Latitude:  3.5,
								Longitude: 4.5,
								Distance:  ptr(float64(20)),
							},
						},
					},
				},
			},
		},
		{
			desc: "empty frequencies",
			content: newZipBuilderWithDefaults().add(
				"frequencies.txt",
				"trip_id,start_time,end_time,headway_secs",
			).build(),
			expected: &Static{
				Agencies: []Agency{defaultAgency},
				Routes:   []Route{defaultRoute},
				Services: []Service{defaultService},
				Stops:    []Stop{defaultStop},
				Trips:    []ScheduledTrip{defaultTrip},
			},
		},
		{
			// csv.New rejects a file with no header row; an empty optional file
			// must be treated as absent rather than aborting the whole parse.
			desc: "zero-byte optional file is treated as absent",
			content: newZipBuilder().add(
				"frequencies.txt", "",
			).build(),
			expected: &Static{},
		},
		{
			desc: "frequencies",
			content: newZipBuilderWithDefaults().add(
				"frequencies.txt",
				"trip_id,start_time,end_time,headway_secs,exact_times",
				"trip_1,00:00:00,01:00:00,180,1",
				"trip_2,01:00:00,02:00:00,300,0",
			).add(
				"trips.txt",
				"route_id,service_id,trip_id",
				"route_id,service_id,trip_1",
				"route_id,service_id,trip_2",
			).build(),
			expected: &Static{
				Agencies: []Agency{defaultAgency},
				Routes:   []Route{defaultRoute},
				Services: []Service{defaultService},
				Stops:    []Stop{defaultStop},
				Trips: []ScheduledTrip{
					{
						ID:      "trip_1",
						Route:   &defaultRoute,
						Service: &defaultService,
						Frequencies: []Frequency{
							{
								StartTime:  0 * time.Hour,
								EndTime:    1 * time.Hour,
								Headway:    3 * time.Minute,
								ExactTimes: ScheduleBased,
							},
						},
					},
					{
						ID:      "trip_2",
						Route:   &defaultRoute,
						Service: &defaultService,
						Frequencies: []Frequency{
							{
								StartTime:  1 * time.Hour,
								EndTime:    2 * time.Hour,
								Headway:    5 * time.Minute,
								ExactTimes: FrequencyBased,
							},
						},
					},
				},
			},
		},
		{
			desc: "frequencies without exact times",
			content: newZipBuilderWithDefaults().add(
				"frequencies.txt",
				"trip_id,start_time,end_time,headway_secs",
				"trip_id,00:00:00,01:00:00,180",
			).add(
				"trips.txt",
				"route_id,service_id,trip_id",
				"route_id,service_id,trip_id",
			).build(),
			expected: &Static{
				Agencies: []Agency{defaultAgency},
				Routes:   []Route{defaultRoute},
				Services: []Service{defaultService},
				Stops:    []Stop{defaultStop},
				Trips: []ScheduledTrip{
					{
						ID:      defaultTrip.ID,
						Route:   &defaultRoute,
						Service: &defaultService,
						Frequencies: []Frequency{
							{
								StartTime:  0 * time.Hour,
								EndTime:    1 * time.Hour,
								Headway:    3 * time.Minute,
								ExactTimes: FrequencyBased,
							},
						},
					},
				},
			},
		},
		{
			desc: "frequencies with blank exact times",
			content: newZipBuilderWithDefaults().add(
				"frequencies.txt",
				"trip_id,start_time,end_time,headway_secs,exact_times",
				"trip_id,00:00:00,01:00:00,180,",
			).build(),
			expected: &Static{
				Agencies: []Agency{defaultAgency},
				Routes:   []Route{defaultRoute},
				Services: []Service{defaultService},
				Stops:    []Stop{defaultStop},
				Trips: []ScheduledTrip{
					{
						ID:      defaultTrip.ID,
						Route:   &defaultRoute,
						Service: &defaultService,
						Frequencies: []Frequency{
							{
								StartTime:  0 * time.Hour,
								EndTime:    1 * time.Hour,
								Headway:    3 * time.Minute,
								ExactTimes: FrequencyBased,
							},
						},
					},
				},
			},
		},
		{
			desc: "frequencies with missing trip",
			content: newZipBuilderWithDefaults().add(
				"frequencies.txt",
				"trip_id,start_time,end_time,headway_secs",
				"some_trip,00:00:00,01:00:00,180",
			).build(),
			expected: &Static{
				Agencies: []Agency{defaultAgency},
				Routes:   []Route{defaultRoute},
				Services: []Service{defaultService},
				Stops:    []Stop{defaultStop},
				Trips:    []ScheduledTrip{defaultTrip},
			},
		},
		{
			desc: "trip with multiple frequencies",
			content: newZipBuilderWithDefaults().add(
				"frequencies.txt",
				"trip_id,start_time,end_time,headway_secs,exact_times",
				"trip_id,00:00:00,01:00:00,180,1",
				"trip_id,01:00:00,02:00:00,300,0",
			).build(),
			expected: &Static{
				Agencies: []Agency{defaultAgency},
				Routes:   []Route{defaultRoute},
				Services: []Service{defaultService},
				Stops:    []Stop{defaultStop},
				Trips: []ScheduledTrip{
					{
						ID:      defaultTrip.ID,
						Route:   &defaultRoute,
						Service: &defaultService,
						Frequencies: []Frequency{
							{
								StartTime:  0 * time.Hour,
								EndTime:    1 * time.Hour,
								Headway:    3 * time.Minute,
								ExactTimes: ScheduleBased,
							},
							{
								StartTime:  1 * time.Hour,
								EndTime:    2 * time.Hour,
								Headway:    5 * time.Minute,
								ExactTimes: FrequencyBased,
							},
						},
					},
				},
			},
		},
		{
			desc: "stop inherits parent wheelchair boarding, accessible",
			content: newZipBuilder().add(
				"stops.txt",
				"stop_id,location_type,parent_station,wheelchair_boarding",
				"a,,b,",
				"b,1,,1",
			).build(),
			opts: ParseStaticOptions{
				InheritWheelchairBoarding: true,
			},
			expected: &Static{
				Stops: []Stop{
					{
						Id:                 "a",
						Type:               StopType_Platform,
						Parent:             &Stop{Id: "b", WheelchairBoarding: WheelchairBoarding_Possible, Type: StopType_Station},
						WheelchairBoarding: WheelchairBoarding_Possible,
					},
					{
						Id:                 "b",
						Type:               StopType_Station,
						WheelchairBoarding: WheelchairBoarding_Possible,
					},
				},
			},
		},
		{
			desc: "stop inherits parent wheelchair boarding, inaccessible",
			content: newZipBuilder().add(
				"stops.txt",
				"stop_id,location_type,parent_station,wheelchair_boarding",
				"a,,b,",
				"b,1,,2",
			).build(),
			opts: ParseStaticOptions{
				InheritWheelchairBoarding: true,
			},
			expected: &Static{
				Stops: []Stop{
					{
						Id:                 "a",
						Type:               StopType_Platform,
						Parent:             &Stop{Id: "b", WheelchairBoarding: WheelchairBoarding_NotPossible, Type: StopType_Station},
						WheelchairBoarding: WheelchairBoarding_NotPossible,
					},
					{
						Id:                 "b",
						Type:               StopType_Station,
						WheelchairBoarding: WheelchairBoarding_NotPossible,
					},
				},
			},
		},
		{
			desc: "stop doesn't inherit parent wheelchair boarding when option is false",
			content: newZipBuilder().add(
				"stops.txt",
				"stop_id,location_type,parent_station,wheelchair_boarding",
				"a,,b,",
				"b,1,,1",
			).build(),
			opts: ParseStaticOptions{
				InheritWheelchairBoarding: false,
			},
			expected: &Static{
				Stops: []Stop{
					{
						Id:                 "a",
						Type:               StopType_Platform,
						Parent:             &Stop{Id: "b", WheelchairBoarding: WheelchairBoarding_Possible, Type: StopType_Station},
						WheelchairBoarding: WheelchairBoarding_NotSpecified,
					},
					{
						Id:                 "b",
						Type:               StopType_Station,
						WheelchairBoarding: WheelchairBoarding_Possible,
					},
				},
			},
		},
		{
			desc: "stop doesn't inherit parent wheelchair boarding by default",
			content: newZipBuilder().add(
				"stops.txt",
				"stop_id,location_type,parent_station,wheelchair_boarding",
				"a,,b,",
				"b,1,,1",
			).build(),
			expected: &Static{
				Stops: []Stop{
					{
						Id:                 "a",
						Type:               StopType_Platform,
						Parent:             &Stop{Id: "b", WheelchairBoarding: WheelchairBoarding_Possible, Type: StopType_Station},
						WheelchairBoarding: WheelchairBoarding_NotSpecified,
					},
					{
						Id:                 "b",
						Type:               StopType_Station,
						WheelchairBoarding: WheelchairBoarding_Possible,
					},
				},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			actual, err := ParseStatic(tc.content, tc.opts)
			if err != nil {
				t.Errorf("error when parsing: %s", err)
			}
			if diff := cmp.Diff(actual, tc.expected); diff != "" {
				t.Errorf("not the same: \ngot: %+v != \nwant:%+v\ndiff:%s", actual, tc.expected, diff)
			}
		})
	}
}

type zipBuilder struct {
	m map[string]string
}

func newZipBuilder() *zipBuilder {
	return (&zipBuilder{m: map[string]string{}}).add(
		"agency.txt", "agency_id,agency_name,agency_url,agency_timezone",
	).add(
		"routes.txt", "route_id,route_type",
	).add(
		"stops.txt", "stop_id",
	).add(
		"transfers.txt", "from_stop_id,to_stop_id",
	).add(
		"trips.txt", "route_id,service_id,trip_id",
	).add(
		"stop_times.txt", "stop_id,trip_id,stop_sequence",
	)
}

func newZipBuilderWithDefaults() *zipBuilder {
	return newZipBuilder().add(
		"agency.txt",
		"agency_id,agency_name,agency_url,agency_timezone\na,b,c,d",
	).add(
		"routes.txt",
		"route_id,route_type\nroute_id,3",
	).add(
		"stops.txt",
		"stop_id\nstop_id",
	).add(
		"calendar.txt",
		"service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date\n"+
			"service_id,0,0,0,0,0,0,0,20220504,20220507",
	).add(
		"stop_times.txt",
		"stop_id,trip_id,arrival_time,departure_time,stop_sequence,stop_headsign\n"+
			"stop_id,a,04:05:06,13:14:15,50,b",
	).add(
		"trips.txt",
		"route_id,service_id,trip_id\nroute_id,service_id,trip_id")
}

func (z *zipBuilder) add(fileName string, fileContent ...string) *zipBuilder {
	z.m[fileName] = strings.Join(fileContent, "\n")
	return z
}

func (z *zipBuilder) remove(fileName string) *zipBuilder {
	delete(z.m, fileName)
	return z
}

func (z *zipBuilder) build() []byte {
	var b bytes.Buffer
	zipWriter := zip.NewWriter(&b)
	for fileName, fileContent := range z.m {
		fileWriter, err := zipWriter.Create(fileName)
		if err != nil {
			panic(err)
		}
		if _, err := io.Copy(fileWriter, bytes.NewBufferString(fileContent)); err != nil {
			panic(err)
		}
	}
	if err := zipWriter.Close(); err != nil {
		panic(err)
	}
	return b.Bytes()
}

func ptr[T any](t T) *T {
	return &t
}

func TestParseStatic_PickupDropOffTypeDefaults(t *testing.T) {
	const header = "stop_id,trip_id,arrival_time,departure_time,stop_sequence"

	for _, tc := range []struct {
		desc            string
		stopTimes       []string
		expectedPickup  PickupDropOffPolicy
		expectedDropOff PickupDropOffPolicy
	}{
		{
			desc: "columns absent",
			stopTimes: []string{
				header,
				"stop_id,trip_id,04:05:06,13:14:15,50",
			},
			expectedPickup:  PickupDropOffPolicy_Yes,
			expectedDropOff: PickupDropOffPolicy_Yes,
		},
		{
			desc: "columns present but blank",
			stopTimes: []string{
				header + ",pickup_type,drop_off_type",
				"stop_id,trip_id,04:05:06,13:14:15,50,,",
			},
			expectedPickup:  PickupDropOffPolicy_Yes,
			expectedDropOff: PickupDropOffPolicy_Yes,
		},
		{
			desc: "explicit zero",
			stopTimes: []string{
				header + ",pickup_type,drop_off_type",
				"stop_id,trip_id,04:05:06,13:14:15,50,0,0",
			},
			expectedPickup:  PickupDropOffPolicy_Yes,
			expectedDropOff: PickupDropOffPolicy_Yes,
		},
		{
			desc: "explicit one is still not allowed",
			stopTimes: []string{
				header + ",pickup_type,drop_off_type",
				"stop_id,trip_id,04:05:06,13:14:15,50,1,1",
			},
			expectedPickup:  PickupDropOffPolicy_No,
			expectedDropOff: PickupDropOffPolicy_No,
		},
		{
			desc: "restricted policies are preserved",
			stopTimes: []string{
				header + ",pickup_type,drop_off_type",
				"stop_id,trip_id,04:05:06,13:14:15,50,2,3",
			},
			expectedPickup:  PickupDropOffPolicy_PhoneAgency,
			expectedDropOff: PickupDropOffPolicy_CoordinateWithDriver,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			content := newZipBuilderWithDefaults().add("stop_times.txt", tc.stopTimes...).build()

			static, err := ParseStatic(content, ParseStaticOptions{})
			if err != nil {
				t.Fatalf("ParseStatic() got error %v, want nil", err)
			}
			if len(static.Trips) != 1 || len(static.Trips[0].StopTimes) != 1 {
				t.Fatalf("got %d trips, want exactly 1 trip holding exactly 1 stop time", len(static.Trips))
			}

			stopTime := static.Trips[0].StopTimes[0]
			if stopTime.PickupType != tc.expectedPickup {
				t.Errorf("PickupType = %v, want %v", stopTime.PickupType, tc.expectedPickup)
			}
			if stopTime.DropOffType != tc.expectedDropOff {
				t.Errorf("DropOffType = %v, want %v", stopTime.DropOffType, tc.expectedDropOff)
			}
		})
	}
}

func TestParseStatic_ContinuousPickupDropOffDefaultToNo(t *testing.T) {
	// GTFS defaults continuous_pickup/continuous_drop_off to 1 (No), unlike
	// pickup_type/drop_off_type which default to 0 (Yes). This pins that
	// difference so the two fields do not get "fixed" together. Both the
	// absent-column case and the present-but-blank-cell case must default to No.
	for _, tc := range []struct {
		desc      string
		stopTimes []string
	}{
		{
			desc: "absent columns default to No",
			stopTimes: []string{
				"stop_id,trip_id,arrival_time,departure_time,stop_sequence",
				"stop_id,trip_id,04:05:06,13:14:15,50",
			},
		},
		{
			desc: "blank cells default to No",
			stopTimes: []string{
				"stop_id,trip_id,arrival_time,departure_time,stop_sequence,continuous_pickup,continuous_drop_off",
				"stop_id,trip_id,04:05:06,13:14:15,50,,",
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			content := newZipBuilderWithDefaults().add("stop_times.txt", tc.stopTimes...).build()

			static, err := ParseStatic(content, ParseStaticOptions{})
			if err != nil {
				t.Fatalf("ParseStatic() got error %v, want nil", err)
			}

			stopTime := static.Trips[0].StopTimes[0]
			if stopTime.ContinuousPickup != PickupDropOffPolicy_No {
				t.Errorf("ContinuousPickup = %v, want %v", stopTime.ContinuousPickup, PickupDropOffPolicy_No)
			}
			if stopTime.ContinuousDropOff != PickupDropOffPolicy_No {
				t.Errorf("ContinuousDropOff = %v, want %v", stopTime.ContinuousDropOff, PickupDropOffPolicy_No)
			}
		})
	}
}

func TestParseStatic_UnknownTripIDIsSkipped(t *testing.T) {
	// The second row switches to a trip_id that trips.txt does not define.
	// Before the fix this dereferenced a nil *ScheduledTrip while presizing
	// its StopTimes slice.
	content := newZipBuilderWithDefaults().add(
		"stop_times.txt",
		"stop_id,trip_id,stop_sequence",
		"stop_id,trip_id,1",
		"stop_id,ghost,2",
	).build()

	static, err := ParseStatic(content, ParseStaticOptions{})
	if err != nil {
		t.Fatalf("ParseStatic() got error %v, want nil", err)
	}
	if len(static.Trips) != 1 {
		t.Fatalf("got %d trips, want 1", len(static.Trips))
	}
	if got := len(static.Trips[0].StopTimes); got != 1 {
		t.Errorf("got %d stop times on trip_id, want 1 (the ghost row must be dropped)", got)
	}
}

func TestParseStatic_StopsFileRequiredWithoutLocations(t *testing.T) {
	content := newZipBuilder().remove("stops.txt").build()

	_, err := ParseStatic(content, ParseStaticOptions{})
	if err == nil {
		t.Fatal("ParseStatic() got nil error, want an error because stops.txt is missing")
	}
	if want := `no "stops.txt" file in GTFS static feed`; err.Error() != want {
		t.Errorf("ParseStatic() error = %q, want %q", err.Error(), want)
	}
}

func TestParseStatic_MalformedLocationsFileIsAnError(t *testing.T) {
	for _, tc := range []struct {
		desc    string
		content string
	}{
		{desc: "not json", content: "{"},
		{desc: "not a feature collection", content: `{"type":"Feature","features":[]}`},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			content := newZipBuilder().add("locations.geojson", tc.content).build()
			_, err := ParseStatic(content, ParseStaticOptions{})
			if err == nil {
				t.Fatal("ParseStatic() got nil error, want an error")
			}
			if !strings.HasPrefix(err.Error(), `failed to read "locations.geojson"`) {
				t.Errorf("ParseStatic() error = %q, want it to start with the file name", err.Error())
			}
		})
	}
}
