package main

import (
	"fmt"
	"math"
	"strings"
	"time"

	"vl/kernel/pep"
)

// Verdicts of one day's comparison against the PDF ledger.
const (
	VMatch     = "MATCH"         // both traded; same side-of-P&L, same exit, gross equal to the dollar
	VNear      = "NEAR"          // both traded, same exit kind and sign, gross within a few ticks
	VDiffExit  = "DIFF_EXIT"     // both traded, different exit kind or sign
	VDiffSize  = "DIFF_SIZE"     // both traded, same exit kind and sign, gross differs by more than a few ticks
	VPDFOnly   = "PDF_ONLY"      // the PDF traded, this run did not
	VMineOnly  = "MINE_ONLY"     // this run traded, the PDF has no row
	VCooldown  = "COOLDOWN"      // this run traded, the PDF skipped the day (account cooldown after a live breach)
	VBreachRow = "PDF_BREACH"    // PDF row is an account-breach day: P&L truncated by the account rule, not comparable
	VShort     = "SHORT_SESSION" // a holiday/half-day session the PDF's 122 valid sessions leave out
)

// dayLine is one compared day.
type dayLine struct {
	Date     string
	PDF      *pdfRow
	Mine     pep.DayResult
	NoData   bool
	Contract string
	Verdict  string
	Note     string
	// PDFBoxPts is the box implied by the PDF row (TP rows: gross/(6*2);
	// SL rows of style A: the same; style E SL rows: twice the stop).
	PDFBoxPts float64
}

const pointValue = 2.0 // MNQ $2 per point

func pdfExit(r string) string {
	switch r {
	case "TP":
		return "TP"
	case "SL":
		return "SL"
	case "CUTOFF_PROXY":
		return "FLAT"
	}
	return r
}

// implied PDF box (points) for a PDF row.
func impliedBox(style pep.Style, r pdfRow, contracts int) float64 {
	pts := math.Abs(r.Gross) / (pointValue * float64(contracts))
	switch {
	case r.Result == "TP":
		return pts
	case r.Result == "SL" && style == pep.StyleA:
		return pts
	case r.Result == "SL" && style == pep.StyleE:
		return 2 * pts // the stop is half the box (rounded outward: +-1 tick)
	}
	return 0
}

// classify fills Verdict and Note for a day where the PDF row may or may not
// exist and this run may or may not have traded.
func classify(l *dayLine, style pep.Style, contracts int, skipped, short map[string]bool) {
	m := l.Mine
	if l.PDF != nil {
		l.PDFBoxPts = impliedBox(style, *l.PDF, contracts)
	}
	switch {
	case l.PDF == nil && m.Traded:
		if short[l.Date] {
			l.Verdict, l.Note = VShort, "short/holiday session: not one of the PDF's 122 valid sessions (110 days with bars - these 5 + 17 missing = 122)"
		} else if skipped[l.Date] {
			l.Verdict, l.Note = VCooldown, "PDF skipped this day (cooldown after a live breach); this run has no account model"
		} else {
			l.Verdict, l.Note = VMineOnly, "PDF has no signal row this day (not cooldown, not a short session); this run trades it"
		}
	case l.PDF == nil:
		l.Verdict, l.Note = "NO_TRADE", "neither traded"
		if l.NoData {
			l.Note = "no bars in the DB; PDF has no row"
		} else if m.Refusal != "" {
			l.Note = "neither traded (this run: " + m.Refusal + ")"
		}
	case !m.Traded:
		l.Verdict = VPDFOnly
		if l.NoData {
			l.Note = "NO DATA: the DB holds no 1m bars for this CT day (contract-roll gap)"
		} else {
			l.Note = "this run refused: " + m.Refusal + " (" + m.RefusalDetail + ")"
		}
	default:
		mineGross := m.GrossUSD(contracts, pointValue)
		pe := pdfExit(l.PDF.Result)
		me := string(m.ExitKind)
		if m.ExitKind == pep.ExitDataEnd {
			me = "FLAT"
		}
		boxNote := ""
		if l.PDFBoxPts > 0 && math.Abs(l.PDFBoxPts-m.Box.Size) > 0.26 {
			boxNote = fmt.Sprintf("box differs (this run %.2f vs PDF-implied %.2f)", m.Box.Size, l.PDFBoxPts)
			if style == pep.StyleE && l.PDF.Result == "SL" && math.Abs(l.PDFBoxPts-m.Box.Size) <= 1.01 {
				boxNote = "" // the PDF stop is rounded outward: the implied box is +-1 tick
			}
		}
		switch {
		case strings.Contains(l.PDF.Result, "BREACH"):
			l.Verdict = VBreachRow
			l.Note = fmt.Sprintf("PDF row is an account-breach day (%s): its P&L is cut by the account rule", l.PDF.Result)
		case math.Abs(mineGross-l.PDF.Gross) < 0.5 && pe == me:
			l.Verdict = VMatch
		case pe == me && sign(mineGross) == sign(l.PDF.Gross) && math.Abs(mineGross-l.PDF.Gross) <= 12*float64(contracts)/6*pointValue*2:
			l.Verdict = VNear
			l.Note = fmt.Sprintf("gross %.0f vs PDF %.0f", mineGross, l.PDF.Gross)
		case pe == me && sign(mineGross) == sign(l.PDF.Gross):
			l.Verdict = VDiffSize
			l.Note = fmt.Sprintf("gross %.0f vs PDF %.0f", mineGross, l.PDF.Gross)
		default:
			l.Verdict = VDiffExit
			l.Note = fmt.Sprintf("this run %s %.0f; PDF %s %.0f", me, mineGross, pe, l.PDF.Gross)
		}
		if boxNote != "" {
			if l.Note != "" {
				l.Note += "; "
			}
			l.Note += boxNote
		}
		if m.BothEdges {
			l.Note += "; first break bar spans both edges"
		}
	}
}

func sign(x float64) int {
	switch {
	case x > 0:
		return 1
	case x < 0:
		return -1
	}
	return 0
}

func clockMs(ms int64) string {
	loc, err := pep.CTLocation()
	if err != nil {
		return ""
	}
	return time.UnixMilli(ms).In(loc).Format("15:04")
}
