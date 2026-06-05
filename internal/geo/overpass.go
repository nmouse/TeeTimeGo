package geo

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
)

const overpassURL = "https://overpass-api.de/api/interpreter"

type Course struct {
	Name    string
	Lat     float64
	Lng     float64
	Website string
	// DistMiles is populated by FindCourses relative to the search origin.
	DistMiles float64
}

// FindCourses returns golf courses within radiusMiles of origin.
func FindCourses(ctx context.Context, origin LatLng, radiusMiles float64) ([]Course, error) {
	radiusMeters := radiusMiles * 1609.344
	query := fmt.Sprintf(`[out:json];(node["leisure"="golf_course"](around:%.0f,%f,%f);way["leisure"="golf_course"](around:%.0f,%f,%f););out center tags;`,
		radiusMeters, origin.Lat, origin.Lng,
		radiusMeters, origin.Lat, origin.Lng,
	)

	params := url.Values{}
	params.Set("data", query)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, overpassURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("building overpass request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "teetime-cli/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("overpass request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Elements []struct {
			Type   string             `json:"type"`
			Lat    float64            `json:"lat"`
			Lon    float64            `json:"lon"`
			Center *struct {
				Lat float64 `json:"lat"`
				Lon float64 `json:"lon"`
			} `json:"center"`
			Tags map[string]string `json:"tags"`
		} `json:"elements"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding overpass response: %w", err)
	}

	seen := map[string]bool{}
	var courses []Course
	for _, el := range result.Elements {
		lat, lng := el.Lat, el.Lon
		if el.Center != nil {
			lat, lng = el.Center.Lat, el.Center.Lon
		}

		name := el.Tags["name"]
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true

		website := el.Tags["website"]
		if website == "" {
			website = el.Tags["url"]
		}
		if website == "" {
			website = el.Tags["contact:website"]
		}

		courses = append(courses, Course{
			Name:      name,
			Lat:       lat,
			Lng:       lng,
			Website:   website,
			DistMiles: HaversineMiles(origin.Lat, origin.Lng, lat, lng),
		})
	}
	return courses, nil
}

// HaversineMiles returns the great-circle distance in miles between two lat/lng points.
func HaversineMiles(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 3958.8 // Earth radius in miles
	dLat := (lat2 - lat1) * math.Pi / 180
	dLng := (lng2 - lng1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLng/2)*math.Sin(dLng/2)
	return R * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
