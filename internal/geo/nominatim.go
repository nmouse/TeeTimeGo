package geo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

const nominatimURL = "https://nominatim.openstreetmap.org/search"

type LatLng struct {
	Lat float64
	Lng float64
}

func Geocode(ctx context.Context, location string) (LatLng, error) {
	params := url.Values{}
	params.Set("q", location)
	params.Set("format", "json")
	params.Set("limit", "1")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nominatimURL+"?"+params.Encode(), nil)
	if err != nil {
		return LatLng{}, fmt.Errorf("building nominatim request: %w", err)
	}
	req.Header.Set("User-Agent", "teetime-cli/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return LatLng{}, fmt.Errorf("nominatim request: %w", err)
	}
	defer resp.Body.Close()

	var results []struct {
		Lat string `json:"lat"`
		Lon string `json:"lon"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return LatLng{}, fmt.Errorf("decoding nominatim response: %w", err)
	}
	if len(results) == 0 {
		return LatLng{}, fmt.Errorf("no results for location %q", location)
	}

	var ll LatLng
	fmt.Sscanf(results[0].Lat, "%f", &ll.Lat)
	fmt.Sscanf(results[0].Lon, "%f", &ll.Lng)
	return ll, nil
}
