package mentor

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"vl/market"
)

const (
	r6RTHStart = 8*60 + 30
	r6RTHEnd   = 15 * 60
)

type r6GoArm struct {
	key          armKey
	armID        string
	reason       string
	action       Action
	placedDay    string
	placedRTH    bool
	status       string
	outcomeMin   string
	cancelReason string
}

type r6MinuteEvents struct {
	placed    int
	cancelled int
	details   []string
}

type r6GoOrderCollector struct {
	active             map[string]*r6GoArm
	seenIDs            map[string]bool
	rthOrders          []*r6GoArm
	rthCancellations   []*r6GoArm
	carriedEvents      []string
	carriedLegRefusals []string
	eventsByMinute     map[string]*r6MinuteEvents
	problems           []string
}

type r6LegSnapshot struct {
	extreme float64
	entries int
	stopped bool
}

func parityReplayDir() string {
	if dir := strings.TrimSpace(os.Getenv("PARITY_REPLAY_DIR")); dir != "" {
		return dir
	}
	return frozenDir
}

func r6InRTHMinute(minute int) bool {
	return minute >= r6RTHStart && minute < r6RTHEnd
}

func r6IsRTHOrderMinute(minCT string) (bool, error) {
	t, err := time.ParseInLocation("2006-01-02 15:04", strings.TrimSpace(minCT), ctime())
	if err != nil {
		return false, fmt.Errorf("invalid order minute %q: %w", minCT, err)
	}
	return r6InRTHMinute(t.Hour()*60 + t.Minute()), nil
}

func r6ReplayOrders(t *testing.T, day string, orders []orderRow) []orderRow {
	t.Helper()
	var out []orderRow
	for _, order := range orders {
		if order.day != day {
			continue
		}
		inRTH, err := r6IsRTHOrderMinute(order.placedMinCT)
		if err != nil {
			t.Fatalf("replay order %q: %v", order.oid, err)
		}
		if inRTH {
			out = append(out, order)
		}
	}
	return out
}

func newR6GoOrderCollector() *r6GoOrderCollector {
	return &r6GoOrderCollector{
		active:         map[string]*r6GoArm{},
		seenIDs:        map[string]bool{},
		eventsByMinute: map[string]*r6MinuteEvents{},
	}
}

func (c *r6GoOrderCollector) minuteEvents(minute string) *r6MinuteEvents {
	if c.eventsByMinute[minute] == nil {
		c.eventsByMinute[minute] = &r6MinuteEvents{}
	}
	return c.eventsByMinute[minute]
}

func (c *r6GoOrderCollector) addDetail(minute, detail string) {
	events := c.minuteEvents(minute)
	events.details = append(events.details, detail)
}

func (c *r6GoOrderCollector) placedCountAt(minute string) int {
	if events := c.eventsByMinute[minute]; events != nil {
		return events.placed
	}
	return 0
}

func (c *r6GoOrderCollector) cancelledCountAt(minute string) int {
	if events := c.eventsByMinute[minute]; events != nil {
		return events.cancelled
	}
	return 0
}

func (c *r6GoOrderCollector) detailsAt(minute string) string {
	if events := c.eventsByMinute[minute]; events != nil {
		return strings.Join(events.details, " | ")
	}
	return ""
}

func r6ArmFillsOnBar(in Intent, cur market.Kline) bool {
	touched := in.Side == SideLong && cur.High >= in.Price ||
		in.Side == SideShort && cur.Low <= in.Price
	if !touched {
		return false
	}
	if in.Action != PlaceStopLimitEntry {
		return true
	}
	if in.Side == SideLong {
		return cur.Open <= in.Price
	}
	return cur.Open >= in.Price
}

func (c *r6GoOrderCollector) consume(cur market.Kline, intents []Intent, targetDay string) {
	minute := minCTFromBar(cur)
	local := time.UnixMilli(cur.OpenTime).In(ctime())
	day := local.Format("2006-01-02")
	minuteOfDay := local.Hour()*60 + local.Minute()
	inTargetRTH := day == targetDay && r6InRTHMinute(minuteOfDay)

	ids := make([]string, 0, len(c.active))
	for id := range c.active {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		arm := c.active[id]
		probe := Intent{Action: arm.action, Side: sideFromNum(arm.key.side), Price: arm.key.entry}
		if !r6ArmFillsOnBar(probe, cur) {
			continue
		}
		delete(c.active, id)
		arm.status = "filled"
		arm.outcomeMin = minute
		if inTargetRTH {
			detail := fmt.Sprintf("FILLED ArmID=%q reason=%q", arm.armID, arm.reason)
			if r6WasPlacedPreRTH(arm, targetDay) {
				event := fmt.Sprintf("FILLED ArmID=%q placed=%s reason=%q",
					arm.armID, arm.key.placedMin, arm.reason)
				c.carriedEvents = append(c.carriedEvents, event)
				detail = "CARRIED " + detail
			}
			c.addDetail(minute, detail)
		}
	}

	for _, in := range intents {
		switch in.Action {
		case PlaceStopEntry, PlaceStopLimitEntry:
			setup := setupFor(in)
			key := armKey{
				setup:     setup,
				side:      replaySideNum(in.Side),
				entry:     in.Price,
				stop:      in.Stop,
				placedMin: minute,
			}
			arm := &r6GoArm{
				key:       key,
				armID:     in.ArmID,
				reason:    in.Reason,
				action:    in.Action,
				placedDay: day,
				placedRTH: inTargetRTH,
				status:    "working",
			}
			if setup == "UNKNOWN" {
				c.problems = append(c.problems,
					fmt.Sprintf("unclassified placement ArmID=%q reason=%q at %s",
						in.ArmID, in.Reason, minute))
			}
			if in.ArmID == "" {
				c.problems = append(c.problems,
					fmt.Sprintf("placement has no ArmID reason=%q at %s", in.Reason, minute))
			} else if c.seenIDs[in.ArmID] {
				c.problems = append(c.problems,
					fmt.Sprintf("duplicate placement ArmID=%q at %s", in.ArmID, minute))
			} else {
				c.seenIDs[in.ArmID] = true
				c.active[in.ArmID] = arm
			}
			if inTargetRTH {
				c.rthOrders = append(c.rthOrders, arm)
				events := c.minuteEvents(minute)
				events.placed++
				c.addDetail(minute, fmt.Sprintf("PLACED ArmID=%q reason=%q",
					in.ArmID, in.Reason))
			}
		case CancelArm:
			arm := c.active[in.ArmID]
			if in.ArmID == "" || arm == nil {
				continue
			}
			delete(c.active, in.ArmID)
			arm.status = "cancelled"
			arm.outcomeMin = minute
			arm.cancelReason = in.Reason
			if r6WasPlacedPreRTH(arm, targetDay) {
				if inTargetRTH {
					c.carriedEvents = append(c.carriedEvents, fmt.Sprintf(
						"CANCELLED ArmID=%q placed=%s place_reason=%q cancel_reason=%q",
						arm.armID, arm.key.placedMin, arm.reason, in.Reason))
					c.addDetail(minute, fmt.Sprintf(
						"CARRIED CANCELLED ArmID=%q reason=%q", arm.armID, in.Reason))
				}
				continue
			}
			if !r6WasPlacedInTargetRTH(arm, targetDay) {
				continue
			}
			c.rthCancellations = append(c.rthCancellations, arm)
			if inTargetRTH {
				events := c.minuteEvents(minute)
				events.cancelled++
				c.addDetail(minute, fmt.Sprintf(
					"CANCELLED ArmID=%q place_reason=%q cancel_reason=%q",
					arm.armID, arm.reason, in.Reason))
			}
		}
	}
}

func sideFromNum(side int) Side {
	if side > 0 {
		return SideLong
	}
	return SideShort
}

func r6WasPlacedInTargetRTH(arm *r6GoArm, targetDay string) bool {
	return arm.placedDay == targetDay && arm.placedRTH
}

func r6WasPlacedPreRTH(arm *r6GoArm, targetDay string) bool {
	return arm.placedDay == targetDay && !arm.placedRTH
}

func copyR6Counts(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for key, count := range in {
		out[key] = count
	}
	return out
}

func snapshotR6Legs(limits Limits) map[Side]r6LegSnapshot {
	out := map[Side]r6LegSnapshot{}
	for side, leg := range map[Side]*Leg{
		SideLong:  limits.Long,
		SideShort: limits.Short,
	} {
		if leg != nil && (leg.Entries > 0 || leg.Stopped) {
			out[side] = r6LegSnapshot{
				extreme: leg.Extreme,
				entries: leg.Entries,
				stopped: leg.Stopped,
			}
		}
	}
	return out
}

func (c *r6GoOrderCollector) collectCarriedLegRefusals(
	minute string,
	before, after map[string]int,
	preRTH, current map[Side]r6LegSnapshot,
) {
	reasons := make([]string, 0, len(after))
	for reason := range after {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	for _, reason := range reasons {
		delta := after[reason] - before[reason]
		if delta <= 0 || !strings.HasPrefix(reason, "leg_budget_") {
			continue
		}
		state := r6CarriedLegCause(reason, preRTH, current)
		if state == "" {
			continue
		}
		for i := 0; i < delta; i++ {
			event := fmt.Sprintf("%s %s; pre-RTH %s", minute, reason, state)
			c.carriedLegRefusals = append(c.carriedLegRefusals, event)
			c.addDetail(minute, "CARRIED LEG REFUSAL "+reason+" ("+state+")")
		}
	}
}

func r6CarriedLegCause(reason string, preRTH, current map[Side]r6LegSnapshot) string {
	var causes []string
	for _, side := range []Side{SideLong, SideShort} {
		before, ok := preRTH[side]
		if !ok {
			continue
		}
		now, ok := current[side]
		if !ok || now.extreme != before.extreme {
			continue
		}
		caused := false
		switch reason {
		case "leg_budget_stopped":
			caused = before.stopped && now.stopped
		case "leg_budget_second_phl":
			caused = before.entries >= 1 && now.entries >= 1
		case "leg_budget_full":
			caused = before.entries >= 2 && now.entries >= 2
		}
		if caused {
			causes = append(causes, fmt.Sprintf("%s extreme=%s entries=%d stopped=%t",
				side, fnum(before.extreme), before.entries, before.stopped))
		}
	}
	return strings.Join(causes, "; ")
}

func compareR6Orders(
	t *testing.T,
	dayName, day string,
	goOrders, goCancellations []*r6GoArm,
	replayOrders []orderRow,
) bool {
	t.Helper()
	failed := false
	for _, class := range []string{"INTRADAY", "SWING4H"} {
		goByKey := map[string][]*r6GoArm{}
		replayByKey := map[string][]orderRow{}
		for _, order := range goOrders {
			if parityArmClass(order.key.setup) == class {
				key := armKeyString(order.key)
				goByKey[key] = append(goByKey[key], order)
			}
		}
		for _, order := range replayOrders {
			if parityArmClass(order.setup) == class {
				key := armKeyString(armKey{
					setup: order.setup, side: order.side, entry: order.entry,
					stop: order.stop, placedMin: order.placedMinCT,
				})
				replayByKey[key] = append(replayByKey[key], order)
			}
		}

		keys := map[string]bool{}
		for key := range goByKey {
			keys[key] = true
		}
		for key := range replayByKey {
			keys[key] = true
		}
		orderedKeys := make([]string, 0, len(keys))
		for key := range keys {
			orderedKeys = append(orderedKeys, key)
		}
		sort.Strings(orderedKeys)

		matched, goOnly, replayOnly := 0, 0, 0
		var goOnlyDetails, replayOnlyDetails []string
		for _, key := range orderedKeys {
			gos, replays := goByKey[key], replayByKey[key]
			n := min(len(gos), len(replays))
			matched += n
			for _, order := range gos[n:] {
				goOnly++
				if len(goOnlyDetails) < 5 {
					goOnlyDetails = append(goOnlyDetails,
						fmt.Sprintf("%s ArmID=%q reason=%q", key, order.armID, order.reason))
				}
			}
			for _, order := range replays[n:] {
				replayOnly++
				if len(replayOnlyDetails) < 5 {
					replayOnlyDetails = append(replayOnlyDetails,
						fmt.Sprintf("%s oid=%q cancel_reason=%q", key, order.oid, order.cancelReason))
				}
			}
		}
		t.Logf("%s RTH ORDERS %-8s %s: matched=%d go-only=%d replay-only=%d",
			class, dayName, day, matched, goOnly, replayOnly)
		if goOnly > 0 {
			t.Errorf("%s %s Go-only RTH orders (%d), first: %s",
				dayName, class, goOnly, strings.Join(goOnlyDetails, " | "))
			failed = true
		}
		if replayOnly > 0 {
			t.Errorf("%s %s replay-only RTH orders (%d), first: %s",
				dayName, class, replayOnly, strings.Join(replayOnlyDetails, " | "))
			failed = true
		}

		goCancelCount := 0
		for _, order := range goCancellations {
			if parityArmClass(order.key.setup) == class {
				goCancelCount++
			}
		}
		replayCancelCount := 0
		for _, order := range replayOrders {
			if parityArmClass(order.setup) == class &&
				strings.EqualFold(order.outcome, "cancelled") {
				replayCancelCount++
			}
		}
		t.Logf("%s RTH CANCELS %-8s %s: Go=%d replay=%d",
			class, dayName, day, goCancelCount, replayCancelCount)
		if goCancelCount != replayCancelCount {
			t.Errorf("%s %s real-order cancellations differ: Go=%d replay=%d",
				dayName, class, goCancelCount, replayCancelCount)
			failed = true
		}
	}
	return failed
}

func r6TestBar(day time.Time, open, high, low, close float64) market.Kline {
	openTime := day.UnixMilli()
	return market.Kline{
		OpenTime:  openTime,
		CloseTime: openTime + 59_999,
		Open:      open,
		High:      high,
		Low:       low,
		Close:     close,
	}
}

func TestR6OrderCollectorFillPrecedesCloseCancel(t *testing.T) {
	day := "2025-05-07"
	collector := newR6GoOrderCollector()
	placeAt := time.Date(2025, 5, 7, 8, 35, 0, 0, ctime())
	collector.consume(r6TestBar(placeAt, 99.5, 99.75, 99.0, 99.5),
		[]Intent{{
			Action: PlaceStopLimitEntry, ArmID: "isb-1", Reason: "ISB: test",
			Side: SideLong, Price: 100, Stop: 99, Target: 104,
		}}, day)

	fillAt := time.Date(2025, 5, 7, 8, 36, 0, 0, ctime())
	collector.consume(r6TestBar(fillAt, 99.5, 100.25, 98.5, 99.0),
		[]Intent{{Action: CancelArm, ArmID: "isb-1", Reason: "body left I1"}}, day)
	if len(collector.rthCancellations) != 0 {
		t.Fatalf("fill-first bar counted %d cancellation(s), want none", len(collector.rthCancellations))
	}
	if len(collector.rthOrders) != 1 || collector.rthOrders[0].status != "filled" {
		t.Fatalf("order state = %+v, want one filled RTH order", collector.rthOrders)
	}
}

func TestR6OrderCollectorCountsOnlyRealPlacedOrderCancellations(t *testing.T) {
	day := "2025-05-07"
	collector := newR6GoOrderCollector()
	placeAt := time.Date(2025, 5, 7, 8, 35, 0, 0, ctime())
	collector.consume(r6TestBar(placeAt, 99.0, 99.5, 98.5, 99.0),
		[]Intent{{
			Action: PlaceStopEntry, ArmID: "lvl-1", Reason: "PHL/PLH test",
			Side: SideLong, Price: 100, Stop: 98, Target: 104,
		}}, day)
	cancelAt := time.Date(2025, 5, 7, 8, 36, 0, 0, ctime())
	collector.consume(r6TestBar(cancelAt, 99.0, 99.5, 98.5, 99.0),
		[]Intent{
			{Action: CancelArm, Reason: "signal without order"},
			{Action: CancelArm, ArmID: "lvl-1", Reason: "close through"},
		}, day)
	if len(collector.rthCancellations) != 1 ||
		collector.rthCancellations[0].cancelReason != "close through" {
		t.Fatalf("real-order cancellations = %+v, want only lvl-1", collector.rthCancellations)
	}
	if got := collector.cancelledCountAt(minCTFromBar(r6TestBar(cancelAt, 99, 99.5, 98.5, 99))); got != 1 {
		t.Fatalf("RTH cancel count = %d, want 1", got)
	}
}

func TestR6RTHOrderCancellationAtCloseIsComparedButNotOnRTHStateRow(t *testing.T) {
	day := "2025-05-07"
	collector := newR6GoOrderCollector()
	placeAt := time.Date(2025, 5, 7, 14, 59, 0, 0, ctime())
	collector.consume(r6TestBar(placeAt, 99.0, 99.5, 98.5, 99.0),
		[]Intent{{
			Action: PlaceStopEntry, ArmID: "lvl-close", Reason: "PHL/PLH test",
			Side: SideLong, Price: 100, Stop: 98, Target: 104,
		}}, day)
	closeAt := time.Date(2025, 5, 7, 15, 0, 0, 0, ctime())
	collector.consume(r6TestBar(closeAt, 99.0, 99.5, 98.5, 99.0),
		[]Intent{{Action: CancelArm, ArmID: "lvl-close", Reason: "RTH window ended"}}, day)
	if len(collector.rthCancellations) != 1 {
		t.Fatalf("RTH-placed order cancellations = %d, want 1", len(collector.rthCancellations))
	}
	if got := collector.cancelledCountAt(minCTFromBar(r6TestBar(closeAt, 99, 99.5, 98.5, 99))); got != 0 {
		t.Fatalf("15:00 state-row cancel count = %d, want 0", got)
	}
}

func TestR6OrderCollectorListsPreRTHFillsAsCarried(t *testing.T) {
	day := "2025-05-07"
	collector := newR6GoOrderCollector()
	placeAt := time.Date(2025, 5, 7, 8, 29, 0, 0, ctime())
	collector.consume(r6TestBar(placeAt, 99.0, 99.5, 98.5, 99.0),
		[]Intent{{
			Action: PlaceStopLimitEntry, ArmID: "preopen-1", Reason: "ISB: preopen",
			Side: SideLong, Price: 100, Stop: 98, Target: 104,
		}}, day)
	fillAt := time.Date(2025, 5, 7, 8, 30, 0, 0, ctime())
	collector.consume(r6TestBar(fillAt, 99.5, 100.25, 99.0, 100.0), nil, day)
	if len(collector.rthOrders) != 0 || len(collector.rthCancellations) != 0 {
		t.Fatalf("pre-RTH order leaked into RTH diff: orders=%d cancels=%d",
			len(collector.rthOrders), len(collector.rthCancellations))
	}
	if len(collector.carriedEvents) != 1 ||
		!strings.Contains(collector.carriedEvents[0], "FILLED ArmID=\"preopen-1\"") {
		t.Fatalf("carried events = %v", collector.carriedEvents)
	}
}

func TestR6OrderFilterUsesInclusiveRTHMinuteBounds(t *testing.T) {
	for _, tc := range []struct {
		minute string
		want   bool
	}{
		{"2025-05-07 08:29", false},
		{"2025-05-07 08:30", true},
		{"2025-05-07 14:59", true},
		{"2025-05-07 15:00", false},
	} {
		got, err := r6IsRTHOrderMinute(tc.minute)
		if err != nil || got != tc.want {
			t.Errorf("r6IsRTHOrderMinute(%q) = %t, %v; want %t",
				tc.minute, got, err, tc.want)
		}
	}
	if _, err := r6IsRTHOrderMinute("not-a-minute"); err == nil {
		t.Fatal("malformed replay order timestamp accepted")
	}
}

func TestR6CarriedLegRefusalRequiresPreRTHState(t *testing.T) {
	pre := map[Side]r6LegSnapshot{
		SideShort: {extreme: 23000, entries: 1, stopped: true},
	}
	current := map[Side]r6LegSnapshot{
		SideShort: {extreme: 23000, entries: 1, stopped: true},
	}
	if got := r6CarriedLegCause("leg_budget_stopped", pre, current); got == "" {
		t.Fatal("pre-RTH stopped leg not classified as carried")
	}
	if got := r6CarriedLegCause("leg_budget_second_phl", pre, current); got == "" {
		t.Fatal("pre-RTH entry budget not classified as carried")
	}
	current[SideShort] = r6LegSnapshot{extreme: 22900, entries: 1, stopped: true}
	if got := r6CarriedLegCause("leg_budget_stopped", pre, current); got != "" {
		t.Fatalf("new leg inherited old carry: %s", got)
	}
}
