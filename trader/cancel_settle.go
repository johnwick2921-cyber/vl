package trader

// RELEASE #22 CANCEL-SETTLE (review-1014 EXECUTION.md F-1/F-2, PLANNER.md F2).

// syncCancelSettledOffDrainKey is the durable counter (system_config) for a
// sync cancel whose row NT8's own report settled OUTSIDE the drain — the
// ordered-execution worker applied it, so no frame the drain consumed proved
// it. Counted, never inferred: the drain READ the terminal state.
const syncCancelSettledOffDrainKey = "arm:sync_cancel_settled_off_drain"
