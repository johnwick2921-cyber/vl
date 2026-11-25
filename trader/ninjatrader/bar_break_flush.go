package ninjatrader

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"vl/kernel"
	"vl/logger"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// ── m424 (release #50, CHART-PIPELINE-RESCAN-1024 §4 P1) — the session's LAST
// bar reaches the store without waiting for the next session ────────────────
//
// What happened [A]: a bar is persisted only when a frame arrives
// (tcp_server.go drainBarIngest → ClosedCacheTail). The AddOn finalises bar N
// only when N+1 starts forming, and nothing arrives 16:00–17:00 CT or over a
// weekend. So 10-23's 15:59 1m (and 15:55 5m …) stayed in the ring only, and a
// restart in the break lost it for good.
//
// P1a. breakFlusher watches the ONE CME calendar (kernel.IsCMEOpen). The first
// tick after the market closes, and every tick whose ring tail changed while it
// stays closed, hands the ring's last closed LIVE bars for every tf to the SAME
// persister closure a frame would have reached (newBarPersister), so the
// contract stamp, the source vocabulary (live if the ring bar is Final,
// live_partial if not, mixed stays mixed) and the upsert rule are the ones
// production already pins. A ring bar still labelled historical is never
// offered (BreakFlushTail): it is a replay and belongs to the hold. The upsert
// rule keeps every better row: a live_partial never displaces a final live row.
//
// P1b. A restart in the break leaves the replay in barReplayHold with no live
// frame to release it. Design choice (the smaller safe one): do NOT persist the
// hold (a new table, a format, a migration, a staleness rule, for rows the NT8
// replay re-delivers at every boot anyway). Instead, while the market is CLOSED
// the hold is judged against the evidence already in hand — the LIVE rows the
// store holds for the same minutes, written before the restart. Same contract,
// same minute, two feeds: the exact comparison the ring's overlap verdict makes
// (overlapScaleVerdict, max(8 ticks, 0.05%)). Every pair agrees → released as
// 'historical' (the upsert rule still keeps every live row); any pair disagrees
// → discarded loudly, exactly like an off-scale ring verdict; no pair at all →
// held, the first-live-bar rule stays the judge. While the market is OPEN this
// never acts — live frames judge, as before.

const (
	breakFlushEvery  = 30 * time.Second
	breakFlushWindow = 2 // last closed bars per tf offered to the store
)

type breakFlushStore interface {
	BarsBetweenOn(symbol, tf, contract string, fromMs, toMs int64) ([]store.BarHistoryDB, error)
}

type breakFlusher struct {
	mu      sync.Mutex
	ring    func() *ntwire.BarCache
	persist func(historical bool, symbol, tf string, bars []ntwire.Bar)
	rep     *persistReport // m426: outcome of the last persist call; nil = assume success
	st      breakFlushStore
	hold    *replayHold
	insert  func([]store.BarHistoryDB) error
	clock   func() time.Time
	isOpen  func(time.Time) bool
	seen    map[string]string // symbol|tf -> fingerprint of the tail last handed over
	flushed int               // bars handed over, for the log line
}

func newBreakFlusher(ring func() *ntwire.BarCache, persist func(bool, string, string, []ntwire.Bar), bh *store.BarHistoryStore, rep *persistReport) *breakFlusher {
	return &breakFlusher{
		ring: ring, persist: persist, rep: rep, st: bh, hold: barReplayHold, insert: bh.InsertBars,
		clock: time.Now, isOpen: kernel.IsCMEOpen, seen: map[string]string{},
	}
}

// tailPrint fingerprints the bars handed over, so an unchanged ring tail is not
// rewritten every 30 s for a whole weekend. Final is part of it: a final that
// lands later changes the print and is handed over again.
func tailPrint(bars []ntwire.Bar) string {
	var b strings.Builder
	for _, x := range bars {
		fmt.Fprintf(&b, "%d/%v/%v/%v/%v/%v/%v/%s;", x.T, x.O, x.H, x.L, x.C, x.V, x.Final, x.Source)
	}
	return b.String()
}

// tick is one pass. Market open → nothing (live frames persist and judge).
func (f *breakFlusher) tick() {
	if f == nil {
		return
	}
	now := f.clock()
	if f.isOpen(now) {
		return
	}
	f.flushRing(now)
	f.settleHold()
}

func (f *breakFlusher) flushRing(now time.Time) {
	var cache *ntwire.BarCache
	if f.ring != nil {
		cache = f.ring()
	}
	if cache == nil || f.persist == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, pair := range cache.AllPairs() {
		sym, tf := pair[0], pair[1]
		tail := ntwire.BreakFlushTail(cache.Get, sym, tf, now.UnixMilli(), breakFlushWindow)
		if len(tail) == 0 {
			continue
		}
		fp := tailPrint(tail)
		key := sym + "|" + tf
		if f.seen[key] == fp {
			continue
		}
		f.persist(false, sym, tf, tail)
		// m426: `seen` is set only when the write LANDED. A failed write
		// ("database is locked", no contract yet) is retried next tick instead
		// of the session-final bar being lost for the whole break.
		if f.rep != nil && !f.rep.ok {
			logger.Warnf("bars: market closed (%s) — session-final %s %s flush NOT persisted (%s); retrying next tick (m424/m426)",
				kernel.ClockCTSeconds(now), sym, tf, f.rep.why)
			continue
		}
		f.seen[key] = fp
		f.flushed += len(tail)
		logger.Infof("🔔 bars: market closed (%s) — persisted the session's last %d %s %s bar(s) from the ring, last open %s (m424)",
			kernel.ClockCTSeconds(now), len(tail), sym, tf, kernel.ClockCTSeconds(time.UnixMilli(tail[len(tail)-1].T)))
	}
}

// settleHold judges every held replay key against the store's own live rows.
func (f *breakFlusher) settleHold() {
	for _, k := range f.hold.heldKeys() {
		sym, tf := k[0], k[1]
		rows := f.hold.peek(sym, tf)
		if len(rows) == 0 {
			continue
		}
		contract := rows[0].Contract
		stored, err := f.st.BarsBetweenOn(sym, tf, contract, rows[0].OpenTimeMs, rows[len(rows)-1].OpenTimeMs+1)
		if err != nil {
			logger.Warnf("bars: replay-hold settle %s %s: store read failed: %v — hold kept (m424)", sym, tf, err)
			continue
		}
		existing := make([]ntwire.Bar, 0, len(stored))
		for _, r := range stored {
			if r.Source == store.BarSourceLive {
				existing = append(existing, ntwire.Bar{T: r.OpenTimeMs, C: r.C, Source: ntwire.BarSourceLive})
			}
		}
		incoming := make([]ntwire.Bar, 0, len(rows))
		for _, r := range rows {
			incoming = append(incoming, ntwire.Bar{T: r.OpenTimeMs, C: r.C, Source: ntwire.BarSourceHistorical})
		}
		pairs, agree := ntwire.OverlapScaleVerdict(sym, existing, incoming)
		if pairs == 0 {
			continue // nothing to judge against; the first live bar stays the judge
		}
		logger.Infof("📼 replay-hold judged against the store while CME is closed: %s %s — %d same-minute live pair(s), agree=%v (m424)", sym, tf, pairs, agree)
		f.hold.resolve(sym, tf, true, !agree, f.insert)
	}
}

// run is the production loop; the first tick is immediate so a boot inside a
// break flushes and settles without waiting.
func (f *breakFlusher) run() {
	f.tick()
	t := time.NewTicker(breakFlushEvery)
	defer t.Stop()
	for range t.C {
		f.tick()
	}
}
