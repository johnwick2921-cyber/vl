package ninjatrader

import (
	"strings"
	"sync"
)

// ── MENTOR-SEED-STATE (release #23, N1) — a history refill is ANNOUNCED ──────
//
// The mentor evaluator seeds its history-derived state (trigger lines, the
// swing line, the 1m EMAs, the key-level deletion) from the STORE once, at
// trader construction, and then advances it incrementally — it never re-reads
// a bar it has already folded in. Two things put OLDER 1m bars into the store
// after that:
//
//   - the replay hold releasing the AddOn's subscribe-time history to the store
//     (replayHold.resolve). At the 10-14 08:22 boot the seed read the store at
//     08:22:40 and the hold released the 03:44–07:47 hole at 08:23:00 — the bot
//     then traded on a 1h trigger line seeded from a 12-minute 07:00 bucket;
//   - a bar hole the gap-backfill tracker measured FILLED (barHoleTracker).
//
// Both now bump a per-symbol 1m generation here. The trader's mentor tick reads
// it and, when it moved past the generation its evaluator was seeded at,
// re-runs the state-only half of the seed from the store before ticking. The
// generation is process-wide (every trader on the symbol re-seeds itself) and
// pull-based — no callback reaches into a trader from the persist goroutine.

type historyRefillGen struct {
	gen  uint64
	why  string
	rows int
}

var (
	historyRefillMu   sync.Mutex
	historyRefillGens = map[string]historyRefillGen{}
)

// NoteHistoryRefill records that `rows` older 1m bars for symbol reached the
// store (`why` names the event, READ by the re-seed's log line). Only the 1m
// series moves the generation — the mentor seeds from 1m alone.
func NoteHistoryRefill(symbol, tf string, rows int, why string) {
	if rows <= 0 || !strings.EqualFold(strings.TrimSpace(tf), "1m") {
		return
	}
	k := strings.ToUpper(strings.TrimSpace(symbol))
	historyRefillMu.Lock()
	g := historyRefillGens[k]
	g.gen++
	g.why = why
	g.rows = rows
	historyRefillGens[k] = g
	historyRefillMu.Unlock()
}

// HistoryRefillGeneration returns the symbol's 1m refill generation (0 = no
// refill this process) and the last refill's description.
func HistoryRefillGeneration(symbol string) (gen uint64, why string) {
	k := strings.ToUpper(strings.TrimSpace(symbol))
	historyRefillMu.Lock()
	defer historyRefillMu.Unlock()
	g := historyRefillGens[k]
	return g.gen, g.why
}
