package ninjatrader

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
	"vl/telemetry"
)

// ── PERSISTENT BAR-GAP AUDIT (m444, owner 2026-10-29: "missing bar all fucked
// up, it has to have the system know missing bar") ──────────────────────────
//
// What happened [A] (log_events + bars, read-only): the bot restarted at 12:07
// CT Mon 2026-10-26 and NT8's TCP link stayed DOWN until 08:36 CT Tue (3477
// FEED DOWN lines — that outage WAS flagged). When the first live bar came the
// boot scan saw MNQ 1m missing 16:00→08:34 (934 open min), 12 re-requests
// later NT8 still had none, and the hole was declared UNFILLED (a log line) and
// then RETIRED (dropped from the tracker and /api/health) once it left NT8's
// 2000-bar replay window. The 17:00-21:56 span itself may still exist in NT8
// beyond that window [B]: the stored 1m rows resume at 21:57, exactly 2000 CME-open
// minutes before the 10-28 08:17 replay [A] — hence the DEEP re-request below. Days later MNQ 1m was still missing 17:00→21:57 while
// MNQ 3m/5m and ES 1m were whole — and NOTHING on the bot knew: the detector
// only ever ran when a live 1m bar arrived (observeLive), the boot scan reached
// back only to the CURRENT session open, and a retired hole left the list.
//
// What this does:
//  1. EVERY auditEvery (10 min) and once shortly after boot, for every
//     subscribed symbol on its current contract, read the stored 1m open times
//     over a rolling window of auditDays CME SESSION days (default 7) — one
//     indexed, single-column range read — and find every stretch of ≥ minGap
//     OPEN minutes with no stored bar (the one calendar, barHoleExpected).
//     It does not wait for a live bar.
//  2. CROSS-FRAME: an open minute covered by a stored 3m/5m bar proves the
//     market traded. A stretch with no 1m bar where 3m/5m exist is a hole even
//     when there is no stored 1m bar after it (the window's trailing edge, or
//     no 1m at all). The leading edge is not judged: a fresh install or a new
//     contract stores 3m/5m further back than 1m (bars_back=2000 on every
//     frame), which is not a hole.
//  3. Every gap goes into the EXISTING barHoleTracker (re-request schedule, fill
//     measurement, UNFILLED, owner push, /api/health). There is no second
//     tracker. A hole already tracked (any state) is never re-added, so an
//     episode is announced ONCE; it stays listed until the store holds the bars
//     (the audit closes it when a later pass no longer finds it) or it ages out
//     of the window.
//  4. A hole older than NT8's standard replay window (the last 2000 1m bars) is
//     NOT given up on: the tracker asks NT8 for it with a DEEP 1m re-request
//     (up to 4800 bars — the replay-hold keeps 5000 — 2 scheduled asks, then
//     UNFILLED, then 2 slow ones). Only a hole beyond THAT is RETIRED (listed,
//     never asked) — and then the dashboard and the phone say exactly that.
//     m444 review round 2: the first cut retired every such hole at once and
//     told the owner "the bot cannot refill it" — false, the deep path existed.
//
// Trailing edge with no higher-frame evidence is NOT judged here: a feed that
// delivers nothing at all is FEED DOWN's story (its own banner and push).
const (
	barGapAuditDaysDefault  = 7
	barGapAuditDaysMax      = 30
	barGapAuditEveryDefault = 10 * time.Minute
	// barGapAuditFirstDelay — the first pass waits for the boot replay to land
	// and the replay-hold to release, so a replay that is about to fill a
	// window is not announced as a hole.
	barGapAuditFirstDelay = 2 * time.Minute
	// barGapAuditGrace — the window ends this far behind now: a 1m bar closes
	// at its minute's end and the persister batches, so the newest minutes are
	// still arriving. Equal to the default hole threshold on purpose.
	barGapAuditGrace = 3 * time.Minute

	// BarsMissingAction is the ONE instruction the owner is given, word for
	// word, by the dashboard banner (web StatusBanner, key missingBarsBanner)
	// and by the owner push body (trader/owner_push.go).
	BarsMissingAction = "NT8: Reload All Historical Data"

	// DeepHoleMaxBarsBack is the deepest 1m ask the bot makes for a hole (the
	// wire's cap); a hole further back is retired. READ by the owner push.
	DeepHoleMaxBarsBack = ntwire.DeepGapBackfillMaxBarsBack
)

// barGapAuditDays resolves BARS_GAP_AUDIT_DAYS (CME session days; 0 = audit off).
func barGapAuditDays() (int, string) {
	v := strings.TrimSpace(os.Getenv("BARS_GAP_AUDIT_DAYS"))
	if v == "" {
		return barGapAuditDaysDefault, "default"
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return barGapAuditDaysDefault, fmt.Sprintf("default — env %q unreadable", v)
	}
	if n > barGapAuditDaysMax {
		return barGapAuditDaysMax, fmt.Sprintf("env, capped at %d", barGapAuditDaysMax)
	}
	return n, "env"
}

// barGapAuditEvery resolves BARS_GAP_AUDIT_EVERY_MIN.
func barGapAuditEvery() (time.Duration, string) {
	v := strings.TrimSpace(os.Getenv("BARS_GAP_AUDIT_EVERY_MIN"))
	if v == "" {
		return barGapAuditEveryDefault, "default"
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return barGapAuditEveryDefault, fmt.Sprintf("default — env %q unreadable", v)
	}
	return time.Duration(n) * time.Minute, "env"
}

// auditOpenTimes is the store read the audit needs. *store.BarHistoryStore has
// it; a holeStore fake without it falls back to BarsBetweenOn.
type auditOpenTimes interface {
	OpenTimesBetweenOn(symbol, tf, contract string, fromMs, toMs int64) ([]int64, error)
}

// auditRoots is the wire's subscribed-root list (*ntwire.TCPServer has it).
type auditRoots interface {
	SubscribedRoots() []string
}

func (t *barHoleTracker) openTimes(symbol, tf, contract string, fromMs, toMs int64) ([]int64, error) {
	if r, ok := t.st.(auditOpenTimes); ok {
		return r.OpenTimesBetweenOn(symbol, tf, contract, fromMs, toMs)
	}
	rows, err := t.st.BarsBetweenOn(symbol, tf, contract, fromMs, toMs)
	if err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.OpenTimeMs)
	}
	return out, nil
}

// auditWindowStart is the open (17:00 CT) of the session `days` trading
// sessions back, counting the current one: a session counts when the CME
// calendar trades an hour into it (so Saturday's and a closed holiday's 17:00
// are skipped).
func auditWindowStart(now time.Time, days int, isOpen func(time.Time) bool) time.Time {
	start := kernel.CMESessionDayStart(now)
	counted := 1
	for i := 0; i < 90 && counted < days; i++ {
		start = start.AddDate(0, 0, -1)
		if isOpen(start.Add(time.Hour)) {
			counted++
		}
	}
	return start
}

// evidenceMinutes returns the set of minute stamps covered by the given
// higher-frame bars: a bar opening at t with width w covers [t, t+w).
func evidenceMinutes(width time.Duration, opens []int64, into map[int64]struct{}) {
	const minute = int64(60_000)
	w := int64(width / time.Millisecond)
	for _, o := range opens {
		for m := o; m < o+w; m += minute {
			into[m] = struct{}{}
		}
	}
}

// auditSpans is the audit's detector (pure — tests drive it with real rows).
// oneMin = ascending stored 1m open times inside [winFromMs, winToMs); evid =
// minutes a stored 3m/5m bar proves traded. It returns, ascending:
//   - every stretch between two stored 1m bars holding ≥ minGap OPEN missing
//     minutes (findBarHoles — the live detector's rule, unchanged), and
//   - the CROSS-FRAME stretch: after the last stored 1m bar (or the whole window
//     when there is none) the open minutes a 3m/5m bar proves traded, when
//     ≥ minGap of them have no 1m. The leading edge is NOT judged (see below).
func auditSpans(oneMin []int64, evid map[int64]struct{}, winFromMs, winToMs int64, minGap int, isOpen func(time.Time) bool) []barHoleSpan {
	const minute = int64(60_000)
	spans := findBarHoles(oneMin, minGap, isOpen)
	// findBarHoles starts a hole at the minute after the last stored bar, which
	// can be the 16:00-17:00 break; the owner reads the window, so it starts at
	// the first minute the CME actually traded (17:00, not 16:00).
	for i := range spans {
		for spans[i].FromMs < spans[i].ToMs && !isOpen(time.UnixMilli(spans[i].FromMs)) {
			spans[i].FromMs += minute
		}
	}
	edge := func(from, to int64) {
		if r := from % minute; r != 0 {
			from += minute - r
		}
		first, last := int64(-1), int64(-1)
		for m := from; m < to; m += minute {
			if _, ok := evid[m]; ok && isOpen(time.UnixMilli(m)) {
				if first < 0 {
					first = m
				}
				last = m
			}
		}
		if first < 0 {
			return
		}
		missing := 0
		for m := first; m <= last; m += minute {
			if isOpen(time.UnixMilli(m)) {
				missing++
			}
		}
		if missing >= minGap {
			spans = append(spans, barHoleSpan{FromMs: first, ToMs: last + minute, OpenMissing: missing})
		}
	}
	// Only the TRAILING edge (or the whole window when there is no 1m bar at all)
	// is judged against higher-frame evidence. The LEADING edge (before the first
	// stored 1m bar) is deliberately NOT: the bot subscribes with bars_back=2000
	// on every timeframe, which reaches ~33 h back for 1m but ~4 days for 3m and
	// ~7 days for 5m, so on a fresh install or a new contract 3m/5m always start
	// earlier than 1m and a leading-edge rule would call that a hole (m444 review
	// round 1, defect 1). A hole with 1m on both sides is findBarHoles' job.
	if len(oneMin) == 0 {
		edge(winFromMs, winToMs)
	} else {
		edge(oneMin[len(oneMin)-1]+minute, winToMs)
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].FromMs < spans[j].FromMs })
	return spans
}

// auditTarget is one (symbol, contract) the audit reads.
type auditTarget struct{ Symbol, Contract string }

// auditTargetsLocked lists what to audit: every subscribed root on the contract
// the AddOn named (the store key), plus every (symbol, contract) a live bar was
// judged for this process. Caller holds t.mu; w may be nil.
func (t *barHoleTracker) auditTargetsLocked(w holeWire) []auditTarget {
	seen := map[string]bool{}
	var out []auditTarget
	add := func(sym, contract string) {
		sym, contract = strings.ToUpper(strings.TrimSpace(sym)), strings.TrimSpace(contract)
		if sym == "" || contract == "" || seen[holeKey(sym, contract)] {
			return
		}
		seen[holeKey(sym, contract)] = true
		out = append(out, auditTarget{sym, contract})
	}
	if w != nil {
		if rl, ok := w.(auditRoots); ok {
			for _, sym := range rl.SubscribedRoots() {
				if f, ok := w.CurrentContract(sym); ok {
					add(sym, f.Contract)
				}
			}
		}
	}
	for k := range t.checked {
		i := strings.Index(k, "|")
		if i <= 0 {
			continue
		}
		sym, contract := k[:i], k[i+1:]
		// a (symbol, contract) the subscription has ROLLED AWAY from is never
		// audited again: t.checked is not pruned on a roll, and an old contract's
		// holes would be re-found (and re-retired) on every pass, forever
		// (m444 review round 1, defect 2).
		if w != nil {
			if f, ok := w.CurrentContract(strings.ToUpper(strings.TrimSpace(sym))); ok && f.Contract != "" && f.Contract != strings.TrimSpace(contract) {
				continue
			}
		}
		add(sym, contract)
	}
	sort.Slice(out, func(i, j int) bool {
		return holeKey(out[i].Symbol, out[i].Contract) < holeKey(out[j].Symbol, out[j].Contract)
	})
	return out
}

// BarGapAuditView is the audit's state as /api/health serves it — every field
// READ. The hole counts are re-derived from the store on every pass, so they
// survive a restart (they are NOT per-boot counters).
type BarGapAuditView struct {
	Enabled         bool              `json:"enabled"`
	WindowSessions  int               `json:"window_sessions"`
	EveryMin        int               `json:"every_min"`
	Passes          int               `json:"passes"`
	LastPass        *string           `json:"last_pass,omitempty"` // absent until the first pass ran
	WindowFrom      *string           `json:"window_from,omitempty"`
	WindowTo        *string           `json:"window_to,omitempty"`
	Audited         map[string]string `json:"audited,omitempty"` // symbol -> contract of the last pass
	FoundByAudit    int               `json:"holes_found_by_audit"`
	ReadErrors      int               `json:"read_errors"`
	OpenHoles       int               `json:"open_holes"`        // every tracked hole (refilling + unfilled + retired)
	UnfilledHoles7d int               `json:"unfilled_holes_7d"` // unfilled + retired: the bot could not refill them
	UnfilledMissing int               `json:"unfilled_missing_min"`
	LastReadErr     string            `json:"last_read_error,omitempty"`
	CrossFrameTFs   []string          `json:"cross_frame_tfs"`
}

type barGapAuditState struct {
	passes     int
	lastPass   time.Time
	winFrom    int64
	winTo      int64
	audited    map[string]string
	found      int
	readErrors int
	lastErr    string
	saidFirst  map[string]bool // one boot line per target
	saidCap    map[string]bool // one overflow line per hole
}

// auditPass is one pass at `now` (A28: the caller's clock).
func (t *barHoleTracker) auditPass(now time.Time) {
	if t == nil || t.st == nil || t.auditDays <= 0 {
		return
	}
	const minute = int64(60_000)
	t.mu.Lock()
	targets := t.auditTargetsLocked(t.wire())
	t.mu.Unlock()
	winFrom := auditWindowStart(now, t.auditDays, t.isOpen).UnixMilli()
	winTo := now.UnixMilli()/minute*minute - int64(barGapAuditGrace/time.Millisecond)

	type result struct {
		tg                 auditTarget
		oneMinRows, spansN int
		spans              []barHoleSpan
		err                error
	}
	results := make([]result, 0, len(targets))
	for _, tg := range targets {
		r := result{tg: tg}
		one, err := t.openTimes(tg.Symbol, barHoleTF, tg.Contract, winFrom, winTo)
		if err != nil {
			r.err = err
			results = append(results, r)
			continue
		}
		evid := map[int64]struct{}{}
		for _, tf := range []struct {
			name string
			w    time.Duration
		}{{"3m", 3 * time.Minute}, {"5m", 5 * time.Minute}} {
			// a higher-frame read failing only narrows the cross-frame evidence;
			// the between-bars check below does not depend on it
			if hi, herr := t.openTimes(tg.Symbol, tf.name, tg.Contract, winFrom-int64(tf.w/time.Millisecond), winTo); herr == nil {
				evidenceMinutes(tf.w, hi, evid)
			}
		}
		r.oneMinRows = len(one)
		r.spans = auditSpans(one, evid, winFrom, winTo, t.minGap, t.isOpen)
		r.spansN = len(r.spans)
		results = append(results, r)
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	st := &t.audit
	st.passes++
	st.lastPass, st.winFrom, st.winTo = now, winFrom, winTo
	st.audited = map[string]string{}
	if st.saidFirst == nil {
		st.saidFirst = map[string]bool{}
		st.saidCap = map[string]bool{}
	}
	for _, r := range results {
		st.audited[r.tg.Symbol] = r.tg.Contract
		if r.err != nil {
			st.readErrors++
			st.lastErr = r.err.Error()
			t.sayErrOnce("audit", fmt.Sprintf("🕳 bar gap audit: store read failed for %s %s on %s (%v) — this pass skips it; the next one retries", r.tg.Symbol, barHoleTF, r.tg.Contract, r.err))
			continue
		}
		added := t.applyAuditLocked(r.tg, r.spans, winFrom, winTo, now)
		k := holeKey(r.tg.Symbol, r.tg.Contract)
		if !st.saidFirst[k] {
			st.saidFirst[k] = true
			lvl := "INFO"
			if len(r.spans) > 0 {
				lvl = "WARN"
			}
			t.say(lvl, fmt.Sprintf("🕳 bar gap audit: %s %s on %s · %d stored 1m bar(s) in %s → %s (%d CME session day(s), cross-checked against 3m/5m) · %d hole(s) found, %d new",
				r.tg.Symbol, barHoleTF, r.tg.Contract, r.oneMinRows, kernel.TableTimeCT(time.UnixMilli(winFrom)), kernel.TableTimeCT(time.UnixMilli(winTo)), t.auditDays, len(r.spans), added))
		}
	}
}

// applyAuditLocked feeds one target's spans to the tracker and reconciles the
// holes it already tracks against what the store now holds. Returns how many
// NEW holes it opened. Caller holds t.mu.
func (t *barHoleTracker) applyAuditLocked(tg auditTarget, spans []barHoleSpan, winFrom, winTo int64, now time.Time) int {
	added := 0
	for _, sp := range spans {
		if t.trackedLocked(tg.Symbol, tg.Contract, sp) {
			continue
		}
		if t.atCapLocked() {
			k := fmt.Sprintf("%s|%d", holeKey(tg.Symbol, tg.Contract), sp.FromMs)
			if !t.audit.saidCap[k] {
				t.audit.saidCap[k] = true
				telemetry.IncBarsHoleOverflow()
				t.say("WARN", fmt.Sprintf("🕳 bars hole NOT tracked (audit): %s %s %s on %s (%d open minute(s) missing) — %d holes already open, the cap; it is seen again once one closes",
					tg.Symbol, barHoleTF, holeWindowCT(sp.FromMs, sp.ToMs), tg.Contract, sp.OpenMissing, barHoleMaxOpen))
			}
			continue
		}
		h := &barHole{
			Symbol: tg.Symbol, Contract: tg.Contract, FromMs: sp.FromMs, ToMs: sp.ToMs,
			MissingAtDetect: sp.OpenMissing, Missing: sp.OpenMissing,
			DetectedAt: now, State: holeStateRefilling, Source: holeSourceAudit,
			nextAttemptAt:     now.Add(barHoleSchedule[0]),
			disagreedAtDetect: t.disagreedNowLocked(tg.Symbol),
		}
		t.holes = append(t.holes, h)
		added++
		t.audit.found++
		telemetry.IncBarsHoleDetected()
		t.say("WARN", fmt.Sprintf("🕳 bars HOLE found by the AUDIT: %s %s %s on %s — %d open minute(s) with no stored bar (threshold %d, %s); found from the store, not from a live arrival; NT8 history re-request scheduled at %s after now (a DEEP 1m one when the hole is older than NT8's standard replay of the last 2000 1m bars), never while the feed is down. If NT8 still has none after the re-requests: %s (a hole older than even the deepest re-request, %d 1m bars, is RETIRED instead and only listed)",
			tg.Symbol, barHoleTF, holeWindowCT(sp.FromMs, sp.ToMs), tg.Contract, sp.OpenMissing, t.minGap, t.minGapSrc, scheduleText(), BarsMissingAction, ntwire.DeepGapBackfillMaxBarsBack))
	}
	// reconcile: a tracked hole the audit can fully judge and no longer finds is
	// FILLED; one wholly before the window has aged out
	for _, h := range append([]*barHole(nil), t.holes...) {
		if h.Symbol != tg.Symbol || h.Contract != tg.Contract {
			continue
		}
		switch {
		case h.ToMs <= winFrom:
			// #55 m449: leaving the AUDIT's window (a few CME sessions) is not
			// leaving the windows a decision READS (deep ~8.7 sessions, swing4h
			// ~18, the mentor RTH key-level leg 90 days). Dropping an unmeasured
			// hole here read as "all clear" while the minutes were still gone, and
			// the gate resumed days before the hole it had promised to wait for.
			// The audit stops WATCHING it; the data-integrity gate keeps it (and
			// re-measures it from the store, remeasureRetired) until it is measured
			// filled or outside every window.
			t.say("INFO", fmt.Sprintf("🕳 bars hole aged out of the audit window: %s %s %s on %s (%s, %d open minute(s) were missing) — the audit no longer watches it; the data-integrity gate keeps it listed until it is measured filled or leaves every window a decision reads",
				h.Symbol, barHoleTF, holeWindowCT(h.FromMs, h.ToMs), h.Contract, h.State, h.Missing))
			t.retainRetiredLocked(h)
			t.removeLocked(h)
		case h.FromMs < winFrom || h.ToMs > winTo:
			// partly outside what this pass read: cannot be judged here
		default:
			overlap, still := false, 0
			for _, sp := range spans {
				if sp.FromMs < h.ToMs && sp.ToMs > h.FromMs {
					overlap = true
					still += sp.OpenMissing
				}
			}
			if !overlap {
				t.fillLocked(h, now)
			} else if (h.State == holeStateUnfilled || h.State == holeStateRetired) && still < h.Missing {
				h.Missing = still // a partial fill: the listed figure follows the store
			}
		}
	}
	return added
}

// runAudit is the production schedule: a first pass after the boot replay has
// settled, then one every auditEvery, forever.
func (t *barHoleTracker) runAudit() {
	if t == nil || t.auditDays <= 0 {
		return
	}
	first := time.NewTimer(barGapAuditFirstDelay)
	<-first.C
	t.auditPass(t.clock())
	tk := time.NewTicker(t.auditEvery)
	defer tk.Stop()
	for range tk.C {
		t.auditPass(t.clock())
	}
}

// auditBootLine prints the armed configuration, every value read (A11).
func (t *barHoleTracker) auditBootLine() string {
	if t.auditDays <= 0 {
		return fmt.Sprintf("🕳 bar gap audit: OFF [%s, env BARS_GAP_AUDIT_DAYS=0] — holes are found only from live arrivals", t.auditDaysSrc)
	}
	return fmt.Sprintf("🕳 bar gap audit: armed — every %s [%s, env BARS_GAP_AUDIT_EVERY_MIN], first pass %s after boot, over the last %d CME session day(s) [%s, env BARS_GAP_AUDIT_DAYS] of the stored %s bars of every subscribed symbol, cross-checked against 3m/5m; a gap goes into the same hole tracker (re-requests, UNFILLED, owner push); a hole older than NT8's standard replay window gets DEEP 1m re-requests (up to %d bars), one older than that is listed as retired; the dashboard says %q",
		t.auditEvery, t.auditEverySrc, barGapAuditFirstDelay, t.auditDays, t.auditDaysSrc, barHoleTF, ntwire.DeepGapBackfillMaxBarsBack, BarsMissingAction)
}

// snapshotAudit is the audit block of /api/health.
func (t *barHoleTracker) snapshotAudit() BarGapAuditView {
	t.mu.Lock()
	defer t.mu.Unlock()
	loc := kernel.CTLocation()
	v := BarGapAuditView{
		Enabled: t.auditDays > 0, WindowSessions: t.auditDays, EveryMin: int(t.auditEvery / time.Minute),
		Passes: t.audit.passes, FoundByAudit: t.audit.found, ReadErrors: t.audit.readErrors,
		LastReadErr: t.audit.lastErr, OpenHoles: len(t.holes), CrossFrameTFs: []string{"3m", "5m"},
	}
	if t.audit.passes > 0 {
		lp := t.audit.lastPass.In(loc).Format(time.RFC3339)
		wf := time.UnixMilli(t.audit.winFrom).In(loc).Format(time.RFC3339)
		wt := time.UnixMilli(t.audit.winTo).In(loc).Format(time.RFC3339)
		v.LastPass, v.WindowFrom, v.WindowTo = &lp, &wf, &wt
		v.Audited = map[string]string{}
		for k, c := range t.audit.audited {
			v.Audited[k] = c
		}
	}
	for _, h := range t.holes {
		if h.State == holeStateUnfilled || h.State == holeStateRetired {
			v.UnfilledHoles7d++
			v.UnfilledMissing += h.Missing
		}
	}
	return v
}

// BarGapAuditSnapshot is the health surface's read; ok=false → no tracker (the
// field is ABSENT, A24).
func BarGapAuditSnapshot() (BarGapAuditView, bool) {
	t := barHoles.Load()
	if t == nil {
		return BarGapAuditView{}, false
	}
	return t.snapshotAudit(), true
}
