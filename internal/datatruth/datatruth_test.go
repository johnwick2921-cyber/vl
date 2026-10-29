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
	if !strings.HasPrefix(rec.kinds[0], "data_truth|c1|MNQ|") {
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
	// second episode: a NEW alert with a NEW key
	bad.Store(true)
	Evaluate("MNQ", t0.Add(2*time.Minute))
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
