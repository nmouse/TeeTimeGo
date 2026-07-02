package foreup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/owner/teetime/internal/provider"
)

const bookingURL = "https://foreupsoftware.com/index.php/api/booking/times"

type Client struct {
	loc *time.Location
}

func New(loc *time.Location) *Client { return &Client{loc: loc} }

func (c *Client) Name() string { return "ForeUP" }

func (c *Client) GetTeeTimes(ctx context.Context, scheduleID string, date time.Time, players, holes int) ([]provider.TeeTime, error) {
	params := url.Values{}
	params.Set("time", "all")
	params.Set("date", date.Format("01-02-2006"))
	if holes != 0 {
		params.Set("holes", fmt.Sprintf("%d", holes))
	}
	params.Set("players", fmt.Sprintf("%d", players))
	params.Set("schedule_id", scheduleID)
	params.Set("schedule_ids[]", scheduleID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, bookingURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("building foreup request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Referer", fmt.Sprintf("https://foreupsoftware.com/index.php/booking/%s", scheduleID))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("foreup request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("foreup returned status %d", resp.StatusCode)
	}

	var slots []struct {
		Time       string  `json:"time"`
		Players    int     `json:"available_spots"`
		Holes      int     `json:"holes"`
		GreenFee   float64 `json:"green_fee"`
		CartFee    float64 `json:"cart_fee"`
		ScheduleID json.Number `json:"schedule_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&slots); err != nil {
		return nil, fmt.Errorf("decoding foreup response: %w", err)
	}

	var times []provider.TeeTime
	for _, s := range slots {
		t, err := time.ParseInLocation("2006-01-02 15:04", s.Time, c.loc)
		if err != nil {
			// Try alternate format
			t, err = time.ParseInLocation("2006-01-02T15:04:05", s.Time, c.loc)
			if err != nil {
				continue
			}
		}
		price := s.GreenFee + s.CartFee
		sid := s.ScheduleID.String()
		if sid == "" || sid == "0" {
			sid = scheduleID
		}
		// The booking URL format is correct; a 404 means the scraped schedule ID
		// is stale — the course may have migrated to a new ForeUP schedule.
		times = append(times, provider.TeeTime{
			Time:    t,
			Players: s.Players,
			Holes:   s.Holes,
			Price:   price,
			BookURL: fmt.Sprintf("https://foreupsoftware.com/index.php/booking/%s#/teetimes", sid),
		})
	}
	return times, nil
}
