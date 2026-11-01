package mentor

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"vl/market"
)

// TestM462PastEntriesAudit (opt-in, M462_AUDIT_DIR=<dir>) answers: which PAST
// filled mentor entries would the m462 all-session 4h/1h gate have refused?
// For every filled mentor arm it SEEDS a fresh evaluator from the stored 1m
// tape up to the arm's author minute (the boot-time seed replays every 1m bar
// through the live intrabar break rule — a restart lands on the line a bot
// that never stopped holds), reads the 4h/1h state, and runs the ONE shared
// check HTFEntryCheck under the DEFAULT config. SWING4H is exempt (D5.2-R7).
// Where the live arm carries mentor_snapshot_json (rows from 10-22 on) the
// reconstructed 4h/1h directions and verdict are compared with what the live
// bot recorded, so the reconstruction is checked against production.
//
// The dir holds (exported READ-ONLY from data/data.db):
//   - bars1m.json.gz  [[open_ms,o,h,l,c],…]  MNQ 12-26 1m
//   - arms.json       [{arm,pos,setup,side,created_ms,pnl,trader,snap_*},…]
func TestM462PastEntriesAudit(t *testing.T) {
	dir := os.Getenv("M462_AUDIT_DIR")
	if dir == "" {
		t.Skip("M462_AUDIT_DIR=<dir> not set (the past-entries audit is opt-in)")
	}
	f, err := os.Open(filepath.Join(dir, "bars1m.json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var rows [][5]float64
	if err := json.NewDecoder(gz).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	bars := make([]market.Kline, 0, len(rows))
	for _, r := range rows {
		ot := int64(r[0])
		bars = append(bars, market.Kline{OpenTime: ot, CloseTime: ot + 59_999, Open: r[1], High: r[2], Low: r[3], Close: r[4], Final: true})
	}
	raw, err := os.ReadFile(filepath.Join(dir, "arms.json"))
	if err != nil {
		t.Fatal(err)
	}
	type arm struct {
		Arm, Pos     int
		Setup, Side  string
		CreatedMs    int64 `json:"created_ms"`
		PNL          *float64
		Trader       string
		SnapVerdict  *string `json:"snap_verdict"`
		Snap4, Snap1 *string
		HasSnap      bool `json:"has_snap"`
	}
	var arms []arm
	if err := json.Unmarshal(raw, &arms); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Enabled = true
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	verdictOf := func(h HTF) string { // arm_snapshot.go's definition
		if ok, d := HTFAgreement(h); ok {
			return "agree_" + string(d)
		}
		if h.FourH.Dir != "" && !oneHSilent(h) && h.OneH.Dir != "" {
			return "conflict"
		}
		return ""
	}
	var refused []string
	var refusedPNL, keptPNL float64
	var nIntraday, nSwing, nSnap, nSnapMatch int
	byKind := map[string]int{}
	fmt.Printf("\n=== m462 PAST MENTOR ENTRIES (default all-session gate) ===\n")
	fmt.Printf("%-4s %-4s %-8s %-5s %-17s %-9s %-26s %-28s %s\n", "arm", "pos", "setup", "side", "author CT", "pnl", "4h / 1h (rebuilt)", "live snapshot", "m462")
	for _, a := range arms {
		now := a.CreatedMs - a.CreatedMs%60_000 // the closed-bar instant the arm was authored on
		n := sort.Search(len(bars), func(i int) bool { return bars[i].CloseTime >= now })
		e := New(cfg)
		Seed(e, bars[:n], now)
		h := e.State.HTF
		h.GateOff = !HTFGateActive(now, cfg)
		side := SideLong
		if a.Side == "short" {
			side = SideShort
		}
		pnl := 0.0
		if a.PNL != nil {
			pnl = *a.PNL
		}
		ct := time.UnixMilli(a.CreatedMs).In(ctime()).Format("01-02 15:04:05")
		rebuilt := fmt.Sprintf("%s@%.2f / %s@%.2f %s", h.FourH.Dir, h.FourH.Price, h.OneH.Dir, h.OneH.Price, verdictOf(h))
		snap := "-"
		if a.HasSnap {
			nSnap++
			snap = fmt.Sprintf("%s / %s %s", str(a.Snap4), str(a.Snap1), str(a.SnapVerdict))
			if str(a.Snap4) == string(h.FourH.Dir) && str(a.Snap1) == string(h.OneH.Dir) && str(a.SnapVerdict) == verdictOf(h) {
				nSnapMatch++
				snap += " =="
			} else {
				snap += " !="
			}
		}
		verdict := "trade"
		if a.Setup == "SWING4H" {
			nSwing++
			verdict = "exempt (SWING4H)"
		} else {
			nIntraday++
			if r, _ := HTFEntryCheck(h, side); r != "" {
				verdict = "REFUSED " + htfRefusalName(a.Setup, r)
				byKind[string(r)]++
				refused = append(refused, fmt.Sprintf("arm %d/pos %d", a.Arm, a.Pos))
				refusedPNL += pnl
			} else {
				keptPNL += pnl
			}
		}
		fmt.Printf("%-4d %-4d %-8s %-5s %-17s %-9.2f %-26s %-28s %s\n", a.Arm, a.Pos, a.Setup, a.Side, ct, pnl, rebuilt, snap, verdict)
	}
	fmt.Printf("\nintraday entries %d (SWING4H exempt %d); refused %d %v; pnl_corrected refused %.2f, kept %.2f\n", nIntraday, nSwing, len(refused), byKind, refusedPNL, keptPNL)
	fmt.Printf("refused: %s\n", strings.Join(refused, ", "))
	fmt.Printf("live-snapshot cross-check: %d/%d rows rebuilt to the same 4h/1h dirs + verdict\n", nSnapMatch, nSnap)
}
