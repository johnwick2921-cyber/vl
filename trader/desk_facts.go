package trader

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"vl/kernel"
	"vl/market"
	nt "vl/provider/ninjatrader"
	"vl/store"
)

// ── THE DESK STRIP (2026-09-06) ──────────────────────────────────────────────
//
// One row per fact the owner needs during a session, computed HERE because the
// facts live behind unexported fields, and rendered by GET /api/desk. This file
// READS. It writes nothing, gates nothing, and decides nothing (A31).
//
// THE STRIP'S OWN LAW — every rule below exists because a surface already broke
// it somewhere in this system:
//
//  1. NOTHING RENDERS UNDATED. Every line carries the as-of instant of the
//     newest input it used, and its age. A number without a time is a number
//     you cannot act on.
//  2. UNKNOWN IS A FIRST-CLASS VALUE, and it carries its reason. An uncomputed
//     value is never 0, never "-", never the last known value (A24). The
//     dashboard's own history is why: a static SYSTEM_STATUS once read green
//     through 113 minutes of silence.
//  3. NO GREEN WORD FOR A COMPOUND QUESTION. Line 1 never collapses to "OK":
//     it names process, feed, link and book separately, because "ONLINE" today
//     means only that the HTTP process answered.
//  4. A STALE SOURCE RENDERS AMBER WITH ITS AGE — never the last value
//     silently. Staleness is information, not an inconvenience.
//  5. A LEDGER PRICE IS NEVER SHOWN AS THE BROKER'S. Arm 35's ledger stop read
//     29351.628 while NT8 had accepted 29355 — 3.371527 points. PROTECTION
//     shows the ACCEPTED price or it shows UNKNOWN; DRIFT shows the difference
//     when the two disagree.
//  6. NUMBERS A TRADER ACTS ON APPEAR IN $ AND IN POINTS/TICKS.
//
// CLASS 23 / A10: a failure computing one line renders that line UNKNOWN. It
// never blanks the strip and never reaches the trading loop.

// DeskLine is one strip row, rendered server-side so the browser cannot invent
// a value the engine did not compute.
type DeskLine struct {
	N     int    `json:"n"`
	Key   string `json:"key"`
	Label string `json:"label"`
	// Text is the row as the owner reads it.
	Text string `json:"text"`
	// State: ok | unknown | stale | flat. Never a bare colour.
	State string `json:"state"`
	// Unit of the primary number, when the row has one.
	Unit string `json:"unit,omitempty"`
	// Source names the table, endpoint or frame the row was computed from.
	Source string `json:"source"`
	// AsOfMs is the newest input's instant; 0 means the row has no dated input.
	AsOfMs int64 `json:"as_of_ms"`
	AgeMs  int64 `json:"age_ms"`
	// Verified is true only when every input was read from its authoritative
	// source and was fresh enough to believe.
	Verified bool `json:"verified"`
	// Reason is REQUIRED whenever State is unknown or stale.
	Reason string `json:"reason,omitempty"`
}

// DeskStrip is the whole surface, one request.
type DeskStrip struct {
	TraderID     string     `json:"trader_id"`
	GeneratedMs  int64      `json:"generated_at_ms"`
	CadenceMs    int        `json:"cadence_ms"`
	UnknownCount int        `json:"unknown_count"`
	StaleCount   int        `json:"stale_count"`
	Lines        []DeskLine `json:"lines"`
}

// deskUnknown builds an UNKNOWN row. The reason is mandatory: an UNKNOWN with
// no reason is the same silence it replaced.
func deskUnknown(n int, key, label, source, reason string) DeskLine {
	if strings.TrimSpace(reason) == "" {
		reason = "no reason recorded — this is a bug in the strip, not an absent fact"
	}
	return DeskLine{N: n, Key: key, Label: label, Text: "UNKNOWN", State: "unknown",
		Source: source, Reason: reason}
}

// deskSafe runs one line's computation. A panic or a nil dependency becomes an
// UNKNOWN row naming what failed — never a blank strip, never a loop failure.
func (at *AutoTrader) deskSafe(n int, key, label, source string, fn func() DeskLine) (out DeskLine) {
	defer func() {
		if r := recover(); r != nil {
			at.logWarnf("🔭 desk strip: line %d (%s) panicked and was contained: %v", n, key, r)
			out = deskUnknown(n, key, label, source, fmt.Sprintf("computing this line panicked: %v", r))
		}
	}()
	return fn()
}

// deskBars reads the 1m tape. A provider that is not installed is an ABSENT
// SOURCE, which the caller renders UNKNOWN — never a panic the recover() has to
// catch. recover() is this file's safety net, not its design.
func deskBars(symbol string, n int) []market.Kline {
	if market.FuturesBarsProvider == nil {
		return nil
	}
	return market.FuturesBarsProvider(symbol, "1m", n)
}

// deskAge renders an age the way the strip states every age.
func deskAge(ms int64) string {
	if ms <= 0 {
		return "0s"
	}
	return (time.Duration(ms) * time.Millisecond).Round(time.Second).String()
}

// DeskStripAt builds the whole strip. now is the caller's clock (A28); the HTTP
// handler is the entry point that supplies it.
func (at *AutoTrader) DeskStripAt(now time.Time) DeskStrip {
	nowMs := now.UnixMilli()
	s := DeskStrip{TraderID: at.id, GeneratedMs: nowMs}

	sym := at.futuresSymbol()
	tick := market.FuturesTickSize(sym)
	pv := market.FuturesPointValue(sym)

	// A10 — THESE TWO ARE COMPUTED BEFORE THE LOOP BECAUSE LATER LINES NEED
	// THEM, so they need the same containment the lines get. Without it a nil
	// broker link panics straight through DeskStripAt and past every deskSafe
	// below it: found by TestEveryLineIsDatedOrUnknownWithAReason, which is the
	// whole reason that pin drives the real entry point instead of the lines.
	pos, posErr := at.deskPositionSafe()
	book := at.deskSafe(8, "book", "BOOK", "cutover gate leg 4", func() DeskLine { return at.deskBook(now) })

	s.Lines = append(s.Lines,
		at.deskSafe(1, "mode", "MODE", "strategy config · session registry · /api/health", func() DeskLine { return at.deskMode(now, book) }),
		at.deskSafe(2, "position", "POSITION", "trader.GetPositions", func() DeskLine { return at.deskPositionLine(pos, posErr, pv, nowMs) }),
		at.deskSafe(3, "protection", "PROTECTION", "accepted_risk (Wave A)", func() DeskLine { return at.deskProtection(pos, tick, pv, nowMs) }),
		at.deskSafe(4, "drift", "DRIFT", "accepted_risk vs armed_orders", func() DeskLine { return at.deskDrift(pos, nowMs) }),
		at.deskSafe(5, "target", "TARGET", "accepted_risk (Wave A)", func() DeskLine { return at.deskTarget(pos, tick, pv, nowMs) }),
		at.deskSafe(6, "day", "DAY", "trader_positions.pnl_corrected · strategy guardrails", func() DeskLine { return at.deskDay(now) }),
		at.deskSafe(7, "arms", "ARMS", "armed_orders", func() DeskLine { return at.deskArms(pos, now, tick) }),
		book,
		at.deskSafe(9, "feed", "FEED", "bars · NT8 link · AddOn build id", func() DeskLine { return at.deskFeed(now) }),
		at.deskSafe(10, "planner", "PLANNER", "plannerReadInFlight claim", func() DeskLine { return at.deskPlanner(now) }),
		at.deskSafe(11, "range", "RANGE", "bars (session) · ATR5m", func() DeskLine { return at.deskRange(now, sym) }),
		at.deskSafe(12, "lastfill", "LAST FILL", "trader_fills", func() DeskLine { return at.deskLastFill(now) }),
		at.deskSafe(13, "confirmation", "CONFIRMATION", "versioned scenario_meta.confirm", func() DeskLine { return at.deskConfirmation(now) }),
		at.deskSafe(14, "scenarios", "SCENARIOS", "authored scenario economics", func() DeskLine { return at.deskScenarioEconomics(now) }),
	)

	for i := range s.Lines {
		if s.Lines[i].AsOfMs > 0 && s.Lines[i].AgeMs == 0 {
			s.Lines[i].AgeMs = nowMs - s.Lines[i].AsOfMs
		}
		switch s.Lines[i].State {
		case "unknown":
			s.UnknownCount++
		case "stale":
			s.StaleCount++
		}
	}
	// D4 — 5s while there is something live to watch, 15s otherwise. Resolved
	// from the state actually computed above, never a file default.
	s.CadenceMs = deskCadenceIdleMs
	armsLive := false
	for _, l := range s.Lines {
		// Decide on the row's STATE, never on a substring of its rendered text:
		// "none resting" contains "resting", and a text match read LIVE with
		// nothing live at all.
		if l.Key == "arms" && l.State == "ok" {
			armsLive = true
		}
	}
	if pos != nil || armsLive {
		s.CadenceMs = deskCadenceLiveMs
	}
	return s
}

// The two cadences. Named so the boot line can READ them (A11).
const (
	deskCadenceLiveMs = 5000
	deskCadenceIdleMs = 15000
)

// DeskBootLine is D7 — every field READ from the code that enforces it.
//
// The UNKNOWN count is per-REQUEST, not per-process: at boot no strip has been
// built for anyone, so this prints n/a rather than a zero it did not measure
// (A24 — a field the process cannot know yet says so). Pass a strip to report
// a real count.
func DeskBootLine(s *DeskStrip) string {
	lines, unknown := deskLineCount, "n/a (no strip built yet — the count is per request)"
	if s != nil {
		lines = len(s.Lines)
		unknown = fmt.Sprintf("%d", s.UnknownCount)
	}
	return fmt.Sprintf("desk strip: lines=%d · unknown=%s · cadence=%ds live / %ds idle · book-age-bound=%s · every row dated; UNKNOWN carries its reason, never a zero and never a dash",
		lines, unknown, deskCadenceLiveMs/1000, deskCadenceIdleMs/1000, snapshotMaxAge())
}

// deskLineCount is the strip's shape, asserted by the tests so the boot line
// and the strip cannot drift.
const deskLineCount = 14

// ── the lines ────────────────────────────────────────────────────────────────

// deskAccountMode READS whether the bound account is a simulation account, from
// the AddOn's own accounts frame (AccountInfo.IsSim, set from NT8's
// Account.Simulation). It never asserts. An absent frame is UNKNOWN, and an
// account the frame does not list is UNKNOWN — not "live", because a missing
// answer is not a dangerous answer, it is a missing one.
func (at *AutoTrader) deskAccountMode() string {
	nt := at.armedTrader()
	if nt == nil {
		return "account UNKNOWN (no NT8 link)"
	}
	srv := nt.GetServer()
	if srv == nil {
		return "account UNKNOWN (no NT8 server)"
	}
	bound := nt.BoundAccount()
	if strings.TrimSpace(bound) == "" {
		return "account UNKNOWN (no account bound)"
	}
	accounts, _ := srv.GetAccountsList()
	for _, a := range accounts {
		if strings.EqualFold(strings.TrimSpace(a.Name), strings.TrimSpace(bound)) {
			if a.IsSim {
				return "SIM"
			}
			// Never softened. If NT8 ever reports a non-simulation account the
			// strip says so in the loudest word it has.
			return "*** LIVE ACCOUNT ***"
		}
	}
	return "account UNKNOWN (the accounts frame does not list the bound account)"
}

// deskPositionSafe contains a broker link that is absent or angry. A nil
// result with a nil error means FLAT; a non-nil error means "we could not ask",
// and the two render differently on purpose.
func (at *AutoTrader) deskPositionSafe() (pos map[string]interface{}, err error) {
	defer func() {
		if r := recover(); r != nil {
			at.logWarnf("🔭 desk strip: reading the position panicked and was contained: %v", r)
			pos, err = nil, fmt.Errorf("reading the broker position panicked: %v", r)
		}
	}()
	return at.deskPosition()
}

func (at *AutoTrader) deskPosition() (map[string]interface{}, error) {
	ps, err := at.GetPositions()
	if err != nil {
		return nil, err
	}
	for _, p := range ps {
		return p, nil // one contract, one position (SIM-only, MNQ)
	}
	return nil, nil
}

func (at *AutoTrader) deskMode(now time.Time, book DeskLine) DeskLine {
	// FIX (2026-09-06, found by review of this wave's own code): this line
	// printed the literal "SIM ·". The word that separates simulated money from
	// real money is the last thing that may be asserted rather than read — a
	// hardcoded SIM would have read as reassurance on an account the AddOn never
	// reported as simulation. It is now READ from the accounts frame, and says
	// UNKNOWN when the frame has not arrived.
	acct := at.deskAccountMode()

	// FIX: this used kernel.DefaultSessionRegistry(), the SHIPPED fallback,
	// bypassing the admin registry in system_config — the exact dead wire W8
	// exists to close (trader/auto_trader_registry.go:10-13). It agreed with the
	// stored registry today, which is how a bypass survives review.
	session := "none"
	if sd, ok := at.sessionRegistry(now).ActiveSession(now); ok && sd != nil {
		session = string(sd.Name)
	}
	// AND: ActiveSession names the WINDOW; it ignores Enabled and it ignores the
	// weekday, so it answers "NY" at 14:22 on a Sunday with CME shut. Two
	// different questions, so the row states both.
	// D5 (owner ruling 2026-09-07) — THE MODE LINE SAYS WHICH DAY IT IS.
	// It read "CME CLOSED (holiday)" beside a feed carrying a bar seconds old,
	// because a boolean could not express "shortened". It now carries the
	// classification, the close time and where the answer came from, and says
	// UNKNOWN in the established in-text shape when the calendar cannot answer.
	market := "OPEN"
	if closed, reason := kernel.CMEClosedReason(now); closed {
		market = "CLOSED (" + reason + ")"
	}
	market += kernel.SessionDayNote(now)
	mode := at.planModeFor(session)
	// RULE 3 — never one green word. Process, feed, link and book are four
	// different questions and the row answers each.
	feed := "UNKNOWN"
	if bars := deskBars(at.futuresSymbol(), 2); len(bars) > 0 {
		feed = deskAge(now.UnixMilli()-bars[len(bars)-1].OpenTime) + " since last bar"
	}
	link, linkKnown := at.deskLinkStatus()
	state, reason := "ok", ""
	if !linkKnown {
		state, reason = "unknown", "NT8 link state has not been received"
	}

	// ROLL WAVE — the contract, from the AddOn's frame, with when it said so.
	// "n/a" when no frame has named one; never a date, never a literal (A24).
	contractText := "contract=n/a (no subscription ACK received)"
	if f, ok := at.contractFactFor(at.futuresSymbol()); ok {
		since := "boot"
		if !f.ReceivedAt.IsZero() {
			since = kernel.ClockCTSeconds(f.ReceivedAt)
		}
		contractText = fmt.Sprintf("contract=%s since %s (%s)", f.Contract, since, f.Source)
		if f.Previous != "" {
			contractText += fmt.Sprintf(" · ROLLED from %s at %s", f.Previous, kernel.ClockCTSeconds(f.RolledAt))
		}
	}

	return DeskLine{
		N: 1, Key: "mode", Label: "MODE", State: state, Verified: linkKnown, Reason: reason,
		Source: "strategy config · session registry · bars · NT8 link · subscription ACK",
		AsOfMs: now.UnixMilli(),
		// The clock routes through kernel/tz.go's ONE time source (class 60 /
		// the TZ guard): a bare "15:04:05" here is a second, unlabelled clock,
		// and this repo has already been bitten by two of those.
		Text: fmt.Sprintf("%s · plan_mode=%s · session=%s · CME %s · %s · %s · process responding · feed %s · link %s · book %s",
			acct, mode, session, market, kernel.ClockCTSeconds(now), contractText, feed, link, book.Text),
	}
}

func (at *AutoTrader) deskPositionLine(pos map[string]interface{}, err error, pv float64, nowMs int64) DeskLine {
	if err != nil {
		return deskUnknown(2, "position", "POSITION", "trader.GetPositions", "the broker link could not be read: "+err.Error())
	}
	if pos == nil {
		return DeskLine{N: 2, Key: "position", Label: "POSITION", State: "flat", Verified: true,
			Source: "trader.GetPositions", AsOfMs: nowMs, Text: "FLAT — no open position"}
	}
	side, _ := pos["side"].(string)
	qty := deskNum(pos, "quantity", "positionAmt")
	entry := deskNum(pos, "entryPrice", "entry_price")
	mark := deskNum(pos, "markPrice", "mark_price")
	if mark == 0 {
		mark = entry
	}
	sign := 1.0
	if strings.EqualFold(side, "short") || strings.EqualFold(side, "sell") {
		sign = -1
	}
	pts := (mark - entry) * sign
	usd := pts * math.Abs(qty) * pv
	return DeskLine{N: 2, Key: "position", Label: "POSITION", State: "ok", Verified: true,
		Unit: "USD", Source: "trader.GetPositions", AsOfMs: nowMs,
		Text: fmt.Sprintf("%s %.0f @ %.2f · mark %.2f · uPnL %+.2f pts = %+.2f USD",
			strings.ToUpper(side), math.Abs(qty), entry, mark, pts, usd)}
}

func deskNum(m map[string]interface{}, keys ...string) float64 {
	for _, k := range keys {
		if v, ok := m[k].(float64); ok && v != 0 {
			return v
		}
	}
	return 0
}

// deskJoinKey reads a join key and REFUSES the poison values.
//
// 567 of 587 trader_positions rows carry the LITERAL five-character string
// "<nil>" in entry_order_id — a formatted nil pointer that was persisted as
// text. It passes IS NULL, it passes = ”, and it joins to nothing, so a naive
// read looks like it worked and silently matched no rows. Treating it as absent
// is the only honest reading.
func deskJoinKey(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		v, _ := m[k].(string)
		v = strings.TrimSpace(v)
		switch strings.ToLower(v) {
		case "", "<nil>", "nil", "null", "0":
			continue
		}
		return v
	}
	return ""
}

// acceptedFor finds the immutable accepted-risk record for the open position's
// signal. RULE 5: this is the ONLY source PROTECTION and TARGET may use.
func (at *AutoTrader) acceptedFor(pos map[string]interface{}) (*store.AcceptedRisk, string) {
	if at.store == nil {
		return nil, "no store"
	}
	if pos == nil {
		return nil, "no open position"
	}
	sig := deskJoinKey(pos, "signal_id", "entry_order_id")
	if sig == "" {
		return nil, "the open position carries no usable signal id to join on"
	}
	rows, err := at.store.AcceptedRisk().ForSignal(sig)
	if err != nil {
		return nil, "accepted_risk read failed: " + err.Error()
	}
	for i := range rows {
		if rows[i].AcceptedStopPx != nil {
			return &rows[i], ""
		}
	}
	if len(rows) == 0 {
		return nil, "no accepted-risk record for this signal — the recorder writes one at the next order acceptance"
	}
	return nil, "an accepted-risk row exists but carries no broker stop (the book did not report one)"
}

func (at *AutoTrader) deskProtection(pos map[string]interface{}, tick, pv float64, nowMs int64) DeskLine {
	if pos == nil {
		return DeskLine{N: 3, Key: "protection", Label: "PROTECTION", State: "flat", Verified: true,
			Source: "accepted_risk", AsOfMs: nowMs, Text: "FLAT — nothing to protect"}
	}
	ar, why := at.acceptedFor(pos)
	if ar == nil {
		return deskUnknown(3, "protection", "PROTECTION", "accepted_risk", why)
	}
	mark := deskNum(pos, "markPrice", "entryPrice")
	qty := math.Abs(deskNum(pos, "quantity", "positionAmt"))
	stop := *ar.AcceptedStopPx
	dist := math.Abs(mark - stop)
	risk := dist * qty * pv
	return DeskLine{N: 3, Key: "protection", Label: "PROTECTION", State: "ok", Verified: true,
		Unit: "USD", Source: "accepted_risk (the BROKER's accepted stop)", AsOfMs: ar.AcceptedAtMs,
		Text: fmt.Sprintf("accepted stop %.2f · %.2f pts (%.0f ticks) away · %.2f USD at risk if it fills",
			stop, dist, dist/tick, risk)}
}

func (at *AutoTrader) deskDrift(pos map[string]interface{}, nowMs int64) DeskLine {
	if pos == nil {
		return DeskLine{N: 4, Key: "drift", Label: "DRIFT", State: "flat", Verified: true,
			Source: "accepted_risk vs armed_orders", AsOfMs: nowMs, Text: "FLAT"}
	}
	ar, why := at.acceptedFor(pos)
	if ar == nil {
		return deskUnknown(4, "drift", "DRIFT", "accepted_risk vs armed_orders", why)
	}
	d := *ar.AcceptedStopPx - ar.LedgerStopPx
	if ar.LedgerStopPx == 0 {
		return deskUnknown(4, "drift", "DRIFT", "accepted_risk vs armed_orders",
			"the accepted record carries no ledger stop to compare against")
	}
	if math.Abs(d) < 1e-9 {
		return DeskLine{N: 4, Key: "drift", Label: "DRIFT", State: "ok", Verified: true,
			Source: "accepted_risk vs armed_orders", AsOfMs: ar.AcceptedAtMs,
			Text: "none — ledger and broker agree"}
	}
	return DeskLine{N: 4, Key: "drift", Label: "DRIFT", State: "ok", Verified: true, Unit: "pts",
		Source: "accepted_risk vs armed_orders", AsOfMs: ar.AcceptedAtMs,
		Text: fmt.Sprintf("ledger %.6f vs accepted %.2f → %+.6f pts (the ledger price is NOT your stop)",
			ar.LedgerStopPx, *ar.AcceptedStopPx, d)}
}

func (at *AutoTrader) deskTarget(pos map[string]interface{}, tick, pv float64, nowMs int64) DeskLine {
	if pos == nil {
		return DeskLine{N: 5, Key: "target", Label: "TARGET", State: "flat", Verified: true,
			Source: "accepted_risk", AsOfMs: nowMs, Text: "FLAT"}
	}
	ar, why := at.acceptedFor(pos)
	if ar == nil || ar.AcceptedTargetPx == nil {
		if why == "" {
			why = "the accepted record carries no broker target"
		}
		return deskUnknown(5, "target", "TARGET", "accepted_risk", why)
	}
	mark := deskNum(pos, "markPrice", "entryPrice")
	qty := math.Abs(deskNum(pos, "quantity", "positionAmt"))
	tgt := *ar.AcceptedTargetPx
	dist := math.Abs(tgt - mark)
	return DeskLine{N: 5, Key: "target", Label: "TARGET", State: "ok", Verified: true, Unit: "USD",
		Source: "accepted_risk (the BROKER's accepted target)", AsOfMs: ar.AcceptedAtMs,
		Text: fmt.Sprintf("accepted target %.2f · %.2f pts (%.0f ticks) away · %.2f USD if it fills",
			tgt, dist, dist/tick, dist*qty*pv)}
}

func (at *AutoTrader) deskDay(now time.Time) DeskLine {
	if at.store == nil {
		return deskUnknown(6, "day", "DAY", "trader_positions", "no store")
	}
	// THE ENFORCED limit, resolved the same way the ledger boot line resolves it
	// (auto_trader_pause.go): Studio value → env fallback → and ONLY while the
	// guardrails master is ON. A limit nothing enforces is said to be unenforced.
	limit, src, enforced := at.deskGuardrail()
	realized, n, unresolved := at.deskRealizedToday(now)
	limitTxt := deskDailyLimitText(at, limit)
	if enforced {
		limitTxt = fmt.Sprintf("limit -%.0f USD (%s, ENFORCED)", limit, src)
	}
	unres := ""
	if unresolved > 0 {
		unres = fmt.Sprintf(" · %d closed row(s) excluded: pnl_corrected NULL", unresolved)
	}
	return DeskLine{N: 6, Key: "day", Label: "DAY", State: "ok", Verified: true, Unit: "USD",
		Source: "trader_positions.pnl_corrected · strategy guardrails", AsOfMs: now.UnixMilli(),
		Text: fmt.Sprintf("realized %+.2f USD on %d trade(s) · %s%s", realized, n, limitTxt, unres)}
}

// deskGuardrail resolves the ENFORCED daily-loss limit. One implementation of
// the rule the ledger boot line already states (A24: never a second copy).
func (at *AutoTrader) deskGuardrail() (limit float64, source string, enforced bool) {
	if at.config.StrategyConfig == nil {
		return 0, "no strategy config", false
	}
	rc := at.config.StrategyConfig.RiskControl
	if !hlBool(rc.GuardrailsEnabled, true) {
		return 0, "guardrails master OFF", false
	}
	// 2026-09-09 — BOTH TOGGLES, because the GATE requires both.
	//
	// kernel/risk_limits.go:309 reads
	//   if g.DailyLossEnabled && g.DailyLossLimitUSD > 0 && g.DailyRealizedPnL <= -g.DailyLossLimitUSD
	// so daily_loss_enabled gates this leg independently of the master. This
	// resolver checked only the master, and the live config has BOTH off — so
	// the moment the owner turned the master ON, the strip would have reported
	// the $450 limit ENFORCED while the gate went on ignoring it. A green word
	// answering a narrower question than the reader will assume (class 82).
	//
	// ABSENT stays true: engine_analysis.go builds the gate with
	// boolOrDefault(rc.DailyLossEnabled, true), and this must agree with it or
	// the strip becomes a second, disagreeing definition (A24).
	if !hlBool(rc.DailyLossEnabled, true) {
		return 0, "daily_loss_enabled OFF", false
	}
	if rc.DailyLossLimitUSD > 0 {
		return rc.DailyLossLimitUSD, "studio", true
	}
	return kernel.LoadRiskLimitsFromConfig().MaxDailyLossUSD, "env RISK_MAX_DAILY_LOSS_USD fallback — Studio value unset", true
}

// deskRealizedToday sums pnl_corrected for the CME session-day under the A22
// rules, and COUNTS what it had to exclude rather than hiding it.
func (at *AutoTrader) deskRealizedToday(now time.Time) (total float64, n int, unresolved int) {
	dayMs := kernel.CMESessionDayStart(now).UnixMilli()
	// P3-4 (rel10): scope to the BOUND account, the same scope the done-after-win
	// day-P&L gate uses (MentorDayActivity) — two gates, one account, one day sum.
	// The 500-row limit reads the newest 500 closes overall; a session-day with
	// >500 closes would truncate today's sum, but no live path can produce that
	// (mentor caps entries per day; the AI path caps 3/day) — documented, not a
	// live truncation.
	rows, err := at.store.Position().GetClosedPositions(at.id, 500, at.currentAccountName())
	if err != nil {
		return 0, 0, 0
	}
	for _, p := range rows {
		if p == nil || p.ExitTime < dayMs {
			continue
		}
		if store.UnknownPnLReason(p.CloseReason) || p.PlanID == "UNRESOLVABLE" {
			continue
		}
		if p.PnlCorrected == nil {
			unresolved++
			continue
		}
		total += *p.PnlCorrected
		n++
	}
	return total, n, unresolved
}

func (at *AutoTrader) deskArms(pos map[string]interface{}, now time.Time, tick float64) DeskLine {
	if at.store == nil {
		return deskUnknown(7, "arms", "ARMS", "armed_orders", "no store")
	}
	rows, err := at.store.ArmedOrders().ListNonTerminal(at.id)
	if err != nil {
		return deskUnknown(7, "arms", "ARMS", "armed_orders", "ledger read failed: "+err.Error())
	}
	if len(rows) == 0 {
		return DeskLine{N: 7, Key: "arms", Label: "ARMS", State: "flat", Verified: true,
			Source: "armed_orders", AsOfMs: now.UnixMilli(), Text: "none resting"}
	}
	mark := deskNum(pos, "markPrice", "entryPrice")
	parts := make([]string, 0, len(rows))
	newest := int64(0)
	for _, r := range rows {
		placed := "NOT placed at broker"
		if strings.TrimSpace(r.SignalID) != "" {
			placed = "placed"
		}
		dist := "distance UNKNOWN (no mark)"
		if mark > 0 && tick > 0 {
			d := math.Abs(r.EntryPx - mark)
			dist = fmt.Sprintf("%.2f pts (%.0f ticks) from mark", d, d/tick)
		}
		age := deskAge(now.UnixMilli() - r.CreatedAt.UnixMilli())
		if ms := r.CreatedAt.UnixMilli(); ms > newest {
			newest = ms
		}
		kind := r.Kind
		if kind == "" {
			kind = "limit"
		}
		parts = append(parts, fmt.Sprintf("%s %s %s @ %.2f · %s · age %s · %s · %s",
			r.Scenario, strings.ToUpper(r.Side), kind, r.EntryPx, dist, age, placed, r.State))
	}
	return DeskLine{N: 7, Key: "arms", Label: "ARMS", State: "ok", Verified: true,
		Source: "armed_orders", AsOfMs: newest,
		Text: fmt.Sprintf("%d resting — %s", len(rows), strings.Join(parts, " | "))}
}

// deskBook is cutover leg 4 ON SCREEN, CONTINUOUSLY. It reuses the gate's own
// formatter rather than re-deriving it. Each display read uses the caller's
// clock and dates the exact broker snapshot supplying its values.
func (at *AutoTrader) deskBook(now time.Time) DeskLine {
	cache, account, symbol := at.brokerBook()
	if cache == nil {
		return deskUnknown(8, "book", "BOOK", "NT8 order_snapshot", "no broker book received; AddOn build UNKNOWN")
	}
	snap, received, ok := cache.LatestReceived(account)
	if !ok || received.IsZero() {
		return deskUnknown(8, "book", "BOOK", "NT8 order_snapshot", "no dated broker book received for this account; AddOn build UNKNOWN")
	}
	build := strings.TrimSpace(snap.BuildID)
	if build == "" {
		build = "UNKNOWN"
	}
	age := now.Sub(received)
	if age < 0 {
		return deskUnknown(8, "book", "BOOK", "NT8 order_snapshot", "broker receipt is ahead of the server clock; age UNKNOWN · AddOn build "+build)
	}
	metadata := fmt.Sprintf("received %s · age %s · AddOn build %s", kernel.ClockCTSeconds(received), deskAge(age.Milliseconds()), build)
	orders, err := at.gateOpenOrders()
	if err != nil {
		line := deskUnknown(8, "book", "BOOK", "NT8 order_snapshot × armed_orders", "ledger comparison unavailable: "+err.Error())
		line.Text = "UNKNOWN — " + line.Reason + " · " + metadata
		line.AsOfMs, line.AgeMs = received.UnixMilli(), age.Milliseconds()
		return line
	}
	// Freeze this display read so the gate's existing count/freshness formatter
	// and the displayed age/build all describe the same received frame. This
	// request-local view changes no trading gate or shared cache.
	view := nt.NewOrderSnapshotCache()
	view.PutAt(snap, received)
	leg := Leg4FromBrokerAt(view, account, symbol, OrderSnapshotInterval(), orders, now)
	state, reason := "ok", ""
	if !leg.Pass {
		state, reason = "stale", leg.Detail
	}
	return DeskLine{N: 8, Key: "book", Label: "BOOK", State: state, Verified: leg.Pass,
		Source: leg.Source, AsOfMs: received.UnixMilli(), AgeMs: age.Milliseconds(),
		Text: leg.Detail + " · " + metadata, Reason: reason}
}

// FeedStatus is a received field, not the bar-based trading connectivity gate.
func (at *AutoTrader) deskLinkStatus() (string, bool) {
	// P-D ruling: the owner-facing desk strip LINK follows the REAL TCP socket
	// (the same helper health uses) — it must never read "up" on a latched
	// edge-triggered feed_status frame after the socket is gone. "up"/"down"
	// mirror the health link field; UNKNOWN only when there is no NT8 TCP
	// trader to measure.
	if connected, ok := at.HealthLinkConnected(); ok {
		if connected {
			return "up", true
		}
		return "down", true
	}
	return "UNKNOWN (no NT8 link)", false
}

func (at *AutoTrader) deskFeed(now time.Time) DeskLine {
	bars := deskBars(at.futuresSymbol(), 2)
	if len(bars) == 0 {
		return deskUnknown(9, "feed", "FEED", "bars", "no 1m bars are available to age")
	}
	last := bars[len(bars)-1].OpenTime
	age := now.UnixMilli() - last
	link, linkKnown := at.deskLinkStatus()
	build := "UNKNOWN"
	if nt := at.armedTrader(); nt != nil {
		if srv := nt.GetServer(); srv != nil {
			if snap, ok := srv.OrderSnapshots().Latest(nt.BoundAccount()); ok && snap.BuildID != "" {
				build = snap.BuildID
			}
		}
	}
	state := "ok"
	reason := ""
	if age > int64(2*time.Minute/time.Millisecond) {
		state = "stale"
		reason = "the newest 1m bar is older than 2 minutes"
	}
	if !linkKnown {
		state = "unknown"
		if reason != "" {
			reason += "; "
		}
		reason += "NT8 link state has not been received"
	}
	return DeskLine{N: 9, Key: "feed", Label: "FEED", State: state, Verified: state == "ok",
		Source: "bars · NT8 link · AddOn build id", AsOfMs: last, Reason: reason,
		Text: fmt.Sprintf("last bar %s ago · link %s · AddOn build %s", deskAge(age), link, build)}
}

func (at *AutoTrader) deskPlanner(now time.Time) DeskLine {
	inFlight, key := at.AnyPlannerReadInFlight()
	txt := "idle — no planner read claimed"
	if inFlight {
		txt = "read IN FLIGHT: " + key
	}

	state := "ok"
	if plan := kernel.ActivePlanFor(at.id, at.futuresSymbol()); plan != nil {
		identities := kernel.ScenarioIdentities(&plan.Doc)
		for _, sc := range plan.Doc.Scenarios {
			r := identities[sc.ID]
			if r.Level == nil {
				txt += fmt.Sprintf(" · %s level_id=%s (%s)", sc.ID, identityIDText(r.LevelID), r.Basis)
			} else {
				// A reference level ("ref|…") has NO formation close by construction
				// (W-GEOMETRY-REFUSAL b1): n/a, never a dereference of nil (the
				// live "line 10 (planner) panicked" on every scan, 2026-09-23).
				formed := "n/a"
				if r.Level.FormedCloseMs != nil {
					formed = strconv.FormatInt(*r.Level.FormedCloseMs, 10)
				}
				txt += fmt.Sprintf(" · %s level_id=%s @ %.2f formed_close_ms=%s", sc.ID, identityIDText(r.LevelID), r.Level.Price, formed)
				if r.Disagreed {
					txt += fmt.Sprintf(" (evaluator %.2f differs; decision unchanged)", *r.EvaluatorAnchor)
				}
			}
		}
		ids := make([]string, 0, len(plan.Doc.Scenarios))
		for _, sc := range plan.Doc.Scenarios {
			ids = append(ids, sc.ID)
		}
		live := at.store.ScenarioLivenessFor(at.id, plan.PlanID, plan.Version, ids, now)
		if live.Tradeable == nil {
			txt += " · tradeable UNKNOWN — " + live.Reason
			state = "unknown"
		} else {
			txt += fmt.Sprintf(" · %s v%d tradeable %d/%d (evaluator) · observed %s", plan.Session, plan.Version, *live.Tradeable, live.Total, kernel.FormatCT(*live.ObservedAt))
			if live.Total > 0 && *live.Tradeable == 0 {
				txt += " · EXHAUSTED (warning only; no exhaustion wake)"
				state = "warn"
			}
		}
	} else {
		txt += " · tradeable UNKNOWN — no active plan"
	}
	return DeskLine{N: 10, Key: "planner", Label: "PLANNER", State: state, Verified: state != "unknown",
		Source: "plannerReadInFlight claim", AsOfMs: now.UnixMilli(), Text: txt}
}

func (at *AutoTrader) deskRange(now time.Time, sym string) DeskLine {
	bars := deskBars(sym, kernel.AISVPBarCount)
	if len(bars) == 0 {
		return deskUnknown(11, "range", "RANGE", "bars", "no 1m bars are available")
	}
	dayMs := kernel.CMESessionDayStart(now).UnixMilli()
	hi, lo, n, newest := math.Inf(-1), math.Inf(1), 0, int64(0)
	for _, b := range bars {
		if b.OpenTime < dayMs {
			continue
		}
		hi, lo, n = math.Max(hi, b.High), math.Min(lo, b.Low), n+1
		if b.OpenTime > newest {
			newest = b.OpenTime
		}
	}
	if n == 0 {
		return deskUnknown(11, "range", "RANGE", "bars",
			"no bars inside the current CME session-day — the session has not started or the feed is behind")
	}
	atr := plannerATR5m(sym)
	atrTxt := "ATR5m UNKNOWN"
	if atr > 0 {
		atrTxt = fmt.Sprintf("%.2f× ATR5m (%.2f)", (hi-lo)/atr, atr)
	}
	return DeskLine{N: 11, Key: "range", Label: "RANGE", State: "ok", Verified: true, Unit: "pts",
		Source: "bars (session-day) · ATR5m", AsOfMs: newest,
		Text: fmt.Sprintf("session %.2f–%.2f = %.2f pts on %d bar(s) · %s", lo, hi, hi-lo, n, atrTxt)}
}

func (at *AutoTrader) deskLastFill(now time.Time) DeskLine {
	if at.store == nil {
		return deskUnknown(12, "lastfill", "LAST FILL", "trader_fills", "no store")
	}
	f, err := at.store.Order().LatestFillForTrader(at.id)
	if err != nil || f == nil {
		return deskUnknown(12, "lastfill", "LAST FILL", "trader_fills",
			"no fill has been recorded for this trader")
	}
	// Slippage is NOT knowable today: slippage_ticks is transported on the wire
	// and has no production consumer, and the intended price is not stored
	// beside the fill. Saying so is the honest rendering (A24).
	return DeskLine{N: 12, Key: "lastfill", Label: "LAST FILL", State: "ok", Verified: true,
		Source: "trader_fills", AsOfMs: f.CreatedAt,
		Text: fmt.Sprintf("%s %s %.2f qty %.2f · slippage UNKNOWN (the intended price is not stored beside the fill; the AddOn's slippage_ticks has no consumer)",
			f.Side, f.Symbol, f.Price, f.Quantity)}
}

// deskDailyLimitText says, in the owner's own words, which switches are off and
// what the configured limit would be if they moved. "soft-audit only" told the
// owner the state but not the remedy; this names both toggles and the value, so
// the line reads as an instruction rather than a status.
func deskDailyLimitText(at *AutoTrader, limit float64) string {
	if at == nil || at.config.StrategyConfig == nil {
		return "no enforced daily limit (no strategy config)"
	}
	rc := at.config.StrategyConfig.RiskControl
	master := hlBool(rc.GuardrailsEnabled, true)
	leg := hlBool(rc.DailyLossEnabled, true)
	if master && leg {
		return "no enforced daily limit (no limit configured)"
	}
	cfgLimit := rc.DailyLossLimitUSD
	if cfgLimit <= 0 {
		cfgLimit = limit
	}
	off := []string{}
	if !master {
		off = append(off, "guardrails master OFF")
	}
	if !leg {
		off = append(off, "daily_loss_enabled OFF")
	}
	amount := "no value set"
	if cfgLimit > 0 {
		amount = fmt.Sprintf("$%.0f", cfgLimit)
	}
	return fmt.Sprintf("daily limit %s is DECORATIVE — %s; it enforces nothing until %s move",
		amount, strings.Join(off, " and "), map[bool]string{true: "that switch", false: "both switches"}[len(off) == 1])
}
