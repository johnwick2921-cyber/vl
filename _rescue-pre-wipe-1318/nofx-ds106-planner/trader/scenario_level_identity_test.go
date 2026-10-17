package trader

import (
	"encoding/json"
	"nofx/kernel"
	"nofx/market"
	"nofx/store"
	"strings"
	"testing"
	"time"
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
			version, lc, err := at.runPlannerReadCoreObserved(func() time.Time { return now }, nil, "NY", "2026-09-10", "", "fixture", "hash", "", "", "", "fixture", kernel.PlanFacts{IdentityMap: candidates}, nil, nil, nil, true, func(string) (string, error) { calls++; return raw, nil })
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
