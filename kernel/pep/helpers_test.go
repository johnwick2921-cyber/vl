package pep

import (
	"fmt"
	"testing"
	"time"

	"vl/market"
)

// ctms is the epoch ms of "YYYY-MM-DD HH:MM" in America/Chicago (the zone the
// owner's PDF quotes: "08:30 Houston").
func ctms(t testing.TB, date, hhmm string) int64 {
	t.Helper()
	loc, err := CTLocation()
	if err != nil {
		t.Fatalf("zoneinfo: %v", err)
	}
	tt, err := time.ParseInLocation("2006-01-02 15:04", date+" "+hhmm, loc)
	if err != nil {
		t.Fatalf("parse %s %s: %v", date, hhmm, err)
	}
	return tt.UnixMilli()
}

type ohlc struct{ o, h, l, c float64 }

func flat(p float64) ohlc { return ohlc{p, p, p, p} }

func kl(open int64, b ohlc) market.Kline {
	return market.Kline{OpenTime: open, CloseTime: open + 59_999, Open: b.o, High: b.h, Low: b.l, Close: b.c, Volume: 1}
}

// session builds 1m bars for [from, to) CT of one date: every minute flat at
// price, except the minutes named in over ("HH:MM") and the minutes in skip.
func session(t testing.TB, date, from, to string, price float64, over map[string]ohlc, skip ...string) []market.Kline {
	t.Helper()
	skipSet := map[string]bool{}
	for _, s := range skip {
		skipSet[s] = true
	}
	loc, _ := CTLocation()
	var out []market.Kline
	for ms := ctms(t, date, from); ms < ctms(t, date, to); ms += minuteMs {
		hh := time.UnixMilli(ms).In(loc).Format("15:04")
		if skipSet[hh] {
			continue
		}
		b, ok := over[hh]
		if !ok {
			b = flat(price)
		}
		out = append(out, kl(ms, b))
	}
	return out
}

// join concatenates ascending runs.
func join(runs ...[]market.Kline) []market.Kline {
	var out []market.Kline
	for _, r := range runs {
		out = append(out, r...)
	}
	return out
}

// nowAfter is the instant right after the bar opening at hhmm closed.
func nowAfter(t testing.TB, date, hhmm string) int64 {
	return ctms(t, date, hhmm) + minuteMs
}

func cfgA() Config {
	c := DefaultConfig()
	c.Style = StyleA
	return c
}

func cfgE() Config {
	c := DefaultConfig()
	c.Style = StyleE
	return c
}

func mustOne(t testing.TB, r Result) Intent {
	t.Helper()
	if len(r.Intents) != 1 {
		t.Fatalf("want exactly one intent, got %d (refusals %+v)", len(r.Intents), r.Refusals)
	}
	return r.Intents[0]
}

func wantRefusal(t testing.TB, r Result, code string, sev Severity) {
	t.Helper()
	if len(r.Intents) != 0 {
		t.Fatalf("want refusal %s, got intent %+v", code, r.Intents)
	}
	for _, f := range r.Refusals {
		if f.Code == code {
			if f.Severity != sev {
				t.Fatalf("refusal %s severity=%s, want %s (%s)", code, f.Severity, sev, f.Detail)
			}
			return
		}
	}
	t.Fatalf("want refusal %s, got %+v", code, r.Refusals)
}

func near(t testing.TB, what string, got, want float64) {
	t.Helper()
	if d := got - want; d > 1e-9 || d < -1e-9 {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
}

// aDay is a style-A fixture: a flat 25020 tape with the 08:30-08:34 box
// 25000..25040 (size 40) and the named later bars.
func aDay(t testing.TB, date string, over map[string]ohlc, skip ...string) []market.Kline {
	o := map[string]ohlc{
		"08:30": {25010, 25040, 25000, 25020},
		"08:31": {25020, 25035, 25005, 25030},
		"08:32": {25030, 25036, 25010, 25015},
		"08:33": {25015, 25030, 25008, 25025},
		"08:34": {25025, 25034, 25012, 25020},
	}
	for k, v := range over {
		o[k] = v
	}
	return session(t, date, "07:00", "15:10", 25020, o, skip...)
}

var _ = fmt.Sprintf
