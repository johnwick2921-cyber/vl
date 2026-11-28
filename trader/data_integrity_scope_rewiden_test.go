package trader

import (
	"strings"
	"testing"
	"time"

	"vl/market"
	ntTrader "vl/trader/ninjatrader"
)

// m471 L3-7 — every scope change of one pause episode is told, and the owner's
// last message always matches the current scope. The widen / narrow pushes used
// the fixed keys e.key+"|widen" / e.key+"|narrow"; ownerpush drops a key it has
// already seen, so widen -> narrow -> widen AGAIN sent nothing on the second
// widening and the owner's last word stayed "PAUSE NARROWED - intraday mentor
// setups trade normally again" while every entry path was refused. Production
// seam: monitorTick -> dataIntegrityBeat -> ownerpush.
//
// MUTANT (shown RED): nextScopeKeyLocked returns e.key+"|"+dir (no counter) ->
// "the second WIDEN was neither delivered nor held" -> RED.
func TestSecondWideningOfOnePauseIsTold(t *testing.T) {
	rec, flush := installPush(t)
	at := scopeTextTrader(t, true)
	now := cmeOpenNow(t)
	withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 30_000, Final: true}})
	fk := &intFake{clearsAtMs: ctTimeAt(t, "2026-11-19", "19:57").UnixMilli()}
	swingOnly := func(openMin int) bool { return openMin == ntTrader.IntegritySwingOpenMin }
	fk.blockedFor = swingOnly
	armIntegrityGate(t, fk)

	at.monitorTick(now) // pause (swing only)
	fk.blockedFor = nil
	at.monitorTick(now.Add(time.Minute)) // widen
	fk.blockedFor = swingOnly
	at.monitorTick(now.Add(2 * time.Minute)) // narrow
	fk.blockedFor = nil
	at.monitorTick(now.Add(3 * time.Minute)) // widen AGAIN: every path refused
	flush()

	msgs := rec.all()
	if c := countContaining(msgs, "PAUSE WIDENED"); c != 2 {
		t.Fatalf("the second WIDEN was neither delivered nor held (widened=%d) - the owner's last word is %q", c, msgs[len(msgs)-1])
	}
	if last := msgs[len(msgs)-1]; !strings.Contains(last, "PAUSE WIDENED") {
		t.Fatalf("the owner's last word must match the current scope (every path refused): %q", last)
	}
}

// While the owner cannot be reached, only the NEWEST scope change is delivered:
// each change withdraws the previous one still held, so a delayed NARROWED can
// never arrive after the pause widened again.
func TestHeldScopeChangeIsWithdrawnByTheNextOne(t *testing.T) {
	net, p, clk := m469Push(t, time.Date(2026, 8, 17, 17, 0, 0, 0, time.UTC))
	at := scopeTextTrader(t, true)
	now := cmeOpenNow(t)
	withProvider(t, []market.Kline{{OpenTime: now.UnixMilli() - 30_000, Final: true}})
	fk := &intFake{clearsAtMs: ctTimeAt(t, "2026-11-19", "19:57").UnixMilli()}
	swingOnly := func(openMin int) bool { return openMin == ntTrader.IntegritySwingOpenMin }
	fk.blockedFor = swingOnly
	armIntegrityGate(t, fk)

	net.setDown(true)
	at.monitorTick(now) // pause
	fk.blockedFor = nil
	at.monitorTick(now.Add(time.Minute)) // widen #1 (held)
	fk.blockedFor = swingOnly
	at.monitorTick(now.Add(2 * time.Minute)) // narrow (withdraws widen #1)
	fk.blockedFor = nil
	at.monitorTick(now.Add(3 * time.Minute)) // widen #2 (withdraws the narrow)
	p.Flush()

	net.setDown(false)
	m469Drive(p, clk, 2*time.Minute)
	msgs := net.sent()
	if countContaining(msgs, "TRADING PAUSED") != 1 || countContaining(msgs, "PAUSE WIDENED") != 1 || countContaining(msgs, "PAUSE NARROWED") != 0 {
		t.Fatalf("after the outage want the pause + only the newest scope change (one WIDENED, no NARROWED): %q", msgs)
	}
}
