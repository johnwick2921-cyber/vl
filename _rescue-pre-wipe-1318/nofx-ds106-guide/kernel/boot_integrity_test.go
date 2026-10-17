package kernel

import (
	"os"
	"strings"
	"testing"
)

// P1 — the assertion itself. Both paths are exercised: a declared release that
// MATCHES (trading allowed) and one that does NOT (trading refused).
func TestAssertBootIntegrityBothPaths(t *testing.T) {
	t.Cleanup(func() { SetTradingRefusedForTest(false, "") })

	// ── path A: no expectation declared → never refuses, goldens still checked
	t.Setenv("NOFX_EXPECTED_REVISION", "")
	a := AssertBootIntegrity()
	if !a.GoldensOK {
		t.Fatalf("goldens must pass in a healthy tree: %+v", a.Goldens)
	}
	if a.Refused {
		t.Fatalf("no declared release must NOT refuse trading: %s", a.Reason)
	}
	if _, refused := TradingRefused(); refused {
		t.Fatal("gate must be open when nothing is violated")
	}
	if !strings.Contains(a.Line(), "BOOT INTEGRITY OK") {
		t.Fatalf("boot line should read OK: %q", a.Line())
	}

	// ── path B: an expectation that CANNOT match → refuse to trade
	t.Setenv("NOFX_EXPECTED_REVISION", "0000000000000000000000000000000000000000")
	b := AssertBootIntegrity()
	if b.RevisionOK {
		t.Fatal("a bogus expected revision must not match")
	}
	if !b.Refused {
		t.Fatal("a revision mismatch MUST refuse trading (the stale-binary control)")
	}
	reason, refused := TradingRefused()
	if !refused || !strings.Contains(reason, "stale binary") {
		t.Fatalf("gate must be closed with an explanatory reason, got %q", reason)
	}
	if !strings.Contains(b.Line(), "BOOT INTEGRITY REFUSED") {
		t.Fatalf("boot line should read REFUSED: %q", b.Line())
	}

	// ── back to A: re-asserting a good state re-opens the gate (restart recovers)
	t.Setenv("NOFX_EXPECTED_REVISION", "")
	if c := AssertBootIntegrity(); c.Refused {
		t.Fatal("clearing the expectation must clear the refusal on the next boot")
	}
	if _, refused := TradingRefused(); refused {
		t.Fatal("gate must reopen after a clean assertion")
	}
}

// P1 — a SHORT sha in deploy/RELEASE matches the full embedded revision.
func TestExpectedRevisionPrefixMatch(t *testing.T) {
	t.Cleanup(func() { SetTradingRefusedForTest(false, "") })
	rev, _, _ := buildStamp()
	if rev == "" {
		t.Skip("binary built without VCS stamping (go test w/o -buildvcs)")
	}
	t.Setenv("NOFX_EXPECTED_REVISION", rev[:7]) // the short sha an operator would paste
	got := AssertBootIntegrity()
	if !got.RevisionOK || got.Refused {
		t.Fatalf("short-sha prefix must match the full revision: exp=%q rev=%q", rev[:7], rev)
	}
}

// P1 — deploy/RELEASE is honored when the env var is unset (the file is the
// operator-friendly way to declare the intended release).
func TestExpectedRevisionFromReleaseFile(t *testing.T) {
	t.Cleanup(func() { SetTradingRefusedForTest(false, "") })
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.MkdirAll(dir+"/deploy", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/deploy/RELEASE", []byte("# intended release\nabc1234\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOFX_EXPECTED_REVISION", "")
	if got := expectedRevision(); got != "abc1234" {
		t.Fatalf("deploy/RELEASE not read (comments must be skipped): got %q", got)
	}
}

// P1 — the refusal is readable by the entry gate from anywhere (atomic, no lock).
func TestTradingRefusedLatch(t *testing.T) {
	t.Cleanup(func() { SetTradingRefusedForTest(false, "") })
	SetTradingRefusedForTest(true, "stale binary: rev abc but expected def")
	reason, refused := TradingRefused()
	if !refused || reason == "" {
		t.Fatal("latch must report refused + reason")
	}
	SetTradingRefusedForTest(false, "")
	if _, refused := TradingRefused(); refused {
		t.Fatal("latch must clear")
	}
}
