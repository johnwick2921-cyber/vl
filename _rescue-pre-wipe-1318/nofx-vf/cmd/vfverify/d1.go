package main

// d1.go — DRESS REHEARSAL (2026-08-30): SESSION step.
//
// Sandbox-only: every read comes from the SCRATCH DB copy
// (/tmp/nofx-vf-db/data.db, refreshed from the LIVE DB via .backup in
// read-only mode). Nothing here writes to the scratch DB either — this step
// is render + ONE real AI call + the REAL validator chain + evidence files.
// The LIVE DB and the live bot are never touched.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"nofx/crypto"
	"nofx/kernel"
	"nofx/market"
	"nofx/mcp"
	_ "nofx/mcp/provider" // registers deepseek
	"nofx/store"
)

const dressEvDir = "/home/hoang/nofx-vf/docs/superpowers/dress-rehearsal-0830"

// dressSystemPrompt — the EXACT planner system prompt (same string as p4.go's
// plannerSystemPrompt).
const dressSystemPrompt = "You are a disciplined CME index-futures day-plan reasoner. Output ONLY the single JSON object requested — reasoning first, then the answer fields. No prose outside the JSON."

// aiPlanMax mirrors trader/auto_trader_planner.go:924 aiPlanMaxTokens()
// (env AI_PLAN_MAX_TOKENS, default 65536).
func aiPlanMax() int {
	if v := os.Getenv("AI_PLAN_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return n
		}
	}
	return 65536
}

// evLog captures every printed line for the verbatim evidence files.
type evLog struct {
	lines []string
}

func (l *evLog) p(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	fmt.Println(line)
	l.lines = append(l.lines, line)
}

func (l *evLog) write(path string) {
	must(os.WriteFile(path, []byte(strings.Join(l.lines, "\n")+"\n"), 0o600))
}

// decryptDeepSeekKey decrypts the rotated key from the SCRATCH ai_models row.
func decryptDeepSeekKey(sdb *sql.DB) (string, string) {
	var cipher string
	if err := sdb.QueryRow(`SELECT api_key FROM ai_models WHERE id='8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek'`).Scan(&cipher); err != nil {
		must(fmt.Errorf("scratch ai_models read: %v", err))
	}
	cs, err := crypto.NewCryptoService()
	must(err)
	key, err := cs.DecryptFromStorage(cipher)
	must(err)
	return key, cipher
}

// nearestInPool returns the closest scored level to px across the union of
// pool + seated + HTF zones, plus its distance.
func nearestInPool(px float64, sets ...[]kernel.ScoredLevel) (*kernel.ScoredLevel, float64, string) {
	var best *kernel.ScoredLevel
	bd := 1e9
	src := ""
	consider := func(rows []kernel.ScoredLevel, name string) {
		for i := range rows {
			d := math.Abs(rows[i].Price - px)
			if d < bd {
				bd = d
				c := rows[i]
				best = &c
				src = name
			}
		}
	}
	for i, s := range sets {
		consider(s, []string{"pool", "seated", "htfzones", "htfzonesfull"}[i%4])
	}
	return best, bd, src
}

// fridayTapeTag reports whether Friday's stored 1m tape traded `px` and when.
func fridayTapeTag(bars1m []market.Kline, px float64, loc *time.Location) string {
	daily := kernel.DailySessionBars(bars1m)
	if len(daily) == 0 {
		return "no daily session bars — cannot check"
	}
	start := daily[len(daily)-1].OpenTime
	n, first := 0, ""
	for _, b := range bars1m {
		if b.OpenTime < start {
			continue
		}
		if b.Low <= px && px <= b.High {
			if first == "" {
				first = time.UnixMilli(b.OpenTime).In(loc).Format("01-02 15:04")
			}
			n++
		}
	}
	if n == 0 {
		return fmt.Sprintf("NEVER traded on the last stored session tape (session start %s CT)",
			time.UnixMilli(start).In(loc).Format("01-02 15:04"))
	}
	return fmt.Sprintf("TRADED %d× — first at %s CT", n, first)
}

// scenarioArmMath recomputes the arm bracket math independently of the model.
type armMath struct {
	dist, rr   float64
	wellFormed bool
}

func recomputeArm(a *kernel.PlanArmSpec, dir string) armMath {
	m := armMath{}
	if a == nil || !a.Enabled {
		return m
	}
	if strings.EqualFold(dir, "short") {
		m.dist = a.Stop - a.Entry
		if m.dist > 0 {
			m.rr = (a.Entry - a.Target) / m.dist
		}
		m.wellFormed = m.dist > 0 && a.Target < a.Entry && a.Entry < a.Stop
	} else {
		m.dist = a.Entry - a.Stop
		if m.dist > 0 {
			m.rr = (a.Target - a.Entry) / m.dist
		}
		m.wellFormed = m.dist > 0 && a.Stop < a.Entry && a.Entry < a.Target
	}
	return m
}

func trendOf(structure []string, tf string) string {
	for _, s := range structure {
		f := strings.Fields(s)
		if len(f) >= 2 && strings.TrimSuffix(f[0], ":") == tf {
			return f[1]
		}
	}
	return "unavailable"
}

func vetoOpposesLocal(side, trend string) bool {
	return (side == "short" && trend == "TRENDING_UP") || (side == "long" && trend == "TRENDING_DOWN")
}

func runD1() {
	log := &evLog{}
	log.p("════════ d1 DRESS REHEARSAL — SESSION STEP (2026-08-30) ════════")
	log.p("scratch DB: %s (opened mode=ro — this step writes NOTHING to it)", sandboxDBPath)

	sdb := openRO(sandboxDBPath)
	defer sdb.Close()
	log.p("scratch bars rows=%d · scratch plans rows=%d · scratch WEEKLY rows=%d",
		qint(sdb, `SELECT COUNT(*) FROM bars`),
		qint(sdb, `SELECT COUNT(*) FROM plans`),
		qint(sdb, `SELECT COUNT(*) FROM plans WHERE session='WEEKLY'`))

	now := time.Now()
	loc := kernel.CTLocation()
	log.p("now CT=%s · now UTC=%s", now.In(loc).Format(time.RFC3339), now.UTC().Format(time.RFC3339))

	// Session identity EXACTLY like trader/auto_trader_planner.go:180
	// maybeRunSessionReadsAt: registry session → kernel.PlanChainTradeDate.
	reg := loadRegistry(sdb)
	var asia *kernel.SessionDef
	for i := range reg.Sessions {
		if reg.Sessions[i].Name == "ASIA" {
			asia = &reg.Sessions[i]
			break
		}
	}
	var tradeDate string
	if asia != nil {
		log.p("registry ASIA row: enabled=%v window=%s→%s read=%s flat=%s (system_config.session_registry, scratch DB)",
			asia.Enabled, asia.WindowStartCT, asia.WindowEndCT, asia.ReadCT, asia.FlatCT)
		tradeDate, _ = kernel.PlanChainTradeDate(asia, now)
	} else {
		tradeDate = now.In(loc).Format("2006-01-02")
		log.p("registry ASIA row MISSING — fallback tradeDate=%s", tradeDate)
	}
	session := "ASIA"
	log.p("chain identity: session=%s trade_date=%s (kernel.PlanChainTradeDate, kernel/plan_chain_date.go:60)", session, tradeDate)

	as := assemble(sdb, session, tradeDate, now, 0)
	prompt := kernel.BuildPlannerPrompt(as.in)

	// ── e1: full prompt verbatim + token footer ─────────────────────────────
	chars := len([]rune(prompt))
	scratchBars := qint(sdb, `SELECT COUNT(*) FROM bars`)
	footer := fmt.Sprintf("\n\n════ E1 FOOTER (dress-rehearsal-0830) ════\nchars=%d bytes=%d tokens(chars/4)=%d %%of65536=%.1f%%\nrendered=%s CT from SCRATCH DB %s (%d bars rows)\nsession=%s trade_date=%s · builder=kernel.BuildPlannerPrompt (kernel/planner_prompt.go:309)\n",
		chars, len(prompt), chars/4, float64(chars)/65536*100,
		now.In(loc).Format(time.RFC3339), sandboxDBPath, scratchBars, session, tradeDate)
	must(os.WriteFile(dressEvDir+"/e1_prompt_rendered.txt", []byte(prompt+footer), 0o600))
	log.p("e1 written: %d chars ≈ %d tokens (%.1f%% of 65536)", chars, chars/4, float64(chars)/65536*100)

	// ── e7: env truth — knobs resolved AT CALL TIME ─────────────────────────
	key, cipher := decryptDeepSeekKey(sdb)
	fpOK := strings.HasPrefix(key, "sk-") && strings.HasSuffix(key, "4681")
	proxRaw := 0.0
	if as.dp != nil {
		proxRaw = as.dp.ProximityFilterATR
	}
	minRR, minConf := 3.0, 60
	if as.sc != nil {
		minRR, minConf = as.sc.RiskControl.MinRiskRewardRatio, as.sc.RiskControl.MinConfidence
	}
	e7 := fmt.Sprintf(`════ E7 ENV TRUTH (dress-rehearsal-0830) — resolved at call time ════
scratch ai_models row: id=8ef641a7-815c-4bb5-9798-b070b67d7998_deepseek · name="DeepSeek AI" · provider=deepseek · enabled=1
  custom_api_url="" · custom_model_name="" · thinking_mode="" · reasoning_effort="" · api_key len=%d (%s)
decrypted key: %s…%s (len %d, ENC:v1 AES-256-GCM, DATA_ENCRYPTION_KEY from /home/hoang/nofx/.env)
fingerprint check (must be sk-e…4681): %v
model: client=mcp.NewAIClientByProvider("deepseek") → DefaultDeepSeekModel="deepseek-v4-pro" (mcp/providers.go:20)
max_tokens: AI_PLAN_MAX_TOKENS=%q → aiPlanMaxTokens()=%d (trader/auto_trader_planner.go:924; default 65536)
proximity: strategy day_plan.proximity_filter_atr=%.2f → kernel.ResolveProximityK=%g (kernel/plan_lifecycle.go:25)
veto: HTF_VETO_MODE=%q → kernel.HTFVetoMode()=%q (kernel/htf_veto.go:39; cross = 1h AND 4h must agree)
arm_rr: ARM_MIN_RR = 2.0 (fixed contract, kernel/planner_prompt.go:600 + plan_doc.go:413)
min-RR floor (decision gate): risk_control.min_risk_reward_ratio=%.2f · min_confidence=%d (store.RiskControlConfig, live strategy row)
min-SL: MIN_SL_ATR_MULT=%q → kernel.MinSLATRMult()=%.2f (kernel/min_sl.go:33)
PLANNER_CANDLES: env=%q → kernel.PlannerCandlesEnabled()=%v (kernel/weekly_knobs.go:121)
BD_MIN_DISP_ATR: env=%q (kernel/breakdown_continue.go:44)
CONFLUENCE_CAP: env=%q (default 3)
session caps: max_levels=%d · scenario_cap=%d · side_quota=%d · min_grade=%q (resolved strategy config)
market: price=%.2f · dATR=%.1f · ATR5m=%.2f (StaleConfirmATR5m, kernel/plan_confirm.go)
`,
		len(cipher), cipher[:13],
		key[:5], key[len(key)-4:], len(key), fpOK,
		os.Getenv("AI_PLAN_MAX_TOKENS"), aiPlanMax(),
		proxRaw, kernel.ResolveProximityK(proxRaw),
		os.Getenv("HTF_VETO_MODE"), kernel.HTFVetoMode(),
		minRR, minConf,
		os.Getenv("MIN_SL_ATR_MULT"), kernel.MinSLATRMult(),
		os.Getenv("PLANNER_CANDLES"), kernel.PlannerCandlesEnabled(),
		os.Getenv("BD_MIN_DISP_ATR"),
		os.Getenv("CONFLUENCE_CAP"),
		as.in.MaxLevels, as.in.ScenarioCap, as.sideQuota, resolvePlanCfgMinGrade(as.dp, session),
		as.price, as.dATR, as.atr5m)
	must(os.WriteFile(dressEvDir+"/e7_env_truth.txt", []byte(e7), 0o600))
	log.p("e7 written (fingerprint check=%v)", fpOK)
	if !fpOK {
		log.p("FATAL: decrypted key fingerprint does NOT match sk-e…4681 — aborting before any AI call")
		log.write(dressEvDir + "/e4_validator_log.txt")
		os.Exit(2)
	}

	// ── the ONE real AI call (session planner) ──────────────────────────────
	client := mcp.NewAIClientByProvider("deepseek")
	if client == nil {
		log.p("FATAL: deepseek provider not registered")
		log.write(dressEvDir + "/e4_validator_log.txt")
		os.Exit(2)
	}
	mcp.ApplyThinking(client, "", "")
	client.SetAPIKey(key, "", "")
	restore := mcp.ApplyMaxTokens(client, aiPlanMax())
	defer restore()
	start := time.Now()
	raw, callErr := client.CallWithMessages(dressSystemPrompt, prompt)
	elapsed := time.Since(start)
	finish := mcp.LastFinishReason(client)
	resolvedModel := client.ResolvedModel()
	log.p("AI call: err=%v · elapsed=%.1fs · raw bytes=%d · finish_reason=%q · resolved_model=%q",
		callErr, elapsed.Seconds(), len(raw), finish, resolvedModel)
	must(os.WriteFile(dressEvDir+"/e2_ai_response_raw.json", []byte(raw), 0o600))
	log.p("e2 written: raw response untrimmed (%d bytes)", len(raw))

	atCall := fmt.Sprintf("\n══ AT-CALL-TIME APPENDIX ══\nresolved_model at call time: %q\nfinish_reason=%q · elapsed=%.1fs · raw bytes=%d\nerr=%v\n",
		resolvedModel, finish, elapsed.Seconds(), len(raw), callErr)
	f, ferr := os.OpenFile(dressEvDir+"/e7_env_truth.txt", os.O_APPEND|os.O_WRONLY, 0o600)
	if ferr == nil {
		f.WriteString(atCall)
		f.Close()
	}
	if callErr != nil || strings.TrimSpace(raw) == "" {
		log.p("FATAL: AI call failed/empty — stopping (no fabrication)")
		log.write(dressEvDir + "/e4_validator_log.txt")
		os.Exit(2)
	}

	// ── the REAL parse + validator chain (the write path) ───────────────────
	processSessionResponse(log, raw, as, "call", now)
	log.p("\n════ d1 COMPLETE — evidence e1/e2/e7 written; chain + e3/e4 by processSessionResponse ════")
	log.write(dressEvDir + "/e4_validator_log.txt")
}

// runD1Replay re-processes the SAME saved raw response (e2) through the full
// chain — ZERO new AI calls (used to regenerate e3/e4 after a harness edit).
func runD1Replay() {
	log := &evLog{}
	log.p("════ d1 REPLAY (no AI call — re-processes e2_ai_response_raw.json) ════")
	sdb := openRO(sandboxDBPath)
	defer sdb.Close()
	now := time.Now()
	loc := kernel.CTLocation()
	reg := loadRegistry(sdb)
	var asia *kernel.SessionDef
	for i := range reg.Sessions {
		if reg.Sessions[i].Name == "ASIA" {
			asia = &reg.Sessions[i]
			break
		}
	}
	tradeDate := now.In(loc).Format("2006-01-02")
	if asia != nil {
		tradeDate, _ = kernel.PlanChainTradeDate(asia, now)
	}
	as := assemble(sdb, "ASIA", tradeDate, now, 0)
	raw, err := os.ReadFile(dressEvDir + "/e2_ai_response_raw.json")
	must(err)
	log.p("loaded raw from e2 (%d bytes) — the SAME response the live call produced; no new call", len(raw))
	processSessionResponse(log, string(raw), as, "replay", now)
	log.write(dressEvDir + "/e4_validator_log.txt")
}

// processSessionResponse runs the REAL parse + write-path validator chain and
// emits e3 + the desk-review data + the e4 log. Post-parse rejects are
// RECORDED (the live write path would retry up to 2 more times with the reason
// appended, trader/auto_trader_planner.go:1146); a schema reject (no doc) is a
// hard stop.
func processSessionResponse(log *evLog, raw string, as *assembled, mode string, now time.Time) {
	log.p("── validator chain start (mode=%s) ──", mode)
	var rejects []string
	maxLevels, scenarioCap := as.in.MaxLevels, as.in.ScenarioCap
	sideQuota := as.sideQuota
	doc, perr := kernel.ParsePlanDocCapped(raw, maxLevels, scenarioCap)
	if perr != nil {
		log.p("ParsePlanDocCapped (schema gate incl. 9-condition enum): REJECTED: %v", perr)
		os.Exit(2)
	}
	log.p("ParsePlanDocCapped (schema gate incl. 9-condition enum): ACCEPTED (levels %d ≤ %d · scenarios %d)",
		len(doc.Levels), maxLevels, len(doc.Scenarios))
	if collapsed, n := kernel.CollapsePlanLevels(doc.Levels, kernel.LevelClusterTicks*0.25); n > 0 {
		doc.Levels = collapsed
		log.p("CollapsePlanLevels: merged %d near-duplicate level(s) (tolerance %d ticks × 0.25)", n, kernel.LevelClusterTicks)
	} else {
		log.p("CollapsePlanLevels: nothing to merge (tolerance %d ticks × 0.25)", kernel.LevelClusterTicks)
	}
	if mis := kernel.MislabeledStructuralLevels(doc, as.machineLabels); len(mis) > 0 {
		log.p("MislabeledStructuralLevels: REJECT %v", mis)
		rejects = append(rejects, "MislabeledStructuralLevels: "+strings.Join(mis, "; "))
	} else {
		log.p("MislabeledStructuralLevels: clean")
	}
	thin, verr := kernel.ValidatePlanDocWithFactsMachine(doc, as.facts, as.machineLabels, sideQuota, maxLevels, scenarioCap)
	if verr != nil {
		log.p("ValidatePlanDocWithFactsMachine: REJECTED: %v", verr)
		rejects = append(rejects, "ValidatePlanDocWithFactsMachine: "+verr.Error())
	} else {
		log.p("ValidatePlanDocWithFactsMachine: ACCEPTED (sideQuota=%d, thin-side notes: %v)", sideQuota, thin)
	}
	atr5m := as.atr5m
	for _, w := range kernel.ArmFeasibilityWarnings(doc, atr5m, 2.0, kernel.MinSLATRMult()) {
		log.p("arm feasibility WARN: %s", w)
	}
	if kernel.HasFvgScenario(doc) {
		origin := map[string]bool{}
		for _, lbl := range as.machineLabels {
			origin[lbl] = true
		}
		if verr := kernel.ValidateFvgEntryScenarios(doc, as.bars1m, symbol, origin, now); verr != nil {
			log.p("ValidateFvgEntryScenarios: REJECTED: %v", verr)
			rejects = append(rejects, "ValidateFvgEntryScenarios: "+verr.Error())
		} else {
			log.p("ValidateFvgEntryScenarios: ACCEPTED")
		}
	} else {
		log.p("ValidateFvgEntryScenarios: n/a (no fvg_entry scenario)")
	}
	if kernel.HasBreakdownScenario(doc) {
		if verr := kernel.ValidateBreakdownContinueScenarios(doc, as.bars1m, atr5m, as.facts.Price, now.UnixMilli()); verr != nil {
			log.p("ValidateBreakdownContinueScenarios: REJECTED: %v", verr)
			rejects = append(rejects, "ValidateBreakdownContinueScenarios: "+verr.Error())
		} else {
			log.p("ValidateBreakdownContinueScenarios: ACCEPTED")
		}
	} else {
		log.p("ValidateBreakdownContinueScenarios: n/a (no waterfall scenario)")
	}
	for _, m := range kernel.RoleMismatches(doc) {
		log.p("role mismatch WARN: %s", m)
	}
	for _, m := range kernel.ChainWarnings(*doc) {
		log.p("chain WARN: %s", m)
	}
	for _, m := range kernel.FantasyTargetWarnings(*doc) {
		log.p("fantasy-target WARN: %s", m)
	}
	for _, m := range kernel.FvgDemandWarnings(*doc, as.in.FreshFVGs) {
		log.p("fvg-demand WARN: %s", m)
	}

	if strings.Contains(strings.ToLower(doc.Reasoning), "bias-tree") {
		log.p("bias-tree branch: CITED ✓ — %s", firstWords(doc.Reasoning, 220))
	} else {
		log.p("bias-tree branch: NOT CITED ✗ (contract violation)")
	}
	cited := 0
	for _, s := range doc.Scenarios {
		for _, line := range strings.Split(s.Trigger+"\n"+s.Invalid, "\n") {
			if strings.Contains(strings.ToLower(line), "per candles") {
				cited++
			}
		}
	}
	log.p(`"per candles" citation present in %d scenario line(s) — %s`, cited,
		map[bool]string{true: "candle-grounded ✓", false: "ABSENT (honest report)"}[cited > 0])

	// ── e3: canonical post-parse doc + write-path verdict envelope ──────────
	if len(rejects) > 0 {
		log.p("WRITE-PATH VERDICT: REJECTED (%d reject reason(s)) — the LIVE write path would retry up to 2 more times on the SAME prompt (the session path LOGS the reason, it does not append it — trader/auto_trader_planner.go:1146), then fail-closes to the NO-TRADE marker (:1386-1389); this rehearsal made NO retry (hard budget: 2 real calls total)", len(rejects))
		env := map[string]any{
			"status":         "REJECTED_AT_WRITE_PATH",
			"note":           "single-call dress rehearsal — the live write path would retry up to 2 more times on the same prompt (session path logs the reason, never appends it — only the weekly path appends); no retry was made here (hard budget: 2 real calls total)",
			"reject_reasons": rejects,
			"doc":            doc,
		}
		b, _ := json.MarshalIndent(env, "", "  ")
		must(os.WriteFile(dressEvDir+"/e3_plan_parsed.json", b, 0o600))
		log.p("e3 written: REJECTED envelope + parsed doc (%d bytes)", len(b))
	} else {
		log.p("WRITE-PATH VERDICT: ACCEPTED (all validators clean)")
		b, _ := json.MarshalIndent(doc, "", "  ")
		must(os.WriteFile(dressEvDir+"/e3_plan_parsed.json", b, 0o600))
		log.p("e3 written: canonical post-parse/post-gate doc (%d bytes)", len(b))
	}

	minRR, minConf := 3.0, 60
	if as.sc != nil {
		minRR, minConf = as.sc.RiskControl.MinRiskRewardRatio, as.sc.RiskControl.MinConfidence
	}
	loc := kernel.CTLocation()
	dumpDeskReviewData(log, doc, as, atr5m, loc, now, minRR, minConf, len(rejects) > 0)
}

func resolvePlanCfgMinGrade(dp *store.DayPlanConfig, session string) string {
	if dp == nil {
		return ""
	}
	for _, so := range dp.Sessions {
		if so.Session == session && so.MinGrade != nil {
			return *so.MinGrade
		}
	}
	return ""
}

// dumpDeskReviewData prints every recompute e5 needs: level truth, stop sanity,
// target realism, R:R, condition legality, bias, candles, gate prediction.
func dumpDeskReviewData(log *evLog, doc *kernel.PlanDoc, as *assembled, atr5m float64, loc *time.Location, now time.Time, minRR float64, minConf int, writeRejected bool) {
	tick := 0.25
	band := 1.5 * as.dATR
	minSL := kernel.MinSLATRMult() * atr5m
	allSets := append([]kernel.ScoredLevel{}, as.pool...)
	allSets = append(allSets, as.seated...)
	allSets = append(allSets, as.in.HTFZones...)
	allSets = append(allSets, as.in.HTFZonesFull...)

	log.p("\n════ DESK-REVIEW DATA ════")
	log.p("context: price=%.2f dATR=%.1f ATR5m=%.2f tick=%.2f proximity_band=±%.1f pts minSL=%.2f cluster_tol=%.2f pts (12 ticks)",
		as.price, as.dATR, atr5m, tick, band, minSL, float64(kernel.LevelClusterTicks)*tick)
	log.p("structure: %v", as.in.StructureSummary)
	log.p("bias-ctx line: %s", as.in.BiasCtx)
	log.p("facts: PDH=%.2f PDL=%.2f PDC(biasCtx)=%.2f price=%.2f", as.facts.PDH, as.facts.PDL, as.in.BiasCtxFacts.PDC, as.facts.Price)

	// weekly refs for target cross-check (no AI — pure compute)
	wf := kernel.ComputeWeeklyFacts(as.bars1m, now, as.price)
	log.p("weekly refs (compute only): PWH=%.2f PWL=%.2f PWC=%.2f weekly_open=%.2f thin=%v completed_weeks=%d",
		wf.Refs.PWH, wf.Refs.PWL, wf.Refs.PWC, wf.Refs.WeeklyOpen, wf.ThinHistory, len(wf.Weeks))

	log.p("\n── 2.1 LEVEL TRUTH (doc row → real map object) ──")
	for _, l := range doc.Levels {
		k := math.Round(l.Price*100) / 100
		mGrade, hasG := as.machineGrades[k]
		mLabel, hasL := as.machineLabels[k]
		n, d, src := nearestInPool(l.Price, allSets)
		verdict := "FLAG-HALLUCINATED"
		if hasG {
			verdict = fmt.Sprintf("EXACT (machine row @%.2f %s/%s)", k, mLabel, mGrade)
		} else if n != nil && d <= 0.011 {
			verdict = fmt.Sprintf("NEAR (no exact machine row; nearest %.2f %s d=%.3f)", n.Price, n.Label, d)
		}
		dPts, dATR := 0.0, 0.0
		if n != nil {
			dPts = l.Price - n.Price
			if atr5m > 0 {
				dATR = dPts / atr5m
			}
		}
		log.p("  %-10.2f %-16s grade=%s machine_grade=%q | exact-key=%v label=%v | nearest=[%s] %-10.2f kind=%-9s grade=%s fresh=%-6q conf=%d score=%.3f | dist_pt=%+.2f (%.2f×ATR5m) | %s",
			l.Price, l.Label, l.Grade, l.MachineGrade, hasG, hasL,
			src, n.Price, n.Kind, n.Grade, n.Fresh, n.Confluence, n.Score, dPts, dATR, verdict)
	}
	log.p("machine row count: grades=%d labels=%d pool=%d seated=%d htfzones=%d htfzonesfull=%d",
		len(as.machineGrades), len(as.machineLabels), len(as.pool), len(as.seated),
		len(as.in.HTFZones), len(as.in.HTFZonesFull))

	log.p("\n── 2.2 STOP SANITY (arm scenarios) ──")
	for _, s := range doc.Scenarios {
		if s.Arm == nil || !s.Arm.Enabled {
			continue
		}
		m := recomputeArm(s.Arm, s.Direction)
		stop := s.Arm.Stop
		n, d, _ := nearestInPool(stop, allSets)
		behind := "naked in noise"
		if n != nil && d <= 3.0 {
			behind = fmt.Sprintf("near structure %s %.2f (kind=%s, %.1f pts away)", n.Label, n.Price, n.Kind, d)
		}
		log.p("  %s stop=%.2f | stopDist=%.2f (≥minSL %.2f? %v) | %s | tape: %s",
			s.ID, stop, m.dist, minSL, m.dist+1e-9 >= minSL, behind, fridayTapeTag(as.bars1m, stop, loc))
	}

	log.p("\n── 2.3 TARGET REALISM (each scenario target vs real pools) ──")
	profRows := dumpSessionProfiles()
	log.p("  last session profiles (session_date/poc/vah/val): %s", profRows)
	for _, s := range doc.Scenarios {
		targets := s.TargetChain
		if s.Arm != nil && s.Arm.Enabled {
			targets = append(targets, s.Arm.Target)
		}
		for _, t := range targets {
			n, d, _ := nearestInPool(t, allSets)
			dPts := 0.0
			dATR5 := 0.0
			desc := "VACUUM (no pool row within 3 pts)"
			if n != nil {
				dPts = t - n.Price
				if atr5m > 0 {
					dATR5 = dPts / atr5m
				}
				if d <= 3.0 {
					desc = fmt.Sprintf("AT pool %s %.2f (kind=%s grade=%s)", n.Label, n.Price, n.Kind, n.Grade)
				} else {
					desc = fmt.Sprintf("nearest pool %s %.2f %.1f pts away (kind=%s)", n.Label, n.Price, d, n.Kind)
				}
			}
			fromPrice := t - as.price
			reach := fmt.Sprintf("%+.1f pts from price (%.2f×dATR; band=±%.1f pts → %s)", fromPrice, fromPrice/as.dATR, band,
				map[bool]string{true: "session-reachable", false: "OUTSIDE band"}[math.Abs(fromPrice) <= band])
			log.p("  %s target=%.2f | %s | dist_vs_pool=%+.2f pts (%.2f×ATR5m) | %s", s.ID, t, desc, dPts, dATR5, reach)
		}
	}

	log.p("\n── 2.4 R:R RECOMPUTE (independent; 2dp) ──")
	for _, s := range doc.Scenarios {
		if s.Arm != nil && s.Arm.Enabled {
			m := recomputeArm(s.Arm, s.Direction)
			log.p("  %s arm entry=%.2f stop=%.2f target=%.2f | implied R:R=%.2f (model stated none — prices only) | arm-floor 2.0? %v | decision-floor %.2f? %v | wellFormed=%v",
				s.ID, s.Arm.Entry, s.Arm.Stop, s.Arm.Target, m.rr, m.rr+1e-9 >= 2.0, minRR, m.rr+1e-9 >= minRR, m.wellFormed)
		} else {
			log.p("  %s no-arm (AI path) — R:R decided by the executor at decision time; no model-stated R:R in the doc", s.ID)
		}
	}

	log.p("\n── 2.5 CONDITION LEGALITY (verbatim scenario objects) ──")
	for _, s := range doc.Scenarios {
		log.p("  %s: condition=%q direction=%s quality=%q armable=%v", s.ID, s.Condition, s.Direction, s.Quality, kernel.ArmableCondition(s.Condition))
		log.p("      trigger: %q", s.Trigger)
		log.p("      invalid: %q", s.Invalid)
		if s.Confirm != nil {
			log.p("      confirm: rule=%q ref_price=%.2f side=%q", s.Confirm.Rule, s.Confirm.RefPrice, s.Confirm.Side)
		}
		if s.Confirm2 != nil {
			log.p("      confirm2: rule=%q ref_price=%.2f side=%q", s.Confirm2.Rule, s.Confirm2.RefPrice, s.Confirm2.Side)
		}
		if s.Fvg != nil {
			j, _ := json.Marshal(s.Fvg)
			log.p("      fvg: %s", j)
		}
		if s.Breakdown != nil {
			j, _ := json.Marshal(s.Breakdown)
			log.p("      breakdown: %s", j)
			st := kernel.BreakdownContinueState(s, as.bars1m, 0, now.UnixMilli())
			log.p("      breakdown machine state: leg1=%v leg2=%v reclaimed=%v measured_disp=%.2f pts last_close=%.2f",
				st.Leg1Met, st.Leg2Met, st.Reclaimed, st.BreakLegPts, st.LastClose)
		}
		if s.Arm != nil {
			j, _ := json.Marshal(s.Arm)
			log.p("      arm: %s", j)
			if err := kernel.ArmSpecValid(s); err != nil {
				log.p("      ArmSpecValid: REJECTED: %v", err)
			} else {
				log.p("      ArmSpecValid: OK")
			}
		}
	}

	log.p("\n── 2.6 BIAS (facts vs claims) ──")
	log.p("  bias: %s / %s · flip_condition: %q", doc.Bias.Direction, doc.Bias.Conviction, doc.Bias.FlipCondition)
	log.p("  reasoning (full): %s", doc.Reasoning)
	log.p("  day_type=%q · death_condition=%q", doc.DayType, doc.DeathCondition)

	log.p("\n── 2.7 CANDLE USE ──")
	ct := 0
	for _, s := range doc.Scenarios {
		for _, line := range strings.Split(s.Trigger+"\n"+s.Invalid, "\n") {
			if strings.Contains(strings.ToLower(line), "candle") {
				ct++
				log.p("  candle-grounded line (%s): %q", s.ID, line)
			}
		}
	}
	log.p("  scenario lines mentioning candles: %d", ct)

	log.p("\n── 2.8 GATE PREDICTION (per scenario, plan-time computable) ──")
	mode := kernel.HTFVetoMode()
	trend1h, trend4h := trendOf(as.in.StructureSummary, "1h"), trendOf(as.in.StructureSummary, "4h")
	log.p("  veto mode=%q · 1h=%s · 4h=%s · decision floor minRR=%.2f minConf=%d (conf is executor-time, plan-time N/A)",
		mode, trend1h, trend4h, minRR, minConf)
	if writeRejected {
		log.p("  OVERRIDE: the write path REJECTED this plan (see the chain verdicts above) — tradeable %% = 0 regardless of per-scenario math; the desk review proceeds for the record")
	}
	passCount := 0
	for _, s := range doc.Scenarios {
		veto1h := vetoOpposesLocal(s.Direction, trend1h)
		veto4h := vetoOpposesLocal(s.Direction, trend4h)
		cross := veto1h && veto4h
		veto := false
		switch mode {
		case "cross":
			veto = cross
		case "4h":
			veto = veto4h
		default:
			veto = veto1h
		}
		m := recomputeArm(s.Arm, s.Direction)
		armOK := true
		if s.Arm != nil && s.Arm.Enabled {
			armOK = m.wellFormed && m.rr+1e-9 >= 2.0 && m.dist+1e-9 >= minSL
		}
		pass := !veto && armOK
		if pass {
			passCount++
		}
		log.p("  %s %s/%s: veto1h=%v veto4h=%v cross_veto=%v veto_applied=%v | arm_ok=%v | plan-time tradeable=%v",
			s.ID, s.Condition, s.Direction, veto1h, veto4h, cross, veto, armOK, pass)
	}
	log.p("  plan-time tradeable: %d/%d scenarios (%.0f%%) — confidence is the executor's at decision time; NOT predictable at plan time",
		passCount, len(doc.Scenarios), float64(passCount)/float64(len(doc.Scenarios))*100)
	if writeRejected {
		log.p("  FINAL tradeable: 0%% — the plan never reaches the executor (write-path reject)")
	}

	// full machine rows (bounded) for the reviewer's cross-checks
	log.p("\n── MACHINE ROWS (seated) ──")
	sort.Slice(as.seated, func(i, j int) bool { return as.seated[i].Distance > as.seated[j].Distance })
	for _, l := range as.seated {
		log.p("  %-10.2f kind=%-9s label=%-16s grade=%s fresh=%-6q conf=%d score=%.3f dist=%+.1f role=%s",
			l.Price, l.Kind, l.Label, l.Grade, l.Fresh, l.Confluence, l.Score, l.Distance, l.Role)
	}
}

// dumpSessionProfiles returns the last 5 session profile rows as a compact string.
func dumpSessionProfiles() string {
	db := openRO(sandboxDBPath)
	defer db.Close()
	rows, err := db.Query(`SELECT session_date, poc, vah, val FROM session_profiles WHERE symbol=? ORDER BY session_date DESC LIMIT 5`, symbol)
	if err != nil {
		return fmt.Sprintf("ERR:%v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		var poc, vah, val float64
		if err := rows.Scan(&d, &poc, &vah, &val); err == nil {
			out = append(out, fmt.Sprintf("%s{poc=%.2f vah=%.2f val=%.2f}", d, poc, vah, val))
		}
	}
	return strings.Join(out, " · ")
}
