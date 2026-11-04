package mentor

// m466Rel61PreValues puts EVERY release #61 (m466) knob at its pre-#61 value:
// P1 boxes/breaks/ranges (m466P1PreValues), P2 levels/touches
// (m466P2PreValues), P3 news/open/ISB ceiling (m466PreValues) and P4 swing
// inside bar (m61PreP4). The packages were built in parallel on #60
// (45053f78a); each package's "pre-values reproduce the #60 golden" pin can
// only hold at the merged head when all four are chained, because the other
// packages move the same m434 tapes. P1's G37 has no knob — callers also set
// the test hook midRangeLegacyRole — and neither has the review-round-2
// box-return conflict consume (boxConflictLegacySkip).
func m466Rel61PreValues(cfg *Config) {
	m466P1PreValues(cfg)
	m466P2PreValues(cfg)
	m466PreValues(cfg)
	m61PreP4(cfg)
}
