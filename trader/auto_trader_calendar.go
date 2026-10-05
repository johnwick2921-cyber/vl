package trader

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"vl/calendar"
	"vl/kernel"
	"vl/store"
)

// calFetchThrottle persists the calendar-fetch throttle ACROSS trader
// reconstruction (P2, CTO 2026-10-05): keyed by trader id, so an NT8 reconnect
// that rebuilds the AutoTrader (and zeroes its instance fields) does not re-hit
// the FF feed inside the 1h window. Package-level, one entry per trader id.
var calFetchThrottle sync.Map // calFetchThrottleKey(trader id, trade date) → time.Time (last fetch)

// calFetchThrottleKey scopes the throttle to (trader, trade date): a fetch
// attempt just before the date roll must never delay the NEW date's first
// fetch (DS-102 #409 review (d), P2).
func calFetchThrottleKey(traderID, tradeDate string) string { return traderID + "|" + tradeDate }

// resetCalFetchThrottleForTest clears every throttle entry of one trader.
func resetCalFetchThrottleForTest(traderID string) {
	calFetchThrottle.Range(func(k, _ any) bool {
		if ks, ok := k.(string); ok && strings.HasPrefix(ks, traderID+"|") {
			calFetchThrottle.Delete(k)
		}
		return true
	})
}

// W3 — the calendar PRODUCER (the audit's dead wire): fetch the ForexFactory
// weekly feed and store one slice per CT trade-date, so the planner's GetSlice
// read finally has data and T1 (red) events become HARD no-trade blackouts. Gated
// on day_plan → dormant by default; idempotent (SaveSliceIfAbsent); throttled.

// maybeFetchCalendar stores the week's calendar slices when the current
// trade-date's slice is missing (covers on-boot + each new day — F0: it is called
// ABOVE the session gate, so a weekend/closed-hours boot still ignites it).
// Network attempts are throttled to ≤1/hour so an outage doesn't hammer the feed.
// Every outcome logs plainly: fetched / fallback static / skip-fresh (F0.1).
func (at *AutoTrader) maybeFetchCalendar(now time.Time) {
	if !at.dayPlanEnabled() || at.store == nil {
		return
	}
	tradeDate := plannerTradeDateCT(now)
	// F0.4 (2026-08-24) — a LIVE slice is only "fresh" for 3 hours; after that
	// we re-fetch (throttled below) so SAME-DAY corrections (speaker
	// cancellations, impact changes) propagate instead of freezing the morning
	// snapshot. Past dates stay frozen (replay integrity).
	const liveFreshness = 3 * time.Hour
	staleLive := false
	var staleAge time.Duration
	if slice, _ := at.store.Calendar().GetSlice(tradeDate); slice != nil && slice.Source == string(calendar.SourceLive) {
		age := now.Sub(time.UnixMilli(slice.CreatedAt))
		if age < liveFreshness {
			// skip-fresh — only a LIVE-source slice is fresh (F0.3: a static/none
			// slice is stale and keeps re-fetching for upgrade, throttled below).
			// Logged once per trade date, not every 3-min cycle.
			if at.lastCalSkipDate != tradeDate {
				at.lastCalSkipDate = tradeDate
				at.logInfof("📅 calendar: skip-fresh — slice for %s already stored (src %s)", tradeDate, slice.Source)
			}
			return
		}
		staleLive = true
		staleAge = age
	}
	if v, ok := calFetchThrottle.Load(calFetchThrottleKey(at.id, tradeDate)); ok {
		if now.Sub(v.(time.Time)) < time.Hour {
			return // throttle (P2: persisted across trader reconstruction)
		}
	}
	calFetchThrottle.Store(calFetchThrottleKey(at.id, tradeDate), now)
	// P2 — the re-fetch line fires only when the fetch ACTUALLY runs (it used to
	// log every cycle before the throttle, reading as a 2-min hammer).
	if staleLive {
		at.logInfof("📅 calendar: live slice for %s is %s old — re-fetching for same-day corrections", tradeDate, staleAge.Truncate(time.Minute))
	}

	fetch := at.calFetch // test seam (F0); nil → live FF fetch
	if fetch == nil {
		fetch = calendar.DefaultFetch
	}
	res := calendar.FetchWeek(fetch, calendarStaticLoader)
	stored, upgraded, events := 0, 0, 0
	for date, evs := range res.Days {
		js, _ := json.Marshal(evs)
		row := &store.CalendarSliceDB{
			TradeDate: date, Source: string(res.Source), EventsJSON: string(js), CreatedAt: now.UnixMilli(),
		}
		if res.Source == calendar.SourceLive {
			// live wins over a prior static/none guess (F0.3). A frozen live row
			// still refreshes when the payload CHANGED (F0.4 — same-day
			// corrections), but stays byte-identical otherwise (replay integrity).
			if wrote, up, err := at.store.Calendar().UpsertSliceUpgrade(row); err == nil && wrote {
				stored++
				events += len(evs)
				if up {
					upgraded++
				}
			} else if err == nil {
				// C10/D2 (2026-08-25) — the past-date freeze: a corrected
				// payload for a PAST trade-date must NOT overwrite the frozen
				// replay slice. Only TODAY's slice accepts live corrections.
				todayCT := now.In(kernel.CTLocation()).Format("2006-01-02")
				if date != todayCT {
					continue
				}
				if changed, cerr := at.store.Calendar().UpdateLiveSliceIfChanged(row); cerr == nil && changed {
					at.logInfof("📅 calendar: %s slice corrected by the fresh feed", date)
				} else if cerr != nil {
					at.logWarnf("📅 calendar: live-slice refresh for %s failed: %v", date, cerr)
				}
			}
		} else if wrote, err := at.store.Calendar().SaveSliceIfAbsent(row); err == nil && wrote {
			stored++
			events += len(evs)
		}
	}
	fetched := 0
	for _, evs := range res.Days {
		fetched += len(evs)
	}
	switch {
	case res.Source == calendar.SourceLive && upgraded > 0:
		at.logInfof("📅 calendar: fetched %d events — %d day slice(s) stored, %d upgraded (src forexfactory)", fetched, stored, upgraded)
	case res.Source == calendar.SourceLive && stored == 0:
		// P3 (CTO 2026-10-05): "0 stored" here means every fetched day already has
		// a frozen forexfactory slice and the payload is unchanged — nothing to do,
		// not an error.
		at.logInfof("📅 calendar: fetched %d events — all frozen/unchanged, nothing to store (src forexfactory)", fetched)
	case res.Source == calendar.SourceLive:
		at.logInfof("📅 calendar: fetched %d events — %d day slice(s) stored (src forexfactory)", fetched, stored)
	case stored > 0:
		at.logWarnf("📅 calendar: fallback static — stored %d day slice(s), %d events (%s)", stored, fetched, res.Warning)
	default:
		at.logWarnf("📅 calendar: fetch failed, no static rows to store (%s) — retry in ≤1h", res.Warning)
	}
}

// calendarStaticLoader loads the owner-editable static T1 fallback file (F0.2)
// so blackout coverage never depends on feed availability: on a feed 404/outage
// the static events STORE as source=static rows. Path: env VL_CALENDAR_STATIC,
// else ./calendar_static_t1.json (repo-shipped template). Shape: JSON array of
// calendar.Event ({"time": RFC3339-UTC, "currency", "title", "impact":"T1"}).
// Missing/unreadable/invalid file → nil (FetchWeek reports SourceNone + warns).
func calendarStaticLoader() []calendar.Event {
	path := os.Getenv("VL_CALENDAR_STATIC") // R5: VL_ only
	if path == "" {
		path = "calendar_static_t1.json"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var evs []calendar.Event
	if err := json.Unmarshal(raw, &evs); err != nil {
		return nil
	}
	// NEWS-HYGIENE AMEND (2026-08-30) — the static file may carry NOTE entries
	// (holidays, contract rolls, quad-witching) as owner annotations. They
	// never gate and never reach the prompt: drop them here.
	out := evs[:0]
	for _, e := range evs {
		if strings.EqualFold(strings.TrimSpace(string(e.Impact)), "note") || strings.TrimSpace(string(e.Impact)) == "" {
			continue
		}
		out = append(out, e)
	}
	return out
}

// currentT1Windows returns the red-news HARD no-trade windows for the active
// session at `now`.
//
// P0.6 (2026-08-19) — FAIL-CLOSED: a missing/undecodable slice used to return
// nil (entries allowed, blackouts silently gone). Everything else in this
// system fails closed; now this does too — the owner-editable static T1
// fallback supplies the windows and a P0 alert fires (once per session-day).
func (at *AutoTrader) currentT1Windows(now time.Time) []kernel.CTWindow {
	if at.store == nil {
		return nil
	}
	reg := at.sessionRegistry(now) // W8
	sess, ok := reg.ActiveSession(now)
	if !ok {
		return nil
	}
	return at.t1WindowsFor(plannerTradeDateCT(now), sess)
}

// t1WindowsFor is currentT1Windows for an EXPLICIT session and trade date, so
// the plan-write path can stamp the card with the SAME windows the entry gate
// will enforce (no-trade-band wave, 2026-09-02). A read authors ASIA while NY
// is still the active session, so "the session at now" is the wrong key there.
func (at *AutoTrader) t1WindowsFor(tradeDate string, sess *kernel.SessionDef) []kernel.CTWindow {
	if at.store == nil || sess == nil {
		return nil
	}
	slice, err := at.store.Calendar().GetSlice(tradeDate)
	if err == nil && slice != nil {
		var evs []calendar.Event
		if json.Unmarshal([]byte(slice.EventsJSON), &evs) == nil {
			// W-T1-CURRENCIES (2026-09-18): the ONE split — only events in the
			// strategy's t1_currencies set (default USD) open HARD windows; the
			// rest are advisory lines the plan-write path renders (plannerT1Lines)
			// and this gate never sees. An event WITHOUT a currency fails closed
			// to hard and is named once per trade date.
			split := kernel.SplitT1(sessionPlannerEvents(evs, sess.Name), at.t1Currencies())
			if len(split.Uncurrencied) > 0 && at.firstFor(&at.lastT1NoCurrencyWarn, tradeDate) {
				for _, title := range split.Uncurrencied {
					at.logWarnf("⚠️ T1 event without currency treated as hard: %s (%s %s, t1_currencies=%s)",
						title, tradeDate, sess.Name, kernel.T1CurrencySetLabel(at.t1Currencies()))
				}
			}
			return at.widenT1Windows(split.Hard, tradeDate, sess)
		}
	}
	// P0.6 fail-closed: no slice (or undecodable) → date-aware fallback.
	if at.firstFor(&at.lastCalFailClosedAlert, tradeDate) {
		at.emitAlert("P0", "calendar-slice-missing",
			fmt.Sprintf("calendar-fail-closed:%s:%s", tradeDate, sess.Name),
			"Calendar slice missing",
			fmt.Sprintf("No stored calendar slice for %s — session %s is now BLACKED OUT around the fallback times (fail-closed). Fix the calendar feed; this alert repeats daily until the slice exists.",
				tradeDate, sess.Name))
	}
	return at.widenT1Windows(at.calendarFallbackWindows(tradeDate, sess), tradeDate, sess)
}

// calendarFallbackWindows is the DATE-AWARE static fallback (P1, CTO 2026-10-05):
// the frozen static file must NEVER apply another week's HH:MM schedule to a
// date it does not cover. For a trade date with no live slice:
//
//	(a) a date the static file COVERS → that date's own static events (the
//	    existing ±T1BlackoutMinutes split);
//	(b) a non-trading date (CME closed — Saturday, or Sunday under the 17:00 CT
//	    session-day mapping in kernel/mentor/day_gate.go tradingDayKey, or a
//	    holiday) → NO windows, logged as such;
//	(c) any other uncovered TRADING date → ONE conservative window: the standard
//	    07:30 CT T1 print [07:20, 07:35) (mentorNewsPreWindow / mentorNewsPostWindow),
//	    logged as such — not September's list.
func (at *AutoTrader) calendarFallbackWindows(tradeDate string, sess *kernel.SessionDef) []kernel.CTWindow {
	loc := kernel.CTLocation()
	// (a) covered: only that date's static events.
	static := calendarStaticLoader()
	var day []calendar.Event
	for _, e := range static {
		if e.Time.In(loc).Format("2006-01-02") == tradeDate {
			day = append(day, e)
		}
	}
	if len(day) > 0 {
		split := kernel.SplitT1(sessionPlannerEvents(day, sess.Name), at.t1Currencies())
		at.logWarnf("📅 calendar FAIL-CLOSED: no slice for %s — the static file covers this date; using its %d event(s) (%d window(s))",
			tradeDate, len(day), len(split.Hard))
		return split.Hard
	}
	// (b) non-trading date → no windows.
	if d, err := time.ParseInLocation("2006-01-02", tradeDate, loc); err == nil {
		noon := time.Date(d.Year(), d.Month(), d.Day(), 12, 0, 0, 0, loc)
		if closed, reason := kernel.CMEClosedReason(noon); closed {
			at.logInfof("📅 calendar FAIL-CLOSED: no slice for %s and it is a non-trading day (%s) — no news windows",
				tradeDate, reason)
			return nil
		}
	}
	// (c) uncovered trading date → ONE conservative 07:30 CT print window.
	at.logWarnf("📅 calendar FAIL-CLOSED: no slice for %s and the static file does not cover it — holding the standard 07:30 CT T1 print window (07:20–07:35) instead of the static schedule",
		tradeDate)
	return []kernel.CTWindow{{Start: 7*60 + 20, End: 7*60 + 35, Label: "standard 07:30 CT T1 print (fallback, uncovered date)"}}
}

// widenT1Windows is the F6 clock-drift widening shared by the live and the
// fallback paths (CLASS 145: capped by kernel.ClockWidenCapMinutes).
func (at *AutoTrader) widenT1Windows(windows []kernel.CTWindow, tradeDate string, sess *kernel.SessionDef) []kernel.CTWindow {
	if drift, ok := clockHoldDriftFn(at.futuresSymbol()); ok {
		if _, widen := kernel.ClockHoldDecision(drift, true, kernel.ClockWarnMs(), kernel.C2ToleranceMs()); widen > 0 {
			windows = kernel.WidenCTWindows(windows, drift)
			if at.firstFor(&at.lastClockWidenLog, tradeDate) {
				at.logWarnf("🕰 clock-hold: T1 no-trade windows widened by %dm (|drift| %dms, cap %dm) for %s %s (F6)",
					kernel.ClockWidenMinutes(drift), widen, kernel.ClockWidenCapMinutes, tradeDate, sess.Name)
				if note := kernel.ClockDriftStaleNote(drift); note != "" {
					at.logWarnf("🕰 clock-hold: %s — %s %s (CLASS 145)", note, tradeDate, sess.Name)
				}
			}
		}
	}
	return windows
}

// sessionPlannerEvents maps stored calendar.Event → kernel.PlannerCalendarEvent
// (session-sliced, CT time). Shared by the gate + the planner input + the plan
// no-trade injection so all three agree.
func sessionPlannerEvents(evs []calendar.Event, session string) []kernel.PlannerCalendarEvent {
	loc := kernel.CTLocation()
	var out []kernel.PlannerCalendarEvent
	for _, e := range calendar.EventsForSession(evs, session) {
		out = append(out, kernel.PlannerCalendarEvent{
			TimeCT: e.Time.In(loc).Format("15:04"), Currency: e.Currency,
			Title: e.Title, Impact: string(e.Impact),
		})
	}
	return out
}

// t1Currencies is the trader's read of the W-T1-CURRENCIES knob through the
// ONE resolver (store.DayPlanConfig.T1CurrenciesFor): nil config → ["USD"].
// Every T1 consumer on this trader — the arm gate (t1WindowsFor), the plan
// write (plannerT1Lines + the machine no-trade band), the planner prompt input
// and the fade facts — reads this, so they cannot disagree on which red events
// gate.
func (at *AutoTrader) t1Currencies() []string {
	return at.dayPlanCfg().T1CurrenciesFor()
}
