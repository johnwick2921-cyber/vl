package main

import (
	"database/sql"
	"fmt"
	"sort"
	"time"

	"vl/kernel/pep"
	"vl/market"
	"vl/store"
)

// ImportShiftMs is applied to rows whose source is historical_import: the
// importer stored the bar's CLOSE as the open_time (explorer finding 6, verified
// 2026-10-30 by identical-OHLCV rows: an import row at T equals the live row at
// T-60000). The convention column says epoch_floor on those rows; it is wrong.
const ImportShiftMs = -60_000

// loadSeries reads MNQ 1m rows whose OPEN time falls in [fromMs, toMs] from a
// SQLite file opened READ-ONLY and returns one ascending, OPEN-stamped series
// per contract. Rows from source historical_import are shifted by `shift`; a
// non-import row at the same contract+minute wins over an import row.
func loadSeries(db *sql.DB, symbol string, fromMs, toMs int64, shift int64) (map[string][]market.Kline, error) {
	rows, err := db.Query(`SELECT contract, open_time_ms, o, h, l, c, v, COALESCE(source,''), COALESCE(convention,'') FROM bars
		WHERE symbol = ? AND tf = '1m' AND open_time_ms >= ? AND open_time_ms <= ?
		ORDER BY contract, open_time_ms`, symbol, fromMs-2*60_000, toMs+2*60_000)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type key struct {
		c string
		t int64
	}
	type val struct {
		k   market.Kline
		imp bool
	}
	got := map[key]val{}
	for rows.Next() {
		var contract, source, convention string
		var t int64
		var o, h, l, c, v sql.NullFloat64
		if err := rows.Scan(&contract, &t, &o, &h, &l, &c, &v, &source, &convention); err != nil {
			return nil, err
		}
		if !o.Valid || !h.Valid || !l.Valid || !c.Valid {
			continue
		}
		imp := source == "historical_import"
		// m460: rows the importer wrote since it OPEN-stamps (or that the owner-run
		// cmd/import-label-shift tool has already re-stamped) carry
		// store.ConventionEpochFloorOpen and are right as stored; shifting them
		// again would put every bar one minute EARLY. Only the old close-stamped
		// rows (plain epoch_floor) are shifted.
		if imp && convention != store.ConventionEpochFloorOpen {
			t += shift
		}
		if t < fromMs || t > toMs {
			continue
		}
		k := key{contract, t}
		if prev, dup := got[k]; dup && !prev.imp {
			continue // a real row is already kept
		}
		got[k] = val{market.Kline{OpenTime: t, CloseTime: t + 59_999, Open: o.Float64, High: h.Float64, Low: l.Float64, Close: c.Float64, Volume: v.Float64}, imp}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	byC := map[string][]market.Kline{}
	for k, v := range got {
		byC[k.c] = append(byC[k.c], v.k)
	}
	for c := range byC {
		s := byC[c]
		sort.Slice(s, func(i, j int) bool { return s[i].OpenTime < s[j].OpenTime })
	}
	return byC, nil
}

// pickContract returns the contract with the most 1m bars in the day's
// [boxStart, flat) window, and that count. Zero count = no data that day.
func pickContract(series map[string][]market.Kline, date string, boxStartMin, flatMin int) (string, int) {
	loc, err := pep.CTLocation()
	if err != nil {
		return "", 0
	}
	d, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return "", 0
	}
	y, mo, dd := d.Date()
	a := time.Date(y, mo, dd, boxStartMin/60, boxStartMin%60, 0, 0, loc).UnixMilli()
	b := time.Date(y, mo, dd, flatMin/60, flatMin%60, 0, 0, loc).UnixMilli()
	best, bestN := "", 0
	names := make([]string, 0, len(series))
	for c := range series {
		names = append(names, c)
	}
	sort.Strings(names)
	for _, c := range names {
		s := series[c]
		i := sort.Search(len(s), func(k int) bool { return s[k].OpenTime >= a })
		n := 0
		for ; i < len(s) && s[i].OpenTime < b; i++ {
			n++
		}
		if n > bestN {
			best, bestN = c, n
		}
	}
	return best, bestN
}

// weekdays lists the CT calendar weekdays in [from, to] inclusive.
func weekdays(from, to string) ([]string, error) {
	f, err := time.Parse("2006-01-02", from)
	if err != nil {
		return nil, fmt.Errorf("from: %w", err)
	}
	t, err := time.Parse("2006-01-02", to)
	if err != nil {
		return nil, fmt.Errorf("to: %w", err)
	}
	var out []string
	for d := f; !d.After(t); d = d.AddDate(0, 0, 1) {
		if wd := d.Weekday(); wd == time.Saturday || wd == time.Sunday {
			continue
		}
		out = append(out, d.Format("2006-01-02"))
	}
	return out, nil
}
