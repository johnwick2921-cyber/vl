package main

import (
	"context"
	"errors"
	"time"
)

// flatwait.go — the update must never swap the add-on while the friend has an
// open position. The app polls the friend bot's positions endpoint until flat,
// up to a deadline; then it gives up for today (never mid-trade).

// Position is the single field the flat check needs.
type Position struct {
	Quantity float64 `json:"quantity"`
}

// positionsProvider reports the friend's current open quantity (0 = flat).
// The real implementation queries the friend bot; tests use a fake.
type positionsProvider func(ctx context.Context) (float64, error)

// errNotFlat is returned when the flat wait times out — the caller must DEFER
// the update (never show it as an error) and continue.
var errNotFlat = errors.New("not flat before the deadline — update deferred")

// waitUntilFlat polls p every interval until it reports 0 (flat) or the context
// deadline passes. It returns nil once flat, and errNotFlat if the deadline
// passes with an open position (the caller leaves the current version alone).
func waitUntilFlat(ctx context.Context, p positionsProvider, interval time.Duration) error {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		q, err := p(ctx)
		if err == nil && q == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return errNotFlat
		case <-t.C:
		}
	}
}
