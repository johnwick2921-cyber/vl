package mentor

import "testing"

// r22 P1-4 — the role check at the three LADDER call sites (oldExtremeRoleFits):
// a long's target / leg extreme is an old HIGH, never a nearer old LOW; a short's
// is an old LOW, never a nearer old HIGH. The level sets put the WRONG-role
// extreme nearer in the trade direction, so a role check that admits every old
// extreme (mutant: `return true` in oldExtremeRoleFits) picks it and goes RED.

// r22RoleLadder: long side — an old LOW at 103 nearer than the old HIGH at 108;
// short side — an old HIGH at 97 nearer than the old LOW at 92. No other level.
func r22RoleLadder() []Level {
	return []Level{
		{Key: "old_extreme:103.00", Kind: KindOldExtreme, Price: 103, Role: OldLow},
		{Key: "old_extreme:108.00", Kind: KindOldExtreme, Price: 108, Role: OldHigh},
		{Key: "old_extreme:97.00", Kind: KindOldExtreme, Price: 97, Role: OldHigh},
		{Key: "old_extreme:92.00", Kind: KindOldExtreme, Price: 92, Role: OldLow},
	}
}

// TestR22NextLevelBeyondSkipsTheWrongRole — eval.go nextLevelBeyond (the PHL
// first-obstacle target and the mode-A runner target).
func TestR22NextLevelBeyondSkipsTheWrongRole(t *testing.T) {
	if got := nextLevelBeyond(r22RoleLadder(), 100, SideLong); got != 108 {
		t.Fatalf("long from 100: next level = %.2f, want the old HIGH 108 — never the nearer old LOW 103", got)
	}
	if got := nextLevelBeyond(r22RoleLadder(), 100, SideShort); got != 92 {
		t.Fatalf("short from 100: next level = %.2f, want the old LOW 92 — never the nearer old HIGH 97", got)
	}
}

// TestR22NextLevelBeyondRoomSkipsTheWrongRole — eval.go nextLevelBeyondRoom
// (the ISB / reverse-ISB / box target ladder).
func TestR22NextLevelBeyondRoomSkipsTheWrongRole(t *testing.T) {
	if got := nextLevelBeyondRoom(r22RoleLadder(), 100, 98, SideLong, 2); got != 108 {
		t.Fatalf("long entry 100 stop 98: target = %.2f, want the old HIGH 108 — never the nearer old LOW 103", got)
	}
	if got := nextLevelBeyondRoom(r22RoleLadder(), 100, 102, SideShort, 2); got != 92 {
		t.Fatalf("short entry 100 stop 102: target = %.2f, want the old LOW 92 — never the nearer old HIGH 97", got)
	}
}

// TestR22LegExtremeSkipsTheWrongRole — limits.go nearestOldExtreme (the G1 leg
// extreme and the G2 anchor): a long leg runs to the old HIGH.
func TestR22LegExtremeSkipsTheWrongRole(t *testing.T) {
	if got := nearestOldExtreme(r22RoleLadder(), 100, SideLong); got != 108 {
		t.Fatalf("long from 100: leg extreme = %.2f, want the old HIGH 108 — never the nearer old LOW 103", got)
	}
	if got := nearestOldExtreme(r22RoleLadder(), 100, SideShort); got != 92 {
		t.Fatalf("short from 100: leg extreme = %.2f, want the old LOW 92 — never the nearer old HIGH 97", got)
	}
}
