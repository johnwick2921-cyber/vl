package ninjatrader

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"vl/internal/datatruth"
	"vl/internal/datatruth/checks"
	"vl/kernel"
)

// ── DATA-TRUTH WIRING (release #54, m446) ─────────────────────────────────────
//
// The five checks live in internal/datatruth/checks as pure functions. This file
// is the provider-side half: the sources they read (the bar cache, the AddOn's
// contract facts), the LOSS LEDGER the ingest drop sites write into, and the
// live-contract tracker. It registers nothing by itself - the integrator calls
// WireDataTruth once at boot (and datatruth.UseOwnerPush for the alert).

// dataTruthLedger records every FINAL or HISTORICAL bar the ingest queue drops
// (and counts intrabar drops for the rate). Package-level because the drop
// counters it sits beside are package-level atomics.
var dataTruthLedger = checks.NewLedger(20000)

// DataTruthLedger exposes the ledger (tests, the health surface).
func DataTruthLedger() *checks.Ledger { return dataTruthLedger }

// noteIngestLoss classifies one dropped ingest message and writes it to the
// ledger. A HISTORICAL batch evicted as the "oldest" frame is data loss even
// though evictForIngest counts it as an intrabar drop-oldest (the channel
// carries both kinds): it is classified by the message, not by the counter.
func noteIngestLoss(m barIngestMsg) {
	now := time.Now()
	switch {
	case m.historical:
		dataTruthLedger.RecordLoss(checks.LossHistorical, m.symbol, m.timeframe, cacheOpenTimes(m.timeframe, m.bars, false), now)
	case barsCarryFinal(m.bars):
		dataTruthLedger.RecordLoss(checks.LossFinal, m.symbol, m.timeframe, cacheOpenTimes(m.timeframe, m.bars, true), now)
	default:
		dataTruthLedger.RecordLoss(checks.LossIntrabar, m.symbol, m.timeframe, nil, now)
	}
}

// cacheOpenTimes returns the open times the CACHE would have stored for these
// wire bars (placeholders dropped, close-stamp -> open-stamp, stub re-anchor:
// the exact Upsert/Seed pipeline), so the ledger and the ring speak one stamp.
// onlyFinal keeps just the bars carrying the AddOn's final flag.
func cacheOpenTimes(tf string, bars []Bar, onlyFinal bool) []int64 {
	if len(bars) == 0 {
		return nil
	}
	cp := make([]Bar, 0, len(bars))
	for _, b := range bars {
		if onlyFinal && !b.Final {
			continue
		}
		cp = append(cp, b)
	}
	cp, _ = dropPlaceholderBars(cp)
	cp = append([]Bar(nil), cp...)
	cp = reanchorStubBars(tf, openStampBars(cp, tf))
	ts := make([]int64, 0, len(cp))
	for _, b := range cp {
		ts = append(ts, b.T)
	}
	return ts
}

// ── live contract tracker ─────────────────────────────────────────────────────

type contractTracker struct {
	mu   sync.Mutex
	seen map[string]map[string]int64 // symbol -> contract -> newest bar open time (ms)
}

var liveContracts = &contractTracker{seen: map[string]map[string]int64{}}

// note records the contract a LIVE frame named, with its newest bar time.
func (c *contractTracker) note(symbol, contract string, bars []Bar) {
	contract = strings.TrimSpace(contract)
	if contract == "" || len(bars) == 0 {
		return
	}
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	var newest int64
	for _, b := range bars {
		if b.T > newest {
			newest = b.T
		}
	}
	c.mu.Lock()
	m := c.seen[sym]
	if m == nil {
		m = map[string]int64{}
		c.seen[sym] = m
	}
	if newest > m[contract] {
		m[contract] = newest
	}
	c.mu.Unlock()
}

// reset forgets a symbol's history: called when the ring is purged on a roll,
// because the retired contract's bars are no longer in what the decision reads.
func (c *contractTracker) reset(symbol string) {
	c.mu.Lock()
	delete(c.seen, strings.ToUpper(strings.TrimSpace(symbol)))
	c.mu.Unlock()
}

// window returns the contracts with a live bar at or after sinceMs.
func (c *contractTracker) window(symbol string, sinceMs int64) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for k, t := range c.seen[strings.ToUpper(strings.TrimSpace(symbol))] {
		if t >= sinceMs {
			out = append(out, k)
		}
	}
	return out
}

// newest returns the contract of the newest live bar ("" = none).
func (c *contractTracker) newest(symbol string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	best, bt := "", int64(-1)
	for k, t := range c.seen[strings.ToUpper(strings.TrimSpace(symbol))] {
		if t > bt {
			best, bt = k, t
		}
	}
	return best
}

// ── sources over the live cache ───────────────────────────────────────────────

// DataTruthSources implements checks.Sources over the BarCache and the AddOn's
// contract facts. HistoryContract reports the contract of the stored history the
// evaluator seeded from (the trader's store reader); nil falls back to the
// contract of the newest live bar, which makes live-vs-history a check of the
// AddOn's ACK against its own frames until the store reader is wired.
type DataTruthSources struct {
	Server          *TCPServer
	Cache           *BarCache
	HistoryContract func(symbol string) (string, bool)
	PartnerOf       func(symbol string) string
	// WindowMin is the decision window the contract check judges (default 30).
	WindowMin int
	Now       func() time.Time
	// SnapshotTTL is how long a ring snapshot is reused while verifying ledger
	// entries (default 500ms).
	SnapshotTTL time.Duration
}

func tfString(tfMin int) string {
	if tfMin >= 60 && tfMin%60 == 0 {
		return fmt.Sprintf("%dh", tfMin/60)
	}
	return fmt.Sprintf("%dm", tfMin)
}

// Bars returns the cached ring for the symbol's frame.
func (d *DataTruthSources) Bars(symbol string, tfMin int) ([]checks.Bar, error) {
	if d == nil || d.Cache == nil {
		return nil, fmt.Errorf("data truth: no bar cache wired")
	}
	raw := d.Cache.Get(symbol, tfString(tfMin))
	out := make([]checks.Bar, len(raw))
	for i, b := range raw {
		out[i] = checks.Bar{T: b.T, O: b.O, H: b.H, L: b.L, C: b.C, V: b.V, Final: b.Final}
	}
	return out, nil
}

// Partner maps a trading symbol to its confirming partner.
func (d *DataTruthSources) Partner(symbol string) string {
	if d != nil && d.PartnerOf != nil {
		return d.PartnerOf(symbol)
	}
	switch strings.ToUpper(strings.TrimSpace(symbol)) {
	case "MNQ", "NQ":
		return "ES"
	case "ES", "MES":
		return "MNQ"
	}
	return ""
}

// Open is the CME calendar.
func (d *DataTruthSources) Open() checks.OpenFn {
	return func(ms int64) bool { return kernel.IsCMEOpen(time.UnixMilli(ms)) }
}

// Contract gathers the roll facts for the symbol.
func (d *DataTruthSources) Contract(symbol string) (checks.ContractFacts, error) {
	var f checks.ContractFacts
	if d == nil {
		return f, fmt.Errorf("data truth: no sources")
	}
	win := d.WindowMin
	if win <= 0 {
		win = 30
	}
	now := time.Now()
	if d.Now != nil {
		now = d.Now()
	}
	if d.Server != nil {
		if cf, ok := d.Server.CurrentContract(symbol); ok {
			f.Live = cf.Contract
		}
	}
	if d.HistoryContract != nil {
		if h, ok := d.HistoryContract(symbol); ok {
			f.History = h
		}
	} else {
		f.History = liveContracts.newest(symbol)
	}
	f.Window = liveContracts.window(symbol, now.UnixMilli()-int64(win)*60_000)
	return f, nil
}

// snapshotCache memoises ring snapshots briefly so verifying thousands of
// ledger entries copies each ring once, not once per entry.
type snapshotCache struct {
	mu sync.Mutex
	m  map[string]snapEntry
}
type snapEntry struct {
	at    time.Time
	bars  map[int64]Bar
	first int64
	last  int64
}

// VerifyLoss is the checks.VerifyFn over the cache: the lost bar is verified
// when it is present in the ring and (for a lost final) closed - it carries the
// final flag or a later bar exists - or when it has aged out of the ring (the
// decision no longer reads it).
func (d *DataTruthSources) VerifyLoss() checks.VerifyFn {
	sc := &snapshotCache{m: map[string]snapEntry{}}
	return func(symbol, tf string, t int64, kind string) (bool, error) {
		if d == nil || d.Cache == nil {
			return false, fmt.Errorf("no bar cache wired")
		}
		key := symbol + "|" + tf
		sc.mu.Lock()
		e, ok := sc.m[key]
		ttl := d.SnapshotTTL
		if ttl <= 0 {
			ttl = 500 * time.Millisecond
		}
		if !ok || time.Since(e.at) > ttl {
			raw := d.Cache.Get(symbol, tf)
			e = snapEntry{at: time.Now(), bars: make(map[int64]Bar, len(raw))}
			for i, b := range raw {
				e.bars[b.T] = b
				if i == 0 {
					e.first = b.T
				}
				e.last = b.T
			}
			sc.m[key] = e
		}
		sc.mu.Unlock()
		if len(e.bars) > 0 && t < e.first {
			return true, nil // aged out of the ring
		}
		b, present := e.bars[t]
		if !present {
			return false, nil
		}
		if kind == checks.LossFinal {
			return b.Final || e.last > t, nil
		}
		return true, nil
	}
}

// WireDataTruth registers the cache-backed checks with internal/datatruth. The
// integrator calls it once at boot, then wires datatruth.Evaluate into the ONE
// entry verdict. swing (optional) supplies the store-backed 1m and 5m history
// and the evaluator's live 4h line for the swing/levels checks.
func WireDataTruth(src *DataTruthSources, swing func(symbol string, now time.Time) (m1, m5 []checks.Bar, botLine float64, err error)) {
	checks.RegisterAll(checks.Config{
		Src:         src,
		Ledger:      dataTruthLedger,
		Verify:      src.VerifyLoss(),
		SwingInputs: swing,
	})
}

// DataTruthStats feeds /api/health: the datatruth counters plus the ledger.
func DataTruthStats(now time.Time) map[string]any {
	h := datatruth.HealthSnapshot()
	h["_ingest_loss_pending"] = dataTruthLedger.Pending()
	h["_intrabar_drops_per_min"] = dataTruthLedger.IntrabarPerMin(now)
	return h
}
