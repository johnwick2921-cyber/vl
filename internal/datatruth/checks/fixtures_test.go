package checks

import (
	"encoding/json"
	"os"
	"sort"
	"testing"
	"time"
)

// Real-data fixtures exported READ-ONLY from data/data.db on 2026-10-29 (MNQ
// 12-26 / ES 12-26). Row layout: [t, o, h, l, c, v, rowid, source].

func loadJSON(t *testing.T, name string, v any) {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

type window struct {
	Mnq   [][]any `json:"mnq"`
	Es    [][]any `json:"es"`
	Start int64   `json:"start"`
	End   int64   `json:"end"`
}

func f(x any) float64 { return x.(float64) }

// rowID is the data.db rowid the fixture row came from.
func rowID(r []any) float64 { return f(r[6]) }

func toBars(rows [][]any) []Bar {
	out := make([]Bar, 0, len(rows))
	for _, r := range rows {
		out = append(out, Bar{T: int64(f(r[0])), O: f(r[1]), H: f(r[2]), L: f(r[3]), C: f(r[4]), V: f(r[5])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].T < out[j].T })
	return out
}

func loadWindows(t *testing.T) map[string]window {
	var w map[string]window
	loadJSON(t, "real_1m_windows.json", &w)
	return w
}

// replayBadPrint walks the window minute by minute the way production calls the
// check: at each bar's own minute (forming) and one minute later (closed), with
// only the bars known at that instant. It returns every open time ever flagged
// and, per flagged time, the first/last evaluation instants.
func replayBadPrint(mnq, es []Bar, p BadPrintParams) map[int64][2]int64 {
	out := map[int64][2]int64{}
	for i := range mnq {
		for _, off := range []int64{30_000, 90_000} {
			now := time.UnixMilli(mnq[i].T + off)
			var m, e []Bar
			for _, b := range mnq {
				if b.T <= now.UnixMilli() {
					m = append(m, b)
				}
			}
			for _, b := range es {
				if b.T <= now.UnixMilli() {
					e = append(e, b)
				}
			}
			for _, s := range SuspectBars(m, e, now, p) {
				r, ok := out[s.T]
				if !ok {
					r = [2]int64{now.UnixMilli(), now.UnixMilli()}
				}
				r[1] = now.UnixMilli()
				out[s.T] = r
			}
		}
	}
	return out
}
