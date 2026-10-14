package kernel

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"vl/market"
)

// Deliberately a small complete grammar, not a price mined from prose. Compound,
// sequential ("back below", "after"), MSS and subjective rules stay UNKNOWN.
var authoredCloseRule = regexp.MustCompile(`(?i)^(?:(?:a|one) )?(?:(1|2)x)?5m(?:_close| closes?)?\s*(above|below|>|<)\s*(\d+(?:\.\d+)?)(?:\s+\((?:SWG-[HL]·(?:1|5|15|30)m|OR-[HL]|ON[HL]|PD[HL]|VWAP[+−-][12]σ|POC)\))?(?:\s+(?:invalidates|kills|cancels|voids|negates|aborts) (?:the |this )?(?:setup|short|long|rejection|fade|hold|hold thesis|breakout|reclaim))?\.?$`)

type AuthoredInvalidationVerdict struct {
	ScenarioID  string
	Known       bool
	Invalidated bool
	// Unknown (W-EXEC-TRUTH W2 A1, lane decision D1) says WHICH unknown a
	// !Known verdict is: AuthoredUnknownGrammar (the sentence is outside the
	// grammar — a WRITE-TIME REFUSAL) or AuthoredUnknownTape (the minutes the
	// rule needs are missing/malformed/duplicated — accepted and counted, a
	// missing minute is never the model's fault). "" when Known.
	Unknown string
	Anchor  float64
	Price   float64
	At      time.Time
	Reason  string
	// Groups (W2 A2) — the open_ms of every 5m group whose close boundary the
	// check judged. Set by EvaluateAuthoredInvalidationBetween only.
	Groups []int64
}

// The two kinds of UNKNOWN (W2 A1 / D1). Grammar is the author's defect and is
// refused; tape is the feed's and is accepted with a recorded count.
const (
	AuthoredUnknownGrammar = "grammar"
	AuthoredUnknownTape    = "tape"
)

// EvaluateAuthoredInvalidationAt tests only the most recently completed rule
// window at authoring. Every constituent minute is required; incomplete,
// missing, duplicate or non-finite tape never becomes a refusal. This does NOT
// alter EvaluateScenario, confirmation timing, or any EntryGate verdict.
func EvaluateAuthoredInvalidationAt(sc PlanScenario, bars []market.Kline, now time.Time) AuthoredInvalidationVerdict {
	out := AuthoredInvalidationVerdict{ScenarioID: sc.ID, Reason: "UNKNOWN: authored invalidation is outside the supported explicit 1/2 × 5m close grammar"}
	m := authoredCloseRule.FindStringSubmatch(strings.TrimSpace(sc.Invalid))
	if m == nil {
		out.Unknown = AuthoredUnknownGrammar
		return out
	}
	ref, err := strconv.ParseFloat(m[3], 64)
	if err != nil || ref <= 0 || math.IsInf(ref, 0) || math.IsNaN(ref) {
		out.Unknown = AuthoredUnknownGrammar
		return out
	}
	need := 1
	if m[1] == "2" {
		need = 2
	}
	end := now.UnixMilli() / 300000 * 300000
	start := end - int64(need)*300000
	minutes := make(map[int64]float64, need*5)
	for _, b := range bars {
		if b.OpenTime < start || b.OpenTime >= end {
			continue
		}
		if b.OpenTime%60000 != 0 || b.CloseTime < b.OpenTime+59999 || b.CloseTime > b.OpenTime+60000 || b.CloseTime > now.UnixMilli() || b.Close <= 0 || math.IsNaN(b.Close) || math.IsInf(b.Close, 0) {
			out.Reason = "UNKNOWN: malformed or incomplete minute tape"
			out.Unknown = AuthoredUnknownTape
			return out
		}
		if _, exists := minutes[b.OpenTime]; exists {
			out.Reason = "UNKNOWN: duplicate minute tape"
			out.Unknown = AuthoredUnknownTape
			return out
		}
		minutes[b.OpenTime] = b.Close
	}
	for ms := start; ms < end; ms += 60000 {
		if _, ok := minutes[ms]; !ok {
			out.Reason = "UNKNOWN: missing completed minute tape for authored close window"
			out.Unknown = AuthoredUnknownTape
			return out
		}
	}
	out.Known = true
	out.Anchor = ref
	out.At = time.UnixMilli(end)
	out.Price = minutes[end-60000]
	out.Invalidated = true
	above := m[2] == "above" || m[2] == ">"
	for boundary := start + 300000; boundary <= end; boundary += 300000 {
		close := minutes[boundary-60000]
		if (above && close <= ref) || (!above && close >= ref) {
			out.Invalidated = false
		}
	}
	out.Reason = fmt.Sprintf("authored condition %q: %d completed 5m close(s), last %.2f at %s, threshold %.2f, invalidated=%t", sc.Invalid, need, out.Price, FormatCT(out.At), ref, out.Invalidated)
	return out
}

// AuthoredInvalidationLine (release #25 PK-4) returns the price the scenario's
// authored invalidation sentence names, when the sentence parses under the ONE
// grammar above (authoredCloseRule) — never a price mined from free prose.
// ok=false when the sentence is outside the grammar.
func AuthoredInvalidationLine(sc PlanScenario) (float64, bool) {
	m := authoredCloseRule.FindStringSubmatch(strings.TrimSpace(sc.Invalid))
	if m == nil {
		return 0, false
	}
	ref, err := strconv.ParseFloat(m[3], 64)
	if err != nil || ref <= 0 || math.IsInf(ref, 0) || math.IsNaN(ref) {
		return 0, false
	}
	return ref, true
}

// ── W-EXEC-TRUTH W2 A1 + A2 (2026-09-23) — structured invalidation or refuse;
// publication-time re-validation ─────────────────────────────────────────────
//
// Row 455 (2026-09-23 LONDON v1) is the evidence. Its four scenario.invalid
// sentences were all outside the grammar above, so every one read UNKNOWN and
// was ACCEPTED (log_events 92916–92919). The read clock was 01:30:27 CT and the
// publish clock 01:51:47 CT: four 5m groups closed in between (01:35 31081.00,
// 01:40 31090.75, 01:45 31083.00, 01:50 31079.75) and the check looked only at
// the last one. The plan's own death{2x5m above 31075.75} was met by the first
// two of them before the plan existed.
//
// Both halves are WRITE-TIME refusals inside the existing attempt budget. The
// runtime lifecycle (buffers, windows, flip hold, touch gate) is untouched.

// AuthoredInvalidationPolicy is the ONE statement of what the write site does
// with scenario.invalid. The 🧭 boot line and the born_check record on every
// plan row READ it; nothing else spells it.
func AuthoredInvalidationPolicy() string { return "enforced (grammar)" }

// AuthoredGrammarRefusalMarker opens the combined grammar refusal. The repair
// router (planner_repair.go lawExcerptsFor) keys on it, so it must not drift.
const AuthoredGrammarRefusalMarker = "invalidation grammar refusal"

// AuthoredInvalidationGrammarForms is the canonical grammar the prompt states
// and the refusal repeats. Every form parses under authoredCloseRule (the
// parity test substitutes a number into each and runs it).
func AuthoredInvalidationGrammarForms() []string {
	return []string{"5m close above <price>", "5m close below <price>", "2x5m close above <price>", "2x5m close below <price>"}
}

// authoredGrammarExample is the ONE example the prompt carries. <price> is a
// PLACEHOLDER, never a real number, so the model cannot copy a stale level.
const authoredGrammarExample = "2x5m close below <price>"

// AuthoredInvalidationGrammarLine is the planner-prompt statement of the
// grammar (rendered under the scenarios schema line). The class-38 registry
// asserts its fragments; the parity test extracts its example.
func AuthoredInvalidationGrammarLine() string {
	forms := AuthoredInvalidationGrammarForms()
	q := make([]string, len(forms))
	for i, f := range forms {
		q[i] = `"` + f + `"`
	}
	return `  // "invalid" GRAMMAR (machine-checked at write; anything else is REFUSED, never accepted as UNKNOWN): EXACTLY one of ` +
		strings.Join(q, " | ") +
		` — <price> is ONE plain number (no commas), nothing before or after it: no "any", "back", "then", no second clause, no other timeframe, no MSS. Example: "invalid": "` + authoredGrammarExample + `"`
}

// AuthoredBornGroups (W2 A2) is the set of 5m groups the born check judges: every
// group whose close boundary B satisfies read < B ≤ publish, plus the latest
// completed group at publish (so the check never judges LESS than the
// latest-window check it replaces — a read and publish inside one bucket judge
// exactly that window). read.IsZero() (legacy facts-less callers) → the latest
// completed group only. Closure is the canonical EvaluateBucketClose predicate.
func AuthoredBornGroups(read, publish time.Time) []BucketClose {
	if publish.IsZero() {
		return nil
	}
	pub := publish.UnixMilli()
	last := EvaluateBucketClose(pub, 5, pub).OpenMs - 300_000
	first := last
	if !read.IsZero() {
		if o := EvaluateBucketClose(read.UnixMilli(), 5, pub).OpenMs; o < first {
			first = o
		}
	}
	var out []BucketClose
	for o := first; o <= last; o += 300_000 {
		if b := EvaluateBucketClose(o, 5, pub); b.Closed {
			out = append(out, b)
		}
	}
	return out
}

// bornTape is the minute tape the born check reads: one close per complete 5m
// group, and the set of groups whose minutes are missing, malformed or
// duplicated (tape-UNKNOWN for that group only — never a refusal).
type bornTape struct {
	close map[int64]float64 // group open_ms → close of its last minute
	known map[int64]bool    // group open_ms → all 5 minutes present and sound
}

// readBornTape keeps the all-5-minutes rule of EvaluateAuthoredInvalidationAt,
// per group: a group is known only when each of its five minutes is present,
// minute-aligned, closed by publish, finite, positive and unique.
func readBornTape(bars []market.Kline, fromOpen, toOpen, publishMs int64) bornTape {
	t := bornTape{close: map[int64]float64{}, known: map[int64]bool{}}
	minutes := map[int64]float64{}
	bad := map[int64]bool{} // group open → a malformed/duplicate minute landed in it
	end := toOpen + 300_000
	for _, b := range bars {
		if b.OpenTime < fromOpen || b.OpenTime >= end {
			continue
		}
		g := b.OpenTime - b.OpenTime%300_000
		if b.OpenTime%60000 != 0 || b.CloseTime < b.OpenTime+59999 || b.CloseTime > b.OpenTime+60000 || b.CloseTime > publishMs || b.Close <= 0 || math.IsNaN(b.Close) || math.IsInf(b.Close, 0) {
			bad[g] = true
			continue
		}
		if _, dup := minutes[b.OpenTime]; dup {
			bad[g] = true
			continue
		}
		minutes[b.OpenTime] = b.Close
	}
	for g := fromOpen; g <= toOpen; g += 300_000 {
		ok := !bad[g]
		for ms := g; ok && ms < g+300_000; ms += 60_000 {
			_, ok = minutes[ms]
		}
		t.known[g] = ok
		if ok {
			t.close[g] = minutes[g+240_000]
		}
	}
	return t
}

// bornBreach is the first group set that satisfied a rule.
type bornBreach struct {
	closes []float64
	opens  []int64
	at     int64 // close boundary of the last group in the breach
}

// judgeBorn applies ONE rule (need consecutive 5m closes strictly beyond ref)
// to every group in the set. A 2x rule pairs each group with the one before it;
// that earlier group may close at or before the read clock. Returns the first
// breach, and whether any evaluation hit tape-UNKNOWN without a breach.
func judgeBorn(t bornTape, groups []BucketClose, need int, above bool, ref float64) (*bornBreach, bool) {
	unknown := false
	for _, g := range groups {
		var br bornBreach
		sound, beyond := true, true
		for k := need - 1; k >= 0; k-- {
			o := g.OpenMs - int64(k)*300_000
			if !t.known[o] {
				sound = false
				break
			}
			c := t.close[o]
			br.closes = append(br.closes, c)
			br.opens = append(br.opens, o)
			if (above && c <= ref) || (!above && c >= ref) {
				beyond = false
			}
		}
		if !sound {
			unknown = true
			continue
		}
		if beyond {
			br.at = g.CloseMs
			return &br, false
		}
	}
	return nil, unknown
}

func (b *bornBreach) String() string {
	parts := make([]string, len(b.closes))
	for i, c := range b.closes {
		parts[i] = fmt.Sprintf("%.2f (%s)", c, ClockCT(time.UnixMilli(b.opens[i]+300_000)))
	}
	return strings.Join(parts, ", ")
}

func bornWindowText(read, publish time.Time) string {
	r := "n/a (read clock unknown — latest completed window only)"
	if !read.IsZero() {
		r = FormatCT(read)
	}
	return fmt.Sprintf("between read %s and publish %s", r, FormatCT(publish))
}

// EvaluateAuthoredInvalidationBetween (W2 A2) judges the authored rule against
// EVERY group in AuthoredBornGroups(read, publish). read.IsZero() is the legacy
// latest-window check, byte-identical (EvaluateAuthoredInvalidationAt). Any
// breach on complete tape is Known+Invalidated; no breach with a tape gap in
// some group is tape-UNKNOWN (accepted), never a refusal.
func EvaluateAuthoredInvalidationBetween(sc PlanScenario, bars []market.Kline, read, publish time.Time) AuthoredInvalidationVerdict {
	groups := AuthoredBornGroups(read, publish)
	ids := make([]int64, len(groups))
	for i, g := range groups {
		ids[i] = g.OpenMs
	}
	if read.IsZero() {
		v := EvaluateAuthoredInvalidationAt(sc, bars, publish)
		v.Groups = ids
		return v
	}
	out := AuthoredInvalidationVerdict{ScenarioID: sc.ID, Groups: ids, Unknown: AuthoredUnknownGrammar, Reason: "UNKNOWN: authored invalidation is outside the supported explicit 1/2 × 5m close grammar"}
	m := authoredCloseRule.FindStringSubmatch(strings.TrimSpace(sc.Invalid))
	if m == nil {
		return out
	}
	ref, err := strconv.ParseFloat(m[3], 64)
	if err != nil || ref <= 0 || math.IsInf(ref, 0) || math.IsNaN(ref) {
		return out
	}
	need := 1
	if m[1] == "2" {
		need = 2
	}
	above := m[2] == "above" || m[2] == ">"
	out.Anchor = ref
	if len(groups) == 0 {
		out.Unknown, out.Reason = AuthoredUnknownTape, "UNKNOWN: no completed 5m group at publish"
		return out
	}
	t := readBornTape(bars, groups[0].OpenMs-int64(need-1)*300_000, groups[len(groups)-1].OpenMs, publish.UnixMilli())
	breach, unknown := judgeBorn(t, groups, need, above, ref)
	switch {
	case breach != nil:
		out.Known, out.Invalidated, out.Unknown = true, true, ""
		out.Price, out.At = breach.closes[len(breach.closes)-1], time.UnixMilli(breach.at)
		out.Reason = fmt.Sprintf("authored condition %q breached %s: %d× 5m close(s) %s beyond threshold %.2f (%d group(s) judged)", sc.Invalid, bornWindowText(read, publish), need, breach, ref, len(groups))
	case unknown:
		out.Unknown, out.Reason = AuthoredUnknownTape, "UNKNOWN: missing completed minute tape for authored close window"
	default:
		lastG := groups[len(groups)-1]
		out.Known, out.Unknown = true, ""
		out.Price, out.At = t.close[lastG.OpenMs], time.UnixMilli(lastG.CloseMs)
		out.Reason = fmt.Sprintf("authored condition %q: no breach %s over %d completed 5m group(s), last %.2f at %s, threshold %.2f, invalidated=false", sc.Invalid, bornWindowText(read, publish), len(groups), out.Price, FormatCT(out.At), ref)
	}
	return out
}

// PlanConditionBornVerdict is a death{}/flip{} line judged on the born-check
// group set (W2 D5).
type PlanConditionBornVerdict struct {
	Judged bool   // false: no line, no read clock, or a rule outside 2x5m|5m_close
	Met    bool   // the line's rule was satisfied between read and publish
	Tape   bool   // not met and at least one evaluation lacked complete tape
	Reason string // names the line, the closes and the window
}

// EvaluatePlanConditionBetween (W2 D5) judges a structured death/flip line on
// the SAME groups as the scenario check: the raw line, its rule (5m_close = one
// close, 2x5m = two consecutive) and its side. It is a write-time question —
// did the line fire between read and publish — so it needs a read clock; the
// runtime evaluator's buffer and touch gate are not involved and not changed.
func EvaluatePlanConditionBetween(kind string, c *PlanCondition, bars []market.Kline, read, publish time.Time) PlanConditionBornVerdict {
	if c == nil || c.Price <= 0 {
		return PlanConditionBornVerdict{Reason: kind + ": no structured line"}
	}
	head := fmt.Sprintf("%s{%s %.2f %s", kind, c.Side, c.Price, c.Rule)
	if to := strings.TrimSpace(c.FlipTo); to != "" {
		head += " → " + to
	}
	head += "}"
	if read.IsZero() {
		return PlanConditionBornVerdict{Reason: head + ": not judged (read clock unknown)"}
	}
	need := 0
	switch c.Rule {
	case "5m_close":
		need = 1
	case "2x5m":
		need = 2
	}
	if need == 0 || (c.Side != "above" && c.Side != "below") {
		return PlanConditionBornVerdict{Reason: head + ": not judged (rule/side outside 2x5m|5m_close above|below)"}
	}
	groups := AuthoredBornGroups(read, publish)
	if len(groups) == 0 {
		return PlanConditionBornVerdict{Judged: true, Tape: true, Reason: head + ": UNKNOWN (no completed 5m group at publish)"}
	}
	t := readBornTape(bars, groups[0].OpenMs-int64(need-1)*300_000, groups[len(groups)-1].OpenMs, publish.UnixMilli())
	breach, unknown := judgeBorn(t, groups, need, c.Side == "above", c.Price)
	if breach != nil {
		return PlanConditionBornVerdict{Judged: true, Met: true, Reason: fmt.Sprintf("%s met %s: 5m close(s) %s", head, bornWindowText(read, publish), breach)}
	}
	if unknown {
		return PlanConditionBornVerdict{Judged: true, Tape: true, Reason: head + ": UNKNOWN (missing completed minute tape) — not met on the complete groups"}
	}
	return PlanConditionBornVerdict{Judged: true, Reason: fmt.Sprintf("%s not met %s (%d group(s))", head, bornWindowText(read, publish), len(groups))}
}

// BornVerdict is one subject's outcome in the born_check record.
type BornVerdict struct {
	Subject string `json:"subject"` // S1..Sn | death | flip
	Outcome string `json:"outcome"` // alive | invalidated | grammar_refusal | tape_unknown | met | not_met | not_judged
	Reason  string `json:"reason"`
}

// BornCheck is the record stored on the plan row (born_check) and in a refused
// attempt's liveness-event detail: what was judged, on which groups, against
// which clocks. ReadClockMs nil = the read clock was unknown (legacy path).
type BornCheck struct {
	Policy         string        `json:"policy"`
	ReadClockMs    *int64        `json:"read_clock_ms"`
	PublishClockMs int64         `json:"publish_clock_ms"`
	Groups         []int64       `json:"groups"`
	Verdicts       []BornVerdict `json:"verdicts"`
}

// JSON renders the record; "" only if marshalling fails (never a fake record).
func (c BornCheck) JSON() string {
	b, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return string(b)
}

// BornCheckResult is the pure outcome of one attempt's born check. The trader
// wraps it with logging and liveness events; the shadow A/B reads Err() only.
type BornCheckResult struct {
	Check       BornCheck
	Grammar     []PlanScenario // scenarios whose invalid is outside the grammar (REFUSED)
	TapeUnknown []AuthoredInvalidationVerdict
	Dead        []string // "S1: <reason>" — scenarios invalidated between read and publish
	Death       *PlanConditionBornVerdict
	Flip        *PlanConditionBornVerdict
}

// EvaluateBornCheck runs A1 + A2 + D5 over one candidate doc. Pure.
func EvaluateBornCheck(doc *PlanDoc, bars []market.Kline, read, publish time.Time) BornCheckResult {
	var r BornCheckResult
	r.Check.Policy = AuthoredInvalidationPolicy()
	r.Check.PublishClockMs = publish.UnixMilli()
	if !read.IsZero() {
		ms := read.UnixMilli()
		r.Check.ReadClockMs = &ms
	}
	r.Check.Groups = []int64{}
	for _, g := range AuthoredBornGroups(read, publish) {
		r.Check.Groups = append(r.Check.Groups, g.OpenMs)
	}
	r.Check.Verdicts = []BornVerdict{}
	if doc == nil {
		return r
	}
	for _, sc := range doc.Scenarios {
		v := EvaluateAuthoredInvalidationBetween(sc, bars, read, publish)
		switch {
		case !v.Known && v.Unknown == AuthoredUnknownGrammar:
			r.Grammar = append(r.Grammar, sc)
			r.Check.Verdicts = append(r.Check.Verdicts, BornVerdict{sc.ID, "grammar_refusal", fmt.Sprintf("%q is outside the invalidation grammar", sc.Invalid)})
		case !v.Known:
			r.TapeUnknown = append(r.TapeUnknown, v)
			r.Check.Verdicts = append(r.Check.Verdicts, BornVerdict{sc.ID, "tape_unknown", v.Reason})
		case v.Invalidated:
			r.Dead = append(r.Dead, sc.ID+": "+v.Reason)
			r.Check.Verdicts = append(r.Check.Verdicts, BornVerdict{sc.ID, "invalidated", v.Reason})
		default:
			r.Check.Verdicts = append(r.Check.Verdicts, BornVerdict{sc.ID, "alive", v.Reason})
		}
	}
	for _, line := range []struct {
		kind string
		c    *PlanCondition
		dst  **PlanConditionBornVerdict
	}{{"death", doc.DeathStructured, &r.Death}, {"flip", doc.FlipStructured, &r.Flip}} {
		v := EvaluatePlanConditionBetween(line.kind, line.c, bars, read, publish)
		outcome := "not_judged"
		switch {
		case v.Met:
			outcome = "met"
			vv := v
			*line.dst = &vv
		case v.Tape:
			outcome = "tape_unknown"
		case v.Judged:
			outcome = "not_met"
		}
		r.Check.Verdicts = append(r.Check.Verdicts, BornVerdict{line.kind, outcome, v.Reason})
	}
	return r
}

// Err is the ONE refusal the candidate receives, or nil. Every defect is named
// in one error so a single repair can fix all of them: the grammar refusal
// first (quoting each offending sentence), then the born-dead scenarios and
// death line, then a flip that already fired.
func (r BornCheckResult) Err() error {
	var parts []string
	if len(r.Grammar) > 0 {
		q := make([]string, len(r.Grammar))
		for i, sc := range r.Grammar {
			q[i] = fmt.Sprintf("%s invalid %q", sc.ID, sc.Invalid)
		}
		parts = append(parts, fmt.Sprintf("%s: %d scenario invalidation line(s) outside the grammar — %s — rewrite EACH as exactly one of: %s (one plain number, nothing else)",
			AuthoredGrammarRefusalMarker, len(r.Grammar), strings.Join(q, "; "), strings.Join(AuthoredInvalidationGrammarForms(), " | ")))
	}
	if r.BornDead() {
		dead := append([]string(nil), r.Dead...)
		if r.Death != nil {
			dead = append(dead, "plan "+r.Death.Reason+" — the plan would be born dead")
		}
		parts = append(parts, "born-dead authored scenario: "+strings.Join(dead, "; "))
	}
	if r.Flip != nil {
		parts = append(parts, "plan "+r.Flip.Reason+" — the flip already fired before publication; re-author on the flipped side")
	}
	if len(parts) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(parts, " · "))
}

// BornDead reports a scenario invalidated or the death line met between read
// and publish (the existing born-dead refusal class).
func (r BornCheckResult) BornDead() bool { return len(r.Dead) > 0 || r.Death != nil }

// ReadClockPtr / PublishClockPtr / JSONPtr are the plan-row column values: a
// nil record (pre-W2 path, fail-closed NO-TRADE row) writes NULL, never 0/"".
func (c *BornCheck) ReadClockPtr() *int64 {
	if c == nil || c.ReadClockMs == nil {
		return nil
	}
	v := *c.ReadClockMs
	return &v
}

func (c *BornCheck) PublishClockPtr() *int64 {
	if c == nil || c.PublishClockMs <= 0 {
		return nil
	}
	v := c.PublishClockMs
	return &v
}

func (c *BornCheck) JSONPtr() *string {
	if c == nil {
		return nil
	}
	s := c.JSON()
	if s == "" {
		return nil
	}
	return &s
}
