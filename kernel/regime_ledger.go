package kernel

import "vl/logger"

// LogRegimeBootLedger (Cutover 2, regime wave 2026-08-21) prints one line per
// regime knob at boot — VALUE + SOURCE — so the boot block self-documents what
// the wave is enforcing. The Studio toggles (htf_veto / transition_standdown)
// are per-strategy and read at cycle time; their SHIPPED defaults are declared
// here. Env knobs are resolved live.
func LogRegimeBootLedger() {
	logger.Infof("🛡️ regime ledger: htf_veto=%s (Studio regime.htf_veto, default ON) · htf_veto_tf=%s (env HTF_VETO_TF)", "ON", HTFVetoTF())
	logger.Infof("🛡️ regime ledger: transition_standdown=%s (Studio regime.transition_standdown, default ON) · cap=%dmin (env TRANSITION_MAX_MIN)", "ON", TransitionMaxMin())
	logger.Infof("🛡️ regime ledger: flip hysteresis hold=%dmin (env FLIP_MIN_HOLD_MIN)", FlipMinHoldMin())
	logger.Infof("🛡️ regime ledger: structure engine TFs=%v (5m/15m/1h, swing k=%d, min-swing %.2f×ATR, MSS body %.1f×ATR)", StructureTFs, structureSwingK(), structureMinSwingATR(), structureMSSBodyATR())
	logger.Infof("🛡️ regime ledger: htf veto inputs — 1h from the 1m cache + %s from the NATIVE %s bar series (cross/4h modes); a RANGING TF closing ≥ %.2f×ATR beyond its last swing extreme reads one-way (env STRUCTURE_ONEWAY_ATR, default %.1f) · mode=%s", HTFVetoNativeTF, HTFVetoNativeTF, structureOneWayATR(), DefaultStructureOneWayATR, HTFVetoMode())
	logger.Infof("🛡️ regime ledger: flip-eval freshness cap=%dms (env FLIP_EVAL_MAX_STALE_S, default %ds)", FlipEvalMaxStaleMs(), DefaultFlipEvalMaxStaleSeconds)
}
