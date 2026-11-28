package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "vl/store/sqlitedriver"

	"vl/kernel/pep"
)

const miniPDF = `===== PAGE 5 =====
3. E — 6 MNQ: đối chiếu từng giai đoạn
2025-10-20
1
SIM
===== PAGE 6 =====
4. A — 6 MNQ: từng ngày giao dịch (USD)
 Ngày
2025-10-20
1
SIM
50,000.00
1,041.00
6.00
1,035.00
0.00
0.00
51,035.00
TP
2025-12-04
1
SIM
54,943.00
-813.00
6.00
-819.00
0.00
0.00
53,731.00
CUTOFF_PROXY
===== PAGE 12 =====
3. E — 6 MNQ: đối chiếu từng giai đoạn
2025-10-20
===== PAGE 14 =====
4. E — 6 MNQ: từng ngày giao dịch (USD)
2025-10-21
1
SIM
50,870.00
-216.00
6.00
-222.00
0.00
0.00
50,648.00
SL
===== PAGE 19 =====
5. Kiểm chứng, nguồn và giới hạn
2025-10-22
`

func TestParsePDFLedgersReadsOnlyTheDayTables(t *testing.T) {
	p := filepath.Join(t.TempDir(), "pdf.txt")
	if err := os.WriteFile(p, []byte(miniPDF), 0o644); err != nil {
		t.Fatal(err)
	}
	a, e, err := parsePDFLedgers(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 2 || len(e) != 1 {
		t.Fatalf("a=%d e=%d (stage-table dates must be ignored)", len(a), len(e))
	}
	if a[0].Date != "2025-10-20" || a[0].Gross != 1041 || a[0].Result != "TP" || a[0].Stage != "SIM" {
		t.Fatalf("%+v", a[0])
	}
	if a[1].Gross != -813 || a[1].Result != "CUTOFF_PROXY" {
		t.Fatalf("%+v", a[1])
	}
	if e[0].Date != "2025-10-21" || e[0].Gross != -216 || e[0].Result != "SL" {
		t.Fatalf("%+v", e[0])
	}
}

// The real extraction is checked in when present: 113 A rows and 106 E rows.
func TestParseRealPDFRowCounts(t *testing.T) {
	const p = "/home/hoang/fleet-1017/out/pep-spec-coa707.txt"
	if _, err := os.Stat(p); err != nil {
		t.Skip("PDF extraction not on this box")
	}
	a, e, err := parsePDFLedgers(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 113 || len(e) != 106 {
		t.Fatalf("A rows %d (want 113), E rows %d (want 106)", len(a), len(e))
	}
}

func day(traded bool, side pep.Side, entry, exit float64, kind pep.ExitKind, box float64) pep.DayResult {
	d := pep.DayResult{Traded: traded, Side: side, Entry: entry, Exit: exit, ExitKind: kind}
	d.Box = pep.Box{Size: box, Drawn: true}
	if side == pep.SideLong {
		d.GrossPts = exit - entry
	} else {
		d.GrossPts = entry - exit
	}
	return d
}

func TestClassifyVerdicts(t *testing.T) {
	skip := map[string]bool{"2026-01-15": true}
	short := map[string]bool{"2025-11-27": true}
	mk := func(date string, p *pdfRow, m pep.DayResult) dayLine {
		l := dayLine{Date: date, PDF: p, Mine: m}
		classify(&l, pep.StyleA, 6, skip, short)
		return l
	}
	tp := &pdfRow{Date: "d", Gross: 1041, Result: "TP"}
	if l := mk("d", tp, day(true, pep.SideLong, 100, 186.75, pep.ExitTP, 86.75)); l.Verdict != VMatch {
		t.Fatalf("%+v", l)
	}
	if l := mk("d", tp, day(true, pep.SideLong, 100, 186.5, pep.ExitTP, 86.5)); l.Verdict != VNear {
		t.Fatalf("%+v", l)
	}
	if l := mk("d", tp, day(true, pep.SideLong, 100, 60, pep.ExitSL, 40)); l.Verdict != VDiffExit {
		t.Fatalf("%+v", l)
	}
	if l := mk("d", tp, pep.DayResult{Refusal: "NO_DATA"}); l.Verdict != VPDFOnly {
		t.Fatalf("%+v", l)
	}
	if l := mk("2026-01-15", nil, day(true, pep.SideLong, 100, 140, pep.ExitTP, 40)); l.Verdict != VCooldown {
		t.Fatalf("%+v", l)
	}
	if l := mk("2025-11-27", nil, day(true, pep.SideLong, 100, 140, pep.ExitTP, 40)); l.Verdict != VShort {
		t.Fatalf("%+v", l)
	}
	if l := mk("2026-02-10", nil, day(true, pep.SideLong, 100, 140, pep.ExitTP, 40)); l.Verdict != VMineOnly {
		t.Fatalf("%+v", l)
	}
	br := &pdfRow{Date: "d", Gross: -99, Result: "MLL_BREACH"}
	if l := mk("d", br, day(true, pep.SideLong, 100, 140, pep.ExitTP, 40)); l.Verdict != VBreachRow {
		t.Fatalf("%+v", l)
	}
}

func TestWeekdaysSkipsWeekends(t *testing.T) {
	ds, err := weekdays("2026-01-09", "2026-01-13")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2026-01-09", "2026-01-12", "2026-01-13"}
	if len(ds) != 3 || ds[0] != want[0] || ds[1] != want[1] || ds[2] != want[2] {
		t.Fatalf("%v", ds)
	}
}

// loadSeries on a read-only DB: import rows shift -60 s, a real row at the
// same minute wins, and the connection can write nothing.
func TestLoadSeriesShiftsImportsAndIsReadOnly(t *testing.T) {
	loc, _ := pep.CTLocation()
	t0 := time.Date(2026, 1, 12, 8, 31, 0, 0, loc).UnixMilli() // a close-stamped import label
	path := filepath.Join(t.TempDir(), "d.db")
	w, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE bars (symbol text, tf text, contract text, open_time_ms integer, o real, h real, l real, c real, v real, convention text, source text)`,
		`INSERT INTO bars VALUES ('MNQ','1m','MNQ 03-26',` + itoa(t0) + `,1,2,0.5,1.5,10,'epoch_floor','historical_import')`,
		`INSERT INTO bars VALUES ('MNQ','1m','MNQ 03-26',` + itoa(t0-60000) + `,9,9,9,9,99,'epoch_floor','live')`,
		`INSERT INTO bars VALUES ('MNQ','1m','MNQ 03-26',` + itoa(t0+60000) + `,3,4,2,3.5,10,'epoch_floor','historical_import')`,
	} {
		if _, err := w.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()
	ro, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=query_only(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	got, err := loadSeries(ro, "MNQ", t0-3600_000, t0+3600_000, ImportShiftMs)
	if err != nil {
		t.Fatal(err)
	}
	s := got["MNQ 03-26"]
	if len(s) != 2 {
		t.Fatalf("want 2 minutes, got %d: %+v", len(s), s)
	}
	// the import at label t0 is the bar OPENING at t0-60000; the live row there wins
	if s[0].OpenTime != t0-60000 || s[0].Open != 9 {
		t.Fatalf("real row must win at the shifted minute: %+v", s[0])
	}
	// the import at label t0+60000 opens at t0
	if s[1].OpenTime != t0 || s[1].Open != 3 {
		t.Fatalf("import shift: %+v", s[1])
	}
	if _, err := ro.Exec(`DELETE FROM bars`); err == nil {
		t.Fatal("the read-only connection accepted a write")
	}
}

// m460 interaction: an import row already stamped at the bar OPEN (convention
// epoch_floor_open: the importer since m460, or a row the owner-run shift tool
// re-stamped) must NOT be shifted again. MUTANT: drop the convention test in
// loadSeries (shift every import row) -> the open-stamped row lands a minute
// early -> RED.
func TestLoadSeriesDoesNotShiftOpenStampedImports(t *testing.T) {
	loc, _ := pep.CTLocation()
	t0 := time.Date(2026, 1, 12, 8, 30, 0, 0, loc).UnixMilli()
	path := filepath.Join(t.TempDir(), "d.db")
	w, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE bars (symbol text, tf text, contract text, open_time_ms integer, o real, h real, l real, c real, v real, convention text, source text)`,
		`INSERT INTO bars VALUES ('MNQ','1m','MNQ 03-26',` + itoa(t0) + `,1,2,0.5,1.5,10,'epoch_floor_open','historical_import')`,
		`INSERT INTO bars VALUES ('MNQ','1m','MNQ 03-26',` + itoa(t0+120000) + `,3,4,2,3.5,10,'epoch_floor','historical_import')`,
	} {
		if _, err := w.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()
	ro, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=query_only(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	got, err := loadSeries(ro, "MNQ", t0-3600_000, t0+3600_000, ImportShiftMs)
	if err != nil {
		t.Fatal(err)
	}
	s := got["MNQ 03-26"]
	if len(s) != 2 || s[0].OpenTime != t0 || s[0].Open != 1 {
		t.Fatalf("open-stamped import must stay at its label: %+v", s)
	}
	// the old close-stamped row (epoch_floor) still shifts one minute
	if s[1].OpenTime != t0+120000-60000 || s[1].Open != 3 {
		t.Fatalf("close-stamped import must shift -60s: %+v", s[1])
	}
}

func itoa(n int64) string { return fmtInt(n) }

func fmtInt(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func TestWriteNewRefusesToOverwrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "r.md")
	if err := writeNew(p, "one"); err != nil {
		t.Fatal(err)
	}
	if err := writeNew(p, "two"); err == nil {
		t.Fatal("overwrote an existing report")
	}
	if b, _ := os.ReadFile(p); string(b) != "one" {
		t.Fatalf("content %q", b)
	}
}

// The fee is the pep_fee_per_contract_usd setting, not a constant: the rendered
// summary must move with Config.FeePerContractRT (reviewer repro: DefaultConfig
// fee 1 -> 2 left the report byte-identical).
func TestRenderFeesFollowTheFeeSetting(t *testing.T) {
	build := func(fee float64) *report {
		cfgA := pep.DefaultConfig()
		cfgA.Style = pep.StyleA
		cfgA.FeePerContractRT = fee
		cfgE := pep.DefaultConfig()
		cfgE.Style = pep.StyleE
		cfgE.FeePerContractRT = fee
		ls := []dayLine{
			{Date: "2025-10-20", Mine: day(true, pep.SideLong, 100, 110, pep.ExitTP, 10)},
			{Date: "2025-10-21", Mine: day(true, pep.SideShort, 100, 110, pep.ExitSL, 10)},
		}
		return &report{
			cfgs:  map[pep.Style]pep.Config{pep.StyleA: cfgA, pep.StyleE: cfgE},
			lines: map[pep.Style][]dayLine{pep.StyleA: ls, pep.StyleE: ls},
			pdf:   map[pep.Style][]pdfRow{},
		}
	}
	// 2 traded days: fee $1 -> 12 / 6; fee $2.50 -> 30 / 15; fee 0 -> 0 / 0.
	for _, c := range []struct {
		fee  float64
		want string
	}{
		{1, "| fees at $1.00 RT/contract, 6 / 3 MNQ | 12 / 6 |"},
		{2.5, "| fees at $2.50 RT/contract, 6 / 3 MNQ | 30 / 15 |"},
		{0, "| fees at $0.00 RT/contract, 6 / 3 MNQ | 0 / 0 |"},
	} {
		got := build(c.fee).render(false)
		if !strings.Contains(got, c.want) {
			t.Fatalf("fee %v: report lacks %q", c.fee, c.want)
		}
	}
	// net moves with the fee: gross 6 MNQ = (10-10)*... flat, so net = -fees.
	if got := build(2.5).render(false); !strings.Contains(got, "| net USD, 6 MNQ | -30 |") {
		t.Fatalf("net does not subtract the setting's fee:\n%s", got)
	}
}
