package mentor

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// m462 — stored values are sacred. A strategy that STORED
// htf_gate_news_only=true must keep the pre-m462 behaviour exactly. This pin
// replays the four m434 tapes (5,216 ticks, real 10-13 and 10-23 sessions)
// with the knob explicitly ON and compares every tick against the m434 golden
// as it stands WITHOUT m462 (testdata/m462_prem462_tick_identity.golden.gz).
// On the m462 branch that was a gzip of testdata/m434_tick_identity.golden at
// b6389c023 (= dev 7e2231f01). Release #59 merges m463 (swing D5.2) and m464
// (rolling ladder) beside m462, both of which change these tapes on purpose, so
// at the release merge the reference was re-recorded on a tree of dev 7e2231f01
// + m463 83461cd84 + m464 3c9be6955 WITHOUT m462 (M434_WRITE_GOLDEN there,
// gzip -n -9): stored news-only at the release head must equal "release #59
// minus m462" tick for tick. Re-recorded the same way for the release #59 fix
// round (HEAD minus the m462 merge diff 898df8205^1..898df8205, code only — the
// procedure first reproduced the previous reference byte for byte at 3afe97289). Intents, notes,
// chart levels and trendlines must be byte-identical; the State may differ
// ONLY by the m462 refusal-name split (isb/isbrev/box/phl _htf_blocked →
// _htf_no_4h | _htf_conflict, phl's side mismatch → phl_htf_side_mismatch)
// and the new rolling_htf_* counter + its candle mark.
// MUTANT: route the stored-true path through the all-day gate (drop the
// HTFGateNewsOnly read in HTFGateActive) → intents diverge → RED.
const m462PreGoldenPath = "testdata/m462_prem462_tick_identity.golden.gz"

// m462LegacyState maps the m462 refusal ledger back to the pre-m462 names.
func m462LegacyState(st State) State {
	out := map[string]int{}
	for k, v := range st.Refusals {
		switch {
		case strings.HasPrefix(k, "rolling_htf_"):
			continue
		case strings.HasSuffix(k, "_"+string(HTFRefuseNo4h)):
			k = strings.TrimSuffix(k, "_"+string(HTFRefuseNo4h)) + "_htf_blocked"
		case strings.HasSuffix(k, "_"+string(HTFRefuseConflict)):
			k = strings.TrimSuffix(k, "_"+string(HTFRefuseConflict)) + "_htf_blocked"
		case k == "phl_"+string(HTFRefuseSide):
			k = "phl_htf_blocked"
		}
		out[k] += v
	}
	if len(out) == 0 {
		out = nil
	}
	st.Refusals = out
	st.RollingHTFRefusedAt = 0
	// m465 P1 (release #60): the escalation lines and the early-read watermark
	// are new state the pre-m462 golden never carried; the identity runs with
	// the m465 knobs at their pre-#60 values (m462PreM465Knobs).
	st.Trigger15, st.Trigger30, st.TriggerGovTF, st.LastForming15mAt = nil, nil, 0, 0
	return st
}

// m462PreM465Knobs puts the release #60 P1 knobs at their pre-#60 values (no
// early 15m read, mode C from the last closed pairs, no trigger escalation), so
// the stored-news-only identity still compares like with like.
func m462PreM465Knobs(cfg *Config) {
	cfg.Early15mRead = false
	cfg.MTFConfluenceSource = MTFConfluenceLastPair
	cfg.TriggerEscalation = false
}

func TestM462StoredNewsOnlyIsThePreM462Behaviour(t *testing.T) {
	boxPeak1Level = false    // pre-m473 B2 construction: the frozen golden predates peak-1 / level pairing
	firstRTH5mNotISB = false // pre-m473 B4: the frozen golden predates the 08:30-candle-is-not-ISB rule [Oct D4.3 @ 57:20-57:35]
	t.Cleanup(func() { boxPeak1Level = true; firstRTH5mNotISB = true })
	raw, err := os.Open(m462PreGoldenPath)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	gz, err := gzip.NewReader(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{}
	sc := bufio.NewScanner(gz)
	for sc.Scan() {
		ln := sc.Text()
		name := strings.SplitN(ln, " ", 2)[0]
		want[name] = append(want[name], ln)
	}
	for _, c := range m434Cases() {
		c := c
		t.Run(c.name, func(t *testing.T) {
			old := flatNearScale
			flatNearScale = c.scale
			defer func() { flatNearScale = old }()
			// m465 (#60): E6's inside-the-opposing-box rule has no knob (the
			// course is explicit); switch it off here so the pre-#60 golden
			// still proves every other m465 change is undone by its knob.
			nearBoxInsideOpposingOff.Store(true)
			defer nearBoxInsideOpposingOff.Store(false)
			midRangeLegacyRole.Store(true) // m466 G37 (#61, no knob): the pre-#61 b.Kind mid-range read
			defer midRangeLegacyRole.Store(false)
			boxConflictLegacySkip.Store(true) // release #61 review round 2 (no knob): the pre-#61 conflict skip of the box-return walk
			defer boxConflictLegacySkip.Store(false)
			bars := c.tape(t)
			from := c.from
			if from < 0 {
				for i := range bars {
					if bars[i].OpenTime >= r22Open1013 {
						from = i
						break
					}
				}
			}
			cfg := c.cfg()
			cfg.HTFGateNewsOnly = true // the explicitly STORED old value
			// m465 P2 (release #60): the reference predates the ISB loss range
			// box; mentor_isb_loss_box=band is the pre-#60 rule, so this pin
			// also proves band mode reproduces the old State byte for byte.
			cfg.ISBLossBox = ISBLossBoxBand
			m465PreValues(&cfg) // m465 (#60): the pre-#60 knob values — this golden predates them
			m462PreM465Knobs(&cfg)
			m466PreValues(&cfg)   // m466 P3 (#61): the pre-#61 knob values — this golden predates them
			m61PreP4(&cfg)        // m466 P4 (#61): the pre-#61 swing ISB knobs (required, keep OFF) — this golden predates them
			m466P2PreValues(&cfg) // m466 P2 (#61): the pre-#61 knob values — this golden predates them
			m466P1PreValues(&cfg) // m466 P1 (#61): the pre-#61 knob values — this golden predates them
			e := New(cfg)
			w := want[c.name]
			if len(w) != len(bars)-from {
				t.Fatalf("golden rows %d, ticks %d", len(w), len(bars)-from)
			}
			for i := from; i < len(bars); i++ {
				lo := i + 1 - 1500
				if lo < 0 {
					lo = 0
				}
				out := e.Tick(bars[lo:i+1], BarCloseInstant(bars[i]))
				for _, in := range out {
					switch in.Action {
					case ActionMoveStopBE:
						e.SwingBEApplied(in.ArmID)
					case ActionClosePosition:
						e.SwingEnded(in.ArmID)
					}
				}
				ij, _ := json.Marshal(out)
				sj, _ := json.Marshal(m462LegacyState(e.State))
				notes := strings.Join(e.TakeNotes(), "\n")
				wf := strings.Fields(w[i-from])
				if m434Sum(string(ij)) != wf[2] {
					t.Fatalf("tick %d: intents differ from the pre-m462 golden under stored news-only", i)
				}
				if m434Sum(string(sj)) != wf[3] {
					t.Fatalf("tick %d: state differs from the pre-m462 golden beyond the m462 refusal-name split", i)
				}
				if m434Sum(notes) != wf[4] {
					t.Fatalf("tick %d: notes differ from the pre-m462 golden", i)
				}
			}
		})
	}
}
