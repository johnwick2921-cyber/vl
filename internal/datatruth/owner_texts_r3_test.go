package datatruth

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// Release #55 round 3 (review 4, OWNER TEXT + REMEDIES lens). The pins drive
// Evaluate - the one call every production path makes - with a scoped check whose
// verdict the test changes between calls.

type pushCollector struct {
	mu   sync.Mutex
	list []string
}

func (p *pushCollector) add(kind, text string) {
	p.mu.Lock()
	p.list = append(p.list, kind+"|"+text)
	p.mu.Unlock()
}
func (p *pushCollector) all() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.list...)
}

// D2: a block that opens for the mentor readers and then WIDENS to every reader is
// pushed again ("WIDENED"), once, and the first header names its scope.
//
// MUTANT: drop the widens(...) branch in Evaluate -> RED (1 push, the AI path
// paused silently).
func TestR3WideningScopeIsPushedOnceAndTheHeaderNamesTheScope(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	var pc pushCollector
	SetNotifier(pc.add)
	var readers []string = []string{ReaderMentor}
	RegisterScoped("cross_frame", func(string, time.Time) (bool, string, []string, error) {
		return true, "5m close disagrees", readers, nil
	})
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	Evaluate("MNQ", now)
	Evaluate("MNQ", now.Add(time.Minute)) // same scope: silent
	if got := pc.all(); len(got) != 1 || !strings.Contains(got[0], "PAUSED for MNQ (mentor traders)") {
		t.Fatalf("first push must name the mentor scope: %v", got)
	}
	readers = nil // the same check now concerns every reader
	Evaluate("MNQ", now.Add(2*time.Minute))
	Evaluate("MNQ", now.Add(3*time.Minute)) // widened already: silent
	got := pc.all()
	if len(got) != 2 || !strings.Contains(got[1], "WIDENED") || !strings.Contains(got[1], "every entry path") || !strings.Contains(got[1], "was mentor traders") {
		t.Fatalf("the widening must be pushed once: %v", got)
	}
	// narrowing again is not news
	readers = []string{ReaderAI}
	Evaluate("MNQ", now.Add(4*time.Minute))
	if len(pc.all()) != 2 {
		t.Fatalf("a narrower scope must not push: %v", pc.all())
	}
}

// D2: a second trader/account joining an open scoped episode is a widening, and
// the header of the FIRST push names the account instead of reading as the symbol.
func TestR3TraderJoiningAnOpenScopedEpisodeIsPushed(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	var pc pushCollector
	SetNotifier(pc.add)
	scopes := []string{ScopeAccount("Sim101")}
	RegisterScoped("broker_agreement", func(string, time.Time) (bool, string, []string, error) {
		return true, "views disagree", scopes, nil
	})
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	Evaluate("MNQ", now)
	scopes = []string{ScopeAccount("Sim101"), ScopeTrader("B")}
	Evaluate("MNQ", now.Add(time.Minute))
	got := pc.all()
	if len(got) != 2 {
		t.Fatalf("want first push + one widening push, got %v", got)
	}
	if !strings.Contains(got[0], "PAUSED for MNQ (for account SIM101)") {
		t.Fatalf("the header must name the account: %q", got[0])
	}
	if !strings.Contains(got[1], "WIDENED") || !strings.Contains(got[1], "trader B") {
		t.Fatalf("the join must be named: %q", got[1])
	}
}

// D1: the snapshot (and so /api/health) carries the readers of the standing
// blocks; an every-path block wins over a named one.
func TestR3SnapshotCarriesTheReaders(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	RegisterScoped("mentor_only", func(string, time.Time) (bool, string, []string, error) {
		return true, "x", []string{ReaderMentor}, nil
	})
	RegisterScoped("everyone", func(string, time.Time) (bool, string, []string, error) { return true, "y", nil, nil })
	Evaluate("MNQ", time.Now())
	snap := Snapshot()
	if r := snap["mentor_only"].Readers; len(r) != 1 || r[0] != ReaderMentor {
		t.Fatalf("mentor_only readers = %v", r)
	}
	if r := snap["everyone"].Readers; len(r) != 0 {
		t.Fatalf("an every-path block has no readers: %v", r)
	}
	if hs := HealthSnapshot()["mentor_only"].(map[string]any); hs["readers"] == nil {
		t.Fatalf("health snapshot must carry readers: %v", hs)
	}
}

// D5: a pause that ends is TOLD to the owner, once, after the merge window has
// passed without a re-block; a flap inside the window never reads "resolved".
//
// MUTANT: drop the clear push branch in Evaluate -> RED.
func TestR3ClearIsPushedOnceAfterTheMergeWindowAndNeverForAFlap(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	var pc pushCollector
	SetNotifier(pc.add)
	bad := true
	Register("bad_print", func(string, time.Time) (bool, string, error) { return bad, "wick", nil })
	t0 := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	Evaluate("MNQ", t0)
	bad = false
	Evaluate("MNQ", t0.Add(10*time.Minute)) // cleared
	Evaluate("MNQ", t0.Add(20*time.Minute)) // inside the window: nothing yet
	if len(pc.all()) != 1 {
		t.Fatalf("no clear push inside the merge window: %v", pc.all())
	}
	// a flap inside the window: re-block, no new alert, and still no "resolved"
	bad = true
	Evaluate("MNQ", t0.Add(25*time.Minute))
	bad = false
	Evaluate("MNQ", t0.Add(26*time.Minute))
	Evaluate("MNQ", t0.Add(40*time.Minute)) // only 14 min after the second clear
	if len(pc.all()) != 1 {
		t.Fatalf("a flap must not push: %v", pc.all())
	}
	Evaluate("MNQ", t0.Add(26*time.Minute+ReopenMergeWindow)) // window passed with no re-block
	Evaluate("MNQ", t0.Add(90*time.Minute))                    // and only once
	got := pc.all()
	if len(got) != 2 || !strings.Contains(got[1], "bad_print is clear for MNQ") || !strings.Contains(got[1], "CT") {
		t.Fatalf("want exactly one resolved push in CT: %v", got)
	}
}

// D5: a check whose pause ends only by an owner action does not promise an
// automatic resume.
func TestR3ResumeNoteReplacesTheAutomaticPromise(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	var pc pushCollector
	SetNotifier(pc.add)
	SetResumeNote("fill_price", "entries resume when you acknowledge it")
	Register("fill_price", func(string, time.Time) (bool, string, error) { return true, "2 far fills", nil })
	Register("other", func(string, time.Time) (bool, string, error) { return true, "z", nil })
	Evaluate("MNQ", time.Now())
	var fill, other string
	for _, p := range pc.all() {
		if strings.Contains(p, "fill_price") {
			fill = p
		} else if strings.Contains(p, "other") {
			other = p
		}
	}
	if strings.Contains(fill, "resume automatically") || !strings.Contains(fill, "resume when you acknowledge") {
		t.Fatalf("fill_price must not promise an automatic resume: %q", fill)
	}
	if !strings.Contains(other, "resume automatically") {
		t.Fatalf("other checks keep the automatic promise: %q", other)
	}
}
