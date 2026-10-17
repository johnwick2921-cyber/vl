package trader

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"nofx/kernel"
	"nofx/market"
	"nofx/store"
)

// ── W6 wake wave (2026-08-25) — event-diff planner wake-ups ─────────────────
//
// The planner reads once per session + re-plans on death + wakes on a structure
// MSS (G4.6). W6 adds the FIFTH class of wake: level events — fresh S/D zones,
// FVGs, OBs and iFVGs the plan never saw, plus invalidation of levels it DID
// seat. This is deterministic change detection (research: CUSUM/change-point
// family), not statistical detection: the detector output is diffed against the
// plan row's birth time (row.CreatedAt), a wake fires for the newest event, and
// wake_min_interval_min is the false-alarm/detection-delay knob. W6-D: wakes are
// unlimited and spend NO re-plan budget — only deaths consume replan_cap, so a
// wake can never dark a session. Death-first ordering in maybeRunSessionReadsAt
// is preserved (deaths are handled before any wake runs).

// levelWakeCandidate is one wakeable event, already filtered by the knobs.
type levelWakeCandidate struct {
	key     string // planID:version:kind:label:tier:birthBarTime — one wake per event
	desc    string // human line for the log + trigger context
	kind    string // zone | fvg | ob | ifvg | invalidation
	label   string
	tier    string // 15m | 1h | 4h
	birthMs int64
	prio    int // lower fires first
}

// wakePrio orders candidate classes (invalidation strongest, HTF before 15m).
const (
	wakePrioInvalidation = 0
	wakePrioHTFZone      = 1
	wakePrio15mZone      = 2
	wakePrio15mFVG       = 3
	wakePrioIFVG         = 4
	wakePrioHTFOB        = 5
)

// wakeTFs are the detection timeframes of the W6 wake loop.
var wakeTFs = []string{"15m", "1h", "4h"}

// wakeATR computes ATR for a TF slice with the detector fallback (0.002 × last
// close) so a cold cache never zeroes the tolerance.
func wakeATR(bars []market.Kline) float64 {
	atr := market.ExportCalculateATR(bars, 14)
	if atr <= 0 && len(bars) > 0 {
		atr = 0.002 * bars[len(bars)-1].Close
	}
	return atr
}

// lastClosedBar returns the most recent bar with CloseTime < nowMs (closed-only,
// mirroring kernel.closedBars) or false when none.
func lastClosedBar(bars []market.Kline, nowMs int64) (market.Kline, bool) {
	for i := len(bars) - 1; i >= 0; i-- {
		if bars[i].CloseTime < nowMs {
			return bars[i], true
		}
	}
	return market.Kline{}, false
}

// collectLevelWakeCandidates is the PURE W6 collector: detector output vs the
// plan row's birth time. `row` may be nil (no candidates). `cfg` may be nil
// (all ON defaults). It never mutates state — maybeWakePlannerOnLevelEvents
// applies the throttle, dedupe and budget.
func collectLevelWakeCandidates(cfg *store.DayPlanConfig, fetch func(tf string, count int) []market.Kline, symbol string, row *store.PlanDB, now time.Time) []levelWakeCandidate {
	if row == nil || fetch == nil {
		return nil
	}
	nowMs := now.UnixMilli()
	birthMs := row.CreatedAt.UnixMilli()
	tick := market.FuturesTickSize(symbol)
	if tick <= 0 {
		tick = 0.25
	}
	barsByTF := map[string][]market.Kline{}
	atrByTF := map[string]float64{}
	for _, tf := range wakeTFs {
		bars := fetch(tf, 500)
		barsByTF[tf] = bars
		atrByTF[tf] = wakeATR(bars)
	}

	var out []levelWakeCandidate
	add := func(c levelWakeCandidate) { out = append(out, c) }

	// W-KNOB-PRUNE (2026-09-18): ONE switch, wake_on_level_events (nil = ON),
	// gates every class below except HTF order blocks, which stay OFF unless a
	// legacy stored wake_on_htf_ob=true exists (WakeOnHTFOrderBlocks). At the
	// shipped default the candidate set is byte-identical to the five-switch
	// era (trader/testdata/knob_prune/wake_candidates.json, pinned pre-prune).
	levelEvents := cfg.WakeOnLevelEventsEnabled()

	// 15m tier: reversal S/D zones + FVG formations.
	if levelEvents {
		b15 := barsByTF["15m"]
		for _, z := range kernel.SupplyDemandZones(b15, atrByTF["15m"], now) {
			if z.ZonePattern != "reversal" || z.FormedAtMs <= birthMs {
				continue
			}
			add(levelWakeCandidate{
				key:  fmt.Sprintf("%s:%d:%s:%s:%s:%d", row.PlanID, row.Version, "zone", z.Label, "15m", z.FormedAtMs),
				desc: fmt.Sprintf("15m reversal S/D zone %s [%.2f–%.2f]", z.Label, z.Lo, z.Hi),
				kind: "zone", label: z.Label, tier: "15m", birthMs: z.FormedAtMs, prio: wakePrio15mZone,
			})
		}
		for _, f := range kernel.FairValueGaps(b15, wakeATR(b15), now) {
			if f.Kind != kernel.KindFVG || f.FormedAtMs <= birthMs {
				continue
			}
			add(levelWakeCandidate{
				key:  fmt.Sprintf("%s:%d:%s:%s:%s:%d", row.PlanID, row.Version, "fvg", f.Label, "15m", f.FormedAtMs),
				desc: fmt.Sprintf("15m FVG [%.2f–%.2f]", f.Lo, f.Hi),
				kind: "fvg", label: f.Label, tier: "15m", birthMs: f.FormedAtMs, prio: wakePrio15mFVG,
			})
		}
	}

	// HTF tier: 1h/4h S/D zones (any pattern).
	if levelEvents {
		for _, tf := range []string{"1h", "4h"} {
			for _, z := range kernel.SupplyDemandZones(barsByTF[tf], atrByTF[tf], now) {
				if z.FormedAtMs <= birthMs {
					continue
				}
				add(levelWakeCandidate{
					key:  fmt.Sprintf("%s:%d:%s:%s:%s:%d", row.PlanID, row.Version, "zone", z.Label, tf, z.FormedAtMs),
					desc: fmt.Sprintf("%s S/D zone %s [%.2f–%.2f]", tf, z.Label, z.Lo, z.Hi),
					kind: "zone", label: z.Label, tier: tf, birthMs: z.FormedAtMs, prio: wakePrioHTFZone,
				})
			}
		}
	}

	// HTF order blocks — OFF by default; only a legacy stored wake_on_htf_ob=true.
	if cfg.WakeOnHTFOrderBlocks() {
		for _, tf := range []string{"1h", "4h"} {
			for _, ob := range kernel.OrderBlocks(barsByTF[tf], atrByTF[tf], now) {
				if ob.FormedAtMs <= birthMs {
					continue
				}
				add(levelWakeCandidate{
					key:  fmt.Sprintf("%s:%d:%s:%s:%s:%d", row.PlanID, row.Version, "ob", ob.Label, tf, ob.FormedAtMs),
					desc: fmt.Sprintf("%s order block %s [%.2f–%.2f]", tf, ob.Label, ob.Lo, ob.Hi),
					kind: "ob", label: ob.Label, tier: tf, birthMs: ob.FormedAtMs, prio: wakePrioHTFOB,
				})
			}
		}
	}

	// iFVG: filled→inverted gaps on any wake tier.
	if levelEvents {
		for _, tf := range wakeTFs {
			for _, f := range kernel.FairValueGaps(barsByTF[tf], wakeATR(barsByTF[tf]), now) {
				if f.Kind != kernel.KindIFVG || f.FormedAtMs <= birthMs {
					continue
				}
				add(levelWakeCandidate{
					key:  fmt.Sprintf("%s:%d:%s:%s:%s:%d", row.PlanID, row.Version, "ifvg", f.Label, tf, f.FormedAtMs),
					desc: fmt.Sprintf("%s %s [%.2f–%.2f] (%s)", tf, f.Label, f.Lo, f.Hi, f.Info),
					kind: "ifvg", label: f.Label, tier: tf, birthMs: f.FormedAtMs, prio: wakePrioIFVG,
				})
			}
		}
	}

	// Seated-level invalidation: a zone-kind level
	// the plan SEATED is closed beyond by more than a noise band (max(2 ticks,
	// 0.15×ATR15)) — support (Demand/OB(bull)/iFVG(bull)) below, resistance
	// (Supply/OB(bear)/iFVG(bear)) above. Fresh only (violating close within
	// 2× the wake interval) so a stale invalidation never fires on restart.
	if levelEvents {
		var doc kernel.PlanDoc
		if json.Unmarshal([]byte(row.Doc), &doc) == nil {
			close, ok := lastClosedBar(barsByTF["15m"], nowMs)
			if ok {
				noise := 2 * tick
				if alt := 0.15 * atrByTF["15m"]; alt > noise {
					noise = alt
				}
				freshMs := int64(cfg.WakeMinIntervalMinutes()) * 2 * int64(time.Minute/time.Millisecond)
				for _, l := range doc.Levels {
					dir := seatedLevelSide(l.Label)
					if dir == "" || l.Price <= 0 {
						continue
					}
					violated := (dir == "below" && close.Close < l.Price-noise) ||
						(dir == "above" && close.Close > l.Price+noise)
					if !violated || close.CloseTime < nowMs-freshMs {
						continue
					}
					add(levelWakeCandidate{
						key:  fmt.Sprintf("%s:%d:%s:%s:%s:%d", row.PlanID, row.Version, "invalidation", l.Label, "15m", close.CloseTime),
						desc: fmt.Sprintf("seated %s invalidated: close %.2f %s %.2f (noise %.2f)", l.Label, close.Close, dir, l.Price, noise),
						kind: "invalidation", label: l.Label, tier: "15m", birthMs: close.CloseTime, prio: wakePrioInvalidation,
					})
				}
			}
		}
	}

	// Priority first, newest within a class first.
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].prio != out[b].prio {
			return out[a].prio < out[b].prio
		}
		return out[a].birthMs > out[b].birthMs
	})
	return out
}

// seatedLevelSide classifies a plan level by its provenance label into the
// invalidation direction ("" = not a zone-kind level, never wakes).
func seatedLevelSide(label string) string {
	l := strings.TrimSpace(label)
	switch {
	case strings.HasPrefix(l, "Demand"), strings.HasPrefix(l, "OB(bull"), strings.HasPrefix(l, "iFVG(bull"):
		return "below" // support: invalidated by a close below
	case strings.HasPrefix(l, "Supply"), strings.HasPrefix(l, "OB(bear"), strings.HasPrefix(l, "iFVG(bear"):
		return "above" // resistance: invalidated by a close above
	}
	return ""
}

// maybeWakePlannerOnLevelEvents is the W6 wake entry: fires AT MOST one planner
// wake per cycle on the newest level event, throttled by wake_min_interval_min
// (shared with the MSS wake via lastPlannerWakeAt) and deduped per
// (plan,version,kind,label,tier,birth). W6-D: wakes are UNLIMITED and spend NO
// budget — the per-session re-plan cap belongs to deaths alone. Death-first
// ordering is preserved by the caller.
// maybeWakePlannerOnLevelEvents is the production entry point: it reads the
// wall clock and delegates.
//
// CLOCK SEAM (2026-09-03). The clock moved from incidental to LOAD-BEARING when
// the class-47 cutoffs began enforcing: the last-window cutoff asks how many
// minutes remain to the session flat, so this path's behaviour now depends on
// the time of day it runs. A test calling it with a fixed fixture and a real
// clock passes in the morning and fails after ~14:20 CT — which is exactly what
// TestMaybeWakePlannerOnLevelEventsThrottleDedupe started doing, on dev, the
// day the cutoffs shipped. The seam lets a test state its own clock instead of
// borrowing the machine's.
func (at *AutoTrader) maybeWakePlannerOnLevelEvents(session, tradeDate string, row *store.PlanDB) {
	at.maybeWakePlannerOnLevelEventsAt(time.Now(), session, tradeDate, row)
}

func (at *AutoTrader) maybeWakePlannerOnLevelEventsAt(now time.Time, session, tradeDate string, row *store.PlanDB) {
	if market.FuturesBarsProvider == nil || at.store == nil || row == nil {
		return
	}
	// W-FLIP-OWNS-THE-BREACH R1/R3 (2026-09-17): while the active plan's flip
	// line is breached, or the flip evaluation was skipped for stale bars,
	// an ordinary wake must not author — the flip evaluator owns the plan.
	if at.wakeDeferredByFlip(now, session, row) {
		return
	}
	var cfg *store.DayPlanConfig
	if at.config.StrategyConfig != nil {
		cfg = at.config.StrategyConfig.DayPlan
	}
	symbol := at.futuresSymbol()
	fetch := func(tf string, count int) []market.Kline {
		return market.FuturesBarsProvider(symbol, tf, count)
	}
	cands := collectLevelWakeCandidates(cfg, fetch, symbol, row, now)
	if len(cands) == 0 {
		return
	}
	ev := cands[0]
	// One wake per (plan,version,event) — the same event must not re-fire on
	// the next cycle after the wake.
	if at.lastLevelWakeKey == ev.key {
		return
	}
	// Min-interval throttle shared with the MSS wake: ANY planner wake resets
	// the clock, so a death/MSS/level cascade can't triple-spend one window.
	if !at.lastPlannerWakeAt.IsZero() && now.Sub(at.lastPlannerWakeAt) < time.Duration(cfg.WakeMinIntervalMinutes())*time.Minute {
		at.logWarnf("🗓️ level wake %s on %s %s — SKIPPED: %.0fm elapsed < wake_min_interval_min (%dm).",
			ev.desc, session, tradeDate, now.Sub(at.lastPlannerWakeAt).Minutes(), cfg.WakeMinIntervalMinutes())
		return
	}
	// ── CLASS 47 — CADENCE CUTOFFS, ENFORCING (owner ruling 2026-09-03) ──────
	// Both cutoffs shipped WARN-first and recorded what a suppression WOULD
	// have skipped. 2026-09-03 supplied the two cases that decided it: a wake
	// with 24 min to flat wrote LONDON v2 at 08:15:44 for an 08:30 flat, and a
	// wake 21 min after the previous wake-authored version wrote another. Both
	// now RETURN.
	//
	// Wakes only: this whole path is reached from the level-event wake, and
	// WakeCadenceGoverns pins that the trigger is one. Scheduled reads, death
	// re-plans and owner resets never come through here.
	dec := WakeCadenceDecision{
		Session: session, Desc: ev.desc,
		CutoffMin: wakeCutoffMinutes(), CooldownMin: wakeCooldownMinutes(),
		FastMarketThreshold: fastMarketATR(),
	}
	// FAST-MARKET EXEMPTION (owner ruling 2026-09-03) — measured HERE, before
	// the cadence verdict, from the same function the read itself uses. On
	// 2026-09-03 at 10:14 a wake 26 min into a 30-min cooldown carried a drift
	// of 133.8 pts = 2.8×ATR5m; the enforcing cooldown would have refused the
	// re-plan while the executor kept trading a plan 133 points away.
	if _, driftATR := at.fastMarketDrift(at.wakeTimePrice()); driftATR > 0 {
		dec.FastMarketATR = driftATR
	}
	if sess, okS := at.sessionRegistry(now).ActiveSession(now); okS {
		dec.MinutesToFlat, dec.HaveFlat = minutesToSessionFlat(now, sess)
	}
	// Measured from the last WAKE-AUTHORED version, not the last wake ATTEMPT:
	// a wake whose read failed or kept the active plan wrote nothing, so it must
	// not start this clock (that is what makes it distinct from
	// wake_min_interval_min above).
	if dec.CooldownMin > 0 && at.store != nil {
		if last, lerr := at.store.Plan().GetLatestPlanForTraderSession(tradeDate, session, at.id); lerr == nil && last != nil &&
			WakeCadenceGoverns(last.TriggerReason) && !last.CreatedAt.IsZero() {
			dec.SinceLastWakeVersionMin = int(now.Sub(last.CreatedAt).Minutes())
			dec.HaveLastWakeVersion = true
		}
	}
	if dec.SkipForCutoff() {
		n := 0
		if at.store != nil {
			if c, cerr := store.IncWakeCounter(at.store, at.id, tradeDate, session, store.WakeWouldSkipCutoffKind); cerr == nil {
				n = c
			} else {
				at.logWarnf("⏱ wake cutoff counter write failed: %v", cerr)
			}
		}
		at.logWarnf("%s", wakeCutoffLine(session, ev.desc, dec.MinutesToFlat, dec.CutoffMin, n))
		return
	}
	if note := dec.BypassNote(); note != "" && dec.CooldownMin > 0 &&
		dec.HaveLastWakeVersion && dec.SinceLastWakeVersionMin < dec.CooldownMin {
		// Loud, and only when the bypass actually saved this wake from the
		// cooldown — never as a decoration on a wake the cooldown would have
		// let through anyway.
		at.logWarnf("⏱ wake %s (cooldown %d min since the last wake-authored version, cooldown %dm) — %s on %s. Proceeding (class 47, owner ruling 2026-09-03)",
			note, dec.SinceLastWakeVersionMin, dec.CooldownMin, ev.desc, session)
	}
	if dec.SkipForCooldown() {
		n := 0
		if at.store != nil {
			if c, cerr := store.IncWakeCounter(at.store, at.id, tradeDate, session, store.WakeWouldSkipCooldownKind); cerr == nil {
				n = c
			} else {
				at.logWarnf("⏱ wake cooldown counter write failed: %v", cerr)
			}
		}
		at.logWarnf("%s", wakeCooldownLine(session, ev.desc, dec.SinceLastWakeVersionMin, dec.CooldownMin, n))
		return
	}
	// F3 — a WAKE defers while ANY planner stream is open, in any trading
	// session, for any trader in this process. SCHEDULED reads never defer: the
	// naive rule would have parked today's NY session read behind LONDON's.
	// The dedupe key is deliberately NOT consumed here, so the same event can
	// wake again on the next cycle once the stream closes.
	if held, open := anyPlannerStreamOpen(); open {
		if at.store != nil {
			if _, cerr := store.IncWakeCounter(at.store, at.id, tradeDate, session, store.WakeStreamDeferKind); cerr != nil {
				at.logWarnf("⏱ wake defer counter write failed: %v", cerr)
			}
		}
		at.logWarnf("%s", wakeStreamDeferLine(session, ev.desc, held))
		return
	}
	// W6-D (2026-08-25) — wakes are UNLIMITED and count against NOTHING: a wake
	// re-plan must never consume the death budget, and can never be the cause of
	// a replans_exhausted NO-TRADE (live bug 19:35 CT: 4 wake re-plans ate the
	// whole budget and a later death darked the session). Only the dedupe key +
	// the min-interval throttle limit wake frequency; deaths keep their own cap.
	at.lastLevelWakeKey = ev.key
	at.lastPlannerWakeAt = now
	at.logWarnf("🗓️ level wake %s on %s %s — waking the planner (W6, 5th wake-up).", ev.desc, session, tradeDate)
	at.warnIfReplanOrphansOverlays(row)
	// W6-C (2026-08-25) — the wake re-read is NON-fatal (a failed read keeps the
	// active plan; failClosed=false) and ASYNC so a slow/timing-out planner can
	// never stall the decision loop (live bug: a 2×300s retry chain blocked 14
	// minutes of cycles and then no-traded a healthy session).
	go func() {
		_ = at.runPlannerReadWithTriggerClaimedCtx(session, tradeDate, "level_event", "level event: "+ev.desc, priorPlanLevelLines(row), false)
		if fresh, fErr := at.store.Plan().GetLatestPlanForTraderSession(tradeDate, session, at.id); fErr == nil && fresh != nil && fresh.Version != row.Version {
			at.carryOwnerEditsInto(fresh.PlanID, row.Version, fresh.Version)
		}
	}()
}

// wakeTimePrice is the price the fast-market check reads at WAKE-decision time:
// the latest closed 1m bar.
//
// The read itself uses the price that AssembleScoredLevelsFullMinGrade returns,
// but that assembly is the expensive part of a read and cannot run inside a
// wake decision. The latest 1m close is what that assembly is anchored on, so
// the two agree to within a bar.
//
// With no bars provider it returns 0, fastMarketDrift then returns no drift,
// and the cooldown applies as before — the fail-safe direction is "no
// exemption", never "exempt everything".
func (at *AutoTrader) wakeTimePrice() float64 {
	if market.FuturesBarsProvider == nil {
		return 0
	}
	bars := market.FuturesBarsProvider(at.futuresSymbol(), "1m", 2)
	if len(bars) == 0 {
		return 0
	}
	return bars[len(bars)-1].Close
}
