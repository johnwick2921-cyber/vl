package trader

import (
	"strconv"
	"strings"
	"sync"

	"vl/store"
)

// RELEASE #22 CANCEL-SETTLE (review-1014 EXECUTION.md F-1/F-2, PLANNER.md F2).

// syncCancelSettledOffDrainKey is the durable counter (system_config) for a
// sync cancel whose row NT8's own report settled OUTSIDE the drain — the
// ordered-execution worker applied it, so no frame the drain consumed proved
// it. Counted, never inferred: the drain READ the terminal state.
const syncCancelSettledOffDrainKey = "arm:sync_cancel_settled_off_drain"

// syncCancelOffDrainLogged dedupes the settled-off-drain line per row
// placement (trader + row id + signal): logged ONCE, counted every time.
var syncCancelOffDrainLogged sync.Map

// noteSyncCancelSettledOffDrain records a sync-cancel row that NT8's own
// report settled while the drain was not looking (F-1): counted on the durable
// counter, logged once. The row is NOT re-requested and NO cancel is re-sent —
// the line says so, because that is the whole fix.
func (at *AutoTrader) noteSyncCancelSettledOffDrain(r store.ArmedOrderDB, final string) {
	shown := "n/a"
	if at.store != nil {
		if n, err := store.IncSystemCounter(at.store, syncCancelSettledOffDrainKey); err == nil {
			shown = strconv.Itoa(n)
		}
	}
	if strings.TrimSpace(final) == "" {
		final = "n/a"
	}
	key := at.id + "|" + strconv.FormatInt(r.ID, 10) + "|" + r.SignalID
	if _, seen := syncCancelOffDrainLogged.LoadOrStore(key, struct{}{}); seen {
		return
	}
	at.logInfof("✕ armed sync cancel %s signal=%s: settled %s by NT8's own report (applied by the ordered worker, not by a frame this drain read) — counted settled; NO re-request, NO re-send · settled-off-drain recorded: %s",
		r.Scenario, shortID(r.SignalID), final, shown)
}
