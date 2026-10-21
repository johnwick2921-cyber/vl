package updaterworker

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// U5: journalctl reads a bare "YYYY-MM-DD HH:MM:SS" as LOCAL time. FriendBootOK
// passed since.UTC().Format(...) with no zone, so on a box in America/Chicago
// the window landed five hours in the future and the scan matched nothing —
// FriendBootOK false for every friend on every update. The argv must use the
// epoch form @<unix>. This pin asserts the exact argv value; the mutant that
// reverts to the UTC wall-clock string fails here (RED) before it reaches a
// journalctl invocation.
func TestFriendBootArgsUsesEpochSince(t *testing.T) {
	since := time.Date(2026, 10, 21, 5, 41, 0, 0, time.UTC)
	args := friendBootArgs("vl-friend@alice", since)

	idx := -1
	for i, a := range args {
		if a == "--since" {
			idx = i
			break
		}
	}
	if idx < 0 || idx+1 >= len(args) {
		t.Fatalf("friendBootArgs must pass --since <value>, got %v", args)
	}
	val := args[idx+1]
	want := fmt.Sprintf("@%d", since.Unix())
	if val != want {
		t.Fatalf("--since value = %q, want epoch form %q (journalctl misreads a bare UTC wall-clock string as local)", val, want)
	}
	if !strings.HasPrefix(val, "@") {
		t.Fatalf("--since value must be the @<unix> epoch form, got %q", val)
	}
}
