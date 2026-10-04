// EVERY CANCEL SENDER IS GUARDED (2026-09-07, D4).
//
// The 09-06 wave guarded seven cancel sites in armed_executor.go and missed two
// others that send the identical frame — cancelOtherArmsInPlan and the class-27
// desync sweep. Nothing caught that, because each site was reviewed on its own.
//
// So the pin is a CENSUS, not a list: it finds every call that puts a
// cancel_order on the wire and requires a guard in the same function. A new
// sender added next month fails this without anyone remembering to add it here.

package trader

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// cancelSendRe matches a cancel going to the NT8 trader. The CEX/DEX brokers
// take (symbol, orderID) and are a different method on a different interface;
// this one takes a single signal id.
//
// It deliberately does NOT require a following "(": the first draft did, and
// routing a sender through cancelSignalIfSafeWith(nt.CancelOrder, ...) — passing
// the method as a VALUE — dropped it out of the census entirely. The count fell
// from 8 to 7 and the pin went green partly by going blind. A censor that stops
// seeing what it censors is worse than none.
// THE SUBJECT IS THE RAW WIRE, not the seam. Matching `cancelFn(` instead was
// wrong in both directions: it missed the two dispatchers that hand the raw
// method down unguarded, and it flagged reconcileStaleWorking and
// confirmPendingCancels, which receive a closure that has ALREADY adjudicated
// (armed_executor.go:1088 and :1099). So: every reference to the NT8 trader's
// CancelOrder — called or passed as a value — must sit in a function that
// guards, or delegate to one that does.
var cancelSendRe = regexp.MustCompile(`\b(nt|ntTrader|trader)\.CancelOrder\b`)

// delegatingSenders hand the RAW wire to a seam that adjudicates per row. The
// delegation is not taken on trust: the named callee is checked for a guard, so
// this exemption cannot rot into a hole.
var delegatingSenders = map[string]string{}

// guardedBy names the two adjudicators. Both end in adjudicateArmCancel.
var guardRe = regexp.MustCompile(`cancelSafetyFor\(|cancelSignalIfSafe`)

// exemptSenders are the functions allowed to send an unguarded cancel, each
// with the reason it is exempt. A test seam reachable only from a debug
// endpoint is not a trading path.
var exemptSenders = map[string]string{
	"TestArmCancel":          "explicit test seam, debug endpoint only — never reached by the trading loop",
	"cancelSignalIfSafeWith": "this IS the guard; it adjudicates before it sends",

	// ── DELIBERATELY UNGUARDED, 2026-09-07 ──────────────────────────────────
	//
	// The census found these two beyond the two the owner named, and guarding
	// them was tried and REVERTED. The guard refuses when there is no broker
	// book, and on these two paths refusing is the MORE dangerous failure:
	//
	//   cancelArmedOrdersSyncWith is what flattens at session close and on a
	//   news halt. A refusal there leaves live arms standing into an EOD
	//   flatten or a news event. (TestSListEODFlatCancelsArmsBeforeFlatten and
	//   TestT1NewsFlatTraderArmCancelled both went red when it was guarded.)
	//
	//   sweepPreBootArmsWith retires orders orphaned by a dead process. A
	//   refusal leaves them resting — the class-33 double-order of 2026-09-02
	//   00:16 CT. It already has its OWN answer to a missing link: it DEFERS
	//   without latching and retries next cycle, which is the same
	//   conservatism expressed where it belongs.
	//
	// And the harm the Go guard exists to prevent is fixed at its source by
	// D1: HandleCancelOrder no longer touches placedBrackets, so a cancel can
	// no longer reach a protective order however it is sent. The Go guard is
	// defence in depth on the paths where refusing is cheap; these are not
	// those paths. Named here rather than silently passing, so the trade is
	// visible to whoever reads this next.
	"cancelArmedOrdersSync":         "session-close/news flatten — a refusal leaves arms live into an EOD flatten or a news halt",
	"cancelArmedOrdersSyncFiltered": "session-close/news flatten — a refusal leaves arms live into an EOD flatten or a news halt (B3: the origin-filtered form of cancelArmedOrdersSync)",
	"sweepPreBootArms":              "class-33 orphan sweep — a refusal leaves a dead process's orders resting; it defers on a missing link instead",
}

// stripGoLineComments blanks // comments while preserving line numbering.
func stripGoLineComments(src string) string {
	out := make([]string, 0, 512)
	inBlock := false
	for _, ln := range strings.Split(src, "\n") {
		if inBlock {
			if i := strings.Index(ln, "*/"); i >= 0 {
				ln, inBlock = ln[i+2:], false
			} else {
				out = append(out, "")
				continue
			}
		}
		if i := strings.Index(ln, "/*"); i >= 0 {
			if j := strings.Index(ln[i:], "*/"); j >= 0 {
				ln = ln[:i] + ln[i+j+2:]
			} else {
				ln, inBlock = ln[:i], true
			}
		}
		if i := strings.Index(ln, "//"); i >= 0 {
			ln = ln[:i]
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}

// guardedFunc reports whether the named function's body contains a guard.
func guardedFunc(t *testing.T, name string) bool {
	t.Helper()
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		src := stripGoLineComments(string(b))
		i := strings.Index(src, ") "+name+"(")
		if i < 0 {
			continue
		}
		body := src[i:]
		if j := strings.Index(body[1:], "\nfunc "); j >= 0 {
			body = body[:j+1]
		}
		return guardRe.MatchString(body)
	}
	t.Errorf("delegation target %s not found at all", name)
	return false
}

func TestEveryCancelSenderIsGuarded(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	funcRe := regexp.MustCompile(`^func (?:\([^)]*\) )?([A-Za-z0-9_]+)\(`)

	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		// Comments are not senders. Without this the census counts the prose
		// that DESCRIBES a cancel — the same class of error as a pin satisfied
		// by its own documentation, arriving from the other direction.
		lines := strings.Split(stripGoLineComments(string(b)), "\n")

		// walk the file tracking the enclosing function
		fnName, fnStart := "", 0
		for i, ln := range lines {
			if m := funcRe.FindStringSubmatch(ln); m != nil {
				fnName, fnStart = m[1], i
			}
			if !cancelSendRe.MatchString(ln) {
				continue
			}
			checked++
			if why, ok := exemptSenders[fnName]; ok {
				t.Logf("exempt: %s:%d %s — %s", f, i+1, fnName, why)
				continue
			}
			if callee, ok := delegatingSenders[fnName]; ok {
				if !guardedFunc(t, callee) {
					t.Errorf("%s:%d — %s hands the raw wire to %s, which does NOT guard. "+
						"The delegation this exemption rests on is gone.", f, i+1, fnName, callee)
				} else {
					t.Logf("delegates: %s:%d %s → %s (verified guarded)", f, i+1, fnName, callee)
				}
				continue
			}
			// the enclosing function's body, so far and onward to its end
			end := len(lines)
			for j := i + 1; j < len(lines); j++ {
				if strings.HasPrefix(lines[j], "func ") {
					end = j
					break
				}
			}
			body := strings.Join(lines[fnStart:end], "\n")
			if !guardRe.MatchString(body) {
				t.Errorf("%s:%d — %s sends a cancel_order with NO filled-arm guard.\n"+
					"    NT8 resolves a cancel by SIGNAL ID. If that signal's entry has already\n"+
					"    filled, the only orders left under it are its protective stop and target.\n"+
					"    This is the 2026-09-06 23:37:02 path. Route it through cancelSafetyFor\n"+
					"    (when a ledger row is in hand) or cancelSignalIfSafe (signal id only).",
					f, i+1, fnName)
			}
		}
	}
	// A FLOOR, NOT A ZERO CHECK. Every known sender must still be visible: if a
	// refactor renames the receiver or hides the call behind a wrapper, the
	// census must fail rather than quietly examine fewer sites.
	const knownSenders = 8
	if checked < knownSenders {
		t.Errorf("the census sees only %d cancel senders, expected at least %d — a sender has become "+
			"invisible to this pin (a renamed receiver, or the method passed somewhere it is not "+
			"matched). Fix the census before trusting its green.", checked, knownSenders)
	}
	t.Logf("cancel senders examined: %d", checked)
}
