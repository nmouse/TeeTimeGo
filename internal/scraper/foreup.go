package scraper

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// scheduleIDPatterns matches ForeUP schedule_id values embedded in HTML/JS.
var scheduleIDPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)schedule_ids?\s*(?:\[\])?\s*[=:]\s*["\[]?(\d+)`),
	regexp.MustCompile(`(?i)data-schedule-id=["\'](\d+)`),
	regexp.MustCompile(`(?i)foreupsoftware\.com[^"']*[?&]schedule_id[s]?(?:\[\])?=(\d+)`),
	regexp.MustCompile(`(?i)"schedule_id"\s*:\s*"?(\d+)"?`),
	// Matches booking URLs like /index.php/booking/{courseID}/{scheduleID}
	regexp.MustCompile(`(?i)foreupsoftware\.com/index\.php/booking/\d+/(\d+)`),
}

// ForeUpScheduleID fetches websiteURL and attempts to extract a ForeUP schedule_id.
// Returns ("", nil) if the site doesn't appear to use ForeUP.
func ForeUpScheduleID(ctx context.Context, websiteURL string) (string, error) {
	if websiteURL == "" {
		return "", nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, websiteURL, nil)
	if err != nil {
		return "", fmt.Errorf("building request for %s: %w", websiteURL, err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; teetime-cli/1.0)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching %s: %w", websiteURL, err)
	}
	defer resp.Body.Close()

	// Read up to 512 KB — enough to find embedded booking scripts without loading entire page.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", fmt.Errorf("reading body from %s: %w", websiteURL, err)
	}

	// Quick check: skip if ForeUP not referenced at all.
	if !strings.Contains(strings.ToLower(string(body)), "foreupsoftware") {
		return "", nil
	}

	for _, re := range scheduleIDPatterns {
		if m := re.FindSubmatch(body); len(m) > 1 {
			return string(m[1]), nil
		}
	}
	return "", nil
}
