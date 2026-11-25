package checks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vl/internal/datatruth"
	"vl/internal/ownerpush"
)

// Release #55 round 4, builder h3 (OWNER TEXT + REMEDIES lens, re-reviews 2 and 4).
// Every pin below drives the PRODUCTION call (RegisterFillPrice / EnableState /
// datatruth.Evaluate) and then the owner channel's own filter, ownerpush.Scrub.

// fillPushes returns the pushes Evaluate sends while accounts Sim101.. join one by one
// (account k joins with two far fills), in order: the opening push, then one
// WIDENED push per joiner.
func fillPushes(t *testing.T, accounts int) []string {
	t.Helper()
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	prev := AckBaseURL()
	t.Cleanup(func() { SetAckBaseURL(prev) })
	SetAckBaseURL("http://127.0.0.1:8123")
	var pushes []string
	datatruth.SetNotifier(func(kind, text string) { pushes = append(pushes, text) })
	g, _ := newTestGuard(nil)
	RegisterFillPrice(g)
	now := time.Now()
	m := now.UnixMilli() / minuteMs * minuteMs
	for i := 1; i <= accounts; i++ {
		acct := fmt.Sprintf("Sim10%d", i)
		g.Record(farObs(acct+"a1", acct+"a1", acct, m-5_000), ringAround(m-minuteMs), now)
		g.Record(farObs(acct+"a2", acct+"a2", acct, m-4_000), ringAround(m-minuteMs), now)
		datatruth.Evaluate("MNQ", now)
	}
	return pushes
}

// P2 (re-review 2): the opening push for three or more accounts, and every
// WIDENED push, was longer than 600 runes - composePush clipped only the detail, so
// ownerpush.Scrub cut the END (the remedy). The delivered text must be the sent
// text, and must still carry the whole remedy, for 1..8 accounts.
//
// MUTANT (shown RED): composePush clips only `why` (the round-3 body) -> RED from 3
// accounts up.
func TestR4FillPricePushesSurviveScrubForEveryNumberOfAccounts(t *testing.T) {
	for n := 1; n <= 8; n++ {
		pushes := fillPushes(t, n)
		if len(pushes) != n {
			t.Fatalf("%d accounts: want one opening push and %d WIDENED, got %d: %v", n, n-1, len(pushes), pushes)
		}
		for i, p := range pushes {
			got := ownerpush.Scrub(p)
			if got != p {
				t.Fatalf("%d accounts, push %d: Scrub changed it (%d runes):\n raw=%q\n got=%q", n, i+1, len([]rune(p)), p, got)
			}
			for _, want := range []string{
				"POST http://127.0.0.1:8123/api/login",
				"/api/risk/ack-fill-price?symbol=MNQ",
				"header Authorization set to Bearer, a space, the token",
				"the web app has no button",
				"do not resume by themselves",
				"exits, stops and protection are unaffected",
			} {
				if !strings.Contains(got, want) {
					t.Fatalf("%d accounts, push %d must contain %q:\n%s", n, i+1, want, got)
				}
			}
		}
	}
}

// The unreadable-state block: a new CME session does NOT lift it, so neither the
// Evaluate push nor the boot push may promise it (re-review 4 P3); both survive
// Scrub; the pause text names the time the file was found unreadable, not "the last
// restart"; and a block whose file could not be moved aside says the
// acknowledgement does not outlive a restart.
//
// MUTANT (shown RED): RegisterFillPrice passes the strike tail (fillResume) for
// every block -> RED (the push says the next CME session starts).
func TestR4UnreadableStateTextsAreHonestAndSurviveScrub(t *testing.T) {
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	prev := AckBaseURL()
	t.Cleanup(func() { SetAckBaseURL(prev) })
	SetAckBaseURL("http://127.0.0.1:8123")
	var pushes []string
	datatruth.SetNotifier(func(kind, text string) { pushes = append(pushes, text) })

	path := filepath.Join(t.TempDir(), "fill.json")
	if err := os.WriteFile(path, []byte("{corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	g, _ := newTestGuard(nil)
	var boot []string
	g.SetAlert(func(kind, text string) { boot = append(boot, text) })
	RegisterFillPrice(g)
	if _, err := g.EnableState(path); err == nil {
		t.Fatal("a corrupt file must report its error")
	}
	if len(boot) != 1 {
		t.Fatalf("one boot push: %v", boot)
	}
	now := time.Now()
	b, why, _, _ := g.CheckScoped()("MNQ", now)
	if !b {
		t.Fatal("an unreadable state file blocks")
	}
	if strings.Contains(why, "at the last restart") || !strings.Contains(why, "when the bot started at") || !strings.Contains(why, " CT") {
		t.Fatalf("the pause text must name the time the file was found unreadable: %s", why)
	}
	if !strings.Contains(why, "a new CME session does not lift it") {
		t.Fatalf("the pause text must say a new session does not lift it: %s", why)
	}
	datatruth.Evaluate("MNQ", now)
	all := append(append([]string(nil), boot...), pushes...)
	if len(all) != 2 {
		t.Fatalf("a boot push and an episode push: %v", all)
	}
	for i, p := range all {
		if got := ownerpush.Scrub(p); got != p {
			t.Fatalf("push %d: Scrub changed it:\n raw=%q\n got=%q", i, p, got)
		}
		for _, bad := range []string{"next CME session starts", "resume automatically"} {
			if strings.Contains(p, bad) {
				t.Fatalf("push %d promises a resume that never comes (%q): %s", i, bad, p)
			}
		}
		for _, want := range []string{"POST http://127.0.0.1:8123/api/login", "/api/risk/ack-fill-price?symbol=MNQ", "header Authorization set to Bearer", "does NOT lift"} {
			if !strings.Contains(p, want) {
				t.Fatalf("push %d must contain %q: %s", i, want, p)
			}
		}
	}

	// the file could not be moved aside: say the ack does not survive a restart
	u := &unreadableState{Since: now.UnixMilli(), Why: "bad json"}
	text := u.describe("/x/fill.json", "MNQ")
	if !strings.Contains(text, "acknowledgement lasts until the next restart") || !strings.Contains(text, "pause returns") {
		t.Fatalf("the not-moved text must say the ack does not outlive a restart: %s", text)
	}
}

// ingest_loss: the spent-budget pause does not promise "entries resume automatically
// when the data measures clear" (re-review 4 P3), the text says the ring age is
// OPEN-market time, and for a 1h/4h frame that only a restart ends it.
//
// MUTANT (shown RED): drop SetResumeNote(CheckIngestLoss, ...) in RegisterAll's
// ingest wiring -> RED (the generic tail comes back).
func TestR4IngestLossTextsAreHonestAboutTheWayOut(t *testing.T) {
	for _, tc := range []struct{ tf, want string }{
		{"1m", "not wall-clock hours - a Friday loss can last into Tuesday"},
		{"5m", "open-market hours, not wall-clock"},
		{"4h", "in practice only a restart ends it"},
	} {
		datatruth.Reset()
		var pushes []string
		datatruth.SetNotifier(func(kind, text string) { pushes = append(pushes, text) })
		l := NewLedger(10)
		ts := time.Date(2026, 10, 29, 18, 3, 0, 0, time.UTC).UnixMilli()
		l.RecordLoss(LossHistorical, "MNQ", tc.tf, []int64{ts}, time.Now())
		RegisterAll(Config{
			Src:        fakeSrc{},
			Ledger:     l,
			Verify:     func(string, string, int64, string) (bool, error) { return false, nil },
			Unrequired: []string{CheckLookbackSwing4h, CheckLookbackLevels, CheckLookbackSession},
		})
		b, why := l.Judge("MNQ", func(string, string, int64, string) (bool, error) { return false, nil })
		if !b || !strings.Contains(why, tc.want) {
			t.Fatalf("%s: the pause text must say %q: %s", tc.tf, tc.want, why)
		}
		datatruth.Evaluate("MNQ", time.Now())
		var push string
		for _, p := range pushes {
			if strings.Contains(p, "ingest_loss") {
				push = p
			}
		}
		if push == "" {
			t.Fatalf("%s: no ingest_loss push in %v", tc.tf, pushes)
		}
		if strings.Contains(push, "resume automatically") {
			t.Fatalf("%s: the spent-budget push promises an automatic resume: %s", tc.tf, push)
		}
		if got := ownerpush.Scrub(push); got != push {
			t.Fatalf("%s: Scrub changed the push: %q", tc.tf, got)
		}
		for _, want := range []string{"restart the bot in a flat/safe window", "ages out of the ring"} {
			if !strings.Contains(push, want) {
				t.Fatalf("%s: the push must contain %q: %s", tc.tf, want, push)
			}
		}
		datatruth.Reset()
	}
}
