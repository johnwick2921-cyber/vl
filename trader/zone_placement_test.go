package trader

import (
	"context"
	"encoding/json"
	"math"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vl/discipline"
	"vl/kernel"
	"vl/market"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"

	"gorm.io/gorm"
)

// ── W-EXEC-TRUTH W3 — market_in_zone at the production call sites ──────────
//
// The rig is liveArmFixture's construction (a REAL TCP server + AddOn conn, a
// real store, the plan provider on a fixed clock, a whole-day TEST session)
// with the AddOn conn kept so the test can read every frame IN ORDER and
// barrier on a sentinel (a bars_history_request is ordered on the one conn
// after anything the producer already wrote — the parity wire's barrier).
// The tape is swappable under a lock so the event goroutine may read it.

const zoneSentinel = "W3-ZONE-SENTINEL"

type zoneFrame struct {
	sig      *ntwire.SignalPayload
	cancel   *ntwire.CancelOrderPayload
	sentinel string
}

type zoneRig struct {
	t    *testing.T
	at   *AutoTrader
	st   *store.Store
	srv  *ntwire.TCPServer
	conn net.Conn
	ev   chan zoneFrame
	seq  int
	now  time.Time
	pid  string

	mu   sync.Mutex
	bars []market.Kline
}

// zoneTape is an 80-bar 1m tape ending at now-1m whose LAST close is exactly
// last (lows/highs ±0.5). shift moves it (negative = older).
func zoneTape(last float64, now time.Time, shift time.Duration) []market.Kline {
	base := now.Add(-80 * time.Minute).Truncate(time.Minute).Add(shift).UnixMilli()
	out := make([]market.Kline, 0, 80)
	for i := 0; i < 80; i++ {
		cl := last - float64(79-i)*0.05
		o := base + int64(i)*60_000
		out = append(out, market.Kline{OpenTime: o, CloseTime: o + 59_999, Open: cl, High: cl + 0.5, Low: cl - 0.5, Close: cl})
	}
	return out
}

// zoneTapeReacted is zoneTape with ONE rejecting touch candle at now−10m (after
// the rig's plan birth at now−30m): it comes from ABOVE the 99.50 confirm ref
// (open 99.70), trades down to it (low 99.45) and CLOSES back above it (99.60)
// — the F-1 reaction a level play needs before its market_in_zone leg is
// authorized (kernel/touch_trigger.go). The last close is unchanged, so a tape
// that ends below the zone is "reacted, then slipped short of the zone".
func zoneTapeReacted(last float64, now time.Time) []market.Kline {
	out := zoneTape(last, now, 0)
	b := &out[70]
	b.Open, b.High, b.Low, b.Close = 99.7, 99.9, 99.45, 99.6
	return out
}

func (r *zoneRig) setTape(bars []market.Kline) {
	r.mu.Lock()
	r.bars = bars
	r.mu.Unlock()
}

// zoneScenario is liveArmFixture's reject long at 100 with a market_in_zone
// policy and the planner's own entry zone.
func zoneScenario(id string, policy string, zone []float64, waitConfirm bool) kernel.PlanScenario {
	sc := kernel.PlanScenario{ID: id, Trigger: "t", Condition: "reject", Direction: "long",
		TargetChain: []float64{110}, Invalid: "i", Quality: "B",
		// F-1 (planner-touch-fills): the confirm ref is the zone's support edge
		// 99.50 — a market_in_zone reject now waits for a touch candle that
		// CLOSES back above its ref, and the rig's rising tapes print that
		// reaction after birth (closes 99.55–100.00 with lows inside the zone).
		Confirm: &kernel.PlanConfirm{Rule: "touch", RefPrice: 99.5, Side: "above"},
		Arm:     &kernel.PlanArmSpec{Enabled: true, Entry: 100, Stop: 98, Target: 110, Policy: policy, WaitConfirm: waitConfirm}}
	if zone != nil {
		sc.Economics = &kernel.ScenarioEconomics{Version: 1, EntryZone: zone}
	}
	return sc
}

func zoneDoc(scs ...kernel.PlanScenario) kernel.PlanDoc {
	d := kernel.PlanDoc{Bias: kernel.PlanBias{Direction: "long", Conviction: "low", FlipCondition: "n/a"},
		Levels:    []kernel.PlanLevel{{Price: 100, Label: "PDH", Grade: "A", Instruction: "fade"}},
		Scenarios: scs, NoTrade: []string{}, DeathCondition: "n/a"}
	// The market_in_zone composition (non-structural, from the NEAR bound
	// 99.50): stop = min(authored 98, 99.50 − 1.5×ATR5m ≈ 97.70), so R:R at the
	// FAR bound 100.50 is ≈ 3.4 ≥ 2 and min-SL holds at the near bound. A
	// legacy reject still takes the structural path (stop 98.00 from the PDH
	// zone's lower edge − 0.5 buffer).
	structuralTestMap(&d, structuralTestZone{100, 98.5, 100, "PDH"}, structuralTestZone{110, 110, 111, "target"})
	return d
}

func newZoneRig(t *testing.T, id string, doc kernel.PlanDoc) *zoneRig {
	t.Helper()
	now := time.Date(2026, time.September, 11, 15, 0, 0, 0, time.UTC) // Thu 10:00 CT
	cfg := store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}
	oneSetupOff(&cfg)
	structuralTestPolicy(&cfg, .5)
	cfg.RiskControl.MinRiskRewardRatio = 2

	s := ntwire.NewTCPServer(nil)
	s.SetAddrForTest("127.0.0.1:0")
	s.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}}, "Sim101")
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatalf("server start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop(); cancel() })
	// W117 F4 — GetPositions is UNKNOWN until a snapshot or a confirmed fill
	// exists (never fabricated as flat). Production always has one: the AddOn
	// emits a positions frame on connect. The rig models that with the
	// known-flat seed so the one-contract guard reads a fresh empty book
	// instead of refusing the whole pass as unverifiable.
	s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{})
	conn, err := net.Dial("tcp", s.ListenAddrForTest().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	waitAddonRegistered(t, s)
	t.Cleanup(func() { _ = conn.Close() })
	ev := make(chan zoneFrame, 64)
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		for {
			env, err := ntwire.ReadFrame(conn)
			if err != nil {
				return
			}
			var f zoneFrame
			switch env.Type {
			case ntwire.FrameSignal:
				var p ntwire.SignalPayload
				if json.Unmarshal(env.Payload, &p) != nil {
					continue
				}
				f.sig = &p
			case ntwire.FrameCancelOrder:
				var p ntwire.CancelOrderPayload
				if json.Unmarshal(env.Payload, &p) != nil {
					continue
				}
				f.cancel = &p
			case ntwire.FrameBarsHistoryRequest:
				var p ntwire.BarsHistoryRequestPayload
				if json.Unmarshal(env.Payload, &p) != nil || p.Symbol != zoneSentinel {
					continue
				}
				f.sentinel = p.RequestID
			default:
				continue
			}
			select {
			case ev <- f:
			case <-done:
				return
			}
		}
	}()
	st, err := store.New(filepath.Join(t.TempDir(), "zone.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, now)
	at := &AutoTrader{id: id, exchange: "ninjatrader", store: st, trader: ntTrader.NewTCPTrader(s, "MNQ", "Sim101")}
	at.config.StrategyConfig = &cfg
	at.mcpClient = &fakeDecisionClient{}
	t.Cleanup(func() { kernel.SetTraderPlanProviders(id, kernel.TraderPlanProviders{}) })
	r := &zoneRig{t: t, at: at, st: st, srv: s, conn: conn, ev: ev, now: now}
	blob, _ := json.Marshal(doc)
	r.pid = shadowPlanAtTime(t, at, st, string(blob), now)
	r.setTape(zoneTape(101.95, now, 0))
	prev := market.FuturesBarsProvider
	market.FuturesBarsProvider = func(string, string, int) []market.Kline {
		r.mu.Lock()
		defer r.mu.Unlock()
		return append([]market.Kline(nil), r.bars...)
	}
	t.Cleanup(func() { market.FuturesBarsProvider = prev })
	return r
}

// drain returns every frame the producer wrote before now (sentinel barrier).
func (r *zoneRig) drain() (sigs []ntwire.SignalPayload, cancels []ntwire.CancelOrderPayload) {
	r.t.Helper()
	r.seq++
	id := zoneSentinel + "-" + strconv.Itoa(r.seq)
	if err := r.srv.SendBarsHistoryRequest(ntwire.BarsHistoryRequestPayload{RequestID: id, Symbol: zoneSentinel}); err != nil {
		r.t.Fatalf("sentinel: %v", err)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case f := <-r.ev:
			switch {
			case f.sig != nil:
				sigs = append(sigs, *f.sig)
			case f.cancel != nil:
				cancels = append(cancels, *f.cancel)
			case f.sentinel == id:
				return sigs, cancels
			}
		case <-deadline:
			r.t.Fatal("sentinel never came back")
			return nil, nil
		}
	}
}

// flatBook is the AddOn's periodic snapshot of an empty book at `at` (the
// one-contract guard refuses on a stale book by design).
func (r *zoneRig) flatBook(at time.Time) {
	r.srv.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, at)
}

func (r *zoneRig) rows() []store.ArmedOrderDB {
	r.t.Helper()
	rows, err := r.st.ArmedOrders().ListForPlan(r.pid)
	if err != nil {
		r.t.Fatal(err)
	}
	return rows
}

func (r *zoneRig) row(scenario string) store.ArmedOrderDB {
	r.t.Helper()
	var out store.ArmedOrderDB
	found := false
	for _, x := range r.rows() {
		if x.Scenario == scenario && (!found || x.ID > out.ID) {
			out, found = x, true
		}
	}
	if !found {
		r.t.Fatalf("no ledger row for %s: %+v", scenario, r.rows())
	}
	return out
}

func (r *zoneRig) armRefusals(class string) int {
	plan := kernel.ActivePlanFor(r.at.id, r.at.futuresSymbol())
	if plan == nil {
		r.t.Fatal("fixture: no active plan")
	}
	return store.ArmRefusalCount(r.st, r.at.id, kernel.PlanTradeDateFor(plan), plan.Session, class)
}

var zone = []float64{99.5, 100.5}

// ── the pure verdict ────────────────────────────────────────────────────────

func TestZonePlacementVerdictTable(t *testing.T) {
	now := time.Date(2026, 9, 11, 15, 0, 0, 0, time.UTC)
	fresh := now.Add(-time.Minute).UnixMilli()
	for _, c := range []struct {
		name   string
		price  float64
		lo, hi float64
		side   string
		barMs  int64
		want   zoneVerdict
	}{
		{"long at lo (inclusive)", 31000, 31000, 31010, "long", fresh, zoneInside},
		{"long at hi (inclusive)", 31010, 31000, 31010, "long", fresh, zoneInside},
		{"long mid", 31005, 31000, 31010, "LONG", fresh, zoneInside},
		{"long one tick above hi", 31010.25, 31000, 31010, "long", fresh, zoneBeyond},
		{"long one tick below lo", 30999.75, 31000, 31010, "long", fresh, zoneShortOfZone},
		{"short at lo (inclusive)", 31000, 31000, 31010, "short", fresh, zoneInside},
		{"short at hi (inclusive)", 31010, 31000, 31010, "SHORT", fresh, zoneInside},
		{"short one tick below lo", 30999.75, 31000, 31010, "short", fresh, zoneBeyond},
		{"short one tick above hi", 31010.25, 31000, 31010, "short", fresh, zoneShortOfZone},
		{"zero price", 0, 31000, 31010, "long", fresh, zoneUnknown},
		{"negative price", -1, 31000, 31010, "long", fresh, zoneUnknown},
		{"NaN price", math.NaN(), 31000, 31010, "long", fresh, zoneUnknown},
		{"no zone", 31005, 0, 0, "long", fresh, zoneUnknown},
		{"inverted zone", 31005, 31010, 31000, "long", fresh, zoneUnknown},
		{"bad side", 31005, 31000, 31010, "flat", fresh, zoneUnknown},
		{"no bar", 31005, 31000, 31010, "long", 0, zoneUnknown},
		{"bar opened exactly 3m ago (not stale)", 31005, 31000, 31010, "long", now.Add(-3 * time.Minute).UnixMilli(), zoneInside},
		{"bar opened 3m+1ms ago (stale)", 31005, 31000, 31010, "long", now.Add(-3*time.Minute - time.Millisecond).UnixMilli(), zoneUnknown},
	} {
		if got := zonePlacementVerdict(c.price, c.lo, c.hi, c.side, c.barMs, now); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
	if zoneVerdict(0) != zoneUnknown || zoneVerdict(0).String() != "unknown" {
		t.Fatal("UNKNOWN must be the iota zero — an unset verdict can never place")
	}
}

func TestArmPolicyConstMirrorsKernel(t *testing.T) {
	if store.ArmPolicyMarketInZone != kernel.EntryPolicyMarketInZone {
		t.Fatalf("store mirror %q != kernel %q", store.ArmPolicyMarketInZone, kernel.EntryPolicyMarketInZone)
	}
}

// ── the call site ───────────────────────────────────────────────────────────

// inside and beyond: exactly ONE limit frame at the FAR bound, and the row is
// place_pending with the evidence the verdict read.
func TestZoneRowPlacesOneLimitAtTheFarBound(t *testing.T) {
	for _, c := range []struct {
		name string
		last float64
		want string
	}{{"inside", 100.0, "inside"}, {"beyond", 101.95, "beyond"}} {
		t.Run(c.name, func(t *testing.T) {
			r := newZoneRig(t, "w3-zone-"+c.name, zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
			r.setTape(zoneTape(c.last, r.now, 0))
			r.at.maybeManageArmedOrdersAt(nil, r.now)
			sigs, _ := r.drain()
			if len(sigs) != 1 {
				t.Fatalf("want exactly 1 signal frame, got %d: %+v", len(sigs), sigs)
			}
			s := sigs[0]
			if s.OrderType != "limit" || s.LimitPrice != 100.5 || s.Entry != 100.5 || !strings.EqualFold(s.Side, "long") {
				t.Fatalf("the limit must sit at the FAR bound 100.50: %+v", s)
			}
			row := r.row("S1")
			if row.State != store.StatePlacePending || row.SignalID != s.SignalID || row.Policy != kernel.EntryPolicyMarketInZone {
				t.Fatalf("row must be place_pending under the sent signal with the policy: %+v", row)
			}
			if row.EntryPx != 100.5 || row.ZoneLo == nil || *row.ZoneLo != 99.5 || *row.ZoneHi != 100.5 || row.PlannedEntryPx == nil || *row.PlannedEntryPx != 100 {
				t.Fatalf("composer stamp wrong (entry=far, zone inward, planned=authored): %+v", row)
			}
			tape := zoneTape(c.last, r.now, 0)
			if row.EvalPrice == nil || *row.EvalPrice != c.last || row.EvalBarMs == nil || *row.EvalBarMs != tape[len(tape)-1].OpenTime ||
				row.PlacedAtMs == nil || *row.PlacedAtMs != r.now.UnixMilli() {
				t.Fatalf("placement evidence (the evaluation that placed it): eval=%v bar=%v placed=%v", row.EvalPrice, row.EvalBarMs, row.PlacedAtMs)
			}
			if row.FilledAtMs != nil || row.FillSlippageTicks != nil {
				t.Fatalf("no fill yet — the fill receipt must be absent: %v / %v", row.FilledAtMs, row.FillSlippageTicks)
			}
			if row.LastVerdict != c.want {
				t.Fatalf("last_verdict = %q, want %q", row.LastVerdict, c.want)
			}
			if row.ZoneProvenance != "frozen_overlap:PDH[98.50,100.00]" {
				t.Fatalf("provenance label: %q", row.ZoneProvenance)
			}
		})
	}
}

// short_of_zone: no frame, the row stays armed, ONE warn and ONE count over
// three passes.
func TestZoneRowShortOfZoneWaitsAndCountsOnce(t *testing.T) {
	r := newZoneRig(t, "w3-zone-short", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	r.setTape(zoneTapeReacted(99.0, r.now))
	before := gateBlocks(r.at.id, "market_in_zone_short_of_zone")
	for i := 0; i < 3; i++ {
		r.at.maybeManageArmedOrdersAt(nil, r.now.Add(time.Duration(i)*time.Second))
	}
	if sigs, cancels := r.drain(); len(sigs) != 0 || len(cancels) != 0 {
		t.Fatalf("short_of_zone must send nothing: sigs=%d cancels=%d", len(sigs), len(cancels))
	}
	row := r.row("S1")
	if row.State != store.StateArmed || row.SignalID != "" || row.LastVerdict != "short_of_zone" {
		t.Fatalf("the row must stay armed with verdict short_of_zone: %+v", row)
	}
	if n := r.armRefusals("market_in_zone:short_of_zone"); n != 1 {
		t.Fatalf("three passes must count ONE short_of_zone, got %d", n)
	}
	if gateBlocks(r.at.id, "market_in_zone_short_of_zone") != before+1 {
		t.Fatal("the gate-block counter must move once")
	}
	// Price comes back into the zone: the same row places.
	r.setTape(zoneTape(100.25, r.now, 0))
	r.at.maybeManageArmedOrdersAt(nil, r.now.Add(5*time.Second))
	if sigs, _ := r.drain(); len(sigs) != 1 || sigs[0].LimitPrice != 100.5 {
		t.Fatalf("back inside the zone the armed row must place once at 100.50: %+v", sigs)
	}
}

// unknown (a stale tape): nothing placed, nothing cancelled, the row stays armed.
func TestZoneRowUnknownOnAStaleTapeDoesNothing(t *testing.T) {
	r := newZoneRig(t, "w3-zone-stale", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	r.setTape(zoneTape(100.0, r.now, -5*time.Minute))
	r.at.maybeManageArmedOrdersAt(nil, r.now)
	if sigs, cancels := r.drain(); len(sigs) != 0 || len(cancels) != 0 {
		t.Fatalf("unknown must send nothing: sigs=%d cancels=%d", len(sigs), len(cancels))
	}
	if row := r.row("S1"); row.State != store.StateArmed || row.LastVerdict != "unknown" {
		t.Fatalf("unknown leaves the row armed: %+v", row)
	}
}

// The hold refuses (the row survives) and the row places after release.
func TestZoneRowHoldRefusesThenPlacesAfterRelease(t *testing.T) {
	dir := withMaintenanceDir(t)
	r := newZoneRig(t, "w3-zone-hold", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	r.setTape(zoneTape(100.0, r.now, 0))
	setHold(t, dir, "job-zone")
	before := gateBlocks(r.at.id, "maintenance_hold")
	r.at.maybeManageArmedOrdersAt(nil, r.now)
	if sigs, _ := r.drain(); len(sigs) != 0 {
		t.Fatalf("held: nothing may reach the wire, got %d", len(sigs))
	}
	if row := r.row("S1"); row.State != store.StateArmed || !strings.HasPrefix(row.LastVerdict, "refused: maintenance_hold") {
		t.Fatalf("the hold must not advance the row: %+v", row)
	}
	if gateBlocks(r.at.id, "maintenance_hold") <= before {
		t.Fatal("the refusal must count maintenance_hold")
	}
	if err := store.ClearMaintenanceHold(dir, "job-zone"); err != nil {
		t.Fatal(err)
	}
	r.at.maybeManageArmedOrdersAt(nil, r.now.Add(time.Second))
	if sigs, _ := r.drain(); len(sigs) != 1 {
		t.Fatalf("after release the surviving arm must place once, got %d", len(sigs))
	}
}

// Two policy scenarios both inside: ONE entry per plan, one frame.
func TestZoneTwoScenariosInsidePlaceOnce(t *testing.T) {
	r := newZoneRig(t, "w3-zone-two", zoneDoc(
		zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false),
		zoneScenario("S2", kernel.EntryPolicyMarketInZone, zone, false)))
	r.setTape(zoneTape(100.0, r.now, 0))
	r.at.maybeManageArmedOrdersAt(nil, r.now)
	if sigs, _ := r.drain(); len(sigs) != 1 {
		t.Fatalf("two inside scenarios must reach the wire ONCE, got %d", len(sigs))
	}
}

// A market_in_zone arm with no zone (an overlay that skipped the write check)
// is refused at authoring (D9): no row, no frame, counted once.
func TestZoneLegWithoutAZoneIsRefusedAtAuthoring(t *testing.T) {
	r := newZoneRig(t, "w3-zone-missing", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, nil, false)))
	r.setTape(zoneTape(100.0, r.now, 0))
	for i := 0; i < 3; i++ {
		r.at.maybeManageArmedOrdersAt(nil, r.now.Add(time.Duration(i)*time.Second))
	}
	if sigs, _ := r.drain(); len(sigs) != 0 {
		t.Fatalf("a zoneless market_in_zone leg must never place, got %d", len(sigs))
	}
	if rows := r.rows(); len(rows) != 0 {
		t.Fatalf("a refused leg must not author a row: %+v", rows)
	}
	if n := r.armRefusals("market_in_zone:" + kernel.ZoneMissing); n != 1 {
		t.Fatalf("three passes must count ONE zone_missing, got %d", n)
	}
}

// Legacy (no policy): the row carries no W3 field and the legacy limit path
// runs (limit AT the authored entry 100.00, not a zone bound).
func TestLegacyArmIsUntouchedByTheZonePath(t *testing.T) {
	r := newZoneRig(t, "w3-zone-legacy", zoneDoc(zoneScenario("S1", "", zone, false)))
	r.setTape(zoneTape(101.95, r.now, 0))
	r.at.maybeManageArmedOrdersAt(nil, r.now)
	sigs, _ := r.drain()
	if len(sigs) != 1 || sigs[0].LimitPrice != 100 {
		t.Fatalf("legacy arm must place its authored limit 100.00: %+v", sigs)
	}
	row := r.row("S1")
	if row.Policy != "" || row.ZoneLo != nil || row.PlannedEntryPx != nil || row.EvalPrice != nil || row.PlacedAtMs != nil || row.LastVerdict != "" {
		t.Fatalf("a legacy row must carry no W3 field: %+v", row)
	}
}

// The rest cap: a policy limit resting 31 min is cancelled "zone rest
// expired"; 29 min is left alone; a legacy working row of the same age is
// never touched.
func TestZoneRestCap(t *testing.T) {
	r := newZoneRig(t, "w3-zone-rest", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	r.setTape(zoneTape(101.95, r.now, 0)) // beyond: rests at 100.50
	r.at.maybeManageArmedOrdersAt(nil, r.now)
	sigs, _ := r.drain()
	if len(sigs) != 1 {
		t.Fatalf("fixture: want 1 placement, got %d", len(sigs))
	}
	sid := sigs[0].SignalID
	// A legacy working row of the same age, on another plan.
	legacy := &store.ArmedOrderDB{TraderID: r.at.id, PlanID: "2026-09-11:TEST:other", Version: 1, Session: "TEST", Scenario: "S9",
		Side: "long", EntryPx: 90, StopPx: 85, TargetPx: 100, State: store.StateArmed, CreatedAt: r.now, UpdatedAt: r.now}
	if err := r.st.ArmedOrders().UpsertArm(legacy); err != nil {
		t.Fatal(err)
	}
	if err := r.st.ArmedOrders().BeginPlacement(legacy.ID, "legacy-sig"); err != nil {
		t.Fatal(err)
	}
	book := func(at time.Time) {
		r.srv.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{
			{OrderID: "o1", Name: sid, Symbol: "MNQ", Action: "buy", Type: "limit", LimitPrice: 100.5, Quantity: 1, State: "Working"},
			{OrderID: "o2", Name: "legacy-sig", Symbol: "MNQ", Action: "buy", Type: "limit", LimitPrice: 90, Quantity: 1, State: "Working"},
		}}, at)
	}
	at29 := r.now.Add(29 * time.Minute)
	book(at29)
	r.setTape(zoneTape(101.95, at29, 0))
	r.at.maybeManageArmedOrdersAt(nil, at29)
	if _, cancels := r.drain(); len(cancels) != 0 {
		t.Fatalf("29 min must not cancel: %+v", cancels)
	}
	at31 := r.now.Add(31 * time.Minute)
	book(at31)
	r.setTape(zoneTape(101.95, at31, 0))
	r.at.maybeManageArmedOrdersAt(nil, at31)
	_, cancels := r.drain()
	if len(cancels) != 1 || cancels[0].SignalID != sid {
		t.Fatalf("31 min must cancel exactly the policy limit %s: %+v", sid, cancels)
	}
	row := r.row("S1")
	// WAVE PLANNER B1 (P1 fold, CTO #213): the expiry REQUESTS the cancel —
	// cancel_pending with the signal id KEPT. The re-arm waits for the broker
	// book to confirm it and never runs on the request alone (the pre-fold
	// contract reset here and could orphan a live order or lose a fill).
	if row.State != store.StateCancelPending || !strings.Contains(row.StateReason, "zone rest expired") ||
		!strings.Contains(row.StateReason, "re-arm on broker-book confirm") || row.SignalID != sid {
		t.Fatalf("the rest-capped row must request the cancel and KEEP its signal id: %+v", row)
	}
	// The broker book CONFIRMS the cancel → armed-unplaced, stamp cleared,
	// seq+1. Price sits beyond the band so the settle pass places nothing.
	at32 := r.now.Add(32 * time.Minute)
	r.srv.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, at32)
	r.persistFlat(at32)
	r.setTape(zoneTape(160.0, at32, 0))
	r.at.maybeManageArmedOrdersAt(nil, at32)
	if sigs, cancels := r.drain(); len(sigs) != 0 || len(cancels) != 0 {
		t.Fatalf("the settle pass must send nothing: sigs=%+v cancels=%+v", sigs, cancels)
	}
	row = r.row("S1")
	if row.State != store.StateArmed || row.SignalID != "" || row.PlacedAtMs != nil {
		t.Fatalf("the book-confirmed cancel must reset to armed-unplaced 'zone rest expired': %+v", row)
	}
	if row.PlacementSeq != 1 {
		t.Fatalf("the reset mints the next placement seq (0 authored +1), got %d: %+v", row.PlacementSeq, row)
	}
	var lg store.ArmedOrderDB
	if err := r.st.GormDB().First(&lg, legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if lg.State == store.StateCancelPending || lg.CancelRequestedAtMs != 0 || strings.Contains(lg.StateReason, "zone rest") {
		t.Fatalf("a legacy row of the same age must be untouched: %+v", lg)
	}
}

// D15 at the call site: fill → position closed → the next pass authors no
// new row and sends nothing within the version; a new version re-arms.
func TestZoneReArmPinnedWithinVersionAtTheCallSite(t *testing.T) {
	r := newZoneRig(t, "w3-zone-pin", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	r.setTape(zoneTape(100.0, r.now, 0))
	r.at.maybeManageArmedOrdersAt(nil, r.now)
	sigs, _ := r.drain()
	if len(sigs) != 1 {
		t.Fatalf("fixture: want 1 placement, got %d", len(sigs))
	}
	ledger := r.st.ArmedOrders()
	r.at.onArmedOrderUpdate(ntwire.OrderUpdatePayload{SignalID: sigs[0].SignalID, State: "filled", FillPrice: 100.25, Account: "Sim101"}, ledger)
	if row := r.row("S1"); row.State != store.StateFilled || row.FilledAtMs == nil || row.FillSlippageTicks == nil || *row.FillSlippageTicks != -1 {
		// 100.25 filled against the buy limit 100.50: one tick BETTER → −1 (+ = worse).
		t.Fatalf("the fill frame must stamp filled_at_ms and fill_slippage_ticks −1: %+v", row)
	}
	opens, err := r.st.Position().GetOpenPositions(r.at.id)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range opens {
		if _, err := r.st.Position().ClosePosition(p.ID, 98, "sl", -2.25, 0, "stop"); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 2; i++ {
		at := r.now.Add(time.Duration(i) * time.Minute)
		r.setTape(zoneTape(100.0, at, 0)) // a fresh tape INSIDE the zone and a fresh flat
		r.flatBook(at)                    // book: only the pin can stop a re-place
		r.at.maybeManageArmedOrdersAt(nil, at)
	}
	if sigs, _ := r.drain(); len(sigs) != 0 {
		t.Fatalf("a filled market_in_zone arm must not re-place within its version, got %d frame(s)", len(sigs))
	}
	if rows := r.rows(); len(rows) != 1 {
		t.Fatalf("no new placement row may be minted within the version: %+v", rows)
	}
	// A new plan version re-arms.
	blob, _ := json.Marshal(zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	shadowPlanAtTime(t, r.at, r.st, string(blob), r.now)
	// The broker adapter's B3 dupe guard drops an identical entry within a
	// WALL-CLOCK 55 s; the fixture clock says 3 minutes passed, so the adapter
	// is re-made to stand for that elapsed time (same server, same account).
	r.at.trader = ntTrader.NewTCPTrader(r.srv, "MNQ", "Sim101")
	r.setTape(zoneTape(100.0, r.now.Add(3*time.Minute), 0))
	r.flatBook(r.now.Add(3 * time.Minute))
	r.at.maybeManageArmedOrdersAt(nil, r.now.Add(3*time.Minute))
	if sigs, _ := r.drain(); len(sigs) != 1 {
		t.Fatalf("a NEW version must re-arm and place once, got %d", len(sigs))
	}
}

// ── receipts at onArmedOrderUpdate ──────────────────────────────────────────

func TestZoneFillReceiptAtTheOrderUpdateCallSite(t *testing.T) {
	r := newZoneRig(t, "w3-zone-receipt", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	ledger := r.st.ArmedOrders()
	mk := func(scenario, side, sig string, policy string, entry, lo, hi float64) int64 {
		row := &store.ArmedOrderDB{TraderID: r.at.id, PlanID: "2026-09-11:TEST:rcpt", Version: 1, Session: "TEST", Scenario: scenario,
			Side: side, EntryPx: entry, StopPx: entry - 20, TargetPx: entry + 50, State: store.StateArmed, CreatedAt: r.now, UpdatedAt: r.now, Policy: policy}
		if strings.EqualFold(side, "short") {
			row.StopPx, row.TargetPx = entry+20, entry-50
		}
		if policy != "" {
			row.ZoneLo, row.ZoneHi = &lo, &hi
		}
		if err := ledger.UpsertArm(row); err != nil {
			t.Fatal(err)
		}
		if err := ledger.BeginPlacement(row.ID, sig); err != nil {
			t.Fatal(err)
		}
		return row.ID
	}
	get := func(id int64) store.ArmedOrderDB {
		var x store.ArmedOrderDB
		if err := r.st.GormDB().First(&x, id).Error; err != nil {
			t.Fatal(err)
		}
		return x
	}
	fill := func(sig string, px float64) {
		r.at.onArmedOrderUpdate(ntwire.OrderUpdatePayload{SignalID: sig, State: "filled", FillPrice: px, Account: "Sim101"}, ledger)
	}
	// long, buy bound 31010: a 31006 fill is 16 ticks BETTER → −16 (+ = worse).
	idL := mk("R1", "long", "sig-long", kernel.EntryPolicyMarketInZone, 31010, 31000, 31010)
	fill("sig-long", 31006)
	if x := get(idL); x.FillSlippageTicks == nil || *x.FillSlippageTicks != -16 || x.FilledAtMs == nil {
		t.Fatalf("long receipt: want −16 ticks, got %+v / filled_at %v", x.FillSlippageTicks, x.FilledAtMs)
	}
	// short, sell bound 31000: a 31004 fill is 16 ticks better → −16.
	idS := mk("R2", "short", "sig-short", kernel.EntryPolicyMarketInZone, 31000, 31000, 31010)
	fill("sig-short", 31004)
	if x := get(idS); x.FillSlippageTicks == nil || *x.FillSlippageTicks != -16 {
		t.Fatalf("short receipt: want −16 ticks, got %+v", x.FillSlippageTicks)
	}
	// legacy: NULL.
	idG := mk("R3", "long", "sig-legacy", "", 31010, 0, 0)
	fill("sig-legacy", 31006)
	if x := get(idG); x.FillSlippageTicks != nil || x.FilledAtMs != nil {
		t.Fatalf("a legacy row's receipt must stay NULL: %+v / %v", x.FillSlippageTicks, x.FilledAtMs)
	}
	// A fill BEYOND the far bound is a contradiction: counted, never hidden.
	beyond0, _ := store.SystemCounter(r.st, "market_in_zone:fill_beyond_far")
	idB := mk("R4", "long", "sig-beyond", kernel.EntryPolicyMarketInZone, 31010, 31000, 31010)
	fill("sig-beyond", 31011)
	if n, _ := store.SystemCounter(r.st, "market_in_zone:fill_beyond_far"); n != beyond0+1 {
		t.Fatalf("a fill beyond the far bound must be counted as a contradiction (%d → %d)", beyond0, n)
	}
	if x := get(idB); x.FillSlippageTicks == nil || *x.FillSlippageTicks != 4 {
		t.Fatalf("beyond-far slippage must be +4 (worse): %+v", x.FillSlippageTicks)
	}
	// A near-side fill (improved, outside the zone) is flagged.
	near0, _ := store.SystemCounter(r.st, "market_in_zone:fill_near_side")
	mk("R5", "long", "sig-near", kernel.EntryPolicyMarketInZone, 31010, 31000, 31010)
	fill("sig-near", 30999)
	if n, _ := store.SystemCounter(r.st, "market_in_zone:fill_near_side"); n != near0+1 {
		t.Fatalf("a near-side fill must be flagged (%d → %d)", near0, n)
	}
}

// The receipt math, pure: a recorded fill can never be labelled in-zone when
// it lies outside the zone.
func TestZoneFillReceiptNeverLabelsAnOutsideFillInZone(t *testing.T) {
	for _, c := range []struct {
		side             string
		fill             float64
		beyond, pastNear bool
	}{
		{"long", 31000, false, false}, {"long", 31010, false, false}, {"long", 31010.25, true, false}, {"long", 30999.75, false, true},
		{"short", 31000, false, false}, {"short", 31010, false, false}, {"short", 30999.75, true, false}, {"short", 31010.25, false, true},
	} {
		limit := 31010.0
		if c.side == "short" {
			limit = 31000
		}
		_, b, n := zoneFillReceipt(c.side, c.fill, limit, 31000, 31010, 0.25)
		if b != c.beyond || n != c.pastNear {
			t.Errorf("%s fill %.2f: beyond=%v near=%v, want %v/%v", c.side, c.fill, b, n, c.beyond, c.pastNear)
		}
	}
	if s, _, _ := zoneFillReceipt("long", 31006, 31010, 31000, 31010, 0); s != nil {
		t.Fatal("no tick → slippage is absent (nil), never 0")
	}
}

// D7 at the call site, on the non-structural composition (reclaim — legacy
// kind stop_entry, so this also proves the policy decides the order kind): the
// stop is composed from the NEAR bound, so min-SL holds at the best fill, and
// the limit still rests at the FAR bound.
func TestZoneStopComposedFromTheNearBound(t *testing.T) {
	sc := zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)
	sc.Condition = "reclaim"
	sc.Arm.Stop = 99.25 // tighter than any floor: the composition decides the stop
	r := newZoneRig(t, "w3-zone-d7", zoneDoc(sc))
	tape := zoneTape(100.0, r.now, 0)
	r.setTape(tape)
	r.at.maybeManageArmedOrdersAt(nil, r.now)
	sigs, _ := r.drain()
	if len(sigs) != 1 || sigs[0].OrderType != "limit" || sigs[0].LimitPrice != 100.5 {
		t.Fatalf("a market_in_zone reclaim must rest a LIMIT at the far bound 100.50: %+v", sigs)
	}
	floor := kernel.MinSLATRMult() * armSeamATR5mFromBars(tape)
	if floor <= 0 {
		t.Fatal("fixture: the tape must yield an ATR5m")
	}
	// The wire rounds the stop to the NEAREST tick (RoundToTick), so it may sit
	// up to half a tick inside the composed floor — a pre-existing property of
	// every armed stop, not of this wave. From the far bound it would be a
	// whole point tighter.
	if got := sigs[0].StopLoss; got > 99.5-floor+0.125+1e-9 {
		t.Fatalf("stop %.2f must be at least %.2f (min-SL) below the NEAR bound 99.50 (±½ tick) — composed from the far bound it would be %.2f", got, floor, 100.5-floor)
	}
}

// A refusal at the send point's admission chain (armAdmitted → admitEntry) is
// recorded in the row's verdict with its class — the card's "Blocked: …".
func TestZoneRowAdmissionRefusalIsRecorded(t *testing.T) {
	r := newZoneRig(t, "w3-zone-admit", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	r.setTape(zoneTape(100.0, r.now, 0))
	r.at.config.StrategyConfig.RiskControl.ReentryCooldownMinutes = 20
	discipline.NoteStopLossExit(r.at.id, "MNQ", "long", 99, r.now.Add(-time.Minute).UnixMilli())
	t.Cleanup(discipline.ResetReentryForTest)
	r.at.maybeManageArmedOrdersAt(nil, r.now)
	if s, _ := r.drain(); len(s) != 0 {
		t.Fatalf("a send-point admission refusal must place nothing: %+v", s)
	}
	row := r.row("S1")
	if row.State != store.StateArmed || !strings.HasPrefix(row.LastVerdict, "refused: reentry_cooldown: ") {
		t.Fatalf("the row stays armed and its verdict names the admission class: %+v", row.LastVerdict)
	}
}

// R2 parity with the write-time zone check: a market_in_zone reject never
// takes the structural-geometry path, so a scenario with no frozen zone map —
// which the legacy reject path REFUSES (no provenance) — still places, and the
// row records the provenance as a label.
func TestZoneRejectSkipsTheStructuralGeometryRefusal(t *testing.T) {
	bare := func(policy string) kernel.PlanDoc {
		return kernel.PlanDoc{Bias: kernel.PlanBias{Direction: "long", Conviction: "low", FlipCondition: "n/a"},
			Levels:    []kernel.PlanLevel{{Price: 100, Label: "PDH", Grade: "A", Instruction: "fade"}},
			Scenarios: []kernel.PlanScenario{zoneScenario("S1", policy, zone, false)}, NoTrade: []string{}, DeathCondition: "n/a"}
	}
	legacy := newZoneRig(t, "w3-zone-nomap-legacy", bare(""))
	legacy.setTape(zoneTape(100.0, legacy.now, 0))
	legacy.at.maybeManageArmedOrdersAt(nil, legacy.now)
	if s, _ := legacy.drain(); len(s) != 0 {
		t.Fatalf("fixture: the legacy reject with no frozen map must be refused by the geometry, got %d frame(s)", len(s))
	}
	r := newZoneRig(t, "w3-zone-nomap", bare(kernel.EntryPolicyMarketInZone))
	r.setTape(zoneTape(100.0, r.now, 0))
	r.at.maybeManageArmedOrdersAt(nil, r.now)
	sigs, _ := r.drain()
	if len(sigs) != 1 || sigs[0].LimitPrice != 100.5 {
		t.Fatalf("a market_in_zone reject must not be refused by the structural geometry: %+v", sigs)
	}
	if row := r.row("S1"); !strings.HasPrefix(row.ZoneProvenance, "planner_only(") {
		t.Fatalf("provenance is a label: %q", row.ZoneProvenance)
	}
}

// The ledger row is the record (CTO): at the composer's UpsertArm call site
// the four authoring fields are stamped on create, rewritten by the armed
// refresh, and re-stamped by a re-authorize on a new version.
func TestZoneLedgerStampsAtAuthoringThroughTheComposer(t *testing.T) {
	r := newZoneRig(t, "w3-zone-stamp", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	r.setTape(zoneTapeReacted(99.0, r.now)) // short of the zone: authored, never placed
	check := func(what string, row store.ArmedOrderDB) {
		t.Helper()
		if row.Policy != kernel.EntryPolicyMarketInZone || row.ZoneLo == nil || *row.ZoneLo != 99.5 || row.ZoneHi == nil || *row.ZoneHi != 100.5 ||
			row.ZoneProvenance != "frozen_overlap:PDH[98.50,100.00]" || row.PlannedEntryPx == nil || *row.PlannedEntryPx != 100 || row.EntryPx != 100.5 {
			t.Fatalf("%s: the row must carry policy / zone / provenance / planned entry (entry = far bound): %+v", what, row)
		}
		if row.EvalPrice != nil || row.EvalBarMs != nil || row.PlacedAtMs != nil || row.FilledAtMs != nil || row.FillSlippageTicks != nil {
			t.Fatalf("%s: an unplaced row carries no placement or fill receipt: %+v", what, row)
		}
	}
	// create
	r.at.maybeManageArmedOrdersAt(nil, r.now)
	created := r.row("S1")
	check("create", created)
	// armed refresh: a row whose stored zone disagrees with the plan is
	// rewritten by the composer's refresh (same row, same version).
	lo, hi, planned := 1.0, 2.0, 7.0
	if err := r.st.GormDB().Model(&store.ArmedOrderDB{}).Where("id = ?", created.ID).Updates(map[string]any{
		"zone_lo": lo, "zone_hi": hi, "zone_provenance": "stale", "planned_entry_px": planned, "entry_px": 3.0}).Error; err != nil {
		t.Fatal(err)
	}
	r.at.maybeManageArmedOrdersAt(nil, r.now.Add(time.Second))
	refreshed := r.row("S1")
	if refreshed.ID != created.ID || refreshed.State != store.StateArmed {
		t.Fatalf("the refresh must rewrite the SAME armed row: %+v", refreshed)
	}
	check("armed refresh", refreshed)
	// re-authorize: the never-placed row is cancelled, a new plan version
	// re-authorizes it in place.
	if err := r.st.ArmedOrders().SetState(created.ID, store.StateCancelled, "owner cancel"); err != nil {
		t.Fatal(err)
	}
	blob, _ := json.Marshal(zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	shadowPlanAtTime(t, r.at, r.st, string(blob), r.now)
	r.at.maybeManageArmedOrdersAt(nil, r.now.Add(2*time.Second))
	reauth := r.row("S1")
	if reauth.ID != created.ID || reauth.State != store.StateArmed || reauth.Version != 2 || reauth.ArmedUnderVersion != 2 {
		t.Fatalf("a new version must re-authorize the never-placed row in place: %+v", reauth)
	}
	check("re-authorize", reauth)
}

// last_verdict is evaluated on every pass and WRITTEN only when it changes:
// short_of_zone → short_of_zone → inside is exactly TWO writes, and
// last_verdict_ms is when the current verdict was first reached.
func TestZoneVerdictWrittenOnlyOnChange(t *testing.T) {
	r := newZoneRig(t, "w3-zone-verdict-writes", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	var writes atomic.Int32
	db := r.st.GormDB()
	if err := db.Callback().Update().After("gorm:update").Register("w3_count_last_verdict", func(tx *gorm.DB) {
		// Every UPDATE statement that names last_verdict counts — a no-op
		// update still takes SQLite's write lock.
		if tx.Error == nil && strings.Contains(tx.Statement.SQL.String(), "last_verdict") {
			writes.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t0, t1, t2 := r.now, r.now.Add(20*time.Second), r.now.Add(40*time.Second)
	r.setTape(zoneTapeReacted(99.0, r.now))
	r.at.maybeManageArmedOrdersAt(nil, t0)
	r.at.maybeManageArmedOrdersAt(nil, t1)
	if row := r.row("S1"); row.LastVerdict != "short_of_zone" || row.LastVerdictMs == nil || *row.LastVerdictMs != t0.UnixMilli() {
		t.Fatalf("an unchanged verdict keeps the time it was first reached: %+v / %v", row.LastVerdict, row.LastVerdictMs)
	}
	r.setTape(zoneTape(100.0, r.now, 0))
	r.at.maybeManageArmedOrdersAt(nil, t2)
	row := r.row("S1")
	if row.LastVerdict != "inside" || row.LastVerdictMs == nil || *row.LastVerdictMs != t2.UnixMilli() {
		t.Fatalf("the changed verdict is written with its time: %+v / %v", row.LastVerdict, row.LastVerdictMs)
	}
	if n := writes.Load(); n != 2 {
		t.Fatalf("short → short → inside must be exactly 2 last_verdict writes, observed %d", n)
	}
}

// CTO 1790187980085: each verdict change is ONE counted event per class and ONE
// log line; an unchanged verdict is neither counted nor logged.
func TestZoneVerdictChangeIsOneCountAndOneLogLine(t *testing.T) {
	r := newZoneRig(t, "w3-zone-verdict-counts", zoneDoc(zoneScenario("S1", kernel.EntryPolicyMarketInZone, zone, false)))
	logs := captureTraderLog(t)
	t0, t1, t2 := r.now, r.now.Add(20*time.Second), r.now.Add(40*time.Second)
	r.setTape(zoneTapeReacted(99.0, r.now))
	r.at.maybeManageArmedOrdersAt(nil, t0)
	r.at.maybeManageArmedOrdersAt(nil, t1)
	r.setTape(zoneTape(100.0, r.now, 0))
	r.at.maybeManageArmedOrdersAt(nil, t2)
	for class, want := range map[string]int{"short_of_zone": 1, "inside": 1, "beyond": 0, "unknown": 0} {
		if n, _ := store.SystemCounter(r.st, "market_in_zone:verdict:"+class); n != want {
			t.Errorf("verdict class %s counted %d, want %d", class, n, want)
		}
	}
	if n := strings.Count(logs.String(), "🧭 zone verdict S1 leg 1:"); n != 2 {
		t.Fatalf("short → short → inside must log exactly 2 verdict lines, got %d:\n%s", n, logs.String())
	}
	if !strings.Contains(logs.String(), "none → short_of_zone") || !strings.Contains(logs.String(), "short_of_zone → inside") {
		t.Fatalf("each line names the change:\n%s", logs.String())
	}
}

func TestZoneVerdictChangeClass(t *testing.T) {
	for v, want := range map[string]string{
		"inside": "inside", "beyond": "beyond", "short_of_zone": "short_of_zone", "unknown": "unknown",
		"refused: slot: slot not free at the broker": "refused", "waiting: x": "other",
	} {
		if got := zoneVerdictChangeClass(v); got != want {
			t.Errorf("%q → %q, want %q", v, got, want)
		}
	}
}
