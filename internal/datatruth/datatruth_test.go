package datatruth

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)

type pushRec struct{ kinds, texts []string }

func capture() *pushRec {
	r := &pushRec{}
	SetNotifier(func(kind, text string) { r.kinds = append(r.kinds, kind); r.texts = append(r.texts, text) })
	return r
}

func TestNoChecksNoBlock(t *testing.T) {
	Reset()
	if b, rs := Evaluate("MNQ", t0); b || len(rs) != 0 {
		t.Fatalf("empty registry must not block: %v %v", b, rs)
	}
}

func TestEpisodeAlertsOncePerEpisodeAndClearsOnlyWhenMeasuredClear(t *testing.T) {
	Reset()
	defer Reset()
	rec := capture()
	var bad atomic.Bool
	bad.Store(true)
	Register("c1", func(sym string, now time.Time) (bool, string, error) {
		if bad.Load() {
			return true, "bar 12:03 suspect", nil
		}
		return false, "", nil
	})
	for i := 0; i < 5; i++ {
		b, rs := Evaluate("MNQ", t0.Add(time.Duration(i)*time.Second))
		if !b || len(rs) != 1 || rs[0].Check != "c1" || rs[0].SinceMs != t0.UnixMilli() {
			t.Fatalf("eval %d: %v %+v", i, b, rs)
		}
	}
	if len(rec.kinds) != 1 {
		t.Fatalf("one owner push per episode, got %d", len(rec.kinds))
	}
	if !strings.HasPrefix(rec.kinds[0], "data_truth_c1|MNQ|") {
		t.Fatalf("push key must be an ownerpush kind|episode key: %q", rec.kinds[0])
	}
	s := Snapshot()["c1"]
	if !s.Blocked || s.Episodes != 1 || s.LastReason != "bar 12:03 suspect" {
		t.Fatalf("snapshot: %+v", s)
	}
	bad.Store(false)
	if b, _ := Evaluate("MNQ", t0.Add(time.Minute)); b {
		t.Fatal("measured clear must resume entries")
	}
	if Snapshot()["c1"].Blocked {
		t.Fatal("snapshot must show clear")
	}
	// second episode (well past the merge window): a NEW alert with a NEW key
	bad.Store(true)
	Evaluate("MNQ", t0.Add(2*time.Hour))
	if len(rec.kinds) != 2 || rec.kinds[0] == rec.kinds[1] {
		t.Fatalf("second episode must alert once with a distinct key: %v", rec.kinds)
	}
	if Snapshot()["c1"].Episodes != 2 {
		t.Fatalf("episodes: %+v", Snapshot()["c1"])
	}
}

func TestErrorAndPanicFailClosed(t *testing.T) {
	Reset()
	defer Reset()
	capture()
	Register("err", func(string, time.Time) (bool, string, error) { return false, "", errors.New("store unreadable") })
	Register("boom", func(string, time.Time) (bool, string, error) { panic("nil map") })
	b, rs := Evaluate("MNQ", t0)
	if !b || len(rs) != 2 {
		t.Fatalf("both must block: %v %+v", b, rs)
	}
	for _, r := range rs {
		if !strings.Contains(r.Detail, "fail closed") {
			t.Fatalf("reason must say fail closed: %+v", r)
		}
	}
}

func TestEpisodesArePerSymbol(t *testing.T) {
	Reset()
	defer Reset()
	rec := capture()
	Register("c", func(sym string, _ time.Time) (bool, string, error) { return sym == "MNQ", "x", nil })
	Evaluate("MNQ", t0)
	if b, _ := Evaluate("ES", t0); b {
		t.Fatal("ES must not be blocked by MNQ's episode")
	}
	if len(rec.kinds) != 1 {
		t.Fatalf("pushes: %v", rec.kinds)
	}
}

func TestHealthSnapshotShape(t *testing.T) {
	Reset()
	defer Reset()
	Register("c", func(string, time.Time) (bool, string, error) { return true, "r", nil })
	Evaluate("MNQ", t0)
	h := HealthSnapshot()["c"].(map[string]any)
	if h["blocked"] != true || h["episodes"] != int64(1) || h["last_reason"] != "r" {
		t.Fatalf("%v", h)
	}
}

func TestReregisterKeepsOpenEpisode(t *testing.T) {
	Reset()
	defer Reset()
	rec := capture()
	fn := func(string, time.Time) (bool, string, error) { return true, "r", nil }
	Register("c", fn)
	Evaluate("MNQ", t0)
	Register("c", fn) // a re-wire
	Evaluate("MNQ", t0.Add(time.Second))
	if len(rec.kinds) != 1 || Snapshot()["c"].Episodes != 1 {
		t.Fatal("re-registering must not reopen an open episode")
	}
}

// D2: a check that blocks again soon after it cleared is the SAME episode
// re-opening: no second owner push, the original since is kept, the flap is
// counted. Past the window it is a new episode with its own push.
func TestFlappingCheckMergesIntoOneEpisode(t *testing.T) {
	Reset()
	defer Reset()
	rec := capture()
	var bad atomic.Bool
	Register("flap", func(string, time.Time) (bool, string, error) { return bad.Load(), "r", nil })
	bad.Store(true)
	Evaluate("MNQ", t0)
	for i := 1; i <= 5; i++ { // clear, re-block, clear, re-block ... inside the window
		bad.Store(false)
		if b, _ := Evaluate("MNQ", t0.Add(time.Duration(i*4)*time.Minute)); b {
			t.Fatal("a measured clear still resumes entries even when it flaps")
		}
		bad.Store(true)
		b, rs := Evaluate("MNQ", t0.Add(time.Duration(i*4+2)*time.Minute))
		if !b || rs[0].SinceMs != t0.UnixMilli() {
			t.Fatalf("flap %d: must stay blocked under the ORIGINAL since: %v %+v", i, b, rs)
		}
	}
	if len(rec.kinds) != 1 {
		t.Fatalf("a flapping check alerts ONCE, got %d pushes: %v", len(rec.kinds), rec.kinds)
	}
	if st := Snapshot()["flap"]; st.Episodes != 1 || st.Reopens != 5 {
		t.Fatalf("episodes=1 reopens=5 expected: %+v", st)
	}
	bad.Store(false)
	Evaluate("MNQ", t0.Add(time.Hour))
	bad.Store(true)
	Evaluate("MNQ", t0.Add(2*time.Hour))
	if len(rec.kinds) != 2 || Snapshot()["flap"].Episodes != 2 {
		t.Fatalf("past the merge window it is a new episode with its own push: %v", rec.kinds)
	}
}

// D2: the owner-push rate-limit kind is the prefix before "|". Each check must
// own its kind, or one flapping check burns the budget of the critical ones.
func TestEachCheckHasItsOwnPushKind(t *testing.T) {
	Reset()
	defer Reset()
	rec := capture()
	for _, n := range []string{"cross_frame", "ingest_loss", "contract_roll"} {
		Register(n, func(string, time.Time) (bool, string, error) { return true, "r", nil })
	}
	Evaluate("MNQ", t0)
	seen := map[string]bool{}
	for _, k := range rec.kinds {
		kind := k[:strings.IndexByte(k, '|')]
		if seen[kind] {
			t.Fatalf("two checks share the push kind %q: %v", kind, rec.kinds)
		}
		seen[kind] = true
	}
	if len(seen) != 3 {
		t.Fatalf("kinds: %v", rec.kinds)
	}
}

// D5: a REQUIRED check nobody registered blocks (fail closed) and says so; the
// real registration replaces the stub.
func TestRequiredButUnregisteredCheckBlocks(t *testing.T) {
	Reset()
	defer Reset()
	rec := capture()
	Require("ingest_loss", "swing")
	Register("swing", func(string, time.Time) (bool, string, error) { return false, "", nil })
	b, rs := Evaluate("MNQ", t0)
	if !b || len(rs) != 1 || rs[0].Check != "ingest_loss" || !strings.Contains(rs[0].Detail, "not wired") {
		t.Fatalf("a required-but-missing check must block: %v %+v", b, rs)
	}
	if len(rec.kinds) != 1 {
		t.Fatalf("the omission alerts once: %v", rec.kinds)
	}
	if h := HealthSnapshot()["ingest_loss"].(map[string]any); h["blocked"] != true {
		t.Fatalf("health must show the missing check blocked: %v", h)
	}
	Register("ingest_loss", func(string, time.Time) (bool, string, error) { return false, "", nil })
	if b, _ := Evaluate("MNQ", t0.Add(time.Minute)); b {
		t.Fatal("registering the real check replaces the stub")
	}
	Require("ingest_loss") // idempotent: must not clobber a real check
	if b, _ := Evaluate("MNQ", t0.Add(2*time.Minute)); b {
		t.Fatal("Require must not replace a registered check")
	}
}

// Release #55 review: a scoped check names the readers its block concerns; the
// Reason carries them, the alert says whom it pauses, an unscoped check concerns
// everyone, and a check that cannot judge (error / panic) concerns everyone no
// matter what readers it tried to name (fail closed).
func TestScopedChecksCarryTheirReaders(t *testing.T) {
	Reset()
	defer Reset()
	rec := capture()
	RegisterScoped("scoped", func(string, time.Time) (bool, string, []string, error) {
		return true, "wick", []string{ReaderMentor}, nil
	})
	Register("plain", func(string, time.Time) (bool, string, error) { return true, "hole", nil })
	RegisterScoped("broken", func(string, time.Time) (bool, string, []string, error) {
		return true, "x", []string{ReaderAI}, errors.New("store down")
	})
	_, rs := Evaluate("MNQ", t0)
	by := map[string]Reason{}
	for _, r := range rs {
		by[r.Check] = r
	}
	if r := by["scoped"]; len(r.Readers) != 1 || r.Readers[0] != ReaderMentor || !r.Concerns(ReaderMentor) || r.Concerns(ReaderAI) {
		t.Fatalf("scoped reason: %+v", r)
	}
	if r := by["plain"]; len(r.Readers) != 0 || !r.Concerns(ReaderAI) || !r.Concerns(ReaderMentor) {
		t.Fatalf("an unscoped check concerns everyone: %+v", r)
	}
	if r := by["broken"]; len(r.Readers) != 0 || !r.Concerns(ReaderAI) || !r.Concerns(ReaderMentor) {
		t.Fatalf("a check that cannot judge concerns everyone: %+v", r)
	}
	joined := strings.Join(rec.texts, "\n")
	if !strings.Contains(joined, "(mentor traders)") {
		t.Fatalf("the alert of a scoped block must say whom it pauses: %v", rec.texts)
	}
}

// Count records an immaterial disagreement once per key and surfaces it.
func TestCountIsIdempotentPerKeyAndVisible(t *testing.T) {
	Reset()
	defer Reset()
	Register("c", func(string, time.Time) (bool, string, error) { return false, "", nil })
	Count("c", "k1", "a")
	Count("c", "k1", "a")
	Count("c", "k2", "b")
	Count("nope", "k1", "ignored")
	if st := Snapshot()["c"]; st.Counted != 2 {
		t.Fatalf("counted %d, want 2 (one per distinct key)", st.Counted)
	}
	if h := HealthSnapshot()["c"].(map[string]any); h["counted"].(int64) != 2 {
		t.Fatalf("health: %v", h)
	}
}

// TestScopedTokensSplitIntoReadersAndScopes pins the release #55 integration of
// m450 (readers) and m451 (scopes): ONE returned []string carries both kinds and
// Evaluate must put the plain names on Reason.Readers and the trader:/acct:
// tokens on Reason.Scopes. A block carrying both applies only to the matching
// reader AND the matching trader; an error or panic loses both (blocks everyone).
// MUTANT: splitTokens returning every token as a reader -> Scopes empty -> RED.
func TestScopedTokensSplitIntoReadersAndScopes(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	RegisterScoped("both", func(string, time.Time) (bool, string, []string, error) {
		return true, "x", []string{ReaderMentor, ScopeTrader("t1"), ScopeAccount("sim101")}, nil
	})
	RegisterScoped("broken", func(string, time.Time) (bool, string, []string, error) {
		return true, "x", []string{ReaderAI, ScopeTrader("t1")}, errors.New("store down")
	})
	_, rs := Evaluate("MNQ", time.Now())
	by := map[string]Reason{}
	for _, r := range rs {
		by[r.Check] = r
	}
	b := by["both"]
	if len(b.Readers) != 1 || b.Readers[0] != ReaderMentor || len(b.Scopes) != 2 {
		t.Fatalf("tokens not split: readers=%v scopes=%v", b.Readers, b.Scopes)
	}
	if !b.Concerns(ReaderMentor) || b.Concerns(ReaderAI) || !b.AppliesTo("t1", "") || !b.AppliesTo("t2", "SIM101") || b.AppliesTo("t2", "Sim102") {
		t.Fatalf("reader/scope predicates wrong: %+v", b)
	}
	e := by["broken"]
	if len(e.Readers) != 0 || len(e.Scopes) != 0 || !e.Concerns(ReaderAI) || !e.Concerns(ReaderMentor) || !e.AppliesTo("t9", "") {
		t.Fatalf("a check that cannot judge must block everyone on every path: %+v", e)
	}
}
