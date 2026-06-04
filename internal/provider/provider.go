package provider

import (
	"context"
	"time"
)

type TeeTime struct {
	Time    time.Time
	Players int
	Holes   int
	Price   float64
	BookURL string
}

type Provider interface {
	Name() string
	GetTeeTimes(ctx context.Context, scheduleID string, date time.Time, players, holes int) ([]TeeTime, error)
}
