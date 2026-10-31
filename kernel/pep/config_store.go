package pep

import "vl/store"

// FromSettings turns the strategy store's resolved pep block (store.ResolvePep,
// the ONE resolver: shipped defaults filled, vocabulary canonicalised, clamps
// applied and logged) into the evaluator's Config. It is the only bridge between
// the saved settings and Evaluate; nothing else in the repo builds a pep Config
// from stored values. The A / E lock (PDF rules, stored numbers ignored) is
// applied by store.ResolvePep, so this bridge is faithful to it by construction.
func FromSettings(r store.PepResolved) Config {
	return Config{
		Style:            Style(r.Style),
		Contracts:        r.Contracts,
		BoxStartMin:      r.BoxStartMin,
		FlatMin:          r.FlatMin,
		TickSize:         r.TickSize,
		SLRounding:       SLRounding(r.SLRounding),
		MaxTradesPerDay:  r.MaxTradesPerDay,
		MaxBoxPoints:     r.MaxBoxPoints,
		SignalMaxAgeMin:  r.SignalMaxAgeMin,
		FeePerContractRT: r.FeePerContractRT,
		A: ParamsA{
			BoxMinutes: r.A.BoxMinutes,
			SLFraction: r.A.SLFraction,
			TPFraction: r.A.TPFraction,
			BothEdges:  BothEdges(r.A.BothEdges),
		},
		E: ParamsE{
			BoxMinutes: r.E.BoxMinutes,
			SLFraction: r.E.SLFraction,
			TPFraction: r.E.TPFraction,
			EMAPeriod:  r.E.EMAPeriod,
			EMATFMin:   r.E.EMATFMin,
			Source:     SignalSource(r.E.SignalSource),
			WarmupBars: r.E.WarmupBars,
		},
		Custom: ParamsCustom{
			Entry:      EntryType(r.Custom.EntryType),
			BoxMinutes: r.Custom.BoxMinutes,
			SLFraction: r.Custom.SLFraction,
			TPFraction: r.Custom.TPFraction,
			BothEdges:  BothEdges(r.Custom.BothEdges),
			EMAPeriod:  r.Custom.EMAPeriod,
			EMATFMin:   r.Custom.EMATFMin,
			Source:     SignalSource(r.Custom.SignalSource),
			WarmupBars: r.Custom.WarmupBars,
		},
	}
}

// ConfigOf resolves a strategy's pep block and bridges it: the production path
// from a stored StrategyConfig to Evaluate's Config.
func ConfigOf(c *store.StrategyConfig) Config { return FromSettings(store.ResolvePep(c)) }
