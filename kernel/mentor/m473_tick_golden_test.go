package mentor

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

// m473 — the October boxes (B1 carry, B2 peak-1/level pairs, B3 close5m, B4
// 08:30 candle) change tick output on the four real tapes. The pre-m473
// m434 golden is kept byte for byte (TestM434TickOutputIsByteIdentical turns
// every m473 switch off); THIS golden pins the new behaviour, recorded once at
// the m473 head and explained: which ticks' intents moved and why is in the
// PR body (attribution run per switch against the m434 golden).
//
// Regenerate (only for a deliberate behaviour change):
//
//	M473_WRITE_GOLDEN=1 go test -count=1 -run TestM473TickOutputGolden ./kernel/mentor
const m473GoldenPath = "testdata/m473_tick_identity.golden"

func TestM473TickOutputGolden(t *testing.T) {
	if os.Getenv("M473_WRITE_GOLDEN") != "" {
		var all []string
		for _, c := range m434Cases() {
			all = append(all, m434Digest(t, c)...)
		}
		if err := os.WriteFile(m473GoldenPath, []byte(strings.Join(all, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %d tick digests", len(all))
		return
	}
	f, err := os.Open(m473GoldenPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	want := map[string][]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		name := strings.SplitN(sc.Text(), " ", 2)[0]
		want[name] = append(want[name], sc.Text())
	}
	for _, c := range m434Cases() {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got, w := m434Digest(t, c), want[c.name]
			if len(w) == 0 || len(got) != len(w) {
				t.Fatalf("%s: %d ticks, golden has %d", c.name, len(got), len(w))
			}
			for i := range got {
				if got[i] != w[i] {
					t.Fatalf("%s: first divergence at row %d\n got  %s\n want %s", c.name, i, got[i], w[i])
				}
			}
		})
	}
}

// TestM473Attribution (env M473_ATTR=1, report only): for each m473 switch
// enabled ALONE against the pre-m473 golden, list the ticks whose INTENTS moved.
func TestM473Attribution(t *testing.T) {
	if os.Getenv("M473_ATTR") == "" {
		t.Skip("report only")
	}
	f, err := os.Open(m434GoldenPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	want := map[string][]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		name := strings.SplitN(sc.Text(), " ", 2)[0]
		want[name] = append(want[name], sc.Text())
	}
	run := func(label string, b1, b2, b3, b4 bool) {
		pb, pf, pc, pm := boxPeak1Level, firstRTH5mNotISB, boxCarryRTH, m434PreM473
		boxPeak1Level, firstRTH5mNotISB, boxCarryRTH, m434PreM473 = b2, b4, b1, !b3
		defer func() { boxPeak1Level, firstRTH5mNotISB, boxCarryRTH, m434PreM473 = pb, pf, pc, pm }()
		for _, c := range m434Cases() {
			got, w := m434Digest(t, c), want[c.name]
			var ticks []string
			for i := range got {
				g, wf := strings.Fields(got[i]), strings.Fields(w[i])
				if g[2] != wf[2] {
					ticks = append(ticks, g[1])
				}
			}
			t.Logf("ATTR %s %s: %d intent ticks moved: %s", label, c.name, len(ticks), strings.Join(ticks, ","))
		}
	}
	run("B1-only", true, false, false, false)
	run("B2-only", false, true, false, false)
	run("B3-only", false, false, true, false)
	run("B4-only", false, false, false, true)
	run("ALL", true, true, true, true)
}

// TestM473SwitchesNeedSerialTests — boxPeak1Level, firstRTH5mNotISB and
// boxCarryRTH are package-level test hooks flipped (with t.Cleanup) by the
// frozen-golden pins. They are race-free only while no test in this package
// runs in parallel; this guard fails the day one does.
func TestM473SwitchesNeedSerialTests(t *testing.T) {
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	needle := "t." + "Parallel()"
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), "_test.go") {
			continue
		}
		b, err := os.ReadFile(f.Name())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), needle) {
			t.Fatalf("%s uses %s: the m473 package-level switches would race; move them into Config first", f.Name(), needle)
		}
	}
}
