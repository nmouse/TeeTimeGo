package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/owner/teetime/internal/display"
	"github.com/owner/teetime/internal/geo"
	"github.com/owner/teetime/internal/provider/chronogolf"
	"github.com/owner/teetime/internal/provider/foreup"
	"github.com/owner/teetime/internal/scraper"
)

func main() {
	location := flag.String("location", "", "Location to search (address, city, or zip) [required]")
	dateStr := flag.String("date", "", "Date to search in YYYY-MM-DD format [required]")
	radius := flag.Float64("radius", 25, "Search radius in miles")
	players := flag.Int("players", 1, "Number of players")
	holes := flag.Int("holes", 18, "Number of holes (9 or 18)")
	fromStr := flag.String("from", "", "Earliest tee time to show in HH:MM (24h)")
	toStr := flag.String("to", "", "Latest tee time to show in HH:MM (24h)")
	web := flag.Bool("web", false, "Open results in browser instead of printing to terminal")
	flag.Parse()

	if *location == "" || *dateStr == "" {
		fmt.Fprintln(os.Stderr, "usage: teetime --location <location> --date <YYYY-MM-DD> [--radius <miles>] [--players <n>] [--holes <9|18>] [--from HH:MM] [--to HH:MM] [--web]")
		os.Exit(1)
	}

	date, err := time.Parse("2006-01-02", *dateStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid date %q: must be YYYY-MM-DD\n", *dateStr)
		os.Exit(1)
	}
	if *holes != 9 && *holes != 18 {
		fmt.Fprintln(os.Stderr, "--holes must be 9 or 18")
		os.Exit(1)
	}

	fromMins, toMins, err := parseTimeRange(*fromStr, *toStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	fmt.Printf("Searching for golf courses near %q within %.0f miles on %s...\n\n",
		*location, *radius, date.Format("January 2, 2006"))

	ll, err := geo.Geocode(ctx, *location)
	if err != nil {
		fmt.Fprintf(os.Stderr, "geocoding error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Location resolved: %.4f, %.4f\n", ll.Lat, ll.Lng)

	// In web mode fetch with players=1 to get all available tee times;
	// --players only pre-populates the UI spots filter.
	fetchPlayers := *players
	if *web {
		fetchPlayers = 1
	}
	results := fetchResults(ctx, ll, *radius, date, fetchPlayers, *holes)
	final := deduplicate(filterByTime(results, fromMins, toMins))

	if *web {
		fetchFn := func(d time.Time) ([]display.CourseResult, error) {
			r := fetchResults(ctx, ll, *radius, d, 1, *holes)
			return deduplicate(filterByTime(r, fromMins, toMins)), nil
		}
		defaults := display.WebUIDefaults{From: *fromStr, To: *toStr, Players: *players}
		if err := display.ServeWeb(final, *location, date, defaults, fetchFn); err != nil {
			fmt.Fprintf(os.Stderr, "web server error: %v\n", err)
			os.Exit(1)
		}
	} else {
		display.PrintTable(os.Stdout, final)
	}
}

func fetchResults(ctx context.Context, ll geo.LatLng, radius float64, date time.Time, players, holes int) []display.CourseResult {
	sem := make(chan struct{}, 10)
	var mu sync.Mutex
	var results []display.CourseResult

	// Pipeline 1: Overpass + ForeUP
	courses, err := geo.FindCourses(ctx, ll, radius)
	if err != nil {
		fmt.Fprintf(os.Stderr, "course discovery error: %v\n", err)
	} else if len(courses) > 0 {
		fmt.Printf("Found %d course(s) via OpenStreetMap. Checking for ForeUP booking...\n\n", len(courses))

		foreupClient := foreup.New()
		var wg sync.WaitGroup
		for _, c := range courses {
			wg.Add(1)
			c := c
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				result := display.CourseResult{
					CourseName: c.Name,
					DistMiles:  c.DistMiles,
				}

				cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
				defer cancel()

				scheduleID, err := scraper.ForeUpScheduleID(cctx, c.Website)
				if err != nil {
					result.Error = fmt.Sprintf("scrape error: %v", err)
					mu.Lock()
					results = append(results, result)
					mu.Unlock()
					return
				}

				if scheduleID != "" {
					result.ProviderFound = true
					times, err := foreupClient.GetTeeTimes(cctx, scheduleID, date, players, holes)
					if err != nil {
						result.Error = fmt.Sprintf("API error: %v", err)
					} else {
						result.TeeTimes = times
					}
				}

				mu.Lock()
				results = append(results, result)
				mu.Unlock()
			}()
		}
		wg.Wait()
	}

	// Pipeline 2: Chronogolf
	cgClient := chronogolf.New()
	radiusKm := radius * 1.609344
	cgClubs, err := cgClient.SearchClubs(ctx, ll.Lat, ll.Lng, radiusKm)
	if err != nil {
		fmt.Fprintf(os.Stderr, "chronogolf search error: %v\n", err)
	} else if len(cgClubs) > 0 {
		fmt.Printf("Found %d course(s) on Chronogolf. Fetching tee times...\n\n", len(cgClubs))

		cgSem := make(chan struct{}, 2) // Chronogolf rate-limits aggressively; keep concurrency low
		var wg sync.WaitGroup
		for _, club := range cgClubs {
			wg.Add(1)
			club := club
			go func() {
				defer wg.Done()
				cgSem <- struct{}{}
				defer func() { <-cgSem }()

				result := display.CourseResult{
					CourseName:    club.Name,
					DistMiles:     geo.HaversineMiles(ll.Lat, ll.Lng, club.Lat, club.Lng),
					ProviderFound: true,
				}

				cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
				defer cancel()

				times, err := cgClient.GetTeeTimes(cctx, club.Slug, date, players, holes)
				if err != nil {
					result.Error = fmt.Sprintf("API error: %v", err)
				} else {
					result.TeeTimes = times
				}

				mu.Lock()
				results = append(results, result)
				mu.Unlock()
			}()
		}
		wg.Wait()
	}

	return results
}

// deduplicate merges results with the same course name, preferring entries with tee times
// over those with only a provider found, over those with no booking at all.
func deduplicate(results []display.CourseResult) []display.CourseResult {
	best := make(map[string]display.CourseResult, len(results))
	for _, r := range results {
		key := strings.ToLower(strings.TrimSpace(r.CourseName))
		existing, ok := best[key]
		if !ok || resultScore(r) > resultScore(existing) {
			best[key] = r
		}
	}
	deduped := make([]display.CourseResult, 0, len(best))
	for _, r := range best {
		deduped = append(deduped, r)
	}
	return deduped
}

func resultScore(r display.CourseResult) int {
	if len(r.TeeTimes) > 0 {
		return 2
	}
	if r.ProviderFound {
		return 1
	}
	return 0
}

// parseTimeRange parses --from and --to flag values into minutes-from-midnight.
// Returns -1 for an unset bound. Returns an error if either value is malformed.
func parseTimeRange(from, to string) (fromMins, toMins int, err error) {
	fromMins, toMins = -1, -1
	parse := func(s, flag string) (int, error) {
		t, err := time.Parse("15:04", s)
		if err != nil {
			return 0, fmt.Errorf("invalid --%s %q: must be HH:MM (e.g. 08:00)", flag, s)
		}
		return t.Hour()*60 + t.Minute(), nil
	}
	if from != "" {
		if fromMins, err = parse(from, "from"); err != nil {
			return
		}
	}
	if to != "" {
		if toMins, err = parse(to, "to"); err != nil {
			return
		}
	}
	return
}

// filterByTime removes tee times outside [fromMins, toMins] (minutes-from-midnight).
// A bound of -1 means no limit on that side.
func filterByTime(results []display.CourseResult, fromMins, toMins int) []display.CourseResult {
	if fromMins == -1 && toMins == -1 {
		return results
	}
	for i := range results {
		filtered := results[i].TeeTimes[:0]
		for _, tt := range results[i].TeeTimes {
			m := tt.Time.Hour()*60 + tt.Time.Minute()
			if fromMins != -1 && m < fromMins {
				continue
			}
			if toMins != -1 && m > toMins {
				continue
			}
			filtered = append(filtered, tt)
		}
		results[i].TeeTimes = filtered
	}
	return results
}
