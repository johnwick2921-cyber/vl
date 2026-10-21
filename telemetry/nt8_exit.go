// W117 a3 — the ordered worker's final-failure net for a close that met SQLite
// lock contention. The exit is never dropped: the broker's price is parked
// (reconcile orphan close), the flat signal is dropped, the receipt is
// persisted, and RetryPendingNT8Exits applies it later.
package telemetry

import (
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// NT8ExitBusyParksTotal counts exit receipts parked after ApplyNT8Exit
// exhausted its bounded busy retries (the receipt is persisted and retried by
// RetryPendingNT8Exits — this is the never-dropped net, not a loss).
var NT8ExitBusyParksTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "vl_nt8_exit_busy_parks_total",
		Help: "NT8 exit receipts parked (priced + persisted) after ApplyNT8Exit busy retries exhausted.",
	},
)

// NT8ExitBusyParkPersistFailuresTotal counts the subset of parked exits whose
// small receipt-persist write also failed (the 2-minute priced park is then the
// only net until the next frame).
var NT8ExitBusyParkPersistFailuresTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "vl_nt8_exit_busy_park_persist_failures_total",
		Help: "NT8 exit receipts whose post-busy small persist write failed.",
	},
)

// IncNT8ExitBusyPark records one exit receipt parked after busy retries.
func IncNT8ExitBusyPark() { NT8ExitBusyParksTotal.Inc() }

// IncNT8ExitBusyParkPersistFailure records one failed post-busy persist write.
func IncNT8ExitBusyParkPersistFailure() { NT8ExitBusyParkPersistFailuresTotal.Inc() }

// r24 ADDON-CLOSE-ROUTING — every position_close_rejected frame the bot
// receives: NT8 refused an exit/flatten (e.g. "no market data"), or the AddOn
// (build 2026-10-14-a+) REFUSED a close it would not route (the named account
// not found or not SIM, an ambiguous legacy close, an unknown instrument). The
// position is STILL OPEN in each case; close_sync.go logs the 🚨 line, and
// this counts it, so a refusal is never only a log line.
var NT8CloseRejectedTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "vl_nt8_close_rejected_total",
		Help: "position_close_rejected frames received: an NT8 or AddOn refusal of a close (the position stays open).",
	},
)

var nt8CloseRejected atomic.Int64

// IncNT8CloseRejected records one received position_close_rejected frame.
func IncNT8CloseRejected() {
	nt8CloseRejected.Add(1)
	NT8CloseRejectedTotal.Inc()
}

// NT8CloseRejectedCount is the process-lifetime count IncNT8CloseRejected kept.
func NT8CloseRejectedCount() int64 { return nt8CloseRejected.Load() }
