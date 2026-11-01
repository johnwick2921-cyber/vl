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
// as it stood BEFORE m462 (testdata/m462_prem462_tick_identity.golden.gz = a
// gzip of testdata/m434_tick_identity.golden at b6389c023): intents, notes,
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
	return st
}

func TestM462StoredNewsOnlyIsThePreM462Behaviour(t *testing.T) {
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
