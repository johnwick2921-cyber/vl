// Command pep-backtest replays the pep evaluator (kernel/pep, the SAME code the
// live loop will call) over stored MNQ 1m history and compares it DAY BY DAY with
// the owner's PDF "A + E — Full calculation" ledgers.
//
//	go run ./cmd/pep-backtest -db /path/to/COPY-of-data.db \
//	    -pdf-text /home/hoang/fleet-1017/out/pep-spec-coa707.txt \
//	    -out /home/hoang/fleet-1017/out/pep-backtest-data.md
//
// READ-ONLY: the database is opened mode=ro with query_only; point it at a COPY.
// It writes only the -out report (a NEW file; an existing one is refused).
//
// Method (each choice is a flag or a labelled constant):
//   - rows from source historical_import are close-stamped and are shifted -60 s;
//   - 2m candles are built from 1m on CT-even minutes inside kernel/pep;
//   - America/Chicago zoneinfo (never a fixed offset);
//   - $1 round trip per contract, same-bar stop before target, flat at 15:00 CT at
//     the last price, 3 and 6 contracts;
//   - the evaluator is handed the last -window closed bars at every closed 1m bar.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	_ "vl/store/sqlitedriver" // registers database/sql driver "sqlite"

	"vl/kernel/pep"
	"vl/market"
)

func main() {
	var (
		dbPath   = flag.String("db", "", "path to a COPY of data.db (opened read-only)")
		pdfText  = flag.String("pdf-text", "/home/hoang/fleet-1017/out/pep-spec-coa707.txt", "pdftotext extraction of the owner's PDF")
		outPath  = flag.String("out", "", "report path (must NOT exist)")
		from     = flag.String("from", "2025-10-20", "first CT day")
		to       = flag.String("to", "2026-04-17", "last CT day")
		window   = flag.Int("window", 1500, "closed 1m bars handed to Evaluate (the live feed reads 1500)")
		shiftMs  = flag.Int64("import-shift-ms", ImportShiftMs, "shift for historical_import rows (close-stamped)")
		bothEdge = flag.String("both-edges", "no_trade", "style A, one bar through both edges: no_trade | nearest_open")
		eBox     = flag.Int("e-box-minutes", 2, "style E box minutes")
		eSrc     = flag.String("e-source", "ema", "style E signal source: ema | close")
		eTF      = flag.Int("e-tf", 2, "style E candle minutes")
		ePeriod  = flag.Int("e-period", 9, "style E EMA period")
		aBox     = flag.Int("a-box-minutes", 5, "style A box minutes")
		csvOut   = flag.String("csv", "", "optional per-day CSV path (must NOT exist)")
		stopThru = flag.Bool("stop-through", true, "a stop fills only when a bar trades strictly THROUGH it (a touch does not). Default ON: it is what the PDF engine does (2026-03-25 E)")
		tgtThru  = flag.Bool("target-through", false, "a target fills only when a bar trades strictly THROUGH it")
		feeRT    = flag.Float64("fee", pep.DefaultConfig().FeePerContractRT, "round-trip fee per contract in USD (the pep_fee_per_contract_usd setting; reporting only)")
		quiet    = flag.Bool("quiet", false, "omit the per-day tables, print the summary only")
	)
	flag.Parse()
	if *dbPath == "" || *outPath == "" {
		fmt.Fprintln(os.Stderr, "need -db (a COPY of data.db) and -out (a new file)")
		os.Exit(2)
	}
	for _, p := range []string{*outPath, *csvOut} {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			fmt.Fprintf(os.Stderr, "%s exists: refusing to overwrite (move it to ~/trash first)\n", p)
			os.Exit(2)
		}
	}

	cfgA := pep.DefaultConfig()
	cfgA.Style = pep.StyleA
	cfgA.FeePerContractRT = *feeRT
	cfgA.A.BoxMinutes = *aBox
	cfgA.A.BothEdges = pep.BothEdges(*bothEdge)
	cfgE := pep.DefaultConfig()
	cfgE.Style = pep.StyleE
	cfgE.FeePerContractRT = *feeRT
	cfgE.E.BoxMinutes = *eBox
	cfgE.E.Source = pep.SignalSource(*eSrc)
	cfgE.E.EMATFMin = *eTF
	cfgE.E.EMAPeriod = *ePeriod
	for _, c := range []pep.Config{cfgA, cfgE} {
		if err := c.Validate(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}

	rep, err := run(*dbPath, *pdfText, *from, *to, *window, *shiftMs, cfgA, cfgE, pep.ReplayOpts{StopThrough: *stopThru, TargetThrough: *tgtThru})
	if err != nil {
		fmt.Fprintln(os.Stderr, "pep-backtest:", err)
		os.Exit(1)
	}
	md := rep.render(!*quiet)
	if err := writeNew(*outPath, md); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *csvOut != "" {
		if err := writeNew(*csvOut, rep.csv()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	fmt.Println(rep.summaryText())
}

// writeNew creates path exclusively (O_EXCL): an existing file is never touched.
func writeNew(path, body string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// report is the whole run.
type report struct {
	from, to, window  string
	shift             int64
	cfgs              map[pep.Style]pep.Config
	days              []string
	lines             map[pep.Style][]dayLine
	pdf               map[pep.Style][]pdfRow
	skipped           map[pep.Style]map[string]bool
	pdfWindow         int
	noData            []string
	sources           string
	opts              pep.ReplayOpts
	contractsByDay    map[string]string
	cooldownSkipLists map[pep.Style][]string
	short             map[string]bool
}

// pdfShortSessions are the holiday/half-day sessions the PDF's 122 valid
// sessions leave out. INFERRED, not stated by the PDF: 110 days with bars minus
// these 5 plus the 17 missing days equals 122 exactly (explorer 5, 2026-10-29).
var pdfShortSessions = []string{"2025-11-27", "2025-11-28", "2025-12-24", "2026-01-19", "2026-02-16"}

// the PDF's own "days with a signal skipped because of cooldown" lists.
var pdfCooldown = map[pep.Style][]string{
	pep.StyleA: {"2026-01-15", "2026-01-16", "2026-01-20", "2026-01-21", "2026-01-22", "2026-01-23", "2026-01-26", "2026-01-27"},
	pep.StyleE: {"2026-02-18", "2026-02-19", "2026-02-20", "2026-02-23", "2026-02-24", "2026-02-25", "2026-02-26", "2026-02-27", "2026-03-02"},
}

func run(dbPath, pdfText, from, to string, window int, shift int64, cfgA, cfgE pep.Config, opts pep.ReplayOpts) (*report, error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	loc, err := pep.CTLocation()
	if err != nil {
		return nil, err
	}
	f, err := time.ParseInLocation("2006-01-02", from, loc)
	if err != nil {
		return nil, err
	}
	t, err := time.ParseInLocation("2006-01-02", to, loc)
	if err != nil {
		return nil, err
	}
	// 10 days of lead for the EMA warm-up/overnight, 2 days of tail
	series, err := loadSeries(db, "MNQ", f.AddDate(0, 0, -10).UnixMilli(), t.AddDate(0, 0, 2).UnixMilli(), shift)
	if err != nil {
		return nil, err
	}
	days, err := weekdays(from, to)
	if err != nil {
		return nil, err
	}
	pa, pe, err := parsePDFLedgers(pdfText)
	if err != nil {
		return nil, err
	}
	rep := &report{
		from: from, to: to, window: fmt.Sprint(window), shift: shift, days: days, opts: opts,
		cfgs:           map[pep.Style]pep.Config{pep.StyleA: cfgA, pep.StyleE: cfgE},
		lines:          map[pep.Style][]dayLine{},
		pdf:            map[pep.Style][]pdfRow{pep.StyleA: pa, pep.StyleE: pe},
		skipped:        map[pep.Style]map[string]bool{},
		contractsByDay: map[string]string{},
	}
	for st, ds := range pdfCooldown {
		rep.skipped[st] = map[string]bool{}
		for _, d := range ds {
			rep.skipped[st][d] = true
		}
	}
	rep.short = map[string]bool{}
	for _, d := range pdfShortSessions {
		rep.short[d] = true
	}
	var names []string
	for c, s := range series {
		names = append(names, fmt.Sprintf("%s=%d", c, len(s)))
	}
	sort.Strings(names)
	rep.sources = strings.Join(names, ", ")

	// PDF rows by date; also PDF dates outside weekdays range are kept
	pdfBy := map[pep.Style]map[string]*pdfRow{pep.StyleA: {}, pep.StyleE: {}}
	for i := range pa {
		pdfBy[pep.StyleA][pa[i].Date] = &pa[i]
	}
	for i := range pe {
		pdfBy[pep.StyleE][pe[i].Date] = &pe[i]
	}
	allDays := map[string]bool{}
	for _, d := range days {
		allDays[d] = true
	}
	for _, st := range []pep.Style{pep.StyleA, pep.StyleE} {
		for d := range pdfBy[st] {
			allDays[d] = true // a PDF day outside Mon-Fri would still be listed
		}
	}
	var every []string
	for d := range allDays {
		every = append(every, d)
	}
	sort.Strings(every)
	rep.days = every

	for _, d := range every {
		contract, n := pickContract(series, d, cfgA.BoxStartMin, cfgA.FlatMin)
		rep.contractsByDay[d] = contract
		for _, st := range []pep.Style{pep.StyleA, pep.StyleE} {
			cfg := rep.cfgs[st]
			l := dayLine{Date: d, PDF: pdfBy[st][d], Contract: contract}
			if n == 0 {
				l.NoData = true
				l.Mine = pep.DayResult{Date: d, Style: st, Refusal: "NO_DATA"}
			} else {
				l.Mine = pep.ReplayDayOpts(series[contract], d, cfg, window, opts)
			}
			classify(&l, st, 6, rep.skipped[st], rep.short)
			rep.lines[st] = append(rep.lines[st], l)
		}
		if n == 0 {
			rep.noData = append(rep.noData, d)
		}
	}
	return rep, nil
}

var _ = market.Kline{}
