package mentor

// Release #60 P2 — the loss RANGE box (D4.2-R27 / R28 / R30).
//
// D4.2 p2 @02:41–03:17: "Market này có tổng cộng 3 phần: Trend, Range và
// Trend. Khi anh chị bị thua 1 lệnh, có nghĩa là anh chị đã bị thua ở cái phần
// giữa là cái phần Range... em đợi cho cái điểm entry nằm ở vòng vòng bên
// ngoài"; @03:43–04:07: "tôi bọc cái khu vực này lại. Tôi sẽ không trade bên
// trong khu vực này. Tôi đợi nó đi ra một cái khu vực khác."
//
// Pinned definition (WE-3): Box 1 28,880.50–28,934.00 is exactly the 08:30 5m
// candle (D4.3 Ex.1 H 28,933.75 L 28,880.50). An ISB loss therefore boxes the
// whole 5m range that held the trade — every closed 5m bucket from the fill
// candle's bucket through the stop-out candle's bucket — not only the ISB's
// entry-to-stop band. R28 @03:06–03:31 "tạo thêm cái range nữa thì làm cái
// gì? Đợi thoát ra đánh tiếp": each loss boxes its own range (Box 2 ~28,991–
// 29,047.75). R30 @04:13–05:03 (Winnie, Box 3 29,020.50/28,963.50, 57 pts):
// "cứ đè đây đánh, đè đây đánh" — overlapping losses union into one place and
// two of them turn it off for the day, both sides.

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

// m60K builds a 1m candle opening at hh:mm Chicago time on 2026-10-02 (RTH).
func m60K(hh, mm int, o, h, l, c float64) market.Kline {
	ot := time.Date(2026, 10, 2, hh, mm, 0, 0, ctime()).UnixMilli()
	return market.Kline{Open: o, High: h, Low: l, Close: c, OpenTime: ot, CloseTime: ot + 60_000}
}

// m60Now is the close instant of the hh:mm candle.
func m60Now(hh, mm int) int64 {
	return time.Date(2026, 10, 2, hh, mm, 0, 0, ctime()).UnixMilli() + 60_000
}

func m60ISB(side Side, entry, stop, target float64, expiry int64) Intent {
	return Intent{Action: PlaceStopLimitEntry, Side: side, Price: entry, Stop: stop, Target: target, ExpiryMs: expiry}
}

// m60Cfg: G2 only (the G1 leg budget must not shadow the loss box), the
// shipped ISB loss box default.
func m60Cfg() Config {
	cfg := DefaultConfig()
	cfg.LegBudgetEnabled = false
	return cfg
}

// m60Step runs one closed candle through Limits.Apply.
type m60Run struct {
	l    *Limits
	cfg  Config
	prev market.Kline
}

func (r *m60Run) step(k market.Kline, in ...Intent) []Intent {
	now := k.OpenTime + 60_000
	out := r.l.Apply(in, r.prev, k, now, nil, r.cfg)
	r.prev = k
	return out
}

// box1 replays the WE-3 / D4.3 Ex.1 08:30 5m candle (H 28,933.75 L 28,880.50)
// as five 1m candles with a long ISB 28,905 / stop 28,893 placed at the 08:31
// close, filled 08:32 and stopped 08:34.
func m60Box1(t *testing.T, r *m60Run) {
	t.Helper()
	far := m60Now(15, 0)
	r.prev = m60K(8, 29, 28_900, 28_904, 28_896, 28_900)
	r.step(m60K(8, 30, 28_900, 28_933.75, 28_895, 28_920))
	if out := r.step(m60K(8, 31, 28_920, 28_925, 28_898, 28_900), m60ISB(SideLong, 28_905, 28_893, 28_960, far)); len(out) != 1 {
		t.Fatalf("ISB placement at 08:31: want 1, got %d (refusals=%v)", len(out), r.l.Refusals)
	}
	r.step(m60K(8, 32, 28_900, 28_910, 28_896, 28_903))    // fill (H >= 28,905), no stop
	r.step(m60K(8, 33, 28_903, 28_908, 28_895, 28_897))    // open
	r.step(m60K(8, 34, 28_897, 28_899, 28_880.50, 28_890)) // stop (L <= 28,893) — the loss
	r.step(m60K(8, 35, 28_890, 28_915, 28_888, 28_912))    // inside the range: no departure
}

func m60RangePlace(l *Limits, lo, hi float64) *Place {
	for _, p := range l.Places {
		if p.Lo == lo && p.Hi == hi {
			return p
		}
	}
	return nil
}

// TestM60ISBLossBoxesWhole5mRange — D4.2-R27 + R28.
// MUTANT: drop the range union in loss() (Place Lo/Hi = the band only) → the
// 28,925 entry at 08:36 passes → RED.
func TestM60ISBLossBoxesWhole5mRange(t *testing.T) {
	far := m60Now(15, 0)

	// --- range mode (the default) ---
	r := &m60Run{l: &Limits{}, cfg: m60Cfg()}
	m60Box1(t, r)
	p1 := m60RangePlace(r.l, 28_880.50, 28_933.75)
	if p1 == nil || !p1.Blocked || p1.Losses != 1 {
		t.Fatalf("R27: the ISB loss must box the whole 08:30 5m range 28,880.50–28,933.75, places=%+v", m60Places(r.l))
	}
	// 08:36: an entry at 28,925 (outside the 28,893–28,905 band, inside the
	// range) is refused loss_range_blocked.
	if out := r.step(m60K(8, 36, 28_912, 28_922, 28_905, 28_920), m60ISB(SideLong, 28_925, 28_915, 28_960, far)); len(out) != 0 {
		t.Fatalf("R27: entry 28,925 inside the range: want refused, got %d", len(out))
	}
	if r.l.Refusals["loss_range_blocked"] != 1 {
		t.Fatalf("R27: want loss_range_blocked=1, refusals=%v", r.l.Refusals)
	}
	// 08:37: a 1m candle wholly above 28,933.75 frees the range; an entry at
	// 28,936 (the course's crosshair, outside the range) is allowed.
	if out := r.step(m60K(8, 37, 28_935, 28_942, 28_934.25, 28_940), m60ISB(SideLong, 28_936, 28_926, 28_990, m60Now(8, 38))); len(out) != 1 {
		t.Fatalf("R27: entry 28,936 after a candle fully above the range: want 1, got %d (refusals=%v)", len(out), r.l.Refusals)
	}
	if p1.Blocked {
		t.Fatalf("R27: a 1m candle wholly above the range must free it")
	}
	r.step(m60K(8, 38, 28_935, 28_935.5, 28_930, 28_932)) // the 28,936 order expires untouched

	// --- R28: a new range, its own box (Box 2 shape ~28,991–29,047.75) ---
	r.prev = m60K(8, 49, 29_000, 29_010, 28_995, 29_005)
	r.step(m60K(8, 50, 29_005, 29_047.75, 29_000, 29_015), m60ISB(SideLong, 29_020, 29_005, 29_080, far))
	r.step(m60K(8, 51, 29_015, 29_025, 29_008, 29_018)) // fill, no stop
	r.step(m60K(8, 52, 29_018, 29_022, 29_006, 29_010))
	r.step(m60K(8, 53, 29_010, 29_012, 29_000, 29_004)) // stop (L <= 29,005) — loss 2
	r.step(m60K(8, 54, 29_004, 29_009, 28_991, 29_002)) // the rest of the stop-out's 5m bucket
	p2 := m60RangePlace(r.l, 28_991, 29_047.75)
	if p2 == nil || !p2.Blocked || p2.Losses != 1 || p2.OffDay {
		t.Fatalf("R28: the second loss must box ITS OWN 08:50 5m range 28,991–29,047.75 (1 loss), places=%+v", m60Places(r.l))
	}
	if p1.Blocked || p1.Losses != 1 || p1.OffDay {
		t.Fatalf("R28: Box 1 must stay freed and untouched, got %+v", *p1)
	}
	// Inside Box 2 → refused; inside the freed Box 1 → allowed.
	if out := r.step(m60K(8, 55, 29_002, 29_010, 28_995, 29_006), m60ISB(SideShort, 29_030, 29_040, 28_990, far)); len(out) != 0 {
		t.Fatalf("R28: a short at 29,030 inside Box 2: want refused, got %d", len(out))
	}
	if r.l.Refusals["loss_range_blocked"] != 2 {
		t.Fatalf("R28: want loss_range_blocked=2, refusals=%v", r.l.Refusals)
	}
	if out := r.step(m60K(8, 56, 29_006, 29_010, 28_995, 29_000), m60ISB(SideLong, 28_936, 28_926, 28_990, m60Now(8, 57))); len(out) != 1 {
		t.Fatalf("R28: an entry inside the freed Box 1 must pass, got %d (refusals=%v)", len(out), r.l.Refusals)
	}

	// --- band mode (knob mentor_isb_loss_box=band): the pre-#60 band box ---
	rb := &m60Run{l: &Limits{}, cfg: m60Cfg()}
	rb.cfg.ISBLossBox = ISBLossBoxBand
	m60Box1(t, rb)
	if out := rb.step(m60K(8, 36, 28_912, 28_922, 28_905, 28_920), m60ISB(SideLong, 28_925, 28_915, 28_960, far)); len(out) != 1 {
		t.Fatalf("band mode: entry 28,925 outside the 28,893–28,905 band must pass, got %d (refusals=%v)", len(out), rb.l.Refusals)
	}
	if rb.l.Refusals["loss_range_blocked"] != 0 {
		t.Fatalf("band mode must never count loss_range_blocked, refusals=%v", rb.l.Refusals)
	}
}

// TestM60RepeatedLossesInChopOffDay — D4.2-R30 (Winnie, Box 3 29,020.50 /
// 28,963.50, 09:24–10:05). Two ISB losses whose 5m ranges overlap inside the
// chop union into one place; the second turns it off for the day, and the
// third entry inside — long OR short — is refused loss_box_off_day, even after
// a candle fully outside it.
// MUTANT: drop the overlap union in loss() (each band its own place) → the two
// losses never reach 2 at one place → the third entry passes → RED.
func TestM60RepeatedLossesInChopOffDay(t *testing.T) {
	far := m60Now(15, 0)
	r := &m60Run{l: &Limits{}, cfg: m60Cfg()}

	// Loss 1: the 09:25 5m bucket (H 29,020.50 L 28,975) — a long ISB
	// 29,000 / 28,985 fills 09:27 and stops 09:28.
	r.prev = m60K(9, 24, 28_990, 28_998, 28_985, 28_995)
	r.step(m60K(9, 25, 28_995, 29_020.50, 28_990, 29_005))
	r.step(m60K(9, 26, 29_005, 29_008, 28_992, 28_996), m60ISB(SideLong, 29_000, 28_985, 29_040, far))
	r.step(m60K(9, 27, 28_996, 29_003, 28_990, 28_994)) // fill
	r.step(m60K(9, 28, 28_994, 28_996, 28_980, 28_984)) // stop — loss 1
	r.step(m60K(9, 29, 28_984, 28_990, 28_975, 28_988)) // rest of the bucket
	if p := m60RangePlace(r.l, 28_975, 29_020.50); p == nil || p.Losses != 1 || p.OffDay {
		t.Fatalf("R30 loss 1 must box the 09:25 5m range 28,975–29,020.50, places=%+v", m60Places(r.l))
	}

	// A candle fully BELOW frees it (departure unchanged), then price comes
	// back into the chop.
	r.step(m60K(9, 30, 28_970, 28_972, 28_960, 28_965))
	r.step(m60K(9, 31, 28_965, 28_985, 28_963.50, 28_982))
	// "cứ đè đây đánh" [@04:13–05:03]: back in the chop, a short priced INSIDE
	// loss 1's range (28,978) is refused even though the range was departed —
	// the departure frees entries OUTSIDE it only (R27/R29, m465 review).
	if out := r.step(m60K(9, 32, 28_982, 28_984, 28_976, 28_980), m60ISB(SideShort, 28_978, 28_995, 28_940, far)); len(out) != 0 || r.l.Refusals["loss_range_blocked"] != 1 {
		t.Fatalf("R29: a short at 28,978 inside the departed range must be refused loss_range_blocked, got %d (refusals=%v)", len(out), r.l.Refusals)
	}

	// Loss 2: a SHORT ISB 28,972 / 28,995 (entry just under loss 1's range,
	// so it is allowed) in the 09:35 bucket (H 29,000 L 28,963.50); its 5m
	// range overlaps loss 1's → union, 2 losses → off day.
	r.prev = m60K(9, 34, 28_982, 28_990, 28_978, 28_985)
	r.step(m60K(9, 35, 28_985, 29_000, 28_980, 28_984), m60ISB(SideShort, 28_972, 28_995, 28_940, far))
	r.step(m60K(9, 36, 28_984, 28_986, 28_970, 28_976)) // fill (L <= 28,972)
	r.step(m60K(9, 37, 28_983, 28_996, 28_981, 28_994)) // stop (H >= 28,995) — loss 2
	r.step(m60K(9, 38, 28_994, 28_996, 28_963.50, 28_970))
	r.step(m60K(9, 39, 28_970, 28_990, 28_968, 28_988))
	p := m60RangePlace(r.l, 28_963.50, 29_020.50)
	if p == nil || p.Losses != 2 || !p.OffDay {
		t.Fatalf("R30: two overlapping losses must union into Box 3 28,963.50–29,020.50, off for the day, places=%+v", m60Places(r.l))
	}
	if n := len(r.l.Places); n != 1 {
		t.Fatalf("R30: the overlapping losses must be ONE place, got %d: %+v", n, m60Places(r.l))
	}

	// Even a candle fully outside does not free an off-day place.
	r.step(m60K(9, 40, 29_030, 29_040, 29_025, 29_035))
	// Third entry inside the chop — long, then short — refused off-day.
	if out := r.step(m60K(9, 41, 29_000, 29_010, 28_995, 29_005), m60ISB(SideLong, 29_012, 29_000, 29_050, far)); len(out) != 0 {
		t.Fatalf("R30: a third long inside the chop must be refused, got %d", len(out))
	}
	if out := r.step(m60K(9, 42, 29_005, 29_008, 28_990, 28_995), m60ISB(SideShort, 28_988, 29_000, 28_950, far)); len(out) != 0 {
		t.Fatalf("R30: a third short inside the chop must be refused, got %d", len(out))
	}
	if r.l.Refusals["loss_box_off_day"] != 2 {
		t.Fatalf("R30: want loss_box_off_day=2, refusals=%v", r.l.Refusals)
	}
	// Outside Box 3 is still tradable.
	if out := r.step(m60K(9, 43, 28_995, 29_000, 28_990, 28_996), m60ISB(SideLong, 29_030, 29_021, 29_080, far)); len(out) != 1 {
		t.Fatalf("R30: an entry above Box 3 must pass, got %d (refusals=%v)", len(out), r.l.Refusals)
	}
}

// TestM60RowFillSeedsFromFillCandleOnly — a fill registered from the armed
// ROW (a restart dropped the pend) has no record of the candles before the
// fill: its range starts at the fill candle only, and that is COUNTED
// (loss_range_seed_only), never silent.
func TestM60RowFillSeedsFromFillCandleOnly(t *testing.T) {
	r := &m60Run{l: &Limits{}, cfg: m60Cfg()}
	r.prev = m60K(8, 29, 28_900, 28_904, 28_896, 28_900)
	r.step(m60K(8, 30, 28_900, 28_933.75, 28_895, 28_920))
	r.step(m60K(8, 31, 28_920, 28_925, 28_898, 28_900))
	row := &FillRow{Side: SideLong, Entry: 28_905, Stop: 28_893, Target: 28_960, ISB: true, SignalID: "sig-row"}
	lv := []Level{{Key: "old_extreme:29000", Kind: KindOldExtreme, Price: 29_000}}
	if got := r.l.RecordFill("sig-row", "", 28_896, 28_910, row, lv); got != FillConsumed {
		t.Fatalf("row fill: want consumed, got %v", got)
	}
	if r.l.Counters["loss_range_seed_only"] != 1 {
		t.Fatalf("row fill must count loss_range_seed_only, counters=%v", r.l.Counters)
	}
	r.step(m60K(8, 32, 28_903, 28_908, 28_896, 28_900))
	r.step(m60K(8, 33, 28_900, 28_902, 28_890, 28_891)) // stop — loss
	r.step(m60K(8, 34, 28_891, 28_895, 28_888, 28_894))
	// The range is the fill candle (28,896–28,910) through the stop bucket's
	// end — NOT the pre-fill 08:30 high 28,933.75.
	if p := m60RangePlace(r.l, 28_888, 28_910); p == nil || !p.Blocked {
		t.Fatalf("row fill range must be 28,888–28,910 (fill candle seed), places=%+v", m60Places(r.l))
	}
}

func m60Places(l *Limits) map[string]Place {
	out := map[string]Place{}
	for k, p := range l.Places {
		out[k] = *p
	}
	return out
}

// TestM60RealFillRangeAdoptsFillBucket — the LIVE path (RealFillOnly): the
// broker's fill receipt drains BEFORE the fill candle reaches Apply, and the
// stop-out receipt drains before the stop candle does. The range must still be
// the whole 08:30 5m bucket (28,880.50–28,933.75), including the 08:34 candle
// that closes the bucket after the stop receipt.
func TestM60RealFillRangeAdoptsFillBucket(t *testing.T) {
	far := m60Now(15, 0)
	r := &m60Run{l: &Limits{}, cfg: m60Cfg()}
	r.cfg.RealFillOnly = true
	lv := []Level{{Key: "old_extreme:29000", Kind: KindOldExtreme, Price: 29_000}}
	r.prev = m60K(8, 29, 28_900, 28_904, 28_896, 28_900)
	r.step(m60K(8, 30, 28_900, 28_933.75, 28_895, 28_920))
	in := m60ISB(SideLong, 28_905, 28_893, 28_960, far)
	in.ArmID = "isb-arm-1"
	r.step(m60K(8, 31, 28_920, 28_925, 28_898, 28_900), in)
	// The fill happens inside 08:32; the receipt drains before 08:32's Apply
	// with the last closed bar (08:31) as lo/hi.
	r.l.RecordFill("sig-1", "isb-arm-1", 28_898, 28_925, nil, lv)
	r.step(m60K(8, 32, 28_900, 28_910, 28_896, 28_903))
	// The stop fills inside 08:33; its receipt drains before 08:33's Apply.
	r.l.RealExit("sig-1", true)
	r.step(m60K(8, 33, 28_903, 28_908, 28_885, 28_887))
	r.step(m60K(8, 34, 28_887, 28_899, 28_880.50, 28_890))
	r.step(m60K(8, 35, 28_890, 28_915, 28_888, 28_912))
	p := m60RangePlace(r.l, 28_880.50, 28_933.75)
	if p == nil || !p.Blocked || p.Losses != 1 {
		t.Fatalf("live path: the range must be the whole 08:30 5m bucket, places=%+v", m60Places(r.l))
	}
	if out := r.step(m60K(8, 36, 28_912, 28_922, 28_905, 28_920), m60ISB(SideLong, 28_925, 28_915, 28_960, far)); len(out) != 0 {
		t.Fatalf("live path: 28,925 inside the range must be refused, got %d", len(out))
	}
}

// ── opt-in REAL-DAY replay (range vs band) ─────────────────────────────────
//
// M465_REPLAY="<dir>:<YYYY-MM-DD>[,<dir>:<day>…]" replays each CME session
// (<day-1> 17:00 CT → <day> 16:00 CT) from a deploy/realday_export_day.py
// export (bars read-only from data/data.db — never a DB copy) through the
// PRODUCTION call site e.Tick, one closed 1m bar per tick (1500-bar window,
// the m434 drive, simulated candle fills), once with mentor_isb_loss_box=range
// and once with band, and prints every entry intent that appears or
// disappears plus the loss-range refusals. Skipped when unset.
func TestM60ReplayRealDays(t *testing.T) {
	spec := os.Getenv("M465_REPLAY")
	if spec == "" {
		t.Skip("M465_REPLAY unset")
	}
	for _, one := range strings.Split(spec, ",") {
		dir, day, ok := strings.Cut(one, ":")
		if !ok {
			t.Fatalf("bad M465_REPLAY entry %q", one)
		}
		bars := m60LoadRealday(t, dir)
		d, err := time.ParseInLocation("2006-01-02", day, ctime())
		if err != nil {
			t.Fatal(err)
		}
		from := d.AddDate(0, 0, -1).Add(17 * time.Hour).UnixMilli()
		to := d.Add(16 * time.Hour).UnixMilli()
		rng, rngL := m60ReplayRun(bars, from, to, ISBLossBoxRange)
		bnd, bndL := m60ReplayRun(bars, from, to, ISBLossBoxBand)
		var b strings.Builder
		fmt.Fprintf(&b, "=== M465 REPLAY session %s (range %d entries, band %d entries)\n", day, len(rng), len(bnd))
		for k, v := range bnd {
			if _, ok := rng[k]; !ok {
				fmt.Fprintf(&b, "  GONE  (band only)  %s %s\n", k, v)
			}
		}
		for k, v := range rng {
			if _, ok := bnd[k]; !ok {
				fmt.Fprintf(&b, "  NEW   (range only) %s %s\n", k, v)
			}
		}
		fmt.Fprintf(&b, "  range refusals: loss_range_blocked=%d loss_box_off_day=%d loss_box_blocked=%d · counters loss_range_union=%d\n",
			rngL.Refusals["loss_range_blocked"], rngL.Refusals["loss_box_off_day"], rngL.Refusals["loss_box_blocked"], rngL.Counters["loss_range_union"])
		fmt.Fprintf(&b, "  band  refusals: loss_box_off_day=%d loss_box_blocked=%d\n", bndL.Refusals["loss_box_off_day"], bndL.Refusals["loss_box_blocked"])
		t.Log(b.String())
	}
}

func m60LoadRealday(t *testing.T, dir string) []market.Kline {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, "mnq_realday.json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Bars map[string][][]any `json:"bars"`
	}
	if err := json.NewDecoder(gz).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	var out []market.Kline
	for _, r := range doc.Bars["1m"] {
		ot := int64(r[0].(float64))
		out = append(out, market.Kline{OpenTime: ot, CloseTime: ot + 60_000,
			Open: r[1].(float64), High: r[2].(float64), Low: r[3].(float64), Close: r[4].(float64)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OpenTime < out[j].OpenTime })
	return out
}

// m60ReplayRun drives e.Tick over [from,to) and returns the entry intents
// keyed "<CT time> <action> <side> <price>" → "stop/target arm", plus the
// final Limits ledgers (the day's refusals survive to the session end).
func m60ReplayRun(bars []market.Kline, from, to int64, mode string) (map[string]string, Limits) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.ISBLossBox = mode
	e := New(cfg)
	out := map[string]string{}
	refusals := map[string]int{}
	counters := map[string]int{}
	day := ""
	for i := range bars {
		if bars[i].OpenTime < from || bars[i].OpenTime >= to {
			continue
		}
		lo := i + 1 - 1500
		if lo < 0 {
			lo = 0
		}
		ins := e.Tick(bars[lo:i+1], BarCloseInstant(bars[i]))
		for _, in := range ins {
			switch in.Action {
			case ActionMoveStopBE:
				e.SwingBEApplied(in.ArmID)
			case ActionClosePosition:
				e.SwingEnded(in.ArmID)
			case PlaceStopEntry, PlaceStopLimitEntry:
				at := time.UnixMilli(BarCloseInstant(bars[i])).In(ctime()).Format("01-02 15:04")
				k := fmt.Sprintf("%s %s %s %.2f", at, in.Action, in.Side, in.Price)
				out[k] = fmt.Sprintf("stop %.2f target %.2f arm %s", in.Stop, in.Target, in.ArmID)
			}
		}
		// The trading-day rollover clears the Limits ledgers: fold them per day.
		if dk := e.State.Limits.dayKey; dk != day {
			day = dk
		}
		for k, v := range e.State.Limits.Refusals {
			if strings.HasPrefix(k, "loss_") && v > refusals[k] {
				at := time.UnixMilli(BarCloseInstant(bars[i])).In(ctime()).Format("01-02 15:04")
				for pk, p := range e.State.Limits.Places {
					if p.Blocked || p.OffDay {
						out[fmt.Sprintf("%s REFUSED %s", at, k)] += fmt.Sprintf("[place %s %.2f–%.2f losses %d range %v] ", pk, p.Lo, p.Hi, p.Losses, p.Range)
					}
				}
			}
			refusals[k] = max(refusals[k], v)
		}
		for k, v := range e.State.Limits.Counters {
			counters[k] = max(counters[k], v)
		}
	}
	return out, Limits{Refusals: refusals, Counters: counters}
}

// TestM60LossRangeWickOutAndBackStaysBlocked — D4.2-R27 "em đợi cho cái điểm
// entry nằm ở vòng vòng bên ngoài" [D4.2 p2 @02:41–03:17] and R29 "tôi bọc cái
// khu vực này lại. Tôi sẽ không trade bên trong khu vực này. Tôi đợi nó đi ra
// một cái khu vực khác" [@03:43–04:07]. A 1m candle wholly outside the range
// frees entries OUTSIDE it only: an entry priced INSIDE the old loss range is
// still refused loss_range_blocked after a wick out and straight back (a wick
// out and back is not a different zone). The range stays a no-trade area for
// the trading day; outside entries still pass.
// MUTANT: blockedPlace skips a freed Range place again → the 28,920 entry
// passes → RED.
func TestM60LossRangeWickOutAndBackStaysBlocked(t *testing.T) {
	far := m60Now(15, 0)
	r := &m60Run{l: &Limits{}, cfg: m60Cfg()}
	m60Box1(t, r)
	p1 := m60RangePlace(r.l, 28_880.50, 28_933.75)
	if p1 == nil || !p1.Blocked {
		t.Fatalf("fixture: the WE-3 range must be boxed, places=%+v", m60Places(r.l))
	}
	// 08:36: one 1m candle wholly above the range — departure.
	r.step(m60K(8, 36, 28_935, 28_942, 28_934.25, 28_940))
	if p1.Blocked {
		t.Fatalf("a 1m candle wholly above the range frees it for entries outside")
	}
	// 08:37: straight back in; a long ISB entry at 28,920 is INSIDE the range.
	before := r.l.Refusals["loss_range_blocked"]
	if out := r.step(m60K(8, 37, 28_940, 28_941, 28_915, 28_918), m60ISB(SideLong, 28_920, 28_910, 28_990, far)); len(out) != 0 {
		t.Fatalf("R27/R29: an entry at 28,920 inside the old loss range must stay refused after a wick out and back, got %d", len(out))
	}
	if got := r.l.Refusals["loss_range_blocked"]; got != before+1 {
		t.Fatalf("R27/R29: want loss_range_blocked counted once more, refusals=%v", r.l.Refusals)
	}
	// A short entry priced inside from above is the same area — refused too.
	if out := r.step(m60K(8, 38, 28_918, 28_930, 28_912, 28_925), m60ISB(SideShort, 28_910, 28_932, 28_850, far)); len(out) != 0 {
		t.Fatalf("R29: a short at 28,910 inside the range must stay refused, got %d", len(out))
	}
	// Outside the range (above 28,933.75) still passes.
	if out := r.step(m60K(8, 39, 28_925, 28_936, 28_924, 28_935), m60ISB(SideLong, 28_938, 28_928, 28_990, m60Now(8, 40))); len(out) != 1 {
		t.Fatalf("R27: an entry outside the range passes after the departure, got %d (refusals=%v)", len(out), r.l.Refusals)
	}
}
