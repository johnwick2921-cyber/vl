package ninjatrader

import (
	"encoding/json"
	"math"
	"os"
	"sort"
	"testing"
	"time"

	"vl/internal/datatruth/checks"
	"vl/kernel"
	"vl/market"
)

// The session-volume check (checks/session_volume.go) recomputes the levels the
// kernel emits; this holds the recomputation to the PRODUCTION functions on a
// real session (MNQ 12-26, session of 10-22 17:00 CT, rows in the fixture), so
// a change to kernel.SessionVWAPLevels / PriorDayProfileLevels / the SVP row
// geometry that the check does not follow turns this red instead of silently
// judging a different number than the bot reads.
func loadSessionFixture(t *testing.T, key string) (m1 []checks.Bar, ks []market.Kline, start, end int64) {
	t.Helper()
	b, err := os.ReadFile("../../internal/datatruth/checks/testdata/sessions_vol.json")
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]struct {
		Start int64       `json:"start"`
		End   int64       `json:"end"`
		M1    [][]float64 `json:"m1"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	s := raw[key]
	sort.Slice(s.M1, func(i, j int) bool { return s.M1[i][0] < s.M1[j][0] })
	for _, r := range s.M1 {
		m1 = append(m1, checks.Bar{T: int64(r[0]), O: r[1], H: r[2], L: r[3], C: r[4], V: r[5]})
		ks = append(ks, market.Kline{OpenTime: int64(r[0]), CloseTime: int64(r[0]) + 59_999, Open: r[1], High: r[2], Low: r[3], Close: r[4], Volume: r[5]})
	}
	return m1, ks, s.Start, s.End
}

func TestSessionVolumeMirrorsKernelLevels(t *testing.T) {
	m1, ks, start, end := loadSessionFixture(t, "2026-10-22")
	// the session VWAP and sigma the kernel emits mid-session
	now := time.UnixMilli(start).Add(8 * time.Hour)
	var kv, ksd float64
	for _, l := range kernel.SessionVWAPLevels(ks, now) {
		switch l.Label {
		case "VWAP":
			kv = l.Price
		case "VWAP+1σ":
			ksd = l.Price
		}
	}
	if kv == 0 {
		t.Fatal("kernel emitted no session VWAP on the fixture")
	}
	// the same closed bars: the check judges whole 5m slots one minute after
	// they end, so at now+1min its window ends exactly at the kernel's `now`.
	var cut []checks.Bar
	for _, b := range m1 {
		if b.T >= start && b.T < now.UnixMilli() {
			cut = append(cut, b)
		}
	}
	v := checks.JudgeSessionVolume("x", cut, nil, start, end, now.Add(time.Minute), func(ms int64) bool { return kernel.IsCMEOpen(time.UnixMilli(ms)) }, checks.DefaultSessionVol(), true, false)
	if math.Abs(v.VWAP-kv) > 1e-6 || math.Abs((v.VWAP+v.Sigma)-ksd) > 1e-6 {
		t.Fatalf("check VWAP %.6f / +1s %.6f, kernel %.6f / %.6f", v.VWAP, v.VWAP+v.Sigma, kv, ksd)
	}
	// the prior-day profile
	nowNext := time.UnixMilli(end).Add(2 * time.Hour)
	var kpoc, kvah, kval float64
	for _, l := range kernel.PriorDayProfileLevels(ks, nowNext) {
		switch l.Label {
		case "POC":
			kpoc = l.Price
		case "VAH":
			kvah = l.Price
		case "VAL":
			kval = l.Price
		}
	}
	if kpoc == 0 {
		t.Fatal("kernel emitted no prior-day profile on the fixture")
	}
	var sess []checks.Bar
	for _, b := range m1 {
		if b.T >= start && b.T < end {
			sess = append(sess, b)
		}
	}
	poc, vah, val, ok := checks.ProfileOf(sess)
	if !ok || poc != kpoc || vah != kvah || val != kval {
		t.Fatalf("check profile POC/VAH/VAL %.2f/%.2f/%.2f, kernel %.2f/%.2f/%.2f", poc, vah, val, kpoc, kvah, kval)
	}
}
