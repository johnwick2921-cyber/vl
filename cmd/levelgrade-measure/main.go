// levelgrade-measure — FIX-LEVEL-GRADE measurement harness (DS-108, read-only).
//
// Re-scores the persisted candidate_pool (full per-read candidate sets) with
// fixed confluence bands (±3/±5/±10/±20 pts) and joins the SEATED candidates to
// the level_stats outcome rows (touched/reacted/broke_clean), to answer: does a
// tighter confluence band separate grade A from grade C on reaction / broke-clean?
//
// Method (faithful to kernel/levels_score.go scoreLevelsPool):
//   - base = stored_score / stored_confluence_multiplier  (all non-confluence
//     components — evidence, freshness, size, TF, HTF — held fixed)
//   - conf(band) = # distinct families within ±band pts of the level, over the
//     SAME read's candidate pool, excluding exact duplicates (price+kind+label)
//   - effConf = min(conf, 3)  ·  confMult = 1 + 0.20*effConf
//   - newScore = base * confMult  ·  newGrade = gradeFromScore(newScore)
//   - zones: TF floor/cap re-applied from the label's ·tf suffix, then the
//     Tier-1 proximity cap (B/A zone only within 3.00 pts of a Tier-1 anchor).
//
// Reference "today (0.10×dATR)" = the STORED grade (level_stats.grade), which
// is the production band; the fixed bands are recomputed.
//
// Reads only. Run against a COPY of the live DB:
//
//	go run ./cmd/levelgrade-measure --db /tmp/lvlgrade-db.db
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	_ "vl/store/sqlitedriver" // the ONE sqlite registration site
)

type cand struct {
	readAtMs int64
	day      string // plan_id prefix YYYY-MM-DD
	price    float64
	kind     string
	label    string
	family   string
	seated   bool
	score    float64
	confMult float64 // stored confluence_multiplier
	grade    string
	zone     bool
	tier     string // "" | 1m | 15m | 1h | 4h
}

type statRow struct {
	day     string
	price   float64
	label   string
	family  string
	grade   string
	touched bool
	reacted bool
	broke   bool
}

type keyT struct {
	day   string
	price float64
	label string
}

func main() {
	dbPath := flag.String("db", "/tmp/lvlgrade-db.db", "path to a COPY of data.db")
	flag.Parse()

	db, err := sql.Open("sqlite", *dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}
	defer db.Close()

	cands := loadCandidates(db)
	fmt.Fprintf(os.Stderr, "candidate_pool rows: %d\n", len(cands))
	stats := loadStats(db)
	fmt.Fprintf(os.Stderr, "level_stats rows: %d\n", len(stats))

	// Group candidates by read.
	reads := map[int64][]*cand{}
	for i := range cands {
		c := &cands[i]
		reads[c.readAtMs] = append(reads[c.readAtMs], c)
	}

	// Family fallback for rows whose score_components were not persisted.
	for _, cs := range reads {
		for _, c := range cs {
			if c.family == "" {
				c.family = familyForKind(c.kind)
			}
			if c.zone {
				c.tier = zoneTierForLabel(c.label)
			}
		}
	}

	// Re-score every read under the fixed bands. Store per-candidate per-band grade.
	bands := []float64{3, 5, 10, 20}
	// key: readAtMs|price|kind|label → map[band]grade
	re := map[string]map[float64]string{}
	for readAt, cs := range reads {
		for _, c := range cs {
			key := fmt.Sprintf("%d|%.6f|%s|%s", readAt, c.price, c.kind, c.label)
			g := map[float64]string{}
			for _, b := range bands {
				g[b] = rescoreGrade(c, cs, b)
			}
			re[key] = g
		}
	}

	// Join each level_stats row to the latest read on the SAME day seating the same
	// level (candidate day comes from the plan_id prefix, not the read timestamp).
	statsKey := func(r statRow) keyT { return keyT{r.day, r.price, r.label} }
	joined := map[keyT]*cand{}
	for i := range stats {
		r := &stats[i]
		k := statsKey(*r)
		if _, ok := joined[k]; ok {
			continue
		}
		// All reads that day, newest first.
		var dayReads []int64
		for readAt, cs := range reads {
			if len(cs) > 0 && cs[0].day == r.day {
				dayReads = append(dayReads, readAt)
			}
		}
		sort.Slice(dayReads, func(a, b int) bool { return dayReads[a] > dayReads[b] })
		for _, readAt := range dayReads {
			for _, c := range reads[readAt] {
				if c.seated && c.label == r.label && math.Abs(c.price-r.price) < 0.01 {
					joined[k] = c
					break
				}
			}
			if joined[k] != nil {
				break
			}
		}
	}

	// Print tables.
	printBandTable(stats, joined, re, statsKey)
	printFitHoldout(stats, joined, re, statsKey)
	printFamilyTable(stats)
	printTouchTable(db, stats)
}

func printBandTable(stats []statRow, joined map[keyT]*cand, re map[string]map[float64]string, statsKey func(statRow) keyT) {
	fmt.Println("\n## Band sweep — reaction% / broke-clean% by grade (seated levels, n)")
	// Reference = stored grade.
	ref := aggregateByGrade(stats)
	fmt.Println("\n### today (0.10×dATR) — stored grade")
	printGradeAgg(ref)

	for _, band := range []float64{3, 5, 10, 20} {
		rows := make([]statRow, 0, len(stats))
		skipped := 0
		for i := range stats {
			r := &stats[i]
			c := joined[statsKey(*r)]
			if c == nil {
				skipped++
				continue
			}
			key := fmt.Sprintf("%d|%.6f|%s|%s", c.readAtMs, c.price, c.kind, c.label)
			g, ok := re[key][band]
			if !ok {
				skipped++
				continue
			}
			nr := *r
			nr.grade = g
			rows = append(rows, nr)
		}
		fmt.Printf("\n### ±%.0f pts (joined %d, skipped %d)\n", band, len(rows), skipped)
		printGradeAgg(aggregateByGrade(rows))
	}
}

type gradeAgg struct {
	n, touched, reacted, broke int
}

func aggregateByGrade(rows []statRow) map[string]*gradeAgg {
	out := map[string]*gradeAgg{}
	for i := range rows {
		r := &rows[i]
		a := out[r.grade]
		if a == nil {
			a = &gradeAgg{}
			out[r.grade] = a
		}
		a.n++
		if r.touched {
			a.touched++
		}
		if r.reacted {
			a.reacted++
		}
		if r.broke {
			a.broke++
		}
	}
	return out
}

func printGradeAgg(m map[string]*gradeAgg) {
	total := 0
	for _, a := range m {
		total += a.n
	}
	grades := make([]string, 0, len(m))
	for g := range m {
		grades = append(grades, g)
	}
	sort.Strings(grades)
	fmt.Printf("  total=%d\n", total)
	for _, g := range grades {
		a := m[g]
		reactPct := 0.0
		brokePct := 0.0
		if a.touched > 0 {
			reactPct = 100 * float64(a.reacted) / float64(a.touched)
			brokePct = 100 * float64(a.broke) / float64(a.touched)
		}
		shareA := 0.0
		if total > 0 {
			shareA = 100 * float64(a.n) / float64(total)
		}
		fmt.Printf("  %s n=%d share=%.1f%% touched=%d reacted=%.1f%% broke_clean=%.1f%%\n", g, a.n, shareA, a.touched, reactPct, brokePct)
	}
}

func printFitHoldout(stats []statRow, joined map[keyT]*cand, re map[string]map[float64]string, statsKey func(statRow) keyT) {
	// Fit/hold-out split (DECLARED BEFORE LOOKING at hold-out results):
	// chronological by session day; fit = days < 2026-09-28, hold-out = >= 2026-09-28.
	const splitDay = "2026-09-28"
	fmt.Printf("\n## Fit/hold-out split at %s (A vs C gap; negative broke gap = A holds better)\n", splitDay)
	fmt.Printf("%-14s | %-24s | %-24s\n", "band", "FIT (n)", "HOLD-OUT (n)")
	for _, band := range []float64{3, 5, 10, 20} {
		var fit, hold []statRow
		for i := range stats {
			r := &stats[i]
			c := joined[statsKey(*r)]
			if c == nil {
				continue
			}
			key := fmt.Sprintf("%d|%.6f|%s|%s", c.readAtMs, c.price, c.kind, c.label)
			g, ok := re[key][band]
			if !ok {
				continue
			}
			nr := *r
			nr.grade = g
			if r.day < splitDay {
				fit = append(fit, nr)
			} else {
				hold = append(hold, nr)
			}
		}
		fa, fc := gapFor(fit)
		ha, hc := gapFor(hold)
		fmt.Printf("%-14s | %-24s | %-24s\n", fmt.Sprintf("±%.0f pts", band), gapStr("fit", len(fit), fa, fc), gapStr("hold", len(hold), ha, hc))
	}
}

func gapFor(rows []statRow) (a, c *gradeAgg) {
	m := aggregateByGrade(rows)
	return m["A"], m["C"]
}

func gapStr(tag string, n int, a, c *gradeAgg) string {
	if a == nil || c == nil {
		return fmt.Sprintf("%s n=%d (no A or C)", tag, n)
	}
	reactGap := pct(a.reacted, a.touched) - pct(c.reacted, c.touched)
	brokeGap := pct(a.broke, a.touched) - pct(c.broke, c.touched)
	return fmt.Sprintf("%s n=%d A=%d C=%d Δreact=%.1f Δbroke=%.1f", tag, n, a.n, c.n, reactGap, brokeGap)
}

func pct(num, den int) float64 {
	if den == 0 {
		return 0
	}
	return 100 * float64(num) / float64(den)
}

func printFamilyTable(stats []statRow) {
	fmt.Println("\n## Reaction% by family (stored grade)")
	fam := map[string]*gradeAgg{}
	for i := range stats {
		r := &stats[i]
		a := fam[r.family]
		if a == nil {
			a = &gradeAgg{}
			fam[r.family] = a
		}
		a.n++
		if r.touched {
			a.touched++
		}
		if r.reacted {
			a.reacted++
		}
		if r.broke {
			a.broke++
		}
	}
	keys := make([]string, 0, len(fam))
	for k := range fam {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		a := fam[k]
		reactPct := 0.0
		brokePct := 0.0
		if a.touched > 0 {
			reactPct = 100 * float64(a.reacted) / float64(a.touched)
			brokePct = 100 * float64(a.broke) / float64(a.touched)
		}
		fmt.Printf("  %-10s n=%d touched=%d reacted=%.1f%% broke_clean=%.1f%%\n", k, a.n, a.touched, reactPct, brokePct)
	}
}

func printTouchTable(db *sql.DB, stats []statRow) {
	fmt.Println("\n## Reaction% by touch count (touch_episodes max touch_number)")
	// max touch_number per (session_day, label, price)
	rows, err := db.Query(`SELECT session_day, label, level_price, MAX(touch_number) FROM touch_episodes GROUP BY session_day, label, level_price`)
	if err != nil {
		fmt.Println("  touch_episodes query failed:", err)
		return
	}
	defer rows.Close()
	type tk struct {
		day   string
		label string
		price float64
	}
	maxTouch := map[tk]int{}
	for rows.Next() {
		var day, label string
		var price float64
		var n int
		if err := rows.Scan(&day, &label, &price, &n); err != nil {
			continue
		}
		maxTouch[tk{day, label, price}] = n
	}
	buckets := map[string]*gradeAgg{}
	for i := range stats {
		r := &stats[i]
		n := maxTouch[tk{r.day, r.label, r.price}]
		bucket := "0"
		switch {
		case n >= 3:
			bucket = "3+"
		case n == 2:
			bucket = "2"
		case n == 1:
			bucket = "1"
		}
		a := buckets[bucket]
		if a == nil {
			a = &gradeAgg{}
			buckets[bucket] = a
		}
		a.n++
		if r.touched {
			a.touched++
		}
		if r.reacted {
			a.reacted++
		}
	}
	for _, b := range []string{"0", "1", "2", "3+"} {
		a := buckets[b]
		if a == nil {
			continue
		}
		reactPct := 0.0
		if a.touched > 0 {
			reactPct = 100 * float64(a.reacted) / float64(a.touched)
		}
		fmt.Printf("  touches=%-2s n=%d touched=%d reacted=%.1f%%\n", b, a.n, a.touched, reactPct)
	}
}

func loadCandidates(db *sql.DB) []cand {
	rows, err := db.Query(`SELECT read_at_ms, plan_id, level_price, level_kind, label, seated, score, score_components FROM candidate_pool`)
	if err != nil {
		fmt.Fprintln(os.Stderr, "candidate_pool:", err)
		os.Exit(1)
	}
	defer rows.Close()
	var out []cand
	for rows.Next() {
		var readAt int64
		var price, score float64
		var planID, kind, label, comps string
		var seated bool
		if err := rows.Scan(&readAt, &planID, &price, &kind, &label, &seated, &score, &comps); err != nil {
			continue
		}
		day := planID
		if len(day) >= 10 {
			day = day[:10]
		}
		c := cand{readAtMs: readAt, day: day, price: price, kind: kind, label: label, seated: seated, score: score, confMult: 1.0}
		c.family = familyForKind(kind)
		c.zone = isZoneKind(kind)
		if comps != "" && comps != "{}" {
			var m struct {
				Family   string  `json:"family"`
				ConfMult float64 `json:"confluence_multiplier"`
				ConfRaw  int     `json:"confluence_raw"`
			}
			if json.Unmarshal([]byte(comps), &m) == nil {
				if m.Family != "" {
					c.family = m.Family
				}
				if m.ConfMult > 0 {
					c.confMult = m.ConfMult
				}
			}
		}
		out = append(out, c)
	}
	return out
}

func loadStats(db *sql.DB) []statRow {
	rows, err := db.Query(`SELECT session_day, price, label, family, grade, touched, reacted, broke_clean FROM level_stats`)
	if err != nil {
		fmt.Fprintln(os.Stderr, "level_stats:", err)
		os.Exit(1)
	}
	defer rows.Close()
	var out []statRow
	for rows.Next() {
		var r statRow
		if err := rows.Scan(&r.day, &r.price, &r.label, &r.family, &r.grade, &r.touched, &r.reacted, &r.broke); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out
}

func rescoreGrade(c *cand, pool []*cand, band float64) string {
	conf := 0
	seen := map[string]bool{}
	for _, o := range pool {
		if o.price == c.price && o.kind == c.kind && o.label == c.label {
			continue
		}
		if math.Abs(o.price-c.price) <= band {
			f := o.family
			if f == "" {
				f = familyForKind(o.kind)
			}
			if f != c.family && !seen[f] {
				seen[f] = true
				conf++
			}
		}
	}
	eff := conf
	if eff > 3 {
		eff = 3
	}
	confMult := 1 + 0.20*float64(eff)
	base := c.score / c.confMult
	if c.confMult <= 0 {
		base = c.score
	}
	score := base * confMult
	grade := gradeFromScore(score)
	if c.zone {
		// TF floor/cap (v3): 1m → C, 15m → B exactly, 1h/4h floor B.
		switch c.tier {
		case "15m":
			grade = "B"
		case "1h", "4h":
			if grade == "C" {
				grade = "B"
			}
		default:
			grade = "C"
		}
		// Tier-1 proximity cap: B/A zone only within 3.00 pts of a Tier-1 anchor.
		if grade != "C" && !withinTier1(pool, c.price) {
			grade = "C"
		}
	}
	return grade
}

func gradeFromScore(s float64) string {
	switch {
	case s >= 1.0:
		return "A"
	case s >= 0.70:
		return "B"
	default:
		return "C"
	}
}

func withinTier1(pool []*cand, price float64) bool {
	const tol = 3.00 // Tier1ProximityTicks(12) * 0.25
	for _, o := range pool {
		if !isTier1Kind(o.kind) {
			continue
		}
		if math.Abs(o.price-price) <= tol {
			return true
		}
	}
	return false
}

// ── mirrors of kernel/levels_score.go (cited, not imported — unexported there) ──

func familyForKind(k string) string {
	switch k {
	case "VWAP", "eVWAP", "pdVWAP", "VWAP±2σ":
		return "vwap"
	case "POC", "nPOC", "VAH", "VAL":
		return "profile"
	case "PDH", "PDL", "PDC", "RTH-H", "RTH-L", "SETT":
		return "prior"
	case "PWH", "PWL", "PMH", "PML":
		return "anchor"
	case "ONH", "ONL", "AS-H", "AS-L", "LDN-H", "LDN-L", "OR-H", "OR-L", "IB-H", "IB-L", "MID-O":
		return "overnight"
	case "EQH", "EQL":
		return "liquidity"
	case "SUPPLY", "DEMAND", "FVG", "IFVG", "OB":
		return "zone"
	case "RN":
		return "round"
	case "GAP":
		return "gap"
	default:
		return "other"
	}
}

func isZoneKind(k string) bool {
	switch k {
	case "SUPPLY", "DEMAND", "FVG", "IFVG", "OB":
		return true
	}
	return false
}

func isTier1Kind(k string) bool {
	switch k {
	case "PDH", "PDL", "PDC", "RTH-H", "RTH-L", "OR-H", "OR-L", "ONH", "ONL",
		"PWH", "PWL", "PMH", "PML", "VAH", "VAL", "SETT", "nPOC":
		return true
	}
	return false
}

func zoneTierForLabel(label string) string {
	l := strings.ToLower(label)
	switch {
	case strings.Contains(l, "·4h"), strings.Contains(l, "·1d"):
		return "4h"
	case strings.Contains(l, "·1h"), strings.Contains(l, "·2h"):
		return "1h"
	case strings.Contains(l, "·15m"), strings.Contains(l, "·30m"):
		return "15m"
	default:
		return "1m"
	}
}
