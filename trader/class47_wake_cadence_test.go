package trader

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/kernel"
	"vl/store"
)

// ── CLASS 47 — F4: STALE-ARM EXPIRY (owner-ruled; the only non-WARN change) ──
//
// 2026-09-02: one NEVER-PLACED v5 arm stayed non-terminal across six plan
// versions and held the class-33 cutover gate's leg 4 shut for ~5 hours. A row
// with no broker signal id has nothing at the broker to orphan.
//
// This test uses ONLY pre-class-47 surfaces (UpsertArm + ListNonTerminal), so it
// compiles on the old tree and FAILS there: nothing retires the stale row.
func TestClass47StaleArmExpiry(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "c47.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ledger := st.ArmedOrders()
	const tid, planID = "t1", "2026-09-02:NY:t1"
	now := time.Now()

	seed := func(scenario string, version int, state, signal string) int64 {
		row := &store.ArmedOrderDB{
			TraderID: tid, PlanID: planID, Version: version, Session: "NY",
			Scenario: scenario, Side: "long", EntryPx: 29044, StopPx: 29014, TargetPx: 29104,
			State: state, SignalID: signal, EntryClass: "armed_fill",
			CreatedAt: now, UpdatedAt: now, LegIndex: 0, LegCount: 1, Kind: "limit",
		}
		if err := ledger.UpsertArm(row); err != nil {
			t.Fatal(err)
		}
		return row.ID
	}
	stale := seed("S1", 5, "armed", "")            // the v5 arm that held the gate
	placed := seed("S2", 5, "working", "sig-live") // placed at the broker — MUST survive
	current := seed("S3", 6, "armed", "")          // the CURRENT version — MUST survive

	ids, err := ledger.SupersedeUnplacedArms(tid, planID, 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != stale {
		t.Fatalf("exactly the unplaced v5 row must be retired, got %v (stale=%d)", ids, stale)
	}

	byID := map[int64]store.ArmedOrderDB{}
	rows, _ := ledger.ListForPlan(planID)
	for _, r := range rows {
		byID[r.ID] = r
	}
	if got := byID[stale]; got.State != "superseded" || !strings.Contains(got.StateReason, "never placed") {
		t.Fatalf("the stale arm must be terminal `superseded` with a reason, got state=%q reason=%q", got.State, got.StateReason)
	}
	if got := byID[placed]; got.State != "working" || got.SignalID != "sig-live" {
		t.Fatalf("a PLACED row must be untouched (stop-line), got %+v", got)
	}
	if got := byID[current]; got.State != "armed" {
		t.Fatalf("the CURRENT version's arm must be untouched, got %q", got.State)
	}

	// The gate's leg 4 reads ListNonTerminal — the stale row must no longer hold it.
	nt, _ := ledger.ListNonTerminal(tid)
	for _, r := range nt {
		if r.ID == stale {
			t.Fatal("a superseded row must leave the cutover gate's non-terminal set (leg 4)")
		}
	}
	if len(nt) != 2 {
		t.Fatalf("leg 4 should see exactly the placed + current rows, got %d", len(nt))
	}
	// Idempotent: a second pass retires nothing more.
	if again, _ := ledger.SupersedeUnplacedArms(tid, planID, 6); len(again) != 0 {
		t.Fatalf("second pass must be a no-op, retired %v", again)
	}
}

// ── F1/F2 — WARN-FIRST: the line is logged and the wake still PROCEEDS ──────

func TestClass47CutoffAndCooldownEnforce(t *testing.T) {
	if got := wakeCutoffMinutes(); got != 25 {
		t.Fatalf("resolved cutoff = %d, want the shipped 25", got)
	}
	if got := wakeCooldownMinutes(); got != 30 {
		t.Fatalf("resolved cooldown = %d, want the shipped 30", got)
	}
	t.Setenv("WAKE_CUTOFF_MIN", "40")
	if got := wakeCutoffMinutes(); got != 40 {
		t.Fatalf("env override ignored, got %d", got)
	}
	t.Setenv("WAKE_CUTOFF_MIN", "0")
	if got := wakeCutoffMinutes(); got != 0 {
		t.Fatalf("0 must disable the check, got %d", got)
	}

	// SUPERSEDED SPEC (owner ruling 2026-09-03): these lines used to say
	// "would_skip … WARN-first: the wake PROCEEDS", and the assertion below
	// forbade the word SKIPPED. The cutoffs enforce now, so the wording is
	// inverted — the line must say the wake was skipped and must NOT claim it
	// proceeded. The numbers, the counter n and the class tag are unchanged.
	cut := wakeCutoffLine("NY", "seated OB(bull)·1h invalidated", 10, 25, 3)
	for _, want := range []string{"⏱ wake SKIPPED", "10 min to flat", "cutoff 25m", "n=3", "class 47"} {
		if !strings.Contains(cut, want) {
			t.Errorf("cutoff line %q missing %q", cut, want)
		}
	}
	cool := wakeCooldownLine("NY", "fresh FVG", 12, 30, 7)
	for _, want := range []string{"⏱ wake SKIPPED", "cooldown 12 min since the last wake-authored version", "cooldown 30m", "n=7"} {
		if !strings.Contains(cool, want) {
			t.Errorf("cooldown line %q missing %q", cool, want)
		}
	}
	if strings.Contains(cut, "PROCEEDS") || strings.Contains(cool, "PROCEEDS") {
		t.Fatal("an enforcing line must never claim the wake proceeded")
	}
}

// minutesToSessionFlat drives F1 — pinned against the real registry windows,
// including today's two would-be skips and the ASIA midnight wrap.
func TestClass47MinutesToFlat(t *testing.T) {
	reg := kernel.DefaultSessionRegistry()
	get := func(name string) *kernel.SessionDef {
		for i := range reg.Sessions {
			if reg.Sessions[i].Name == name {
				return &reg.Sessions[i]
			}
		}
		t.Fatalf("session %s missing", name)
		return nil
	}
	ny, london, asia := get("NY"), get("LONDON"), get("ASIA")

	// Today's NY wake at 14:20:29 — 25 min to the 14:45 flat: AT the cutoff.
	if m, ok := minutesToSessionFlat(ctTime(t, 2026, 9, 2, 14, 20), ny); !ok || m != 25 {
		t.Fatalf("NY 14:20 → %d min (ok=%v), want 25", m, ok)
	}
	// Today's LONDON wake at 08:12:30 — 17 min to the 08:30 flat: inside it.
	if m, ok := minutesToSessionFlat(ctTime(t, 2026, 9, 2, 8, 12), london); !ok || m != 18 {
		t.Fatalf("LONDON 08:12 → %d min (ok=%v), want 18", m, ok)
	}
	// Mid-window: nowhere near.
	if m, ok := minutesToSessionFlat(ctTime(t, 2026, 9, 2, 10, 0), ny); !ok || m != 285 {
		t.Fatalf("NY 10:00 → %d min (ok=%v), want 285", m, ok)
	}
	// ASIA wraps midnight: 17:00→02:00, so 01:30 is 30 min out.
	if m, ok := minutesToSessionFlat(ctTime(t, 2026, 9, 2, 1, 30), asia); !ok || m != 30 {
		t.Fatalf("ASIA 01:30 → %d min (ok=%v), want 30", m, ok)
	}
	// Outside the window → ok=false, never an invented deadline.
	if _, ok := minutesToSessionFlat(ctTime(t, 2026, 9, 2, 15, 30), ny); ok {
		t.Fatal("outside the NY window must return ok=false")
	}
}

// ── F3 — a WAKE defers on any open stream; a SCHEDULED read never does ──────

func TestClass47CrossSessionClaimDefersWakesOnly(t *testing.T) {
	// Nothing open.
	if _, open := anyPlannerStreamOpen(); open {
		t.Fatal("fixture: no stream should be open at the start")
	}
	// Today's overlap: LONDON's read is in flight when NY's wake arrives.
	londonKey := store.MakePlanIDForTrader("t1", "2026-09-02", "LONDON")
	if !claimPlannerRead(londonKey) {
		t.Fatal("fixture: could not claim")
	}
	t.Cleanup(func() { releasePlannerRead(londonKey) })

	held, open := anyPlannerStreamOpen()
	if !open || held != londonKey {
		t.Fatalf("an open LONDON stream must be visible globally, got %q open=%v", held, open)
	}
	// The claim is per (trader, date, session): NY's own key is still FREE —
	// which is exactly why the two streamed concurrently today at 08:01.
	nyKey := store.MakePlanIDForTrader("t1", "2026-09-02", "NY")
	if nyKey == londonKey {
		t.Fatal("fixture: the two session keys must differ")
	}
	if !claimPlannerRead(nyKey) {
		t.Fatal("the per-session claim does NOT block a different session — the overlap this wave defers wakes on")
	}
	releasePlannerRead(nyKey)

	line := wakeStreamDeferLine("NY", "fresh FVG", londonKey)
	for _, want := range []string{"⏱ wake DEFERRED", "a planner stream is already open", "Scheduled reads never defer", "class 47"} {
		if !strings.Contains(line, want) {
			t.Errorf("defer line %q missing %q", line, want)
		}
	}
}

// ── F5 — the boot line reads every value from its resolver (A24) ────────────

func TestClass47BootLineReadsResolvers(t *testing.T) {
	t.Setenv("WAKE_CUTOFF_MIN", "17")
	t.Setenv("WAKE_COOLDOWN_MIN", "41")
	line := WakeCadenceBootLine()
	for _, want := range []string{"cutoff=17m", "cooldown=41m", "cross-session=on", "stale-arm-expiry=on", "class 47"} {
		if !strings.Contains(line, want) {
			t.Fatalf("boot line %q missing %q — every field must be READ, never a literal", line, want)
		}
	}
}

// ── counters: recorded, per session, malformed reads as 0 ──────────────────

func TestClass47CountersRecorded(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "c47c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	const tid, date = "t1", "2026-09-02"
	for i := 1; i <= 3; i++ {
		if n, err := store.IncWakeCounter(st, tid, date, "NY", store.WakeWouldSkipCutoffKind); err != nil || n != i {
			t.Fatalf("cutoff bump %d → %d (%v)", i, n, err)
		}
	}
	if _, err := store.IncWakeCounter(st, tid, date, "NY", store.WakeWouldSkipCooldownKind); err != nil {
		t.Fatal(err)
	}
	if _, err := store.IncWakeCounter(st, tid, date, "LONDON", store.WakeWouldSkipCutoffKind); err != nil {
		t.Fatal(err)
	}
	if got := store.WakeCounterCount(st, tid, date, "NY", store.WakeWouldSkipCutoffKind); got != 3 {
		t.Fatalf("NY cutoff = %d, want 3", got)
	}
	if got := store.WakeCounterCount(st, tid, date, "NY", store.WakeWouldSkipCooldownKind); got != 1 {
		t.Fatalf("kinds must be separable, got %d", got)
	}
	if got := store.WakeCounterCount(st, tid, date, "LONDON", store.WakeWouldSkipCutoffKind); got != 1 {
		t.Fatalf("sessions must not bleed, got %d", got)
	}
	_ = st.SetSystemConfig(store.WakeCounterKey(tid, date, "ASIA", store.WakeWouldSkipCutoffKind), "not-a-number")
	if got := store.WakeCounterCount(st, tid, date, "ASIA", store.WakeWouldSkipCutoffKind); got != 0 {
		t.Fatalf("malformed must read 0, got %d", got)
	}
}

// ── PROMOTION TO ENFORCE (owner ruling 2026-09-03) ──────────────────────────
//
// Class 47 shipped WARN-first: both cutoffs recorded what a suppression WOULD
// have skipped and the wake ran anyway. One morning of live observation decided
// it. On 2026-09-03 the two guards each caught a real case:
//
//   ⏱ wake would_skip: 24 min to flat (cutoff 25m) — 1h S/D zone Demand
//     [29101.75–29187.25] on LONDON. WARN-first: the wake PROCEEDS.
//   ⏱ wake would_skip: cooldown 21 min since the last wake-authored version
//     (cooldown 30m) — seated Supply·1h invalidated
//
// The first wrote LONDON v2 at 08:15:44 for a session that flattens at 08:30 —
// a max-reasoning read whose plan could never be entered. Both now SKIP.

// THE PIN — today's 08:15 LONDON case. 24 minutes to flat, cutoff 25.
func TestWakeCutoffEnforcedSkipsTheLondon0815Case(t *testing.T) {
	d := WakeCadenceDecision{MinutesToFlat: 24, HaveFlat: true, CutoffMin: 25}
	if !d.SkipForCutoff() {
		t.Fatal("24 min to flat against a 25m cutoff must SKIP — this wrote LONDON v2 at 08:15:44 for an 08:30 flat")
	}
	if got := d.Reason(); !strings.Contains(got, "SKIPPED") {
		t.Errorf("the line must say the wake was skipped, not that it proceeds: %q", got)
	}
	// the boundary belongs to the wake: exactly at the cutoff it still runs
	if (WakeCadenceDecision{MinutesToFlat: 25, HaveFlat: true, CutoffMin: 25}).SkipForCutoff() {
		t.Error("exactly at the cutoff the wake PROCEEDS — the rule is < cutoff")
	}
	// no readable flat → never skip; an unknown deadline is not a deadline
	if (WakeCadenceDecision{MinutesToFlat: 0, HaveFlat: false, CutoffMin: 25}).SkipForCutoff() {
		t.Error("an unreadable session window must not manufacture a skip")
	}
	// knob off
	if (WakeCadenceDecision{MinutesToFlat: 1, HaveFlat: true, CutoffMin: 0}).SkipForCutoff() {
		t.Error("cutoff=0 disables the check")
	}
}

// THE PIN — today's 21-minute cooldown case against a 30-minute cooldown.
func TestWakeCooldownEnforcedSkipsThe21MinuteCase(t *testing.T) {
	d := WakeCadenceDecision{SinceLastWakeVersionMin: 21, HaveLastWakeVersion: true, CooldownMin: 30}
	if !d.SkipForCooldown() {
		t.Fatal("21 min since the last wake-authored version against a 30m cooldown must SKIP")
	}
	if got := d.Reason(); !strings.Contains(got, "SKIPPED") {
		t.Errorf("the line must say skipped: %q", got)
	}
	if (WakeCadenceDecision{SinceLastWakeVersionMin: 30, HaveLastWakeVersion: true, CooldownMin: 30}).SkipForCooldown() {
		t.Error("exactly at the cooldown the wake PROCEEDS — the rule is < cooldown")
	}
	// NO previous wake-authored version → nothing to cool down from
	if (WakeCadenceDecision{SinceLastWakeVersionMin: 0, HaveLastWakeVersion: false, CooldownMin: 30}).SkipForCooldown() {
		t.Error("with no prior wake-authored version there is no clock to be inside of")
	}
}

// Only WAKES are governed. A scheduled read, a death re-plan and an owner reset
// must be untouched — the ruling is explicit, and this is the assertion that
// keeps a future edit from widening it.
func TestWakeCadenceEnforcementGovernsWakesOnly(t *testing.T) {
	for _, trigger := range []string{"NY_scheduled_read", "LONDON_scheduled_read", "death_replan", "owner_reset", "owner_reread", "sunday_weekly_read"} {
		if WakeCadenceGoverns(trigger) {
			t.Errorf("%q is not a wake — the cadence cutoffs must never apply to it", trigger)
		}
	}
	for _, trigger := range []string{"level_event", "structure_mss"} {
		if !WakeCadenceGoverns(trigger) {
			t.Errorf("%q IS a wake and must be governed", trigger)
		}
	}
}

// The boot line must say ENFORCE now, from the resolvers.
func TestWakeCadenceBootLineSaysEnforce(t *testing.T) {
	line := WakeCadenceBootLine()
	for _, want := range []string{"cutoff=25m(enforce)", "cooldown=30m(enforce", "fast-market≥1.5×ATR exempt", "cutoff is NOT exempted"} {
		if !strings.Contains(line, want) {
			t.Errorf("boot line missing %q:\n%s", want, line)
		}
	}
	if strings.Contains(line, "WARN") {
		t.Errorf("the boot line still claims WARN-first:\n%s", line)
	}
}

// ── FAST-MARKET EXEMPTION (owner ruling 2026-09-03) ─────────────────────────
//
// The cooldown was promoted to ENFORCE at 10:28 CT and, fourteen minutes
// earlier, this had already happened:
//
//   ⏱ wake would_skip: cooldown 26 min since the last wake-authored version
//     (cooldown 30m) — seated Supply·1h invalidated … Recorded n=3
//   🧠 planner mode: fast-market (drift 133.8 pts = 2.8×ATR5m) — reasoning
//     downgraded to fast→low for this read (F3)
//
// Price had moved 2.8×ATR5m from the plan the executor was trading, and the
// enforcing cooldown would have refused the re-plan. F3 exists BECAUSE those
// reads matter — it downgrades reasoning to get the plan out faster. So fast
// market exempts the cooldown.
//
// It does NOT exempt the last-window cutoff: a re-plan with 20 minutes left is
// a re-plan with 20 minutes left, fast or not.

// THE PIN — today's 10:14 case: 26 min into a 30-min cooldown, drift 2.8×
// against a 1.5× threshold. Proceeds.
func TestFastMarketBypassesTheCooldown(t *testing.T) {
	d := WakeCadenceDecision{
		SinceLastWakeVersionMin: 26, HaveLastWakeVersion: true, CooldownMin: 30,
		FastMarketATR: 2.8, FastMarketThreshold: 1.5,
	}
	if d.SkipForCooldown() {
		t.Fatal("a 2.8×ATR drift must bypass the cooldown — this is the 10:14 read, 133.8 pts from the plan being traded")
	}
	if !d.FastMarketBypass() {
		t.Error("the decision must report that the bypass is what saved it")
	}
	if got := d.BypassNote(); !strings.Contains(got, "cooldown bypassed: fast market 2.8×ATR") {
		t.Errorf("bypass note = %q, want the ruling's wording", got)
	}
}

// A drift BELOW the threshold is not a fast market and is still skipped.
func TestSubThresholdDriftIsStillSkipped(t *testing.T) {
	d := WakeCadenceDecision{
		SinceLastWakeVersionMin: 26, HaveLastWakeVersion: true, CooldownMin: 30,
		FastMarketATR: 0.9, FastMarketThreshold: 1.5,
	}
	if !d.SkipForCooldown() {
		t.Fatal("0.9×ATR is not a fast market — the cooldown still applies")
	}
	if d.FastMarketBypass() {
		t.Error("no bypass below the threshold")
	}
	if d.BypassNote() != "" {
		t.Errorf("nothing to note when nothing was bypassed, got %q", d.BypassNote())
	}
	// exactly at the threshold IS a fast market (the ruling says ≥)
	at := d
	at.FastMarketATR = 1.5
	if at.SkipForCooldown() {
		t.Error("exactly at the threshold must bypass — the rule is ≥")
	}
}

// The cutoff is NOT exempted. The ruling is explicit and this is the assertion
// that keeps the exemption from spreading to it.
func TestFastMarketDoesNotBypassTheLastWindowCutoff(t *testing.T) {
	d := WakeCadenceDecision{
		MinutesToFlat: 20, HaveFlat: true, CutoffMin: 25,
		FastMarketATR: 2.8, FastMarketThreshold: 1.5,
	}
	if !d.SkipForCutoff() {
		t.Fatal("20 minutes to flat is 20 minutes to flat, fast market or not — the cutoff still skips")
	}
	// and the bypass note must not claim to have saved it
	if strings.Contains(d.Reason(), "bypassed") {
		t.Errorf("the cutoff reason must not mention a bypass: %q", d.Reason())
	}
}

// A threshold of zero (knob unresolvable) must not turn every wake into a
// fast-market wake — that would silently disable the cooldown entirely.
func TestZeroThresholdDoesNotBypassEverything(t *testing.T) {
	d := WakeCadenceDecision{
		SinceLastWakeVersionMin: 1, HaveLastWakeVersion: true, CooldownMin: 30,
		FastMarketATR: 0, FastMarketThreshold: 0,
	}
	if d.FastMarketBypass() {
		t.Fatal("with no resolved threshold there is no fast-market verdict — 0 >= 0 must not read as one")
	}
	if !d.SkipForCooldown() {
		t.Error("the cooldown must still apply when the threshold is unresolved")
	}
}

// TestDeferredWakeRefireDedupedPerTraderSession (W26-CTO-GATES F4) — while a
// stream is open, each deferred cycle must NOT queue another closure: at most
// one pending re-fire per (trader, session), so a 2h stream queues 1 closure,
// not ~60, and the herd never fires at stream close.
func TestDeferredWakeRefireDedupedPerTraderSession(t *testing.T) {
	defer drainDeferredWakeRefires() // leave the registry empty for the next test
	k := deferredWakeRefireKey("t1", "2026-08-18", "NY")
	n := 0
	registerDeferredWakeRefire(k, func() { n++ })
	registerDeferredWakeRefire(k, func() { n++ })                                               // same key → no second closure
	registerDeferredWakeRefire(deferredWakeRefireKey("t2", "2026-08-18", "NY"), func() { n++ }) // other trader → its own slot
	pending := drainDeferredWakeRefires()
	if len(pending) != 2 {
		t.Fatalf("one pending per (trader, session): got %d closures, want 2", len(pending))
	}
	for _, f := range pending {
		f()
	}
	if n != 2 {
		t.Fatalf("the two distinct closures must both run, got %d", n)
	}
}
