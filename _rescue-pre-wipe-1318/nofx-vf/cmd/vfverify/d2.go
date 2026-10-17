package main

// d2.go — DRESS REHEARSAL (2026-08-30): WEEKLY READ step.
//
// Sandbox-only: facts computed from SCRATCH bars, ONE real DeepSeek call,
// the REAL validator chain (kernel.ParseWeeklyDoc + kernel.ValidateWeeklyDoc
// with the retry-once semantics of trader/auto_trader_weekly.go:178-196), and
// the doc row written to the SCRATCH plans table via the REAL store path
// (store.Plan AppendPlan — the same columns the Sunday scheduler writes).
// The LIVE DB is never opened by this step, not even read-only.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"nofx/kernel"
	"nofx/mcp"
	_ "nofx/mcp/provider" // registers deepseek
	"nofx/store"
)

// weeklySystemPrompt — the EXACT scheduler system prompt
// (trader/auto_trader_weekly.go:26 const weeklySystemPrompt).
const weeklySystemPrompt = "You are a disciplined CME index-futures weekly-bias reasoner. Output ONLY the single JSON object requested — no prose outside the JSON."

func runD2() {
	log := &evLog{}
	log.p("════════ d2 DRESS REHEARSAL — WEEKLY READ STEP (2026-08-30) ════════")
	log.p("scratch DB: %s (mode=rw — the ONE write goes here, never the live DB)", sandboxDBPath)

	sdb := openRW(sandboxDBPath)
	defer sdb.Close()
	log.p("scratch pre-write: plans=%d · WEEKLY rows=%d",
		qint(sdb, `SELECT COUNT(*) FROM plans`),
		qint(sdb, `SELECT COUNT(*) FROM plans WHERE session='WEEKLY'`))

	now := time.Now()
	loc := kernel.CTLocation()
	log.p("now CT=%s · now UTC=%s", now.In(loc).Format(time.RFC3339), now.UTC().Format(time.RFC3339))

	// W-1: the governing week MUST be 2026-08-31 (kernel.WeekGoverningMonday).
	mondayT := kernel.WeekGoverningMonday(now)
	monday := mondayT.Format("2006-01-02")
	wd, hr, mn := kernel.WeeklyReadSpec()
	deadline := kernel.WeeklyReadDeadline(now)
	log.p("WeekGoverningMonday(now)=%s (kernel/weekly_knobs.go:58) — MUST be 2026-08-31: %v", monday, monday == "2026-08-31")
	log.p("WeeklyReadSpec: %s %02d:%02d CT · WeeklyReadDeadline=%s CT (now vs deadline: %v)",
		wd.String(), hr, mn, deadline.In(loc).Format(time.RFC3339), now.Before(deadline))
	if monday != "2026-08-31" {
		log.p("FATAL: governing week is not 2026-08-31 — stopping")
		log.write(dressEvDir + "/e11_weekly_validator.txt")
		os.Exit(2)
	}

	// Bars from the SCRATCH table — the same read the scheduler makes
	// (trader/auto_trader_weekly.go:71 weeklyBars1m).
	bs := newBarStore(sdb, 0)
	bars1m := bs.bars1m
	log.p("scratch 1m bars: %d rows (first %s CT · last %s CT)",
		len(bars1m),
		time.UnixMilli(bars1m[0].OpenTime).In(loc).Format("01-02 15:04"),
		time.UnixMilli(bars1m[len(bars1m)-1].OpenTime).In(loc).Format("01-02 15:04"))
	if len(bars1m) == 0 {
		log.p("FATAL: no stored 1m bars in scratch — stopping (thin/cold store)")
		log.write(dressEvDir + "/e11_weekly_validator.txt")
		os.Exit(2)
	}
	price := bars1m[len(bars1m)-1].Close
	facts := kernel.ComputeWeeklyFacts(bars1m, now, price)
	log.p("facts: completed_weeks=%d (expect 2) · thin_history=%v · price=%.2f · facts_hash=%s…",
		len(facts.Weeks), facts.ThinHistory, facts.Price, facts.FactsHash[:16])
	log.p("facts sections:\n%s", facts.SectionsText)
	log.p("WeeklyRefSet: %d draw-eligible references: %v", len(kernel.WeeklyRefSet(facts)), kernel.WeeklyRefSet(facts))

	prompt := kernel.BuildWeeklyPrompt(facts)
	chars := len([]rune(prompt))
	footer := fmt.Sprintf("\n\n════ E8 FOOTER (dress-rehearsal-0830) ════\nchars=%d bytes=%d tokens(chars/4)=%d %%of65536=%.1f%%\nbuilt=%s CT from SCRATCH bars (%d rows) · builder=kernel.BuildWeeklyPrompt (kernel/weekly_prompt.go:276) — the SAME builder trader/auto_trader_weekly.go:163 calls\n",
		chars, len(prompt), chars/4, float64(chars)/65536*100,
		now.In(loc).Format(time.RFC3339), len(bars1m))
	must(os.WriteFile(dressEvDir+"/e8_weekly_prompt.txt", []byte(prompt+footer), 0o600))
	log.p("e8 written: %d chars ≈ %d tokens", chars, chars/4)

	// Decrypt the rotated key from the SCRATCH ai_models row.
	key, _ := decryptDeepSeekKey(sdb)
	fpOK := strings.HasPrefix(key, "sk-") && strings.HasSuffix(key, "4681")
	log.p("decrypted key fingerprint check (sk-e…4681): %v", fpOK)
	if !fpOK {
		log.p("FATAL: key fingerprint mismatch — stopping before any AI call")
		log.write(dressEvDir + "/e11_weekly_validator.txt")
		os.Exit(2)
	}

	client := mcp.NewAIClientByProvider("deepseek")
	if client == nil {
		log.p("FATAL: deepseek provider not registered")
		log.write(dressEvDir + "/e11_weekly_validator.txt")
		os.Exit(2)
	}
	mcp.ApplyThinking(client, "", "")
	client.SetAPIKey(key, "", "")
	restore := mcp.ApplyMaxTokens(client, aiPlanMax())
	defer restore()

	// Retry-once semantics — trader/auto_trader_weekly.go:178-196
	// (initial + ONE retry with the reject reason appended).
	var lastErr string
	var doc *kernel.WeeklyDoc
	accepted := false
	for attempt := 1; attempt <= 2 && !accepted; attempt++ {
		start := time.Now()
		raw, err := client.CallWithMessages(weeklySystemPrompt, prompt)
		elapsed := time.Since(start)
		finish := mcp.LastFinishReason(client)
		log.p("WEEKLY AI call attempt %d/2: err=%v · elapsed=%.1fs · raw bytes=%d · finish_reason=%q · resolved_model=%q",
			attempt, err, elapsed.Seconds(), len(raw), finish, client.ResolvedModel())
		if attempt == 1 {
			must(os.WriteFile(dressEvDir+"/e9_weekly_response_raw.json", []byte(raw), 0o600))
			log.p("e9 written: attempt-1 raw response untrimmed (%d bytes)", len(raw))
		}
		if err != nil {
			lastErr = err.Error()
			log.p("attempt %d/2 call failed: %v", attempt, err)
			continue
		}
		doc, err = kernel.ParseWeeklyDoc(raw)
		if err != nil {
			lastErr = err.Error()
			log.p("ParseWeeklyDoc attempt %d/2 REJECTED: %v", attempt, err)
			continue
		}
		log.p("ParseWeeklyDoc attempt %d/2: ACCEPTED (tolerant fence/prose extract, kernel/weekly_prompt.go:196)", attempt)
		reason := kernel.ValidateWeeklyDoc(doc, kernel.WeeklyRefSet(facts), facts.ThinHistory)
		if reason != "" {
			lastErr = reason
			log.p("ValidateWeeklyDoc attempt %d/2 REJECTED: %s — retrying once with the reason appended (scheduler semantics)", attempt, reason)
			prompt = prompt + "\n\nREJECTED by the validator — fix ONLY these and answer again:\n" + reason
			continue
		}
		log.p("ValidateWeeklyDoc attempt %d/2: ACCEPTED (r1-r6 clean)", attempt)
		accepted = true
	}
	if !accepted || doc == nil {
		log.p("FATAL: weekly doc never accepted after 2 attempts: %s — no row written (fail-closed, scheduler parity)", lastErr)
		log.write(dressEvDir + "/e11_weekly_validator.txt")
		os.Exit(2)
	}

	// ── independent r1-r6 recompute for the e11/e12 evidence ────────────────
	log.p("\n── independent r1-r6 recompute (evidence for e11/e12) ──")
	bias := strings.ToLower(strings.TrimSpace(doc.Bias))
	conv := strings.ToLower(strings.TrimSpace(doc.Conviction))
	r1ok := (bias == "bull" || bias == "bear" || bias == "neutral") &&
		(conv == "low" || conv == "med" || conv == "high")
	log.p("r1 enums: bias=%q conviction=%q → %v", bias, conv, r1ok)
	log.p("r2 invalidation: px=%.2f (>0? %v) basis=%q (non-empty? %v)",
		doc.Invalidation.Px, doc.Invalidation.Px > 0, doc.Invalidation.Basis, strings.TrimSpace(doc.Invalidation.Basis) != "")
	bestRef, bestDist := 0.0, 1e9
	for _, ref := range kernel.WeeklyRefSet(facts) {
		d := doc.Draw.Px - ref
		if d < 0 {
			d = -d
		}
		if d < bestDist {
			bestDist, bestRef = d, ref
		}
	}
	log.p("r3 draw: name=%q px=%.2f · nearest ref=%.2f distance=%.3f (±1 tick=0.25 → %v) · refs=%d",
		doc.Draw.Name, doc.Draw.Px, bestRef, bestDist, bestDist <= 0.25, len(kernel.WeeklyRefSet(facts)))
	log.p("r4 narrative lines: %d (≤3 → %v) · narrative=%q",
		strings.Count(strings.TrimSpace(doc.Narrative), "\n")+1,
		strings.Count(strings.TrimSpace(doc.Narrative), "\n")+1 <= 3, doc.Narrative)
	log.p("r5 day-of-week tokens (kernel.HasDayOfWeekTokens, kernel/weekly_bias.go:314): %v", kernel.HasDayOfWeekTokens(doc.Narrative))
	log.p("r6 thin-history depth guard: facts.ThinHistory=%v → doc.Conviction MUST be \"low\" — doc says %q → %v",
		facts.ThinHistory, doc.Conviction, !facts.ThinHistory || strings.EqualFold(strings.TrimSpace(doc.Conviction), "low"))
	log.p("weekly_levels (%d rows):", len(doc.WeeklyLevels))
	for _, l := range doc.WeeklyLevels {
		log.p("  %-14s %.2f", l.Name, l.Px)
	}

	// ── stamp + canonical JSON (e10) ────────────────────────────────────────
	doc.FactsHash = facts.FactsHash
	doc.ThinHistory = facts.ThinHistory
	docJSON, _ := json.MarshalIndent(doc, "", "  ")
	must(os.WriteFile(dressEvDir+"/e10_weekly_doc_parsed.json", docJSON, 0o600))
	log.p("e10 written: stamped canonical weekly doc (%d bytes)", len(docJSON))

	// ── the REAL write path: store.Plan AppendPlan → SCRATCH plans only ─────
	gdb, err := store.InitGorm(sandboxDBPath)
	must(err)
	ps := store.NewPlanStore(gdb)
	defer ps.Close()
	planID := ps.ResolvePlanID(monday, "WEEKLY", traderID)
	log.p("ResolvePlanID(%s, WEEKLY, %s) = %q (store/plan.go:98 — fresh trader-scoped id, no existing WEEKLY chain)", monday, traderID, planID)
	version, werr := ps.AppendPlan(&store.PlanDB{
		PlanID:        planID,
		StrategyID:    traderID,
		TradeDate:     monday,
		Session:       "WEEKLY",
		TriggerReason: "sunday_weekly_read",
		Lifecycle:     "active",
		ModelID:       client.ResolvedModel(),
		PromptHash:    facts.FactsHash,
		Doc:           string(docJSON),
	})
	if werr != nil {
		log.p("FATAL: AppendPlan failed on SCRATCH: %v", werr)
		log.write(dressEvDir + "/e11_weekly_validator.txt")
		os.Exit(2)
	}
	log.p("AppendPlan OK: version=%d · columns = the scheduler's exact set (trader/auto_trader_weekly.go:197-207)", version)

	// Verify the row in the SCRATCH plans table.
	log.p("\n── scratch row verification ──")
	if r, err := sdb.Query(`SELECT plan_id, version, trade_date, session, trigger_reason, lifecycle, model_id, substr(prompt_hash,1,16), length(doc), created_at FROM plans WHERE session='WEEKLY'`); err == nil {
		defer r.Close()
		for r.Next() {
			var pid string
			var v int
			var td, sess, trig, lc, mid, ph, created string
			var docLen int
			if r.Scan(&pid, &v, &td, &sess, &trig, &lc, &mid, &ph, &docLen, &created) == nil {
				log.p("  plan_id=%q version=%d trade_date=%s session=%s trigger=%q lifecycle=%s model=%q prompt_hash=%s… doc_len=%d created_at=%s", pid, v, td, sess, trig, lc, mid, ph, docLen, created)
			}
		}
	}
	log.p("scratch post-write: plans=%d · WEEKLY rows=%d (was 0)",
		qint(sdb, `SELECT COUNT(*) FROM plans`),
		qint(sdb, `SELECT COUNT(*) FROM plans WHERE session='WEEKLY'`))
	log.p("ISOLATION: this write touched ONLY %s — the LIVE DB and the live bot (PID 482741) were never opened by this process", sandboxDBPath)

	log.p("\n════ d2 COMPLETE — evidence e8/e9/e10 written; validator log → e11 ════")
	log.write(dressEvDir + "/e11_weekly_validator.txt")
}
