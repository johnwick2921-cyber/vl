package store

// ── PEP KNOBS (owner order 2026-10-29) ───────────────────────────────────────
//
// One registry entry per leaf of the root pep block, keyed by the EXACT schema
// path ("pep.<leaf>", "pep.style_a.<leaf>", "pep.style_e.<leaf>") and, because
// the registry still falls back on the bare leaf name, every leaf name is
// unique (pep_*, pep_a_*, pep_e_*) — TestPepLeafNamesAreUnique pins that no
// other schema leaf shares one.
//
// Status is live: the reader is kernel/pep's FromSettings (it reads ResolvePep,
// the ONE resolver) feeding Evaluate. The trader seam that calls them is the
// pep trader lane (docs/superpowers/plans PEP-PLAN-1029 §4); until it lands the
// master switch pep_enabled is read by StrategyConfig.PepEnabled and the mode
// exclusivity guard, and a saved block changes no order.
func init() {
	const (
		resolver = "store/pep.go:ResolvePep"
		bridge   = "kernel/pep/config_store.go:FromSettings → kernel/pep/eval.go:Evaluate"
		eff      = "trader/effective_settings.go (pep rows → store.ResolvePep)"
	)
	live := func(path, note string, extra ...string) {
		cons := append([]string{resolver, bridge, eff}, extra...)
		knobRegistry[path] = KnobEntry{Path: path, Status: KnobLive, Consumers: cons, Note: note}
	}
	live("pep.pep_enabled", "Master switch; nil/false = OFF (shipped). Read by StrategyConfig.PepEnabled (mode exclusivity guard store/pep.go:guardStrategyModes; api/strategy.go).",
		"store/pep.go:StrategyConfig.PepEnabled", "store/pep.go:TradingModeConflict")
	live("pep.pep_style", "A (ORB breakout, enter immediately) or E (EMA9 ORB, candle-close); shipped E (owner 2026-10-30); case-folded where it enters.")
	live("pep.pep_contracts", "Shipped 6 (owner 2026-10-30); HARD ceiling 10 (clamped and logged, never honoured above).")
	live("pep.pep_box_start_ct", "HH:MM America/Chicago; shipped 08:30.")
	live("pep.pep_flat_time_ct", "HH:MM America/Chicago; shipped 15:00 (the session flat is 14:45 and must not be used).")
	live("pep.pep_tick_size", "Shipped 0.25 (MNQ).")
	live("pep.pep_sl_rounding", "outward (shipped; the PDF rounds the E stop away from entry) | nearest.")
	live("pep.pep_max_trades_per_day", "Shipped 1; style A takes the first break only whatever this says.")
	live("pep.pep_fee_per_contract_usd", "Shipped $1 round trip per contract. BACKTEST/REPORTING only: no order or gate reads it.")
	live("pep.pep_max_box_points", "0/absent = OFF (shipped, matches the PDF); >0 refuses a wider box.")
	live("pep.pep_signal_max_age_min", "Shipped 2: a break/cross older than this, or a newest 1m bar older than this, is refused as stale.")
	live("pep.style_a.pep_a_box_minutes", "Shipped 5 (the PDF's 5-minute ORB).")
	live("pep.style_a.pep_a_sl_fraction", "Shipped 1.0 x box.")
	live("pep.style_a.pep_a_tp_fraction", "Shipped 1.0 x box.")
	live("pep.style_a.pep_a_both_edges", "One 1m bar through both edges: no_trade (shipped, fail closed) | nearest_open (a bar-level guess).")
	live("pep.style_e.pep_e_box_minutes", "Shipped 2: the PDF's style-E ledger equals the first TWO minutes' range (cmd/pep-backtest 2026-10-30).")
	live("pep.style_e.pep_e_sl_fraction", "Shipped 0.5 x box, rounded per pep_sl_rounding.")
	live("pep.style_e.pep_e_tp_fraction", "Shipped 1.0 x box.")
	live("pep.style_e.pep_e_ema_period", "Shipped 9.")
	live("pep.style_e.pep_e_ema_tf_min", "Shipped 2; must divide an hour.")
	live("pep.style_e.pep_e_signal_source", "ema (owner ruling, shipped: the EMA crosses the edge) | close (the candle close crosses the edge).")
	live("pep.style_e.pep_e_warmup_bars", "0/absent = 3 x EMA period complete candles before the EMA may signal.")
}
