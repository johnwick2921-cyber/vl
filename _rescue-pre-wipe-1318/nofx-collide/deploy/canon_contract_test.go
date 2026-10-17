// Class 105 — the contract test the canon mirror was missing.
//
// docs/superpowers/CLAUDE-canon.md exists because ~/nofx/CLAUDE.md is UNTRACKED:
// no wave can correct it, no review can see it drift, no test can check it. The
// mirror closed the git half of that. It did NOT close the test half — nothing
// asserted the mirror still described deploy/nofx-lock.sh, so the mirror was
// class 105 with one more copy in it: a second piece of prose about the code,
// free to drift from the code AND from the original, independently and silently.
//
// This file is that assertion. It pins the three behavioural claims the canon
// makes about the keeper against the script itself:
//
//  1. acquire SPAWNS the keeper          (canon: "acquire now spawns the keeper itself")
//  2. the expiry is enforced AT cmd_heartbeat, the single place a beat is written
//  3. keeper.pid is a STOP HANDLE ONLY — never liveness (class 70 stays green)
//
// Each check reads BOTH sides. A change to the script that makes the canon wrong
// fails here, and so does a rewrite of the canon that stops describing the
// script. Which side to correct is the reader's judgement; the failure message
// names both so the choice is informed.
//
// Deliberately NOT asserted: prose wording, section order, or anything cosmetic.
// A contract test that breaks on an edited sentence trains people to delete it.
package deploy

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	lockScriptPath = "nofx-lock.sh"
	canonPath      = "../docs/superpowers/CLAUDE-canon.md"
)

// flattenProse collapses every whitespace run to a single space. Markdown wraps
// sentences at the column, so a phrase this test cares about is routinely split
// across a newline — matching raw text makes the assertion fail on reflow rather
// than on meaning, which is the fastest way to get a contract test deleted.
func flattenProse(s string) string {
	return regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// shellFunc returns the body of a POSIX-shell function `name() {` up to the
// first line that is exactly "}". Reading the BODY rather than the whole file is
// what makes assertion 2 meaningful: "the expiry is enforced at cmd_heartbeat"
// is a claim about WHERE the check lives, and a whole-file grep cannot tell the
// difference between the guard being in the one place every writer passes
// through and it being somewhere convenient.
func shellFunc(t *testing.T, script, name string) string {
	t.Helper()
	lines := strings.Split(script, "\n")
	start := -1
	for i, ln := range lines {
		if strings.HasPrefix(ln, name+"() {") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("deploy/nofx-lock.sh no longer defines %s().\n"+
			"CLAUDE-canon.md describes the keeper in terms of this function; if it was renamed,\n"+
			"update the canon's description and this test together.", name)
	}
	for i := start + 1; i < len(lines); i++ {
		if lines[i] == "}" {
			return strings.Join(lines[start:i+1], "\n")
		}
	}
	t.Fatalf("%s() in deploy/nofx-lock.sh has no closing brace at column 0", name)
	return ""
}

// TestCanonMirrorDescribesTheLockScript is the whole point of the file: the
// canon's claims and the script's behaviour, checked against each other.
func TestCanonMirrorDescribesTheLockScript(t *testing.T) {
	script := readRepoFile(t, lockScriptPath)
	canon := readRepoFile(t, canonPath)

	t.Run("acquire spawns the keeper", func(t *testing.T) {
		acquire := shellFunc(t, script, "cmd_acquire")
		// Substring matching is not enough: renaming the call to _spawn_keeper_disabled,
		// or commenting it out, leaves "_spawn_keeper" in the text. Require a real call —
		// the name at the start of a statement, followed by whitespace or end of line.
		callsSpawn := regexp.MustCompile(`(?m)^[ \t]*_spawn_keeper([ \t]|$)`).MatchString(acquire)
		if !callsSpawn {
			t.Errorf("cmd_acquire() no longer calls _spawn_keeper.\n"+
				"CLAUDE-canon.md tells every lane that acquire starts the heartbeat for them and that\n"+
				"hand-beating is therefore a second writer. If acquire has stopped spawning, that\n"+
				"instruction is now DANGEROUS in the opposite direction — a lane that does not hand-beat\n"+
				"has no heartbeat at all and its lock goes STALE under a live holder.\n"+
				"Fix the script or fix %s. Do not delete this test.", canonPath)
		}
		if !regexp.MustCompile(`(?i)acquire[^.]*spawns the keeper|STARTS THE KEEPER`).MatchString(flattenProse(canon)) {
			t.Errorf("%s no longer states that acquire starts the keeper, but cmd_acquire() still calls\n"+
				"_spawn_keeper. The mirror has drifted away from the script it exists to describe.", canonPath)
		}
	})

	t.Run("the expiry is enforced at cmd_heartbeat", func(t *testing.T) {
		hb := shellFunc(t, script, "cmd_heartbeat")
		// cmd_heartbeat contains more than one REFUSED (a missing lock dir is also
		// refused), so the bare word proves nothing about the expiry. Pin the expiry
		// refusal itself, and the comparison that reaches it.
		hasCompare := strings.Contains(hb, "expiry_epoch") &&
			strings.Contains(hb, "past the declared expiry") &&
			regexp.MustCompile(`-ge "\$exp"`).MatchString(hb)
		if !hasCompare {
			t.Errorf("cmd_heartbeat() no longer compares expiry_epoch and refuses past it.\n"+
				"%s states the bound is enforced HERE precisely because this is the single place a\n"+
				"heartbeat can be written, which is what makes it bind every writer including a\n"+
				"hand-rolled one. Enforcing anywhere else constrains only the keeper this script starts.\n"+
				"Class 101 is the version of this defect where a bound is written, printed and never\n"+
				"compared.", canonPath)
		}
		// The canon is explicit that a pre-existing lock stays unbounded on purpose.
		// If that guard goes, a live holder's next beat can be refused mid-cutover.
		if hasCompare && !strings.Contains(hb, `[ -n "$exp" ]`) {
			t.Errorf("cmd_heartbeat() enforces the expiry without the empty-value guard.\n"+
				"%s promises that a lock acquired before this landed carries no expiry_epoch and stays\n"+
				"UNBOUNDED, deliberately — retroactively bounding a live lock could refuse a holder's\n"+
				"next heartbeat in the middle of a cutover.", canonPath)
		}
		if !strings.Contains(flattenProse(canon), "cmd_heartbeat") {
			t.Errorf("%s no longer names cmd_heartbeat as the enforcement point.", canonPath)
		}
	})

	t.Run("keeper.pid is a stop handle, never liveness", func(t *testing.T) {
		// The class-70 invariant: liveness is the heartbeat and ONLY the heartbeat.
		// A pid answers "does a process exist", which is the wrong question — that is
		// the whole reason the flat pid file was replaced.
		if regexp.MustCompile(`kill -0[^\n]*keeper`).MatchString(script) ||
			regexp.MustCompile(`keeper\.pid[^\n]*kill -0`).MatchString(script) {
			t.Errorf("deploy/nofx-lock.sh probes keeper.pid with `kill -0`.\n" +
				"That makes a pid answer the liveness question again, which is exactly the failure\n" +
				"class 70 replaced: a dead pid under a working owner, and a resumed session naming\n" +
				"its own former process. Liveness is the heartbeat and only the heartbeat.")
		}

		// The ALIVE/STALE decision must rest on heartbeat age, not on the keeper.
		status := shellFunc(t, script, "cmd_status")
		// The name appears in cmd_status's own output string as well, so its presence
		// proves nothing. Pin the comparison that actually produces the verdict.
		if !regexp.MustCompile(`-gt "\$HEARTBEAT_STALE_SECONDS"`).MatchString(status) {
			t.Errorf("cmd_status() no longer decides STALE from HEARTBEAT_STALE_SECONDS.\n" +
				"If the staleness verdict has moved onto the keeper, a pid is deciding liveness again.")
		}

		// keeper.pid may be written, removed, displayed, and read to STOP the keeper.
		// It may not be read anywhere else — every other read is a liveness read waiting
		// to happen.
		// _spawn_keeper writes it, _stop_keeper reads it to signal and removes it,
		// _auto_beat tests its EXISTENCE to report auto-beat on/ENDED/off. Those three
		// and no others.
		allowed := map[string]bool{"_spawn_keeper": true, "_stop_keeper": true, "_auto_beat": true}
		for _, fn := range shellFuncsMentioning(script, "keeper.pid") {
			if !allowed[fn] {
				t.Errorf("keeper.pid is read in %s(), which is not one of the functions allowed to touch it.\n"+
					"%s calls it a STOP HANDLE ONLY. A new reader is how it becomes a liveness signal by\n"+
					"accident — if this read is legitimate, add it here deliberately and say why.", fn, canonPath)
			}
		}

		// The canon: status reports auto-beat "read from the file, never by probing a
		// process". _auto_beat is where that promise lives or dies.
		autoBeat := shellFunc(t, script, "_auto_beat")
		for _, probe := range []string{"kill ", "/proc/", "ps -", "pgrep"} {
			if strings.Contains(autoBeat, probe) {
				t.Errorf("_auto_beat() probes a process (%q).\n"+
					"%s promises the auto-beat line is read FROM THE FILE and never by probing. A probe\n"+
					"here reintroduces pid-as-liveness through the status surface, which is the one\n"+
					"surface every other lane trusts when deciding whether to reclaim.", probe, canonPath)
			}
		}

		if !regexp.MustCompile(`(?i)stop handle only`).MatchString(flattenProse(canon)) {
			t.Errorf("%s no longer describes keeper.pid as a stop handle only.", canonPath)
		}
		if !regexp.MustCompile(`(?i)heartbeat and only the heartbeat`).MatchString(flattenProse(canon)) {
			t.Errorf("%s no longer states that liveness is the heartbeat and only the heartbeat —\n"+
				"the class-70 invariant this whole design rests on.", canonPath)
		}
	})
}

// shellFuncsMentioning returns the names of top-level shell functions whose body
// contains needle. Lines outside any function are attributed to "" and ignored,
// which is correct here: the constants at the top of the script name paths, not
// behaviour.
func shellFuncsMentioning(script, needle string) []string {
	def := regexp.MustCompile(`^([_a-zA-Z][_a-zA-Z0-9]*)\(\) \{`)
	var out []string
	cur := ""
	seen := map[string]bool{}
	for _, ln := range strings.Split(script, "\n") {
		if m := def.FindStringSubmatch(ln); m != nil {
			cur = m[1]
			continue
		}
		if ln == "}" {
			cur = ""
			continue
		}
		if cur != "" && strings.Contains(ln, needle) && !seen[cur] {
			seen[cur] = true
			out = append(out, cur)
		}
	}
	return out
}

// TestCanonMirrorIsReachableFromTheUntrackedFile records the half of class 105's
// law that no wave can close.
//
// The law is: a rule about the code lives in a tracked file with a contract test,
// and untracked guidance POINTS AT IT. The first two clauses are satisfied by
// CLAUDE-canon.md plus this file. The third cannot be — CLAUDE.md is untracked,
// so no branch can add the pointer; only the owner can.
//
// This test therefore asserts only what is assertable: that the canon file states
// its own precedence, so a reader who arrives holding the stale CLAUDE.md learns
// which copy wins. It is deliberately not a check on CLAUDE.md, which is invisible
// to git and would make this test pass or fail on an untracked file's contents.
func TestCanonMirrorDeclaresItsOwnPrecedence(t *testing.T) {
	canon := readRepoFile(t, canonPath)
	if !regexp.MustCompile(`(?i)newer by construction|CLAUDE\.md is stale`).MatchString(flattenProse(canon)) {
		t.Errorf("%s no longer declares that it wins over CLAUDE.md.\n"+
			"CLAUDE.md is untracked: no wave can make it point here, so this file saying so is the\n"+
			"only pointer that exists. Without it a reader has two documents and no precedence rule.", canonPath)
	}
}

// ── THE FOURTH CLAIM: check's rc SET ─────────────────────────────────────────
//
// ADDED 2026-09-10, after this file passed green across a change that made the
// canon wrong. The lock wave added `clear-incomplete` and rc 3/4 to cmd_check;
// the canon still listed "rc 0 free · 1 held · 2 stale" and five verbs. Nothing
// failed, because the verb list and the rc codes were a claim this file had
// never pinned — and I read the green run as confirmation that the canon was
// still accurate.
//
// THAT IS THE FAILURE MODE OF CONTRACT TESTS AS SUCH: a green result speaks only
// about the assertions present, and silence about everything else is
// indistinguishable from approval. The header above lists what was deliberately
// excluded as cosmetic; an rc contract is not cosmetic, it was simply not
// thought of. The remedy is not "be more careful" — it is to pin the surface, so
// the next code that grows a return code fails here instead of drifting.
// canonCheckLine returns the canon's single line documenting the `check` verb —
// the list a lane reads to learn what the return codes mean. Scoping the rc
// assertion to this line is what stops it being satisfied by prose elsewhere.
func canonCheckLine(t *testing.T, canon string) string {
	t.Helper()
	for _, ln := range strings.Split(canon, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "deploy/nofx-lock.sh check") {
			return ln
		}
	}
	t.Fatalf("%s has no `deploy/nofx-lock.sh check` line.\n"+
		"The canon is the copy lanes are told to trust over the untracked CLAUDE.md;\n"+
		"if the verb block was restructured, this test must be taught the new shape.", canonPath)
	return ""
}

func TestCanonNamesEveryCheckReturnCode(t *testing.T) {
	script := readRepoFile(t, lockScriptPath)
	canon := readRepoFile(t, canonPath)

	// Every `return N` inside cmd_check — the codes the script can actually hand
	// a caller. Read from the function BODY, never from a list kept beside it.
	body := shellFunc(t, script, "cmd_check")
	codes := map[string]bool{}
	for _, m := range regexp.MustCompile(`return (\d+)`).FindAllStringSubmatch(body, -1) {
		codes[m[1]] = true
	}
	if len(codes) < 3 {
		t.Fatalf("cmd_check yielded only %d return codes (%v) — the parse is wrong, not the script", len(codes), codes)
	}

	// SCOPED TO THE CANON'S OWN rc LIST, not the whole file.
	//
	// The first draft of this assertion searched the entire canon for "rc N".
	// A mutation that deleted rc 4 from the authoritative `check` line SURVIVED,
	// because a paragraph further down happened to say "rc 4 means …". The test
	// was satisfied by any mention anywhere, which is not the claim: the claim is
	// that the LIST a lane reads to learn check's contract is complete. An
	// assertion satisfied by prose is an assertion about prose.
	line := canonCheckLine(t, canon)
	for code := range codes {
		if strings.Contains(line, code) {
			continue
		}
		t.Errorf(`cmd_check can return %s, and the canon's rc list does not name it.

  SCRIPT : deploy/nofx-lock.sh cmd_check returns %s
  CANON  : %s check line reads: %s

One of the two is wrong and this test does not know which. If the code is new,
the canon's rc list needs it. If the code was removed, the canon describes a
contract the script no longer offers.`, code, code, canonPath, line)
	}
}

// TestCanonNamesEveryVerb pins the verb block for the same reason: a verb that
// exists and is undocumented is as much a drift as a documented verb that does
// not exist, and the canon is the copy lanes are told to trust over the
// untracked CLAUDE.md.
func TestCanonNamesEveryVerb(t *testing.T) {
	script := readRepoFile(t, lockScriptPath)
	canon := readRepoFile(t, canonPath)

	// The dispatch table is the authority on which verbs exist.
	tail := script
	if i := strings.LastIndex(tail, `case "${1:-status}" in`); i >= 0 {
		tail = tail[i:]
	}
	verbs := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s{2}([a-z][a-z-]*)\)\s`).FindAllStringSubmatch(tail, -1) {
		verbs[m[1]] = true
	}
	if len(verbs) < 5 {
		t.Fatalf("parsed only %d verbs from the dispatch table (%v) — the parse is wrong, not the script", len(verbs), verbs)
	}
	flat := flattenProse(canon)
	for v := range verbs {
		if strings.Contains(flat, "nofx-lock.sh "+v) {
			continue
		}
		t.Errorf(`the script accepts the verb %q and the canon never names it.

  SCRIPT : deploy/nofx-lock.sh dispatches %q
  CANON  : %s has no "nofx-lock.sh %s" line

A verb lanes cannot find is a verb they will not use — and this file is the copy
they are told to trust over the untracked CLAUDE.md.`, v, v, canonPath, v)
	}
}
