package store

// W-FRIEND-HOSTED B5 S12 (DS-106) — "owner box unchanged", part (c): the owner
// boot line (Owner.BootLine) is byte-identical to its pre-friend-wave form. The
// friend wave adds a SEPARATE friendBootLine (main.go) that is emitted ONLY in
// friend mode; the owner line itself never mentions friends.

import (
	"strings"
	"testing"
)

func TestOwnerBootLineUnchangedByFriendWave(t *testing.T) {
	// Resolved owner — the exact string origin/dev 6606b84bf printed.
	o := Owner{
		UserID:       "396db319-0000-0000-0000-000000000000",
		Source:       OwnerSourceTraders,
		OwnedTraders: 2,
		TotalTraders: 2,
	}
	const wantResolved = "👤 owner: user 396db319 (source traders · owns 2 of 2 traders)"
	if got := o.BootLine(); got != wantResolved {
		t.Fatalf("owner boot line changed: got %q, want %q", got, wantResolved)
	}

	// Unresolved owner — the exact string origin/dev 6606b84bf printed.
	none := Owner{Reason: OwnerReasonNoUsers}
	const wantNone = "👤 owner: NONE — no users and no traders (nobody has registered); owner-only routes refuse, the Telegram bot is off"
	if got := none.BootLine(); got != wantNone {
		t.Fatalf("unresolved owner boot line changed: got %q, want %q", got, wantNone)
	}

	// The friend wave adds a SEPARATE friendBootLine; the owner line must not
	// mention friends at all.
	for name, got := range map[string]string{"resolved": o.BootLine(), "none": none.BootLine()} {
		if strings.Contains(got, "friend") {
			t.Fatalf("owner boot line (%s) leaked friend text: %q", name, got)
		}
	}
}
