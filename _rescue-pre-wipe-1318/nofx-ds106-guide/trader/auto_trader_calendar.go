package trader

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"vl/calendar"
	"vl/internal/envcompat"
	"vl/kernel"
	"vl/store"
)

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
		at.logInfof("📅 calendar: live slice for %s is %s old — re-fetching for same-day corrections", tradeDate, age.Truncate(time.Minute))
	}
	if !at.lastCalFetch.IsZero() && now.Sub(at.lastCalFetch) < time.Hour {
		return // throttle outage retries
	}
	at.lastCalFetch = now

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
// the static events STORE as source=static rows. Path: env NOFX_CALENDAR_STATIC,
// else ./calendar_static_t1.json (repo-shipped template). Shape: JSON array of
// calendar.Event ({"time": RFC3339-UTC, "currency", "title", "impact":"T1"}).
// Missing/unreadable/invalid file → nil (FetchWeek reports SourceNone + warns).
func calendarStaticLoader() []calendar.Event {
	path, _ := envcompat.Env("CALENDAR_STATIC") // R5 removes
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
	var evs []calendar.Event
	fromStatic := false
	if err != nil || slice == nil || json.Unmarshal([]byte(slice.EventsJSON), &evs) != nil {
		// P0.6 fail-closed: static T1 fallback + alert instead of silent nil.
		evs = calendarStaticLoader()
		fromStatic = true
		if at.firstFor(&at.lastCalFailClosedAlert, tradeDate) {
			at.emitAlert("P0", "calendar-slice-missing",
				fmt.Sprintf("calendar-fail-closed:%s:%s", tradeDate, sess.Name),
				"Calendar slice missing",
				fmt.Sprintf("No stored calendar slice for %s — session %s is now BLACKED OUT around the static T1 fallback times (fail-closed). Fix the calendar feed; this alert repeats daily until the slice exists.",
					tradeDate, sess.Name))
		}
	}
	// W-T1-CURRENCIES (2026-09-18): the ONE split — only events in the
	// strategy's t1_currencies set (default USD) open HARD windows; the rest
	// are advisory lines the plan-write path renders (plannerT1Lines) and
	// this gate never sees. An event WITHOUT a currency fails closed to hard
	// and is named once per trade date.
	split := kernel.SplitT1(sessionPlannerEvents(evs, sess.Name), at.t1Currencies())
	windows := split.Hard
	if len(split.Uncurrencied) > 0 && at.firstFor(&at.lastT1NoCurrencyWarn, tradeDate) {
		for _, title := range split.Uncurrencied {
			at.logWarnf("⚠️ T1 event without currency treated as hard: %s (%s %s, t1_currencies=%s)",
				title, tradeDate, sess.Name, kernel.T1CurrencySetLabel(at.t1Currencies()))
		}
	}
	if fromStatic {
		at.logWarnf("📅 calendar FAIL-CLOSED: no slice for %s — using the static T1 fallback (%d window(s)) instead of zero protection",
			tradeDate, len(windows))
	}
	// F6 — widen the hard no-trade windows by the measured clock drift so the
	// red-news blackout survives a skewed clock (same decision the authoring
	// gate uses; the warn line fires once per session-day, not per cycle).
	// CLASS 145 (2026-09-17): the widening goes through the CAPPED
	// kernel.WidenCTWindows — the SAME function the plan-write path uses — so
	// a halt's or gap's feed age (a large POSITIVE "drift") can never open the
	// band by more than ClockWidenCapMinutes; the signed measurement is passed
	// so the label rule sees the sign.
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
