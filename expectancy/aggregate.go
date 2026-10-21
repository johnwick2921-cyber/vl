package expectancy

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"vl/kernel"
	"vl/logger"
	"vl/market"
	"vl/store"

	"gorm.io/gorm"
)

// posRow is one loaded position joined to its plan doc.
type posRow struct {
	ID              int64
	EntryTime       int64
	ExitTime        int64
	Side            string
	Quantity        float64
	EntryQuantity   float64 // the WHOLE position (MATH-EXECUTOR P2-1)
	Origin          string  // R22 R1: "manual" rows are not the bot's
	EntryPrice      float64
	Symbol          string
	PnlCorrected    *float64
	Source          string
	PlanID          string
	PlanSession     string
	PlanVersion     int
	CitedScenarioID string
	Doc             string
}

// armRow is the arm a position filled from, when there was one.
type armRow struct {
	PlanID   string
	Version  int
	Scenario string
	EntryPx  float64
	StopPx   float64
	TargetPx float64
}

type excRow struct {
	PositionID    int64
	MaePts        *float64
	MfePts        *float64
	ExitReason    string
	StopPxInitial float64
	EntryPx       float64
	Size          float64
}

type abRow struct {
	PlanID    string
	Version   int
	Session   string
	Scenario  string
	Rule      string
	Condition string
	Outcome   string
	EntryPx   float64
	NetPnl    float64
}

// rec is one row that survived every exclusion: the atom every figure in the
// table is computed from. Holding the id alongside the value is what makes the
// sample-id law mechanical instead of aspirational.
type rec struct {
	key Key
	id  int64
	// pnl is nil for a row whose pnl_corrected is NULL: it is counted against
	// its cell's ExcludedUnresolved and contributes to NO statistic.
	pnl        *float64
	realizedR  *float64
	plannedRR  *float64
	mae, mfe   *float64
	stopHit    *bool
	targetHit  *bool
	hasExc     bool
	exitTimeMs int64
}

// LoadAndBuildAt is the entry point. THE ONLY CLOCK IS THE CALLER'S (class 60,
// A28): now is passed in, never read here, so a test states its own clock and
// the boot line, the endpoint and the panel all agree on one instant.
func LoadAndBuildAt(gdb *gorm.DB, now time.Time) (Table, error) {
	if gdb == nil {
		return Table{BuiltAtMs: now.UnixMilli()}, fmt.Errorf("expectancy: nil db")
	}
	positions, err := loadPositions(gdb)
	if err != nil {
		return Table{BuiltAtMs: now.UnixMilli()}, err
	}
	arms := loadArms(gdb)
	excs := loadExcursions(gdb)
	abs := loadAB(gdb)
	return BuildAt(now, positions, arms, excs, abs), nil
}

// loadPositions reads CLOSED positions from the day-plan era forward, joined to
// the plan doc that gives them a condition.
//
// The era bound is applied HERE, in the query: pre-day-plan-era (crypto) rows
// are never loaded, so they are ABSENT from the model rather than excluded by
// it. That is the difference A22 draws — an excluded row is one we looked at and
// refused; an absent row was never in scope.
func loadPositions(gdb *gorm.DB) ([]posRow, error) {
	var rows []posRow
	err := gdb.Raw(`
SELECT p.id AS id, p.entry_time AS entry_time, p.exit_time AS exit_time, p.side AS side,
       p.quantity AS quantity, COALESCE(p.entry_quantity,0) AS entry_quantity, COALESCE(p.origin,'') AS origin,
       p.entry_price AS entry_price, p.symbol AS symbol,
       p.pnl_corrected AS pnl_corrected, COALESCE(p.source,'') AS source,
       COALESCE(p.plan_id,'') AS plan_id, COALESCE(p.plan_session,'') AS plan_session,
       p.plan_version AS plan_version, COALESCE(p.cited_scenario_id,'') AS cited_scenario_id,
       COALESCE(pl.doc,'') AS doc
FROM trader_positions p
LEFT JOIN plans pl ON pl.plan_id = p.plan_id AND pl.version = p.plan_version
WHERE p.status = 'CLOSED' AND p.entry_time >= ?
ORDER BY p.id`, store.DayPlanEraStart.UnixMilli()).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("expectancy: load positions: %w", err)
	}
	return rows, nil
}

// loadArms maps (plan, version, scenario) → the filled arm. A missing table is
// not an error: this is a read model (class 23 / A10) and a missing optional
// input degrades the table, it does not fail the loop.
func loadArms(gdb *gorm.DB) map[string]armRow {
	out := map[string]armRow{}
	if !gdb.Migrator().HasTable("armed_orders") {
		return out
	}
	var rows []armRow
	if err := gdb.Raw(`SELECT plan_id, version, scenario, entry_px, stop_px, target_px
FROM armed_orders WHERE state = 'filled'`).Scan(&rows).Error; err != nil {
		logger.Warnf("📊 expectancy: armed_orders read failed (arms unknown): %v", err)
		return out
	}
	for _, r := range rows {
		out[armKey(r.PlanID, r.Version, r.Scenario)] = r
	}
	return out
}

func armKey(planID string, version int, scenario string) string {
	return fmt.Sprintf("%s|%d|%s", planID, version, scenario)
}

func loadExcursions(gdb *gorm.DB) map[int64]excRow {
	out := map[int64]excRow{}
	if !gdb.Migrator().HasTable("trade_excursions") {
		return out
	}
	var rows []excRow
	if err := gdb.Raw(`SELECT position_id, mae_pts, mfe_pts, COALESCE(exit_reason,'') AS exit_reason,
stop_px_initial, entry_px, size FROM trade_excursions`).Scan(&rows).Error; err != nil {
		logger.Warnf("📊 expectancy: trade_excursions read failed (MAE/MFE absent): %v", err)
		return out
	}
	for _, r := range rows {
		out[r.PositionID] = r
	}
	return out
}

func loadAB(gdb *gorm.DB) []abRow {
	if !gdb.Migrator().HasTable("ab_confirm_log") {
		return nil
	}
	var rows []abRow
	if err := gdb.Raw(`SELECT COALESCE(plan_id,'') AS plan_id, version, COALESCE(session,'') AS session,
COALESCE(scenario,'') AS scenario, COALESCE(rule,'') AS rule, COALESCE(condition,'') AS condition,
COALESCE(outcome,'') AS outcome, COALESCE(entry_px,0) AS entry_px, net_pnl FROM ab_confirm_log`).Scan(&rows).Error; err != nil {
		logger.Warnf("📊 expectancy: ab_confirm_log read failed (E8 side-table absent): %v", err)
		return nil
	}
	return rows
}

// BuildAt is the pure aggregation. Given the same inputs and the same clock it
// returns the same table — which is what makes any figure in it reproducible.
func BuildAt(now time.Time, positions []posRow, arms map[string]armRow, excs map[int64]excRow, abs []abRow) Table {
	tbl := Table{BuiltAtMs: now.UnixMilli()}
	recs := make([]rec, 0, len(positions))

	for _, p := range positions {
		// EXCLUSION ORDER is deliberate: a row is attributed to exactly one
		// reason, the most specific first, so the ledger sums to the rows seen.
		if p.Source == TestSeamSource {
			tbl.Excluded.TestSeam++
			continue
		}
		if store.IsManualOrigin(p.Origin) {
			// R22 R1: an NT8 position the bot adopted with no order/arm of
			// its own — not the bot's trade, counted under its own reason.
			tbl.Excluded.Manual++
			continue
		}
		if p.PlanID == store.PlanUnresolvable {
			tbl.Excluded.Unresolvable++
			continue
		}
		cond, levelKind := conditionAndLevelKind(p)
		if cond == "" {
			tbl.Excluded.NoCondition++
			continue
		}
		arm, armed := arms[armKey(p.PlanID, p.PlanVersion, p.CitedScenarioID)]
		path := PathDecision
		if armed {
			path = PathArmed
		}
		r := rec{
			key: Key{
				Condition: cond,
				Session:   p.PlanSession,
				LevelKind: levelKind,
				Path:      path,
				Era:       eraOf(p.EntryTime),
			},
			id:         p.ID,
			exitTimeMs: p.ExitTime,
		}
		if p.PnlCorrected == nil {
			// UNRESOLVED (A22): counted, never coerced, never in a statistic.
			tbl.Excluded.UnresolvedPnL++
			recs = append(recs, r)
			continue
		}
		v := *p.PnlCorrected
		r.pnl = &v

		// Planned R:R — only when an arm gives the bracket it was planned with.
		if armed {
			if risk := math.Abs(arm.EntryPx - arm.StopPx); risk > 0 {
				rr := math.Abs(arm.TargetPx-arm.EntryPx) / risk
				r.plannedRR = &rr
			}
		}
		// Realized R and the excursion statistics — absent unless wave 1A has a
		// row for this position. Never derived from close_reason: every closed
		// row in this store carries close_reason 'sync', which says how the row
		// was written, not how the trade ended.
		if e, ok := excs[p.ID]; ok {
			r.hasExc = true
			r.mae, r.mfe = e.MaePts, e.MfePts
			if pv := market.FuturesPointValue(p.Symbol); pv > 0 {
				// MATH-EXECUTOR P2-1: R is P&L over the risk of the WHOLE
				// position. trade_excursions.size is stamped at the FIRST
				// partial fill (1 of a 5-lot on #627 → −5.00R for a −1.00R
				// trade), so the position's own entry quantity wins.
				if risk := math.Abs(e.EntryPx-e.StopPxInitial) * wholePositionSize(p, e) * pv; risk > 0 {
					rr := v / risk
					r.realizedR = &rr
				}
			}
			stop := hitOf(e.ExitReason, "stop")
			target := hitOf(e.ExitReason, "target")
			r.stopHit, r.targetHit = &stop, &target
		}
		recs = append(recs, r)
		if p.ExitTime > tbl.AsOfMs {
			tbl.AsOfMs = p.ExitTime
		}
	}

	tbl.recs = recs
	tbl.rollUp()
	tbl.Counterfactual = buildE8(abs, positions)
	return tbl
}

// rollUp recomputes every projection from tbl.recs. Called once at build and
// again after an era filter, so a filtered roll-up is a real aggregation of the
// filtered rows and never the unfiltered population wearing a scoped label.
func (t *Table) rollUp() {
	t.Cells = aggregate(t.recs, func(k Key) Key { return k })
	t.Conditions = aggregate(t.recs, func(k Key) Key { return Key{Condition: k.Condition} })
	t.Sessions = aggregate(t.recs, func(k Key) Key { return Key{Session: k.Session} })
	t.Kinds = aggregate(t.recs, func(k Key) Key { return Key{LevelKind: k.LevelKind} })
	t.Paths = aggregate(t.recs, func(k Key) Key { return Key{Path: k.Path} })
}

// FilterEra returns the table restricted to one era, RE-AGGREGATED from the raw
// rows. An era it does not know yields an empty table rather than the
// unfiltered one: a filter that silently does nothing is how a scoped claim
// becomes a global claim without anyone editing a number.
func FilterEra(t Table, era string) Table {
	out := Table{BuiltAtMs: t.BuiltAtMs, Excluded: t.Excluded, Counterfactual: t.Counterfactual}
	if era != EraPre0B && era != EraPost0B {
		return out
	}
	for _, r := range t.recs {
		if r.key.Era != era {
			continue
		}
		out.recs = append(out.recs, r)
		if r.pnl != nil && r.exitTimeMs > out.AsOfMs {
			out.AsOfMs = r.exitTimeMs
		}
	}
	out.rollUp()
	return out
}

// eraOf splits by the trade's own instant. Deliberately NOT by session-day: 0B
// booted at 07:49:06 CT, mid-session-day, so a session-day bucket would put
// trades taken under two different binaries in one cell (E3).
func eraOf(entryMs int64) string {
	if entryMs < Era0BStart.UnixMilli() {
		return EraPre0B
	}
	return EraPost0B
}

func hitOf(reason, want string) bool {
	return len(reason) > 0 && containsFold(reason, want)
}

func containsFold(s, sub string) bool {
	ls, lsub := []rune(s), []rune(sub)
	if len(lsub) == 0 || len(ls) < len(lsub) {
		return false
	}
	lower := func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + 32
		}
		return r
	}
	for i := 0; i+len(lsub) <= len(ls); i++ {
		ok := true
		for j := range lsub {
			if lower(ls[i+j]) != lower(lsub[j]) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// conditionAndLevelKind recovers the play a position traded from the plan doc it
// cited. The condition is NOT stored on the position — trader_positions carries
// cited_scenario_id, and the scenario that id names lives in the doc. A row
// whose doc or scenario is missing returns "" and is counted, never guessed.
func conditionAndLevelKind(p posRow) (string, string) {
	if p.Doc == "" || p.CitedScenarioID == "" {
		return "", ""
	}
	var doc kernel.PlanDoc
	if err := json.Unmarshal([]byte(p.Doc), &doc); err != nil {
		return "", ""
	}
	for _, sc := range doc.Scenarios {
		if sc.ID != p.CitedScenarioID {
			continue
		}
		return sc.Condition, levelKindForScenario(doc, sc)
	}
	return "", ""
}

// levelKindForScenario resolves the level the scenario is judged against by
// EXACT price identity with a level in the same doc — the confirm's reference
// price is written from that level, so the match is an identity, not a
// proximity guess. No match, or no confirm, yields "" (unknown, and the cell
// says so) rather than the nearest level, which would be a fabricated join.
func levelKindForScenario(doc kernel.PlanDoc, sc kernel.PlanScenario) string {
	if sc.Confirm == nil || sc.Confirm.RefPrice == 0 {
		return ""
	}
	for _, l := range doc.Levels {
		if math.Abs(l.Price-sc.Confirm.RefPrice) < 1e-6 {
			return LevelKindFromLabel(l.Label)
		}
	}
	return ""
}

// aggregate collapses records onto a projected key and computes each cell.
func aggregate(recs []rec, project func(Key) Key) []Cell {
	type acc struct {
		vals       []float64
		ids        []int64
		unresolved int
		realizedR  []float64
		plannedRR  []float64
		mae, mfe   []float64
		stopHits   int
		targetHits int
		excN       int
	}
	buckets := map[Key]*acc{}
	order := []Key{}
	for _, r := range recs {
		k := project(r.key)
		a, ok := buckets[k]
		if !ok {
			a = &acc{}
			buckets[k] = a
			order = append(order, k)
		}
		if r.pnl == nil {
			a.unresolved++
			continue
		}
		a.vals = append(a.vals, *r.pnl)
		a.ids = append(a.ids, r.id)
		if r.realizedR != nil {
			a.realizedR = append(a.realizedR, *r.realizedR)
		}
		if r.plannedRR != nil {
			a.plannedRR = append(a.plannedRR, *r.plannedRR)
		}
		if r.hasExc {
			a.excN++
			if r.mae != nil {
				a.mae = append(a.mae, *r.mae)
			}
			if r.mfe != nil {
				a.mfe = append(a.mfe, *r.mfe)
			}
			if r.stopHit != nil && *r.stopHit {
				a.stopHits++
			}
			if r.targetHit != nil && *r.targetHit {
				a.targetHits++
			}
		}
	}

	out := make([]Cell, 0, len(order))
	for _, k := range order {
		a := buckets[k]
		// A cell with no resolved rows is NOT emitted: "no cell without n"
		// (A24). Its unresolved rows are already in the table-level ledger.
		if len(a.vals) == 0 {
			continue
		}
		c := computeCell(k, a.vals, a.ids)
		c.ExcludedUnresolved = a.unresolved
		c.AvgRealizedR = meanOrNil(a.realizedR)
		c.AvgPlannedRR = meanOrNil(a.plannedRR)
		c.MedianMAE = medianOrNil(a.mae)
		c.MedianMFE = medianOrNil(a.mfe)
		if a.excN > 0 {
			s := float64(a.stopHits) / float64(a.excN)
			t := float64(a.targetHits) / float64(a.excN)
			c.StopHitShare, c.TargetHitShare = &s, &t
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Key, out[j].Key
		if a.Condition != b.Condition {
			return a.Condition < b.Condition
		}
		if a.Session != b.Session {
			return a.Session < b.Session
		}
		if a.LevelKind != b.LevelKind {
			return a.LevelKind < b.LevelKind
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Era < b.Era
	})
	return out
}

func computeCell(k Key, vals []float64, ids []int64) Cell {
	c := Cell{Key: k, N: len(vals), RowIDs: ids}
	for _, v := range vals {
		c.SumPnL += v
		switch {
		case v > 0:
			c.Wins++
		case v < 0:
			c.Losses++
		default:
			c.Flats++
		}
	}
	n := float64(c.N)
	c.Mean = c.SumPnL / n
	if c.N > 1 {
		var ss float64
		for _, v := range vals {
			d := v - c.Mean
			ss += d * d
		}
		c.SD = math.Sqrt(ss / (n - 1))
	}
	c.WinRate = float64(c.Wins) / n
	c.WilsonLo, c.WilsonHi = wilson(c.Wins, c.N)
	if c.SD > 0 {
		se := c.SD / math.Sqrt(n)
		c.TStat = c.Mean / se
		c.MeanLo, c.MeanHi = c.Mean-z*se, c.Mean+z*se
	} else {
		c.MeanLo, c.MeanHi = c.Mean, c.Mean
	}

	// THE FLOOR (D3). Below MinN the cell is descriptive and carries no verdict.
	// The criterion is pre-registered and computed here — it is never typed into
	// a report by hand, which is how "expectancy > 0" verdicts got made on
	// samples too small to support them.
	if c.N < MinN {
		c.Descriptive = true
		c.Status = StatusNotEnoughData
		return c
	}
	if c.Mean > 0 && c.MeanLo > 0 {
		c.Status = StatusPasses
	} else {
		c.Status = StatusFails
	}
	return c
}

// wilson is the score interval on a proportion — used instead of the normal
// approximation because at these sample sizes the normal interval runs past 0
// and 1 and reads as certainty the data does not have.
func wilson(k, n int) (float64, float64) {
	if n == 0 {
		return 0, 0
	}
	fk, fn := float64(k), float64(n)
	d := fn + z*z
	center := (fk + z*z/2) / d
	margin := (z / d) * math.Sqrt(fk*(fn-fk)/fn+z*z/4)
	return center - margin, center + margin
}

func meanOrNil(v []float64) *float64 {
	if len(v) == 0 {
		return nil
	}
	var s float64
	for _, x := range v {
		s += x
	}
	m := s / float64(len(v))
	return &m
}

func medianOrNil(v []float64) *float64 {
	if len(v) == 0 {
		return nil
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	var m float64
	if len(s)%2 == 1 {
		m = s[len(s)/2]
	} else {
		m = (s[len(s)/2-1] + s[len(s)/2]) / 2
	}
	return &m
}

// buildE8 is the counterfactual side-table (D4). It is built from a DIFFERENT
// source table into a DIFFERENT type, and every row carries Counterfactual=true,
// so a counterfactual number cannot reach a realized cell by any code path.
func buildE8(abs []abRow, positions []posRow) []E8Cell {
	if len(abs) == 0 {
		return nil
	}
	// Direction is recovered from the plan doc, because ab_confirm_log has no
	// direction column — the same gap that makes the E8 sign bug unauditable
	// row by row.
	dir := map[string]string{}
	for _, p := range positions {
		if p.Doc == "" {
			continue
		}
		var doc kernel.PlanDoc
		if json.Unmarshal([]byte(p.Doc), &doc) != nil {
			continue
		}
		for _, sc := range doc.Scenarios {
			dir[armKey(p.PlanID, p.PlanVersion, sc.ID)] = sc.Direction
		}
	}

	type acc struct {
		n, wins, losses int
		usableN         int
		sum             float64
		suspect         bool
		exclPriceScale  int
		exclZeroPnL     int
	}
	buckets := map[Key]*acc{}
	rules := map[Key]string{}
	order := []Key{}
	for _, r := range abs {
		k := Key{Condition: r.Condition, Session: r.Session}
		a, ok := buckets[k]
		if !ok {
			a = &acc{}
			buckets[k] = a
			rules[k] = r.Rule
			order = append(order, k)
		}
		a.n++
		// USABILITY, decided per row and counted, never silently averaged over.
		switch {
		case r.EntryPx > 0 && math.Abs(r.NetPnl) >= r.EntryPx:
			// A "P&L" at least as large as the instrument's price is a price,
			// not a result. Live shape: net_pnl ≈ −(entry × multiplier).
			a.exclPriceScale++
		case r.NetPnl == 0:
			// Zero beside a resolved outcome is UNCOMPUTED, not break-even.
			a.exclZeroPnL++
		default:
			a.usableN++
			a.sum += r.NetPnl
		}
		switch r.Outcome {
		case "win":
			a.wins++
		case "loss":
			a.losses++
		}
		// SUSPECT unless the direction is recovered AND is long. An
		// unrecoverable direction stays suspect: it cannot be cleared.
		if d := dir[armKey(r.PlanID, r.Version, r.Scenario)]; d != "long" {
			a.suspect = true
		}
	}
	out := make([]E8Cell, 0, len(order))
	for _, k := range order {
		a := buckets[k]
		c := E8Cell{
			Key: k, Rule: rules[k], N: a.n, UsableN: a.usableN,
			Wins: a.wins, Losses: a.losses,
			ExcludedPriceScale: a.exclPriceScale, ExcludedZeroPnL: a.exclZeroPnL,
			Counterfactual: true, ShortSuspect: a.suspect,
			Note: "counterfactual (E8) — never comparable with a realized cell",
		}
		// The money fields exist ONLY when something in the cell could be
		// arithmetic'd. No usable row means no number, not a zero.
		if a.usableN > 0 {
			sum := a.sum
			mean := sum / float64(a.usableN)
			c.SumPnL, c.Mean = &sum, &mean
		}
		if a.suspect {
			c.Note = "counterfactual (E8) · SHORT ROWS SUSPECT (E8 sign bug) — " +
				"direction is not stored on ab_confirm_log; an unrecovered direction stays suspect"
		}
		if a.usableN == 0 {
			c.Note = fmt.Sprintf("counterfactual (E8) · NO USABLE net_pnl in %d rows "+
				"(%d price-scale, %d uncomputed zero) — no mean is computable",
				a.n, a.exclPriceScale, a.exclZeroPnL)
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Condition != out[j].Condition {
			return out[i].Condition < out[j].Condition
		}
		return out[i].Session < out[j].Session
	})
	return out
}

// BootLine is D7. Every number in it is READ from the table that was just
// built — there is no literal in this string, so it cannot drift from what the
// process actually computed (A24: no boot-line literal).
func (t *Table) BootLine() string {
	withN := 0
	for _, c := range t.Cells {
		if c.N >= MinN {
			withN++
		}
	}
	// judged_rollups is a separate count because the two can disagree in a way
	// that misleads. Live on 2026-09-03: cells=41 with_n>=30=0 while the
	// `reject` CONDITION roll-up stood at n=31 and was judged FAILS. Both
	// numbers were true; together they read as "nothing is judgeable", which
	// was not. Verdicts are made on roll-ups, so roll-ups are counted here.
	judged := 0
	for _, set := range [][]Cell{t.Conditions, t.Sessions, t.Kinds, t.Paths} {
		for _, c := range set {
			if !c.Descriptive {
				judged++
			}
		}
	}
	return fmt.Sprintf("📊 expectancy: cells=%d with_n>=%d=%d judged_rollups=%d unresolved=%d excluded_test=%d",
		len(t.Cells), MinN, withN, judged, t.Excluded.UnresolvedPnL, t.Excluded.TestSeam)
}

// wholePositionSize is the contract count R is measured over: the position's
// entry quantity (the whole fill), then its quantity, then — only when the
// position row carries neither — the excursion row's own size.
func wholePositionSize(p posRow, e excRow) float64 {
	if p.EntryQuantity > 0 {
		return p.EntryQuantity
	}
	if p.Quantity > 0 {
		return p.Quantity
	}
	return e.Size
}
