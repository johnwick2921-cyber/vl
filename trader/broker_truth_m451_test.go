package trader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/internal/datatruth"
	"vl/internal/datatruth/checks"
	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// release #55 m451 - broker_agreement + fill_price pins.
//
// Every pin drives the PRODUCTION call sites: datatruth.Evaluate over the checks
// RegisterBrokerAgreement / RegisterFillPrice registered, REAL AutoTraders
// registered the way Run registers them, REAL stores and TCP caches, and the
// entry admission chain (admitEntry) that every entry path crosses.
//
// Mutants (each shown RED, then restored):
//   M1  brokerAgreementCheck returns no scope (a whole-symbol block) -> scope pins RED
//   M2  dataTruthReasons drops the AppliesTo filter                  -> scope pins RED
//   M3  brokerAgreementVerdict resets + reports clear when closed    -> reopen pin RED
//   M4  PosSkip not set (old snap=nil) / hold nil                    -> held-position pin RED
//   M5  unregisterBrokerAgreement does a plain Delete                -> registry pin RED
//   M6  agreementRemedy appended nowhere                             -> remedy pins RED
//   M7  RegisterFillPrice skips EnableState                          -> restart pin RED

// scopedAT is one running NT8 trader bound to its own account.
type scopedAT struct {
	at   *AutoTrader
	st   *store.Store
	srv  *ntwire.TCPServer
	tr   *ntTrader.TCPTrader
	acct string
}

func newScopedAT(t *testing.T, id, acct string) *scopedAT {
	t.Helper()
	at, st := pauseTrader(t)
	at.id = id
	at.isRunning = true
	at.config.StrategyConfig = &store.StrategyConfig{DayPlan: &store.DayPlanConfig{PlanEnabled: true}}
	s := ntwire.NewTCPServer(nil)
	s.SetAccountsList([]ntwire.AccountInfo{{Name: acct, IsSim: true}}, acct)
	tr := ntTrader.NewTCPTrader(s, "MNQ", acct)
	at.trader = tr
	f := &scopedAT{at: at, st: st, srv: s, tr: tr, acct: acct}
	f.positions()
	f.book(agreeT0.Add(-5 * time.Second))
	at.registerBrokerAgreement()
	t.Cleanup(at.unregisterBrokerAgreement)
	return f
}

func (f *scopedAT) positions(ps ...ntwire.OpenPosition) {
	f.srv.SeedPositionsForTestAt(f.acct, ps, agreeT0.Add(-5*time.Second))
}

func (f *scopedAT) book(at time.Time) {
	f.srv.OrderSnapshots().PutAt(ntwire.OrderSnapshotPayload{Account: f.acct, BuildID: "t"}, at)
}

// armScopedGate turns the trader-side gate on over a clean registry with the REAL
// broker_agreement check registered and a fresh fill_price guard.
func armScopedGate(t *testing.T) *checks.FillGuard {
	t.Helper()
	armDataTruthGate(t, "zz_probe") // wired flag, ready edge, clean registry; the probe never blocks
	RegisterBrokerAgreement()
	g := checks.NewFillGuard(checks.DefaultFillParams(), nil)
	saved := checks.SetDefaultFills(g)
	t.Cleanup(func() { checks.SetDefaultFills(saved) })
	checks.RegisterFillPrice(g)
	return g
}

// admitAs is the entry admission's data-integrity leg for the AI-decision path:
// exactly what admitChain asks (dataIntegrityAdmitRefusal -> dataIntegrityVerdictAt
// -> dataTruthVerdictAt). The earlier gates of admitChain (the dead-man watchdog,
// the pause table ...) are other pins' business and are not what these assert.
func admitAs(at *AutoTrader, now time.Time) (string, bool) {
	_, text, refused := at.dataIntegrityAdmitRefusal(admitIntent{Path: admitDecision, Symbol: "MNQ", Action: "open_long", Now: now, Key: "k"}, now)
	return text, refused
}

func mustRefuseData(t *testing.T, who string, at *AutoTrader, now time.Time, want ...string) {
	t.Helper()
	reason, refused := admitAs(at, now)
	if !refused || !strings.HasPrefix(reason, "data_truth: ") {
		t.Fatalf("%s must be refused by the data-truth gate: refused=%v %q", who, refused, reason)
	}
	for _, w := range want {
		if !strings.Contains(reason, w) {
			t.Fatalf("%s: the refusal %q must say %q", who, reason, w)
		}
	}
}

func mustNotRefuseData(t *testing.T, who string, at *AutoTrader, now time.Time) {
	t.Helper()
	if reason, refused := admitAs(at, now); refused && strings.HasPrefix(reason, "data_truth") {
		t.Fatalf("%s must NOT be paused by another account's disagreement: %q", who, reason)
	}
}

// ── P2: broker_agreement pauses the disagreeing trader, not the symbol ───────

func TestM451BrokerAgreementPausesOnlyTheDisagreeingTrader(t *testing.T) {
	armScopedGate(t)
	a := newScopedAT(t, "m451-A", "Sim101")
	b := newScopedAT(t, "m451-B", "SimAccount1")
	// account A: NT8 holds a SHORT the bot has no row for. Account B agrees.
	a.positions(ntwire.OpenPosition{Symbol: "MNQ", Side: "SHORT", Quantity: 1, AvgPrice: 29310})
	if _, refused := admitAs(a.at, agreeT0); refused {
		t.Fatal("first sighting is inside the settle window")
	}
	later := agreeT0.Add(agreementSettle + time.Second)
	a.book(later.Add(-5 * time.Second))
	b.book(later.Add(-5 * time.Second))
	mustRefuseData(t, "trader A", a.at, later, "broker_agreement", "trader m451-A", "NT8 holds MNQ SHORT")
	mustNotRefuseData(t, "trader B", b.at, later)
	// the datatruth reason itself says who it is for (the health scopes read it)
	if st := datatruth.Snapshot()[CheckBrokerAgreement]; !st.Blocked || len(st.Scopes) != 1 || st.Scopes[0] != datatruth.ScopeTrader("m451-A") {
		t.Fatalf("the standing block must be scoped to trader A: %+v", st)
	}
}

// A trader that CANNOT JUDGE (its own order book went stale) is blocked on its
// own; the healthy trader on the same symbol is not.
func TestM451BrokerAgreementCannotJudgeBlocksOnlyThatTrader(t *testing.T) {
	armScopedGate(t)
	a := newScopedAT(t, "m451-A", "Sim101")
	b := newScopedAT(t, "m451-B", "SimAccount1")
	now := agreeT0.Add(10 * time.Minute)
	a.positions()
	b.positions()
	a.srv.SeedPositionsForTestAt("Sim101", nil, now.Add(-5*time.Second))
	b.srv.SeedPositionsForTestAt("SimAccount1", nil, now.Add(-5*time.Second))
	b.book(now.Add(-5 * time.Second)) // B's book is fresh; A's is 10 minutes old
	mustRefuseData(t, "trader A (stale book)", a.at, now, "broker_agreement", "cannot judge")
	mustNotRefuseData(t, "trader B", b.at, now)
}

// A trader that is not under the check at all (stopped, never started) is not
// admitted on the silence of the others' checks while the CME is open.
func TestM451UnregisteredAskerIsNotAdmittedOnOthersSilence(t *testing.T) {
	armScopedGate(t)
	a := newScopedAT(t, "m451-A", "Sim101")
	b := newScopedAT(t, "m451-B", "SimAccount1")
	b.at.unregisterBrokerAgreement()
	now := agreeT0.Add(time.Second)
	mustNotRefuseData(t, "trader A", a.at, now)
	mustRefuseData(t, "the unregistered trader B", b.at, now, "not registered with the broker check")
}

// ── P2: fill_price strikes are per account ───────────────────────────────────

func farRing(m int64) checks.RingFn {
	return func(string) ([]checks.Bar, error) {
		return []checks.Bar{
			{T: m, O: 29000, H: 29005, L: 29000, C: 29005, V: 1, Final: true},
			{T: m + 60_000, O: 29000, H: 29005, L: 29000, C: 29005, V: 1, Final: true},
		}, nil
	}
}

func TestM451FillPriceStrikesPauseOnlyTheAccountThatPrintedThem(t *testing.T) {
	g := armScopedGate(t)
	a := newScopedAT(t, "m451-A", "Sim101")
	b := newScopedAT(t, "m451-B", "SimAccount1")
	now := agreeT0.Add(time.Second)
	m := now.UnixMilli() / 60_000 * 60_000
	far := func(id, acct string) {
		g.Record(checks.FillObs{ID: id, Event: id, Account: acct, Symbol: "MNQ", Kind: "exit", Side: "LONG", Price: 29100, AtMs: m + 5_000, Tick: 0.25}, farRing(m), now)
	}
	// one far event on each account: nobody is paused
	far("e1", "Sim101")
	far("e2", "SimAccount1")
	mustNotRefuseData(t, "trader A (1 strike)", a.at, now)
	mustNotRefuseData(t, "trader B (1 strike)", b.at, now)
	// a second far event on Sim101 pauses Sim101's trader, not SimAccount1's
	far("e3", "Sim101")
	mustRefuseData(t, "trader A (2 strikes)", a.at, now, "fill_price", "account SIM101", "ack-fill-price?symbol=MNQ&account=SIM101")
	mustNotRefuseData(t, "trader B (1 strike)", b.at, now)
	// the account-scoped acknowledgement lifts A and nothing else
	if !g.AckAccount("MNQ", "Sim101") {
		t.Fatal("ack must report the standing pause")
	}
	mustNotRefuseData(t, "trader A (acked)", a.at, now)
}

// ── P3: standing disagreements survive a closed market ───────────────────────

func TestM451StandingDisagreementIsStillBlockingAtTheReopen(t *testing.T) {
	armScopedGate(t)
	a := newScopedAT(t, "m451-A", "Sim101")
	a.positions(ntwire.OpenPosition{Symbol: "MNQ", Side: "SHORT", Quantity: 1, AvgPrice: 29310})
	if _, refused := admitAs(a.at, agreeT0); refused {
		t.Fatal("first sighting is inside the settle window")
	}
	blockAt := agreeT0.Add(agreementSettle + time.Second)
	a.book(blockAt.Add(-5 * time.Second))
	mustRefuseData(t, "trader A", a.at, blockAt, "broker_agreement")

	// the daily break: closed ticks neither judge nor forget
	closed := time.Date(2026, 10, 26, 16, 30, 0, 0, kernel.CTLocation())
	if kernel.IsCMEOpen(closed) {
		t.Fatal("fixture: 16:30 CT must be the daily break")
	}
	if b, _ := datatruth.Evaluate("MNQ", closed); !b {
		t.Fatal("a standing block is carried through the closed market, not reported clear")
	}
	// the reopen: the books still disagree, and the FIRST open tick blocks at once -
	// no new 60 s window to hide in.
	reopen := time.Date(2026, 10, 26, 17, 0, 5, 0, kernel.CTLocation())
	a.srv.SeedPositionsForTestAt("Sim101", []ntwire.OpenPosition{{Symbol: "MNQ", Side: "SHORT", Quantity: 1, AvgPrice: 29310}}, reopen.Add(-5*time.Second))
	a.book(reopen.Add(-5 * time.Second))
	mustRefuseData(t, "trader A at the reopen", a.at, reopen, "broker_agreement", "disagreeing for")
	// and it ends on a MEASURED agreement, not on the clock
	a.srv.SeedPositionsForTestAt("Sim101", nil, reopen.Add(10*time.Second))
	a.book(reopen.Add(10 * time.Second))
	mustNotRefuseData(t, "trader A once the books agree", a.at, reopen.Add(15*time.Second))
}

// ... and across the one call that cannot read the positions frame because an
// entry just landed.
func TestM451StandingPositionDisagreementSurvivesTheEntryJustLandedCall(t *testing.T) {
	armScopedGate(t)
	a := newScopedAT(t, "m451-A", "Sim101")
	a.positions(ntwire.OpenPosition{Symbol: "MNQ", Side: "SHORT", Quantity: 1, AvgPrice: 29310})
	admitAs(a.at, agreeT0)
	blockAt := agreeT0.Add(agreementSettle + time.Second)
	a.book(blockAt.Add(-5 * time.Second))
	mustRefuseData(t, "trader A", a.at, blockAt, "broker_agreement")
	// an entry receipt newer than the positions frame: this call cannot compare
	a.srv.NoteEntryExecution("MNQ", "Sim101", "sig-new", 1)
	at2 := blockAt.Add(5 * time.Second)
	a.book(at2.Add(-5 * time.Second))
	mustRefuseData(t, "trader A on the entry-just-landed call", a.at, at2, "broker_agreement", "not re-measured")
}

// ── P3: the pause text names a remedy that works ─────────────────────────────

func TestM451PauseTextNamesAWorkingRemedy(t *testing.T) {
	f := newAgreeFx(t)
	// quantity divergence
	f.row("LONG", 1, agreeT0.Add(-10*time.Minute))
	f.positions(ntwire.OpenPosition{Symbol: "MNQ", Side: "LONG", Quantity: 2, AvgPrice: 29000})
	mustClear(t, f, agreeT0, "first sighting")
	mustBlock(t, f, agreeT0.Add(agreementSettle+time.Second), "REMEDY", "close the position", "does NOT lift this pause")
}

func TestM451EveryDisagreementKindHasARemedy(t *testing.T) {
	for _, k := range []string{"pos:qty:1", "pos:orphan:1", "pos:untracked:MNQ|LONG", "ord:unknown:x", "ord:zombie:x", "ord:gone:1", "bind:not_offered:Sim101", "bind:row_acct:1"} {
		if r := agreementRemedy(k); !strings.HasPrefix(r, "REMEDY: ") {
			t.Fatalf("disagreement kind %q has no owner remedy: %q", k, r)
		}
	}
	// every key judgeAgreement can emit is one of the kinds above
	src, err := os.ReadFile("broker_agreement.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{`"bind:not_offered:"`, `"bind:row_acct:%d"`, `"pos:orphan:%d"`, `"pos:qty:%d"`, `"pos:untracked:"`, `"ord:gone:%d"`, `"ord:zombie:"`, `"ord:unknown:"`} {
		if !strings.Contains(string(src), p) {
			t.Fatalf("judgeAgreement no longer emits %s - update agreementRemedy and this list together", p)
		}
	}
}

// ── P3: compare-and-delete ───────────────────────────────────────────────────

func TestM451UnregisterOfAnOlderInstanceKeepsTheNewerOne(t *testing.T) {
	armScopedGate(t)
	old := newScopedAT(t, "m451-same", "Sim101")
	newer := newScopedAT(t, "m451-same", "Sim101") // the same trader id started again
	if !newer.at.brokerAgreementRegistered() || old.at.brokerAgreementRegistered() {
		t.Fatal("fixture: the newer instance owns the id")
	}
	old.at.unregisterBrokerAgreement() // the older instance's late teardown
	if !newer.at.brokerAgreementRegistered() {
		t.Fatal("an older instance's teardown removed the newer instance from the check")
	}
	b, rs := datatruth.Evaluate("MNQ", agreeT0.Add(time.Second))
	for _, r := range rs {
		if strings.Contains(r.Detail, "no started NT8 trader is registered") {
			t.Fatalf("the check lost its trader: %v %+v", b, r)
		}
	}
}

// ── P3: the fill_price strikes survive a restart (RegisterFillPrice wiring) ──

func TestM451FillPriceStrikesAndAckSurviveARestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data", "vl.db.fillprice.json")
	prevPath, _ := fillPriceStatePath.Load().(string)
	SetFillPriceStatePath(path)
	t.Cleanup(func() { SetFillPriceStatePath(prevPath) })
	savedDefault := checks.DefaultFills()
	t.Cleanup(func() { checks.SetDefaultFills(savedDefault) })

	now := agreeT0
	boot := func() *checks.FillGuard { // a process start: a fresh guard, the boot wiring
		datatruth.Reset()
		g := checks.NewFillGuard(checks.DefaultFillParams(), nil)
		// The fixture lives on the fixed agreeT0 day; the guard's 96 h state age
		// cutoff must read the SAME clock, or the fixture ages out of the file as
		// real time passes (it did, 2026-10-30 00:00).
		g.SetClock(func() time.Time { return now })
		checks.SetDefaultFills(g)
		RegisterFillPrice() // the production boot call (RegisterBrokerTruthChecks -> here)
		return g
	}
	t.Cleanup(datatruth.Reset)
	m := now.UnixMilli() / 60_000 * 60_000
	g1 := boot()
	for _, id := range []string{"e1", "e2"} {
		g1.Record(checks.FillObs{ID: id, Event: id, Account: "Sim101", Symbol: "MNQ", Kind: "exit", Price: 29100, AtMs: m + 5_000, Tick: 0.25}, farRing(m), now)
	}
	if b, _ := datatruth.Evaluate("MNQ", now); !b {
		t.Fatal("two far events pause")
	}
	g1.Flush(5 * time.Second) // m55r3: the state write is off the exit path

	g2 := boot() // kill -9 + restart: nothing in memory
	if b, rs := datatruth.Evaluate("MNQ", now); !b {
		t.Fatalf("a standing, unacknowledged pause must survive the restart: %+v (%+v)", rs, g2.Stats())
	}
	if !g2.Ack("MNQ") {
		t.Fatal("ack")
	}
	boot() // another restart: the acknowledgement survives too
	if b, rs := datatruth.Evaluate("MNQ", now); b {
		t.Fatalf("an acknowledged pause must stay acknowledged across a restart: %+v", rs)
	}
}
