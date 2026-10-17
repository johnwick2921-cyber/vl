package trader

import (
	"fmt"
	"sync"
	"time"

	"nofx/kernel"
	"nofx/logger"
	"nofx/market"
	ntwire "nofx/provider/ninjatrader"
	"nofx/store"
)

// PICTURE-HTF LIVE WIRING (2026-09-20) — routes NATIVE LIVE NT8 bar events to
// each trader's two-picture evaluator. Historical replays never arrive here
// (the provider fans out live frames only), so a replay receipt can never
// mint a real opportunity.

// pictureHtfTraders is the process-wide registry of AutoTraders that own an
// evaluator (keyed by trader id — a restarted trader replaces its entry).
var pictureHtfTraders sync.Map // id → *AutoTrader

func init() {
	ntwire.SetLiveBarSink(pictureHtfLiveBars)
}

// pictureHtfLiveBars is the process-wide sink: converts wire bars to klines
// and fans out to every registered trader. Registered traders whose mode is
// off drop the frame in the evaluator (cheap no-op).
func pictureHtfLiveBars(symbol, tf, contract string, bars []ntwire.Bar, receivedAt time.Time) {
	if len(bars) == 0 {
		return
	}
	dur, ok := kernel.TFDurationMs(tf)
	kl := make([]market.Kline, 0, len(bars))
	for _, b := range bars {
		// W4: Final, EmittedAt and Contract used to be dropped here, so the
		// evaluator could not tell a closed candle from a forming one, could
		// not age the frame against the SOURCE clock, and could not tell
		// which instrument it was reading. They are the evidence; they travel.
		k := market.Kline{
			OpenTime: b.T, Open: b.O, High: b.H, Low: b.L, Close: b.C,
			Final: b.Final, EmittedAt: b.EmittedAt, Contract: contract,
		}
		if ok {
			k.CloseTime = b.T + dur - 1
		}
		kl = append(kl, k)
	}
	pictureHtfTraders.Range(func(_, v any) bool {
		if at, ok := v.(*AutoTrader); ok {
			at.NotifyLiveBars(symbol, tf, kl, receivedAt)
			// W3 D14 — the live-bar armed pass (market_in_zone): a non-blocking
			// kick on a final 1m bar or a zone-verdict change. It reads the
			// WIRE bars because the kline copy above drops Final.
			at.noteLiveBarsForArmedPass(symbol, tf, bars)
		}
		return true
	})
}

// pictureHtfContractOf reports the front month this trader is trading and
// where that came from. It is a seam so a test can state the trader's
// contract without standing up an AddOn ACK.
var pictureHtfContractOf = func(at *AutoTrader, symbol string) (string, string) {
	return at.currentContract(symbol)
}

// registerPictureHtf installs the trader in the live-bar registry and opens a
// new GENERATION. Every evaluation records the generation it began under and
// re-checks it before the wire, so a frame in flight across a Stop/restart
// cannot send on behalf of a trader that no longer exists (W4/D25).
func (at *AutoTrader) registerPictureHtf() {
	if at == nil || at.id == "" {
		return
	}
	at.pictureGen.Add(1)
	pictureHtfTraders.Store(at.id, at)
}

// unregisterPictureHtf removes the trader from the live-bar registry on Stop
// and closes its generation.
//
// CompareAndDelete, never Delete: a RESTARTED trader may already have
// re-registered under the same id, and a late Stop from the OLD instance must
// not evict the new one. The registry is also W3's armed-kick registry
// (pictureHtfLiveBars Ranges it to call noteLiveBarsForArmedPass), so evicting
// the wrong entry would silently stop the armed event pass for a live trader.
func (at *AutoTrader) unregisterPictureHtf() {
	if at == nil || at.id == "" {
		return
	}
	at.pictureGen.Add(1)
	pictureHtfTraders.CompareAndDelete(at.id, at)
}

// pictureTraderGenerationOf reads a trader's current Picture generation. A seam
// so a test can move the generation between an evaluation's start and its send.
var pictureTraderGenerationOf = func(at *AutoTrader) int64 {
	if at == nil {
		return 0
	}
	return at.pictureGen.Load()
}

// pictureHtfResolvedConfig is the trader's Picture knobs with defaults
// applied — the one resolution the evaluator and the plan card both read.
func (at *AutoTrader) pictureHtfResolvedConfig() store.PictureHtfConfig {
	if sc := at.GetStrategyConfig(); sc != nil && sc.DayPlan != nil && sc.DayPlan.PictureHtf != nil {
		return store.PictureHtfResolved(sc.DayPlan.PictureHtf)
	}
	return store.PictureHtfResolved(nil)
}

// pictureHtfEvaluator lazily builds (or rebuilds, when the strategy knobs
// change) the trader's two-picture evaluator. Returns nil when the mode is
// absent/disabled or the trader is not on the NT8 path.
func (at *AutoTrader) pictureHtfEvaluator() *PictureHtfEvaluator {
	if at == nil || at.exchange != "ninjatrader" {
		return nil
	}
	at.pictureHtfMu.Lock()
	defer at.pictureHtfMu.Unlock()
	cfg := at.pictureHtfResolvedConfig()
	sig := fmt.Sprintf("%t|%.5f|%d|%d|%d|%d|%.4f",
		cfg.Enabled, cfg.TickSize, cfg.PivotWindow, cfg.SwingLookback, cfg.EntryWindowSec, cfg.FreshnessSec, cfg.MinRR)
	if at.pictureHtf != nil && at.pictureHtfSig == sig {
		// The broker consumer self-heals here: if its listener channel ever
		// closed (underlying subscription died), the registry entry was
		// deleted and this per-cycle call re-listens.
		at.ensurePictureHtfBrokerConsumer()
		return at.pictureHtf
	}
	at.pictureHtf = NewPictureHtfEvaluator(at, cfg)
	at.pictureHtfSig = sig
	at.ensurePictureHtfBrokerConsumer()
	return at.pictureHtf
}

// NotifyLiveBars routes a native LIVE bar event into the evaluator.
func (at *AutoTrader) NotifyLiveBars(symbol, tf string, bars []market.Kline, receivedAt time.Time) {
	if ev := at.pictureHtfEvaluator(); ev != nil {
		ev.OnBars(symbol, tf, bars, receivedAt)
	}
}

// pictureHtfTickFallback is the wall-clock fallback the run loop calls once
// per cycle: it covers a missed boundary frame (feed stall) with the same
// evaluation, and runs the reconciliation sweep so pending rows recover
// across disconnects/restarts WITHOUT another entry (the sweep only ever
// observes the broker book). The evaluator's freshness gate still applies.
func (at *AutoTrader) pictureHtfTickFallback(now time.Time) {
	if ev := at.pictureHtfEvaluator(); ev != nil {
		// W4/D24: the fallback covers a MISSED boundary frame. Where no
		// completed frame has ever arrived there is nothing to be late about,
		// and the evaluation would run against zero stamps. The reconciliation
		// sweep still runs either way — pending rows must recover across a
		// disconnect whether or not the tape has spoken since.
		if ev.HasCompletedFrame() {
			ev.Evaluate(at.futuresSymbol(), now)
		} else {
			ev.noteTickFallbackSkip()
		}
		pictureHtfReconcilePending(at)
	}
}

// pictureHtfBootLine is the mode's boot line: mode, rule version, SIM status,
// native-data readiness, and the AddOn capability verdict — READ from the
// live far side, never assumed.
func (at *AutoTrader) pictureHtfBootLine() string { return at.pictureHtfBootLineAt(time.Now()) }

// pictureHtfBootLineAt is the 📷 boot line on an injected clock.
func (at *AutoTrader) pictureHtfBootLineAt(now time.Time) string {
	ev := at.pictureHtfEvaluator()
	mode := "off"
	if ev != nil && ev.Enabled() {
		mode = "on"
	}
	sim := "SIM-only"
	native := "native NT8 bars (final+emitted_at)"
	cap := "not proven"
	if pictureHtfCapabilityProven(at) {
		cap = "proven"
	}
	// W-EXEC-TRUTH W0 (CTO Q6): the plan-mode verdict, READ — under strict
	// Picture is refused until W5 makes it a Day Plan scenario, and the line
	// says so in those words.
	planGate := "admitted (plan mode is not strict)"
	if r := at.pictureStrictVisible(now); r != "" {
		planGate = r
	}
	// W4/D21: contract identity, READ at print time. "n/a" when this trader
	// has no `subscribed` ACK yet — never a literal, never a guess.
	contract := "n/a"
	if c, _ := pictureHtfContractOf(at, at.futuresSymbol()); c != "" {
		contract = c
	}
	// W4/D24: the frame-age and fallback counters are READ here too, so a feed
	// that is quietly being refused (or quietly unaged) is visible on the line
	// rather than only in a log nobody greps.
	return fmt.Sprintf("picture-htf: mode=%s rule=v1 %s data=%s addon=%s (build=%q, need ≥ %s) plan_gate=%s contract=%s · foreign=%d · unknown=%d · stale=%d · unaged=%d · tick_skips=%d",
		mode, sim, native, cap, at.farSideBuildID(), ntwire.MinAddonBuildPictureHtf, planGate,
		contract, ev.ForeignContractFrames(), ev.UnknownContractFrames(),
		ntwire.StaleLiveFrames(), ntwire.UnagedLiveFrames(), ev.TickFallbackSkips())
}

// logPictureHtfBootLine prints the boot line at trader start.
func (at *AutoTrader) logPictureHtfBootLine() {
	logger.Info("📷 " + at.pictureHtfBootLine())
}
