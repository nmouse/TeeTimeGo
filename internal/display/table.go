package display

import (
	"fmt"
	"io"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/owner/teetime/internal/provider"
)

type CourseResult struct {
	CourseName  string
	DistMiles   float64
	TeeTimes    []provider.TeeTime
	ForeUPFound bool   // true when a schedule ID was detected, even if no times returned
	Error       string // non-empty when scrape or fetch failed
}

func PrintTable(w io.Writer, results []CourseResult) {
	sort.Slice(results, func(i, j int) bool {
		return results[i].DistMiles < results[j].DistMiles
	})

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "COURSE\tDIST\tTIME\tSPOTS\tHOLES\tPRICE\t")
	fmt.Fprintln(tw, "------\t----\t----\t-----\t-----\t-----\t")

	for _, r := range results {
		dist := fmt.Sprintf("%.1fmi", r.DistMiles)
		if len(r.TeeTimes) == 0 {
			status := "no ForeUP booking found"
			if r.Error != "" {
				status = r.Error
			} else if r.ForeUPFound {
				status = "no times available"
			}
			fmt.Fprintf(tw, "%s\t%s\t--\t--\t--\t%s\t\n", r.CourseName, dist, status)
			continue
		}

		// Sort tee times chronologically.
		sort.Slice(r.TeeTimes, func(i, j int) bool {
			return r.TeeTimes[i].Time.Before(r.TeeTimes[j].Time)
		})

		for i, tt := range r.TeeTimes {
			name := ""
			d := ""
			if i == 0 {
				name = r.CourseName
				d = dist
			}
			timeStr := tt.Time.Format(time.Kitchen)
			spots := fmt.Sprintf("%d", tt.Players)
			holes := fmt.Sprintf("%d", tt.Holes)
			price := "--"
			if tt.Price > 0 {
				price = fmt.Sprintf("$%.2f", tt.Price)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t\n", name, d, timeStr, spots, holes, price)
		}
	}
	tw.Flush()
}
