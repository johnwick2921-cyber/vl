package datatruth

import (
	"strings"
	"testing"
	"time"
)

// m467 (release #62): a data-truth pause that LASTS is news again every hour.
// On 11-03 indicator_lookback opened 02:01 CT and stood until 17:42 - the owner
// got one push (which the network outage then lost) and nothing for 15 hours.
//
// MUTANT (RED before m467): no reminder -> one push in 2h30m.
func TestStillPausedReminderHourly(t *testing.T) {
	Reset()
	defer Reset()
	r := capture()
	var cancelled []string
	SetCanceller(func(k string) { cancelled = append(cancelled, k) })
	Register("cross_frame", func(string, time.Time) (bool, string, error) { return true, "5m bar 23:55Z disagrees", nil })
	t0 := time.Date(2026, 11, 4, 0, 7, 23, 0, time.UTC) // 18:07:23 CT
	for m := 0; m <= 150; m++ {
		Evaluate("MNQ", t0.Add(time.Duration(m)*time.Minute))
	}
	var still []string
	for i, k := range r.kinds {
		if strings.HasPrefix(k, "data_truth_still_cross_frame|MNQ|") {
			still = append(still, r.texts[i])
		}
	}
	if len(still) != 2 {
		t.Fatalf("2h30m paused wants 2 STILL PAUSED reminders, got %d: %q", len(still), r.kinds)
	}
	if !strings.Contains(still[1], "STILL PAUSED") || !strings.Contains(still[1], "since 11-03 18:07 CT") || !strings.Contains(still[1], "2h00m") {
		t.Fatalf("reminder text: %q", still[1])
	}
	if len(cancelled) != 1 || !strings.HasSuffix(cancelled[0], "|1") {
		t.Fatalf("the second reminder must withdraw the first: %q", cancelled)
	}
}
