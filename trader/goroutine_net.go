package trader

// panic-net-complete (2026-09-26): one wrapper for every LONG-LIVED goroutine
// this trader owns. The net is safe.GoNet (ERROR + stack + counter + freeze),
// and the owner key is the trader id so the P1-F freeze contract applies:
// entries refuse, exits keep working.

import (
	"fmt"
	"time"

	"vl/kernel"
	"vl/safe"
)

// goNetted launches fn in a new goroutine under the full panic net with this
// trader as owner and an in-app P0 alert. A panic → ERROR + stack, counter,
// trader FROZEN, P0 alert, goroutine exits cleanly.
func (at *AutoTrader) goNetted(name string, fn func()) {
	safe.GoNet(name, at.id, fn, func(r interface{}) {
		// m469: one alert (and one owner push) per goroutine per CME session
		// day; the old id had no day in it, so the store deduped it forever.
		at.emitAlert("P0", "goroutine_panic", "panic:"+at.id+":"+name+":"+kernel.CMESessionDayKey(time.Now()),
			"💥 Trader goroutine panic — entries frozen (closes still work)",
			fmt.Sprintf("%s: %v", name, r))
	})
}
