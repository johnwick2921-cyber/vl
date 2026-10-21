package trader

import (
	"strings"
	"testing"
	"time"

	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── W-ONE-BUTTON M2 — the installation-wide gate ───────────────────────────
//
// One verdict for the whole installation: every leg must pass, a leg that
// cannot be evaluated FAILS, every leg quotes its source, and the function
// never panics. The fixture is a gate that is READY; each case breaks exactly
// one thing and names the leg that must fail.

type gateFixture struct {
	dir    string
	st     *store.Store
	loaded map[string]*AutoTrader
	wire   installationWire
	srv    *ntwire.TCPServer
}

func goodCensusAck(job string) *ntwire.MaintenanceAckPayload {
	return &ntwire.MaintenanceAckPayload{Held: true, JobID: job, BuildID: "2026-09-22-m2",
		Connections: []ntwire.CensusConnection{{Sim: true, Connected: true, Settled: true}, {Sim: false, Connected: false, Settled: true}},
		Accounts:    []ntwire.CensusAccount{{Sim: true, Positions: 0, Working: 0}}}
}

func newGateFixture(t *testing.T) *gateFixture {
	t.Helper()
	dir := withMaintenanceDir(t)
	at, st := resetTrader(t, store.StrategyConfig{})
	at.id = "gate-t1"
	s := ntwire.NewTCPServer(nil)
	s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{})                        // W117 F4: known-flat on connect
	s.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: true}}, "Sim101") // sim_accounts: Sim101 is SIM-tradeable
	at.trader = ntTrader.NewTCPTrader(s, "MNQ", "Sim101")
	setHold(t, dir, "job-g")
	f := &gateFixture{dir: dir, st: st, loaded: map[string]*AutoTrader{at.id: at}, srv: s,
		wire: installationWire{Connected: true, Rec: ntwire.ConnectionRecord{AcceptSeq: 3, RemotePort: 50123, Ack: goodCensusAck("job-g")}, HasAck: true, AckAge: time.Second}}
	prevW, prevC := installationWireView, installationTraderCutover
	installationWireView = func([]*AutoTrader) (installationWire, bool) { return f.wire, true }
	installationTraderCutover = func(*AutoTrader) []CutoverLeg {
		return []CutoverLeg{{N: 1, Name: "db_open_positions", Pass: true}, {N: 2, Name: "api_positions", Pass: true}, {N: 4, Name: "working_orders", Pass: true}}
	}
	t.Cleanup(func() { installationWireView, installationTraderCutover = prevW, prevC })
	return f
}

func (f *gateFixture) run() InstallationGate { return InstallationGateStatus(f.loaded, f.st) }

func legOf(g InstallationGate, name string) (InstallationGateLeg, bool) {
	for _, l := range g.Legs {
		if l.Name == name {
			return l, true
		}
	}
	return InstallationGateLeg{}, false
}

func mustFail(t *testing.T, g InstallationGate, name, want string) {
	t.Helper()
	if g.Ready {
		t.Fatalf("gate READY although leg %s must fail: %+v", name, g.Legs)
	}
	l, ok := legOf(g, name)
	if !ok {
		t.Fatalf("leg %s missing: %+v", name, g.Legs)
	}
	if l.Pass {
		t.Fatalf("leg %s passed; want fail (%s)", name, l.Detail)
	}
	if want != "" && !strings.Contains(l.Detail, want) {
		t.Fatalf("leg %s detail %q does not say %q", name, l.Detail, want)
	}
}

func TestInstallationGateReadyWhenEveryLegPasses(t *testing.T) {
	g := newGateFixture(t).run()
	if !g.Ready {
		t.Fatalf("the fixture must be READY: %+v", g.Legs)
	}
	for _, l := range g.Legs {
		if l.Source == "" {
			t.Errorf("leg %s quotes no source", l.Name)
		}
	}
	if g.JobID != "job-g" {
		t.Fatalf("job_id: %q", g.JobID)
	}
	for _, name := range []string{"hold", "go_drained", "in_flight_sends", "queued_signals", "planner_in_flight", "traders_nt8", "addon_ack", "addon_census", "addon_census_prehold", "ledger_exposure", "sim_accounts", "trader_cutover:gate-t1"} {
		if _, ok := legOf(g, name); !ok {
			t.Errorf("leg %s missing", name)
		}
	}
	// prehold-stale-census: the drain census leg prints its AGE in the detail
	// so a preflight read shows staleness (detail text only).
	if l, ok := legOf(g, "addon_census"); !ok || !strings.Contains(l.Detail, "age=") {
		t.Fatalf("the addon_census leg must print the census age in its detail: %+v", l)
	}
}

func TestInstallationGateFailsWithoutAHold(t *testing.T) {
	f := newGateFixture(t)
	if err := store.ClearMaintenanceHold(f.dir, "job-g"); err != nil {
		t.Fatal(err)
	}
	mustFail(t, f.run(), "hold", "no hold")
}

// W-ADDON-ONE-CLICK guard #1 (#6 fold): the MAIN-view sim_accounts leg fails
// when a bound account is not SIM-tradeable — not only on the nt8_absent view.
func TestInstallationGateSimAccountsNonSimFails(t *testing.T) {
	f := newGateFixture(t)
	f.srv.SetAccountsList([]ntwire.AccountInfo{{Name: "Sim101", IsSim: false}}, "Sim101")
	mustFail(t, f.run(), "sim_accounts", "not SIM-tradeable")
}

// F35 — accounts-remembered. With the AddOn DISCONNECTED the sim_accounts leg
// vouches from the last PERSISTED accounts frame instead of the live list: a
// persisted SIM row lets a bot restart with NT8 closed still vouch for its
// bound account (detail names the source and the "last seen" time).
func TestInstallationGateSimAccountsPersistedFallbackPasses(t *testing.T) {
	f := newGateFixture(t)
	f.wire.Connected = false // NT8 closed at this restart
	now := time.Now().UnixMilli()
	if err := f.st.NTAccountsLast().Upsert([]store.NTAccountLast{
		{Name: "Sim101", IsSim: true, Connection: "SimulationConnection", ReceivedAtMs: now},
	}); err != nil {
		t.Fatal(err)
	}
	l, ok := legOf(f.run(), "sim_accounts")
	if !ok || !l.Pass {
		t.Fatalf("sim_accounts must pass from the persisted row: %+v", l)
	}
	if !strings.Contains(l.Detail, "persisted") || !strings.Contains(l.Detail, "last seen") {
		t.Fatalf("detail must say persisted + last seen: %q", l.Detail)
	}
}

// F35 — a persisted account that is NON-SIM stays non-SIM: the disconnected
// fallback fails the leg exactly as the live list would.
func TestInstallationGateSimAccountsPersistedNonSimFails(t *testing.T) {
	f := newGateFixture(t)
	f.wire.Connected = false
	if err := f.st.NTAccountsLast().Upsert([]store.NTAccountLast{
		{Name: "Sim101", IsSim: false, Connection: "LiveConnection", ReceivedAtMs: time.Now().UnixMilli()},
	}); err != nil {
		t.Fatal(err)
	}
	mustFail(t, f.run(), "sim_accounts", "not SIM")
}

// F35 — missing data fails closed: disconnected AND no persisted row for the
// bound account means nothing can vouch for it.
func TestInstallationGateSimAccountsPersistedMissingFails(t *testing.T) {
	f := newGateFixture(t)
	f.wire.Connected = false
	mustFail(t, f.run(), "sim_accounts", "no persisted account")
}

// F35 — the ORDER path stays live-only: a disconnected AddOn (empty live list)
// refuses the bound account even when a persisted SIM row exists. The trader
// never reads the store, so the gate's fallback can never leak into an order.
func TestIsAccountTradeableStaysLiveOnly(t *testing.T) {
	s := ntwire.NewTCPServer(nil) // no live accounts — NT8 closed
	tr := ntTrader.NewTCPTrader(s, "MNQ", "Sim101")
	if tr.IsAccountTradeable("Sim101") {
		t.Fatal("IsAccountTradeable must be false on an empty live list, persisted row or not")
	}
}

func TestInstallationGateFailsWhileAnEntrySendHoldsAPermit(t *testing.T) {
	f := newGateFixture(t)
	if err := store.ClearMaintenanceHold(f.dir, "job-g"); err != nil {
		t.Fatal(err)
	}
	release, ok := MaintenanceEntryPermit() // a send in flight when the hold lands
	if !ok {
		t.Fatal("fixture: permit refused before the hold")
	}
	defer release()
	setHold(t, f.dir, "job-g")
	g := f.run()
	mustFail(t, g, "in_flight_sends", "1")
	mustFail(t, g, "go_drained", "")
}

func TestInstallationGateAddOnAckCases(t *testing.T) {
	cases := map[string]struct {
		mut  func(w *installationWire)
		want string
	}{
		"old AddOn never acks": {func(w *installationWire) { w.Rec.Ack, w.HasAck = nil, false }, "n/a"},
		"ack for another job":  {func(w *installationWire) { w.Rec.Ack.JobID = "job-old" }, "job-old"},
		"ack says not held":    {func(w *installationWire) { w.Rec.Ack.Held = false }, "not held"},
		"stale ack":            {func(w *installationWire) { w.AckAge = ntwire.MaintenanceAckMaxAge() + time.Second }, "old"},
		"AddOn disconnected":   {func(w *installationWire) { w.Connected = false }, "not connected"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newGateFixture(t)
			c.mut(&f.wire)
			mustFail(t, f.run(), "addon_ack", c.want)
		})
	}
}

func TestInstallationGateCensusCases(t *testing.T) {
	cases := map[string]struct {
		mut  func(a *ntwire.MaintenanceAckPayload)
		want string
	}{
		"census error": {func(a *ntwire.MaintenanceAckPayload) {
			a.CensusError, a.Connections, a.Accounts = "census failed: X", nil, nil
		}, "census failed"},
		"connections not taken": {func(a *ntwire.MaintenanceAckPayload) { a.Connections = nil }, "connections"},
		"accounts not taken":    {func(a *ntwire.MaintenanceAckPayload) { a.Accounts = nil }, "accounts"},
		"position on a LIVE account": {func(a *ntwire.MaintenanceAckPayload) {
			a.Connections[1].Connected = true // the connected non-SIM connection whose suffix must not survive a refusal
			a.Accounts = append(a.Accounts, ntwire.CensusAccount{Sim: false, Positions: 1})
		}, "open position(s) on a non-SIM (live) account"},
		"working order on a LIVE account": {func(a *ntwire.MaintenanceAckPayload) {
			a.Connections[1].Connected = true // the connected non-SIM connection whose suffix must not survive a refusal
			a.Accounts = append(a.Accounts, ntwire.CensusAccount{Sim: false, Working: 1})
		}, "working order(s) on a non-SIM (live) account"},
		// M2.1 (review d): a connection neither Connected nor Disconnected
		// (Connecting, ConnectionLost …) — its accounts' zeros cannot be trusted.
		"connection in a transitional state": {func(a *ntwire.MaintenanceAckPayload) { a.Connections[1].Settled = false }, "transitional"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newGateFixture(t)
			c.mut(f.wire.Rec.Ack)
			g := f.run()
			mustFail(t, g, "addon_census", c.want)
			// The pre-hold census leg shares the content judgment: a census
			// that names exposure fails BOTH legs (#206 review fold).
			mustFail(t, g, "addon_census_prehold", c.want)
			// D0: a REFUSAL must never read "all accounts flat — allowed" —
			// the suffix belongs to the pass path only.
			if l, ok := legOf(g, "addon_census"); ok && strings.Contains(l.Detail, "all accounts flat") {
				t.Fatalf("a refusal detail must not claim all-accounts-flat: %q", l.Detail)
			}
		})
	}
}

// UPDATER-FLAT-LIVE-OK (owner ruling 2026-09-28): a connected non-SIM (live)
// connection no longer fails the census BY ITSELF — every account flat (the
// census proves positions=0 and working=0 across ALL accounts, live included)
// means the update may proceed. The updater never disconnects anything; only
// exposure refuses. The detail names the ruling.
func TestInstallationGateConnectedNonSIMFlatPasses(t *testing.T) {
	f := newGateFixture(t)
	f.wire.Rec.Ack.Connections[1].Connected = true // the live connection is up
	f.wire.Rec.Ack.Accounts = append(f.wire.Rec.Ack.Accounts, ntwire.CensusAccount{Sim: false, Positions: 0, Working: 0})
	g := f.run()
	l, ok := legOf(g, "addon_census")
	if !ok || !l.Pass {
		t.Fatalf("a connected non-SIM connection with every account flat must PASS the census: %+v", l)
	}
	for _, want := range []string{"connected_non_SIM=1", "allowed (owner ruling 2026-09-28)"} {
		if !strings.Contains(l.Detail, want) {
			t.Fatalf("detail %q must say %q", l.Detail, want)
		}
	}
	// the pre-hold leg shares censusVerdict — same verdict
	pl, ok := legOf(g, "addon_census_prehold")
	if !ok || !pl.Pass {
		t.Fatalf("the pre-hold census leg must share the verdict: %+v", pl)
	}
}

// The C22 pre-hold probe on a NEVER-HELD connection: the wire has sent no
// maintenance frame (it only sends while held), so the census does not exist
// yet. That absence is a state, not missing evidence — the pre-hold leg must
// PASS (the ledger/planner/trader legs vouch for flat; drain re-checks a fresh
// census after the hold), while the drain leg addon_census must still FAIL on
// the same absent census. (#206 review fold: the old preflight demanded
// addon_census and refused every install on a fresh bot process.)
func TestInstallationGatePreholdCensusOnAFreshConnection(t *testing.T) {
	f := newGateFixture(t)
	f.wire.Rec.Ack, f.wire.HasAck, f.wire.AckAge = nil, false, 0
	g := f.run()
	l, ok := legOf(g, "addon_census_prehold")
	if !ok {
		t.Fatalf("leg addon_census_prehold missing: %+v", g.Legs)
	}
	if !l.Pass {
		t.Fatalf("a never-held connection must pass the pre-hold census leg: %s", l.Detail)
	}
	if !strings.Contains(l.Detail, "never been held") {
		t.Fatalf("the pass must say WHY (the never-held state), got %q", l.Detail)
	}
	mustFail(t, g, "addon_census", "no census")
}

// prehold-stale-census (owner order 2026-10-02 10:15 CT): a census that
// EXISTS but is STALE — the held:false release ack left by the previous
// install's hold, which the wire never refreshes — is never STRONGER evidence
// than no census. A stale census that is FLAT passes exactly like the
// no-census state (the ledger/planner/trader legs vouch for flat; drain
// re-checks a fresh census after the hold), so the second install of one bot
// process no longer demands an NT8 or bot restart.
func TestInstallationGatePreholdCensusStaleFlatPasses(t *testing.T) {
	f := newGateFixture(t)
	f.wire.Rec.Ack.Held = false // a released ack: nothing resends it
	f.wire.AckAge = 3 * time.Hour
	l, ok := legOf(f.run(), "addon_census_prehold")
	if !ok {
		t.Fatalf("leg addon_census_prehold missing: %+v", f.run().Legs)
	}
	if !l.Pass {
		t.Fatalf("a stale flat census must pass exactly like no census: %s", l.Detail)
	}
	for _, want := range []string{"3h0m0s old", "treated as no census", "drain re-checks a fresh census"} {
		if !strings.Contains(l.Detail, want) {
			t.Fatalf("the pass must say WHY (real age + treated-as-no-census), got %q (want %q)", l.Detail, want)
		}
	}
}

// The stale-flat pass only covers FLATNESS: a stale census that shows exposure
// must still fail the pre-hold leg (fail-closed), positions and working alike,
// and the refusal names the counts.
func TestInstallationGatePreholdCensusStaleButExposed(t *testing.T) {
	cases := map[string]struct {
		mut  func(a *ntwire.MaintenanceAckPayload)
		want string
	}{
		"3h-old census with an open position": {func(a *ntwire.MaintenanceAckPayload) { a.Accounts[0].Positions = 1 }, "NOT flat (positions=1 working=0)"},
		"3h-old census with a working order":  {func(a *ntwire.MaintenanceAckPayload) { a.Accounts[0].Working = 1 }, "NOT flat (positions=0 working=1)"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newGateFixture(t)
			f.wire.Rec.Ack.Held = false
			f.wire.AckAge = 3 * time.Hour
			c.mut(f.wire.Rec.Ack)
			mustFail(t, f.run(), "addon_census_prehold", c.want)
		})
	}
}

// A stale census that carries NO account census at all has no flat evidence
// to judge — the refusal must say exactly that (never "not flat", which
// would fabricate a verdict over evidence that does not exist).
func TestInstallationGatePreholdCensusStaleWithoutAnAccountCensus(t *testing.T) {
	cases := map[string]struct {
		mut  func(a *ntwire.MaintenanceAckPayload)
		want string
	}{
		"3h-old census that failed to take": {func(a *ntwire.MaintenanceAckPayload) {
			a.CensusError, a.Connections, a.Accounts = "census failed: X", nil, nil
		}, "carries no account census (census failed: X)"},
		"3h-old census with accounts not enumerated": {func(a *ntwire.MaintenanceAckPayload) {
			a.CensusError, a.Accounts = "", nil
		}, "carries no account census (connections/accounts not enumerated)"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newGateFixture(t)
			f.wire.Rec.Ack.Held = false
			f.wire.AckAge = 3 * time.Hour
			c.mut(f.wire.Rec.Ack)
			mustFail(t, f.run(), "addon_census_prehold", c.want)
		})
	}
}

// A FRESH released census that shows exposure fails the pre-hold leg too:
// content and freshness are both judged (the leg must not pass a stale ack
// through the content check or a bad census through the age check).
func TestInstallationGatePreholdCensusFreshButExposed(t *testing.T) {
	cases := map[string]struct {
		mut  func(a *ntwire.MaintenanceAckPayload)
		want string
	}{
		"fresh census with a working order":  {func(a *ntwire.MaintenanceAckPayload) { a.Accounts[0].Working = 1 }, "working"},
		"fresh census with an open position": {func(a *ntwire.MaintenanceAckPayload) { a.Accounts[0].Positions = 1 }, "position"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newGateFixture(t)
			f.wire.Rec.Ack.Held = false
			c.mut(f.wire.Rec.Ack)
			mustFail(t, f.run(), "addon_census_prehold", c.want)
		})
	}
}

func TestInstallationGateQueuedSignalsFail(t *testing.T) {
	f := newGateFixture(t)
	f.wire.Queued = 1
	mustFail(t, f.run(), "queued_signals", "1")
}

func TestInstallationGateNoWireFailsTheWireLegs(t *testing.T) {
	f := newGateFixture(t)
	installationWireView = func([]*AutoTrader) (installationWire, bool) { return installationWire{}, false }
	g := f.run()
	for _, leg := range []string{"addon_ack", "addon_census", "addon_census_prehold", "queued_signals"} {
		mustFail(t, g, leg, "no NT8")
	}
}

// Leg (a): the UNION of every planner-class claim, any trader.
func TestInstallationGatePlannerUnion(t *testing.T) {
	maps := map[string]func(key string) func(){
		"plannerReadInFlight": func(k string) func() {
			plannerReadInFlight.Store(k, struct{}{})
			return func() { plannerReadInFlight.Delete(k) }
		},
		"weeklyReadClaim": func(k string) func() {
			weeklyReadClaim.Store(k, struct{}{})
			return func() { weeklyReadClaim.Delete(k) }
		},
		"flipRereadInFlight": func(k string) func() {
			flipRereadInFlight.Store(k, time.Now())
			return func() { flipRereadInFlight.Delete(k) }
		},
		"deathRereadInFlight": func(k string) func() {
			deathRereadInFlight.Store(k, time.Now())
			return func() { deathRereadInFlight.Delete(k) }
		},
	}
	for name, claim := range maps {
		t.Run(name, func(t *testing.T) {
			f := newGateFixture(t)
			undo := claim("some-OTHER-trader:" + name)
			defer undo()
			mustFail(t, f.run(), "planner_in_flight", name)
		})
	}
}

// Leg (b): a loaded trader that is not an NT8 TCP trader is unknown coverage.
func TestInstallationGateNonNT8TraderFails(t *testing.T) {
	f := newGateFixture(t)
	other, _ := resetTrader(t, store.StrategyConfig{})
	other.id = "csv-or-crypto"
	f.loaded[other.id] = other
	mustFail(t, f.run(), "traders_nt8", "csv-or-crypto")
}

// U3 + M2.1 (CTO ruling on review item b): the Picture HTF registry never
// unregisters, so it keeps every trader that ever Run() — crypto and removed
// ones too. A REGISTRY-ONLY trader that is not an NT8 TCP trader cannot reach
// NT8 (its evaluator is nil unless the exchange is ninjatrader, and the send
// needs a *TCPTrader), so it is listed as covered and informational — it must
// not keep the gate failing until a restart the update itself needs. A RUNNING
// (manager-loaded) non-NT8 trader still fails the leg.
func TestInstallationGateRegistryOnlyCryptoTraderIsInformational(t *testing.T) {
	f := newGateFixture(t)
	ghost, _ := resetTrader(t, store.StrategyConfig{})
	ghost.id = "picture-ghost-crypto"
	ghost.exchange = "binance"
	pictureHtfTraders.Store(ghost.id, ghost)
	defer pictureHtfTraders.Delete(ghost.id)
	g := f.run()
	l, _ := legOf(g, "traders_nt8")
	if !l.Pass || !strings.Contains(l.Detail, "picture-ghost-crypto") {
		t.Fatalf("a registry-only crypto trader is informational (listed, not failing): %+v", l)
	}
	if !g.Ready {
		t.Fatalf("a registry-only crypto trader must not keep the gate closed: %+v", g.Legs)
	}
	found := false
	for _, id := range g.Traders {
		found = found || id == "picture-ghost-crypto"
	}
	if !found {
		t.Fatalf("the registry-only trader must still be listed as covered: %v", g.Traders)
	}
	// ...while a RUNNING non-NT8 trader in the manager still fails the leg.
	running, _ := resetTrader(t, store.StrategyConfig{})
	running.id = "running-crypto"
	running.exchange = "binance"
	f.loaded[running.id] = running
	mustFail(t, f.run(), "traders_nt8", "running-crypto")
}

// Leg (e): the ledger for EVERY trader id, loaded or not; an authorized but
// unplaced arm is informational.
func TestInstallationGateLedgerExposureAllTraders(t *testing.T) {
	f := newGateFixture(t)
	led := f.st.ArmedOrders()
	arm := func(trader, plan string) int64 {
		r := store.ArmedOrderDB{TraderID: trader, PlanID: plan, Version: 1, Session: "NY", Scenario: "S1", Side: "LONG", EntryPx: 100, StopPx: 95, TargetPx: 110, State: store.StateArmed}
		if err := f.st.ArmedOrders().UpsertArm(&r); err != nil {
			t.Fatal(err)
		}
		return r.ID
	}
	arm("never-loaded", "2026-09-22:NY:never-loaded")
	if g := f.run(); !g.Ready {
		leg, _ := legOf(g, "ledger_exposure")
		t.Fatalf("an unplaced arm is informational, not exposure: %+v", leg)
	}
	id := arm("never-loaded-2", "2026-09-22:NY:never-loaded-2")
	if err := led.BeginPlacement(id, "sig-orphan"); err != nil {
		t.Fatal(err)
	}
	mustFail(t, f.run(), "ledger_exposure", "never-loaded-2")
}

func TestInstallationGatePicturePendingFails(t *testing.T) {
	f := newGateFixture(t)
	if _, _, err := f.st.PictureHtfClaim(&store.PictureHtfOpportunityDB{OppKey: "opp-1", TraderID: "someone", Stage: "confirmed"}); err != nil {
		t.Fatal(err)
	}
	if won, err := f.st.PictureHtfClaimSubmission("opp-1", "picture-htf-1"); err != nil || !won {
		t.Fatalf("fixture: %v %v", won, err)
	}
	mustFail(t, f.run(), "ledger_exposure", "picture")
}

// The production per-trader seam is CutoverGateStatus (canon 53): a trader
// whose own cutover leg fails fails the installation gate, quoting that leg.
// (Leg 4 with no broker snapshot falls back to the ledger by design — F12's
// transition case — so the failing leg here is leg 1, a real open DB row.)
func TestInstallationGateUsesEachTradersCutoverGate(t *testing.T) {
	f := newGateFixture(t)
	installationTraderCutover = func(at *AutoTrader) []CutoverLeg { return at.CutoverGateStatus().Legs }
	if g := f.run(); !g.Ready {
		t.Fatalf("fixture: with the real seam and a flat trader the gate must be ready: %+v", g.Legs)
	}
	if err := f.st.Position().CreateOpenPosition(&store.TraderPosition{
		TraderID: "gate-t1", ExchangeType: "ninjatrader", ExchangePositionID: "gate-pos-1",
		Symbol: "MNQ", Side: "LONG", Quantity: 1, EntryQuantity: 1, EntryPrice: 29000,
		EntryOrderID: "sig-gate", Leverage: 1, Status: "OPEN", Source: "test", Account: "Sim101",
	}); err != nil {
		t.Fatal(err)
	}
	mustFail(t, f.run(), "trader_cutover:gate-t1", "db_open_positions")
}

// (g) never panics: a leg that panics fails ITSELF, the rest still compute.
func TestInstallationGateNeverPanics(t *testing.T) {
	f := newGateFixture(t)
	installationTraderCutover = func(*AutoTrader) []CutoverLeg { panic("boom") }
	g := f.run()
	mustFail(t, g, "trader_cutover:gate-t1", "panicked")
	if _, ok := legOf(g, "addon_census"); !ok {
		t.Fatal("the other legs must still be computed")
	}
}

// A hold file the process cannot read HOLDS (entries stay refused) but the
// update must NOT proceed on it: no job can be bound to it (the AddOn acks job
// "" and a job-scoped clear refuses it). Found by the M2 adversarial review:
// the gate returned ready=true job_id=n/a with every leg passing.
func TestInstallationGateFailsOnAnUnreadableHold(t *testing.T) {
	f := newGateFixture(t)
	if err := writeRaw(f.dir, "{broken"); err != nil {
		t.Fatal(err)
	}
	f.wire.Rec.Ack.JobID = "" // what the AddOn acks for a corrupt hold
	g := f.run()
	mustFail(t, g, "hold", "unreadable")
	mustFail(t, g, "addon_ack", "")
}

// The AddOn reports its command-queue depth; anything above zero is work in
// flight at the AddOn (PROTOCOL.md: an async dispatcher must report it).
func TestInstallationGateFailsOnAddOnQueuedCommands(t *testing.T) {
	f := newGateFixture(t)
	f.wire.Rec.Ack.QueuedCommands = 2
	mustFail(t, f.run(), "addon_ack", "queued_commands=2")
}

// A panic outside any single leg (the wire read, the registry scan) still
// yields a failed verdict, never a crash.
func TestInstallationGateSurvivesAPanicOutsideALeg(t *testing.T) {
	f := newGateFixture(t)
	installationWireView = func([]*AutoTrader) (installationWire, bool) { panic("wire boom") }
	g := f.run()
	if g.Ready {
		t.Fatalf("a panicking gate must not be ready: %+v", g)
	}
	found := false
	for _, l := range g.Legs {
		found = found || (!l.Pass && strings.Contains(l.Detail, "panicked"))
	}
	if !found {
		t.Fatalf("the panic must be reported as a failed leg: %+v", g.Legs)
	}
}

// M2.1 (review 3 F4, CTO: FIRST) — the FALSE-PASS direction, at the production
// path: no stubbed wire view. A real server + TCPTrader; a client connects,
// acks the hold with a clean census, and addon_ack passes; the client
// DISCONNECTS and addon_ack must FAIL at once — never READY on the last ack of
// a connection that is gone (mutations: the view forcing Connected, or
// ConnectionRecord always reporting connected).
func TestInstallationGateAddOnAckFailsTheMomentTheAddOnDisconnects(t *testing.T) {
	w := newDropWire(t) // real server, real TCPTrader, wireNT8Maintenance
	setHold(t, w.dir, "job-f4")
	loaded := map[string]*AutoTrader{w.at.id: w.at}

	c := dialRaw(t, w.addr)
	if err := ntwire.WriteFrame(c, ntwire.FrameHello, ntwire.HelloPayload{ProtocolVersion: ntwire.ProtocolVersion, Source: "vltrader-addon"}); err != nil {
		t.Fatal(err)
	}
	ack := ntwire.MaintenanceAckPayload{Held: true, JobID: "job-f4", BuildID: "2026-09-22-m2",
		Connections: []ntwire.CensusConnection{{Sim: true, Connected: true, Settled: true}},
		Accounts:    []ntwire.CensusAccount{{Sim: true}}}
	if err := ntwire.WriteFrame(c, ntwire.FrameMaintenanceAck, ack); err != nil {
		t.Fatal(err)
	}
	var pass InstallationGateLeg
	waitDrop(t, "addon_ack to pass on the live connection", 3*time.Second, func() bool {
		pass, _ = legOf(InstallationGateStatus(loaded, w.st), "addon_ack")
		return pass.Pass
	})

	_ = c.Close()
	waitDrop(t, "the server to see the disconnect", 3*time.Second, func() bool {
		_, connected, _, _ := w.nt.MaintenanceView()
		return !connected
	})
	l, _ := legOf(InstallationGateStatus(loaded, w.st), "addon_ack")
	if l.Pass || !strings.Contains(l.Detail, "not connected") {
		t.Fatalf("after the AddOn disconnects, addon_ack must FAIL (it passed on the last ack of a gone connection): %+v", l)
	}
}

// M2.1 (review 3 F5): EVERY per-trader cutover leg the gate takes (1, 2, 4)
// reaches the verdict through the real seam — not only leg 1.
func TestInstallationGateCarriesCutoverLegs2And4(t *testing.T) {
	t.Run("leg 2 api_positions", func(t *testing.T) {
		f := newGateFixture(t)
		installationTraderCutover = func(at *AutoTrader) []CutoverLeg { return at.CutoverGateStatus().Legs }
		s := ntwire.NewTCPServer(nil)
		s.SeedPositionsForTest("Sim101", []ntwire.OpenPosition{{Symbol: "MNQ", Side: "long", Quantity: 1, AvgPrice: 29000}})
		f.loaded["gate-t1"].trader = ntTrader.NewTCPTrader(s, "MNQ", "Sim101")
		mustFail(t, f.run(), "trader_cutover:gate-t1", "api_positions")
	})
	t.Run("leg 4 working_orders", func(t *testing.T) {
		f := newGateFixture(t)
		installationTraderCutover = func(at *AutoTrader) []CutoverLeg { return at.CutoverGateStatus().Legs }
		r := store.ArmedOrderDB{TraderID: "gate-t1", PlanID: "2026-09-22:NY:gate-t1", Version: 1, Session: "NY", Scenario: "S1",
			Side: "LONG", EntryPx: 100, StopPx: 95, TargetPx: 110, State: store.StateArmed}
		if err := f.st.ArmedOrders().UpsertArm(&r); err != nil {
			t.Fatal(err)
		}
		if err := f.st.ArmedOrders().BeginPlacement(r.ID, "sig-leg4"); err != nil {
			t.Fatal(err)
		}
		mustFail(t, f.run(), "trader_cutover:gate-t1", "working_orders")
	})
}
