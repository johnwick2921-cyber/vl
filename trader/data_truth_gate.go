package trader

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vl/internal/datatruth"
	"vl/internal/datatruth/checks"
	"vl/logger"
	ntTrader "vl/trader/ninjatrader"
)

// ── DATA-TRUTH GATE, TRADER SIDE (release #55 integrator: m446 + m447) ──────
//
// Owner, 2026-10-29: "this is a financial product, not a game - nothing related
// to data loss or incorrect data can continue to trade."
//
// m445 answers "are bars MISSING?"; m446/m447 answer "are the bars (and the
// account view, and the fills) CORRECT?" through internal/datatruth: a set of
// registered checks, one episode per (check, symbol), fail closed. This file is
// the join: dataTruthVerdictAt turns datatruth.Evaluate into a dataVerdict and
// dataIntegrityVerdictAt (data_integrity_gate.go) folds it into THE one entry
// verdict, so EVERY path in the table at the top of that file (AI decision,
// agent chat, planner arms at all four wire sites, Picture, mentor arms and
// authoring, the mentor armed pass, the SIM test seams, the debug test trade)
// consults it with no per-path change.
//
// Checks wired (names are the datatruth check names):
//
//	bad_print, cross_frame, indicator_lookback, indicator_lookback_swing4h,
//	indicator_lookback_levels, indicator_lookback_session, ingest_loss,
//	contract_roll          m446  ntTrader.WireDataTruthChecks (ring + store)
//	broker_agreement       m447  RegisterBrokerAgreement (NT8 account vs the bot)
//	fill_price             m447  RegisterFillPrice (fill vs the market it printed in)
//
// SCOPE. Every check blocks every entry path for its symbol EXCEPT
// indicator_lookback_swing4h, which judges the 4h swing line only SWING4H entries
// read: it blocks the window that contains the swing line (>= the swing window:
// the SWING4H mentor arm / authoring) and nothing else. Everything else reads a
// price level, an ATR/EMA or the account itself, which any entry may rest on.
//
// NEVER consulted by exits, stops, BE/trail moves, closes, protection, flatten or
// reconciliation (the gate sits in the ENTRY chain only), and it does NOT withdraw
// resting entry orders (m446: "pause NEW entries only"; m445's withdraw is for
// missing bars). One WARN and one owner push per (check, symbol) episode come from
// datatruth itself (UseOwnerPush); resume is automatic and only on a MEASURED
// clear. A friend instance runs the same gate (the push is a no-op without a
// bound chat).

// dataTruthBootGrace is how long the trader waits for ntTrader.WireDataTruthChecks
// (it runs when the wire server is up) before asking Evaluate - and so before
// the required-but-unwired stubs may alert. During the grace the verdict is
// BLOCKED with a "starting" reason and nothing is evaluated, pushed or counted
// as an episode.
const dataTruthBootGrace = 5 * time.Minute

var (
	dataTruthWired   atomic.Bool  // wireDataTruthBoot ran: the gate applies to NT8 traders
	dataTruthWiredAt atomic.Int64 // unix ms
	dataTruthOnce    sync.Once
)

// dataTruthReadyFn is the registration edge (ntTrader.DataTruthReady); a var so
// the pins can drive both sides of it.
var dataTruthReadyFn = ntTrader.DataTruthReady

// dataTruthRequired lists every check this process must have, by datatruth name.
// A name nobody registers is a stub that BLOCKS.
func dataTruthRequired() []string {
	return []string{
		checks.CheckBadPrint, checks.CheckCrossFrame, checks.CheckLookback,
		checks.CheckLookbackSwing4h, checks.CheckLookbackLevels, checks.CheckLookbackSession,
		checks.CheckIngestLoss, checks.CheckContractRoll,
		CheckBrokerAgreement, checks.CheckFillPrice,
	}
}

// wireDataTruthBoot is the trader-side half of the boot wiring, called once when
// the first NinjaTrader trader is built (next to ntTrader.WireBarPersistence):
// declare the required set (stubs block until the real checks land), route the
// once-per-episode alert to the owner, register the two account-side m447 checks.
// Idempotent.
func wireDataTruthBoot() { dataTruthOnce.Do(doWireDataTruthBoot) }

// doWireDataTruthBoot is the body (a seam so the wiring pin can run it again over
// a fresh registry).
func doWireDataTruthBoot() {
	datatruth.Require(dataTruthRequired()...)
	datatruth.UseOwnerPush()
	RegisterBrokerTruthChecks()
	dataTruthWiredAt.Store(time.Now().UnixMilli())
	dataTruthWired.Store(true)
	logger.Infof("🧪 data-truth gate ON: %d checks required (fail closed until each is wired) · NEW entries on every path pause while any cannot be trusted · one WARN + one owner push per episode · resume only when measured clear · exits, stops and protection never consult it",
		len(dataTruthRequired()))
}

// dataTruthApplies — NinjaTrader futures traders, once the boot wiring ran.
func (at *AutoTrader) dataTruthApplies() bool {
	return at != nil && at.exchange == "ninjatrader" && dataTruthWired.Load()
}

// dataTruthVerdictAt asks datatruth.Evaluate for this trader's symbol and joins
// the blocking checks into one verdict. openMin is the window the admitted path
// reads (the swing4h scope above).
func (at *AutoTrader) dataTruthVerdictAt(openMin int, now time.Time) dataVerdict {
	if !at.dataTruthApplies() {
		return dataVerdict{}
	}
	if !dataTruthReadyFn() {
		if wired := time.UnixMilli(dataTruthWiredAt.Load()); now.Sub(wired) < dataTruthBootGrace {
			return dataVerdict{Blocked: true, Class: integrityClassDataTruth,
				Reason: "the data-truth checks are still starting (waiting for the NT8 bar feed) - new entries wait for them (fail-closed)"}
		}
		// past the grace the stubs speak: Evaluate names every required check that never landed
	}
	return dataTruthReasons(at.futuresSymbol(), openMin, now)
}

// dataTruthReaderFor names the reader behind an admitted window (release #55
// review: a bad bar matters to a trader only through the indicators and frames
// THAT trader's decisions read). The mentor evaluator asks for the intraday or
// swing window; every other entry path (AI decision, agent, Picture, planner
// arm, the SIM seams, the debug door) asks for the deep one and is the AI
// reader - the wide one, so an unknown window is held to the wider reader
// (fail closed).
func dataTruthReaderFor(openMin int) string {
	switch openMin {
	case ntTrader.IntegrityIntradayOpenMin, ntTrader.IntegritySwingOpenMin:
		return datatruth.ReaderMentor
	}
	return datatruth.ReaderAI
}

// dataTruthReasons evaluates and scopes. Evaluate runs EVERY check (episodes and
// alerts are per check, whatever the asking path); the scope filter only decides
// which verdicts this path must obey: the swing4h check only for the swing
// window, and a reason that names its readers only for those readers (a bad
// bar that moves the AI path's 3m/5m indicators does not pause a mentor trader,
// whose indicators read the 1m ring, and the other way round).
func dataTruthReasons(sym string, openMin int, now time.Time) dataVerdict {
	_, reasons := datatruth.Evaluate(sym, now)
	reader := dataTruthReaderFor(openMin)
	var parts []string
	for _, r := range reasons {
		if r.Check == checks.CheckLookbackSwing4h && openMin < ntTrader.IntegritySwingOpenMin {
			continue // the swing line is read by SWING4H only
		}
		if !r.Concerns(reader) {
			continue // the data is wrong, but not in anything THIS reader reads
		}
		parts = append(parts, fmt.Sprintf("%s: %s", r.Check, r.Detail))
	}
	if len(parts) == 0 {
		return dataVerdict{}
	}
	return dataVerdict{Blocked: true, Class: integrityClassDataTruth,
		Reason: fmt.Sprintf("%s [%s data not trusted - entries resume when the checks measure clear; exits and protection are unaffected]", strings.Join(parts, "; "), sym)}
}

// integrityClassDataTruth is the verdict class of the m446/m447 checks.
const integrityClassDataTruth = "data_truth"

// dataTruthBeat keeps the datatruth episodes (and /api/health data_truth) current
// on the 60 s monitor beat even when no entry is attempted, so a bad print is
// alerted when it happens rather than when the next entry asks. It evaluates the
// widest window (the swing check included) and acts on nothing.
func (at *AutoTrader) dataTruthBeat(now time.Time) {
	if !at.dataTruthApplies() || !dataTruthReadyFn() {
		return
	}
	dataTruthReasons(at.futuresSymbol(), ntTrader.IntegritySwingOpenMin, now)
}
