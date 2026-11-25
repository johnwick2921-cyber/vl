package trader

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Release 2 (2026-11-24) — source pins of the settle gate's WIRING. Its behaviour is
// tested outside NT8 by deploy/addon/harness/settlegate (pure) and settlewire (the real
// client); these pins hold what a harness cannot see.

func settleGateSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(addonSourceDir + "/VLSettleGate.cs")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func settleClientCode(t *testing.T) string { return stripCSharpComments(addonSource(t)) }

func wantCount(t *testing.T, src, what string, n int, why string) {
	t.Helper()
	if got := strings.Count(src, what); got != n {
		t.Errorf("%q appears %d times, want %d: %s", what, got, n, why)
	}
}

func inOrder(t *testing.T, body string, parts ...string) {
	t.Helper()
	at := 0
	for _, p := range parts {
		i := strings.Index(body[at:], p)
		if i < 0 {
			t.Fatalf("%q missing or out of order (want %q)", p, parts)
		}
		at += i + len(p)
	}
}

func TestSettleGateRunsBeforeBothHandlersAndNowhereElse(t *testing.T) {
	code := settleClientCode(t)
	frame := csharpMethodBody(t, addonSource(t), "private void HandleFrame(")
	inOrder(t, frame, `type == "signal"`, "lock (CmdLockFor(payload)) { HandleSignal(payload); }",
		`type == "close_position"`, "lock (CmdLockFor(payload))", "if (!TryHoldClose(payload, out closeNow)) HandleClosePosition(closeNow);",
		`type == "place_protective_stop"`, "lock (CmdLockFor(payload))", "if (!RefuseNeedlessStop(payload, out stopNow)) HandlePlaceProtectiveStop(stopNow);")
	wantCount(t, code, "TryHoldClose(", 2, "definition + HandleFrame")
	wantCount(t, code, "RefuseNeedlessStop(", 2, "definition + HandleFrame")
	wantCount(t, code, "HandleClosePosition(", 3, "definition + HandleFrame + the one release path")
	wantCount(t, code, "HandlePlaceProtectiveStop(", 2, "definition + HandleFrame: a stop is never held")
	wantCount(t, code, "HandleSignal(", 2, "definition + HandleFrame")
	if body := csharpMethodBody(t, addonSource(t), "private void ReleaseHeldClose("); !strings.Contains(body, "HandleClosePosition(") {
		t.Error("a held close must be released through HandleClosePosition")
	}
}

// The gate has no refusal: it only delays a close.
func TestSettleGateNeverRefusesAClose(t *testing.T) {
	src := addonSource(t)
	parts := []string{stripCSharpComments(settleGateSource(t))}
	for _, m := range []string{"private bool TryHoldClose(", "private void ReleaseHeldClose(", "private void ReleaseHeldBeforeEntry(",
		"private void RunSettleLoop(", "private void StopSettleGate(", "private void SweepOwnExits("} {
		parts = append(parts, csharpMethodBody(t, src, m))
	}
	for _, p := range parts {
		for _, bad := range []string{"RefuseClose", "SendPositionCloseRejectedFrame", "position_close_moot"} {
			if strings.Contains(p, bad) {
				t.Errorf("the settle gate contains %s — it may only delay a close", bad)
			}
		}
	}
	// any fault in TryHoldClose = run it now, and never in two places
	hold := csharpMethodBody(t, src, "private bool TryHoldClose(")
	inOrder(t, hold, "g.Hold(h, now);", "catch (Exception ex)", "g.Take(token)", "return false;", "settleWake.Set();", "return true;")
}

func TestSettleGateStampsFlattenImmediatelyBeforeTheCall(t *testing.T) {
	code := settleClientCode(t)
	wantCount(t, code, ".Flatten(", 1, "one Flatten call in the AddOn, the stamped one")
	body := csharpMethodBody(t, addonSource(t), "private void HandleClosePosition(")
	if !regexp.MustCompile(`NoteOwnFlatten\([^;]*;\s*try \{ closeTarget\.Flatten\(new\[\] \{ instrument \}\); \}\s*catch \{ NoteOwnFlattenFailed\(closeTarget, instrument\); throw; \}`).MatchString(body) {
		t.Error("NoteOwnFlatten must be the statement before closeTarget.Flatten, and a throwing Flatten must clear it and rethrow")
	}
}

func TestSettleGateOwnExitIsRegisteredBeforeSubmit(t *testing.T) {
	src := addonSource(t)
	inOrder(t, csharpMethodBody(t, src, "private void HandleClosePosition("),
		"OwnLxCovers(closeTarget", "var limitOrder = closeTarget.CreateOrder(", "NoteOwnExit(closeTarget, instrument, limitOrder, true);",
		"closeTarget.Submit(new[] { limitOrder });", "DropOwnExit(limitOrder); throw;", "MarkOwnExitSubmitted(limitOrder);")
	inOrder(t, csharpMethodBody(t, src, "private void HandlePlaceProtectiveStop("),
		"NoteOwnExit(ba, instrument, slOrder, false);", "ba.Submit(new[] { slOrder });", "DropOwnExit(slOrder); throw;", "MarkOwnExitSubmitted(slOrder);")
	wantCount(t, settleClientCode(t), "NoteOwnExit(", 3, "definition, -lx, standalone -sl: never a bracket leg or an entry")
	for _, b := range []string{csharpMethodBody(t, src, "private void SweepOwnExits("), stripCSharpComments(settleGateSource(t))} {
		if strings.Contains(b, "o.Name") || strings.Contains(b, "EndsWith(") {
			t.Error("the sweep must match our own Order objects, never a name")
		}
	}
}

func TestSettleGateThreadsNeverWaitOnNT8(t *testing.T) {
	code := settleClientCode(t)
	wantCount(t, code, "Thread.Sleep(", 1, "the heartbeat loop only: the reader never sleeps")
	wantCount(t, code, "WaitOne(", 2, "the connection loop and the settle loop only")
	src := addonSource(t)
	for _, m := range []string{"private void OnLedgerOrderUpdate(", "private void StampSettlePosition(", "private void NoteSettleConnection("} {
		b := csharpMethodBody(t, src, m)
		for _, bad := range []string{"CmdLockFor", "LogInfo", "LogWarn", "WriteEnvelope", "Send", "TryAccountPosition", "IsSimAccount", "Monitor.", "lock ("} {
			if strings.Contains(b, bad) {
				t.Errorf("%s contains %s — NT8's one SIM fill thread would wait on it", m, bad)
			}
		}
	}
	for _, m := range []string{"private void OnOrderUpdate(", "private void OnPositionUpdate(", "private void SettleCancelledEntryNote("} {
		if strings.Contains(csharpMethodBody(t, src, m), "CmdLockFor") {
			t.Errorf("%s takes a command lock on NT8's thread", m)
		}
	}
	inOrder(t, csharpMethodBody(t, src, "private void OnPositionUpdate("), "StampSettlePosition(sender, e);", "SendOpenPositions(")
	inOrder(t, csharpMethodBody(t, src, "private void SubscribeOrderUpdate("), "HookLedger(a);", "a.OrderUpdate += OnOrderUpdate;")
	inOrder(t, code, "ResolveAccount();", "StartSettleGate();", "SubscribeOrderUpdate(account);")
	inOrder(t, code, "cts?.Cancel();", "StopSettleGate();", "a.OrderUpdate -= OnOrderUpdate;")
	if strings.Contains(csharpMethodBody(t, src, "private void UnsubscribeOrderUpdate("), "OnLedgerOrderUpdate") {
		t.Error("account_select must not unhook the ledger")
	}
}

// P1-4: Account.Cancel / Submit / Change / Flatten raise OnOrderUpdate on the caller's
// thread; none may run inside signalMapLock.
func TestSignalMapLockIsNeverHeldAcrossAnNT8Call(t *testing.T) {
	code := settleClientCode(t)
	const lk = "lock (signalMapLock)"
	n := 0
	for at := 0; ; {
		i := strings.Index(code[at:], lk)
		if i < 0 {
			break
		}
		open := at + i + len(lk)
		open += strings.IndexByte(code[open:], '{')
		depth, j := 0, open
		for ; j < len(code); j++ {
			if code[j] == '{' {
				depth++
			} else if code[j] == '}' {
				depth--
				if depth == 0 {
					break
				}
			}
		}
		block := code[open:j]
		for _, bad := range []string{".Cancel(", ".Submit(", ".Change(", ".Flatten(", "CreateOrder(", "WriteEnvelope(", "LogInfo("} {
			if strings.Contains(block, bad) {
				t.Errorf("signalMapLock is held across %s:\n%s", bad, block)
			}
		}
		n++
		at = open
	}
	if n < 15 {
		t.Fatalf("only %d signalMapLock blocks found — the pin lost its subject", n)
	}
}

func TestSettleGateEntryReleaseComesAfterEveryRefusal(t *testing.T) {
	body := csharpMethodBody(t, addonSource(t), "private void HandleSignal(")
	inOrder(t, body, "if (maintenanceHeld)", "stale signal", "IsSessionAccount(submitAccount.Name)", "seenSignals[signalId] = DateTime.UtcNow;",
		"ReleaseHeldBeforeEntry(submitAccount, instrument, signalId);", "submitAccount.CreateOrder(", "EntryOrder = entryOrder")
	if i := strings.Index(body, "ReleaseHeldBeforeEntry("); strings.Contains(body[i:], `"rejected"`) && strings.Count(body[i:], `"rejected"`) > 1 {
		t.Error("a refusal after ReleaseHeldBeforeEntry: a refused entry must release nothing")
	}
}

func TestSettleGateMaintenanceAckReportsItsDepth(t *testing.T) {
	ack := csharpMethodBody(t, addonSource(t), "private Dictionary<string, object> BuildMaintenanceAck(")
	if !strings.Contains(ack, `["queued_commands"] = SettlePendingCount(),`) {
		t.Error("queued_commands must be the gate's real depth, not a literal")
	}
}

func TestSettleGateNoteSurvivesACancelRequest(t *testing.T) {
	src := addonSource(t)
	b := csharpMethodBody(t, src, "private void HandleCancelOrder(")
	if !regexp.MustCompile(`if \(!KeepBracketNote\(\)\)\s*\{\s*lock \(signalMapLock\) \{ pendingBrackets\.Remove\(signalId\); \}\s*\}`).MatchString(b) {
		t.Error("HandleCancelOrder may drop the bracket note only with keep_note=0")
	}
	inOrder(t, csharpMethodBody(t, src, "private void OnOrderUpdate("),
		"SendOrderUpdateFrame(e);", "if (e.OrderState == OrderState.Cancelled) SettleCancelledEntryNote(e);", "e.OrderState != OrderState.Filled")
}

// The "c0" lesson (#48): no new const string (its blob can land behind the build id), one
// id-shaped literal, and nothing in the new file that manifest.sh or a reader can mistake.
func TestSettleGateFileRules(t *testing.T) {
	all, err := filepath.Glob(addonSourceDir + "/*.cs")
	if err != nil {
		t.Fatal(err)
	}
	consts, ids := 0, 0
	idRe := regexp.MustCompile(`"20\d\d-\d\d-\d\d-[A-Za-z0-9]+"`)
	for _, f := range all {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		code := stripCSharpComments(string(b))
		consts += strings.Count(code, "const string")
		ids += len(idRe.FindAllString(code, -1))
	}
	if consts != 3 || ids != 1 {
		t.Errorf("const strings %d (want 3), id-shaped literals %d (want 1)", consts, ids)
	}
	raw := settleGateSource(t)
	for _, bad := range []string{"PROTOCOL_VERSION", "VL_BUILD_ID", "const string"} {
		if strings.Contains(raw, bad) { // comments too: manifest.sh takes the first text hit
			t.Errorf("VLSettleGate.cs contains %q", bad)
		}
	}
	for _, bad := range []string{"using NinjaTrader", "DateTime", "Thread.", "Account.", "LogWarn"} {
		if strings.Contains(stripCSharpComments(raw), bad) {
			t.Errorf("VLSettleGate.cs code contains %q", bad)
		}
	}
	if m := regexp.MustCompile(`(?m)^    (public|protected)`).FindString(raw); m != "" {
		t.Error("VLSettleGate.cs must declare internal types only")
	}
}

// move_stop, modify_bracket and cancel_order run OUTSIDE the per-account command lock, on
// purpose: they can only Change or Cancel an order that already exists, so beside a held
// close's release they cannot make a second exit or a reverse — and inside it a stuck
// release (a socket stall of 8.5 s was seen) would delay a stop move or an entry cancel.
// The day one of them submits an order or sizes itself from a position, it needs the lock.
func TestSettleGateUnlockedCommandsNeverSubmit(t *testing.T) {
	src := addonSource(t)
	for _, m := range []string{"private void HandleMoveStop(", "private void HandleModifyBracket(", "private void HandleCancelOrder("} {
		b := csharpMethodBody(t, src, m)
		for _, bad := range []string{".Submit(", "CreateOrder(", ".Flatten(", "TryAccountPosition(", ".Positions"} {
			if strings.Contains(b, bad) {
				t.Errorf("%s contains %s: put it under CmdLockFor", m, bad)
			}
		}
	}
}

// The gate tells the owner whether it is armed, and only from its own thread or after a
// close has run.
func TestSettleGateStateIsLogged(t *testing.T) {
	src := addonSource(t)
	inOrder(t, csharpMethodBody(t, src, "private void SettleUpkeep("), "SettleUpkeepAccount(g, a);", "SettleWarnLimited(", "g.TakeDrops()", "settle gate DISARMED")
	inOrder(t, csharpMethodBody(t, src, "private void SettleUpkeepAccount("), "HookLedger(a);", "if (!ledgerSubscribed.Contains(a)) return;",
		"g.Upkeep(", "settle gate ARMED", "g.UntrustedDue(", "settle gate NOT ARMED")
	inOrder(t, csharpMethodBody(t, src, "private void HandleFrame("), "HandleClosePosition(closeNow);", "LogSettleRanNow();")
	inOrder(t, csharpMethodBody(t, src, "private void StartSettleGate("), "catch (Exception ex)", "settleGate.StopSweeps();")
	inOrder(t, csharpMethodBody(t, src, "private void HookLedger("), "try", "IsSimAccount(a)", "catch (Exception ex)")
}
