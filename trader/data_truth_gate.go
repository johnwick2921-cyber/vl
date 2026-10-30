package trader

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vl/internal/datatruth"
	"vl/internal/datatruth/checks"
	"vl/kernel"
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

// dataTruthBootGrace is how long the trader waits for the data-truth boot to
// FINISH - ntTrader.WireDataTruthChecks (when the wire server is up) AND the boot
// seed (the AddOn's replay waited for, the ring rehydrated: DataTruthSeeded) -
// before asking Evaluate. During the grace the verdict is BLOCKED with a
// "starting" reason and nothing is evaluated, pushed or counted as an episode.
// 10 minutes covers the worst honest boot: up to 90 s for the wire server, then
// up to 20 x 15 s of replay retries, then the rehydrate. A boot that has NOT
// seeded by then is judged anyway - and loudly: the checks name what is missing
// (a replay that never lands is a pause the owner is told about, never a silent
// wait, and never an entry on an unjudged ring).
const dataTruthBootGrace = 10 * time.Minute

var (
	dataTruthWired   atomic.Bool  // wireDataTruthBoot ran: the gate applies to NT8 traders
	dataTruthWiredAt atomic.Int64 // unix ms
	dataTruthOnce    sync.Once
)

// dataTruthReadyFn is the judge-ready edge (ntTrader.DataTruthJudgeable): the
// checks are registered AND the boot seed has landed (#55 m449 - registration
// alone is a few seconds before the replay, and an empty ring that has not
// arrived yet is not lost data). A var so the pins can drive both sides of it.
var dataTruthReadyFn = ntTrader.DataTruthJudgeable

// dataTruthStarting is the quiet boot wait: not judge-ready yet, and still inside
// the grace. Entries are BLOCKED while it holds; nothing is evaluated.
func dataTruthStarting(now time.Time) bool {
	if dataTruthReadyFn() {
		return false
	}
	return now.Sub(time.UnixMilli(dataTruthWiredAt.Load())) < dataTruthBootGrace
}

// DataTruthStarting is dataTruthStarting for the health endpoint: true while the
// gate is wired and still in its quiet boot wait, i.e. while EVERY NT8 trader's new
// entries are refused with "the data-truth checks are still starting". The banner
// reads it so it never says "not paused" for the up-to-10 minutes after a deploy
// in which nothing can enter (round 3 re-review P3: that under-claim was disclosed
// but not fixed).
func DataTruthStarting(now time.Time) bool {
	return dataTruthWired.Load() && dataTruthStarting(now)
}

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
	if dataTruthStarting(now) {
		return dataVerdict{Blocked: true, Class: integrityClassDataTruth,
			Reason: "the data-truth checks are still starting (waiting for the NT8 history replay to land in the bar cache) - new entries wait for them (fail-closed)"}
	}
	// past the grace the checks speak: Evaluate names every required check that
	// never landed and judges whatever the ring holds (a replay that never came
	// shows as the lost data it is)
	return dataTruthReasons(at.futuresSymbol(), openMin, now, at.dataTruthAsker())
}

// dataTruthAsker is who is asking: the trader id and the NT8 account it is bound
// to. The account-scoped checks (broker_agreement, fill_price) block only the
// traders they are FOR (m451).
func (at *AutoTrader) dataTruthAsker() truthAsker {
	a := truthAsker{id: at.id, at: at}
	if nt8 := at.armedTrader(); nt8 != nil {
		a.account = strings.TrimSpace(nt8.BoundAccount())
	}
	return a
}

// truthAsker identifies the trader a data-truth verdict is for. The zero value
// asks as nobody in particular: every scoped block applies to it (the widest
// reading, used by the beat that acts on nothing).
type truthAsker struct {
	id, account string
	at          *AutoTrader
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
// window, a reason that names its readers only for those readers (a bad bar that
// moves the AI path's 3m/5m indicators does not pause a mentor trader, whose
// indicators read the 1m ring, and the other way round), and a reason scoped to
// other traders/accounts (m451) only for the traders it is FOR.
func dataTruthReasons(sym string, openMin int, now time.Time, asker truthAsker) dataVerdict {
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
		// A block that is FOR other traders/accounts (broker_agreement, fill_price)
		// is no reason to stop THIS one (m451). The zero asker (the beat) sees all.
		if asker.id != "" && !r.AppliesTo(asker.id, asker.account) {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s", r.Check, r.Detail))
	}
	// The scoped broker check speaks for the traders registered with it. An asking
	// trader that is NOT registered has a broker view nobody compares, and the
	// other traders' silence is not evidence about it: while the CME is open it
	// cannot be admitted (fail closed). Only a trader the asker identifies; the
	// zero asker (the beat) acts on nothing.
	if asker.at != nil && datatruth.Wired(CheckBrokerAgreement) && kernel.IsCMEOpen(now) && !asker.at.brokerAgreementRegistered() {
		parts = append(parts, fmt.Sprintf("%s: this trader is not registered with the broker check (not started, or already stopped) - its account view is not being compared with NT8's", CheckBrokerAgreement))
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
	if !at.dataTruthApplies() || dataTruthStarting(now) {
		return
	}
	dataTruthReasons(at.futuresSymbol(), ntTrader.IntegritySwingOpenMin, now, truthAsker{})
}
