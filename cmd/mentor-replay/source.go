package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"vl/calendar"
	"vl/market"
	"vl/store"
	"vl/store/sqlitedriver"
)

// row is one stored bar, kept compact in memory.
type row struct {
	T          int64
	O, H, L, C float64
	V          float64
	Src        string
	Conv       string
}

// barSet holds every readable MNQ row the replay needs, by tf then contract,
// ascending by open time. It is loaded ONCE from the database opened read-only
// and then served AS OF an instant (asOfSource), so a historical day is seeded
// and ticked from exactly the rows that had CLOSED by then.
type barSet struct {
	byTF map[string]map[string][]row // tf → contract → rows asc
	// importShifted counts the rows moved by -tf (historical_import rows still
	// close-stamped, convention epoch_floor — the m460 defect).
	importShifted map[string]int
	// importDropped counts shifted import rows that landed on a minute a
	// live/historical row of the same contract already holds (the live tape
	// wins, as in cmd/import-label-shift).
	importDropped map[string]int
}

var tfMs = map[string]int64{"1m": 60_000, "5m": 300_000, "1h": 3_600_000}

// openReadOnly opens the SQLite file strictly read-only (mode=ro): no pragma,
// no migration, no write can reach the live database through this handle.
func openReadOnly(path string) (*sql.DB, error) {
	dsn := "file:" + path + "?mode=ro"
	db, err := sqlitedriver.Open(dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// loadBars reads MNQ rows for the given tfs with the SAME filters the store's
// readers apply (store.BarHistoryStore: source not mixed / off-scale; a usable
// contract). shiftImport re-stamps the close-stamped historical_import rows to
// their open (see cmd/import-label-shift); false keeps them exactly as the live
// seed reads them today.
func loadBars(db *sql.DB, symbol string, tfs []string, shiftImport bool) (*barSet, error) {
	bs := &barSet{byTF: map[string]map[string][]row{}, importShifted: map[string]int{}, importDropped: map[string]int{}}
	for _, tf := range tfs {
		span, ok := tfMs[tf]
		if !ok {
			return nil, fmt.Errorf("unsupported tf %q", tf)
		}
		q, err := db.Query(`SELECT contract, open_time_ms, o, h, l, c, COALESCE(v,0), COALESCE(source,''), COALESCE(convention,'')
			FROM bars WHERE symbol = ? AND tf = ? AND contract != '' AND contract != ?
			AND COALESCE(source,'') NOT IN (?, ?) ORDER BY contract, open_time_ms`,
			symbol, tf, store.ContractMixed, store.BarSourceMixed, store.BarSourceOffScale)
		if err != nil {
			return nil, err
		}
		type key struct {
			c string
			t int64
		}
		got := map[key]row{}
		for q.Next() {
			var contract string
			var r row
			var o, h, l, c sql.NullFloat64
			if err := q.Scan(&contract, &r.T, &o, &h, &l, &c, &r.V, &r.Src, &r.Conv); err != nil {
				q.Close()
				return nil, err
			}
			if !o.Valid || !h.Valid || !l.Valid || !c.Valid {
				continue
			}
			r.O, r.H, r.L, r.C = o.Float64, h.Float64, l.Float64, c.Float64
			imp := r.Src == store.BarSourceHistoricalImport && r.Conv != store.ConventionEpochFloorOpen
			if shiftImport && imp {
				r.T -= span
				bs.importShifted[tf]++
			}
			k := key{contract, r.T}
			if prev, dup := got[k]; dup {
				prevImp := prev.Src == store.BarSourceHistoricalImport
				if !prevImp || (prevImp && r.Src == store.BarSourceHistoricalImport) {
					bs.importDropped[tf]++
					continue
				}
				bs.importDropped[tf]++
			}
			got[k] = r
		}
		if err := q.Err(); err != nil {
			q.Close()
			return nil, err
		}
		q.Close()
		m := map[string][]row{}
		for k, r := range got {
			m[k.c] = append(m[k.c], r)
		}
		for c := range m {
			s := m[c]
			sort.Slice(s, func(i, j int) bool { return s[i].T < s[j].T })
		}
		bs.byTF[tf] = m
	}
	return bs, nil
}

// closedBefore returns the prefix of rows whose bar had CLOSED by asOf
// (open + tf <= asOf).
func closedBefore(rows []row, span, asOf int64) []row {
	n := sort.Search(len(rows), func(i int) bool { return rows[i].T+span > asOf })
	return rows[:n]
}

func toDB(symbol, tf, contract string, rs []row) []store.BarHistoryDB {
	out := make([]store.BarHistoryDB, len(rs))
	for i, r := range rs {
		out[i] = store.BarHistoryDB{Symbol: symbol, TF: tf, Contract: contract, OpenTimeMs: r.T,
			O: r.O, H: r.H, L: r.L, C: r.C, V: r.V, Source: r.Src, Convention: r.Conv}
	}
	return out
}

// klines converts rows the way the trader does (storeBarsToKlines): CloseTime =
// open + tf − 1, every stored bar Final.
func klines(rs []row, span int64) []market.Kline {
	out := make([]market.Kline, len(rs))
	for i, r := range rs {
		out[i] = market.Kline{OpenTime: r.T, CloseTime: r.T + span - 1, Open: r.O, High: r.H, Low: r.L, Close: r.C, Volume: r.V, Final: true}
	}
	return out
}

// asOfSource is the bar set seen at one instant. It implements
// trader.MentorSeedBarSource with the semantics of store.BarHistoryStore
// (contract filter, ascending order, spans oldest-first), restricted to bars
// that had closed by AsOf.
type asOfSource struct {
	bs     *barSet
	symbol string
	AsOf   int64
	// Pin, when set, is the contract the replayed day traded on (the one with
	// the most 1m bars in its session): "current contract" reads return it, and
	// contracts that start after it are not yet part of the stitched history.
	// Without it the sparse wave-101 import snapshots of the NEXT contract
	// (MNQ 12-26 at 09-07 17:00 and 09-08/09/10 21:00Z) are the "newest row" on
	// the roll days and the replay ticks a contract nobody traded.
	Pin string
}

func (s asOfSource) rows(tf, contract string) []row {
	return closedBefore(s.bs.byTF[tf][contract], tfMs[tf], s.AsOf)
}

// currentContract is the contract of the newest closed row (store.CurrentContract).
func (s asOfSource) currentContract(tf string) string {
	if s.Pin != "" {
		return s.Pin
	}
	best, bestT := "", int64(-1)
	for c := range s.bs.byTF[tf] {
		rs := s.rows(tf, c)
		if n := len(rs); n > 0 && (rs[n-1].T > bestT || (rs[n-1].T == bestT && c > best)) {
			best, bestT = c, rs[n-1].T
		}
	}
	return best
}

func (s asOfSource) LastNBarsCurrentContract(symbol, tf string, n int) ([]store.BarHistoryDB, error) {
	if n <= 0 || symbol != s.symbol {
		return nil, nil
	}
	c := s.currentContract(tf)
	if c == "" {
		return nil, nil
	}
	rs := s.rows(tf, c)
	if len(rs) > n {
		rs = rs[len(rs)-n:]
	}
	return toDB(symbol, tf, c, rs), nil
}

func (s asOfSource) ContractSpans(symbol, tf string) ([]store.ContractSpan, error) {
	if symbol != s.symbol {
		return nil, nil
	}
	var out []store.ContractSpan
	for c := range s.bs.byTF[tf] {
		rs := s.rows(tf, c)
		if len(rs) == 0 {
			continue
		}
		out = append(out, store.ContractSpan{Contract: c, FirstMs: rs[0].T, LastMs: rs[len(rs)-1].T, N: int64(len(rs))})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FirstMs != out[j].FirstMs {
			return out[i].FirstMs < out[j].FirstMs
		}
		return out[i].Contract < out[j].Contract
	})
	if s.Pin != "" {
		for i, sp := range out {
			if sp.Contract == s.Pin {
				return out[:i+1], nil
			}
		}
	}
	return out, nil
}

func (s asOfSource) AllBarsOn(symbol, tf, contract string) ([]store.BarHistoryDB, error) {
	if symbol != s.symbol {
		return nil, nil
	}
	return toDB(symbol, tf, contract, s.rows(tf, contract)), nil
}

// window returns the newest n closed bars of the current contract as of AsOf —
// what market.FuturesBarsProvider (the NT8 bar cache, current contract) hands
// the live tick, minus the forming bar the tick drops.
func (s asOfSource) window(tf string, n int) []market.Kline {
	c := s.currentContract(tf)
	if c == "" {
		return nil
	}
	rs := s.rows(tf, c)
	if len(rs) > n {
		rs = rs[len(rs)-n:]
	}
	return klines(rs, tfMs[tf])
}

// sessionContract is the contract holding the most 1m bars in [fromMs, toMs).
func (bs *barSet) sessionContract(fromMs, toMs int64) string {
	best, bestN := "", 0
	for _, c := range sortedKeys(bs.byTF["1m"]) {
		rs := bs.byTF["1m"][c]
		lo := sort.Search(len(rs), func(i int) bool { return rs[i].T >= fromMs })
		hi := sort.Search(len(rs), func(i int) bool { return rs[i].T >= toMs })
		if n := hi - lo; n > bestN {
			best, bestN = c, n
		}
	}
	return best
}

// loadCalendar reads every stored calendar slice (read-only), keyed by date.
func loadCalendar(db *sql.DB) (map[string][]calendar.Event, error) {
	q, err := db.Query(`SELECT trade_date, events_json FROM calendar_slices`)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return map[string][]calendar.Event{}, nil
		}
		return nil, err
	}
	defer q.Close()
	out := map[string][]calendar.Event{}
	for q.Next() {
		var d, js string
		if err := q.Scan(&d, &js); err != nil {
			return nil, err
		}
		var evs []calendar.Event
		if json.Unmarshal([]byte(js), &evs) != nil {
			continue // an unreadable slice reads as missing, like mentorDayEvents
		}
		out[d] = evs
	}
	return out, q.Err()
}

// loadStrategy reads one strategy row (read-only) and parses it exactly as the
// manager does for a live trader (store.Strategy.ParseConfig). id may be a
// unique prefix (e.g. a5b7662e).
func loadStrategy(db *sql.DB, id string) (*store.StrategyConfig, string, error) {
	q, err := db.Query(`SELECT id, name, config FROM strategies WHERE id = ? OR id LIKE ?`, id, id+"%")
	if err != nil {
		return nil, "", err
	}
	defer q.Close()
	var found []store.Strategy
	for q.Next() {
		var st store.Strategy
		if err := q.Scan(&st.ID, &st.Name, &st.Config); err != nil {
			return nil, "", err
		}
		found = append(found, st)
	}
	if len(found) != 1 {
		return nil, "", fmt.Errorf("strategy %q matched %d rows (need exactly 1)", id, len(found))
	}
	cfg, err := found[0].ParseConfig()
	if err != nil {
		return nil, "", err
	}
	return cfg, found[0].ID + " (" + found[0].Name + ")", nil
}
