package trader

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"nofx/kernel"
	"nofx/logger"
	"nofx/market"
	ntwire "nofx/provider/ninjatrader"
	"nofx/store"
	"nofx/telemetry"
	ntTrader "nofx/trader/ninjatrader"
)

// PICTURE-HTF EVALUATOR (2026-09-20) — the deterministic half of the owner's
// two-picture mode. It runs from native bar events, NOT from AI cycles: the
// H1 completion, the following 5m interval and the freshness verdicts are
// evaluated directly from received bars, and the AI provides commentary +
// momentum context only.
//
// Ownership: every opportunity is claimed through the store (unique key +
// atomic confirmed→place_pending transition), so repeated frames, restarts and
// the OTHER executor cannot double-submit. Only RECEIVED broker frames may
// move a row to working/filled/rejected.

// PictureHtfEvaluator is the per-trader evaluation state. Zero value is
// unusable; NewPictureHtfEvaluator builds it from the resolved strategy knob.
type PictureHtfEvaluator struct {
	mu      sync.Mutex
	at      *AutoTrader
	cfg     store.PictureHtfConfig
	enabled bool

	// Evaluation state.
	lastH1CloseEval int64 // the newest completed H1 close already scanned
	levels          []kernel.PictureHtfLevel
	levelsEval4H    int64     // the 4H close time the levels snapshot was built from
	freshest5mAt    time.Time // Go RECEIPT clock of the freshest completed 5m frame
	// D23, the other two clocks. freshest5mEmitted is the AddOn's own stamp
	// (the SOURCE clock, on another machine); freshest5mClose is the candle's
	// own close. Both are 0 until a COMPLETED 5m frame arrives — a forming
	// tick refreshes neither.
	freshest5mEmitted int64
	freshest5mClose   int64

	// H1 close series for the advisory momentum stall (last three closes).
	h1Closes []float64

	// capWarned dedupes the once-per-state "mode unavailable" log.
	capWarned bool

	// foreignFrames counts live frames whose contract is definitely NOT the
	// one this trader is on. READ by the accessor; never inferred.
	foreignFrames int64
	// unknownFrames counts frames EVALUATED while identity could not be
	// established (either side ""). They are not dropped — see OnBars — so
	// this counter is the only thing that keeps that window from being
	// silent. unknownSince/unknownWarned drive one WARN per window.
	unknownFrames int64
	unknownSince  time.Time
	unknownWarned bool
	unknownWarns  int64
	// tickSkips counts wall-clock fallbacks that found no completed frame to
	// be late about (W4/D24). READ.
	tickSkips int64
	// staleTraderSends counts sends refused at the wire because the trader
	// that STARTED the evaluation is no longer the one that would send
	// (W4/D25). READ.
	staleTraderSends int64

	// holdRefusedKey dedupes the maintenance-hold refusal (count + WARN) to
	// once per opportunity rather than once per frame.
	holdRefusedKey string

	// levelsSymbol is the symbol the levels snapshot belongs to (W-EXEC-TRUTH
	// W0 defect 4: the snapshot used to be shared across symbols).
	levelsSymbol string

	// pendingAdmission is the evidence the evaluator admitted this
	// opportunity on, handed to the send's re-admission within the SAME call
	// (both run under e.mu).
	pendingAdmission *pictureAdmission
}

// ownsSymbol reports whether a frame's symbol is this trader's instrument.
// The live sink fans every symbol's frames out to every trader; an MNQ trader
// evaluating ES bars claimed ES opportunities and sent them on MNQ with ES
// geometry (W-EXEC-TRUTH W0 defect 4).
func (e *PictureHtfEvaluator) ownsSymbol(symbol string) bool {
	_, own := e.at.latchScope()
	return instrumentRoot(symbol) != "" && instrumentRoot(symbol) == instrumentRoot(own)
}

// NewPictureHtfEvaluator builds the evaluator from the resolved strategy knob.
func NewPictureHtfEvaluator(at *AutoTrader, cfg store.PictureHtfConfig) *PictureHtfEvaluator {
	return &PictureHtfEvaluator{at: at, cfg: cfg, enabled: cfg.Enabled}
}

// Enabled reports the resolved mode switch.
func (e *PictureHtfEvaluator) Enabled() bool { return e != nil && e.enabled }

// pictureHtfSubmitSeam is the hand-off seam: since W5 the production wiring
// (trader/picture_plan_source.go init) records the claimed opportunity as a Day
// Plan scenario, and the shared armed executor places it as a LIMIT — Picture
// has no send path of its own. Tests replace it to prove the admission
// sequence. The seam receives the already-claimed opportunity row and the
// computed geometry.
//
// now is the EVALUATION's clock (W-EXEC-TRUTH W0, class 60): the send-time
// re-checks read the same instant the evaluator judged, never the wall.
var pictureHtfSubmitSeam = func(e *PictureHtfEvaluator, row *store.PictureHtfOpportunityDB, stopPx, targetPx, qty float64, now time.Time) error {
	return fmt.Errorf("picture_htf submit seam unbound (the Day Plan hand-off binds it in picture_plan_source.go)")
}

// pictureHtfCapabilityProven gates the mode on the AddOn's evidence surface
// (final + emitted_at bar markers, rejection reasons) — proven by RECEIPT of
// the far-side build id, never assumed. Tests override it.
var pictureHtfCapabilityProven = func(at *AutoTrader) bool {
	if at == nil {
		return false
	}
	tcp, ok := at.trader.(*ntTrader.TCPTrader)
	if !ok {
		return false
	}
	return tcp.FarSideProves(ntwire.MinAddonBuildPictureHtf)
}

// bars reads completed bars of a timeframe from the live provider (nil-guarded).
func (e *PictureHtfEvaluator) bars(symbol, tf string, n int, nowMs int64) []market.Kline {
	if market.FuturesBarsProvider == nil {
		return nil
	}
	raw := market.FuturesBarsProvider(symbol, tf, n)
	var out []market.Kline
	for _, b := range raw {
		// Only bars the AddOn PROVED closed count. The mode is gated on the
		// capability that guarantees final markers, so an unmarked bar is a
		// forming one — wall-clock inference is not the law here.
		if b.CloseTime < nowMs && b.Final {
			out = append(out, b)
		}
	}
	return out
}

// pictureHtfDepthMargin is how many 4H candles BEYOND the requirement the
// evaluator fetches. It must be at least 2 and is deliberately larger: the
// provider returns the TAIL of the history it holds, so that tail always
// contains the candle still forming, and the break snapshot additionally cuts
// off at the breaking candle's open, removing one more. Asking for exactly the
// requirement and then filtering to completed candles can therefore never
// satisfy the requirement — not rarely, but by construction.
const pictureHtfDepthMargin = 4

// newestFinalBar returns the newest bar the AddOn PROVED closed, if any. A
// frame of forming ticks carries none.
func newestFinalBar(bars []market.Kline) (market.Kline, bool) {
	for i := len(bars) - 1; i >= 0; i-- {
		if bars[i].Final {
			return bars[i], true
		}
	}
	return market.Kline{}, false
}

// staleBy reports how stale the freshest completed 5m data is, and on which
// clock that was measured — the worse of the AddOn's own emitted_at (SOURCE)
// and our receipt of it. The clock is NAMED in the refusal because a fallback
// that cannot be told from the real measurement is worse than no fallback:
// emitted_at is the AddOn's clock and may simply be absent.
//
// The candle's own close time is deliberately NOT one of these. NT8 emits a
// closed bar on the FIRST TICK OF THE NEXT BAR, which in slow tape is seconds
// after the boundary, so candle age (now - close) is ALWAYS >= source age
// (emitted >= close). Including it silently replaces the rule with "elapsed
// since the boundary <= freshness_sec" — 2s by default, against a 10s entry
// window — and refuses the ordinary late-emission case outright: a bar closing
// 14:00:00 and emitted 14:00:03 has a source age of 0.1s and a candle age of
// 3s. freshest5mClose stays as READ evidence (it is what tells us the boundary
// candle is in hand at all) and binds nothing.
func (e *PictureHtfEvaluator) staleBy(nowMs int64, now time.Time) (int64, string) {
	worst, clock := int64(-1), "n/a"
	consider := func(age int64, name string) {
		if age > worst {
			worst, clock = age, name
		}
	}
	if e.freshest5mEmitted > 0 {
		consider(nowMs-e.freshest5mEmitted, "source")
	}
	if !e.freshest5mAt.IsZero() {
		consider(now.Sub(e.freshest5mAt).Milliseconds(), "receipt")
	}
	return worst, clock
}

// fiveMMs is one 5m interval in milliseconds. 5m boundaries are epoch-aligned
// (300000 divides the epoch), so the arithmetic below needs no session offset.
const fiveMMs = int64(5 * 60_000)

// nextFiveMBoundary is the start of the first 5m interval beginning strictly
// after ms. A candle's CloseTime is the last instant it owns, so the boundary
// after it is the next interval's open.
func nextFiveMBoundary(ms int64) int64 {
	return (ms/fiveMMs + 1) * fiveMMs
}

// pictureUnknownContractWarnAfter is how long Picture may evaluate frames
// without contract identity before it says so out loud. One WARN per window;
// the window re-arms when identity returns.
const pictureUnknownContractWarnAfter = 60 * time.Second

// PictureDepthEvidence is what one evaluation KNOWS about its own 4H history.
// Every field is READ from the bars in hand; none is assumed. It is part of
// the evidence contract the Day Plan scenario source consumes.
type PictureDepthEvidence struct {
	Fetched   int // candles asked of the provider
	Completed int // of those, closed before the cutoff and PROVED final
	Required  int // PivotWindow + 4
}

// OK reports whether the two pictures may be drawn at all.
func (d PictureDepthEvidence) OK() bool { return d.Required > 0 && d.Completed >= d.Required }

// Reason is the refusal text, carrying the true counts rather than a verdict.
func (d PictureDepthEvidence) Reason() string {
	return fmt.Sprintf("insufficient depth %d/%d completed 4H candles", d.Completed, d.Required)
}

// depth4H fetches the 4H history with margin and reports what actually came
// back, so callers refuse on evidence instead of on a guess.
func (e *PictureHtfEvaluator) depth4H(symbol string, cutoffMs int64) (PictureDepthEvidence, []market.Kline) {
	required := e.cfg.PivotWindow + 4
	ask := required + pictureHtfDepthMargin
	bars := e.bars(symbol, "4h", ask, cutoffMs)
	return PictureDepthEvidence{Fetched: ask, Completed: len(bars), Required: required}, bars
}

// rebuildLevels recomputes the 4H body-pivot snapshot from completed bars.
func (e *PictureHtfEvaluator) rebuildLevels(symbol string, nowMs int64) PictureDepthEvidence {
	if e.levelsSymbol != symbol {
		// the snapshot is per symbol — never another instrument's levels
		e.levels, e.levelsEval4H, e.levelsSymbol = nil, 0, symbol
	}
	dep, bars := e.depth4H(symbol, nowMs)
	if !dep.OK() {
		// Too little history to draw the higher picture. Drop any snapshot
		// built from a deeper past so a later short read cannot keep trading
		// on levels this evaluation cannot justify.
		e.levels, e.levelsEval4H = nil, 0
		return dep
	}
	newest := bars[len(bars)-1].CloseTime
	if newest == e.levelsEval4H {
		return dep
	}
	e.levels = kernel.BodyPivots4H(bars, e.cfg.PivotWindow)
	e.levelsEval4H = newest
	return dep
}

// frameContract is the contract the frame's bars agree on. It returns "" when
// the frame names none (an AddOn older than the 2026-09-11 ruling) or when its
// bars disagree — both are UNKNOWN, and unknown is never reported as a match.
func frameContract(bars []market.Kline) string {
	out := ""
	for _, b := range bars {
		if b.Contract == "" {
			continue
		}
		if out == "" {
			out = b.Contract
			continue
		}
		if out != b.Contract {
			return ""
		}
	}
	return out
}

// traderStillTheSame reports whether the trader that began this evaluation is
// still the one that would send: same generation, still running, Day Plan
// still on.
func (e *PictureHtfEvaluator) traderStillTheSame(startGen int64) bool {
	if e == nil || e.at == nil {
		return false
	}
	return pictureTraderGenerationOf(e.at) == startGen && e.at.runningNow() && e.at.dayPlanEnabled()
}

// StaleTraderSends reports sends refused because the trader that started the
// evaluation was stopped or restarted before it reached the wire.
func (e *PictureHtfEvaluator) StaleTraderSends() int64 {
	if e == nil {
		return 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.staleTraderSends
}

// HasCompletedFrame reports whether a COMPLETED 5m frame has ever been seen.
// Until one has, there is no data to judge and no boundary to have missed.
func (e *PictureHtfEvaluator) HasCompletedFrame() bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.freshest5mClose > 0
}

// noteTickFallbackSkip records a fallback that ran with no completed frame.
func (e *PictureHtfEvaluator) noteTickFallbackSkip() {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tickSkips++
}

// TickFallbackSkips reports how many wall-clock fallbacks found nothing to
// evaluate — a feed that never delivers is then visible, not merely quiet.
func (e *PictureHtfEvaluator) TickFallbackSkips() int64 {
	if e == nil {
		return 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.tickSkips
}

// UnknownContractFrames reports frames evaluated while contract identity
// could not be established — the AddOn named no contract, or this trader has
// no `subscribed` ACK yet. These frames are NOT dropped, so this is the
// measure of how long Picture ran without knowing whose tape it was reading.
func (e *PictureHtfEvaluator) UnknownContractFrames() int64 {
	if e == nil {
		return 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.unknownFrames
}

// UnknownContractWarnings reports how many times the unknown window has lasted
// past pictureUnknownContractWarnAfter. One per window: it re-arms when
// identity returns, so a reconnect that loses the ACK warns again.
func (e *PictureHtfEvaluator) UnknownContractWarnings() int64 {
	if e == nil {
		return 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.unknownWarns
}

// ForeignContractFrames reports how many live frames named a contract other
// than the one this trader is on. A rising count means the tape and the
// trader disagree about the instrument — during a roll, or after the
// reconnect path that re-subscribes without a fresh ACK.
func (e *PictureHtfEvaluator) ForeignContractFrames() int64 {
	if e == nil {
		return 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.foreignFrames
}

// OnBars is the event entry point: the trader's bar consumers call it for
// native LIVE bar updates. receivedAt is the Go-side receipt time; historical
// or backfill frames must NOT call it. Non-blocking for the caller.
func (e *PictureHtfEvaluator) OnBars(symbol, tf string, bars []market.Kline, receivedAt time.Time) {
	if e == nil || !e.enabled || e.at == nil || len(bars) == 0 {
		return
	}
	if tf != "4h" && tf != "1h" && tf != "5m" {
		return
	}
	if !e.ownsSymbol(symbol) {
		return // another instrument's frame — never this trader's opportunity
	}
	// W4/D21 identity. The AddOn names the front month on EVERY bar frame, so
	// when the frame names one and this trader is provably on another, the
	// frame is a different instrument's tape and must never drive an
	// evaluation — across a roll the SYMBOL alone cannot tell them apart.
	// Read outside the evaluator's lock: currentContract reaches into the TCP
	// server, and a Picture evaluation must never hold a lock across that.
	mine, _ := pictureHtfContractOf(e.at, symbol)
	frameC := frameContract(bars)
	e.mu.Lock()
	defer e.mu.Unlock()
	if mine != "" && frameC != "" && frameC != mine {
		e.foreignFrames++
		logger.Warnf("picture-htf: frame names contract %s but this trader is on %s — frame ignored (%d so far)",
			frameC, mine, e.foreignFrames)
		return
	}
	if mine == "" || frameC == "" {
		// Identity could not be established. The frame is NOT dropped — before
		// the first `subscribed` ACK that would disable Picture for the whole
		// boot and reconnect window — but the window is never silent: it is
		// counted, it is on the boot line, and it warns once if it persists.
		e.unknownFrames++
		if e.unknownSince.IsZero() {
			e.unknownSince = receivedAt
		}
		if !e.unknownWarned && receivedAt.Sub(e.unknownSince) >= pictureUnknownContractWarnAfter {
			e.unknownWarned = true
			e.unknownWarns++
			logger.Warnf("picture-htf: contract unknown for %s — Picture frames evaluated without contract identity (trader=%q frame=%q)",
				pictureUnknownContractWarnAfter, mine, frameC)
		}
	} else {
		// Identity is established again: close the window and re-arm it, so a
		// later reconnect that loses the ACK warns on its own merits.
		e.unknownSince, e.unknownWarned = time.Time{}, false
	}
	if tf == "5m" {
		// D23: only COMPLETED data refreshes freshness. A forming tick is not
		// new evidence, and treating it as such let a stale setup look current
		// for as long as ticks kept arriving.
		if fin, ok := newestFinalBar(bars); ok {
			e.freshest5mAt = receivedAt
			e.freshest5mEmitted = fin.EmittedAt
			e.freshest5mClose = fin.CloseTime
		}
	}
	e.evaluateLocked(symbol, receivedAt)
}

// Evaluate is the tick-based fallback (session opens, reconnect): same work as
// OnBars but driven by the wall clock. Returns the verdict for the AI context
// and dashboard.
func (e *PictureHtfEvaluator) Evaluate(symbol string, now time.Time) EvaluateResult {
	if e == nil || !e.enabled {
		return EvaluateResult{Stage: "watching"}
	}
	if !e.ownsSymbol(symbol) {
		return EvaluateResult{Stage: "watching", Reason: "not this trader's instrument"}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.evaluateLocked(symbol, now)
}

// EvaluateResult is the per-evaluation verdict for the dashboard/AI context.
type EvaluateResult struct {
	Stage    string // watching | confirmed | refused | expired | submitted
	Reason   string
	OppKey   string
	Momentum *kernel.MomentumStall
}

func (e *PictureHtfEvaluator) evaluateLocked(symbol string, now time.Time) EvaluateResult {
	nowMs := now.UnixMilli()
	// D25: the generation this evaluation BEGAN under. Re-checked immediately
	// before the wire — a Stop or a restart between here and there must not
	// send on behalf of a trader that no longer exists.
	startGen := pictureTraderGenerationOf(e.at)
	if !pictureHtfCapabilityProven(e.at) {
		if !e.capWarned {
			e.capWarned = true
			logger.Warnf("picture-htf: mode unavailable — AddOn evidence missing (need build ≥ %s; F5-compile + full NT8 restart with the new AddOn)", ntwire.MinAddonBuildPictureHtf)
		}
		return EvaluateResult{Stage: "watching", Reason: "mode unavailable — AddOn evidence missing"}
	}
	e.capWarned = false
	if dep := e.rebuildLevels(symbol, nowMs); !dep.OK() {
		// D21: no level and no trade until the history is provably deep
		// enough. The reason carries the counts that were READ.
		return EvaluateResult{Stage: "watching", Reason: dep.Reason()}
	}

	// --- H1 completion scan + advisory momentum ---
	h1 := e.bars(symbol, "1h", 4, nowMs)
	for _, b := range h1 {
		if b.CloseTime <= e.lastH1CloseEval {
			continue
		}
		e.lastH1CloseEval = b.CloseTime
		e.h1Closes = append(e.h1Closes, b.Close)
		if len(e.h1Closes) > 3 {
			e.h1Closes = e.h1Closes[len(e.h1Closes)-3:]
		}
	}
	var stall *kernel.MomentumStall
	if len(e.h1Closes) == 3 {
		if s := kernel.H1MomentumStall(e.h1Closes); s.Fired {
			stall = &s
		}
	}

	// --- H1 breakout over the last two completed H1 candles ---
	if len(h1) < 2 || e.lastH1CloseEval == 0 {
		return EvaluateResult{Stage: "watching", Momentum: stall}
	}
	prev, cur := h1[len(h1)-2], h1[len(h1)-1]
	// SIMULTANEOUS H1/4H COMPLETION (spec): the breakout reference is the
	// level set as it stood BEFORE the confirming H1 opened — retirements
	// from a 4h candle closing AT the same boundary as the H1 (which contains
	// the H1's own move) must NOT retro-kill the breakout. So the break check
	// uses a snapshot rebuilt from 4H bars completed before cur.OpenTime.
	// Target selection, below, uses the as-of-now snapshot — retirements ARE
	// applied before target selection, per the same clause. Both snapshots
	// are time-derived from the cache, so frame arrival order cannot change
	// either verdict.
	// The break snapshot reads the same depth rule: its cutoff removes at
	// least one more candle, which is exactly what the margin is for.
	breakDep, breakBars := e.depth4H(symbol, cur.OpenTime)
	if !breakDep.OK() {
		return EvaluateResult{Stage: "watching", Reason: breakDep.Reason(), Momentum: stall}
	}
	breakLevels := kernel.ActiveLevels(
		kernel.BodyPivots4H(breakBars, e.cfg.PivotWindow),
		cur.OpenTime)
	breakVerdict := kernel.H1CloseBreak(breakLevels, prev, cur, e.cfg.TickSize)
	if !breakVerdict.Fired {
		return EvaluateResult{Stage: "watching", Momentum: stall}
	}

	// --- Eligibility: the following 5m interval + freshness ---
	fiveM := e.bars(symbol, "5m", e.cfg.SwingLookback+4, nowMs)
	if len(fiveM) == 0 {
		return EvaluateResult{Stage: "watching", Momentum: stall}
	}
	newest5m := fiveM[len(fiveM)-1]
	// D23: the entry window belongs to the H1 close that CONFIRMED the break —
	// it is the 5m interval that follows THAT close, and it happens once.
	// Measuring from `newest5m.OpenTime + 5m` moved the anchor forward with
	// every new 5m candle, so the window re-opened every five minutes,
	// indefinitely, for a break confirmed long before.
	intervalStart := nextFiveMBoundary(cur.CloseTime)
	elapsed := nowMs - intervalStart
	windowMs := int64(e.cfg.EntryWindowSec) * 1000
	if elapsed < 0 {
		// The next interval has not begun — wait for its boundary frame.
		return EvaluateResult{Stage: "watching", Momentum: stall}
	}
	level := breakLevels[breakVerdict.LevelIdx]
	// The system's strategy identity IS the trader id (plans.strategy_id =
	// trader id) — the opportunity key uses the same binding.
	strategyID := e.at.id
	contract, _ := e.at.currentContract(symbol)
	oppKey := store.PictureHtfOppKey(strategyID, e.at.currentAccountName(), contract, breakVerdict.Direction, level.Role, level.SourceOpen, cur.CloseTime)
	// D23/R2 — THE BOUNDARY CANDLE MUST BE IN HAND BEFORE ANY REFUSAL.
	// The H1 closed frame and the 5m closed frame both arrive AFTER the
	// boundary, and the H1 one typically lands first (~1.0s vs ~1.2s). In that
	// gap the freshness stamps still describe the PREVIOUS 5m candle, so every
	// clock reads ~5 minutes stale and a refusal written then is a durable
	// `expired` that the qualifying 5m frame, 200ms later, would inherit.
	// Before D23 this was masked: forming ticks refreshed the receipt stamp,
	// so the gap looked fresh — and the entry that followed was computed on
	// the previous candle's geometry. Making only completed data count
	// unmasked it, so the wait is explicit.
	boundaryClose := intervalStart - 1
	if e.freshest5mClose < boundaryClose {
		return EvaluateResult{Stage: "watching", Momentum: stall,
			Reason: fmt.Sprintf("awaiting the completed 5m close at %d", boundaryClose)}
	}
	// It is in hand — but did WE have it while the window was still open? A
	// candle delivered after the window shut was never actionable, so it is
	// not an opportunity we refused; no durable row is written for it.
	if !e.freshest5mAt.IsZero() && e.freshest5mAt.UnixMilli() > intervalStart+windowMs {
		return EvaluateResult{Stage: "watching", Momentum: stall,
			Reason: "the completed 5m close arrived after the entry window — never actionable"}
	}
	if elapsed > windowMs {
		return e.refuse(oppKey, "expired", fmt.Sprintf("entry window passed (%dms > %dms)", elapsed, windowMs), stall)
	}
	// ARRIVAL-ORDER INDEPENDENCE: the first 5m frame of the interval has not
	// been received yet. The entry cannot be sent without a fresh receipt, so
	// the mode WAITS — it must NOT write a refusal row that a qualifying
	// in-window frame arriving milliseconds later would have to live with.
	// (A 4h/1h frame landing just before the 5m boundary frame must not kill
	// the setup.)
	if e.freshest5mAt.IsZero() {
		return EvaluateResult{Stage: "watching", Reason: "awaiting the first 5m frame of the interval", Momentum: stall}
	}
	// D23: three clocks, separately measured, and the limit binds the WORST of
	// them. Source age answers "how old is this data"; receipt age answers
	// "how long did this frame sit in our own queue". Either alone leaves a
	// hole: fresh data delivered late, or a prompt delivery of stale data.
	if age, clock := e.staleBy(nowMs, now); age > int64(e.cfg.FreshnessSec)*1000 {
		return e.refuse(oppKey, "expired",
			fmt.Sprintf("%s age %dms exceeds the freshness limit (%ds) — a late frame cannot enter", clock, age, e.cfg.FreshnessSec), stall)
	}

	// --- Geometry: structural stop + opposing-zone target ---
	stopPx, ok := kernel.StructuralSwing5M(fiveM, breakVerdict.Direction, e.cfg.SwingLookback, cur.CloseTime)
	if !ok {
		return e.refuse(oppKey, "refused", "no confirmed 5m swing stop before the H1 close — no trade", stall)
	}
	stopPx -= e.cfg.TickSize
	if breakVerdict.Direction == "short" {
		stopPx = stopPx + 2*e.cfg.TickSize
	}
	entryRef := newest5m.Close
	targetPx, ok := kernel.NearestOpposingZone(kernel.ActiveLevels(e.levels, nowMs), entryRef, breakVerdict.Direction, nowMs)
	if !ok {
		return e.refuse(oppKey, "refused", "no eligible opposing 4H zone — no trade", stall)
	}
	risk := absF(entryRef - stopPx)
	reward := absF(targetPx - entryRef)
	rr := 0.0
	if risk > 0 {
		rr = reward / risk
	}
	// W-EXEC-TRUTH W0 (Q7, D12): the floor is max(Picture's own knob, the
	// strategy floor) — a knob below the strategy floor never loosens it — and
	// with no strategy config there is no floor, so the opportunity refuses.
	minRR, floorOK := e.at.pictureMinRR(e.cfg.MinRR)
	if !floorOK {
		return e.refuse(oppKey, "refused", "no R:R floor resolvable (no strategy config) — fail-closed", stall)
	}
	if rr < minRR {
		return e.refuse(oppKey, "refused", fmt.Sprintf("nearest opposing zone offers %.2fR; the configured minimum is %.2fR — the nearer zone is never skipped", rr, minRR), stall)
	}

	// --- Admission: claim the row, then atomically own the submission ---
	row := &store.PictureHtfOpportunityDB{
		OppKey: oppKey, TraderID: e.at.id, StrategyID: strategyID,
		Account: e.at.currentAccountName(), Contract: contract, Symbol: symbol,
		Direction: breakVerdict.Direction, RuleVer: 1, Stage: "confirmed",
		LevelRole: level.Role, LevelBodyTop: level.BodyTop, LevelBodyBot: level.BodyBottom,
		LevelWickHi: level.WickHigh, LevelWickLo: level.WickLow,
		LevelBarOpen: level.SourceOpen, LevelKnowable: level.KnowableAt,
		H1PrevClose: breakVerdict.PrevClose, H1NewClose: breakVerdict.NewClose, H1Boundary: breakVerdict.Boundary,
		H1OpenTime: cur.OpenTime, H1CloseTime: cur.CloseTime, H1Completion: nowMs,
		WindowOpen: intervalStart, WindowClose: intervalStart + windowMs, FreshVerdict: "fresh",
		EntryRef: entryRef, StopPx: stopPx, StopSource: "5m swing", TargetPx: targetPx, TargetZone: level.Role,
		RREstimate: rr, RRConfigured: minRR,
	}
	if stall != nil {
		row.MomentumStall = stall.Fired
		row.MomentumDir = stall.Direction
	}
	// W-ONE-BUTTON M2 site 3 — THE MAINTENANCE HOLD refuses BEFORE the
	// claim. A claim followed by a refused send would leave the row
	// place_pending ("ambiguous"), blocking re-entry AND the installation gate
	// until reconciled. The refusal is durable (fail-closed): an opportunity
	// seen during maintenance never trades, even after the hold clears.
	if reason, held := MaintenanceHeld(); held {
		return e.refuseHeld(oppKey, reason, stall)
	}
	// W-EXEC-TRUTH W0 (a) — THE ONE ADMISSION GATE, before the claim. A
	// refusal here writes NO row: a transient gate (a pause, a feed flap, the
	// dead-man) must not kill the hour's opportunity for good — the next frame
	// asks again. Counted and logged once per change (admitRefuse).
	adm := &pictureAdmission{
		EntryRef: entryRef, LatestClose: e.latestClose(symbol, nowMs), Stop: stopPx, Target: targetPx,
		ATR5m: armSeamATR5mFromBars(fiveM), KnobMinRR: e.cfg.MinRR,
	}
	if refusal, refused := e.at.admitEntry(admitIntent{
		Path: admitPicture, Symbol: symbol, Action: "open_" + breakVerdict.Direction, Now: now,
		Key: oppKey, Price: entryRef, Picture: adm,
	}); refused {
		return EvaluateResult{Stage: "watching", Reason: "admission refused: " + refusal, OppKey: oppKey, Momentum: stall}
	}
	e.pendingAdmission = adm
	_, fresh, err := e.at.store.PictureHtfClaim(row)
	if err != nil {
		return EvaluateResult{Stage: "watching", Reason: "store claim failed: " + err.Error(), OppKey: oppKey, Momentum: stall}
	}
	if !fresh {
		// Duplicate frame/restart — the opportunity already exists (a refused
		// row counts as an existing opportunity; the claim is durable).
		return EvaluateResult{Stage: "watching", Reason: "opportunity already claimed", OppKey: oppKey, Momentum: stall}
	}
	signalID := fmt.Sprintf("picture-htf-%d", nowMs)
	won, err := e.at.store.PictureHtfClaimSubmission(oppKey, signalID)
	if err != nil || !won {
		return EvaluateResult{Stage: "watching", Reason: "submission ownership lost", OppKey: oppKey, Momentum: stall}
	}
	// W-EXEC-TRUTH W0 defect 2: the claim id rides into the send, whose ledger
	// stamp is refused for any other owner. It was never assigned, so every
	// stamp was refused and Picture's wire path was dead.
	row.SignalID = signalID
	// The atomic owner hands off. The seam is the Day Plan hand-off (W5): it
	// records the scenario and settles the row planned; an error before the
	// record leaves the row for the refusal below, never a blind resend.
	// D25 — THE LAST GATE BEFORE THE WIRE. W0b checks running and Day Plan at
	// ADMISSION; this is the re-check immediately before the send, where a
	// Stop, a restart or a Day Plan switch-off between the two would otherwise
	// let an in-flight evaluation reach the broker for a trader that is gone.
	// It sits HERE rather than in picture_htf_send.go (which W5 retires) and
	// leaves pictureHtfSubmitSeam's signature untouched.
	if !e.traderStillTheSame(startGen) {
		e.staleTraderSends++
		logger.Warnf("picture-htf: send REFUSED — the trader that began this evaluation is gone (generation %d → %d, running=%v, day_plan=%v); opportunity %s not sent",
			startGen, pictureTraderGenerationOf(e.at), e.at.runningNow(), e.at.dayPlanEnabled(), store.RedactPictureOppKey(oppKey))
		return e.refuse(oppKey, "refused", "trader stopped or restarted before the send — no entry", stall)
	}
	if err := pictureHtfSubmitSeam(e, row, stopPx, targetPx, 0, now); err != nil {
		// A maintenance-hold refusal PROVES nothing reached the wire: the
		// permit is taken before the ledger stamp and the send, and a queued
		// entry dropped under the hold is never written. So the row settles
		// refused instead of sitting ambiguous place_pending.
		if ntTrader.IsMaintenanceHold(err) {
			return e.refuseHeld(oppKey, err.Error(), stall)
		}
		// W-EXEC-TRUTH W0: the submission stamp is written in beforeSend, the
		// last step before the wire. No stamp = the send never started, so the
		// row settles refused — provably unsent — instead of an ambiguous
		// place_pending that blocks every later Picture entry and the
		// installation gate.
		// An explicit "a write had started" (the hold's ambiguous drop) is
		// evidence the other way and always stays pending.
		if cur, ok, gerr := e.at.store.PictureHtfGet(oppKey); gerr == nil && ok && cur.SubmittedAt == 0 && !errors.Is(err, ntwire.ErrEntryDropAmbiguous) {
			return e.refuse(oppKey, "refused", "never sent — "+err.Error(), stall)
		}
		// The send failed AFTER the stamp — the row stays place_pending and
		// blocks re-entry until reconciled against NT8 orders (addendum #4).
		return EvaluateResult{Stage: "submitted", Reason: "send ambiguous: " + err.Error(), OppKey: oppKey, Momentum: stall}
	}
	return EvaluateResult{Stage: pictureHtfSeamDoneStage, OppKey: oppKey, Momentum: stall}
}

// refuseHeld is the maintenance-hold refusal: a durable "refused" row, counted
// as the maintenance_hold gate block and WARNed once per opportunity.
func (e *PictureHtfEvaluator) refuseHeld(oppKey, reason string, stall *kernel.MomentumStall) EvaluateResult {
	if e.holdRefusedKey != oppKey {
		e.holdRefusedKey = oppKey
		telemetry.IncGateBlock(e.at.id, "maintenance_hold")
		logger.Warnf("🔒 picture-htf: opportunity %s REFUSED — maintenance hold: %s. It will not be traded after the update.", store.RedactPictureOppKey(oppKey), reason)
	}
	return e.refuse(oppKey, "refused", "maintenance hold — "+reason, stall)
}

func (e *PictureHtfEvaluator) refuse(oppKey, stage, reason string, stall *kernel.MomentumStall) EvaluateResult {
	if e.at != nil && e.at.store != nil && oppKey != "" {
		_, _, _ = e.at.store.PictureHtfClaim(&store.PictureHtfOpportunityDB{OppKey: oppKey, TraderID: e.at.id, Stage: stage, StageReason: reason})
		// W-EXEC-TRUTH W0 (Q8): only a row no send has started may be refused.
		_, _ = e.at.store.PictureHtfRefuse(oppKey, stage, reason)
	}
	return EvaluateResult{Stage: stage, Reason: reason, OppKey: oppKey, Momentum: stall}
}

// latestClose is the newest 1m close for symbol (forming allowed — it is the
// price a market entry would meet), 0 when unknown (W-EXEC-TRUTH W0 Q19).
func (e *PictureHtfEvaluator) latestClose(symbol string, nowMs int64) float64 {
	if market.FuturesBarsProvider == nil {
		return 0
	}
	raw := market.FuturesBarsProvider(symbol, "1m", 2)
	if len(raw) == 0 {
		return 0
	}
	if b := raw[len(raw)-1]; b.OpenTime <= nowMs {
		return b.Close
	}
	return 0
}
