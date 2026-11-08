package ownerpush

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// m471 review 1 — supersededLabel matched every "data_truth_" kind, so the
// per-FILL fact "data_truth_fill_price_far|<sym>|<fill id>"
// (internal/datatruth/checks/fillprice.go: one push per far fill, never raised
// again after a restart because the restored guard puts the fill id back in its
// seen set) was withdrawn at boot, named "DATA TRUTH PAUSED", and the boot
// message promised "the bot re-checks now and tells you again" - which it never
// does. Only the datatruth PAUSE episode shapes are superseded now.
//
// MUTANT (shown RED): supersededLabel's data_truth_ cases without
// isDataTruthEpisodeKey -> "a held far-fill P0 ... was withdrawn at boot" -> RED.
func TestRestoreAtBootDeliversDataTruthFactsAndSupersedesOnlyEpisodes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	f := &flaky{fail: errDNS}
	p, c := newClocked(f.send)
	c.t = time.Date(2026, 11, 3, 15, 0, 0, 0, time.UTC)
	enableState(t, p, path)
	facts := []string{
		"data_truth_fill_price_far|MNQ|ord-123",
		"data_truth_fill_price_far|MNQ|4471", // a numeric fill id is still a fill, not a sinceMs
		"data_truth_fill_price_state|/x/fillguard.json",
	}
	episodes := []string{
		"data_truth_fill_price|MNQ|1762182000000",
		"data_truth_fill_price|MNQ|1762182000000|widen2",
		"data_truth_still_fill_price|MNQ|1762182000000|1",
	}
	for _, k := range append(append([]string{}, facts...), episodes...) {
		p.Push(k, "MSG "+k)
		p.Flush()
	}
	waitState(t, path, func(d *stateDisk) bool { return len(d.Held) == len(facts)+len(episodes) })

	q, c2 := newClocked(f.send)
	c2.t = c.now().Add(10 * time.Minute)
	info := q.RestoreAtBoot(path)
	t.Cleanup(q.CloseState)
	if q.Held() != len(facts) || info.Restored != len(facts) {
		_, text := BootMessage("abc", info, false, c2.now())
		t.Fatalf("a held far-fill / fill-state P0 (a per-fill fact, not a pause episode) was withdrawn at boot: held=%d restored=%d superseded=%v boot=%q",
			q.Held(), info.Restored, info.Superseded, text)
	}
	if got := strings.Join(info.Superseded, ","); !strings.Contains(got, "DATA TRUTH PAUSED") || !strings.Contains(got, "DATA TRUTH STILL PAUSED") {
		t.Fatalf("the fill_price PAUSE episode keys must still be superseded: %v", info.Superseded)
	}
	f.set(nil)
	drive(q, c2, 2*time.Minute)
	sent := strings.Join(f.sent(), "\n")
	for _, k := range facts {
		if !strings.Contains(sent, "MSG "+k) {
			t.Errorf("a data-truth FACT restored at boot was not delivered: %q", k)
		}
	}
}

func TestIsDataTruthEpisodeKeyShapes(t *testing.T) {
	for key, want := range map[string]bool{
		"data_truth_bar_gap|MNQ|1762156800000":            true,
		"data_truth_bar_gap|MNQ|1762156800000|widen3":     true,
		"data_truth_bar_gap|MNQ|1762156800000|clear":      false,
		"data_truth_bar_gap|MNQ|abc":                      false,
		"data_truth_bar_gap|MNQ":                          false,
		"data_truth_fill_price_far|MNQ|1762156800000":     false,
		"data_truth_fill_price_state|/p":                  false,
		"data_truth_bar_gap|MNQ|1762156800000|widen":      false,
		"data_truth_bar_gap||1762156800000":               false,
		"data_truth_still_bar_gap|MNQ|1762156800000|2":    true,
		"data_truth_still_bar_gap|MNQ|1762156800000":      false,
		"data_truth_still_bar_gap|MNQ|1762156800000|x":    false,
		"data_truth_bar_gap|MNQ|1762156800000|widen1|zzz": false,
	} {
		still := strings.HasPrefix(key, "data_truth_still_")
		if got := isDataTruthEpisodeKey(key, still); got != want {
			t.Errorf("isDataTruthEpisodeKey(%q) = %v, want %v", key, got, want)
		}
	}
}

// m471 review 2 — RestoreAtBoot took the superseded keys out of the state file
// at Start, but their only record was BootInfo.Superseded (memory) until main.go
// pushed the boot message, after the traders load (a logger.Fatalf, a panic or
// a kill in between). A boot that died there erased them: the next boot neither
// re-sent nor named them. The names now stay on file (boot_note) until a boot
// message is admitted; the boot message itself is then on file (held / in
// flight) and carries them.
//
// MUTANT (shown RED): EnableState does not load prev.BootNote -> "the FEED DOWN
// the owner never received is gone after a crash" -> RED. MUTANT 2: Push never
// calls clearBootNoteLocked -> "boot 4 must not name them again" -> RED.
func TestSupersededNamesSurviveACrashBeforeTheBootMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	f := &flaky{fail: errDNS}
	p, c := newClocked(f.send)
	c.t = time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC) // 02:00 CT
	enableState(t, p, path)
	p.Push("feed-down|feeddown:2026-11-03:02:00 CT", "FEED DOWN")
	p.Flush()
	waitState(t, path, func(d *stateDisk) bool { return len(d.Held) == 1 })

	// boot 2: RestoreAtBoot runs, then the process dies before the boot message
	q, c2 := newClocked(f.send)
	c2.t = c.now().Add(5 * time.Minute)
	info2 := q.RestoreAtBoot(path)
	if len(info2.Superseded) != 1 {
		t.Fatalf("setup: boot 2 should supersede the FEED DOWN: %+v", info2)
	}
	q.CloseState() // the writer has flushed; the process now dies (no boot push)

	// boot 3: still names it; this time the boot message is pushed (Telegram
	// still down, so it is HELD - on file) and the process dies again.
	r, c3 := newClocked(f.send)
	c3.t = c2.now().Add(time.Minute)
	info3 := r.RestoreAtBoot(path)
	if len(info3.Superseded) != 1 || !strings.Contains(info3.Superseded[0], "FEED DOWN 11-03 02:00 CT") {
		r.CloseState()
		t.Fatalf("the FEED DOWN the owner never received is gone after a crash between RestoreAtBoot and the boot push: boot3=%+v held=%d", info3, r.Held())
	}
	bootKey, bootText := BootMessage("abc", info3, false, c3.now())
	if !strings.Contains(bootText, "FEED DOWN 11-03 02:00 CT") {
		r.CloseState()
		t.Fatalf("boot message does not name the carried alert: %q", bootText)
	}
	r.Push(bootKey, bootText)
	r.Flush()
	r.CloseState()
	if d, err := readState(path); err != nil || d == nil || len(d.BootNote) != 0 {
		t.Fatalf("the boot note must be cleared once the boot message is admitted: %+v err=%v", d, err)
	}

	// boot 4: the names are not listed twice; the held boot message (which
	// names them) is restored and delivered.
	s, c4 := newClocked(f.send)
	c4.t = c3.now().Add(time.Minute)
	info4 := s.RestoreAtBoot(path)
	t.Cleanup(s.CloseState)
	if len(info4.Superseded) != 0 {
		t.Fatalf("boot 4 must not name them again (boot 3's held boot message does): %+v", info4)
	}
	f.set(nil)
	drive(s, c4, 2*time.Minute)
	if sent := strings.Join(f.sent(), "\n"); !strings.Contains(sent, "FEED DOWN 11-03 02:00 CT - not re-sent") {
		t.Fatalf("boot 3's held boot message naming the FEED DOWN was not delivered: %q", sent)
	}
}

// The carried list is bounded; names past maxBootNote are counted and said.
func TestBootNoteOverflowIsCountedInTheBootMessage(t *testing.T) {
	info := BootInfo{PrevExit: PrevExitUnclean, Superseded: []string{"A", "B", "C", "D", "E"}, SupersededMore: 3}
	_, text := BootMessage("abc", info, false, time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC))
	if !strings.Contains(text, "A, B, C, D and 4 more") {
		t.Fatalf("overflow must be counted (1 unlisted + 3 past the cap): %q", text)
	}
}
