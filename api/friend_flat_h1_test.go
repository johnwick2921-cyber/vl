package api

import (
	"errors"
	"testing"

	trader "vl/trader"
)

// H1 — /api/friend/flat adds "known" and "last_known_flat_ms" WITHOUT changing
// flat semantics: flat=true only when ≥1 trader AND all known+fresh AND no
// position/order. known=false on any read error / stale / zero traders.
// last_known_flat_ms latches the latest known-and-flat instant and freezes once
// a trader drops out of the known state.

func TestFriendFlatKnownField(t *testing.T) {
	resetFriendFlatLastKnownForTest()
	defer resetFriendFlatLastKnownForTest()

	s := newFriendFlatServer(t, true, map[string]trader.Trader{
		"t1": fakeFlatTrader{known: true},
	})
	_, body := getFriendFlat(t, s)
	if body["flat"] != true {
		t.Fatalf("flat = %v, want true", body["flat"])
	}
	if body["known"] != true {
		t.Fatalf("known = %v, want true", body["known"])
	}
	if v, ok := body["last_known_flat_ms"].(float64); !ok || v <= 0 {
		t.Fatalf("last_known_flat_ms = %v, want a positive unix-ms", body["last_known_flat_ms"])
	}
}

func TestFriendFlatKnownFalseWhenDisconnected(t *testing.T) {
	resetFriendFlatLastKnownForTest()
	defer resetFriendFlatLastKnownForTest()

	s := newFriendFlatServer(t, true, map[string]trader.Trader{
		"t1": fakeFlatTrader{known: false},
	})
	_, body := getFriendFlat(t, s)
	if body["known"] != false {
		t.Fatalf("known = %v, want false (NT disconnected)", body["known"])
	}
	if body["flat"] != false {
		t.Fatalf("flat = %v, want false", body["flat"])
	}
}

func TestFriendFlatKnownTrueWithOpenPosition(t *testing.T) {
	resetFriendFlatLastKnownForTest()
	defer resetFriendFlatLastKnownForTest()

	// Position state is KNOWN (fresh, connected) but NOT flat — known stays true.
	s := newFriendFlatServer(t, true, map[string]trader.Trader{
		"t1": fakeFlatTrader{known: true, positions: []map[string]interface{}{{"positionAmt": float64(2)}}},
	})
	_, body := getFriendFlat(t, s)
	if body["flat"] != false {
		t.Fatalf("flat = %v, want false (open position)", body["flat"])
	}
	if body["known"] != true {
		t.Fatalf("known = %v, want true (position state was known, just not flat)", body["known"])
	}
}

func TestFriendFlatKnownFalseOnReadError(t *testing.T) {
	resetFriendFlatLastKnownForTest()
	defer resetFriendFlatLastKnownForTest()

	s := newFriendFlatServer(t, true, map[string]trader.Trader{
		"t1": fakeFlatTrader{known: true, err: errors.New("probe down")},
	})
	_, body := getFriendFlat(t, s)
	if body["known"] != false {
		t.Fatalf("known = %v, want false (position-read error is UNKNOWN)", body["known"])
	}
}

func TestFriendFlatEmptyInstanceNotKnown(t *testing.T) {
	resetFriendFlatLastKnownForTest()
	defer resetFriendFlatLastKnownForTest()

	s := newFriendFlatServer(t, true, nil)
	_, body := getFriendFlat(t, s)
	if body["known"] != false {
		t.Fatalf("known = %v, want false (zero traders has nothing known)", body["known"])
	}
	if v := body["last_known_flat_ms"].(float64); v != 0 {
		t.Fatalf("last_known_flat_ms = %v, want 0 (never flat)", v)
	}
}

func TestFriendFlatLatchFreezesAfterNT8Close(t *testing.T) {
	resetFriendFlatLastKnownForTest()
	defer resetFriendFlatLastKnownForTest()

	// Known + flat probe latches the instant.
	s1 := newFriendFlatServer(t, true, map[string]trader.Trader{
		"t1": fakeFlatTrader{known: true},
	})
	_, body1 := getFriendFlat(t, s1)
	t0, ok := body1["last_known_flat_ms"].(float64)
	if !ok || t0 <= 0 {
		t.Fatalf("first probe latch = %v, want positive", body1["last_known_flat_ms"])
	}

	// NT8 closes → known=false. The latch must NOT advance.
	s2 := newFriendFlatServer(t, true, map[string]trader.Trader{
		"t1": fakeFlatTrader{known: false},
	})
	_, body2 := getFriendFlat(t, s2)
	if body2["known"] != false {
		t.Fatalf("post-close known = %v, want false", body2["known"])
	}
	if got := body2["last_known_flat_ms"].(float64); got != t0 {
		t.Fatalf("post-close latch = %v, want %v (frozen at the last all-clear)", got, t0)
	}
}
