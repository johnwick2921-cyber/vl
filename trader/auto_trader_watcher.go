package trader

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"vl/kernel"
	"vl/market"
	"vl/store"
)

// Phase 3 — IN-POSITION WATCHER (final-bundle 2026-08-19). Owner ruling:
// in-position the AI is WATCH-ONLY, zero order authority. This file holds the
// whole watch pipeline and deliberately NEVER references the broker: no
// executor call, no wire frame, no stop amendment originates here. A source
// contract test (watcher_no_wire_test.go) pins that structurally.
//
// Modes (per-trader DB column position_mode, resolved by positionMode()):
//   ai_watch     (DEFAULT) — full watch cycle while holding: snapshot+equity
//                 first (#49 ordering), observer prompt, assessment recorded.
//   bracket_only — the legacy skip-while-open heartbeat, byte-identical.
//
// Hysteresis rails (owner's Jan-2026 signal-stability research; env-tunable):
//   R1 "invalidated" accepted only with a cited condition AND confidence ≥
//      WATCH_INVALIDATE_MIN_CONF (default 70); else recorded as weakening.
//   R2 status downgrades at most one step per cycle and never within
//      WATCH_MIN_HOLD_CYCLES (default 2) of the last change; upgrades free.
//   R3 WARN only after WATCH_WARN_CONSECUTIVE (default 2) consecutive ACCEPTED
//      invalidated reads; one WARN per position per episode; recovery clears.
//   R4 the WARN feeds NOTHING else — dashboard + scoring table only.

const (
	PositionModeWatch       = "ai_watch"
	PositionModeBracketOnly = "bracket_only"

	defaultWatchInvalidateMinConf = 70
	defaultWatchMinHoldCycles     = 2
	defaultWatchWarnConsecutive   = 2
)

// positionMode resolves the trader's in-position mode. Only an explicit
// "bracket_only" keeps the legacy skip; empty/anything else = ai_watch (the
// NEW default, owner ruling).
func (at *AutoTrader) positionMode() string {
	if at.config.PositionMode == PositionModeBracketOnly {
		return PositionModeBracketOnly
	}
	return PositionModeWatch
}

func envIntDefault(name string, def int) int {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func watchInvalidateMinConf() int {
	return envIntDefault("WATCH_INVALIDATE_MIN_CONF", defaultWatchInvalidateMinConf)
}
func watchMinHoldCycles() int {
	return envIntDefault("WATCH_MIN_HOLD_CYCLES", defaultWatchMinHoldCycles)
}
func watchWarnConsecutive() int {
	return envIntDefault("WATCH_WARN_CONSECUTIVE", defaultWatchWarnConsecutive)
}

// ---- entry thesis (the anchor the observer judges against) ----

type entryThesis struct {
	Action     string
	Symbol     string
	Entry      float64
	StopLoss   float64
	TakeProfit float64
	Reasoning  string
	Confidence int
	EntryMs    int64
}

// captureEntryThesis stashes the ORIGINAL entry decision at execution time so
// watch cycles can quote it verbatim. Run-loop goroutine only.
func (at *AutoTrader) captureEntryThesis(d *kernel.Decision, side string, fillPrice float64) {
	if d == nil {
		return
	}
	if at.entryTheses == nil {
		at.entryTheses = make(map[string]entryThesis)
	}
	entry := fillPrice
	if entry <= 0 {
		entry = d.Price
	}
	at.entryTheses[d.Symbol+"_"+side] = entryThesis{
		Action: d.Action, Symbol: d.Symbol, Entry: entry,
		StopLoss: d.StopLoss, TakeProfit: d.TakeProfit,
		Reasoning: d.Reasoning, Confidence: d.Confidence,
		EntryMs: time.Now().UnixMilli(),
	}
}

// thesisFor returns the entry thesis for an open position: memory first, then a
// one-time DB reconstruction (restart case) from the decision record whose
// entry produced this position.
func (at *AutoTrader) thesisFor(p *store.TraderPosition) entryThesis {
	key := p.Symbol + "_" + p.Side
	if th, ok := at.entryTheses[key]; ok {
		return th
	}
	th := entryThesis{
		Symbol: p.Symbol, Entry: p.EntryPrice, Confidence: p.EntryConfidence, EntryMs: p.EntryTime,
	}
	// Restart fallback: find the entry decision in the DB (recorded within a
	// small window around the position's entry time).
	if at.store != nil {
		if recs, err := at.store.Decision().GetLatestRecords(at.id, 40); err == nil {
			wantAction := "open_long"
			if p.Side == "SHORT" {
				wantAction = "open_short"
			}
			for _, r := range recs {
				dtMs := r.Timestamp.UnixMilli()
				if dtMs > p.EntryTime+2*60_000 || dtMs < p.EntryTime-10*60_000 {
					continue
				}
				for i := range r.Decisions {
					d := &r.Decisions[i]
					if d.Action == wantAction && d.Symbol == p.Symbol {
						th.StopLoss = d.StopLoss
						th.TakeProfit = d.TakeProfit
						th.Reasoning = d.Reasoning
						if d.Confidence > 0 {
							th.Confidence = d.Confidence
						}
						if at.entryTheses == nil {
							at.entryTheses = make(map[string]entryThesis)
						}
						at.entryTheses[key] = th
						return th
					}
				}
			}
		}
	}
	return th
}

// ---- hysteresis state + rails ----

type watchState struct {
	Status        string // last ACCEPTED (post-rails) status; "" = fresh (intact)
	LastChangeAt  int    // watch-cycle index of the last status change
	Cycles        int    // watch cycles seen for this position
	ConsecInvalid int    // consecutive R1-ACCEPTED invalidated reads
	Warned        bool   // R3: one WARN per episode
	BestPts       float64
	WorstPts      float64
	// G8 (regime wave 2026-08-21) — structure-conflict hysteresis: the stored
	// verdict only colors after 2 consecutive identical non-none reads.
	SCVerdict string // current accepted structure verdict
	SCConsec  int    // consecutive identical non-none reads
}

type watchRailKnobs struct{ minConf, minHold, warnConsec int }

func liveWatchRailKnobs() watchRailKnobs {
	return watchRailKnobs{watchInvalidateMinConf(), watchMinHoldCycles(), watchWarnConsecutive()}
}

func statusRank(s string) int {
	switch s {
	case "weakening":
		return 1
	case "invalidated":
		return 2
	}
	return 0 // intact / ""
}

// applyWatchRails is the pure hysteresis core (R1–R3). It takes the previous
// state and the raw read, and returns the new state, the ACCEPTED status this
// cycle, and whether the R3 WARN fires now.
func applyWatchRails(st watchState, rawStatus string, conf int, cited string, k watchRailKnobs) (watchState, string, bool) {
	st.Cycles++
	prev := st.Status
	if prev == "" {
		prev = "intact"
	}

	// R1 — an invalidated read must cite the condition AND clear the confidence
	// bar, else it is recorded as weakening (note preserved by the caller).
	read := rawStatus
	acceptedInvalidRead := false
	if read == "invalidated" {
		if cited == "" || conf < k.minConf {
			read = "weakening"
		} else {
			acceptedInvalidRead = true
		}
	}

	// R2 — downgrades clamp to one step per cycle and freeze within minHold
	// cycles of the last change; upgrades (recovery) are free.
	next := read
	if statusRank(read) > statusRank(prev) {
		if st.Cycles-st.LastChangeAt <= k.minHold && st.LastChangeAt > 0 {
			next = prev // too soon after the last change — hold
		} else if statusRank(read) > statusRank(prev)+1 {
			next = "weakening" // intact → invalidated clamps to one step
		}
	}
	if next != prev {
		st.LastChangeAt = st.Cycles
	}
	st.Status = next

	// R3 — consecutive ACCEPTED invalidated reads drive the single WARN.
	warn := false
	if acceptedInvalidRead {
		st.ConsecInvalid++
		if st.ConsecInvalid >= k.warnConsec && !st.Warned {
			st.Warned = true
			warn = true
		}
	} else {
		// Recovery clears the episode.
		st.ConsecInvalid = 0
		st.Warned = false
	}
	return st, next, warn
}

// ---- the watch cycle ----

// runWatchCycle executes one WATCH-ONLY cycle for the held position. ctx is the
// already-built trading context (#49 ordering: snapshot+equity recorded first).
// NOTHING here touches the broker.
func (at *AutoTrader) runWatchCycle(ctx *kernel.Context, record *store.DecisionRecord) error {
	positions, err := at.store.Position().GetOpenPositions(at.id)
	if err != nil || len(positions) == 0 {
		return err
	}
	p := positions[0]
	key := p.Symbol + "_" + p.Side

	// U1 (watcher-eyes hotfix): the loop-built ctx has NO market data — that
	// only exists on the decision path. Fetch it (and stamp SnapshotMs + the
	// prompt snapshot) so the observer sees real bars, forming labels, and a
	// live price. Fail-safe: on fetch failure the cycle records honestly and
	// skips the AI call rather than observing blind.
	if err := kernel.EnsureMarketData(ctx, at.strategyEngine); err != nil {
		at.logWarnf("👁 watch cycle: market fetch failed (%v) — recording heartbeat without an observer read.", err)
		record.CycleType = "watch"
		record.ExecutionStatus = "watch"
		record.Success = true
		record.ExecutionLog = append(record.ExecutionLog, "watch: market fetch failed — no observer call this cycle")
		at.saveDecision(record)
		return nil
	}
	if at.watchStates == nil {
		at.watchStates = make(map[string]*watchState)
	}
	st, ok := at.watchStates[key]
	if !ok {
		st = &watchState{}
		at.watchStates[key] = st
	}

	// Position state for the prompt.
	curPrice := 0.0
	if md := ctx.MarketDataMap[p.Symbol]; md != nil {
		curPrice = md.CurrentPrice
	}
	pnlPts := 0.0
	if curPrice > 0 {
		if p.Side == "LONG" {
			pnlPts = curPrice - p.EntryPrice
		} else {
			pnlPts = p.EntryPrice - curPrice
		}
	}
	if pnlPts > st.BestPts {
		st.BestPts = pnlPts
	}
	if pnlPts < st.WorstPts {
		st.WorstPts = pnlPts
	}
	th := at.thesisFor(p)
	beFired := false
	at.breakevenMu.Lock()
	beFired = at.breakevenDone[key]
	at.breakevenMu.Unlock()
	curStop := th.StopLoss
	if beFired && p.EntryPrice > 0 {
		curStop = p.EntryPrice
	}
	trailLvl := at.currentTrailLevel(key)
	if trailLvl > 0 {
		if p.Side == "LONG" && trailLvl > curStop {
			curStop = trailLvl
		}
		if p.Side == "SHORT" && (curStop == 0 || trailLvl < curStop) {
			curStop = trailLvl
		}
	}
	prevStatus := st.Status

	// T2 (2026-08-26) — touch telemetry for the watcher: feed the registry from
	// the same 1m bars + seated map, then render the nearest 2 TOUCH lines.
	// Advisory — zero gates, zero order authority.
	touchLines := ""
	if market.FuturesBarsProvider != nil {
		if bars := market.FuturesBarsProvider(p.Symbol, "1m", kernel.AISVPBarCount); len(bars) > 0 {
			now := time.Now()
			// S1 (mega-research 2026-08-26) — the watcher's hardcoded 1.5
			// proximity literal survived the config retune (0.3); the seated log
			// exposed it (±459pt vs the executor's ±92pt). Resolve like every
			// other path.
			if sc, _, _ := kernel.AssembleScoredLevels(at.id, bars, kernel.ResolvedSessionRegistryFor(at.id), p.Symbol, 8, now, at.proximityFilterATR()); len(sc) > 0 {
				atr := market.ExportCalculateATR(bars, 14)
				for _, ep := range kernel.TouchUpdate(at.id, p.Symbol, bars, sc, atr, now) {
					if kernel.TouchEpisodeSink != nil {
						kernel.TouchEpisodeSink(ep)
					}
				}
				touchLines = kernel.RenderTouchLines(at.id, p.Symbol, curPrice, 2)
			}
		}
	}

	in := kernel.ObserverInput{
		Symbol: p.Symbol, Side: p.Side,
		EntryPrice: p.EntryPrice, StopLoss: th.StopLoss, TakeProfit: th.TakeProfit,
		Thesis: th.Reasoning, EntryConfidence: th.Confidence,
		AgeMinutes: int((time.Now().UnixMilli() - p.EntryTime) / 60_000),
		PnLPoints:  pnlPts, MFEPoints: st.BestPts, MAEPoints: st.WorstPts,
		CurrentStop: curStop, BreakevenFired: beFired, TrailLevel: trailLvl,
		PrevStatus:    prevStatus,
		StructureLine: kernel.StructurePromptLine(ctx.Structure),
		TouchLines:    touchLines,
	}
	record.AccountState = store.AccountSnapshot{
		TotalBalance:          ctx.Account.TotalEquity,
		AvailableBalance:      ctx.Account.AvailableBalance,
		TotalUnrealizedProfit: ctx.Account.UnrealizedPnL,
		PositionCount:         ctx.Account.PositionCount,
		InitialBalance:        at.initialBalance,
	}
	systemPrompt := at.strategyEngine.BuildObserverSystemPrompt(in, ctx.SnapshotMs)
	userPrompt := at.strategyEngine.BuildUserPrompt(ctx)
	record.SystemPrompt = systemPrompt
	record.InputPrompt = userPrompt
	record.CycleType = "watch"
	record.ExecutionStatus = "watch"

	raw, dur, callErr := kernel.CallObserver(at.mcpClient, systemPrompt, userPrompt)
	record.RawResponse = raw
	record.AIRequestDurationMs = dur.Milliseconds()
	// U3 (watcher-eyes): watch-call latencies deliberately do NOT feed the
	// dodge's rolling average — observer prompts are a different shape, and a
	// long hold would mis-size the dodge window for the next DECISION cycle.
	if callErr != nil {
		record.Success = false
		record.ErrorMessage = "watch call failed: " + callErr.Error()
		at.classifyWatchCallError(record, callErr)
		at.saveDecision(record)
		return nil
	}
	// F5 (REL17) — a successful observer call clears any latched 402 outage,
	// same as the decision path (the watch path already raises on 402 via
	// classifyWatchCallError). Gated on the kill-switch; a parse error below
	// is still a successful DeepSeek call and still clears.
	if ai402ClearAllOn() {
		at.onAISuccess(time.Now())
	}

	a, parseErr := kernel.ParseObserverAssessment(raw)
	if parseErr != nil {
		// Unparseable watch read: recorded, no rail movement, never retried.
		record.Success = true
		record.WatchJSON = fmt.Sprintf(`{"thesis_status":"unparseable","note":%q}`, parseErr.Error())
		record.ExecutionLog = append(record.ExecutionLog, "watch: unparseable observer response (no rail movement)")
		at.saveDecision(record)
		return nil
	}
	if a.ActionIgnored {
		at.logWarnf("👁 observer_action_ignored — the observer response carried an action-like field; watch cycles have zero order authority, field ignored.")
		record.ExecutionLog = append(record.ExecutionLog, "observer_action_ignored: action-like field in watch response dropped")
	}

	newSt, accepted, warn := applyWatchRails(*st, a.ThesisStatus, a.Confidence, a.InvalidationCited, liveWatchRailKnobs())
	*st = newSt

	// G8 (regime wave 2026-08-21) — structure-conflict hysteresis: a non-none
	// verdict colors the badge only after 2 consecutive identical reads; any
	// change resets the run. Zero order authority — this feeds only the dot.
	scAccepted := st.SCVerdict
	if a.StructureConflict == st.SCVerdict && a.StructureConflict != "none" && a.StructureConflict != "" {
		st.SCConsec++
	} else {
		st.SCConsec = 0
		if a.StructureConflict != "" && a.StructureConflict != "none" {
			st.SCConsec = 1
		}
	}
	if st.SCConsec >= 2 && a.StructureConflict != "" {
		scAccepted = a.StructureConflict
	}

	if warn {
		// R3 WARN — Phase 1 path (WARN → journald + log_events + dashboard).
		// R4: this feeds NOTHING else — no gate, no prompt, no post-exit context.
		at.logWarnf("⚠ THESIS INVALIDATED (%d×): %s — %s %s (conf %d). Watch-only: bracket/rails keep managing the trade.",
			st.ConsecInvalid, a.InvalidationCited, p.Symbol, p.Side, a.Confidence)
		// G8 — the strongest honest signal: thesis invalidated AND structure
		// confirmed-conflict. Watch-only; zero order authority.
		if scAccepted == "confirmed" {
			at.logWarnf("👁 STRUCTURE CONFIRMED-CONFLICT — thesis invalidated AND machine structure contradicts the %s %s position (badge red). Watch-only: nothing emitted.", p.Symbol, p.Side)
		}
	}

	wj, _ := json.Marshal(map[string]any{
		"thesis_status":      a.ThesisStatus,
		"accepted_status":    accepted,
		"invalidation_cited": a.InvalidationCited,
		"structure_conflict": scAccepted,
		"note":               a.Note,
		"confidence":         a.Confidence,
		"warned":             warn,
	})
	record.WatchJSON = string(wj)
	record.Success = true
	record.ExecutionLog = append(record.ExecutionLog,
		fmt.Sprintf("watch: status=%s accepted=%s conf=%d (in-position observer — nothing emitted)", a.ThesisStatus, accepted, a.Confidence))
	at.saveDecision(record)

	if at.store != nil {
		_ = at.store.WatchAssessment().Create(&store.WatchAssessment{
			TraderID: at.id, PositionID: p.ID, CycleNumber: at.callCount,
			Timestamp: time.Now().UnixMilli(),
			Status:    a.ThesisStatus, AcceptedStatus: accepted, Confidence: a.Confidence,
			InvalidationCited: a.InvalidationCited, Note: a.Note,
			PriceAtRead: curPrice, Warned: warn,
			StructureConflict: scAccepted,
		})
	}
	return nil
}

// pruneWatchState clears watcher memory once the trader is flat (re-arms for
// the next position). Run-loop goroutine only.
func (at *AutoTrader) pruneWatchState() {
	if len(at.watchStates) > 0 {
		at.watchStates = make(map[string]*watchState)
	}
	if len(at.entryTheses) > 0 {
		at.entryTheses = make(map[string]entryThesis)
	}
}

// classifyWatchCallError reuses the P5 402 typing for watch calls.
func (at *AutoTrader) classifyWatchCallError(record *store.DecisionRecord, err error) {
	record.ErrorClass = classifyAIError(err)
	if record.ErrorClass == "ai_payment_402" {
		at.on402Failure(time.Now(), err)
	}
}
