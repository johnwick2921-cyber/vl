package trader

import ntTrader "nofx/trader/ninjatrader"

// wireNT8Maintenance installs the installation maintenance hold on one NT8
// TCP trader. It is the ONLY production call site of these four setters, and
// NewAutoTrader calls it unconditionally for every *TCPTrader (both pinned —
// review 3 F2: a whole-body AST walk passed with the calls under 'if false').
//
//	SetEntryPermit        site 4  — the three entry sends take the permit
//	SetEntryHoldCheck     site 4b — the reconnect queue drops held entries
//	SetMaintenanceSource  site 7  — the AddOn is told the hold over the wire
//	SetDroppedEntrySink   M-2     — a dropped entry settles what was recorded
func wireNT8Maintenance(at *AutoTrader, nt *ntTrader.TCPTrader) {
	nt.SetEntryPermit(MaintenanceEntryPermit)
	nt.SetEntryHoldCheck(maintenanceQueueHeld)
	nt.SetMaintenanceSource(maintenanceWireState)
	nt.SetDroppedEntrySink(at.id, at.onMaintenanceDroppedEntry) // keyed by trader id (M2.1)
}
