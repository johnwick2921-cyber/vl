package trader

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"nofx/kernel"
	"nofx/market"
	"nofx/store"
)

// ── W-FLIP-OWNS-THE-BREACH (2026-09-17) ─────────────────────────────────────
//
// Owner, 23:2x CT: "why at the flip point it re-reads and the bias is still
// the same — address it carefully and make it run." The kernel side is
// kernel/flip_breach.go (R1–R3); this file is the trader wiring:
//
//   - planChainFacts loads the chain ONCE for both resolvers (the CLASS 139
//     hold anchor and the R2 condition-window anchor);
//   - flipConditionAnchor + noteFlipWindow give describeActivePlanDeath the
//     flip window and log, once per version, whether the window is the chain's
//     or the version's because the line moved;
//   - wakeDeferredByFlip is the R1/R3 gate both ordinary wake paths
//     (maybeWakePlannerOnLevelEventsAt, maybeWakePlannerOnMSSAt) call first.
//     Scheduled reads, death re-plans and owner reads never come through it.

// planChainFacts reads the chain's version facts and lifecycle log for a row.
// ok=false when the store cannot serve them (the resolvers then fall back to
// the row's own birth, tagged as such).
func (at *AutoTrader) planChainFacts(row *store.PlanDB) (versions []kernel.PlanVersionFact, transitions []kernel.PlanTransitionFact, fallbackMs int64, ok bool) {
	if row != nil && !row.CreatedAt.IsZero() {
		fallbackMs = row.CreatedAt.UnixMilli()
	}
	if at.store == nil || row == nil || row.PlanID == "" {
		return nil, nil, fallbackMs, false
	}
	facts, err := at.store.Plan().ListVersionFacts(row.PlanID)
	if err != nil || len(facts) == 0 {
		return nil, nil, fallbackMs, false
	}
	versions = make([]kernel.PlanVersionFact, 0, len(facts))
	for _, f := range facts {
		ms := int64(0)
		if !f.CreatedAt.IsZero() {
			ms = f.CreatedAt.UnixMilli()
		}
		versions = append(versions, kernel.PlanVersionFact{Version: f.Version, TriggerReason: f.TriggerReason, BiasDirection: f.BiasDirection, CreatedAtMs: ms, FlipPrice: f.FlipPrice, FlipSide: f.FlipSide})
	}
	if events, lErr := at.store.Plan().LifecycleLogForPlan(row.PlanID); lErr == nil {
		for _, e := range events {
			ms := int64(0)
			if !e.At.IsZero() {
				ms = e.At.UnixMilli()
			}
			transitions = append(transitions, kernel.PlanTransitionFact{Version: e.Version, Event: e.Event, Reason: e.Reason, AtMs: ms})
		}
	}
	return versions, transitions, fallbackMs, true
}

// flipConditionAnchor resolves the flip CONDITION window for this row (R2):
// the chain anchor when same-bias wake re-reads kept the line within
// kernel.FlipLineClusterTolerance, the row's own birth when the line moved,
// is the first of its run, or the chain cannot be read.
func (at *AutoTrader) flipConditionAnchor(row *store.PlanDB) kernel.FlipConditionAnchor {
	versions, _, fallback, ok := at.planChainFacts(row)
	if !ok {
		v := 0
		if row != nil {
			v = row.Version
		}
		return kernel.FlipConditionAnchor{SinceMs: fallback, Source: kernel.FlipWindowFallback, AnchorVersion: v}
	}
	return kernel.ResolveFlipConditionAnchor(versions, row.Version, kernel.FlipLineClusterTolerance(), fallback)
}

// Once-per-(trader,plan,version) log keys: the flip-window note and the
// wake-deferral line. sync.Map because describeActivePlanDeath is reached
// from both the planner loop and the executor's dead-plan check.
var (
	flipWindowNoted   sync.Map // key → struct{}
	flipWakeDeferNote sync.Map // key → reason ("breach" | "stale")
)

func flipOnceKey(at *AutoTrader, row *store.PlanDB) string {
	return fmt.Sprintf("%s|%s|%d", at.id, row.PlanID, row.Version)
}

// noteFlipWindow logs, once per version, where the flip window opens: on the
// chain (a same-bias re-read kept the line) or on this version because the
// line MOVED. A version anchored on its own birth for the ordinary reasons
// (first line of its run, a re-plan, no chain) says nothing — that is the
// pre-wave behaviour and needs no line.
func (at *AutoTrader) noteFlipWindow(row *store.PlanDB, doc *kernel.PlanDoc, cw kernel.FlipConditionAnchor) {
	if row == nil || doc == nil || doc.FlipStructured == nil || doc.FlipStructured.Price <= 0 {
		return
	}
	if cw.Source != kernel.FlipWindowChain && cw.Source != kernel.FlipWindowMoved {
		return
	}
	key := flipOnceKey(at, row)
	if _, seen := flipWindowNoted.LoadOrStore(key, struct{}{}); seen {
		return
	}
	line := doc.FlipStructured
	if cw.Moved {
		at.logInfof("🗓️ flip line MOVED on %s v%d: %.2f → %.2f (Δ%.2f > %.2f pt tolerance) — the flip window opens at this version's birth, not the chain's",
			row.PlanID, row.Version, cw.PrevPrice, line.Price, absF(line.Price-cw.PrevPrice), kernel.FlipLineClusterTolerance())
		return
	}
	at.logInfof("🗓️ flip window: %s v%d keeps the chain's flip line (%s %.2f, within %.2f pt) — closes counted from v%d's birth (%s)",
		row.PlanID, row.Version, line.Side, line.Price, kernel.FlipLineClusterTolerance(), cw.AnchorVersion, time.UnixMilli(cw.SinceMs).In(kernel.CTLocation()).Format("15:04:05 CT"))
}

func absF(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// wakeDeferredByFlip is the R1/R3 gate for ORDINARY wakes (level_event,
// structure_mss). It answers true — and the caller returns without
// authoring — when the ACTIVE plan's flip line is breached and has not fired
// (the flip evaluator owns the plan), or when the flip evaluation is skipped
// for stale bars (nobody may author on the tape the evaluator refused). A
// dormant / no_trade row, a plan with no structured flip line, or a breach
// the evaluator cannot fire on (untouched line) defers nothing. The deferral
// line is logged once per version and reason; the resume line once when the
// breach clears.
func (at *AutoTrader) wakeDeferredByFlip(now time.Time, session string, row *store.PlanDB) bool {
	if row == nil || row.Lifecycle != "active" || market.FuturesBarsProvider == nil {
		return false
	}
	var doc kernel.PlanDoc
	if json.Unmarshal([]byte(row.Doc), &doc) != nil || doc.FlipStructured == nil || doc.FlipStructured.Price <= 0 {
		return false
	}
	bars := market.FuturesBarsProvider(at.futuresSymbol(), kernel.AISVPBarInterval, kernel.AISVPBarCount)
	if len(bars) == 0 {
		return false
	}
	cw := at.flipConditionAnchor(row)
	b := kernel.FlipBreachState(*doc.FlipStructured, bars, cw.SinceMs, now.UnixMilli())
	key := flipOnceKey(at, row)
	switch {
	case b.Stale:
		if prev, _ := flipWakeDeferNote.Load(key); prev != "stale" {
			flipWakeDeferNote.Store(key, "stale")
			at.logWarnf("🗓️ wake deferred: flip evaluation skipped (%s, age %ds) on %s v%d — a wake must not author on the tape the flip evaluator refused",
				b.StaleWhy, b.AgeMs/1000, session, row.Version)
		}
		return true
	case b.Breached:
		if prev, _ := flipWakeDeferNote.Load(key); prev != "breach" {
			flipWakeDeferNote.Store(key, "breach")
			at.logWarnf("🗓️ wake deferred: flip line breached (%s %.2f, closes %d/%d) — the flip evaluator owns this plan until it fires or price closes back",
				b.Side, b.Price, b.Closes, b.Need)
		}
		return true
	}
	if prev, had := flipWakeDeferNote.LoadAndDelete(key); had && prev == "breach" {
		at.logInfof("🗓️ wakes resume on %s v%d: flip line %s %.2f no longer breached (price closed back inside)", session, row.Version, b.Side, b.Price)
	}
	return false
}
