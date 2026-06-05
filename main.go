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
	flag.Parse()

	if *location == "" || *dateStr == "" {
		fmt.Fprintln(os.Stderr, "usage: teetime --location <location> --date <YYYY-MM-DD> [--radius <miles>] [--players <n>] [--holes <9|18>]")
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

	ctx := context.Background()

	fmt.Printf("Searching for golf courses near %q within %.0f miles on %s...\n\n",
		*location, *radius, date.Format("January 2, 2006"))

	ll, err := geo.Geocode(ctx, *location)
	if err != nil {
		fmt.Fprintf(os.Stderr, "geocoding error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Location resolved: %.4f, %.4f\n", ll.Lat, ll.Lng)

	// Semaphore: cap concurrency at 10 across both pipelines.
	sem := make(chan struct{}, 10)
	var mu sync.Mutex
	var results []display.CourseResult

	// --- Pipeline 1: Overpass + ForeUP ---

	courses, err := geo.FindCourses(ctx, ll, *radius)
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
					times, err := foreupClient.GetTeeTimes(cctx, scheduleID, date, *players, *holes)
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

	// --- Pipeline 2: Chronogolf ---

	cgClient := chronogolf.New()
	radiusKm := *radius * 1.609344
	cgClubs, err := cgClient.SearchClubs(ctx, ll.Lat, ll.Lng, radiusKm)
	if err != nil {
		fmt.Fprintf(os.Stderr, "chronogolf search error: %v\n", err)
	} else if len(cgClubs) > 0 {
		fmt.Printf("Found %d course(s) on Chronogolf. Fetching tee times...\n\n", len(cgClubs))

		var wg sync.WaitGroup
		for _, club := range cgClubs {
			wg.Add(1)
			club := club
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				result := display.CourseResult{
					CourseName:    club.Name,
					DistMiles:     geo.HaversineMiles(ll.Lat, ll.Lng, club.Lat, club.Lng),
					ProviderFound: true,
				}

				cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
				defer cancel()

				times, err := cgClient.GetTeeTimes(cctx, club.Slug, date, *players, *holes)
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

	display.PrintTable(os.Stdout, deduplicate(results))
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
