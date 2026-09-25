package gtfs

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/OneBusAway/go-gtfs/constants"
	"github.com/OneBusAway/go-gtfs/warnings"
)

// Location is a GeoJSON Feature from locations.geojson (Polygon or MultiPolygon only).
type Location struct {
	Id          string
	Name        string // properties.stop_name
	Description string // properties.stop_desc
	Geometry    LocationGeometry
}

// LocationGeometry normalises Polygon and MultiPolygon to one shape.
// Polygons[p][r][i] = [lon, lat]; ring 0 is the exterior, rings 1.. are holes.
type LocationGeometry struct {
	Type     string // "Polygon" | "MultiPolygon"
	Polygons [][][][2]float64
	Raw      json.RawMessage // the geometry object verbatim
}

// geoJSONFeatureCollection mirrors the subset of RFC 7946 that
// locations.geojson uses. Ids and properties stay raw because feeds disagree
// on their JSON types.
type geoJSONFeatureCollection struct {
	Type     string           `json:"type"`
	Features []geoJSONFeature `json:"features"`
}

type geoJSONFeature struct {
	ID         json.RawMessage            `json:"id"`
	Properties map[string]json.RawMessage `json:"properties"`
	Geometry   json.RawMessage            `json:"geometry"`
}

type geoJSONGeometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// parseLocations decodes the content of locations.geojson. Malformed JSON or a
// top-level object that is not a FeatureCollection is an error; an unusable
// individual Feature is skipped with a LocationInvalidGeometry warning.
func parseLocations(content []byte) ([]Location, []warnings.StaticWarning, error) {
	var collection geoJSONFeatureCollection
	if err := json.Unmarshal(bytes.TrimPrefix(content, utf8BOM), &collection); err != nil {
		return nil, nil, err
	}
	if collection.Type != "FeatureCollection" {
		return nil, nil, fmt.Errorf("expected a FeatureCollection, got %q", collection.Type)
	}

	var locations []Location
	var w []warnings.StaticWarning
	for _, feature := range collection.Features {
		id := locationID(feature)
		if id == "" {
			w = append(w, warnings.NewFileWarning(constants.LocationsGeoJSONFile, warnings.LocationInvalidGeometry{
				Reason: "feature has no id",
			}))
			continue
		}
		geometry, droppedHoles, err := parseLocationGeometry(feature.Geometry)
		if err != nil {
			w = append(w, warnings.NewFileWarning(constants.LocationsGeoJSONFile, warnings.LocationInvalidGeometry{
				LocationID: id,
				Reason:     err.Error(),
			}))
			continue
		}
		for _, reason := range droppedHoles {
			w = append(w, warnings.NewFileWarning(constants.LocationsGeoJSONFile, warnings.LocationInvalidGeometry{
				LocationID: id,
				Reason:     reason,
			}))
		}
		locations = append(locations, Location{
			Id:          id,
			Name:        jsonScalarString(feature.Properties["stop_name"]),
			Description: jsonScalarString(feature.Properties["stop_desc"]),
			Geometry:    geometry,
		})
	}
	return locations, w, nil
}

// locationID returns the Feature id, falling back to the draft-era
// properties.id and properties.location_id placements. A JSON number id is
// rendered with its literal text.
func locationID(feature geoJSONFeature) string {
	candidates := []json.RawMessage{
		feature.ID,
		feature.Properties["id"],
		feature.Properties["location_id"],
	}
	for _, raw := range candidates {
		if id := jsonScalarString(raw); id != "" {
			return id
		}
	}
	return ""
}

// jsonScalarString renders a JSON string or number as a Go string. Anything
// else (absent, null, bool, object, array) renders as "".
func jsonScalarString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String()
	}
	return ""
}

// minRingPositions is the RFC 7946 §3.1.6 minimum for a linear ring: three
// distinct positions plus the closing repeat of the first.
const minRingPositions = 4

// parseLocationGeometry decodes a Polygon or MultiPolygon geometry object.
// droppedHoles holds one reason per degenerate hole that was discarded while
// keeping the rest of the geometry.
func parseLocationGeometry(raw json.RawMessage) (geometry LocationGeometry, droppedHoles []string, err error) {
	if len(raw) == 0 {
		return LocationGeometry{}, nil, fmt.Errorf("feature has no geometry")
	}
	var decoded geoJSONGeometry
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return LocationGeometry{}, nil, err
	}
	var polygons [][][][]float64
	switch decoded.Type {
	case "":
		// `"geometry": null` decodes to the zero value.
		return LocationGeometry{}, nil, fmt.Errorf("feature has no geometry")
	case "Polygon":
		var rings [][][]float64
		if err := json.Unmarshal(decoded.Coordinates, &rings); err != nil {
			return LocationGeometry{}, nil, err
		}
		polygons = [][][][]float64{rings}
	case "MultiPolygon":
		if err := json.Unmarshal(decoded.Coordinates, &polygons); err != nil {
			return LocationGeometry{}, nil, err
		}
	default:
		return LocationGeometry{}, nil, fmt.Errorf("unsupported geometry type %q", decoded.Type)
	}
	normalised, droppedHoles, err := normalisePolygons(polygons)
	if err != nil {
		return LocationGeometry{}, nil, err
	}
	return LocationGeometry{Type: decoded.Type, Polygons: normalised, Raw: raw}, droppedHoles, nil
}

// normalisePolygons truncates every position to [lon, lat] (RFC 7946 allows a
// third altitude element). An empty polygon or an exterior ring too short to
// enclose an area is an error. A hole that is too short is dropped and its
// reason returned, because the exterior ring alone is still a usable zone.
func normalisePolygons(polygons [][][][]float64) ([][][][2]float64, []string, error) {
	if len(polygons) == 0 {
		return nil, nil, fmt.Errorf("geometry has no polygons")
	}
	result := make([][][][2]float64, 0, len(polygons))
	var droppedHoles []string
	for _, rings := range polygons {
		if len(rings) == 0 {
			return nil, nil, fmt.Errorf("polygon has no rings")
		}
		if len(rings[0]) < minRingPositions {
			return nil, nil, fmt.Errorf("exterior ring has %d positions; at least %d are required", len(rings[0]), minRingPositions)
		}
		polygon := make([][][2]float64, 0, len(rings))
		for ringIndex, ring := range rings {
			points, err := normaliseRing(ring)
			if err != nil {
				return nil, nil, err
			}
			if ringIndex > 0 && len(points) < minRingPositions {
				droppedHoles = append(droppedHoles, fmt.Sprintf("dropped hole with %d positions; at least %d are required", len(points), minRingPositions))
				continue
			}
			polygon = append(polygon, points)
		}
		result = append(result, polygon)
	}
	return result, droppedHoles, nil
}

// normaliseRing truncates every position of a ring to [lon, lat].
func normaliseRing(ring [][]float64) ([][2]float64, error) {
	points := make([][2]float64, 0, len(ring))
	for _, position := range ring {
		if len(position) < 2 {
			return nil, fmt.Errorf("position %v has fewer than 2 coordinates", position)
		}
		points = append(points, [2]float64{position[0], position[1]})
	}
	return points, nil
}
