package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// pdfRow is one line of the owner's PDF per-day ledger (6 MNQ).
type pdfRow struct {
	Date   string
	Stage  string // SIM / LIVE
	Gross  float64
	Fee    float64
	Result string // TP, SL, CUTOFF_PROXY, MLL_BREACH, LIVE_MLL_BREACH
}

var dateLine = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// parsePDFLedgers reads the pdftotext extraction (one field per line) and
// returns the style-A and style-E 6-MNQ per-day ledgers. The A ledger is the
// text between the "4. A" heading and the "3. E" heading; the E ledger is
// between "4. E" and "5. ". Stage/payout tables elsewhere also carry dates and
// are deliberately outside those windows.
func parsePDFLedgers(path string) (a, e []pdfRow, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	lines := strings.Split(string(b), "\n")
	section := ""
	var cur []string
	flush := func() {}
	_ = flush
	parse := func(date string, f []string) (pdfRow, error) {
		// GĐ, Loại, Đầu ngày, Gross, Phí, Net, Gross rút, Thực nhận, Cuối ngày, Kết quả
		if len(f) < 10 {
			return pdfRow{}, fmt.Errorf("row %s has %d fields", date, len(f))
		}
		num := func(s string) (float64, error) {
			return strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(s), ",", ""), 64)
		}
		g, err := num(f[3])
		if err != nil {
			return pdfRow{}, fmt.Errorf("row %s gross %q: %w", date, f[3], err)
		}
		fee, err := num(f[4])
		if err != nil {
			return pdfRow{}, fmt.Errorf("row %s fee %q: %w", date, f[4], err)
		}
		return pdfRow{Date: date, Stage: f[1], Gross: g, Fee: fee, Result: f[9]}, nil
	}
	date := ""
	emit := func() error {
		if date == "" {
			return nil
		}
		r, err := parse(date, cur)
		date, cur = "", nil
		if err != nil {
			return err
		}
		switch section {
		case "A":
			a = append(a, r)
		case "E":
			e = append(e, r)
		}
		return nil
	}
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(ln, "4. A "):
			if err := emit(); err != nil {
				return nil, nil, err
			}
			section = "A"
			continue
		case strings.HasPrefix(ln, "3. E "):
			if err := emit(); err != nil {
				return nil, nil, err
			}
			section = ""
			continue
		case strings.HasPrefix(ln, "4. E "):
			if err := emit(); err != nil {
				return nil, nil, err
			}
			section = "E"
			continue
		case strings.HasPrefix(ln, "5. "):
			if err := emit(); err != nil {
				return nil, nil, err
			}
			section = ""
			continue
		}
		if section == "" || ln == "" || strings.HasPrefix(ln, "=====") {
			continue
		}
		if dateLine.MatchString(ln) {
			if err := emit(); err != nil {
				return nil, nil, err
			}
			date = ln
			continue
		}
		if date != "" && len(cur) < 10 {
			cur = append(cur, ln)
		}
	}
	if err := emit(); err != nil {
		return nil, nil, err
	}
	return a, e, nil
}
