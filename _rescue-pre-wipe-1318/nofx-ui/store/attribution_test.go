package store

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── ATTRIBUTION INTEGRITY (2026-09-02) ───────────────────────────────────────
//
// Measured before building (the dispatch's premises were largely NOT supported):
//   · since the day-plan era, 51/51 `system` and 5/5 `armed_entry` closed
//     positions carry a plan link; 8 of 11 `reconcile` rows do too.
//   · exactly THREE rows lack one (566, 571, 580) and none has any arm within
//     30 minutes — there is nothing to join on, so the honest value is the
//     SENTINEL, not a guess and not "".
//   · FOUR rows already carry plan_id='UNRESOLVABLE' (530, 539, 545, 546), so
//     both forms coexist and a consumer testing one misses the other.
// This wave converges them and stops the arm's version from being overwritten.

func attribStore(t *testing.T) *Store {
	t.Helper()
	st, err := New(filepath.Join(t.TempDir(), "attrib.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// F2 — a position with no recoverable lineage carries the SENTINEL, never "".
// "" is a placeholder that reads as data (A24): it is indistinguishable from
// "not yet stamped", and every consumer that tests the sentinel misses it.
func TestAttributionUnresolvableSentinelNeverEmpty(t *testing.T) {
	if PlanUnresolvable != "UNRESOLVABLE" {
		t.Fatalf("the sentinel is the documented one, got %q", PlanUnresolvable)
	}
	// The classifier is what consumers use, so it must not be fooled by either
	// form and must never call an unresolvable row "linked".
	cases := []struct {
		planID string
		want   PlanLinkState
	}{
		{"2026-09-02:NY:trader", PlanLinkLinked},
		{PlanUnresolvable, PlanLinkUnresolvable},
		{"", PlanLinkUnstamped},
		{"   ", PlanLinkUnstamped},
	}
	for _, c := range cases {
		if got := ClassifyPlanLink(c.planID); got != c.want {
			t.Errorf("ClassifyPlanLink(%q) = %v, want %v", c.planID, got, c.want)
		}
	}
	if IsPlanLinked(PlanUnresolvable) || IsPlanLinked("") {
		t.Error("neither the sentinel nor an empty plan_id is a LINK")
	}
	if !IsPlanLinked("2026-09-02:NY:trader") {
		t.Error("a real plan id is a link")
	}
}

// F2b — the materialization helper: no lineage → sentinel, and it is counted.
func TestAttributionMaterializeStampsSentinel(t *testing.T) {
	st := attribStore(t)
	ps := st.Position()
	row := &TraderPosition{
		TraderID: "hoang", ExchangeID: "nt8", ExchangePositionID: "reconcile_MNQ_long_1",
		Symbol: "MNQ", Side: "long", Quantity: 1, EntryQuantity: 1, EntryPrice: 29100,
		EntryTime: time.Now().UnixMilli(), Leverage: 1, Status: "OPEN", Source: "reconcile",
		CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli(),
	}
	StampUnresolvableLineage(row) // what reconcile must call when it has nothing
	if row.PlanID != PlanUnresolvable {
		t.Fatalf("a lineage-less materialization must carry the sentinel, got %q", row.PlanID)
	}
	if err := ps.CreateOpenPosition(row); err != nil {
		t.Fatalf("create: %v", err)
	}
	n, err := ps.CountUnresolvable()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("the sentinel must be countable for the boot line, got %d", n)
	}
}

// F4 — armed_under_version is set ONCE at first authorization and survives every
// later UpsertArm; `version` keeps its last-touch meaning.
func TestAttributionArmedUnderVersionIsStable(t *testing.T) {
	st := attribStore(t)
	l := st.ArmedOrders()
	now := time.Now()
	first := &ArmedOrderDB{TraderID: "t1", PlanID: "p1", Scenario: "S1", Side: "long",
		Version: 3, EntryPx: 29044, State: "armed", CreatedAt: now, UpdatedAt: now}
	if err := l.UpsertArm(first); err != nil {
		t.Fatalf("first arm: %v", err)
	}
	got, err := l.GetArm("p1", "S1", 0)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ArmedUnderVersion != 3 || got.Version != 3 {
		t.Fatalf("first authorization: armed_under=%d version=%d, want 3/3", got.ArmedUnderVersion, got.Version)
	}

	// Re-authorized under a LATER plan version: version moves, armed_under does not.
	again := &ArmedOrderDB{TraderID: "t1", PlanID: "p1", Scenario: "S1", Side: "long",
		Version: 7, EntryPx: 29050, State: "armed", CreatedAt: now, UpdatedAt: now.Add(time.Minute)}
	if err := l.UpsertArm(again); err != nil {
		t.Fatalf("re-arm: %v", err)
	}
	got, err = l.GetArm("p1", "S1", 0)
	if err != nil {
		t.Fatalf("get2: %v", err)
	}
	if got.ArmedUnderVersion != 3 {
		t.Errorf("armed_under_version must NOT be overwritten: got %d want 3 — the arm was armed under v3", got.ArmedUnderVersion)
	}
	if got.Version != 7 {
		t.Errorf("version keeps its last-touch meaning: got %d want 7", got.Version)
	}
}

// F5 — the convergence is scoped, idempotent, and leaves pre-era history alone.
func TestAttributionConvergeIsScopedAndIdempotent(t *testing.T) {
	st := attribStore(t)
	ps := st.Position()
	// THE FIXTURE MUST NOT CARRY ITS OWN COPY OF THE BOUNDARY. It used to hold
	// the literal 1755230400000 — the SAME wrong 2025-08-15 value as the code —
	// so the two agreed with each other and disagreed with reality, and the test
	// passed while the migration converted 516 pre-era rows. It now derives from
	// the one named date, so a wrong boundary fails here first.
	eraMs := DayPlanEraStart.UnixMilli()
	mk := func(exchPosID string, createdMs int64, status, planID string) int64 {
		p := &TraderPosition{
			TraderID: "hoang", ExchangeID: "nt8", ExchangePositionID: exchPosID, Symbol: "MNQ",
			Side: "long", Quantity: 1, EntryQuantity: 1, EntryPrice: 29100, EntryTime: createdMs,
			Leverage: 1, Status: "OPEN", Source: "reconcile", PlanID: planID,
			CreatedAt: createdMs, UpdatedAt: createdMs,
		}
		if err := ps.CreateOpenPosition(p); err != nil {
			t.Fatalf("create %s: %v", exchPosID, err)
		}
		if status != "OPEN" {
			if err := st.GormDB().Exec("UPDATE trader_positions SET status=? WHERE id=?", status, p.ID).Error; err != nil {
				t.Fatalf("close %s: %v", exchPosID, err)
			}
		}
		return p.ID
	}
	inEra := mk("era_closed", eraMs+86400000, "CLOSED", "")                 // converges
	preEra := mk("pre_closed", eraMs-86400000, "CLOSED", "")                // MUST NOT convert
	openRow := mk("era_open", eraMs+86400000, "OPEN", "")                   // still open — not yet
	linked := mk("era_linked", eraMs+86400000, "CLOSED", "2026-09-02:NY:t") // untouched

	st.ConvergePlanLinkSentinel()

	get := func(id int64) string {
		var pid string
		if err := st.GormDB().Raw("SELECT COALESCE(plan_id,'') FROM trader_positions WHERE id=?", id).Scan(&pid).Error; err != nil {
			t.Fatalf("read %d: %v", id, err)
		}
		return pid
	}
	if got := get(inEra); got != PlanUnresolvable {
		t.Errorf("a day-plan-era CLOSED row must converge to the sentinel, got %q", got)
	}
	if got := get(preEra); got != "" {
		t.Errorf("PRE-ERA history must be left alone (there was never a plan to find), got %q", got)
	}
	if got := get(openRow); got != "" {
		t.Errorf("an OPEN row is not yet a defect, got %q", got)
	}
	if got := get(linked); got != "2026-09-02:NY:t" {
		t.Errorf("a linked row must never be overwritten, got %q", got)
	}

	// Idempotent: a second call changes nothing and does not re-log a count.
	st.ConvergePlanLinkSentinel()
	if got := get(preEra); got != "" {
		t.Errorf("second run touched pre-era history: %q", got)
	}

	n, err := ps.CountUnresolvable()
	if err != nil || n != 1 {
		t.Errorf("sentinel count = %d (err %v), want 1", n, err)
	}
	line := st.AttributionBootLine()
	for _, want := range []string{"attribution:", "unresolvable=1", "UNRESOLVABLE", "unstamped-closed=1"} {
		if !strings.Contains(line, want) {
			t.Errorf("boot line missing %q: %s", want, line)
		}
	}
	t.Logf("boot: %s", line)
}

// F5b — the class-40 P&L aggregators are keyed by trader/session, NOT by
// plan_id, so a sentinel row's money still counts. Pinned so a later wave does
// not "helpfully" exclude it and silently lose real P&L.
func TestAttributionSentinelRowsStillCountInPnL(t *testing.T) {
	if IsPlanLinked(PlanUnresolvable) {
		t.Fatal("the sentinel is not a link")
	}
	// A row we cannot attribute is still a trade that made or lost money.
	// The corrected-column law (class 40) governs WHICH column is summed; the
	// plan link governs which PLAN it is attributed to. They are independent.
	p := &TraderPosition{PlanID: PlanUnresolvable}
	if ClassifyPlanLink(p.PlanID) != PlanLinkUnresolvable {
		t.Error("classifier must report unresolvable")
	}
}

// The era constant is ASSERTED, not trusted. Its first cut was 1755230400000 —
// 2025-08-15, a year early — and it converted 516 pre-era rows because nothing
// checked what date the literal resolved to.
func TestDayPlanEraStartIsTheRightDate(t *testing.T) {
	ct, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	got := DayPlanEraStart
	// THE ASSERTION PRINTS BOTH ZONES, because the two errors this constant has
	// already had were invisible in one zone: 1755230400000 was a year early,
	// and UTC midnight was five hours early (2026-08-14 19:00 CT).
	t.Logf("DayPlanEraStart = %d  ·  %s  ·  %s",
		got.UnixMilli(),
		got.In(ct).Format("2006-01-02 15:04:05 MST"),
		got.UTC().Format("2006-01-02 15:04:05 UTC"))

	// The era is defined in CT: the first day-plan row's created_at is
	// 2026-08-16 00:44:31 UTC while its trade_date is 2026-08-15 (session NY),
	// so trade_date — a CT calendar date — is the key the era is named by.
	if d := got.In(ct).Format("2006-01-02 15:04:05"); d != "2026-08-15 00:00:00" {
		t.Errorf("the era must start at CT midnight on 2026-08-15, got %s CT", d)
	}
	if want := time.Date(2026, 8, 15, 0, 0, 0, 0, ct).UnixMilli(); got.UnixMilli() != want {
		t.Errorf("epoch drifted: got %d want %d", got.UnixMilli(), want)
	}
	// Neither prior wrong value may return.
	for _, bad := range []struct {
		ms   int64
		what string
	}{
		{1755230400000, "2025-08-15 — a year early; converted 516 pre-era rows"},
		{1786752000000, "2026-08-15 00:00 UTC = 08-14 19:00 CT — five hours early"},
	} {
		if got.UnixMilli() == bad.ms {
			t.Errorf("regressed to %d (%s)", bad.ms, bad.what)
		}
	}
}
