package trader

import (
	"strings"
	"testing"
	"time"

	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// m433 (release #52): the Sunday broker-down state. NT8 is linked over TCP but
// its broker connection is down: accounts_list count=0, no positions frame and
// no order_snapshot for the bound account since connect, the DB shows the
// account FLAT, the market is closed. Every test drives the REAL
// InstallationGateStatus with the REAL per-trader cutover seam
// (CutoverGateStatus, not the fixture's stub legs).

var (
	m433Sunday = time.Date(2026, 10, 25, 12, 0, 0, 0, kernel.CTLocation()) // CME closed
	m433Monday = time.Date(2026, 10, 26, 10, 0, 0, 0, kernel.CTLocation()) // CME open
)

// m433Fixture is the gate fixture with the production cutover seam restored,
// a server that has NEVER received a positions or order_snapshot frame, an
// empty live account list, a persisted SIM census, and the clock on a closed
// Sunday.
func m433Fixture(t *testing.T) *gateFixture {
	t.Helper()
	f := newGateFixture(t)
	installationTraderCutover = func(at *AutoTrader) []CutoverLeg { return at.CutoverGateStatus().Legs }
	s := ntwire.NewTCPServer(nil)
	s.SetAccountsList([]ntwire.AccountInfo{}, "") // broker down: count=0
	for _, at := range f.loaded {
		at.trader = ntTrader.NewTCPTrader(s, "MNQ", "Sim101")
	}
	f.srv = s
	if err := f.st.NTAccountsLast().Upsert([]store.NTAccountLast{
		{Name: "Sim101", IsSim: true, Connection: "SimulationConnection", ReceivedAtMs: m433Sunday.UnixMilli()},
	}); err != nil {
		t.Fatal(err)
	}
	prev := installationGateNow
	installationGateNow = func() time.Time { return m433Sunday }
	t.Cleanup(func() { installationGateNow = prev })
	return f
}

// m433Evidence persists the broker's LAST answers: a positions frame with
// openPos non-flat positions and an order snapshot with working working orders.
func m433Evidence(t *testing.T, f *gateFixture, openPos, working int) {
	t.Helper()
	ms := m433Sunday.Add(-40 * time.Hour).UnixMilli()
	if err := f.st.NTPositionsLast().Upsert("Sim101", openPos, ms); err != nil {
		t.Fatal(err)
	}
	if err := f.st.NT8OrderSnapshots().Insert(&store.NT8OrderSnapshot{
		Account: "Sim101", Reason: "periodic", OrdersJSON: "[]", OrderCount: working, WorkingCount: working, ReceivedMs: ms,
	}); err != nil {
		t.Fatal(err)
	}
}

func m433Cutover(t *testing.T, g InstallationGate) InstallationGateLeg {
	t.Helper()
	l, ok := legOf(g, "trader_cutover:gate-t1")
	if !ok {
		t.Fatalf("trader_cutover leg missing: %+v", g.Legs)
	}
	return l
}

// (0) The state exactly as reported, with NO evidence anywhere: leg 2 blocks
// and STAYS blocked — unknown is not flat. Every other leg's verdict is logged.
func TestInstallationGateM433NoEvidenceStaysClosed(t *testing.T) {
	f := m433Fixture(t)
	g := f.run()
	for _, l := range g.Legs {
		t.Logf("%-26s pass=%-5v %s", l.Name, l.Pass, l.Detail)
	}
	if g.Ready {
		t.Fatalf("no broker evidence at all must NOT be ready: %+v", g.Legs)
	}
	l := m433Cutover(t, g)
	if l.Pass || !strings.Contains(l.Detail, "api_positions") || !strings.Contains(l.Detail, "unknowable, not flat") {
		t.Fatalf("want api_positions refused with the unknowable reason, got %+v", l)
	}
	if sa, _ := legOf(g, "sim_accounts"); !sa.Pass {
		t.Fatalf("m430 leg must pass in this state: %+v", sa)
	}
}

// (1) Persisted flat evidence + closed market + flat DB/ledger -> READY, and the
// pass names its evidence.
func TestInstallationGateM433PersistedFlatClosedMarketPasses(t *testing.T) {
	f := m433Fixture(t)
	m433Evidence(t, f, 0, 0)
	g := f.run()
	if !g.Ready {
		t.Fatalf("persisted flat + closed market must be ready: %+v", g.Legs)
	}
	l := m433Cutover(t, g)
	for _, w := range []string{"m433 broker-quiet", "CME closed", "persisted snapshot of", "persisted frame of"} {
		if !strings.Contains(l.Detail, w) {
			t.Fatalf("detail %q must say %q", l.Detail, w)
		}
	}
}

// (2) Same evidence, market OPEN -> refused.
func TestInstallationGateM433MarketOpenRefuses(t *testing.T) {
	f := m433Fixture(t)
	m433Evidence(t, f, 0, 0)
	installationGateNow = func() time.Time { return m433Monday }
	mustFail(t, f.run(), "trader_cutover:gate-t1", "the CME session is open")
}

// (3) The broker's last positions frame carried an open position -> refused.
func TestInstallationGateM433PersistedOpenPositionRefuses(t *testing.T) {
	f := m433Fixture(t)
	m433Evidence(t, f, 1, 0)
	mustFail(t, f.run(), "trader_cutover:gate-t1", "persisted frame has 1 open position")
}

// (4) The broker's last order snapshot carried working orders -> refused.
func TestInstallationGateM433PersistedWorkingOrderRefuses(t *testing.T) {
	f := m433Fixture(t)
	m433Evidence(t, f, 0, 2)
	mustFail(t, f.run(), "trader_cutover:gate-t1", "persisted snapshot has 2 working order(s)")
}

// (5) The DB shows an open position -> refused, whatever the broker evidence.
func TestInstallationGateM433DBOpenPositionRefuses(t *testing.T) {
	f := m433Fixture(t)
	m433Evidence(t, f, 0, 0)
	if err := f.st.Position().CreateOpenPosition(&store.TraderPosition{
		TraderID: "gate-t1", ExchangeType: "ninjatrader", ExchangePositionID: "m433-pos",
		Symbol: "MNQ", Side: "LONG", Quantity: 1, EntryQuantity: 1, EntryPrice: 29000,
		EntryOrderID: "sig-m433", Leverage: 1, Status: "OPEN", Source: "test", Account: "Sim101",
	}); err != nil {
		t.Fatal(err)
	}
	mustFail(t, f.run(), "trader_cutover:gate-t1", "db_open_positions")
	l := m433Cutover(t, f.run())
	if strings.Contains(l.Detail, "m433 broker-quiet") {
		t.Fatalf("the rule must not have rescued a DB-open position: %q", l.Detail)
	}
}

// (6) A placed armed row (ledger exposure) -> refused.
func TestInstallationGateM433PlacedLedgerRowRefuses(t *testing.T) {
	f := m433Fixture(t)
	m433Evidence(t, f, 0, 0)
	led := f.st.ArmedOrders()
	r := store.ArmedOrderDB{TraderID: "gate-t1", PlanID: "2026-10-25:NY:m433", Version: 1, Session: "NY", Scenario: "S1", Side: "LONG", EntryPx: 100, StopPx: 95, TargetPx: 110, State: store.StateArmed}
	if err := led.UpsertArm(&r); err != nil {
		t.Fatal(err)
	}
	if err := led.BeginPlacement(r.ID, "sig-m433"); err != nil {
		t.Fatal(err)
	}
	g := f.run()
	l := m433Cutover(t, g)
	if l.Pass || strings.Contains(l.Detail, "m433 broker-quiet") {
		t.Fatalf("a placed ledger row must refuse the rule: %+v", l)
	}
}

// (7) Positions evidence only: no order evidence -> the book is unknowable.
func TestInstallationGateM433NoOrderEvidenceRefuses(t *testing.T) {
	f := m433Fixture(t)
	if err := f.st.NTPositionsLast().Upsert("Sim101", 0, m433Sunday.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	mustFail(t, f.run(), "trader_cutover:gate-t1", "no last order snapshot")
}

// (8) Order evidence only: no positions evidence -> unknowable.
func TestInstallationGateM433NoPositionEvidenceRefuses(t *testing.T) {
	f := m433Fixture(t)
	if err := f.st.NT8OrderSnapshots().Insert(&store.NT8OrderSnapshot{Account: "Sim101", OrdersJSON: "[]", ReceivedMs: m433Sunday.UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	mustFail(t, f.run(), "trader_cutover:gate-t1", "no last positions frame")
}

// (9) A LIVE (fresh) order book is never relaxed: its content verdict stands.
func TestInstallationGateM433FreshBookWithWorkingOrderStands(t *testing.T) {
	f := m433Fixture(t)
	m433Evidence(t, f, 0, 0)
	f.srv.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{
		{OrderID: "o1", Symbol: "MNQ", Type: "stop", State: "Working", StopPrice: 29000},
	}}, m433Sunday)
	g := f.run()
	l := m433Cutover(t, g)
	if l.Pass || !strings.Contains(l.Detail, "working_orders") {
		t.Fatalf("a fresh book with a working order must refuse: %+v", l)
	}
	if strings.Contains(l.Detail, "m433 broker-quiet") {
		t.Fatalf("rule must not relax a live book: %q", l.Detail)
	}
}

// (10) State B: the process saw Friday's frames, then the link went quiet. A
// STALE in-memory book/positions that were flat -> passes; a STALE book that had
// a working order, or a known open position, -> refused.
func TestInstallationGateM433StaleInMemoryFlatPasses(t *testing.T) {
	f := m433Fixture(t)
	f.srv.SeedPositionsForTestAt("Sim101", []ntwire.OpenPosition{}, m433Sunday.Add(-40*time.Hour))
	f.srv.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, m433Sunday.Add(-40*time.Hour))
	g := f.run()
	if !g.Ready {
		t.Fatalf("stale-but-flat in-memory evidence on a closed market must be ready: %+v", g.Legs)
	}
	if l := m433Cutover(t, g); !strings.Contains(l.Detail, "in-memory book of") {
		t.Fatalf("detail must name the in-memory evidence: %q", l.Detail)
	}
}

func TestInstallationGateM433StaleInMemoryWorkingOrderRefuses(t *testing.T) {
	f := m433Fixture(t)
	f.srv.SeedPositionsForTestAt("Sim101", []ntwire.OpenPosition{}, m433Sunday.Add(-40*time.Hour))
	f.srv.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{
		{OrderID: "o1", Symbol: "MNQ", Type: "stop", State: "Working", StopPrice: 29000},
	}}, m433Sunday.Add(-40*time.Hour))
	g := f.run()
	l := m433Cutover(t, g)
	if l.Pass || strings.Contains(l.Detail, "m433 broker-quiet:") {
		t.Fatalf("a stale book with a working order must refuse: %+v", l)
	}
}

func TestInstallationGateM433StaleInMemoryOpenPositionRefuses(t *testing.T) {
	f := m433Fixture(t)
	m433Evidence(t, f, 0, 0)
	f.srv.SeedPositionsForTestAt("Sim101", []ntwire.OpenPosition{{Symbol: "MNQ", Side: "long", Quantity: 2, AvgPrice: 29000}}, m433Sunday.Add(-40*time.Hour))
	g := f.run()
	l := m433Cutover(t, g)
	if l.Pass || !strings.Contains(l.Detail, "1 position(s)") || strings.Contains(l.Detail, "m433 broker-quiet:") {
		t.Fatalf("a known open position must refuse and be quoted: %+v", l)
	}
}

// (11) The two sources disagree (memory flat, persisted shows exposure) ->
// refused: when two sources disagree, show both and refuse.
func TestInstallationGateM433SourcesDisagreeRefuses(t *testing.T) {
	f := m433Fixture(t)
	m433Evidence(t, f, 0, 1)
	f.srv.SeedPositionsForTestAt("Sim101", []ntwire.OpenPosition{}, m433Sunday.Add(-40*time.Hour))
	f.srv.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: "Sim101", Orders: []ntwire.NT8Order{}}, m433Sunday.Add(-40*time.Hour))
	mustFail(t, f.run(), "trader_cutover:gate-t1", "persisted snapshot has 1 working order(s)")
}

// (12) The deploy cutover gate itself is unchanged: CutoverGateStatus stays
// strict even where the installation gate relaxes.
func TestInstallationGateM433CutoverGateStaysStrict(t *testing.T) {
	f := m433Fixture(t)
	m433Evidence(t, f, 0, 0)
	for _, at := range f.loaded {
		g := at.CutoverGateStatus()
		pass2 := true
		for _, l := range g.Legs {
			if l.N == 2 {
				pass2 = l.Pass
			}
		}
		if pass2 {
			t.Fatalf("CutoverGateStatus leg 2 must stay strict (broker-answered): %+v", g.Legs)
		}
	}
}
