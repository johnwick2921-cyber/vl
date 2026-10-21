package trader

import (
	"regexp"
	"strings"
	"testing"
)

// ── r24 AC-1 — THE ADDON HONOURS close_position.account (C# source pins) ─────
//
// One NT8 AddOn serves two MNQ traders on two SIM accounts. Go stamps the
// trader's BOUND account on every close_position frame (tcp_trader.go
// sendCloseAt, `Account: t.boundAccount`); until build 2026-10-14-a the AddOn's
// HandleClosePosition never read it and flattened positionAccountBySymbol[root]
// — keyed by root alone, overwritten by every entry fill on ANY account —
// falling back to the AddOn's single active account. A flat trader's Emergency
// Flat therefore closed the OTHER trader's position, and two EOD flats hit the
// same account.
//
// The C# has no unit harness; NT8 compiles it only on the owner's F5. These
// pins read the .cs (like split_cs_pin_test.go) so a drift fails this suite,
// the out-of-NT8 dotnet compile proves it builds, and the PR's SIM steps prove
// it routes. Every pin below was mutated RED once (PR body).

// closeHandlerBody returns HandleClosePosition's source, from its signature to
// the next section header — the region every routing pin must hold inside.
func closeHandlerBody(t *testing.T) string {
	t.Helper()
	src := readSplitAddonSource(t)
	const sig = "private void HandleClosePosition(Dictionary<string, object> p)"
	if strings.Count(src, sig) != 1 {
		t.Fatalf("HandleClosePosition must be defined exactly once, found %d", strings.Count(src, sig))
	}
	start := strings.Index(src, sig)
	end := strings.Index(src[start:], "// === Account switching")
	if end < 0 {
		t.Fatal("cannot find the end of HandleClosePosition (the account-switching header)")
	}
	return src[start : start+end]
}

// TestAddonCloseRoutesToTheNamedAccountFailClosed — a close that NAMES its
// account resolves that account by name and ONLY that account: not found =
// refused (WARN + position_close_rejected), never the map, never the active
// account. Mutant: the not-found branch falls back to `closeTarget = account`
// → RED.
func TestAddonCloseRoutesToTheNamedAccountFailClosed(t *testing.T) {
	body := closeHandlerBody(t)
	for _, want := range []string{
		`string namedAccount = GetString(p, "account");`,
		`if (!string.IsNullOrEmpty(namedAccount))`,
		`closeTarget = FindAccountByName(namedAccount);`,
		`source = "payload";`,
		`"the named account '" + namedAccount + "' was not found in Account.All"`,
		`" → account=" + closeTarget.Name`,
		`" (from " + source + ")`,
		`if (!IsSimAccount(closeTarget))`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("HandleClosePosition lost the named-account routing: missing %q", want)
		}
	}
	// The named branch: resolve → null → REFUSE → return. Nothing between the
	// resolve and the refusal may pick another account.
	named := regexp.MustCompile(`(?s)closeTarget = FindAccountByName\(namedAccount\);\s*source = "payload";\s*if \(closeTarget == null\)\s*\{\s*RefuseClose\([^;]*;\s*return;\s*\}`)
	if !named.MatchString(body) {
		t.Fatal("the named-account branch must REFUSE and return when the account is not found — a fallback to another account is the cross-account flatten this build fixes")
	}
	// The named branch must not read the map or the active account at all:
	// both appear only after `else` (the legacy branch).
	namedBlock := body[strings.Index(body, `if (!string.IsNullOrEmpty(namedAccount))`):]
	namedBlock = namedBlock[:strings.Index(namedBlock, "else")]
	for _, banned := range []string{"positionAccountByAcctRoot", "closeTarget = account"} {
		if strings.Contains(namedBlock, banned) {
			t.Errorf("the named-account branch reads %q — a named close must touch only the named account", banned)
		}
	}
}

// TestAddonCloseLegacyBranchIsFailClosedOnAmbiguity — only a close that names
// NO account reads the owner map, and two holders of the root is a refusal,
// not "last fill wins". Mutant: drop the `holders.Count > 1` refusal → RED.
func TestAddonCloseLegacyBranchIsFailClosedOnAmbiguity(t *testing.T) {
	body := closeHandlerBody(t)
	for _, want := range []string{
		`if (holders.Count > 1)`,
		`" accounts hold " + root`,
		`if (holders.Count == 1) { closeTarget = holders[0]; source = "map"; }`,
		`else { closeTarget = account; source = "active"; }`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the legacy (no account named) branch lost its fail-closed shape: missing %q", want)
		}
	}
}

// TestAddonOneByNameResolverSharedByEntryAndClose — the entry path and the
// close path resolve a named account through ONE resolver, so the two cannot
// disagree about what an account name means. Mutant: re-inline the entry
// path's Account.All loop → RED.
func TestAddonOneByNameResolverSharedByEntryAndClose(t *testing.T) {
	src := readSplitAddonSource(t)
	if n := strings.Count(src, "private Account FindAccountByName(string name)"); n != 1 {
		t.Fatalf("FindAccountByName must be defined once, found %d", n)
	}
	if !strings.Contains(src, "resolved = FindAccountByName(targetAccount);") {
		t.Error("the entry path (HandleSignal) must resolve its named account through FindAccountByName")
	}
	if strings.Contains(src, "if (a.Name == targetAccount)") {
		t.Error("the entry path re-inlined its own by-name loop — two resolvers for one name")
	}
}

// TestAddonPositionOwnerMapIsPerAccount — the owner map is keyed by (root,
// account): an entry fill records ITS account without overwriting another's,
// and the root-only map is gone. Mutant: write `positionAccountBySymbol[entrySym]
// = e.Order.Account` back → RED.
func TestAddonPositionOwnerMapIsPerAccount(t *testing.T) {
	src := readSplitAddonSource(t)
	if strings.Contains(src, "positionAccountBySymbol") {
		t.Fatal("the root-only owner map positionAccountBySymbol is back — one account's fill retargets the other account's close")
	}
	for _, want := range []string{
		"private readonly Dictionary<string, Dictionary<string, Account>> positionAccountByAcctRoot",
		"lock (posAcctLock) { RecordPositionOwner(e.Order.Account, entrySym); }",
		"byAcct[acct.Name] = acct;",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("the per-account owner map lost: missing %q", want)
		}
	}
}

// TestAddonLimitExitSizedToTheWholePosition — the 4.3 limit exit (live:
// EOD_FLAT_LIMIT_TICKS > 0; Go sends quantity 0) is sized to the WHOLE
// position the resolved account holds, on the same resolved account, and is
// never placed against a flat / opposite / unreadable position. It used to
// turn quantity 0 into ONE contract, whose -lx fill then cancelled the
// bracket of the contracts still open. Mutant: restore
// `if (closeQty <= 0) closeQty = 1;` → RED.
func TestAddonLimitExitSizedToTheWholePosition(t *testing.T) {
	body := closeHandlerBody(t)
	if strings.Contains(body, "if (closeQty <= 0) closeQty = 1;") {
		t.Fatal("the limit exit turns quantity 0 into ONE contract again — a 5-lot gets a 1-lot limit and its -lx fill cancels the bracket of the other 4")
	}
	for _, want := range []string{
		"bool heldKnown = TryAccountPosition(closeTarget, instrument, out heldMp, out heldQty);",
		"bool sideMatches = heldKnown && heldQty > 0",
		"int closeQty = heldQty;",
		"var limitOrder = closeTarget.CreateOrder(",
		"closeTarget.Submit(new[] { limitOrder });",
		"closeTarget.Flatten(new[] { instrument });",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the limit/market close lost its account-and-size shape: missing %q", want)
		}
	}
}

// TestAddonCloseRefusalsAreLoud — every refusal in the close path sends the
// position_close_rejected frame Go's close-sync alarms on, carrying the
// account the close named so it routes to the trader that sent it. Mutant:
// drop the frame from RefuseClose → RED.
func TestAddonCloseRefusalsAreLoud(t *testing.T) {
	src := readSplitAddonSource(t)
	re := regexp.MustCompile(`(?s)private void RefuseClose\(string signalId, string root, string side, string acctName, string why\)\s*\{.*?LogWarn\(.*?SendPositionCloseRejectedFrame\(signalId, root, side, why, acctName\);`)
	if !re.MatchString(src) {
		t.Fatal("RefuseClose must WARN and send position_close_rejected with the named account — a silent refusal leaves Go believing the close was submitted")
	}
	body := closeHandlerBody(t)
	if n := strings.Count(body, "RefuseClose("); n < 5 {
		t.Errorf("HandleClosePosition has %d RefuseClose calls; want every refusal (instrument, named-not-found, ambiguous, no-active, non-SIM, exception) loud", n)
	}
}
