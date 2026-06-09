package chronogolf

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/owner/teetime/internal/provider"
)

const productionURL = "https://www.chronogolf.com"

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// Club is a Chronogolf club returned by the search API.
type Club struct {
	UUID string
	Name string
	Slug string
	Lat  float64
	Lng  float64
}

// Client is a Chronogolf marketplace API client.
type Client struct {
	apiBase string         // overridden in tests
	loc     *time.Location // timezone for local-date comparisons; defaults to time.Local
}

// New returns a new Chronogolf client using loc for timestamp conversion.
func New(loc *time.Location) *Client { return &Client{apiBase: productionURL, loc: loc} }

// Name implements provider.Provider.
func (c *Client) Name() string { return "Chronogolf" }

// get performs a GET request with retry on HTTP 429. It respects context cancellation.
func (c *Client) get(ctx context.Context, rawURL string) (*http.Response, error) {
	do := func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/json")
		return http.DefaultClient.Do(req)
	}

	resp, err := do()
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		return resp, nil
	}
	resp.Body.Close()

	delay := 3 * time.Second
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if secs, e := strconv.Atoi(ra); e == nil && secs > 0 {
			delay = time.Duration(secs) * time.Second
		}
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(delay):
	}
	return do()
}

// SearchClubs returns all published Chronogolf clubs within radiusKm of lat/lng.
// The API paginates at 25 results; we fetch all pages until an empty response.
func (c *Client) SearchClubs(ctx context.Context, lat, lng, radiusKm float64) ([]Club, error) {
	type rawClub struct {
		UUID                 string `json:"uuid"`
		Name                 string `json:"name"`
		Slug                 string `json:"slug"`
		OnlineBookingEnabled bool   `json:"online_booking_enabled"`
		Location             struct {
			Lat float64 `json:"lat"`
			Lon float64 `json:"lon"`
		} `json:"location"`
	}

	var clubs []Club
	for page := 1; page <= 20; page++ {
		params := url.Values{}
		params.Set("location[lat]", fmt.Sprintf("%f", lat))
		params.Set("location[lon]", fmt.Sprintf("%f", lng))
		params.Set("location[distance]", fmt.Sprintf("%.0f", radiusKm))
		params.Set("published", "true")
		params.Set("page", fmt.Sprintf("%d", page))

		resp, err := c.get(ctx, c.apiBase+"/marketplace/v2/search?"+params.Encode())
		if err != nil {
			return nil, fmt.Errorf("chronogolf search request: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("chronogolf search returned status %d", resp.StatusCode)
		}

		var raw []rawClub
		decodeErr := json.NewDecoder(resp.Body).Decode(&raw)
		resp.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("decoding chronogolf search response: %w", decodeErr)
		}
		if len(raw) == 0 {
			break
		}

		for _, r := range raw {
			if !r.OnlineBookingEnabled {
				continue
			}
			clubs = append(clubs, Club{
				UUID: r.UUID,
				Name: r.Name,
				Slug: r.Slug,
				Lat:  r.Location.Lat,
				Lng:  r.Location.Lon,
			})
		}
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

		resp, err := c.get(ctx, c.apiBase+"/marketplace/v2/teetimes?"+params.Encode())
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
			t = t.In(c.loc)
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
				BookURL: fmt.Sprintf("%s/club/%s", productionURL, slug),
			})
		}
	}
	return times, nil
}

func (c *Client) courseUUIDs(ctx context.Context, slug string) ([]string, error) {
	resp, err := c.get(ctx, c.apiBase+"/marketplace/v2/clubs/"+slug)
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
