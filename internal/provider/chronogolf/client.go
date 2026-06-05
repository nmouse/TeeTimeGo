package chronogolf

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/owner/teetime/internal/provider"
)

const baseURL = "https://www.chronogolf.com"

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// Club is a Chronogolf club returned by the search API.
type Club struct {
	UUID string
	Name string
	Slug string
	Lat  float64
	Lng  float64
}

type Client struct{}

// New returns a new Chronogolf client.
func New() *Client { return &Client{} }

// Name implements provider.Provider.
func (c *Client) Name() string { return "Chronogolf" }

// SearchClubs returns all published Chronogolf clubs within radiusKm of lat/lng.
func (c *Client) SearchClubs(ctx context.Context, lat, lng, radiusKm float64) ([]Club, error) {
	params := url.Values{}
	params.Set("location[lat]", fmt.Sprintf("%f", lat))
	params.Set("location[lon]", fmt.Sprintf("%f", lng))
	params.Set("location[distance]", fmt.Sprintf("%.0f", radiusKm))
	params.Set("published", "true")
	params.Set("page", "1")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/marketplace/v2/search?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("building chronogolf search request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chronogolf search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("chronogolf search returned status %d", resp.StatusCode)
	}

	var raw []struct {
		UUID     string `json:"uuid"`
		Name     string `json:"name"`
		Slug     string `json:"slug"`
		Location struct {
			Lat float64 `json:"lat"`
			Lon float64 `json:"lon"`
		} `json:"location"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decoding chronogolf search response: %w", err)
	}

	clubs := make([]Club, 0, len(raw))
	for _, r := range raw {
		clubs = append(clubs, Club{
			UUID: r.UUID,
			Name: r.Name,
			Slug: r.Slug,
			Lat:  r.Location.Lat,
			Lng:  r.Location.Lon,
		})
	}
	return clubs, nil
}

// GetTeeTimes implements provider.Provider. scheduleID is the club slug.
// The API paginates at 24 results; we fetch all pages until an empty response.
func (c *Client) GetTeeTimes(ctx context.Context, slug string, date time.Time, players, holes int) ([]provider.TeeTime, error) {
	courseUUIDs, err := c.courseUUIDs(ctx, slug)
	if err != nil {
		return nil, err
	}
	if len(courseUUIDs) == 0 {
		return nil, nil
	}

	localDate := date.Format("2006-01-02")

	type rawTeetime struct {
		StartsAt   string `json:"starts_at"`
		MaxPlayers int    `json:"max_player_size"`
		Course     struct {
			Holes int `json:"holes"`
		} `json:"course"`
		DefaultPrice *struct {
			GreenFee      float64 `json:"green_fee"`
			Subtotal      float64 `json:"subtotal"`
			BookableHoles int     `json:"bookable_holes"`
		} `json:"default_price"`
	}

	var times []provider.TeeTime
	for page := 1; page <= 20; page++ {
		params := url.Values{}
		params.Set("start_date", date.Format("2006-01-02"))
		params.Set("free_slots", fmt.Sprintf("%d", players))
		params.Set("course_ids", strings.Join(courseUUIDs, ","))
		params.Set("page", fmt.Sprintf("%d", page))
		if holes == 9 || holes == 18 {
			params.Set("holes", fmt.Sprintf("%d", holes))
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/marketplace/v2/teetimes?"+params.Encode(), nil)
		if err != nil {
			return nil, fmt.Errorf("building chronogolf teetimes request: %w", err)
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("chronogolf teetimes request: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("chronogolf teetimes returned status %d", resp.StatusCode)
		}

		var result struct {
			Teetimes []rawTeetime `json:"teetimes"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("decoding chronogolf teetimes response: %w", decodeErr)
		}
		if len(result.Teetimes) == 0 {
			break
		}

		for _, tt := range result.Teetimes {
			t, err := time.Parse(time.RFC3339, tt.StartsAt)
			if err != nil {
				continue
			}
			t = t.In(time.Local)
			// UTC timestamps can cross midnight; only keep times on the requested local date.
			if t.Format("2006-01-02") != localDate {
				continue
			}
			var price float64
			holesCount := tt.Course.Holes
			if tt.DefaultPrice != nil {
				price = tt.DefaultPrice.Subtotal
				if price == 0 {
					price = tt.DefaultPrice.GreenFee
				}
				if tt.DefaultPrice.BookableHoles > 0 {
					holesCount = tt.DefaultPrice.BookableHoles
				}
			}
			times = append(times, provider.TeeTime{
				Time:    t,
				Players: tt.MaxPlayers,
				Holes:   holesCount,
				Price:   price,
				BookURL: fmt.Sprintf("%s/club/%s", baseURL, slug),
			})
		}
	}
	return times, nil
}

func (c *Client) courseUUIDs(ctx context.Context, slug string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/marketplace/v2/clubs/"+slug, nil)
	if err != nil {
		return nil, fmt.Errorf("building chronogolf club request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chronogolf club request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("chronogolf club returned status %d", resp.StatusCode)
	}

	var club struct {
		Courses []struct {
			UUID string `json:"uuid"`
		} `json:"courses"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&club); err != nil {
		return nil, fmt.Errorf("decoding chronogolf club response: %w", err)
	}

	uuids := make([]string, len(club.Courses))
	for i, course := range club.Courses {
		uuids[i] = course.UUID
	}
	return uuids, nil
}
