package chronogolf

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// testClient returns a Client wired to the given test server, using the
// provided timezone for local-date comparisons.
func testClient(ts *httptest.Server, loc *time.Location) *Client {
	return &Client{apiBase: ts.URL, loc: loc}
}

// stubServer returns a test server that handles the clubs and teetimes endpoints.
// pages maps page number to a slice of starts_at RFC3339 strings to return.
// Pages not present in the map return an empty teetimes list.
func stubServer(t *testing.T, pages map[int][]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/marketplace/v2/clubs/test-club":
			json.NewEncoder(w).Encode(map[string]any{
				"courses": []map[string]any{{"uuid": "test-uuid"}},
			})
		case r.URL.Path == "/marketplace/v2/teetimes":
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			if page == 0 {
				page = 1
			}
			var tts []map[string]any
			for _, sa := range pages[page] {
				tts = append(tts, map[string]any{
					"starts_at":       sa,
					"max_player_size": 4,
					"course":          map[string]any{"holes": 9},
					"default_price": map[string]any{
						"subtotal": 19.0, "green_fee": 19.0, "bookable_holes": 9,
					},
				})
			}
			json.NewEncoder(w).Encode(map[string]any{"teetimes": tts})
		default:
			http.NotFound(w, r)
		}
	}))
}

// searchStubServer returns a test server for SearchClubs pagination tests.
// clubPages maps page number to a slice of club slugs to return on that page.
func searchStubServer(t *testing.T, clubPages map[int][]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/marketplace/v2/search" {
			http.NotFound(w, r)
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page == 0 {
			page = 1
		}
		var clubs []map[string]any
		for _, slug := range clubPages[page] {
			clubs = append(clubs, map[string]any{
				"uuid": slug + "-uuid",
				"name": slug,
				"slug": slug,
				"location": map[string]any{"lat": 40.76, "lon": -111.89},
			})
		}
		json.NewEncoder(w).Encode(clubs)
	}))
}

func TestSearchClubs_PaginatesAllPages(t *testing.T) {
	page1 := []string{"course-a", "course-b", "course-c"}
	page2 := []string{"course-d", "course-e"}
	ts := searchStubServer(t, map[int][]string{1: page1, 2: page2})
	defer ts.Close()

	c := testClient(ts, time.UTC)
	got, err := c.SearchClubs(context.Background(), 40.76, -111.89, 80)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := len(page1) + len(page2)
	if len(got) != want {
		t.Errorf("got %d clubs, want %d (pages 1+2 combined)", len(got), want)
	}
}

func TestSearchClubs_StopsOnEmptyFirstPage(t *testing.T) {
	ts := searchStubServer(t, map[int][]string{})
	defer ts.Close()

	c := testClient(ts, time.UTC)
	got, err := c.SearchClubs(context.Background(), 40.76, -111.89, 80)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d clubs, want 0", len(got))
	}
}

func TestGetTeeTimes_PaginatesAllPages(t *testing.T) {
	// Page 1 and 2 each have times; page 3 is empty → loop must stop.
	page1 := []string{
		"2026-06-11T12:00:00Z", "2026-06-11T12:10:00Z", "2026-06-11T12:20:00Z",
	}
	page2 := []string{
		"2026-06-11T15:00:00Z", "2026-06-11T15:10:00Z",
	}
	ts := stubServer(t, map[int][]string{1: page1, 2: page2})
	defer ts.Close()

	c := testClient(ts, time.UTC)
	date, _ := time.Parse("2006-01-02", "2026-06-11")
	got, err := c.GetTeeTimes(context.Background(), "test-club", date, 1, 9)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(page1)+len(page2) {
		t.Errorf("got %d tee times, want %d (pages 1+2 combined)", len(got), len(page1)+len(page2))
	}
}

func TestGetTeeTimes_StopsOnEmptyFirstPage(t *testing.T) {
	ts := stubServer(t, map[int][]string{}) // all pages empty
	defer ts.Close()

	c := testClient(ts, time.UTC)
	date, _ := time.Parse("2006-01-02", "2026-06-11")
	got, err := c.GetTeeTimes(context.Background(), "test-club", date, 1, 9)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d tee times, want 0", len(got))
	}
}

func TestGetTeeTimes_LocalDateFilter(t *testing.T) {
	denver, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Skip("America/Denver timezone not available:", err)
	}

	tests := []struct {
		name     string
		startsAt string
		wantKept bool
	}{
		{
			name:     "morning UTC same day",
			startsAt: "2026-06-11T12:00:00Z", // 06:00 MDT June 11 — keep
			wantKept: true,
		},
		{
			name:     "UTC midnight crossing: still June 11 in Denver",
			startsAt: "2026-06-12T00:00:00Z", // 18:00 MDT June 11 — keep
			wantKept: true,
		},
		{
			name:     "clearly next local day",
			startsAt: "2026-06-12T07:00:00Z", // 01:00 MDT June 12 — discard
			wantKept: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ts := stubServer(t, map[int][]string{1: {tc.startsAt}})
			defer ts.Close()

			c := testClient(ts, denver)
			date, _ := time.Parse("2006-01-02", "2026-06-11")
			got, err := c.GetTeeTimes(context.Background(), "test-club", date, 1, 9)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			kept := len(got) == 1
			if kept != tc.wantKept {
				t.Errorf("starts_at %s: kept=%v, want %v", tc.startsAt, kept, tc.wantKept)
			}
		})
	}
}

// TestGetTeeTimes_LocalDateFilter_BugRegression guards against the bug where
// localDate was computed as date.In(time.Local), which shifted midnight-UTC
// dates back one day in negative-offset timezones (e.g. MDT), causing all
// tee times for the requested date to be filtered out.
func TestGetTeeTimes_LocalDateFilter_BugRegression(t *testing.T) {
	denver, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Skip("America/Denver timezone not available:", err)
	}

	// Any time on the requested date should be returned.
	// With the buggy code, localDate would be "2026-06-10" in Denver,
	// so this 06:00 MDT time (12:00 UTC) would be wrongly filtered out.
	ts := stubServer(t, map[int][]string{
		1: {"2026-06-11T12:00:00Z"}, // 06:00 MDT June 11
	})
	defer ts.Close()

	c := testClient(ts, denver)
	date, _ := time.Parse("2006-01-02", "2026-06-11")
	got, err := c.GetTeeTimes(context.Background(), "test-club", date, 1, 9)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) == 0 {
		t.Error("no tee times returned: localDate was probably computed in local timezone, shifting the date back one day")
	}
}
