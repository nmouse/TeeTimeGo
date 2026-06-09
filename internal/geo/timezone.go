package geo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const timezoneAPIURL = "https://timeapi.io/api/timezone/coordinate"

// TimezoneFor returns the IANA timezone for the given coordinates by calling
// timeapi.io. Falls back to time.Local if the lookup fails.
func TimezoneFor(ctx context.Context, ll LatLng) (*time.Location, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s?latitude=%f&longitude=%f", timezoneAPIURL, ll.Lat, ll.Lng)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building timezone request: %w", err)
	}
	req.Header.Set("User-Agent", "teetime-cli/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("timezone request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("timezone API returned status %d", resp.StatusCode)
	}

	var result struct {
		TimeZone string `json:"timeZone"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding timezone response: %w", err)
	}
	if result.TimeZone == "" {
		return nil, fmt.Errorf("timezone API returned empty timezone")
	}

	loc, err := time.LoadLocation(result.TimeZone)
	if err != nil {
		return nil, fmt.Errorf("loading timezone %q: %w", result.TimeZone, err)
	}
	return loc, nil
}
