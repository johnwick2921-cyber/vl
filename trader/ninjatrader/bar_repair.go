package ninjatrader

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vl/internal/datatruth/checks"
	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
)

// ── BAR REPAIR (m467, release #62) — the repairs that live BELOW the hole
// threshold ─────────────────────────────────────────────────────────────────
//
// 11-03 18:07 CT: cross_frame paused MNQ entries on the 5m 17:55 bar, stored
// live_partial (the AddOn never finalised it: o 28356.25 h 28362.75 l 28328.50
// c 28333.25 v 4666) against the 1m it is made of (l 28310.75 c 28324.50 v
// 9621). The 1m 17:56 was missing from the store. NOTHING asked NT8 for that
// window again: the m444 hole tracker re-requests only a run of >=
// BARS_HOLE_MIN_GAP_MIN (3) open minutes, so a one-minute gap, a stale partial
// bar or a wrong HTF bar had no repair path at all, and entries stayed paused
// until the bar aged out of the check's window. ("A repair path that only
// exists above a pause threshold.")
//
// This driver is that path. It REPAIRS; it never pauses or un-pauses anything
// (the hole threshold still alone decides what a hole is for the
// data-integrity gate, and every data-truth check still measures its own
// clear). Three things are noted:
//
//   - gap_1m        a 1..(threshold-1) open-minute run with no stored 1m bar
//     (found by the same detector, observeLive, on the same store read);
//   - live_partial  a stored live bar of a CLOSED period the AddOn never
//     finalised (the persister writes it, m416);
//   - cross_frame   a ring HTF bar that disagrees with its 1m (the m446 check
//     reports each mismatch here through checks.SetCrossFrameRepair).
//
// What it does: one chain per SYMBOL (a re-request replays every timeframe,
// so one send repairs every item on the symbol), sent through the SAME wire
// re-request the hole tracker uses (RequestGapBackfillAt: the bars_subscribe
// resend) under the SAME admission (reRequestGateReason: link + feed up, CME
// open - never while the feed is down; barHoleInSendWindow; the wire's own
// spacing and per-day budget), on the SAME schedule (+1m/+3m/+10m/+30m). Its own
// cap barRepairDayCap per symbol per CME session-day keeps repairs from
// spending the budget real holes need. After each send every item is
// re-measured (store / ring); repaired items are said and counted, items
// still bad after the schedule are said ONCE and given up (the check that
// paused still clears only on its own measure, or when the bar ages out).
//
// What lets the replay actually REPAIR: the store's upsert already lets a
// replay row replace a live_partial (and never lets a partial replace a final
// row); the ring now does the same for a never-finalised live bar of a closed
// period (provider mergeSeedKeepingFinal, same price scale only).

const (
	repairKindGap     = "gap_1m"
	repairKindPartial = "live_partial"
	repairKindCross   = "cross_frame"

	// barRepairDayCap: repair sends per symbol per CME session-day. The wire's
	// own budget is 40/day per symbol, shared with the hole tracker; repairs
	// may use at most this many of it. The store holds ~1 such gap and a few
	// live_partial clusters a day (10 days read 11-03), so 12 is generous.
	barRepairDayCap = 12
	// barRepairMaxItems bounds the items one chain carries (oldest dropped,
	// counted as given up).
	barRepairMaxItems = 64
	// barRepairGiveUpAfter: a chain deferred (feed / link down, CME closed) for
	// longer than this is dropped - a weekend plus a holiday.
	barRepairGiveUpAfter = 72 * time.Hour
)

type repairItem struct {
	Kind, Symbol, TF, Contract string
	FromMs, ToMs               int64 // [from, to): the missing minutes, or the bar's period
	Detail                     string
	NotedAt                    time.Time
}

func (it repairItem) key() string {
	return fmt.Sprintf("%s|%s|%s|%d", it.Kind, it.TF, it.Contract, it.FromMs)
}

func (it repairItem) what() string {
	switch it.Kind {
	case repairKindGap:
		return fmt.Sprintf("%s 1m gap %s (%d open minute(s), below the %s hole threshold)", it.Symbol, holeWindowCT(it.FromMs, it.ToMs), (it.ToMs-it.FromMs)/60_000, "BARS_HOLE_MIN_GAP_MIN")
	case repairKindPartial:
		return fmt.Sprintf("%s %s bar %s stored live_partial (never finalised by the AddOn)", it.Symbol, it.TF, kernel.TableTimeCT(time.UnixMilli(it.FromMs)))
	default:
		return fmt.Sprintf("%s %s bar %s disagrees with its 1m (%s)", it.Symbol, it.TF, kernel.TableTimeCT(time.UnixMilli(it.FromMs)), it.Detail)
	}
}

type repairChain struct {
	items        map[string]*repairItem
	started      time.Time
	attempts     int
	nextAt       time.Time
	measureAt    time.Time
	saidDeferred int
}

type barRepairDriver struct {
	mu       sync.Mutex
	wire     func() holeWire
	clock    func() time.Time
	isOpen   func(time.Time) bool
	say      func(level, msg string)
	resolved func(it repairItem) (bool, error)
	chains   map[string]*repairChain
	dayKey   map[string]string
	daySent  map[string]int
	saidCap  map[string]string // symbol -> day key whose cap was said
	minGap   int
}

func newBarRepairDriver(wire func() holeWire, resolved func(repairItem) (bool, error), minGap int) *barRepairDriver {
	return &barRepairDriver{
		wire: wire, clock: time.Now, isOpen: barHoleExpected, say: sayToLogger, resolved: resolved,
		chains: map[string]*repairChain{}, dayKey: map[string]string{}, daySent: map[string]int{}, saidCap: map[string]string{},
		minGap: minGap,
	}
}

// barRepairs is the process driver (nil = not wired).
var barRepairs atomic.Pointer[barRepairDriver]

func (d *barRepairDriver) bootLine() string {
	return fmt.Sprintf("🧰 bar repair: armed — re-asks NT8 (the gap re-request, bars_subscribe resent) for what lives BELOW the hole threshold: a 1–%d open-minute 1m gap (a hole is ≥%d and alone decides the pause), a stored live_partial bar of a closed period, and a cross_frame HTF bar that disagrees with its 1m · one chain per symbol at %s · ≤%d repair sends per symbol per CME session-day (of the wire's %d, shared with the hole tracker) · never while the link or feed is down or CME is closed · a replay replaces a never-finalised live bar in the ring and the store, never a final one; it repairs, it never pauses or un-pauses (m467)",
		d.minGap-1, d.minGap, scheduleText(), barRepairDayCap, ntwire.GapBackfillDayCap())
}

// note adds one item. Repeats (the check reports a mismatch on every
// evaluation) are no-ops.
func (d *barRepairDriver) note(it repairItem) {
	if d == nil || strings.TrimSpace(it.Symbol) == "" {
		return
	}
	now := d.clock()
	it.NotedAt = now
	d.mu.Lock()
	defer d.mu.Unlock()
	ch := d.chains[it.Symbol]
	if ch == nil {
		ch = &repairChain{items: map[string]*repairItem{}, started: now, nextAt: now.Add(barHoleSchedule[0])}
		d.chains[it.Symbol] = ch
	}
	k := it.key()
	if _, dup := ch.items[k]; dup {
		return
	}
	if len(ch.items) >= barRepairMaxItems {
		oldest := ""
		var at time.Time
		for kk, x := range ch.items {
			if oldest == "" || x.NotedAt.Before(at) {
				oldest, at = kk, x.NotedAt
			}
		}
		telemetry.IncBarRepairGaveUp()
		d.say("WARN", fmt.Sprintf("🧰 bar repair: %s dropped unrepaired — %d items already pending on %s (the cap)", ch.items[oldest].what(), barRepairMaxItems, it.Symbol))
		delete(ch.items, oldest)
	}
	cp := it
	ch.items[k] = &cp
	telemetry.IncBarRepairAsked()
	d.say("WARN", fmt.Sprintf("🧰 bar repair NOTED: %s — NT8 history re-request scheduled (chain attempt %d at %s CT), never while the feed is down",
		it.what(), ch.attempts+1, kernel.ClockCTSeconds(ch.nextAt)))
}

// measureLocked re-reads every item; repaired ones are said, counted, removed.
func (d *barRepairDriver) measureLocked(sym string, ch *repairChain, now time.Time) {
	keys := make([]string, 0, len(ch.items))
	for k := range ch.items {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		it := ch.items[k]
		ok, err := d.resolved(*it)
		if err != nil || !ok {
			continue
		}
		how := fmt.Sprintf("after repair re-request #%d", ch.attempts)
		if ch.attempts == 0 {
			how = "before any repair re-request (another replay carried it)"
		}
		telemetry.IncBarRepairRepaired()
		d.say("INFO", fmt.Sprintf("✅ bar REPAIRED: %s — the store/ring now hold NT8's bar, %s (noted %s CT)", it.what(), how, kernel.ClockCTSeconds(it.NotedAt)))
		NoteHistoryRefill(sym, it.TF, 1, "bar repaired "+it.what())
		delete(ch.items, k)
	}
}

func (d *barRepairDriver) giveUpLocked(sym string, ch *repairChain, why string) {
	for _, it := range ch.items {
		telemetry.IncBarRepairGaveUp()
		d.say("WARN", fmt.Sprintf("🧰 bar repair GAVE UP: %s — %s. Nothing more is asked; a check that paused on it still clears only on its own measure (or when the bar leaves its window)", it.what(), why))
	}
	delete(d.chains, sym)
}

// tick runs the schedule (production: every barHoleTickEvery). The wire send
// happens outside the lock.
func (d *barRepairDriver) tick(now time.Time) {
	if d == nil {
		return
	}
	type due struct {
		sym string
		w   holeWire
	}
	var sends []due
	d.mu.Lock()
	syms := make([]string, 0, len(d.chains))
	for s := range d.chains {
		syms = append(syms, s)
	}
	sort.Strings(syms)
	w := d.wire()
	for _, sym := range syms {
		ch := d.chains[sym]
		if !ch.measureAt.IsZero() && !now.Before(ch.measureAt) {
			ch.measureAt = time.Time{}
		}
		if ch.measureAt.IsZero() {
			d.measureLocked(sym, ch, now)
		}
		if len(ch.items) == 0 {
			delete(d.chains, sym)
			continue
		}
		if !ch.measureAt.IsZero() {
			continue // waiting for the replay to land
		}
		if ch.attempts >= len(barHoleSchedule) {
			d.giveUpLocked(sym, ch, fmt.Sprintf("still not repaired after %d re-requests (NT8's replay did not carry it — a final live bar is never replaced, and a minute NT8 has no bar for stays empty)", ch.attempts))
			continue
		}
		if now.Before(ch.nextAt) {
			continue
		}
		if now.Sub(ch.started) > barRepairGiveUpAfter {
			d.giveUpLocked(sym, ch, fmt.Sprintf("deferred for more than %s", barRepairGiveUpAfter))
			continue
		}
		attempt := ch.attempts + 1
		if reason := reRequestGateReason(gateWire(w), d.isOpen, now); reason != "" {
			if ch.saidDeferred != attempt {
				ch.saidDeferred = attempt
				telemetry.IncBarRepairDeferred()
				d.say("WARN", fmt.Sprintf("🧰 bar repair re-request #%d DEFERRED for %s (%d item(s)): %s — it goes out once that clears", attempt, sym, len(ch.items), reason))
			}
			continue
		}
		if !barHoleInSendWindow(now) {
			continue
		}
		day := kernel.CMESessionDayKey(now)
		if d.dayKey[sym] != day {
			d.dayKey[sym], d.daySent[sym] = day, 0
		}
		if d.daySent[sym] >= barRepairDayCap {
			telemetry.IncBarRepairRefused()
			d.giveUpLocked(sym, ch, fmt.Sprintf("the repair budget for %s is spent for this CME session-day (%d sends; it resets at the 17:00 CT roll, the rest of the wire's budget stays for real holes)", sym, barRepairDayCap))
			continue
		}
		sends = append(sends, due{sym: sym, w: w})
	}
	d.mu.Unlock()

	for _, s := range sends {
		err := s.w.RequestGapBackfillAt(s.sym, now)
		d.mu.Lock()
		ch := d.chains[s.sym]
		if ch == nil {
			d.mu.Unlock()
			continue
		}
		switch {
		case err == nil:
			ch.attempts++
			d.daySent[s.sym]++
			ch.measureAt = now.Add(barHoleSettle)
			if ch.attempts < len(barHoleSchedule) {
				step := barHoleSchedule[ch.attempts] - barHoleSchedule[ch.attempts-1]
				ch.nextAt = laterOf(ch.started.Add(barHoleSchedule[ch.attempts]), now.Add(step))
			}
			telemetry.IncBarRepairSent()
			names := make([]string, 0, len(ch.items))
			for _, it := range ch.items {
				names = append(names, it.what())
			}
			sort.Strings(names)
			d.say("INFO", fmt.Sprintf("🧯 bar repair re-request #%d SENT for %s — bars_subscribe resent, NT8 replays its window; %d item(s): %s; re-measured at %s CT",
				ch.attempts, s.sym, len(ch.items), strings.Join(names, " · "), kernel.ClockCTSeconds(ch.measureAt)))
		case errors.Is(err, ntwire.ErrGapBackfillTooSoon):
			ch.nextAt = now.Add(barHoleSpacing) // the tracker (or another ask) just sent: its replay may carry it; ask again after the spacing
		default:
			telemetry.IncBarRepairRefused()
			d.giveUpLocked(s.sym, ch, fmt.Sprintf("the wire refused the re-request: %v", err))
		}
		d.mu.Unlock()
	}
}

// gateWire keeps a nil holeWire a nil interface for the shared gate.
func gateWire(w holeWire) reRequestGateWire {
	if w == nil {
		return nil
	}
	return w
}

func (d *barRepairDriver) run() {
	tk := time.NewTicker(barHoleTickEvery)
	defer tk.Stop()
	for range tk.C {
		d.tick(d.clock())
	}
}

// BarRepairPendingView is one pending chain as /api/health serves it.
type BarRepairPendingView struct {
	Symbol   string   `json:"symbol"`
	Items    []string `json:"items"`
	Attempts int      `json:"attempts"`
}

// BarRepairSnapshot is the driver's pending chains (ok=false: not wired).
func BarRepairSnapshot() ([]BarRepairPendingView, bool) {
	d := barRepairs.Load()
	if d == nil {
		return nil, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	out := []BarRepairPendingView{}
	for sym, ch := range d.chains {
		v := BarRepairPendingView{Symbol: sym, Attempts: ch.attempts}
		for _, it := range ch.items {
			v.Items = append(v.Items, it.what())
		}
		sort.Strings(v.Items)
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Symbol < out[j].Symbol })
	return out, true
}

// ── production resolution ────────────────────────────────────────────────

// repairResolver measures an item from the store (gap, live_partial) or the
// ring (cross_frame).
func repairResolver(st holeStore, cache func() *ntwire.BarCache) func(repairItem) (bool, error) {
	return func(it repairItem) (bool, error) {
		switch it.Kind {
		case repairKindGap:
			rows, err := st.BarsBetweenOn(it.Symbol, barHoleTF, it.Contract, it.FromMs, it.ToMs)
			if err != nil {
				return false, err
			}
			want := 0
			for m := it.FromMs; m < it.ToMs; m += 60_000 {
				if barHoleExpected(time.UnixMilli(m)) {
					want++
				}
			}
			return len(rows) >= want, nil
		case repairKindPartial:
			rows, err := st.BarsBetweenOn(it.Symbol, it.TF, it.Contract, it.FromMs, it.FromMs+1)
			if err != nil {
				return false, err
			}
			return len(rows) == 1 && rows[0].Source != store.BarSourceLivePartial, nil
		case repairKindCross:
			c := cache()
			if c == nil {
				return false, nil
			}
			return crossFrameRepaired(c, it), nil
		}
		return false, nil
	}
}

// crossFrameRepaired re-judges ONE HTF bar against its 1m in the ring with the
// check's own rule (checks.CompareFrames, judged as of the bar's close). A bar
// the ring no longer holds is gone: nothing is left to repair.
func crossFrameRepaired(c *ntwire.BarCache, it repairItem) bool {
	tfMin := int((it.ToMs - it.FromMs) / 60_000)
	src := &ntwire.DataTruthSources{Cache: c}
	m1, _ := src.Bars(it.Symbol, 1)
	htf, _ := src.Bars(it.Symbol, tfMin)
	held := false
	for _, b := range htf {
		if b.T == it.FromMs {
			held = true
			break
		}
	}
	if !held {
		return true
	}
	p := checks.DefaultCross()
	p.HoldFor = nil
	mm, judged, _ := checks.CompareFrames(m1, htf, tfMin, time.UnixMilli(it.ToMs), src.Open(), p)
	for _, m := range mm {
		if m.T == it.FromMs {
			return false
		}
	}
	return judged > 0
}

// noteSmallGaps hands the 1..(minGap-1) open-minute gaps of one store read to
// the repair driver (called by observeLive with the same times it judged).
func noteSmallGaps(d *barRepairDriver, symbol, contract string, times []int64, minGap int, isOpen func(time.Time) bool) {
	if d == nil || minGap <= 1 {
		return
	}
	for _, sp := range findBarHoles(times, 1, isOpen) {
		if sp.OpenMissing >= minGap {
			continue // a HOLE: the tracker's
		}
		d.note(repairItem{Kind: repairKindGap, Symbol: symbol, TF: barHoleTF, Contract: contract, FromMs: sp.FromMs, ToMs: sp.ToMs})
	}
}

// noteLivePartials hands every live_partial row the persister just wrote to
// the repair driver.
func noteLivePartials(d *barRepairDriver, rows []store.BarHistoryDB) {
	if d == nil {
		return
	}
	for _, r := range rows {
		if r.Source != store.BarSourceLivePartial {
			continue
		}
		span := tfSpanMs(r.TF)
		if span <= 0 {
			continue
		}
		d.note(repairItem{Kind: repairKindPartial, Symbol: r.Symbol, TF: r.TF, Contract: r.Contract, FromMs: r.OpenTimeMs, ToMs: r.OpenTimeMs + span})
	}
}

// tfSpanMs is the bar span of the intraday frames the checks read (1m..1h);
// larger frames are not noted (0): their replay is not what the checks read.
func tfSpanMs(tf string) int64 {
	switch tf {
	case "1m":
		return 60_000
	case "2m":
		return 120_000
	case "3m":
		return 180_000
	case "5m":
		return 300_000
	case "15m":
		return 900_000
	case "30m":
		return 1_800_000
	case "1h":
		return 3_600_000
	}
	return 0
}

// wireCrossFrameRepair installs the checks hook: every cross_frame mismatch is
// a repair item.
func wireCrossFrameRepair(d *barRepairDriver, contract func(symbol string) string) {
	checks.SetCrossFrameRepair(func(symbol string, tfMin int, barT int64, detail string) {
		d.note(repairItem{Kind: repairKindCross, Symbol: symbol, TF: fmt.Sprintf("%dm", tfMin), Contract: contract(symbol),
			FromMs: barT, ToMs: barT + int64(tfMin)*60_000, Detail: detail})
	})
}
