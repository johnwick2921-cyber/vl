// WAVE B (2026-09-05) — the replay, the A29 call-site pin, and the D4 boot line.

package trader

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	ntwire "vl/provider/ninjatrader"
)

// ny0904S2Prices are the 21 cycle prices the executor actually saw on
// 2026-09-04 between 10:05:00 and 10:53:11 CT, one per placement, read from the
// "📏 arm far … from price X" line in data/vl_2026-09-04.log. They belong to
// armed_orders ids 38, 62, 65, 67, 70, 73, 75, 77, 79, 81, 83, 85, 87, 89, 91,
// 93, 95, 97, 99, 101, 102 — NY / v3 / S2 / SHORT, entry_px 29591.02, wire
// trigger 29590.50 (n=21, and 21 is the INVESTIGATORS' measured submission
// count, not the dispatch's framing of one arm).
var ny0904S2Prices = []float64{
	29515.25, 29524.75, 29540.50, 29536.25, 29501.00, 29500.50, 29505.00,
	29503.75, 29505.50, 29487.75, 29497.00, 29500.75, 29495.50, 29503.50,
	29511.50, 29521.00, 29518.75, 29495.50, 29501.50, 29500.25, 29500.25,
}

const ny0904S2Trigger = 29590.50

// TestReplayNY0904S2ThroughTheFixedGuard — E7.
//
// CORRECTION TO THE DISPATCH, and it is the whole point of shipping D1 with D2:
// the dispatch expects "ONE well-formed submission with stopPrice set". Replayed
// against the MEASURED prices the fixed path yields ZERO submissions. At every
// one of the 21 cycles the market was 50.00-102.75 points BELOW a SELL-stop
// trigger — squarely already-through — so the correct answer is 21 cancels and
// no order at all. The 21 that were sent were sent because the guard was
// inverted; the only reason they did no harm is that D1's zero stop slot made
// them inert. A well-formed submission appears in the valid-side replay below.
func TestReplayNY0904S2ThroughTheFixedGuard(t *testing.T) {
	placed, cancelled := 0, 0
	for i, price := range ny0904S2Prices {
		v, why := stopEntryGuardVerdict("short", ny0904S2Trigger, price)
		switch v {
		case stopGuardThrough:
			cancelled++
			if !strings.Contains(why, "accepted through (stop side)") {
				t.Errorf("cycle %d: reason does not name the guard: %q", i, why)
			}
		case stopGuardRest:
			placed++
			t.Errorf("cycle %d: price %.2f is %.2f pts BELOW a sell-stop trigger %.2f and must never be placed",
				i, price, ny0904S2Trigger-price, ny0904S2Trigger)
		default:
			t.Errorf("cycle %d: unadjudicated with a real price and trigger: %s", i, why)
		}
	}
	if len(ny0904S2Prices) != 21 {
		t.Fatalf("the replay must carry all 21 measured cycles, got %d", len(ny0904S2Prices))
	}
	if placed != 0 || cancelled != 21 {
		t.Fatalf("replay of the 21 measured cycles: placed=%d cancelled=%d, want 0/21", placed, cancelled)
	}

	// The valid side of the same arm: price ABOVE the sell-stop trigger. Exactly
	// one placement, and the frame carries the trigger in stop_price with an
	// empty limit slot (the wire half is pinned in
	// trader/ninjatrader/stop_entry_wire_test.go; here we pin the decision).
	v, why := stopEntryGuardVerdict("short", ny0904S2Trigger, 29650.00)
	if v != stopGuardRest {
		t.Fatalf("a sell stop with the market above its trigger must rest: %v (%s)", v, why)
	}
	if !strings.Contains(why, "rests (stop side)") {
		t.Fatalf("resting reason must say so: %q", why)
	}
}

// TestReplayArm35LimitUnchanged — E4. Arm 35 (NY, S1, SHORT, entry_px 29285.00,
// stop_px 29351.63, target_px 29144.50, state filled, 2026-09-03) is a LIMIT
// arm. Its adjudication must be byte-for-byte the behaviour it had before this
// wave: the limit predicate is untouched and is still what the limit branch
// calls. Nothing about the stop-side fix may reach it.
func TestReplayArm35LimitUnchanged(t *testing.T) {
	const arm35Entry = 29285.00
	for _, c := range []struct {
		price float64
		want  bool
		note  string
	}{
		{29280.00, false, "market below a sell limit — rests"},
		{29285.00, false, "exactly at a sell limit — still rests (STRICT boundary, unlike a stop)"},
		{29290.00, true, "market above a sell limit — marketable"},
	} {
		if got := limitMarketableWrongSide(c.price, arm35Entry, "short"); got != c.want {
			t.Errorf("arm 35 limit replay price=%.2f (%s): got %v want %v", c.price, c.note, got, c.want)
		}
	}
	// The two predicates disagree at exactly the level, which is the difference
	// this wave exists to preserve: a limit AT its price rests, a stop AT its
	// trigger fires.
	if limitMarketableWrongSide(arm35Entry, arm35Entry, "short") {
		t.Error("a limit at its own price must rest")
	}
	if !stopEntryMarketableWrongSide("short", arm35Entry, arm35Entry) {
		t.Error("a stop at its own trigger must read as already through")
	}
}

// TestStopEntryGuardHasAProductionCallSite — E8 / A29. Built is not wired. The
// stop-entry placement branch must call the STOP guard, and must no longer call
// the limit predicate with the trigger. Removing the call breaks this test.
func TestStopEntryGuardHasAProductionCallSite(t *testing.T) {
	b, err := os.ReadFile("armed_executor.go")
	if err != nil {
		t.Fatalf("cannot read the placement source: %v", err)
	}
	src := string(b)
	if !strings.Contains(src, "d := decideStopEntry(side, r.EntryPx, float64(stopEntryOffsetTicks())*tick, tick, price)") {
		t.Error("the stop-entry branch does not call decideStopEntry — the adjudication is built but not wired")
	}
	// cancel-confirmation (2026-09-06) added the slot guard as the call's last
	// argument. The assertion keeps its purpose — the dispatch is made HERE and
	// nowhere else — and now also pins that the guard is adjudicated at the call.
	if !strings.Contains(src, "at.placeOneStopEntry(nt, ledger, r, d, price, now, at.armSlotGuard(rows, r, now), entryGroupSiblingSignalIDs(rows, r))") {
		t.Error("the stop-entry branch does not dispatch the decision — nothing acts on the verdict")
	}
	if strings.Contains(src, "limitMarketableWrongSide(price, trigger,") {
		t.Error("the stop-entry branch still calls the LIMIT predicate with the trigger — the inversion is back")
	}
	// The limit branch must keep its own predicate, with the ENTRY argument.
	// `side` is the canonical lowercase fold of r.Side (class 77); the predicate
	// case-folds anyway, but the value handed to the WIRE must be the folded one.
	if !strings.Contains(src, "limitMarketableWrongSide(price, r.EntryPx, side)") {
		t.Error("the limit branch lost limitMarketableWrongSide — the limit path was not supposed to move")
	}
	// NO RAW LEDGER SIDE MAY REACH THE WIRE. The store canonicalizes Side to
	// UPPERCASE and the AddOn reads `side == "long" ? Buy : SellShort`, so
	// PlaceLimitEntry/PlaceStopEntry must be handed the folded value.
	if strings.Contains(src, "PlaceLimitEntry(at.futuresSymbol(), r.Side,") ||
		strings.Contains(src, "PlaceStopEntry(at.futuresSymbol(), r.Side,") {
		t.Error("the placement branch hands the RAW ledger side to the wire — an uppercase LONG submits as a live SellShort")
	}
	// D5's refusal must be counted, not swallowed.
	if !strings.Contains(src, "errors.Is(perr, ntwire.ErrAddonBuildTooOld)") {
		t.Error("a build refusal is not distinguished from a transport failure — it cannot be counted")
	}
}

// TestStopEntryBootLineIsRead — D4 / A11. Every field is resolved from the code
// that enforces it; none is a literal. A proven build reads slots=stop_price and
// match=yes, an unproven one must say so on both.
func TestStopEntryBootLineIsRead(t *testing.T) {
	// match=yes means RECEIVED == EXPECTED, so the received build here must be
	// ExpectedAddonBuild. Passing MinAddonBuildStopSlot only worked while the two
	// constants coincided; the moment the AddOn shipped a newer build than the
	// stop-slot floor, this asserted match=yes on a mismatch.
	proven := StopEntryBootLine(ntwire.ExpectedAddonBuild, ntwire.ExpectedAddonBuild, true)
	for _, want := range []string{"🎯 stop-entry:", "slots=stop_price", "guard=stop-side", "unknown=no-op", "match=yes"} {
		if !strings.Contains(proven, want) {
			t.Errorf("proven-build line missing %q: %s", want, proven)
		}
	}
	if !strings.Contains(proven, "build_id="+ntwire.ExpectedAddonBuild) {
		t.Errorf("the line must name the RECEIVED build id: %s", proven)
	}

	// The build NT8 is running today (the one whose CreateOrder put the trigger
	// in the limit slot) must not be able to render as proven.
	old := StopEntryBootLine("2026-09-03-f12", ntwire.ExpectedAddonBuild, true)
	if strings.Contains(old, "slots=stop_price") {
		t.Errorf("a pre-stop-slot build must not claim slots=stop_price: %s", old)
	}
	if !strings.Contains(old, "match=NO") || !strings.Contains(old, "build_id=2026-09-03-f12") {
		t.Errorf("a stale DLL must read match=NO with its own id: %s", old)
	}

	// No frame received yet: "none", never an empty string that reads as data.
	none := StopEntryBootLine("", ntwire.ExpectedAddonBuild, true)
	if !strings.Contains(none, "build_id=none") || strings.Contains(none, "build_id= ") {
		t.Errorf("an unknown build must render as none: %s", none)
	}
	if strings.Contains(none, "slots=stop_price") {
		t.Errorf("an unheard-from AddOn cannot prove the slot: %s", none)
	}
	// The expected half is READ from the source constant, never restated.
	if !strings.Contains(proven, "expected="+ntwire.ExpectedAddonBuild) {
		t.Errorf("expected= must be the source constant: %s", proven)
	}
}

// TestStopEntryBootLineReEmitsWhenTheBuildArrives — the build id arrives
// ASYNCHRONOUSLY: farSideBuildID() is "" until a hello or heartbeat lands, while
// armedTrader() is non-nil with no far-side connection at all. A latch on
// "having emitted" therefore pinned `slots=unproven … build_id=none … match=NO`
// for the life of the process whenever the first armed cycle beat the AddOn's
// first frame — and the sibling 🔌 line, which reads the same value under the
// same latch, did exactly that on 3 of its 8 observed emissions. Dedupe on the
// rendered LINE instead: the none→proven transition is recorded exactly once,
// and a steady state still prints once.
func TestStopEntryBootLineReEmitsWhenTheBuildArrives(t *testing.T) {
	at := &AutoTrader{id: "boot-line-trader"}
	stopEntryBootLogged.Delete(at.id)
	defer stopEntryBootLogged.Delete(at.id)

	emit := func(received string) (string, bool) {
		line := StopEntryBootLine(received, ntwire.ExpectedAddonBuild, true)
		prev, ok := stopEntryBootLogged.Load(at.id)
		changed := !ok || prev.(string) != line
		if changed {
			stopEntryBootLogged.Store(at.id, line)
		}
		return line, changed
	}

	unproven, first := emit("")
	if !first {
		t.Fatal("the first cycle must emit")
	}
	if !strings.Contains(unproven, "build_id=none") || strings.Contains(unproven, "slots=stop_price") {
		t.Fatalf("with no frame received the line must say so: %s", unproven)
	}
	if _, again := emit(""); again {
		t.Error("an unchanged posture must not re-emit every cycle")
	}
	arrived, changed := emit(ntwire.ExpectedAddonBuild)
	if !changed {
		t.Fatal("the none→proven transition was swallowed — F2's acceptance line is unobtainable without a restart")
	}
	if !strings.Contains(arrived, "slots=stop_price") || !strings.Contains(arrived, "match=yes") {
		t.Fatalf("the proven line must state the proof: %s", arrived)
	}
	if _, again := emit(ntwire.ExpectedAddonBuild); again {
		t.Error("the proven posture must settle to one line")
	}

	// The dedupe key must be the RENDERED LINE, which is what makes the
	// transition visible; a bare "have I emitted" flag cannot express it.
	if v, ok := stopEntryBootLogged.Load(at.id); !ok || v.(string) != arrived {
		t.Errorf("the latch must hold the last rendered line, got %v", v)
	}
}

// TestStopEntryBootLineHasAProductionCallSite — A29 for the emitter itself.
func TestStopEntryBootLineHasAProductionCallSite(t *testing.T) {
	b, err := os.ReadFile("armed_executor.go")
	if err != nil {
		t.Fatalf("cannot read the placement source: %v", err)
	}
	src := string(b)
	if !strings.Contains(src, "at.logStopEntryBootLine()") {
		t.Error("the D4 boot line is never emitted from the armed cycle")
	}
	if strings.Contains(src, "stopEntryBootLogged.LoadOrStore(") {
		t.Error("the boot line latches on HAVING EMITTED again — an unknown build would pin match=NO for the life of the process")
	}
}

// TestSystemMapStopEntryRefsResolve — class 75 (SYSTEM-MAP CONTRACT) has no
// enforcing test, and the first cut of this wave shipped six wrong line
// references INSIDE the sentence whose stated purpose was to correct two stale
// ones. Nothing caught it because nothing reads the map.
//
// This does, for the region this wave owns: every `symbol` :NNN pair between the
// MAPCHECK markers must name a line of trader/armed_executor.go that actually
// mentions that symbol. It reads the numbers OUT of the map, so the map stays
// the single source and no third copy exists to drift.
func TestSystemMapStopEntryRefsResolve(t *testing.T) {
	mb, err := os.ReadFile("../docs/superpowers/SYSTEM-MAP.md")
	if err != nil {
		t.Fatalf("cannot read SYSTEM-MAP: %v", err)
	}
	mapSrc := string(mb)
	const open, close = "<!-- MAPCHECK:trader/armed_executor.go", "<!-- /MAPCHECK -->"
	i := strings.Index(mapSrc, open)
	j := strings.Index(mapSrc, close)
	if i < 0 || j <= i {
		t.Fatal("the MAPCHECK region is gone — the map section this wave owns is no longer checked")
	}
	region := mapSrc[i:j]

	cb, err := os.ReadFile("armed_executor.go")
	if err != nil {
		t.Fatalf("cannot read armed_executor.go: %v", err)
	}
	lines := strings.Split(string(cb), "\n")

	re := regexp.MustCompile("`([A-Za-z_][A-Za-z0-9_]*)`[^`\n]{0,60}?:(\\d+)")
	ms := re.FindAllStringSubmatch(region, -1)
	if len(ms) < 6 {
		t.Fatalf("the MAPCHECK region names only %d symbol:line pairs — it has been gutted", len(ms))
	}
	checked := 0
	for _, m := range ms {
		sym, numStr := m[1], m[2]
		n, cerr := strconv.Atoi(numStr)
		if cerr != nil || n <= 0 || n > len(lines) {
			t.Errorf("%s :%s — armed_executor.go has %d lines", sym, numStr, len(lines))
			continue
		}
		// Only symbols that exist in this file are ours to check; the region
		// also cites store/ and C# coordinates, which are out of its reach.
		if !strings.Contains(string(cb), sym) {
			continue
		}
		checked++
		if !strings.Contains(lines[n-1], sym) {
			t.Errorf("SYSTEM-MAP says %s is at armed_executor.go:%d, but that line reads:\n\t%s", sym, n, strings.TrimSpace(lines[n-1]))
		}
	}
	if checked < 5 {
		t.Errorf("only %d of the region's references were checkable — the pin is going vacuous", checked)
	}

	// Range refs (`symbol` :NNN-MMM) name a branch's SPAN. The symbol must sit
	// on the range's start line and both endpoints must be real lines. Without
	// this the stop-branch range ref drifted silently — a colonless bare range
	// is invisible to the pair regex (class 75, second strike).
	rangeRe := regexp.MustCompile("`([A-Za-z_][A-Za-z0-9_]*)`[^`\n]{0,60}?:(\\d+)-(\\d+)")
	for _, m := range rangeRe.FindAllStringSubmatch(region, -1) {
		sym, s1, s2 := m[1], m[2], m[3]
		n1, err1 := strconv.Atoi(s1)
		n2, err2 := strconv.Atoi(s2)
		if err1 != nil || err2 != nil || n1 <= 0 || n2 <= n1 || n2 > len(lines) {
			t.Errorf("%s :%s-%s — armed_executor.go has %d lines", sym, s1, s2, len(lines))
			continue
		}
		if !strings.Contains(string(cb), sym) {
			continue // not a symbol of this file
		}
		checked++
		if !strings.Contains(lines[n1-1], sym) {
			t.Errorf("SYSTEM-MAP says %s's branch starts at armed_executor.go:%d, but that line reads:\n\t%s", sym, n1, strings.TrimSpace(lines[n1-1]))
		}
	}
}

// TestStopEntryBootLineStatesTheSeam — D4 / A11, owner ruling 2026-09-05.
// The seam is the FIRST thing the line reports because it is the only field
// that decides whether any of the others can matter: with the seam off the
// placement branch returns at :935, before the guard, the build floor or the
// wire. A reader who sees "guard=stop-side" and stops must not conclude the
// binary is placing stop entries. This pin fails if the field is ever written
// as a literal (both renderings are asserted from ONE function) or if the OFF
// rendering stops saying that nothing is placed.
func TestStopEntryBootLineStatesTheSeam(t *testing.T) {
	off := StopEntryBootLine(ntwire.MinAddonBuildStopSlot, ntwire.ExpectedAddonBuild, false)
	for _, want := range []string{"seam=OFF", "NO stop entry is placed", "owner ruling 2026-09-05"} {
		if !strings.Contains(off, want) {
			t.Fatalf("seam-off boot line must contain %q, got:\n%s", want, off)
		}
	}
	// The seam leads: nothing may precede it but the emoji and the label.
	if !strings.HasPrefix(off, "\U0001F3AF stop-entry: seam=") {
		t.Fatalf("the seam must be the FIRST field, got:\n%s", off)
	}
	on := StopEntryBootLine(ntwire.MinAddonBuildStopSlot, ntwire.ExpectedAddonBuild, true)
	if !strings.Contains(on, "seam=on") {
		t.Fatalf("seam-on boot line must read seam=on, got:\n%s", on)
	}
	if strings.Contains(on, "NO stop entry is placed") {
		t.Fatalf("seam-on must NOT claim nothing is placed, got:\n%s", on)
	}
	// A11: the field is RESOLVED, so the two renderings must differ.
	if off == on {
		t.Fatal("seam on and off render identically — the field is a literal, not a read")
	}
}
