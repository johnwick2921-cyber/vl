package checks

import (
	"strings"
	"testing"
	"time"

	"vl/internal/datatruth"
	"vl/internal/ownerpush"
)

// Release #55 round 3 (review 4, OWNER TEXT + REMEDIES lens).

// D3: the third far fill after an acknowledgement says the pause stands (it does:
// the ack is cleared by every far fill), not "3 of 2 (the 2nd pauses ...)".
//
// MUTANT: farCountNote always prints "n of N (the Nth pauses ...)" -> RED.
func TestR3FarFillAlertPastTheLimitSaysThePauseStands(t *testing.T) {
	g, _ := newTestGuard(nil)
	alerts := &[]string{}
	g.SetAlert(func(kind, text string) { *alerts = append(*alerts, text) })
	day := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	m := day.UnixMilli() / minuteMs * minuteMs
	g.Record(farObs("a1", "a1", "Sim101", m+5_000), ringAround(m), day)
	g.Record(farObs("a2", "a2", "Sim101", m+6_000), ringAround(m), day)
	if b, _, _ := g.Check()("MNQ", day); !b {
		t.Fatal("2nd far fill pauses")
	}
	if !g.AckAccount("MNQ", "Sim101") {
		t.Fatal("ack")
	}
	g.Record(farObs("a3", "a3", "Sim101", m+7_000), ringAround(m), day)
	if b, _, _ := g.Check()("MNQ", day); !b {
		t.Fatal("a far fill after the ack pauses again")
	}
	if len(*alerts) != 3 {
		t.Fatalf("one alert per far fill: %v", *alerts)
	}
	for i, want := range []string{"1 of 2", "2 of 2", "past the limit of 2"} {
		if !strings.Contains((*alerts)[i], want) {
			t.Fatalf("alert %d must say %q: %q", i+1, want, (*alerts)[i])
		}
	}
	if strings.Contains((*alerts)[2], "3 of 2") || !strings.Contains((*alerts)[2], "PAUSED (again") {
		t.Fatalf("the post-ack alert is wrong: %q", (*alerts)[2])
	}
}

// D6: the remedy in the pause text is runnable as written: a host and port, the
// login step and the Authorization header - not a bare relative path.
//
// MUTANT: drop ackHowTo() / AckBaseURL() from CheckScoped -> RED.
func TestR3FillPricePauseTextCarriesARunnableAcknowledgement(t *testing.T) {
	prev := AckBaseURL()
	t.Cleanup(func() { SetAckBaseURL(prev) })
	SetAckBaseURL("http://127.0.0.1:8081")
	g, _ := newTestGuard(nil)
	day := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	m := day.UnixMilli() / minuteMs * minuteMs
	g.Record(farObs("a1", "a1", "Sim101", m+5_000), ringAround(m), day)
	g.Record(farObs("a2", "a2", "Sim101", m+6_000), ringAround(m), day)
	b, why, _, _ := g.CheckScoped()("MNQ", day)
	if !b {
		t.Fatal("must pause")
	}
	for _, want := range []string{
		"POST http://127.0.0.1:8081/api/risk/ack-fill-price?symbol=MNQ&account=SIM101",
		"POST http://127.0.0.1:8081/api/login",
		"HTTP header Authorization set to Bearer, a space, then the token",
		"no button",
	} {
		if !strings.Contains(why, want) {
			t.Fatalf("the pause text must contain %q: %s", want, why)
		}
	}
}

// D5: the fill_price owner alert does not promise an automatic resume.
func TestR3FillPriceRegistrationSetsTheResumeNote(t *testing.T) {
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	var got []string
	datatruth.SetNotifier(func(kind, text string) { got = append(got, text) })
	g, _ := newTestGuard(nil)
	RegisterFillPrice(g)
	now := time.Now()
	m := now.UnixMilli() / minuteMs * minuteMs
	g.Record(farObs("a1", "a1", "Sim101", m-5_000), ringAround(m-minuteMs), now)
	g.Record(farObs("a2", "a2", "Sim101", m-4_000), ringAround(m-minuteMs), now)
	datatruth.Evaluate("MNQ", now)
	if len(got) != 1 || strings.Contains(got[0], "resume automatically") || !strings.Contains(got[0], "acknowledge") {
		t.Fatalf("the pause alert must say how entries come back: %v", got)
	}
}

// D4: the ingest_loss reason is in CT and carries the remedy, the duration when
// the re-request is refused, and the restart.
//
// MUTANT: Judge's message drops ingestLossRemedy / formats in UTC -> RED.
func TestR3IngestLossReasonIsCTAndNamesTheWayOut(t *testing.T) {
	l := NewLedger(10)
	// 2026-10-29 18:03Z = 13:03 CT
	ts := time.Date(2026, 10, 29, 18, 3, 0, 0, time.UTC).UnixMilli()
	l.RecordLoss(LossHistorical, "MNQ", "1m", []int64{ts}, time.Now())
	b, why := l.Judge("MNQ", func(string, string, int64, string) (bool, error) { return false, nil })
	if !b {
		t.Fatal("must block")
	}
	for _, want := range []string{"1 lost bar(s)", "10-29 13:03 CT", "re-requests them from NT8 itself", "is refused", "2500-bar 1m ring", "about 41 hours of open market", "restart the bot in a flat/safe window"} {
		if !strings.Contains(why, want) {
			t.Fatalf("reason must contain %q: %s", want, why)
		}
	}
	if strings.Contains(why, "18:03Z") {
		t.Fatalf("no UTC stamps: %s", why)
	}
	l2 := NewLedger(10)
	l2.RecordLoss(LossFinal, "MNQ", "5m", []int64{ts}, time.Now())
	_, why5 := l2.Judge("MNQ", func(string, string, int64, string) (bool, error) { return false, nil })
	if !strings.Contains(why5, "about 208 hours of open market (roughly 10 trading days)") {
		t.Fatalf("the 5m duration is 2500 x 5 min: %s", why5)
	}
}

// P1 (re-review 1): the owner reads the TELEGRAM PUSH, and ownerpush.Scrub cuts it at
// 600 runes and rewrites "Authorization: x" as a credential. Every earlier pin read
// the raw string; these run the real alert through Evaluate (the one call every
// production path makes) and then through Scrub, and assert the remedy and the
// resume note SURVIVE it, for every kind of alert the check sends.
//
// MUTANT: Evaluate pushes the unclipped text (notify(a.kind, a.text)) -> RED (the
// fill_price push is cut mid-sentence, the how-to and resume note are lost).
// MUTANT: fillResume words the header "Authorization: Bearer <token>" -> RED
// (Scrub redacts it).
func TestR3FillPricePushSurvivesOwnerpushScrub(t *testing.T) {
	prev := AckBaseURL()
	t.Cleanup(func() { SetAckBaseURL(prev) })
	SetAckBaseURL("http://127.0.0.1:8123")
	datatruth.Reset()
	t.Cleanup(datatruth.Reset)
	var pushes []string
	datatruth.SetNotifier(func(kind, text string) { pushes = append(pushes, text) })
	g, _ := newTestGuard(nil)
	RegisterFillPrice(g)
	now := time.Now()
	m := now.UnixMilli() / minuteMs * minuteMs
	// two accounts, several fills each: the longest realistic detail
	for _, acct := range []string{"Sim101", "Sim102"} {
		g.Record(farObs(acct+"a1", acct+"a1", acct, m-5_000), ringAround(m-minuteMs), now)
		g.Record(farObs(acct+"a2", acct+"a2", acct, m-4_000), ringAround(m-minuteMs), now)
		g.Record(farObs(acct+"a3", acct+"a3", acct, m-3_000), ringAround(m-minuteMs), now)
	}
	datatruth.Evaluate("MNQ", now)
	if len(pushes) != 1 {
		t.Fatalf("one push for the opening: %v", pushes)
	}
	got := ownerpush.Scrub(pushes[0])
	if got != pushes[0] {
		t.Fatalf("Scrub changed the push (cut or redacted):\n raw=%q\n got=%q", pushes[0], got)
	}
	for _, want := range []string{
		"POST http://127.0.0.1:8123/api/login",
		"/api/risk/ack-fill-price?symbol=MNQ&account=",
		"header Authorization set to Bearer",
		"web app has no button",
		"not by themselves",
		"exits, stops and protection are unaffected",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the delivered push must contain %q:\n%s", want, got)
		}
	}
}

func TestR3IngestLossPushSurvivesOwnerpushScrub(t *testing.T) {
	for _, tf := range []string{"1m", "5m", "4h"} {
		datatruth.Reset()
		var pushes []string
		datatruth.SetNotifier(func(kind, text string) { pushes = append(pushes, text) })
		l := NewLedger(10)
		ts := time.Date(2026, 10, 29, 18, 3, 0, 0, time.UTC).UnixMilli()
		l.RecordLoss(LossHistorical, "MNQ", tf, []int64{ts}, time.Now())
		verify := func(string, string, int64, string) (bool, error) { return false, nil }
		datatruth.Register(CheckIngestLoss, func(symbol string, now time.Time) (bool, string, error) {
			b, why := l.Judge(symbol, verify)
			return b, why, nil
		})
		datatruth.Evaluate("MNQ", time.Now())
		if len(pushes) != 1 {
			t.Fatalf("%s: one push: %v", tf, pushes)
		}
		got := ownerpush.Scrub(pushes[0])
		if got != pushes[0] {
			t.Fatalf("%s: Scrub changed the push:\n raw=%q\n got=%q", tf, pushes[0], got)
		}
		for _, want := range []string{"restart the bot in a flat/safe window", "entries resume automatically when the data measures clear", "exits, stops and protection are unaffected"} {
			if !strings.Contains(got, want) {
				t.Fatalf("%s: the delivered push must contain %q:\n%s", tf, want, got)
			}
		}
		datatruth.Reset()
	}
}
