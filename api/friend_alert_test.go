package api

// W-FRIEND-HOSTED Segment 6 (DS-106): the friend update receipt relay (CTO
// ruling 2026-10-18 item 3). The monitor reads the updater's job file
// friends[] receipts and sends them through the owner's Telegram — these tests
// pin the read, the plain-words rendering and the in-memory dedup.

import (
	"os"
	"path/filepath"
	"testing"

	"vl/trader"
)

func writeJobFile(t *testing.T, dataDir, name, body string) {
	t.Helper()
	jobs := filepath.Join(dataDir, "updater", "jobs")
	if err := os.MkdirAll(jobs, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobs, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func setFriendDataDir(t *testing.T, dir string) {
	t.Helper()
	prev := trader.MaintenanceDataDir()
	trader.SetMaintenanceDataDir(dir)
	t.Cleanup(func() { trader.SetMaintenanceDataDir(prev) })
}

func TestLatestFriendJobReceiptsReadsTheNewestJob(t *testing.T) {
	dd := t.TempDir()
	setFriendDataDir(t, dd)
	// An older job with no receipts, and a newer job with receipts.
	writeJobFile(t, dd, "job-0001.json", `{"schema":1,"job_id":"job-0001","created_at":"2026-10-18T01:00:00Z","state":"complete","friends":[]}`)
	writeJobFile(t, dd, "job-0002.json", `{"schema":1,"job_id":"job-0002","created_at":"2026-10-18T02:00:00Z","state":"complete","friends":[{"name":"tom","state":"restarted","rev":"v123"}]}`)

	rcs := latestFriendJobReceipts()
	if len(rcs) != 1 || rcs[0].Name != "tom" || rcs[0].State != "restarted" || rcs[0].Rev != "v123" {
		t.Fatalf("want tom restarted v123, got %+v", rcs)
	}
}

func TestLatestFriendJobReceiptsNilWhenNoDataDir(t *testing.T) {
	trader.SetMaintenanceDataDir("")
	if rcs := latestFriendJobReceipts(); rcs != nil {
		t.Fatalf("no data dir: want nil, got %+v", rcs)
	}
}

func TestFriendReceiptMessagePlainWords(t *testing.T) {
	cases := []struct {
		rc      friendJobReceipt
		want    string
		skip    bool // expect empty (nothing fabricated)
	}{
		{friendJobReceipt{Name: "tom", State: "restarted", Rev: "v1"}, `✅ Friend "tom" updated — now running v1.`, false},
		{friendJobReceipt{Name: "tom", State: "deferred", Note: "open position"}, `⏳ Friend "tom" update deferred (not flat). open position`, false},
		{friendJobReceipt{Name: "tom", State: "restored-restart", Rev: "v1"}, `⚠️ Friend "tom" update recovered with an extra restart (v1).`, false},
		{friendJobReceipt{Name: "tom", State: "failed", Note: "boot failed"}, `⚠️ Friend "tom" update failed. boot failed`, false},
		{friendJobReceipt{Name: "tom", State: "skipped"}, "", true},
		{friendJobReceipt{Name: "tom", State: "weird"}, "", true},
	}
	for _, c := range cases {
		got := friendReceiptMessage(c.rc)
		if c.skip && got != "" {
			t.Fatalf("%+v: want empty, got %q", c.rc, got)
		}
		if !c.skip && got != c.want {
			t.Fatalf("%+v: want %q, got %q", c.rc, c.want, got)
		}
	}
}

func TestRelayFriendReceiptsDedups(t *testing.T) {
	resetFriendAlertStateForTest()
	t.Cleanup(resetFriendAlertStateForTest)

	dd := t.TempDir()
	setFriendDataDir(t, dd)
	writeJobFile(t, dd, "job-0001.json", `{"schema":1,"job_id":"job-0001","created_at":"2026-10-18T02:00:00Z","state":"complete","friends":[{"name":"tom","state":"restarted","rev":"v123"}]}`)

	s := &Server{}
	first := s.relayFriendReceipts()
	if len(first) != 1 {
		t.Fatalf("first relay: want 1 message, got %d (%v)", len(first), first)
	}
	second := s.relayFriendReceipts()
	if len(second) != 0 {
		t.Fatalf("second relay must be deduped to 0, got %d (%v)", len(second), second)
	}
}
