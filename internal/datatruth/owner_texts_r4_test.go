package datatruth

import (
	"strings"
	"testing"
)

// Release #55 round 4, builder h3. composePush is the one function every owner push
// is built by; its contract is the one the owner depends on: the text that is sent
// is at most PushRunes (ownerpush.Scrub cuts at 600 and the cut is always the END),
// and the TAIL - the remedy - is the last thing sacrificed, never clipped while it
// fits the cap on its own.
//
// MUTANT (shown RED): composePush clips only `why` (the round-3 body: room =
// PushRunes - head - tail, floor 40, head and tail never touched) -> RED for a long
// head.
func TestComposePushNeverExceedsTheCapAndNeverClipsTheTail(t *testing.T) {
	word := strings.Repeat("word ", 400)
	for _, hl := range []int{0, 60, 200, 450, 580, 900} {
		for _, wl := range []int{0, 30, 300, 1200} {
			for _, tl := range []int{0, 150, 380, 560, PushRunes} {
				head := (word)[:hl]
				why := word[:wl]
				tail := strings.Repeat("t", tl)
				got := composePush(head, why, tail)
				if n := len([]rune(got)); n > PushRunes {
					t.Fatalf("head=%d why=%d tail=%d: %d runes > cap %d", hl, wl, tl, n, PushRunes)
				}
				if !strings.HasSuffix(got, tail) {
					t.Fatalf("head=%d why=%d tail=%d: the tail (the remedy) was clipped: ...%q", hl, wl, tl, got[max(0, len(got)-60):])
				}
			}
		}
	}
	// a tail over the cap on its own is the one thing clipped (a bug in a check's text)
	if got := composePush("h", "w", strings.Repeat("t", PushRunes+50)); len([]rune(got)) > PushRunes {
		t.Fatalf("over-long tail not bounded: %d", len([]rune(got)))
	}
	// a clipped detail never ends inside a path or URL (Scrub would redact the stub)
	got := composePush("H: ", "see /home/hoang/vl/data/data.db.fillprice.json for the state "+strings.Repeat("x", 600), strings.Repeat("t", 500))
	if strings.Contains(got, "/home/hoang/vl/data/data.db.fi") && !strings.Contains(got, "data.db.fillprice.json") {
		t.Fatalf("a half path survived the clip: %s", got)
	}
}

// A push header names at most pushNames of a scope list and counts the rest: the
// whole list is in the log line and on /api/health.
//
// MUTANT (shown RED): pushNames = 0 (the whole list in the header) -> RED.
func TestPushHeaderCapsTheNamedScopes(t *testing.T) {
	scopes := []string{"acct:SIM101", "acct:SIM102", "acct:SIM103", "acct:SIM104", "acct:SIM105"}
	got := scopeNoteMax(nil, scopes, pushNames)
	if !strings.Contains(got, "account SIM101, account SIM102 and 3 more") || strings.Contains(got, "SIM104") {
		t.Fatalf("the header must name two and count three: %s", got)
	}
	if full := scopeNote(nil, scopes); !strings.Contains(full, "SIM105") {
		t.Fatalf("the log line keeps every name: %s", full)
	}
	if got := scopeNoteMax(nil, scopes[:2], pushNames); strings.Contains(got, "more") {
		t.Fatalf("two names are named, not counted: %s", got)
	}
}
