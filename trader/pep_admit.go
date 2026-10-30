package trader

import (
	"fmt"
	"time"

	"vl/internal/datatruth"
	"vl/kernel"
	ntTrader "vl/trader/ninjatrader"
)

// ── PEP ADMISSION (entry_admission.go) ──────────────────────────────────────
//
// admitPep is a path of the ONE admission chain, placed DELIBERATELY in every
// branch of it (nothing falls through by accident):
//
//	step                                   pep
//	mentor mode gate                       n/a (mentor and pep are exclusive)
//	pep mode gate (pepAdmitRefusal)        FIRST among the pep steps: only a PepSourced
//	                                       decision on a pep-mode trader passes
//	Picture run / day-plan master          n/a (Picture only)
//	feed down                              KEPT
//	dead-man watchdog                      KEPT
//	frozen                                 KEPT
//	boot integrity                         KEPT
//	data integrity + data truth            KEPT, on pep's OWN window and reader
//	                                       (pepIntegrityOpenMinAt, ReaderPep)
//	stop_until owner pause                 KEPT
//	maintenance hold / install pending     KEPT
//	contract roll                          KEPT
//	daily guardrails                       KEPT (no waiver)
//	session risk (breaker)                 KEPT (the day-plan session LIST is inert:
//	                                       pep and the day plan are exclusive)
//	last entry                             REPLACED by pep's own cutoff (the flat time)
//	CME closed                             KEPT
//	plan-mode / approval                   approval KEPT; plan-mode n/a (decision, agent)
//	re-entry cooldown                      n/a (arm, picture)
//	EntryGate (R:R, min-SL x ATR5m)        NOT called: pep's SL and TP are the box, not
//	                                       an authored bracket. Its job is done by
//	                                       pepWireCheck on the wire-rounded prices; the
//	                                       held-position check is pepSendEntry's own
//	                                       ntHeldPosition read (oneContractGuard is the
//	                                       ARMED path's and is not called for pep; it
//	                                       looks at open positions only, not at working
//	                                       entry orders, and neither does pep's read)
//	stale feed (armFeedStale)              KEPT as the last step
//	SIM-only                               at the wire (isAccountTradeable), untouched

// pepAdmitRefusal is the pep half of the mode switch, symmetric to
// mentorAdmitRefusal. With pep ON it refuses every entry that is not a pep
// decision on every producer path (decision, agent chat, arm, picture). With pep
// OFF it refuses a pep decision (a PepSourced decision on a trader that is not
// in pep mode must never reach the wire). Returns (class, text); "" = allowed.
// With pep OFF and a non-pep entry it returns "" - byte-identical.
func (at *AutoTrader) pepAdmitRefusal(in admitIntent) (class, text string) {
	isPep := in.Path == admitPep || (in.Decision != nil && in.Decision.PepSourced)
	if at.pepEnabled() {
		if in.Path == admitPep && in.Decision != nil && in.Decision.PepSourced {
			return "", ""
		}
		return "pep_ai_entries_off", "pep_mode: AI entries are OFF - every entry comes from the pep evaluator"
	}
	if isPep {
		return "pep_not_enabled", "pep_mode: a pep entry on a trader that is not in pep mode is refused"
	}
	return "", ""
}

// pepPastEntryCutoff is pep's own last-entry rule: no new entry at or after the
// flat time (the day plan's session last-entry offset and the mentor's 15:45
// halt cutoff are not pep's).
func (at *AutoTrader) pepPastEntryCutoff(now time.Time) (string, bool) {
	cfg := at.pepConfigAt(now)
	loc := kernel.CTLocation()
	ct := now.In(loc)
	flatAt := time.Date(ct.Year(), ct.Month(), ct.Day(), cfg.FlatMin/60, cfg.FlatMin%60, 0, 0, loc)
	if !now.Before(flatAt) {
		return fmt.Sprintf("at/after the %s CT flat time - pep opens nothing it would have to close at once", pepClock(cfg.FlatMin)), true
	}
	return "", false
}

// pepIntegritySlackMin is the margin before the box (and the EMA warm-up) that
// pep's data window keeps.
const pepIntegritySlackMin = 6

// pepIntegrityMinOpenMin is the smallest pep data window (a pass before the box
// still reads a sane stretch).
const pepIntegrityMinOpenMin = 30

// pepIntegrityOpenMinAt is pep's OWN data window in OPEN 1m minutes before now:
// from (box start - style E's EMA warm-up - a small margin) to now. It is what
// pep reads - the box and the EMA's 99.8% warm-up - and NOT the 12000-minute AI
// window: an old hole pep never reads must not pause it for days. Style A has
// no warm-up; style E's is WarmupCandles x the candle timeframe (54 minutes at
// the shipped 9 x 2m). Rounded up to 30 so the tracker's per-window memo holds
// a handful of keys, not one per minute.
func (at *AutoTrader) pepIntegrityOpenMinAt(now time.Time) int {
	cfg := at.pepConfig()
	loc := kernel.CTLocation()
	ct := now.In(loc)
	boxStart := time.Date(ct.Year(), ct.Month(), ct.Day(), cfg.BoxStartMin/60, cfg.BoxStartMin%60, 0, 0, loc)
	warm := 0
	if cfg.Style == "E" {
		warm = cfg.WarmupCandles() * cfg.E.EMATFMin
	}
	from := boxStart.Add(-time.Duration(warm+pepIntegritySlackMin) * time.Minute)
	n := int(now.Sub(from) / time.Minute)
	if n < pepIntegrityMinOpenMin {
		n = pepIntegrityMinOpenMin
	}
	if n > ntTrader.IntegrityIntradayOpenMin {
		n = ntTrader.IntegrityIntradayOpenMin
	}
	return (n + 29) / 30 * 30
}

// pepDataVerdictAt is pep's entry data verdict: the m445 hole legs on pep's own
// window, then the m446/m447 data-truth leg held to the pep READER.
func (at *AutoTrader) pepDataVerdictAt(now time.Time, withStale bool) dataVerdict {
	w := at.pepIntegrityOpenMinAt(now)
	if v := at.integrityVerdictAt(w, now, withStale); v.Blocked {
		return v
	}
	return at.dataTruthVerdictForReader(datatruth.ReaderPep, w, now)
}
