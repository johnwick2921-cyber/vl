package trader

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ntTrader "vl/trader/ninjatrader"
)

// ── W-ONE-BUTTON M2 — THE INSTALLATION-WIDE GATE ───────────────────────────
//
// One read-only verdict for the whole installation: may an update proceed past
// its drain step? The class-33 cutover gate answers for ONE trader; an update
// replaces the binary every trader shares and the AddOn every account shares,
// so this gate answers for all of them at once:
//
//	hold              the hold file is present and held (a corrupt file holds)
//	go_drained        the entry barrier is engaged and no entry send holds a permit
//	in_flight_sends   the permit count, quoted on its own
//	queued_signals    nothing waits in the wire's reconnect queue
//	planner_in_flight NO planner-class read is claimed, any trader: the UNION of
//	                  plannerReadInFlight ∪ weeklyReadClaim ∪ flipRereadInFlight
//	                  ∪ deathRereadInFlight
//	traders_nt8       every loaded trader (TraderManager ∪ the Picture HTF
//	                  registry, which never unregisters — gap U3) is an NT8 TCP
//	                  trader; anything else is coverage we cannot see (CSV
//	                  transport, other brokers). Replaces cutover leg 3's
//	                  vacuous "not an NT8 trader — not applicable" pass.
//	addon_ack         the CURRENT connection acked THIS job, held, recently
//	addon_census      the AddOn's census (all accounts, CTO ruling Q1): taken,
//	                  every connection settled, no position and no working
//	                  order of any action on any account — a connected
//	                  non-SIM (live) connection is ALLOWED when every account
//	                  is flat (owner ruling 2026-09-28); the updater never
//	                  disconnects anything
//	ledger_exposure   no placed/unconfirmed armed row and no unresolved Picture
//	                  send for ANY trader id, loaded or not (canonical arm-state
//	                  predicates); authorized-but-unplaced arms are informational
//	trader_cutover:*  each NT8 trader's own cutover legs 1, 2 and 4
//
// Every leg quotes its source; a leg that cannot be evaluated FAILS; a panic in
// one leg fails that leg and the rest are still computed.

// InstallationGateLeg is one leg's verdict.
type InstallationGateLeg struct {
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail"`
	Source string `json:"source"`
}

// InstallationGate is the whole verdict. Ready only when every leg passes.
type InstallationGate struct {
	Ready   bool                  `json:"ready"`
	JobID   string                `json:"job_id"` // "n/a" when no well-formed hold names one
	Legs    []InstallationGateLeg `json:"legs"`
	Traders []string              `json:"traders"` // every trader id the gate covered
	Note    string                `json:"note"`
	// NT8Absent is the nt8_absent verdict (UPDATER-NT8-CLOSED): present when
	// an NT8 TCP trader exists to measure the link; eligible when the link
	// has been down ≥ nt8AbsentMinLinkDown continuously, measured from the
	// server's per-connection record (never inferred from a stale ack): the
	// closing connection's disconnect stamp, or — when no AddOn has connected
	// since the listener came up (a bot booted with NT8 closed) — the moment the
	// listener came up.
	// Ready only when eligible AND every ledger leg passes on its own
	// evidence. Legs stay ABSENT (not []) when not eligible — an uncomputed
	// leg list is absent, never fabricated (canon 49/53).
	NT8Absent *NT8AbsentView `json:"nt8_absent,omitempty"`
}

// NT8AbsentView is the nt8_absent verdict.
type NT8AbsentView struct {
	Eligible      bool                  `json:"eligible"`
	Ready         bool                  `json:"ready"`
	LinkDownSince string                `json:"link_down_since,omitempty"` // RFC3339 of the disconnect stamp; absent when unknown
	Legs          []InstallationGateLeg `json:"legs,omitempty"`            // computed ONLY when eligible; absent otherwise
}

// plannerClaimStarts returns the OLDEST claim-started wall time across the
// four planner in-flight maps, or ok=false when nothing is claimed or no claim
// carried a time (F1). The claim sites (claimPlannerRead / claimWeeklyRead /
// flipRereadInFlight / deathRereadInFlight) store time.Time; a held claim with
// no time (a test fixture storing the old struct{}/bool shape) is tolerated —
// its identity is reported by the leg, the started time is simply absent.
func plannerClaimStarts() (oldest time.Time, ok bool) {
	for _, m := range []*sync.Map{&plannerReadInFlight, &weeklyReadClaim, &flipRereadInFlight, &deathRereadInFlight} {
		m.Range(func(_, v any) bool {
			if t, isTime := v.(time.Time); isTime && (!ok || t.Before(oldest)) {
				oldest, ok = t, true
			}
			return true
		})
	}
	return oldest, ok
}

// nt8AbsentMinLinkDown is the continuous link-down the absent path requires
// (UPDATER-NT8-CLOSED item 1a): the AddOn must have been disconnected for at
// least this long, measured from the server's per-connection record. Shorter
// than this and the verdict is not eligible — a link that just dropped is not
// proof NT8 is closed, it is proof the link dropped.
const nt8AbsentMinLinkDown = 60 * time.Second

// installationWire is what the gate reads from the NT8 wire.
type installationWire struct {
	Rec                ntwire.ConnectionRecord
	Connected          bool
	Queued             int
	HasAck             bool
	AckAge             time.Duration
	DisconnectedAt     time.Time
	HaveDisconnectedAt bool
}

// installationWireView reads the wire through the first NT8 TCP trader (all of
// them share one server). A seam so the leg logic is testable without NT8.
var installationWireView = func(nts []*AutoTrader) (installationWire, bool) {
	for _, at := range nts {
		nt, ok := at.trader.(*ntTrader.TCPTrader)
		if !ok {
			continue
		}
		rec, connected, queued, ok := nt.MaintenanceView()
		if !ok {
			continue
		}
		age, has := rec.AckAge()
		wire := installationWire{Rec: rec, Connected: connected, Queued: queued, HasAck: has, AckAge: age}
		// The link-down stamp: the closing connection's disconnect stamp, or —
		// for a bot that has NEVER seen the AddOn connect since its listener
		// came up (booted with NT8 closed) — the moment the listener came up.
		if dat, ok := rec.LinkDownSince(connected); ok {
			wire.DisconnectedAt, wire.HaveDisconnectedAt = dat, true
		}
		return wire, true
	}
	return installationWire{}, false
}

// installationTraderCutover is each NT8 trader's own cutover verdict (canon
// 53: the production seam is CutoverGateStatus, pinned by a test).
var installationTraderCutover = func(at *AutoTrader) []CutoverLeg { return at.CutoverGateStatus().Legs }

// InstallationGateStatus computes the verdict. loaded is the TraderManager's
// set; st is the store (ledger reads). Read-only; never panics.
func InstallationGateStatus(loaded map[string]*AutoTrader, st *store.Store) (g InstallationGate) {
	g = InstallationGate{JobID: "n/a"}
	// A panic OUTSIDE any single leg (the wire read, the registry scan) still
	// yields a failed verdict — never a crash, never a pass.
	defer func() {
		if r := recover(); r != nil {
			g.Ready = false
			g.Legs = append(g.Legs, InstallationGateLeg{Name: "gate", Pass: false, Source: "InstallationGateStatus",
				Detail: fmt.Sprintf("gate panicked outside a leg: %v — fail-closed", r)})
		}
	}()
	leg := func(name, source string, fn func() (bool, string)) {
		l := InstallationGateLeg{Name: name, Source: source}
		func() {
			defer func() {
				if r := recover(); r != nil {
					l.Pass, l.Detail = false, fmt.Sprintf("leg panicked: %v — fail-closed", r)
				}
			}()
			l.Pass, l.Detail = fn()
		}()
		g.Legs = append(g.Legs, l)
	}

	// The covered set: TraderManager ∪ Picture HTF registry (U3), by id.
	all := map[string]*AutoTrader{}
	registryOnly := []string{}
	for id, at := range loaded {
		if at != nil {
			all[id] = at
		}
	}
	pictureHtfTraders.Range(func(k, v any) bool {
		id, _ := k.(string)
		at, _ := v.(*AutoTrader)
		if at != nil && all[id] == nil {
			all[id] = at
			registryOnly = append(registryOnly, id)
		}
		return true
	})
	ids := make([]string, 0, len(all))
	for id := range all {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	g.Traders = ids
	var nts []*AutoTrader
	for _, id := range ids {
		if _, ok := all[id].trader.(*ntTrader.TCPTrader); ok {
			nts = append(nts, all[id])
		}
	}

	// hold
	st0, configured := maintenanceState()
	holdJob := ""
	holdLeg := func() (bool, string) {
		switch {
		case !configured:
			return false, "maintenance data dir not configured — the hold cannot be read"
		case !st0.Held:
			return false, "no hold present (an update must write the hold before it drains)"
		case st0.Corrupt:
			// It HOLDS (every entry stays refused) but no job can be bound to
			// it: the AddOn acks job "" and a job-scoped clear refuses it. An
			// update may not proceed on a hold it cannot identify.
			return false, maintenanceReason(st0) + "; no job can be bound to it — repair it or force-clear it with the operator CLI before an update proceeds"
		}
		holdJob = st0.Hold.JobID
		g.JobID = holdJob
		return true, maintenanceReason(st0)
	}
	leg("hold", "the installation hold file (store.ReadMaintenanceHold)", holdLeg)

	// go_drained + in_flight_sends
	inFlight := MaintenanceInFlight()
	drainedLeg := func() (bool, string) {
		if !maintenanceBarrier.Held() {
			return false, "entry barrier not engaged"
		}
		if inFlight != 0 {
			return false, fmt.Sprintf("%d entry send(s) still hold a permit", inFlight)
		}
		return true, "barrier engaged, 0 permits held"
	}
	leg("go_drained", "EntryBarrier (trader/maintenance_gate.go)", drainedLeg)
	inFlightLeg := func() (bool, string) {
		return inFlight == 0, fmt.Sprintf("in_flight_sends=%d", inFlight)
	}
	leg("in_flight_sends", "EntryBarrier in-flight counter", inFlightLeg)

	// wire-read legs
	wire, haveWire := installationWireView(nts)
	noWire := "no NT8 TCP trader loaded — the AddOn cannot be asked (fail-closed)"
	queuedLeg := func() (bool, string) {
		if !haveWire {
			return false, noWire
		}
		return wire.Queued == 0, fmt.Sprintf("queued_signals=%d", wire.Queued)
	}
	leg("queued_signals", "TCPServer reconnect queue", queuedLeg)

	// planner_in_flight — the union
	plannerLeg := func() (bool, string) {
		var held []string
		for _, m := range []struct {
			name string
			keys func() []string
		}{
			{"plannerReadInFlight", func() []string { return syncMapKeys(&plannerReadInFlight) }},
			{"weeklyReadClaim", func() []string { return syncMapKeys(&weeklyReadClaim) }},
			{"flipRereadInFlight", func() []string { return syncMapKeys(&flipRereadInFlight) }},
			{"deathRereadInFlight", func() []string { return syncMapKeys(&deathRereadInFlight) }},
		} {
			for _, k := range m.keys() {
				held = append(held, m.name+"["+k+"]")
			}
		}
		if len(held) > 0 {
			inFlight := strings.Join(held, ", ")
			if start, ok := plannerClaimStarts(); ok {
				return false, fmt.Sprintf("waiting for the AI plan (started %s) — IN FLIGHT: %s", kernel.ClockCTSeconds(start), inFlight)
			}
			return false, "waiting for the AI plan — IN FLIGHT: " + inFlight
		}
		return true, "no planner-class read claimed, any trader"
	}
	leg("planner_in_flight", "plannerReadInFlight ∪ weeklyReadClaim ∪ flipRereadInFlight ∪ deathRereadInFlight", plannerLeg)

	// traders_nt8
	leg("traders_nt8", "TraderManager ∪ pictureHtfTraders", func() (bool, string) {
		registry := map[string]bool{}
		for _, id := range registryOnly {
			registry[id] = true
		}
		var bad, inert []string
		for _, id := range ids {
			if _, ok := all[id].trader.(*ntTrader.TCPTrader); ok {
				continue
			}
			if registry[id] {
				// M2.1 (CTO ruling on review item b): registry-only and not NT8
				// — it cannot reach NT8 (no evaluator off ninjatrader, and the
				// send needs a *TCPTrader). Listed, never failing: the registry
				// never unregisters, so failing here would hold the gate shut
				// until a restart the update itself needs.
				inert = append(inert, id)
				continue
			}
			bad = append(bad, fmt.Sprintf("%s (%T)", id, all[id].trader))
		}
		extra := ""
		if len(registryOnly) > 0 {
			sort.Strings(registryOnly)
			extra = "; registry-only (not in the manager): " + strings.Join(registryOnly, ", ")
		}
		if len(inert) > 0 {
			sort.Strings(inert)
			extra += "; of which not NT8 (cannot reach NT8, informational): " + strings.Join(inert, ", ")
		}
		if len(bad) > 0 {
			return false, "not an NT8 TCP trader — coverage unknown: " + strings.Join(bad, ", ") + extra
		}
		return true, fmt.Sprintf("%d trader(s); every running trader is NT8 TCP%s", len(ids), extra)
	})

	// addon_ack
	leg("addon_ack", "maintenance_ack on the current connection's record", func() (bool, string) {
		if !haveWire {
			return false, noWire
		}
		r := wire.Rec
		id := fmt.Sprintf("accept_seq=%d remote_port=%d", r.AcceptSeq, r.RemotePort)
		switch {
		case !wire.Connected:
			return false, "AddOn not connected (" + id + ")"
		case !wire.HasAck || r.Ack == nil:
			return false, "addon_ack=n/a — no maintenance_ack on this connection (an AddOn older than 2026-09-22-m2 never acks) (" + id + ")"
		case !r.Ack.Held:
			return false, "the AddOn's ack says not held (" + id + ")"
		case holdJob == "":
			return false, "no hold job to match the ack against"
		case r.Ack.JobID != holdJob:
			return false, fmt.Sprintf("ack names job %q, the hold is %q (%s)", r.Ack.JobID, holdJob, id)
		case wire.AckAge > ntwire.MaintenanceAckMaxAge():
			return false, fmt.Sprintf("ack is %s old (max %s) (%s)", wire.AckAge.Round(time.Second), ntwire.MaintenanceAckMaxAge(), id)
		case r.Ack.QueuedCommands != 0:
			return false, fmt.Sprintf("the AddOn reports queued_commands=%d — work still in flight at the AddOn (%s)", r.Ack.QueuedCommands, id)
		}
		return true, fmt.Sprintf("held job=%s build=%s age=%s queued_commands=%d (%s)", r.Ack.JobID, r.Ack.BuildID, wire.AckAge.Round(time.Millisecond), r.Ack.QueuedCommands, id)
	})

	// addon_census
	// censusVerdict judges a census that EXISTS: never called with a == nil
	// (the two census legs disagree on what a nil census means, and their
	// own cases handle it).
	censusVerdict := func(a *ntwire.MaintenanceAckPayload) (bool, string) {
		switch {
		case a.CensusError != "":
			return false, a.CensusError
		case a.Connections == nil:
			return false, "connections were not enumerated (absent is not empty)"
		case a.Accounts == nil:
			return false, "accounts were not enumerated (absent is not empty)"
		}
		nonSim, unsettled := 0, 0
		for _, c := range a.Connections {
			if c.Connected && !c.Sim {
				nonSim++
			}
			if !c.Settled {
				unsettled++ // M2.1: Connecting / ConnectionLost — its accounts cannot be vouched for
			}
		}
		// UPDATER-FLAT-LIVE-OK (owner ruling 2026-09-28): the census counts
		// positions/working per SIM vs non-SIM (live) account, so a refusal
		// NAMES where the exposure lives. A connected non-SIM connection no
		// longer fails BY ITSELF — flat is flat, and the updater never
		// disconnects anything.
		simPos, livePos, simWork, liveWork := 0, 0, 0, 0
		for _, ac := range a.Accounts {
			if ac.Sim {
				simPos += ac.Positions
				simWork += ac.Working
			} else {
				livePos += ac.Positions
				liveWork += ac.Working
			}
		}
		positions, working := simPos+livePos, simWork+liveWork
		detail := fmt.Sprintf("connections=%d connected_non_SIM=%d accounts=%d positions=%d working=%d",
			len(a.Connections), nonSim, len(a.Accounts), positions, working)
		var why []string
		if unsettled > 0 {
			why = append(why, fmt.Sprintf("%d connection(s) in a transitional state (neither Connected nor Disconnected)", unsettled))
		}
		if livePos > 0 {
			why = append(why, fmt.Sprintf("%d open position(s) on a non-SIM (live) account", livePos))
		}
		if liveWork > 0 {
			why = append(why, fmt.Sprintf("%d working order(s) on a non-SIM (live) account", liveWork))
		}
		if simPos > 0 {
			why = append(why, fmt.Sprintf("%d open position(s) on a SIM account", simPos))
		}
		if simWork > 0 {
			why = append(why, fmt.Sprintf("%d working order(s) on a SIM account", simWork))
		}
		// D0: the flat-allowed suffix is the PASS path's own claim — it is
		// appended only when the verdict passes (positions == 0 AND working
		// == 0 AND every connection settled), never beside a refusal.
		if len(why) == 0 && nonSim > 0 {
			detail += fmt.Sprintf(" — %d connected non-SIM connection(s), all accounts flat — allowed (owner ruling 2026-09-28)", nonSim)
		}
		if len(why) > 0 {
			return false, strings.Join(why, "; ") + " — " + detail
		}
		return true, detail
	}
	leg("addon_census", "maintenance_ack census (AddOn; every connection and account, no names)", func() (bool, string) {
		if !haveWire {
			return false, noWire
		}
		a := wire.Rec.Ack
		if a == nil {
			return false, "addon_ack=n/a — no census"
		}
		ok, detail := censusVerdict(a)
		// The AGE rides the detail so a preflight read shows staleness
		// (prehold-stale-census, owner order 2026-10-02 10:15 CT) — text only,
		// the verdict is censusVerdict's.
		return ok, fmt.Sprintf("age=%s — %s", wire.AckAge.Round(time.Second), detail)
	})

	// censusExposure totals a census that ENUMERATED its accounts; ok is
	// false when the census failed or never enumerated (no evidence of
	// flatness, never mistaken for flat).
	censusExposure := func(a *ntwire.MaintenanceAckPayload) (positions, working int, ok bool) {
		if a.CensusError != "" || a.Connections == nil || a.Accounts == nil {
			return 0, 0, false
		}
		for _, ac := range a.Accounts {
			positions += ac.Positions
			working += ac.Working
		}
		return positions, working, true
	}

	// addon_census_prehold — the C22 pre-hold flat evidence (#206 review
	// fold). The wire sends maintenance frames ONLY while held
	// (provider/ninjatrader/maintenance_wire.go: pushMaintenance sends nothing
	// when !held), so a connection that has never been held has no census and
	// will not have one until the worker's own hold step. That absence is a
	// STATE, not missing evidence: the other preflight legs (ledger, planner,
	// traders) still fail closed on their own evidence, and drain re-checks a
	// FRESH census right after the hold exists. When a census DOES exist it
	// was sent while held and froze at release — prehold-stale-census (owner
	// order 2026-10-02 10:15 CT): a stale census is NEVER stronger evidence
	// than no census, so a STALE census that is FLAT passes exactly like the
	// no-census state (the ledger, planner and trader legs vouch for flat,
	// and drain re-checks a fresh census after the hold), a STALE census that
	// is NOT flat fails the leg, and a FRESH census takes the content
	// verdict. Without this, the previous install's held:false release ack —
	// which nothing refreshes until the next hold — failed every second
	// install on one bot process until an NT8 or bot restart.
	leg("addon_census_prehold", "maintenance_ack census, pre-hold (C22): stale flat = no census; stale non-flat refuses; fresh takes the content verdict", func() (bool, string) {
		if !haveWire {
			return false, noWire
		}
		a := wire.Rec.Ack
		if a == nil {
			return true, "no census — this connection has never been held (flat is vouched for by the ledger, planner and trader legs; drain re-checks a fresh census after the hold)"
		}
		if wire.AckAge < 0 || wire.AckAge > ntwire.MaintenanceAckMaxAge() {
			positions, working, ok := censusExposure(a)
			if ok && positions == 0 && working == 0 {
				return true, fmt.Sprintf("census is %s old (from an earlier hold) — flat, treated as no census; the ledger, planner and trader legs vouch for flat, and drain re-checks a fresh census after the hold", wire.AckAge.Round(time.Second))
			}
			if !ok {
				// No account census at all: there is no flat evidence to
				// judge, so the refusal says exactly that — never "not flat".
				why := "connections/accounts not enumerated"
				if a.CensusError != "" {
					why = a.CensusError
				}
				return false, fmt.Sprintf("the census ack is %s old (max %s) and carries no account census (%s) — a pre-hold census must be fresh (restart the bot for a fresh connection, or hold it for fresh acks)", wire.AckAge.Round(time.Second), ntwire.MaintenanceAckMaxAge(), why)
			}
			return false, fmt.Sprintf("the census ack is %s old (max %s) and NOT flat (positions=%d working=%d) — a pre-hold census must be fresh (restart the bot for a fresh connection, or hold it for fresh acks)", wire.AckAge.Round(time.Second), ntwire.MaintenanceAckMaxAge(), positions, working)
		}
		return censusVerdict(a)
	})

	// ledger_exposure — every trader id
	ledgerLeg := func() (bool, string) {
		if st == nil || st.ArmedOrders() == nil {
			return false, "store unavailable — ledger cannot be read"
		}
		rows, err := st.ArmedOrders().ListNonTerminalAllTraders()
		if err != nil {
			return false, "armed ledger read failed: " + err.Error()
		}
		unplaced := 0
		var exposed []string
		for _, r := range rows {
			if store.IsUnplacedArm(r.State, r.SignalID) {
				unplaced++
				continue
			}
			exposed = append(exposed, fmt.Sprintf("armed#%d %s %s", r.ID, r.TraderID, r.State))
		}
		pics, perr := st.PictureHtfRecoverableAll()
		if perr != nil {
			return false, "picture ledger read failed: " + perr.Error()
		}
		for _, p := range pics {
			exposed = append(exposed, fmt.Sprintf("picture %s %s %s", p.OppKey, p.TraderID, p.Stage))
		}
		info := fmt.Sprintf("%d authorized-but-unplaced arm(s) (informational)", unplaced)
		if len(exposed) > 0 {
			return false, "placed/unresolved: " + strings.Join(exposed, ", ") + "; " + info
		}
		return true, "no placed or unresolved row; " + info
	}
	leg("ledger_exposure", "armed_orders + picture_htf_opportunities, all trader ids (canonical arm-state predicates)", ledgerLeg)

	// sim_accounts — the absent path's SIM proof (UPDATER-NT8-CLOSED item 1d).
	// With NT8 closed there is no AddOn census, so the gate reads the SAME
	// predicate every order path runs: each bound account must be
	// SIM-tradeable via isAccountTradeable (the hard live/funded-account
	// block) — exported as IsAccountTradeable, NEVER weakened. Any unknown
	// (no bound account, a non-NT8 trader, an account the server cannot
	// vouch for) fails.
	simAccountsLeg := func() (bool, string) {
		registry := map[string]bool{}
		for _, id := range registryOnly {
			registry[id] = true
		}
		var bad []string
		checked := 0
		for _, id := range ids {
			at := all[id]
			nt, ok := at.trader.(*ntTrader.TCPTrader)
			if !ok {
				if registry[id] {
					continue // registry-only, not NT8: cannot reach NT8 (informational in traders_nt8)
				}
				bad = append(bad, fmt.Sprintf("%s: not an NT8 TCP trader (%T) — its account cannot be vouched for", id, at.trader))
				continue
			}
			name := at.currentAccountName()
			checked++
			if name == "" {
				bad = append(bad, fmt.Sprintf("%s: bound to no account", id))
				continue
			}
			if !nt.IsAccountTradeable(name) {
				bad = append(bad, fmt.Sprintf("%s: bound account %s is not SIM-tradeable", id, name))
			}
		}
		if len(bad) > 0 {
			return false, strings.Join(bad, "; ")
		}
		if checked == 0 {
			return false, "no NT8 trader bound to an account — nothing to vouch for (fail-closed)"
		}
		return true, fmt.Sprintf("%d bound account(s) SIM-tradeable", checked)
	}

	// db_open_positions — the DB's own record of exposure, per NT8 trader
	// (UPDATER-NT8-CLOSED P1): the SAME store read CutoverGateStatus leg 1
	// uses (store.Position().GetOpenPositions). With NT8 absent the broker
	// legs cannot run, so this leg IS the position evidence. Fail-closed on
	// any read error or unknown; each open row is NAMED (id, trader,
	// symbol, side).
	dbOpenLeg := func() (bool, string) {
		if st == nil || st.Position() == nil {
			return false, "position store unavailable — leg cannot be evaluated"
		}
		var exposed []string
		total, traders := 0, 0
		for _, id := range ids {
			if _, ok := all[id].trader.(*ntTrader.TCPTrader); !ok {
				continue
			}
			traders++
			rows, err := st.Position().GetOpenPositions(id)
			if err != nil {
				return false, fmt.Sprintf("query failed for %s: %v", id, err)
			}
			total += len(rows)
			for _, r := range rows {
				exposed = append(exposed, fmt.Sprintf("open#%d %s %s %s", r.ID, r.TraderID, r.Symbol, r.Side))
			}
		}
		if len(exposed) > 0 {
			return false, "OPEN: " + strings.Join(exposed, ", ")
		}
		return true, fmt.Sprintf("0 open rows across %d NT8 trader(s)", traders)
	}

	// trader_cutover:<id> — each NT8 trader's legs 1, 2, 4
	for _, at := range nts {
		at := at
		leg("trader_cutover:"+at.id, "CutoverGateStatus legs 1 (db_open_positions), 2 (api_positions), 4 (working_orders)", func() (bool, string) {
			var failed, passed []string
			for _, l := range installationTraderCutover(at) {
				if l.N == 3 || l.N == 5 {
					continue // 3 → traders_nt8 + addon_census; 5 → planner_in_flight
				}
				if l.Pass {
					passed = append(passed, l.Name)
				} else {
					failed = append(failed, l.Name+": "+l.Detail)
				}
			}
			if len(failed) > 0 {
				return false, strings.Join(failed, " | ")
			}
			if len(passed) == 0 {
				return false, "no cutover leg evaluated (fail-closed)"
			}
			return true, "passed: " + strings.Join(passed, ", ")
		})
	}

	// ── nt8_absent (UPDATER-NT8-CLOSED, owner ruling 22:1x CT 09-27) ─────────
	// NT8 closed = nothing can trade (every account is NT8 SIM and Sim101
	// executes INSIDE NT8) = the safest moment to update. The absent verdict
	// is measured, never inferred: the link must be down ≥ nt8AbsentMinLinkDown
	// CONTINUOUSLY, from the server's per-connection disconnect stamp (a new
	// accept clears the stamp, so a reconnect revokes the verdict — never
	// grandfathered). Every ledger leg must pass on its OWN evidence; the
	// ack/census legs cannot exist here (the AddOn cannot answer) and are not
	// required. A leg that cannot be evaluated fails.
	if haveWire {
		absent := NT8AbsentView{}
		if !wire.Connected && wire.HaveDisconnectedAt {
			absent.LinkDownSince = wire.DisconnectedAt.UTC().Format(time.RFC3339Nano)
			absent.Eligible = time.Since(wire.DisconnectedAt) >= nt8AbsentMinLinkDown
		}
		if absent.Eligible {
			absentLegs := []struct {
				name   string
				source string
				fn     func() (bool, string)
			}{
				{"hold", "the installation hold file (store.ReadMaintenanceHold)", holdLeg},
				{"go_drained", "EntryBarrier (trader/maintenance_gate.go)", drainedLeg},
				{"in_flight_sends", "EntryBarrier in-flight counter", inFlightLeg},
				{"queued_signals", "TCPServer reconnect queue", queuedLeg},
				{"planner_in_flight", "plannerReadInFlight ∪ weeklyReadClaim ∪ flipRereadInFlight ∪ deathRereadInFlight", plannerLeg},
				{"ledger_exposure", "armed_orders + picture_htf_opportunities, all trader ids (canonical arm-state predicates)", ledgerLeg},
				{"sim_accounts", "each bound trading account via IsAccountTradeable (the SIM-only order predicate, never weakened)", simAccountsLeg},
				{"db_open_positions", "sqlite trader_positions — the SAME read as CutoverGateStatus leg 1, per NT8 trader", dbOpenLeg},
			}
			absent.Ready = true
			for _, al := range absentLegs {
				l := InstallationGateLeg{Name: al.name, Source: al.source}
				func() {
					defer func() {
						if r := recover(); r != nil {
							l.Pass, l.Detail = false, fmt.Sprintf("leg panicked: %v — fail-closed", r)
						}
					}()
					l.Pass, l.Detail = al.fn()
				}()
				if !l.Pass {
					absent.Ready = false
				}
				absent.Legs = append(absent.Legs, l)
			}
		}
		g.NT8Absent = &absent
	}

	g.Ready = len(g.Legs) > 0
	for _, l := range g.Legs {
		if !l.Pass {
			g.Ready = false
		}
	}
	g.Note = "READ-ONLY. An update may proceed past drain only when ready=true. Every leg that cannot be evaluated fails."
	return g
}

func syncMapKeys(m interface{ Range(func(k, v any) bool) }) []string {
	var out []string
	m.Range(func(k, _ any) bool {
		out = append(out, fmt.Sprint(k))
		return true
	})
	sort.Strings(out)
	return out
}
