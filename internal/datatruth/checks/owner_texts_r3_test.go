package checks

import (
	"strings"
	"testing"
	"time"

	"vl/internal/datatruth"
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
		"Authorization: Bearer <token>",
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
	for _, want := range []string{"1 lost bar(s)", "10-29 13:03 CT", "asks NT8 for them again by itself", "refused", "2500-bar 1m ring", "about 41 hours of open market", "restart the bot"} {
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
