package api

import (
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// handleBarTruthArbiter POST /api/nt/bar-arbiter — BAR-TRUTH WAVE (2026-08-28).
// Gated on the TRADER's owner (traderOwnedBy: the JWT user must own the
// trader named), not on the box owner (requireOwner) — owner-identity review
// DS-103 P3-1 corrected the "Owner-only" this comment used to claim. Machine
// tokens are refused here (not a read, not a risk-reducing action —
// machine_action_guard.go). Two actions:
//
//	backfill: one-shot deep bars_subscribe (default MNQ 1m, 8640 bars) so the
//	          AddOn replays deep history into the cache + persister.
//	diff:     the three-way arbiter — NT8 truth (captured replay) vs kernel
//	          cache vs persisted bars — counts + FNV hashes + an independent
//	          Wilder ATR14(5m) recompute (R2: reimplemented here, never calls
//	          the engine) per source + per-bar OHLC deltas (cache vs DB).
func (s *Server) handleBarTruthArbiter(c *gin.Context) {
	var body struct {
		TraderID  string `json:"trader_id"`
		Action    string `json:"action"` // backfill | diff
		Symbol    string `json:"symbol"`
		Timeframe string `json:"timeframe"`
		BarsBack  int    `json:"bars_back"`
	}
	_ = c.ShouldBindJSON(&body)
	traderID := strings.TrimSpace(c.Query("trader_id"))
	if traderID == "" {
		traderID = strings.TrimSpace(body.TraderID)
	}
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}
	if !s.traderOwnedBy(c.GetString("user_id"), traderID) {
		SafeUnauthorized(c)
		return
	}
	at, err := s.traderManager.GetTrader(traderID)
	if err != nil || at == nil {
		SafeNotFound(c, "Trader")
		return
	}
	nt, ok := at.GetUnderlyingTrader().(*ntTrader.TCPTrader)
	if !ok || nt == nil {
		SafeBadRequest(c, "trader is not the NinjaTrader TCP bridge")
		return
	}
	sym := strings.TrimSpace(body.Symbol)
	if sym == "" {
		sym = "MNQ"
	}
	tf := strings.TrimSpace(body.Timeframe)
	if tf == "" {
		tf = "1m"
	}

	switch strings.ToLower(strings.TrimSpace(body.Action)) {
	case "backfill":
		back := body.BarsBack
		if back <= 0 {
			back = 8640 // 6 days of 1m — covers the 08-24+ retention window
		}
		// CLASS 52 (owner ruling 2026-09-02): MERGE, never wipe. The old code
		// cleared the replay window BEFORE knowing what the provider would
		// return — on 2026-09-02 a 1m ask for 1,000,000 bars came back capped
		// at ~2,000 and the wipe deleted 3 weeks of accumulated 1m that the
		// replay could not replace. The persister's InsertBars is INSERT OR
		// REPLACE: the replay replaces EXACTLY the bars it returns, and rows it
		// cannot replace are never deleted.
		c.JSON(http.StatusAccepted, gin.H{
			"ok": true, "action": "backfill", "symbol": sym,
			"timeframe": tf, "bars_back": back, "cleared_rows": 0,
			"note": "merge (INSERT OR REPLACE within the replay's returned range): rows the replay cannot replace are never deleted — deep bars_subscribe sent; run action=diff after ~30s.",
		})
		if err := nt.RequestDeepBarsBackfill(sym, tf, back); err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		return
	case "diff":
		replay := nt.BarTruthEndCapture()
		cacheBars := nt.BarTruthCache(sym, tf)
		cacheCount, cacheHash := ntwire.FNVBarSet(cacheBars)

		var dbCount int
		var dbHash uint64
		dbBars := dbRows5(s.store, sym, tf)
		if dbBars != nil {
			dbCount, dbHash = ntwire.FNVBarSet(dbBars)
		}

		// ATR is window-sensitive — compare ONLY the common T window so a
		// trimmed cache (2500) can't disagree with an 8-day DB on window
		// alone (the 2dp E-proof must compare like with like).
		cacheCommon, dbCommon := intersectByT(cacheBars, dbBars)

		type dbDiff struct {
			T  int64   `json:"t"`
			O  float64 `json:"o"` // cache values
			H  float64 `json:"h"`
			L  float64 `json:"l"`
			C  float64 `json:"c"`
			DO float64 `json:"d_o"` // cache - db deltas
			DH float64 `json:"d_h"`
			DL float64 `json:"d_l"`
			DC float64 `json:"d_c"`
		}
		var deltas []dbDiff
		mismatches := 0
		for i := range cacheCommon {
			cb, db := cacheCommon[i], dbCommon[i]
			if db.O != cb.O || db.H != cb.H || db.L != cb.L || db.C != cb.C {
				mismatches++
				if len(deltas) < 5 {
					deltas = append(deltas, dbDiff{
						T: cb.T, O: cb.O, H: cb.H, L: cb.L, C: cb.C,
						DO: round2a(cb.O - db.O), DH: round2a(cb.H - db.H),
						DL: round2a(cb.L - db.L), DC: round2a(cb.C - db.C),
					})
				}
			}
		}
		oldest, cur, hist, persist := nt.BarTruthDrops()
		c.JSON(http.StatusOK, gin.H{
			"ok": true, "action": "diff", "symbol": sym, "timeframe": tf,
			"replay": replay,
			"cache": gin.H{"count": cacheCount, "hash": cacheHash,
				"atr5m": round2a(independentATR14(aggregate5m(cacheCommon)))},
			"db": gin.H{"count": dbCount, "hash": dbHash,
				"atr5m": round2a(independentATR14(aggregate5m(dbCommon)))},
			"common_window": gin.H{"count": len(cacheCommon), "mismatches": mismatches},
			"deltas":        deltas,
			"drops": gin.H{"ingest_oldest": oldest, "ingest_current": cur,
				"ingest_historical": hist, "persist_queue": persist},
		})
		return
	default:
		SafeBadRequest(c, "action must be backfill|diff")
	}
}

// ── R2 independent recompute (reimplemented from the formula; never calls the
// engine's ATR/aggregation) ────────────────────────────────────────────────

// aggregate5m buckets 1m bars into 5m bars (floor-aligned, OHLCV).
func aggregate5m(bars []ntwire.Bar) []ntwire.Bar {
	out := []ntwire.Bar{}
	for _, b := range bars {
		bucket := b.T / (5 * 60000) * (5 * 60000)
		if n := len(out); n > 0 && out[n-1].T == bucket {
			out[n-1].H = math.Max(out[n-1].H, b.H)
			out[n-1].L = math.Min(out[n-1].L, b.L)
			out[n-1].C = b.C
			out[n-1].V += b.V
		} else {
			out = append(out, ntwire.Bar{T: bucket, O: b.O, H: b.H, L: b.L, C: b.C, V: b.V})
		}
	}
	return out
}

// independentATR14 is Wilder ATR(14) reimplemented from the formula (R2).
func independentATR14(bars []ntwire.Bar) float64 {
	if len(bars) <= 14 {
		return 0
	}
	trs := make([]float64, len(bars))
	for i := 1; i < len(bars); i++ {
		pc := bars[i-1].C
		trs[i] = math.Max(bars[i].H-bars[i].L, math.Max(math.Abs(bars[i].H-pc), math.Abs(bars[i].L-pc)))
	}
	sum := 0.0
	for i := 1; i <= 14; i++ {
		sum += trs[i]
	}
	atr := sum / 14
	for i := 15; i < len(bars); i++ {
		atr = (atr*13 + trs[i]) / 14
	}
	return atr
}

func dbRows5(st *store.Store, sym, tf string) []ntwire.Bar {
	if st == nil || st.BarHistory() == nil {
		return nil
	}
	// W-BARS-CONTRACT-KEY — ONE contract: on the contract key a roll minute
	// holds two rows, and this series is keyed by T (intersectByT). The
	// store's newest usable contract is the same shadow the chart uses; no
	// contract → no series rather than a mixed one (A24).
	contract, ok := st.BarHistory().LatestContract(sym)
	if !ok {
		return nil
	}
	rows, err := st.BarHistory().BarsBetweenOn(sym, tf, contract, 0, time.Now().UnixMilli())
	if err != nil {
		return nil
	}
	out := make([]ntwire.Bar, 0, len(rows))
	for _, r := range rows {
		out = append(out, ntwire.Bar{T: r.OpenTimeMs, O: r.O, H: r.H, L: r.L, C: r.C, V: r.V})
	}
	return out
}

// intersectByT returns both series restricted to their common timestamps
// (ascending), so the ATR comparison is window-fair.
func intersectByT(a, b []ntwire.Bar) ([]ntwire.Bar, []ntwire.Bar) {
	m := map[int64]ntwire.Bar{}
	for _, x := range a {
		m[x.T] = x
	}
	var ac, bc []ntwire.Bar
	for _, y := range b {
		if x, ok := m[y.T]; ok {
			ac = append(ac, x)
			bc = append(bc, y)
		}
	}
	return ac, bc
}

func round2a(v float64) float64 { return math.Round(v*100) / 100 }
