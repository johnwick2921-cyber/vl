package trader

import (
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"vl/kernel"
	nt "vl/provider/ninjatrader"
	"vl/store"
	"vl/telemetry"
)

// ── LOOK TWICE (spec 2026-11-24 section 3 B; incident 11-19 06:47:30, −1207) ──
//
// For up to ~0.4 s after an ordinary stop-out NT8 still reports the position
// while its stop has already left the book. The reconciler used to act on ONE
// look at that moment: it sent close_position 24 ms after the broker had filled
// the stop, NT8 was flat by then, and the flatten OPENED a reverse with no stop.
//
// So a look that finds a naked (or partly covered) position never acts. It opens
// a HOLD and acts only when ALL of these are true on a later pass:
//
//	1. two looks at least protectionConfirm() apart on the SAME position (symbol,
//	   side, quantity, average price), uncovered both times, on one connection
//	   and one Run, with no fill in between and no gap over 70 s;
//	2. the newest POSITIONS FRAME (never the fill-derived cache) was received
//	   after the newest exit fill AND the newest entry fill, and shows exactly
//	   this position. A frame that shows MORE than the frame before it with no
//	   entry fill in between proves nothing until a later frame repeats it
//	   (nt.ProtectionEvidence.Unproven: the AddOn can write a frame late);
//	3. protectionConfirm() has passed since the newest exit fill, entry fill, own
//	   close send and own stop send;
//	4. a heartbeat sent at the second look has been ACKED (link_probe.go): Go has
//	   read everything the AddOn wrote before it. Positions and book are then
//	   read AGAIN, after the ack;
//	5. it is not our own death (our last row there closed within 60 s by receipts
//	   that sum to its whole entry quantity): then a P0 only, for at most 60 s;
//	6. a stop is placed again only on a book received after the previous send.
//
// The evidence is read AFTER the pass's last database read, and once more by
// protectionProof.check with the socket's writer held, as the last step before
// the order frame: anything that changed in between means nothing is written and
// the next look is a first one. Alerts follow the send.
//
// A hold calls nothing that can send: actOnConfirmed is not reached. The next
// look runs on the trader's own re-look goroutine (joined by monitorWg), and
// holding for 35 s raises one P0.
//
// LOCKS. protMu is held for one whole pass, so a beat and a re-look can never
// judge or act at the same time. Order: protMu → limitFlattenMu (the flatten),
// protMu → isRunningMutex (starting the goroutine). Nothing takes protMu while
// holding either, Stop never takes it, the read loop never takes it, and the
// ack wait happens OUTSIDE it.

const (
	protectionRelookTrigger = "second-look"

	protectionConfirmEnv     = "PROTECTION_CONFIRM_MS"
	protectionConfirmDefault = 3000 * time.Millisecond
	// A typo must never become the old single look: "3" (meant as seconds) is 3
	// ms. Outside [1 s, 30 s] a value is bad; only an exact 0 turns the check off.
	protectionConfirmMin = 1000 * time.Millisecond
	protectionConfirmMax = 30000 * time.Millisecond

	// protectionLookGap: consecutive 60 s beats still count as two looks.
	protectionLookGap = 70 * time.Second
	// protectionHoldAlertAfter: the AddOn's 30 s heartbeat resend + 5 s.
	protectionHoldAlertAfter = 35 * time.Second
	// protectionOwnDeathWindow bounds rule 5 (both by the row's exit time and by
	// the age of the hold, so a skewed clock cannot stretch it).
	protectionOwnDeathWindow = 60 * time.Second
	// protectionProofMaxAge bounds the time from the start of the pass (which read
	// the link check's ack) to the order frame. In a database stall the read loop
	// can be frozen too (the snapshot sink inserts on it), and then nothing in
	// memory changes: only a FRESH ack says the link is caught up. A pass is a few
	// ms; its one scan (14 days of book history) measured 58 ms on the live DB.
	protectionProofMaxAge = 3 * time.Second
)

// protectionConfirm is the PROTECTION_CONFIRM_MS setting: unset or bad = 3000,
// 0 = the old single look (revert switch). 0 brings back WHEN orders go out, not
// the old alerts: the P0s still follow the send and say what was sent.
func protectionConfirm() time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(protectionConfirmEnv)))
	if err != nil {
		return protectionConfirmDefault
	}
	d := time.Duration(n) * time.Millisecond
	if d == 0 || (d >= protectionConfirmMin && d <= protectionConfirmMax) {
		return d
	}
	return protectionConfirmDefault
}

// ProtectionConfirmBootLine — READ from the setting the reconciler obeys (A11).
func ProtectionConfirmBootLine() string {
	raw, set := os.LookupEnv(protectionConfirmEnv)
	if !set {
		raw = "unset"
	}
	d := protectionConfirm()
	mode := fmt.Sprintf("second look after %dms + link check", d.Milliseconds())
	if d == 0 {
		mode = "OFF — acts on ONE look (revert)"
	}
	return fmt.Sprintf("protection: %s (%s=%s)", mode, protectionConfirmEnv, raw)
}

// protectionLooks is embedded in AutoTrader. protMu is held for one whole
// protection pass and guards the rest.
type protectionLooks struct {
	protMu         sync.Mutex
	protHolds      map[string]*protectionHold // protectionHoldKeys -> a naked position waiting for its second look
	protRelookRun  context.Context            // the Run whose re-look goroutine is alive (nil = none)
	protRelookWake chan struct{}              // a beat sent a link check: wait for its ack now; identifies the goroutine
}

// protectionHold is one position waiting for its second look. protMu guards it.
type protectionHold struct {
	qty       int
	avg       float64
	acceptSeq uint64
	run       context.Context

	since     time.Time // the hold opened (35 s alert, own-death bound)
	firstLook time.Time // zero = the next look is the first one
	lastLook  time.Time

	probe   *nt.LinkProbe // the heartbeat sent at the second look
	probeAt time.Time
	proven  bool // its ack was already read when THIS pass began

	alerted bool   // one P0 per hold
	lastWhy string // a hold's log line repeats only when the reason changes
}

// restart makes the next look the first one again. since/alerted stay: a
// position that keeps changing while naked must still reach the 35 s P0.
func (h *protectionHold) restart() {
	h.firstLook, h.probe, h.proven = time.Time{}, nil, false
}

// protectionRunCtx is the current Run's stop context (nil = never run).
func (at *AutoTrader) protectionRunCtx() context.Context {
	at.stopMonitorMu.Lock()
	defer at.stopMonitorMu.Unlock()
	return at.stopMonitorCtx
}

// protectionServer is the wire the evidence is read from. nil (not the NT8 TCP
// trader) also means no broker book, so no verdict there can ever act.
func (at *AutoTrader) protectionServer() *nt.TCPServer {
	if t := at.armedTrader(); t != nil {
		return t.GetServer()
	}
	return nil
}

// beginProtectionLookLocked opens one pass: it reads which heartbeats are acked
// BEFORE the pass reads positions and book, so those are read after the ack.
// protMu held.
func (at *AutoTrader) beginProtectionLookLocked(srv *nt.TCPServer) time.Time {
	if srv == nil {
		return time.Now()
	}
	now := srv.ReceiptNow()
	for _, h := range at.protHolds {
		h.proven = false
		if h.probe == nil {
			continue
		}
		switch acked := h.probe.Acked(); {
		case now.Sub(h.probeAt) > nt.HeartbeatAckWait:
			h.probe = nil // no ack in time (or an ack nobody looked at): ask again
		case acked:
			h.proven = true
		}
	}
	return now
}

// restartProtectionHoldsLocked: positions could not be read, or their cover
// could not — neither is a look. protMu held.
func (at *AutoTrader) restartProtectionHoldsLocked() {
	for _, h := range at.protHolds {
		h.restart()
	}
}

// dropProtectionHoldLocked ends a hold without acting. protMu held.
func (at *AutoTrader) dropProtectionHoldLocked(key, why string) {
	h := at.protHolds[key]
	if h == nil {
		return
	}
	delete(at.protHolds, key)
	telemetry.IncTraderEvent(at.id, "protection_hold_cleared")
	at.logInfof("🧷 protection hold CLEARED: %s — %s after %s; nothing was sent",
		key, why, h.lastLook.Sub(h.since).Round(time.Millisecond))
}

// dropStaleProtectionHoldsLocked drops the hold of every position that is no
// longer held. Runs only on a successful positions read. protMu held.
func (at *AutoTrader) dropStaleProtectionHoldsLocked(positions []map[string]interface{}) {
	live := make(map[string]bool, len(positions))
	for _, key := range protectionHoldKeys(positions) {
		live[key] = true
	}
	for key := range at.protHolds {
		if !live[key] {
			at.dropProtectionHoldLocked(key, "the position is flat")
		}
	}
}

// expireProtectionHoldsLocked forgets holds nothing has looked at for 70 s (the
// positions stayed unreadable), so the re-look goroutine can end. protMu held.
func (at *AutoTrader) expireProtectionHoldsLocked(srv *nt.TCPServer) {
	if srv == nil {
		return
	}
	now := srv.ReceiptNow()
	for key, h := range at.protHolds {
		if now.Sub(h.lastLook) > protectionLookGap {
			delete(at.protHolds, key)
		}
	}
}

// protectionHoldKeys names the hold of each positions row: symbol|side, plus #2,
// #3 … for further rows of one symbol and side (two MNQ expiries are both "MNQ"
// on the wire). With one key for two rows each row would restart the other's
// looks and neither would ever be protected. The rank is by (average, quantity),
// so the order rows arrive in does not matter.
func protectionHoldKeys(positions []map[string]interface{}) []string {
	keys := make([]string, len(positions))
	for i, p := range positions {
		symbol, _ := p["symbol"].(string)
		side, _ := p["side"].(string)
		keys[i] = symbol + "|" + side
	}
	before := func(i, j int) bool {
		ai, aj := positionAvg(positions[i]), positionAvg(positions[j])
		qi, _ := positions[i]["quantity"].(float64)
		qj, _ := positions[j]["quantity"].(float64)
		switch {
		case ai != aj:
			return ai < aj
		case qi != qj:
			return qi < qj
		}
		return i < j
	}
	out := make([]string, len(positions))
	for i := range positions {
		rank := 1
		for j := range positions {
			if j != i && keys[j] == keys[i] && before(j, i) {
				rank++
			}
		}
		out[i] = keys[i]
		if rank > 1 {
			out[i] = fmt.Sprintf("%s#%d", keys[i], rank)
		}
	}
	return out
}

// protectionProof is the evidence one confirmed second look rests on. check is
// the guard of the send.
type protectionProof struct {
	srv          *nt.TCPServer
	account      string
	symbol, side string
	qty          int
	avg          float64
	ev           nt.ProtectionEvidence
	bookAt       time.Time
	passStart    time.Time
	run          context.Context
	hold         *protectionHold
	withheld     string // why check said no; "" = it has not
}

// check re-reads the evidence, in memory only (it runs with the socket's writer
// held: no database, no protMu). nil = still exactly what the second look saw.
func (p *protectionProof) check() error {
	if p.run != nil && p.run.Err() != nil {
		return p.refuse("the trader is stopping")
	}
	ev := p.srv.ProtectionEvidenceFor(p.account, p.symbol)
	_, bookAt, haveBook := p.srv.OrderSnapshots().LatestReceived(p.account)
	switch {
	case ev.AcceptSeq != p.ev.AcceptSeq || !ev.OnLink:
		return p.refuse("the NinjaTrader connection changed")
	case !ev.Entry.Equal(p.ev.Entry) || !ev.Exit.Equal(p.ev.Exit):
		return p.refuse("a fill arrived")
	case !ev.CloseSent.Equal(p.ev.CloseSent) || !ev.StopSent.Equal(p.ev.StopSent):
		return p.refuse("another order of ours went out")
	case !ev.PositionsAfterFills() || !ev.Holds(p.symbol, p.side, p.qty, p.avg):
		return p.refuse("the position changed")
	case ev.Unproven(p.symbol, p.side):
		return p.refuse("the position was reported smaller, then as before")
	case !haveBook || !bookAt.Equal(p.bookAt):
		return p.refuse("a newer order book arrived")
	}
	if age := p.srv.ReceiptNow().Sub(p.passStart); age > protectionProofMaxAge {
		return p.refuse("the link check is more than " + protectionProofMaxAge.String() + " old")
	}
	return nil
}

func (p *protectionProof) refuse(why string) error {
	p.withheld = why
	return fmt.Errorf("%w: %s", nt.ErrSendWithheld, why)
}

// protectionMayActLocked is the second look (rules 1-6). A proof = act now, with
// its check as the guard of the send; nil = hold, nothing may be sent. The hold
// is restarted either way (the proof speaks for this pass only); the caller ends
// it once something was acted on. protMu held.
//
// passStart is when the pass began (before positions and book were read) and
// bookAt the receive instant of the book the verdict was made on.
func (at *AutoTrader) protectionMayActLocked(v protectionVerdict, key, symbol, side string, qty int, avg float64,
	srv *nt.TCPServer, account string, bookAt, passStart time.Time, confirm time.Duration, trigger string) *protectionProof {

	// The ledger is read BEFORE the evidence: a stalled database read must not
	// stand between the evidence and the send.
	var death store.OwnDeath
	var deathErr error
	if h := at.protHolds[key]; h != nil && h.proven && at.store != nil {
		death, deathErr = at.store.Position().OwnDeathFor(at.id, account, symbol, side)
	}

	ev := srv.ProtectionEvidenceFor(account, symbol) // read LAST: anything newer than the book shows up here
	run := at.protectionRunCtx()
	lookAt := srv.ReceiptNow()

	h := at.protHolds[key]
	if h == nil || h.run != run || lookAt.Sub(h.lastLook) > protectionLookGap {
		h = &protectionHold{run: run, since: lookAt}
		if at.protHolds == nil {
			at.protHolds = make(map[string]*protectionHold)
		}
		at.protHolds[key] = h
		telemetry.IncTraderEvent(at.id, "protection_hold")
	}
	fill := ev.Entry
	if ev.Exit.After(fill) {
		fill = ev.Exit
	}
	if h.qty != qty || h.avg != avg || h.acceptSeq != ev.AcceptSeq || fill.After(h.firstLook) {
		h.restart() // another position, another connection, or a fill since the first look
	}
	h.qty, h.avg, h.acceptSeq, h.lastLook = qty, avg, ev.AcceptSeq, lookAt

	settled, why := false, ""
	newest := ev.LastExecution()
	_, latestBook, haveBook := srv.OrderSnapshots().LatestReceived(account)
	switch {
	case h.firstLook.IsZero():
		h.firstLook = lookAt
		why = fmt.Sprintf("first look; a second one is due in %s", confirm)
	case passStart.Sub(h.firstLook) < confirm:
		why = "the two looks are not yet " + confirm.String() + " apart"
	case !ev.Known || !ev.OnLink:
		why = "no positions frame has been received on this connection"
	case !ev.PositionsAfterFills():
		why = "the newest positions frame is OLDER than the newest fill"
	case !ev.Holds(symbol, side, qty, avg):
		why = "the position changed while this pass was reading"
	case ev.Unproven(symbol, side):
		why = "NinjaTrader had already reported smaller (or flat) and no entry fill has been seen since, so this report may be an old one written late; the next report decides"
	case !newest.IsZero() && passStart.Sub(newest) < confirm:
		why = "a fill or one of our own orders is less than " + confirm.String() + " old"
	case !haveBook || !latestBook.Equal(bookAt):
		why = "a newer order book arrived while this pass was reading"
	case v.Action == protectionPlace && !ev.StopSent.IsZero() && !bookAt.After(ev.StopSent):
		why = "a protective stop was already sent and no order book has arrived since"
	default:
		settled = true
	}
	veto := false
	switch {
	case !settled:
		h.probe, h.proven = nil, false
	case !h.proven:
		why = at.protectionProbeLocked(h, srv)
	default:
		h.probe, h.proven = nil, false // consumed: the ack speaks for this pass only
		why, veto = protectionOwnDeath(death, deathErr, ev, lookAt, h)
	}

	if why == "" {
		if run != nil && run.Err() != nil {
			return nil // stopping: nothing may be sent (Stop joins this goroutine)
		}
		at.logWarnf("🧷 protection second look CONFIRMED (%s): %s %s ×%d still %s after %s — positions frame newer than every fill, link ack received",
			trigger, side, symbol, qty, v.Action, lookAt.Sub(h.since).Round(time.Millisecond))
		h.restart() // withheld at the send = a first look again; acted = the caller ends the hold
		return &protectionProof{srv: srv, account: account, symbol: symbol, side: side, qty: qty, avg: avg,
			ev: ev, bookAt: bookAt, passStart: passStart, run: run, hold: h}
	}

	if why != h.lastWhy {
		h.lastWhy = why
		at.logWarnf("🧷 protection HOLD (%s): %s %s ×%d avg %.2f looks like %s — nothing sent: %s [positions frame %s, exit fill %s, entry fill %s, our close %s, our stop %s] (%s)",
			trigger, side, symbol, qty, avg, v.Action, why,
			agoText(lookAt, ev.Positions), agoText(lookAt, ev.Exit), agoText(lookAt, ev.Entry),
			agoText(lookAt, ev.CloseSent), agoText(lookAt, ev.StopSent), v.Why)
	}
	held := lookAt.Sub(h.since)
	if !h.alerted && (veto || held >= protectionHoldAlertAfter) {
		h.alerted = true
		telemetry.IncTraderEvent(at.id, "protection_hold_p0")
		title := "Open position looks unprotected — the bot is HOLDING, check NinjaTrader"
		if veto {
			title = "NinjaTrader shows a position the bot already closed — check it"
		}
		at.emitAlert("P0", alertKindProtectionHeld,
			fmt.Sprintf("held:%s:%s:%d", symbol, side, h.since.UnixMilli()), title,
			fmt.Sprintf("%s %s ×%d (avg %.2f) has no full stop on the book, and for %s the bot has not acted because %s. Nothing was placed or closed. Check the position and its stop in NinjaTrader now.",
				side, symbol, qty, avg, held.Round(time.Second), why))
	}
	return nil
}

// finishProtectionActLocked ends the hold a proof came from — or keeps it, as a
// first look, when the guard withheld the send. protMu held.
func (at *AutoTrader) finishProtectionActLocked(key string, v protectionVerdict, p *protectionProof) {
	if p.withheld != "" {
		telemetry.IncTraderEvent(at.id, "protection_hold_withheld")
		p.hold.lastWhy = "withheld: " + p.withheld
		at.logWarnf("🧷 protection %s WITHHELD at the send: %s %s ×%d — %s after the second look; nothing was written, the next look is a first one",
			v.Action, p.side, p.symbol, p.qty, p.withheld)
		return
	}
	if at.protHolds[key] == p.hold {
		delete(at.protHolds, key)
	}
	telemetry.IncTraderEvent(at.id, "protection_hold_acted_"+v.Action.String())
}

// protectionProbeLocked is rule 4: send (once) the heartbeat whose ack proves the
// link is caught up. It never waits. protMu held.
func (at *AutoTrader) protectionProbeLocked(h *protectionHold, srv *nt.TCPServer) string {
	if h.probe == nil {
		p, err := srv.SendLinkProbe()
		if err != nil {
			return "the NinjaTrader link could not be checked: " + err.Error()
		}
		h.probe, h.probeAt = p, srv.ReceiptNow()
	}
	return "waiting for NinjaTrader to answer the link check"
}

// protectionOwnDeath is rule 5. why != "" = hold (P0). A MISSING row is not a
// veto. An unreadable ledger cannot rule our own death out, so it holds too,
// for the same 60 s at most. An ENTRY filled after the last exit fill makes the
// held position a NEW one (its row may lag): the dead row says nothing about it.
func protectionOwnDeath(d store.OwnDeath, err error, ev nt.ProtectionEvidence, now time.Time, h *protectionHold) (why string, veto bool) {
	if now.Sub(h.since) > protectionOwnDeathWindow {
		return "", false
	}
	if !ev.Exit.IsZero() && ev.EntryOpened.After(ev.Exit) {
		return "", false
	}
	if err != nil {
		return fmt.Sprintf("the position ledger could not be read (%v), so our own close cannot be ruled out", err), true
	}
	if !d.ByReceipts() {
		return "", false
	}
	closed := time.UnixMilli(d.Last.ExitTime)
	if now.Sub(closed) > protectionOwnDeathWindow {
		return "", false
	}
	// No age in the text: it would change every look and repeat the log line.
	return fmt.Sprintf("our own row %d there was closed by its receipts (%.0f of %.0f) at %s, less than %s ago",
		d.Last.ID, d.ReceiptQty, d.EntryQty, kernel.TableTimeCT(closed)+" CT", protectionOwnDeathWindow), true
}

func agoText(now, t time.Time) string {
	if t.IsZero() {
		return "none"
	}
	return now.Sub(t).Round(time.Millisecond).String() + " ago"
}

// ── THE RE-LOOK GOROUTINE ────────────────────────────────────────────────────

// armProtectionRelookLocked makes sure a re-look runs while a hold is open. The
// Add is under isRunningMutex with isRunning true: Stop clears it there before
// it waits on monitorWg, so no Add can race the Wait. protMu held.
func (at *AutoTrader) armProtectionRelookLocked() {
	if len(at.protHolds) == 0 {
		return
	}
	run := at.protectionRunCtx()
	if run == nil || run.Err() != nil {
		return // not running: the next beat (if any) is the next look
	}
	if at.protRelookRun == run {
		for _, h := range at.protHolds {
			if h.probe != nil { // a beat sent the link check: go wait for its ack now
				select {
				case at.protRelookWake <- struct{}{}:
				default:
				}
				break
			}
		}
		return
	}
	at.isRunningMutex.Lock()
	if !at.isRunning {
		at.isRunningMutex.Unlock()
		return
	}
	at.monitorWg.Add(1)
	at.isRunningMutex.Unlock()
	wake := make(chan struct{}, 1)
	at.protRelookRun, at.protRelookWake = run, wake
	at.goNetted("protection-second-look", func() {
		defer at.monitorWg.Done()
		defer func() {
			at.protMu.Lock()
			at.releaseProtectionRelookLocked(wake)
			at.protMu.Unlock()
		}()
		at.protectionRelookLoop(run, wake)
	})
}

// releaseProtectionRelookLocked drops the registration of the goroutine that owns
// wake, never that of a newer one a beat started in between. protMu held.
func (at *AutoTrader) releaseProtectionRelookLocked(wake chan struct{}) {
	if at.protRelookWake == wake {
		at.protRelookRun, at.protRelookWake = nil, nil
	}
}

// protectionRelookLoop looks again every protectionConfirm() — or as soon as a
// link check is answered — until no hold is left or the Run ends.
func (at *AutoTrader) protectionRelookLoop(run context.Context, wake <-chan struct{}) {
	for {
		if probe := at.outstandingProtectionProbe(); probe != nil {
			probe.Wait(run, nt.HeartbeatAckWait+50*time.Millisecond)
		} else {
			wait := protectionConfirm()
			if wait <= 0 {
				return // reverted to the single look
			}
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
			case <-wake:
				timer.Stop()
				continue // a beat sent a link check
			case <-run.Done():
				timer.Stop()
			}
		}
		if run.Err() != nil {
			return
		}
		srv := at.protectionServer()
		if srv == nil || !at.protectionPass(srv.ReceiptNow(), protectionRelookTrigger, run) {
			return
		}
	}
}

func (at *AutoTrader) outstandingProtectionProbe() *nt.LinkProbe {
	at.protMu.Lock()
	defer at.protMu.Unlock()
	for _, h := range at.protHolds {
		if h.probe != nil {
			return h.probe
		}
	}
	return nil
}

// positionAvg is the average price GetPositions carries (NaN when absent, which
// never equals anything: such a position can never reach a second look).
func positionAvg(p map[string]interface{}) float64 {
	if v, ok := p["entryPrice"].(float64); ok {
		return v
	}
	return math.NaN()
}
