package trader

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/logger"
	"vl/market"
	"vl/store"
)

func identityTestLevel(now time.Time, price float64) kernel.DetectedLevel {
	closeMs := now.Add(-10 * time.Hour).UnixMilli()
	return kernel.DetectedLevel{Kind: kernel.KindPDL, Price: price, Lo: price, Hi: price, Label: "PDL", OriginDate: "2026-09-09", FormedAtMs: closeMs - 60000, FormedCloseMs: &closeMs, FormationTF: "1m", IdentitySymbol: "MNQ", FormationLookback: 900}
}

// W-EXEC-TRUTH W2 A3 (correction, 2026-09-23): the "named" mode used to name
// a level 5 pts from the evaluator anchor (15475 vs 15480) and assert it was
// WRITTEN with heuristic-disagreed=1. That was the defect: identity ≠ price is
// now REFUSED at write, and so is an id the frozen map does not carry
// ("unresolved"). "named" now names the level AT the anchor; the runtime
// observation half (a polled disagreement counts once per version) is
// unchanged and is exercised with an anchor the evaluator reports at runtime.
func TestIdentityE1AuthoringToEpisodeAndE3WarnProduction(t *testing.T) {
	now := time.Date(2026, 9, 10, 20, 0, 0, 0, kernel.CTLocation())
	// The fixture's first obstacle is the arm target itself: the seated map
	// here holds only the named level, so the A4 path from entry to target is
	// empty (A4: first_obstacle == arm target).
	plan := strings.Replace(validTraderPlanJSON, `"first_obstacle":{"price":15550,"level":"fixture reference","family":"reference","response":"pass_through"},"r_to_obstacle":7.0`,
		`"first_obstacle":{"price":15620,"level":"PDH","family":"pivot","response":"exit"},"r_to_obstacle":14.0`, 1)
	if plan == validTraderPlanJSON {
		t.Fatal("fixture mutation missed")
	}
	for _, mode := range []string{"named", "disagreed", "unnamed", "unresolved"} {
		t.Run(mode, func(t *testing.T) {
			at := plannerTestTrader(t)
			price := 15480.0
			if mode == "disagreed" {
				price = 15475 // 5 pts from the 15480 anchor: beyond the 3.00 tolerance
			}
			l := identityTestLevel(now, price)
			candidates := kernel.BuildMapCandidates([]kernel.ScoredLevel{{DetectedLevel: l, Grade: "A", Score: 1}}, 15480, 10, kernel.MapCandidateOpts{})
			if len(candidates) != 1 || candidates[0].ID == nil {
				t.Fatal("map did not expose identity")
			}
			id := *candidates[0].ID
			raw := plan
			if mode == "unresolved" {
				id = "not-on-map"
			}
			if mode != "unnamed" {
				raw = strings.Replace(raw, `"id": "S1"`, `"id": "S1", "level_id": "`+id+`"`, 1)
			}
			calls := 0
			version, lc, err := at.runPlannerReadCoreObserved(func() time.Time { return now }, nil, nil, "NY", "2026-09-10", "", "fixture", "hash", "", "", "", "fixture", kernel.PlanFacts{IdentityMap: candidates}, nil, nil, nil, true, 0, func(string) (string, error) { calls++; return raw, nil })
			if mode == "disagreed" || mode == "unresolved" {
				// REFUSED at write on every attempt → the existing fail-closed path.
				if err != nil || lc != "no_trade" || calls != 3 {
					t.Fatalf("%s must be refused at write and fail closed: v%d %s calls=%d %v", mode, version, lc, calls, err)
				}
				c, err := at.store.LevelIdentityCounts(at.id)
				if err != nil {
					t.Fatal(err)
				}
				if c.HeuristicDisagreed != 0 || c.Unresolved != 0 || c.Named != 0 {
					t.Fatalf("a refused scenario must never be recorded as published evidence: %+v", c)
				}
				return
			}
			if err != nil || lc != "active" || version != 1 {
				t.Fatalf("WARN refused authoring: v%d %s %v", version, lc, err)
			}
			p, err := at.store.Plan().GetLatestPlanForTraderSession("2026-09-10", "NY", at.id)
			if err != nil {
				t.Fatal(err)
			}
			var doc kernel.PlanDoc
			if err = json.Unmarshal([]byte(p.Doc), &doc); err != nil {
				t.Fatal(err)
			}
			c, err := at.store.LevelIdentityCounts(at.id)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "unnamed" && c.Unnamed != 1 {
				t.Fatalf("WARN not recorded: %+v", c)
			}
			if mode != "named" {
				return
			}
			resolved, ok := kernel.LevelByID(doc.Scenarios[0].LevelID, doc.IdentityLevels)
			if !ok || resolved.Price != price || c.Named != 1 || c.HeuristicDisagreed != 0 {
				t.Fatalf("lost named evidence %+v %+v", resolved, c)
			}
			tape := oscillatingTape(price, now.Add(-9*time.Hour), 500)
			previous := market.FuturesBarsProvider
			market.FuturesBarsProvider = func(string, string, int) []market.Kline { return tape }
			defer func() { market.FuturesBarsProvider = previous }()
			// The legacy proximity answer points at S-other while the plan names S1.
			at.recordDetectorOutputs("MNQ", p.PlanID, "NY", version, []kernel.DetectedLevel{l}, []kernel.ScoredLevel{{DetectedLevel: l, Grade: "A", Score: 1}}, price, 100, 2, 12, now, []store.ScenarioAnchor{{ID: "S-other", Price: price}})
			rows, err := at.store.TouchOutcomes().AllOutcomes()
			if err != nil || len(rows) == 0 {
				t.Fatalf("no production episode %v", err)
			}
			for _, r := range rows {
				if r.LevelID == nil || *r.LevelID != id || r.ScenarioNearest == nil || *r.ScenarioNearest != "S-other" {
					t.Fatalf("row lost ID or corroboration %+v", r)
				}
				if sc := kernel.EpisodeScenarioByID(r.LevelID, &doc); sc == nil || *sc != "S1" {
					t.Fatal("episode authority fell back to heuristic")
				}
			}
			backfill, err := at.store.BackfillLevelIdentity(at.id)
			if err != nil || backfill.Recomputed != len(rows) || backfill.Unrecomputable != 0 {
				t.Fatalf("complete frozen inputs did not recompute %+v %v", backfill, err)
			}
			// RUNTIME observation is unchanged by A3 (write-time only): a
			// disagreement the evaluator reports while polling the same version
			// is one measured disagreement, not N loop ticks.
			for i := 0; i < 3; i++ {
				at.observeScenarioIdentity(&doc, p.PlanID, version, []kernel.ScenarioEval{{ID: "S1", Anchor: price + 10, HasAnchor: true}}, now)
			}
			c, _ = at.store.LevelIdentityCounts(at.id)
			if c.HeuristicDisagreed != 1 {
				t.Fatalf("counter inferred polling ticks %+v", c)
			}
		})
	}
}

func TestIdentityE6LegacyAndBootReads(t *testing.T) {
	at := plannerTestTrader(t)
	now := time.Date(2026, 9, 10, 20, 0, 0, 0, kernel.CTLocation())
	if _, err := at.store.Plan().AppendPlan(&store.PlanDB{PlanID: "legacy", StrategyID: at.id, TradeDate: "2026-09-10", Session: "NY", Doc: validTraderPlanJSON}); err != nil {
		t.Fatal(err)
	}
	row := store.TouchOutcomeRow{TraderID: at.id, Symbol: "MNQ", LevelKind: "PDL", CreatedAt: now.Add(-24 * time.Hour)}
	if err := at.store.TouchOutcomes().SaveOutcome(&row); err != nil {
		t.Fatal(err)
	}
	b, err := at.store.BackfillLevelIdentity(at.id)
	if err != nil || b.Untouched != 1 || b.Recomputed != 0 {
		t.Fatalf("legacy changed %+v %v", b, err)
	}
	var doc kernel.PlanDoc
	_ = json.Unmarshal([]byte(validTraderPlanJSON), &doc)
	r := kernel.ScenarioIdentities(&doc)["S1"]
	if r.LevelID != nil || r.Level != nil {
		t.Fatal("legacy identity guessed")
	}
	c := store.LevelIdentityCounts{Named: 7, Unnamed: 3, Unresolved: 2, HeuristicDisagreed: 1}
	text := levelIdentityBootLine(&doc, &c, &b)
	for _, want := range []string{"map ids=n/a", "named=7", "unnamed=3[WARN]", "unresolved=2[WARN]", "heuristic-disagreed=1", "untouched=1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("boot did not read %s: %s", want, text)
		}
	}
}

func TestIdentityRecordingPanicContained(t *testing.T) {
	at := plannerTestTrader(t)
	returned := false
	func() { defer at.containLevelIdentity(); panic("fixture recording failure") }()
	returned = true
	if !returned {
		t.Fatal("recording panic escaped")
	}
}

// TestLevelIdentityBootLineFoldsOverlay (WAVE 1a-plan P2) — the boot line's
// identity map reads the plan through the ONE fold: an owner overlay that adds
// an identity level must be visible in the boot line. RED on the base-only
// reader: map ids=0/1. GREEN: map ids=0/2.
func TestLevelIdentityBootLineFoldsOverlay(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	yes := true
	at := mkTrader("ninjatrader", &yes, "5m")
	at.store = st
	at.id = "trader-1"

	now := ctAt(t, 11, 0)
	tradeDate := plannerTradeDateCT(now)
	sessName := at.activeSessionName(now)

	base := kernel.PlanDoc{
		Reasoning:      "wave-1a-p2-identity",
		Bias:           kernel.PlanBias{Direction: "neutral"},
		DeathCondition: "flat",
		Scenarios: []kernel.PlanScenario{
			{ID: "S1", Condition: "reclaim", Direction: "long", Quality: "A"},
		},
		IdentityLevels: []kernel.PlanLevel{{Label: "ONH", Price: 30000, Grade: "A"}},
	}
	baseJSON, _ := json.Marshal(&base)
	planID := st.Plan().ResolvePlanID(tradeDate, sessName, at.id)
	_, err = st.Plan().AppendPlan(&store.PlanDB{
		PlanID: planID, StrategyID: at.id, TradeDate: tradeDate, Session: sessName,
		TriggerReason: "wave-1a-p2-identity", Lifecycle: "active",
		ModelID: "deepseek-v4-pro", PromptHash: "deadbeef", Doc: string(baseJSON),
	})
	if err != nil {
		t.Fatalf("append plan: %v", err)
	}
	_, err = st.Plan().AppendOverlay(&store.PlanOverlayDB{
		PlanID: planID, PlanVersion: 1, OverlayID: "owner-add-onl",
		Origin: "owner",
		Patch:  `[{"op":"add","path":"/identity_levels/-","value":{"label":"ONL","price":29900,"grade":"A"}}]`,
	})
	if err != nil {
		t.Fatalf("append overlay: %v", err)
	}

	var buf syncLogBuf
	// CLASS 142-adjacent (CTO gate RED, 2026-09-24): logger.Log.Writer() is NOT a
	// getter — it spawns a logrus writerScanner goroutine that logs every later
	// entry into an io.Pipe nobody reads. The pipe fills (~64KB), the scanner
	// blocks HOLDING the logrus mutex, and the NEXT test's first log call hangs
	// forever (the package wall named TestZoneAcceptedIdentitySkips...).
	// The restore value is Out; Writer() is a producer, never a capture.
	// FLAKE HUNT 2026-09-26: the capture is the MUTEXED syncLogBuf — a plain
	// strings.Builder String() read races any still-alive background goroutine
	// writing into the shared global Out under -race.
	old := logger.Log.Out
	logger.Log.SetOutput(&buf)
	t.Cleanup(func() { logger.Log.SetOutput(old) })
	at.logLevelIdentityBootAt(now)

	if !strings.Contains(buf.String(), "map ids=0/2 (no-formation=2)") {
		t.Fatalf("the boot line must read the folded identity map (0/2), got:\n%s", buf.String())
	}
}

// TestZoneAcceptedIdentitySkipsHeuristicDisagreement (WAVE 1a-plan P7, #190;
// skeptic F12 re-pinned on the KERNEL predicate) — a zone-accepted scenario
// disagrees with the evaluator BY DESIGN: the anchor sits inside the resolved
// level's [lo−tol, hi+tol]. The exemption is the kernel's zone-aware test
// (IdentityAgreesZoneAware — the write check's own predicate), never a
// label/type allowlist: an out-of-zone anchor records even under a
// Demand/Supply/OB label, and a plain FVG zone edge anchor is exempt.
func TestZoneAcceptedIdentitySkipsHeuristicDisagreement(t *testing.T) {
	now := time.Date(2026, 9, 10, 20, 0, 0, 0, kernel.CTLocation())
	price := 29897.0
	l := identityTestLevel(now, price)
	candidates := kernel.BuildMapCandidates([]kernel.ScoredLevel{{DetectedLevel: l, Grade: "A", Score: 1}}, price, 10, kernel.MapCandidateOpts{})
	if len(candidates) != 1 || candidates[0].ID == nil {
		t.Fatal("map did not expose identity")
	}
	id := *candidates[0].ID

	fresh := func(t *testing.T) (*AutoTrader, *store.Store) {
		yes := true
		at := mkTrader("ninjatrader", &yes, "5m")
		st, err := store.New(filepath.Join(t.TempDir(), "t.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { st.Close() })
		at.store = st
		at.id = "trader-1"
		return at, st
	}

	mkDoc := func() kernel.PlanDoc {
		return kernel.PlanDoc{
			Scenarios:      []kernel.PlanScenario{{ID: "S1", Condition: "zone_entry", Direction: "long", Quality: "A", LevelID: &id}},
			IdentityLevels: kernel.IdentityLevelsFromCandidates(candidates),
		}
	}

	// CONTROL — same levels, no zone acceptance: the disagreement MUST record.
	t.Run("plain_scenario_records", func(t *testing.T) {
		at, st := fresh(t)
		doc := mkDoc()
		at.observeScenarioIdentity(&doc, "p1", 1, []kernel.ScenarioEval{{ID: "S1", Anchor: price + 10, HasAnchor: true}}, now)
		c, err := st.LevelIdentityCounts(at.id)
		if err != nil {
			t.Fatal(err)
		}
		if c.HeuristicDisagreed != 1 {
			t.Fatalf("control: plain scenario must record 1 disagreement, got %d", c.HeuristicDisagreed)
		}
	})

	// FVG/zone case — a zone level (Lo..Hi) whose DISTAL edge is the anchor:
	// inside [lo−tol, hi+tol] by the KERNEL predicate → NEVER a disagreement.
	t.Run("fvg_zone_edge_skips", func(t *testing.T) {
		at, st := fresh(t)
		doc := mkDoc()
		lo, hi := price, price+10
		doc.IdentityLevels[0].Lo, doc.IdentityLevels[0].Hi = &lo, &hi
		doc.Scenarios[0].Fvg = &kernel.PlanFvgEntry{Lo: price, Hi: price + 10, Direction: "long"}
		at.observeScenarioIdentity(&doc, "p1", 1, []kernel.ScenarioEval{{ID: "S1", Anchor: price + 10, HasAnchor: true}}, now)
		c, err := st.LevelIdentityCounts(at.id)
		if err != nil {
			t.Fatal(err)
		}
		if c.HeuristicDisagreed != 0 {
			t.Fatalf("an edge anchor inside the level's zone must NOT record heuristic disagreement, got %d", c.HeuristicDisagreed)
		}
	})

	// Skeptic F12 (b) — the P7 LABEL allowlist's false negative: a line level
	// labelled Demand whose evaluator anchor sits OUTSIDE the zone is a real
	// disagreement and MUST record. RED = the label allowlist → 0 recorded.
	t.Run("demand_label_out_of_zone_records", func(t *testing.T) {
		at, st := fresh(t)
		doc := mkDoc()
		doc.IdentityLevels[0].Label = "Demand 29897"
		at.observeScenarioIdentity(&doc, "p1", 1, []kernel.ScenarioEval{{ID: "S1", Anchor: price + 10, HasAnchor: true}}, now)
		c, err := st.LevelIdentityCounts(at.id)
		if err != nil {
			t.Fatal(err)
		}
		if c.HeuristicDisagreed != 1 {
			t.Fatalf("an out-of-zone anchor must record even when the level is labelled Demand, got %d", c.HeuristicDisagreed)
		}
	})

	// Skeptic F12 (a) — the P7 label allowlist's false positive: a plain FVG
	// zone level (label "FVG", not S/D/OB) whose edge anchor is INSIDE the
	// zone must NOT record. RED = the allowlist → 1 recorded.
	t.Run("plain_fvg_label_edge_anchor_skips", func(t *testing.T) {
		at, st := fresh(t)
		doc := mkDoc()
		lo, hi := price, price+10
		doc.IdentityLevels[0].Lo, doc.IdentityLevels[0].Hi = &lo, &hi
		doc.IdentityLevels[0].Label = "FVG"
		// No sc.Fvg: a plain scenario naming the FVG zone level.
		at.observeScenarioIdentity(&doc, "p1", 1, []kernel.ScenarioEval{{ID: "S1", Anchor: price + 10, HasAnchor: true}}, now)
		c, err := st.LevelIdentityCounts(at.id)
		if err != nil {
			t.Fatal(err)
		}
		if c.HeuristicDisagreed != 0 {
			t.Fatalf("a zone-accepted edge anchor must NOT record for a plain FVG zone level, got %d", c.HeuristicDisagreed)
		}
	})
}

// TestLoggerCaptureNeverSpawnsAWriterScanner (CTO gate RED, 2026-09-24) — the
// capture pattern that took logger.Log.Writer() spawned a logrus writerScanner
// writing into an io.Pipe nobody read; once the pipe filled, the scanner blocked
// HOLDING the logrus mutex and the next test's first log call hung the whole
// package (the wall named TestZoneAcceptedIdentitySkipsHeuristicDisagreement).
// Pin: a capture that logs MORE than the 64KB pipe can hold must complete —
// bounded, and a leaked scanner fails here in 10s instead of hanging forever.
func TestLoggerCaptureNeverSpawnsAWriterScanner(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		var buf syncLogBuf
		old := logger.Log.Out
		logger.Log.SetOutput(&buf)
		defer logger.Log.SetOutput(old)
		big := strings.Repeat("x", 1024)
		for i := 0; i < 200; i++ { // 200KB > the 64KB pipe
			logger.Infof("%s %d", big, i)
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a leaked log writerScanner blocked logging — the capture pattern must use logger.Log.Out, never logger.Log.Writer()")
	}
}

// TestStampPlanIdentityRefAnchorLevelDoesNotPanic (FIX-LEVEL-IDENTITY-NIL) — a
// reference-anchor ("ref|") level has formed_close_ms NULL by construction
// (LevelByReferenceID: the formation close is unknown). stampPlanIdentity's
// "🪪 scenario" log line must render it as NULL, not dereference it — the
// pre-fix code panicked (contained) and lost the line. Named RED: revert the
// int64PtrText guard → the line panics, "recording panic contained" is emitted
// and "formed_close_ms=NULL" never prints.
func TestStampPlanIdentityRefAnchorLevelDoesNotPanic(t *testing.T) {
	refID := "ref|1c275fee1d9006ce39cf34c4a66b3385ea4ffa9f1c9f0f764382fc343a6a2861"
	kind, tf := "ONL", "1m"
	candidates := []kernel.MapCandidate{{
		ID:    &refID,
		Price: 31290,
		Identity: kernel.PlanLevel{
			ID: &refID, Kind: &kind, TF: &tf, Price: 31290, Label: "ONL",
			FormedCloseMs: nil, // reference-anchor: formation close unknown
		},
	}}
	doc := kernel.PlanDoc{
		Scenarios: []kernel.PlanScenario{{ID: "S1", LevelID: &refID}},
	}
	at := mkTrader("ninjatrader", nil, "5m")
	at.id = "t-fix"

	var buf syncLogBuf
	old := logger.Log.Out
	logger.Log.SetOutput(&buf)
	t.Cleanup(func() { logger.Log.SetOutput(old) })

	at.stampPlanIdentity(&doc, candidates)

	out := buf.String()
	if !strings.Contains(out, "🪪 scenario S1") || !strings.Contains(out, "formed_close_ms=NULL") {
		t.Fatalf("the scenario line must print formed_close_ms=NULL for a ref| level, got:\n%s", out)
	}
	if strings.Contains(out, "recording panic contained") {
		t.Fatalf("a ref| level must NOT panic the identity recorder, got:\n%s", out)
	}
}

// TestIdentityPtrTextHelpers — the nil-rendering helpers (int64PtrText /
// stringPtrText / float64PtrText / levelPriceText) print NULL for nil and the
// value otherwise, so the identity log lines never dereference an optional
// field.
func TestIdentityPtrTextHelpers(t *testing.T) {
	if int64PtrText(nil) != "NULL" {
		t.Fatalf("int64PtrText(nil) = %q, want NULL", int64PtrText(nil))
	}
	var i int64 = 123
	if int64PtrText(&i) != "123" {
		t.Fatalf("int64PtrText(123) = %q", int64PtrText(&i))
	}
	s := "1m"
	if stringPtrText(nil) != "NULL" || stringPtrText(&s) != "1m" {
		t.Fatalf("stringPtrText mismatch: nil=%q set=%q", stringPtrText(nil), stringPtrText(&s))
	}
	if float64PtrText(nil) != "NULL" {
		t.Fatalf("float64PtrText(nil) = %q, want NULL", float64PtrText(nil))
	}
	if levelPriceText(kernel.ScenarioIdentity{Level: nil}) != "NULL" {
		t.Fatalf("levelPriceText(nil level) must be NULL")
	}
	l := kernel.PlanLevel{Price: 31290}
	if levelPriceText(kernel.ScenarioIdentity{Level: &l}) != "31290.00" {
		t.Fatalf("levelPriceText(31290) = %q", levelPriceText(kernel.ScenarioIdentity{Level: &l}))
	}
}
