package ninjatrader

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"nofx/discipline"
	"nofx/logger"
	"nofx/market"
	"nofx/store"
	"nofx/telemetry"
)

// Position reconcile — the durable single-source-of-truth anchor for NT8.
//
// The AI-decision write records entry_price = marketData.CurrentPrice (the last
// 5m BarCache close), a decision-time *reference* that goes stale (and freezes
// entirely when NT8 stops streaming bars). This loop periodically compares the
// open trader_positions rows against the NT8 positions snapshot (the truth) and:
//
//	(b) ENTRY TRUTH  — for a row NT8 still holds, replaces a stale entry_price
//	    with the NT8 position average (Position.AveragePrice). close-sync's
//	    realized-PnL formula is unchanged; it simply consumes a correct entry.
//	ORPHAN CLEAR     — for an OPEN row NT8 reports FLAT, marks it closed so the
//	    phantom clears and GetPositions / the risk gate agree with NT8.
//
// SAFETY: acts ONLY on a positive snapshot (PositionsFor ok == true). When NT8
// has not reported positions (disconnected / not-yet-deployed), it does NOTHING
// — never closing a real position on missing data. A grace window also exempts
// freshly-opened rows whose positions frame may not have arrived yet, and rows
// are only judged against the account NT8 is currently reporting.
const (
	reconcileInterval = 20 * time.Second
	// orphanGraceMs: a row younger than this is not orphan-closed — its positions
	// frame (PositionUpdate) may simply not have arrived yet after the open.
	orphanGraceMs = 120_000
	// flatGraceMs: once a row is observed NT8-flat-but-DB-open, reconcile waits this
	// long before orphan-closing it. NT8 publishes a flat positions snapshot at/around
	// the same instant it sends the position_close frame, but that frame reaches
	// close-sync a beat later; orphan-closing immediately makes close-sync find no
	// open row and skip → the real ×pv P&L is lost (the reconcile-FIRST race that the
	// PART-1a status guard alone did NOT cover). 60s (3 reconcile cycles) is ample
	// headroom for the frame + close-sync, while bounding how long a genuinely-
	// uncaptured row lingers before it falls to reconcile_flat "—".
	flatGraceMs = 60_000
	// reconcileDivergenceGraceMs (A4/G4): a qty divergence (NT8 held ≠ DB belief)
	// must PERSIST this long before it freezes the trader — a fill frame can be
	// momentarily in flight, so a single-pass divergence is within tolerance. 60s
	// (3 reconcile passes) matches the other reconcile grace windows.
	reconcileDivergenceGraceMs = 60_000
	// untrackedGraceMs: an NT8-held position with NO open DB row (manual NT8
	// entry / unrecorded fill) must PERSIST this long before reconcile
	// materializes an OPEN row for it. A bot-opened row lands within seconds of
	// its fill, so 60s (3 passes) only ever fires for genuinely untracked
	// positions — and still beats the 120s priced-close park grace, so a close
	// frame that arrived while the position was untracked can be consumed with
	// its real exit price.
	untrackedGraceMs = 60_000
	// entryConfirmGraceMs (C8, 2026-08-25): an entry whose signal never produced
	// a fill within this window is dropped from the pending map (rejected /
	// AddOn drop) — it must never linger as a would-be position.
	entryConfirmGraceMs = 45_000
)

// StartPositionReconcile launches the periodic reconcile goroutine (idempotent
// via reconcileOnce). Wired next to StartCloseSync for the ninjatrader exchange.
func (t *TCPTrader) StartPositionReconcile(traderID, exchangeID, exchangeType string, st *store.Store) {
	if st == nil {
		return
	}
	// GAR-F1 — wire the store handle BEFORE the repair pass so the reconcile
	// goroutine and MoveStopToBreakeven can resolve materialized rows' entry
	// identities (the #566 dead-cell fix).
	t.mu.Lock()
	t.st = st
	if t.entryOrderID == nil {
		t.entryOrderID = make(map[string]string)
	}
	t.mu.Unlock()
	t.reconcileOnce.Do(func() {
		// F3 (LONDON-FORENSICS 2026-08-28) — one-time idempotent repair: positions
		// materialized before the lineage stamp existed (live proof: pos #567)
		// get their armed-fill plan linkage back from the armed ledger.
		if n := RepairArmedLineage(st, traderID); n > 0 {
			logger.Infof("🩹 RepairArmedLineage: stamped %d position(s) with their armed-fill plan linkage (the #567 class)", n)
		}
		go func() {
			ticker := time.NewTicker(reconcileInterval)
			defer ticker.Stop()
			for range ticker.C {
				t.reconcilePositions(traderID, exchangeID, exchangeType, st)
			}
		}()
		logger.Infof("🔧 NinjaTrader position-reconcile started (anchors entry_price to NT8 avg + clears orphan rows)")
	})
}

// reconcilePositions runs one reconcile pass. See file header for semantics.
func (t *TCPTrader) reconcilePositions(traderID, exchangeID, exchangeType string, st *store.Store) {
	// Reconcile against THIS trader's OWN bound account, not the shared connection
	// CurrentAccount() (display-switchable). Otherwise viewing another account would
	// make reconcile read the WRONG account's positions and mis-clear this trader's
	// DB rows as orphans. Empty boundAccount → PositionsFor("") !ok → early return
	// (never touches the DB), preserving the ef550df7 refuse semantics.
	acct := t.boundAccount
	snap, ok := t.server.PositionsFor(acct)
	if !ok {
		// NT8 has not reported positions for this account — do NOT touch the DB.
		return
	}

	// C8 (2026-08-25) — sweep stale UNCONFIRMED entries: a pending signal that
	// never produced a fill (NT8 rejected it, or the AddOn dropped the frame)
	// must not linger as a would-be position. Drop it and alarm; NT8 truth is
	// what the snapshot below says.
	t.pendingMu.Lock()
	var staleSignals []string
	cutoff := time.Now().UTC().UnixMilli() - entryConfirmGraceMs
	for sid, atMs := range t.pendingAt {
		if atMs < cutoff {
			staleSignals = append(staleSignals, sid)
		}
	}
	for _, sid := range staleSignals {
		delete(t.pending, sid)
		delete(t.pendingAt, sid)
		// Hygiene: a swept signal can never fill, so it must not stay as the
		// "current entry" for GetOrderStatus either.
		t.mu.Lock()
		if t.lastEntrySignalID == sid {
			t.lastEntrySignalID = ""
		}
		t.mu.Unlock()
	}
	t.pendingMu.Unlock()
	if len(staleSignals) > 0 {
		suffix := "y"
		if len(staleSignals) > 1 {
			suffix = "ies"
		}
		logger.Warnf("🧹 C8 reconcile: dropped %d UNCONFIRMED pending entr%s (no fill within %ds) — NT8 never confirmed; no position recorded.", len(staleSignals), suffix, entryConfirmGraceMs/1000)
	}

	// NT8-held positions keyed "SYMBOL|SIDE" → average price (canonical form).
	held := make(map[string]float64, len(snap))
	heldQty := make(map[string]float64, len(snap))
	for _, p := range snap {
		if p.Quantity == 0 {
			continue
		}
		side := "LONG"
		if strings.EqualFold(p.Side, "short") {
			side = "SHORT"
		}
		key := market.Normalize(p.Symbol) + "|" + side
		held[key] = p.AvgPrice
		heldQty[key] = float64(p.Quantity)
	}

	rows, err := st.Position().GetOpenPositions(traderID)
	if err != nil {
		logger.Warnf("ninjatrader/tcp: reconcile list open positions failed: %v", err)
		return
	}
	nowMs := time.Now().UTC().UnixMilli()
	if t.flatSince == nil {
		t.flatSince = make(map[int64]int64)
	}
	seenFlat := make(map[int64]bool) // rows observed flat-but-open this pass (for flatSince pruning)
	for _, row := range rows {
		// Only judge rows for the account NT8 is currently reporting (avoids
		// closing another account's positions during a transient account switch).
		if row.Account != "" && acct != "" && row.Account != acct {
			continue
		}
		key := row.Symbol + "|" + row.Side
		if avg, isHeld := held[key]; isHeld {
			// (b) Entry truth — anchor a stale entry to the NT8 average.
			if avg > 0 && row.EntryPrice != avg {
				if err := st.Position().UpdateEntryPrice(row.ID, avg); err != nil {
					logger.Warnf("ninjatrader/tcp: reconcile entry update failed (row %d): %v", row.ID, err)
				} else {
					logger.Infof("🔧 reconcile: %s %s entry %.2f → NT8 avg %.2f (row %d)",
						row.Symbol, row.Side, row.EntryPrice, avg, row.ID)
				}
			}
			// (c) QTY TRUTH — alarm when NT8's held contract count diverges from the
			// DB row. This is the id=45→id=46 net-2 tell: an unclosed prior contract
			// (rejected flatten) the next entry netted onto, so NT8 holds 2 while the
			// row says 1. Log-only (no auto-flatten): loudly flagging the divergence
			// is the safe SIM action; the close routing now prevents the phantom that
			// causes it, and the entry-anchor above keeps the price honest.
			if hq, ok := heldQty[key]; ok && row.Quantity > 0 && hq != row.Quantity {
				logger.Warnf("🚨 reconcile QTY DIVERGENCE: %s %s — NT8 holds %.0f but DB row %d has %.0f. Likely an unclosed prior contract netted onto by a later entry (id=45→46 class). DB qty NOT auto-changed — investigate.",
					row.Symbol, row.Side, hq, row.ID, row.Quantity)
				// A4 (G4) — belief≠broker divergence. Debounce across passes (a fill
				// frame may be momentarily in flight); once it PERSISTS beyond the grace,
				// FREEZE the trader so it can't open on top of a mis-believed position.
				// Open-position management continues; the owner clears via the API.
				if t.divergeSince == nil {
					t.divergeSince = make(map[int64]int64)
				}
				if t.divergeSince[row.ID] == 0 {
					t.divergeSince[row.ID] = nowMs
				} else if nowMs-t.divergeSince[row.ID] >= reconcileDivergenceGraceMs {
					reason := fmt.Sprintf("reconcile qty divergence persisted: %s %s NT8=%.0f DB row %d=%.0f", row.Symbol, row.Side, hq, row.ID, row.Quantity)
					if discipline.FreezeTrader(traderID, reason, nowMs) {
						logger.Errorf("🚨 A4 FREEZE: %s — trader FROZEN (new entries blocked; management continues). Clear via /api/risk/clear-freeze after reconciling.", reason)
					}
				}
			} else if t.divergeSince != nil {
				delete(t.divergeSince, row.ID) // qty matches → reset the debounce
			}
		} else {
			// Orphan — NT8 reports flat for this (symbol,side) but the row is OPEN.
			// Skip very fresh rows (positions frame may still be in flight).
			if nowMs-row.EntryTime < orphanGraceMs {
				continue
			}
			// FLAT-GRACE (the reconcile-FIRST race). NT8 reports this row FLAT in the
			// snapshot at/around the same instant it sends the position_close frame, but
			// that frame reaches close-sync a beat later. If we orphan-close the still-
			// OPEN row now, close-sync then finds no open row and skips → the real ×pv
			// P&L is lost. So DEFER: on first observing the row flat-but-open, record the
			// time and skip; only orphan-close after it's been continuously flat
			// ≥ flatGraceMs (close-sync gets first crack — it closes the row CLOSED/sync
			// and the next pass prunes it below). A genuinely-uncaptured row (no frame
			// ever) is still OPEN after the grace → orphaned → reconcile_flat "—".
			seenFlat[row.ID] = true
			if t.flatSince[row.ID] == 0 {
				t.flatSince[row.ID] = nowMs
				logger.Infof("⏳ reconcile: %s %s row=%d NT8-flat — deferring orphan-close (awaiting close-sync frame)", row.Symbol, row.Side, row.ID)
				continue
			}
			if nowMs-t.flatSince[row.ID] < flatGraceMs {
				continue // within grace — keep deferring to close-sync
			}
			// PRICED-FRAME FALLBACK: before writing the unknown marker, check whether a
			// position_close frame carrying the REAL exit price arrived for this
			// (account,symbol,side) but found no open row when it landed (owner-routing
			// miss / frame-before-entry race). If so, close with the REAL exit + P&L
			// (reason "sync" → counted in stats, shown as a real number) instead of a
			// fabricated exit=entry pnl=0. takePricedClose is idempotent (consumed once).
			if exitPx, okp := takePricedClose(row.Account, row.Symbol, row.Side, nowMs); okp {
				pv := market.FuturesPointValue(row.Symbol)
				if pv <= 0 {
					pv = 1
				}
				pnl := 0.0
				if row.EntryPrice > 0 {
					if row.Side == "LONG" {
						pnl = (exitPx - row.EntryPrice) * row.Quantity * pv
					} else {
						pnl = (row.EntryPrice - exitPx) * row.Quantity * pv
					}
				}
				closed, perr := st.Position().ClosePosition(row.ID, exitPx, "reconcile_priced", pnl, 0, "sync")
				if perr == nil && closed && OnPositionClosed != nil {
					OnPositionClosed(row.TraderID, row.ID) // Phase 4: post-exit rescan
				}
				delete(t.flatSince, row.ID)
				if perr != nil {
					logger.Warnf("ninjatrader/tcp: reconcile priced-close failed (row %d): %v", row.ID, perr)
				} else if closed {
					logger.Infof("💵 reconcile: recovered PARKED exit price for %s %s row=%d exit=%.2f pnl=%.2f (close-sync frame had no open row when it arrived)", row.Symbol, row.Side, row.ID, exitPx, pnl)
				}
				continue
			}
			// CLASS-27 NETTING-FILL FALLBACK (2026-08-31): a NETTING close emits
			// NO position_close frame at all — an opposite-side ENTRY fill
			// flattened the account (live proof: the S1 long closed by the S3
			// SellShort @ 29459). Derive the real exit from the fill ring; the
			// LATEST opposite-side fill in the flat window IS the close.
			if exitPx, okn := t.takeNettingExit(row.Account, row.Symbol, row.Side, t.flatSince[row.ID], nowMs); okn {
				pv := market.FuturesPointValue(row.Symbol)
				if pv <= 0 {
					pv = 1
				}
				pnl := 0.0
				if row.EntryPrice > 0 {
					if row.Side == "LONG" {
						pnl = (exitPx - row.EntryPrice) * row.Quantity * pv
					} else {
						pnl = (row.EntryPrice - exitPx) * row.Quantity * pv
					}
				}
				closed, nerr := st.Position().ClosePosition(row.ID, exitPx, "netting_fill", pnl, 0, "sync")
				delete(t.flatSince, row.ID)
				if nerr == nil && closed && OnPositionClosed != nil {
					OnPositionClosed(row.TraderID, row.ID) // Phase 4: post-exit rescan
				}
				switch {
				case nerr != nil:
					logger.Warnf("ninjatrader/tcp: reconcile netting-close failed (row %d): %v", row.ID, nerr)
				case closed:
					logger.Infof("💵 reconcile: recovered NETTING exit for %s %s row=%d exit=%.2f pnl=%.2f (opposite-side entry fill flattened the account; no close frame exists)", row.Symbol, row.Side, row.ID, exitPx, pnl)
				default:
					logger.Infof("✓ reconcile: row=%d already closed by close-sync (kept real P&L); skip", row.ID)
				}
				continue
			}
			// Past the grace, no priced frame, no netting fill → the exit is
			// GENUINELY unknown. Mark UNRESOLVED and alarm. NEVER fabricate
			// exit=entry (the old fake-$0 that hid today's +$92.00 for 26
			// minutes). ClosePositionUnresolved is guarded on status='OPEN'
			// (PART-1a), so a last-moment close-sync win is still kept.
			closed, err := st.Position().ClosePositionUnresolved(row.ID,
				"class-27: NT8 flat, no close frame, no netting fill — exit price UNKNOWN")
			delete(t.flatSince, row.ID)
			switch {
			case err != nil:
				logger.Warnf("ninjatrader/tcp: reconcile unresolved-close failed (row %d): %v", row.ID, err)
			case closed:
				logger.Errorf("🚨 class-27 UNRESOLVED EXIT: %s %s row=%d closed with close_reason=unresolved — NT8 flat ≥%ds with NO close frame and NO netting fill. Real P&L is unknown (visible gap, not a fake zero).",
					row.Symbol, row.Side, row.ID, flatGraceMs/1000)
				telemetry.RecordError(traderID, "exit_unresolved",
					fmt.Sprintf("%s %s row=%d flat with no close frame and no netting fill", row.Symbol, row.Side, row.ID), telemetry.CostNone)
				if OnPositionClosed != nil {
					OnPositionClosed(row.TraderID, row.ID) // Phase 4: post-exit rescan
				}
			default:
				// Row was already closed by close-sync between the snapshot and now —
				// the status guard preserved close-sync's real ×pv P&L. No clobber.
				logger.Infof("✓ reconcile: row=%d already closed by close-sync (kept real P&L); skip", row.ID)
			}
		}
	}

	// Prune flat-timers for rows no longer orphan-candidates (close-sync closed them,
	// or NT8 re-holds them) so the map can't leak and a recycled row id starts fresh.
	for id := range t.flatSince {
		if !seenFlat[id] {
			delete(t.flatSince, id)
		}
	}

	// (e) UNTRACKED NT8 POSITIONS — NT8 holds a position this trader has NO open
	// row for (a MANUAL NT8 entry, or an entry whose fill was never recorded).
	// The row-driven loop above never sees these, so they were invisible to
	// history: when they later closed, close-sync found no owner row, DROPPED
	// the priced close, and the real P&L was lost forever (2026-08-25 incident:
	// manual SHORT @29310 held 8.5 min, closed by the bot @29350.50 → −81.00
	// realized, never recorded). Fix: after untrackedGraceMs of persistence —
	// a bot-opened row lands within seconds of its fill, so this only fires for
	// genuinely untracked positions — materialize an OPEN row anchored to the
	// NT8 average (Source="reconcile", Account=bound account) so close-sync can
	// record the real exit + ×pv P&L. A priced close parked while the position
	// was still untracked is consumed immediately, preserving the real exit.
	if t.untrackedSince == nil {
		t.untrackedSince = make(map[string]int64)
	}
	seenHeld := make(map[string]bool)
	for key, avg := range held {
		seenHeld[key] = true
		sym, side := key, "LONG"
		if i := strings.LastIndex(key, "|"); i >= 0 {
			sym, side = key[:i], key[i+1:]
		}
		// Authoritative account-aware check (not the row list): a row owned by a
		// different account must not mask a held position on OUR bound account.
		owner, oerr := st.Position().GetOpenPositionByAccountSymbol(acct, sym, side)
		if oerr != nil {
			logger.Warnf("ninjatrader/tcp: reconcile untracked owner lookup failed (%s %s acct=%q): %v", sym, side, acct, oerr)
			continue
		}
		if owner == nil {
			// CLASS-27 FIX 3 (2026-08-31): the dedupe race that birthed the
			// 577+578 duplicates — an armed-materialized row can carry
			// account="" (its order_update frame predates the account binding),
			// so the account-scoped lookup above misses it and reconcile
			// materializes a SECOND row for the same NT8 position. Retry
			// account-agnostically; if found, backfill the bound account so the
			// later close-sync frame (which carries the account) finds its owner.
			owner, oerr = st.Position().GetOpenPositionByAccountSymbol("", sym, side)
			if oerr != nil {
				logger.Warnf("ninjatrader/tcp: reconcile untracked owner lookup (account-agnostic) failed (%s %s): %v", sym, side, oerr)
				continue
			}
			if owner != nil && owner.Account == "" && acct != "" {
				if aerr := st.Position().SetPositionAccount(owner.ID, acct); aerr != nil {
					logger.Warnf("ninjatrader/tcp: reconcile account backfill failed (row %d): %v", owner.ID, aerr)
				} else {
					logger.Infof("🔧 reconcile: backfilled account %q on armed row %d (%s %s) — dedupe + close-sync ownership", acct, owner.ID, sym, side)
				}
			}
		}
		if owner != nil {
			delete(t.untrackedSince, key) // tracked now — drop any debounce
			continue
		}
		if t.untrackedSince[key] == 0 {
			t.untrackedSince[key] = nowMs
			logger.Infof("🧩 reconcile: NT8 holds UNTRACKED position %s %s @ avg %.2f (no open row) — materializing after %ds if it persists", sym, side, avg, untrackedGraceMs/1000)
			continue
		}
		if nowMs-t.untrackedSince[key] < untrackedGraceMs {
			continue
		}
		firstSeen := t.untrackedSince[key] // W1b E15: anchors the fill ring's window
		qty := heldQty[key]
		if qty <= 0 {
			qty = 1
		}
		row := &store.TraderPosition{
			TraderID:           traderID,
			ExchangeID:         exchangeID,
			ExchangeType:       exchangeType,
			ExchangePositionID: fmt.Sprintf("reconcile_%s_%s_%d", sym, side, nowMs),
			Symbol:             sym,
			Side:               side,
			Quantity:           qty,
			EntryQuantity:      qty,
			EntryPrice:         avg,
			EntryTime:          nowMs,
			Leverage:           1,
			Status:             "OPEN",
			Source:             "reconcile",
			// ATTRIBUTION E1-lite (2026-09-02): this path knows an account, a
			// symbol, a side and an average price — no order of ours, so no
			// lineage to recover. It is stamped UNRESOLVABLE rather than left
			// "", which is indistinguishable from "not yet stamped". Measured:
			// 3 rows in three weeks reached the DB this way (566, 571, 580),
			// none with an arm within 30 minutes. We never guess a link.
			PlanID:    store.PlanUnresolvable,
			Account:   acct,
			CreatedAt: nowMs,
			UpdatedAt: nowMs,
		}
		logger.Warnf("🔗 attribution: materialized %s %s @ %.2f with NO recoverable lineage — plan_id=%s (never joinable; counted in the boot line)",
			sym, side, avg, store.PlanUnresolvable)
		if err := st.Position().CreateOpenPosition(row); err != nil {
			logger.Warnf("ninjatrader/tcp: reconcile materialize untracked %s %s failed: %v", sym, side, err)
			delete(t.untrackedSince, key) // retry on a later pass
			continue
		}
		// F3 (LONDON-FORENSICS 2026-08-28) — stamp the armed-fill lineage NOW:
		// the fill-time stamp failed because this row did not exist yet (live proof:
		// pos #567 landed with plan_version 0 / adherence grade F).
		// GAR-F1 (2026-08-28) — the returned signal identity is cached on the
		// trader so move_stop/trailing can address the live bracket.
		//
		// W1b E15 — the fill ring FIRST: the exact signal of this position's own
		// fill beats a price guess. Only with no same-side evidence in the window
		// does the price-match fallback run, and (FOLD-4) only over arms filled in
		// that same window whose signal explains no position yet; an ambiguous or
		// unreadable answer leaves the row untagged (a guess is fabricated lineage).
		// W1b FOLD-6 — origin is what the 🧩 line below says the position WAS:
		// "manual/NT8-side" only when nothing evidenced this trader's own entry.
		origin := "manual/NT8-side entry (no fill-ring or in-window armed-fill evidence of this trader's own entry)"
		switch f, verdict, why := t.lateEntryFillFor(st, acct, sym, side, firstSeen); verdict {
		case lateFillOne:
			if sig, what := t.tagLateEntryFill(st, traderID, exchangeID, row.ID, sym, side, f); sig != "" {
				t.rememberEntryOrderID(sym, side, sig)
				origin = what
			} else {
				origin = fmt.Sprintf("UNTAGGED entry (fill-ring signal %s %s)", f.SignalID, what)
			}
		case lateFillUnresolved:
			logger.Warnf("🔗 attribution: pos %d (%s %s) — fill ring %s — left UNTAGGED; no price-match guess", row.ID, sym, side, why)
			origin = fmt.Sprintf("UNTAGGED entry (fill ring %s)", why)
		default:
			switch stamped, sig, failed := stampArmedLineageInWindow(st, traderID, row.ID, sym, side, avg, firstSeen); {
			case stamped:
				t.rememberEntryOrderID(sym, side, sig)
				if sig == "" {
					sig = "(none)"
				}
				origin = fmt.Sprintf("this trader's armed fill matched by price in the window (signal %s)", sig)
			case failed != "":
				origin = fmt.Sprintf("UNTAGGED entry (%s)", failed)
			}
		}
		logger.Warnf("🧩 reconcile: MATERIALIZED untracked NT8 position %s %s qty=%.0f @ %.2f (acct=%s) — %s now tracked; its close will record real P&L", sym, side, qty, avg, acct, origin)
		delete(t.untrackedSince, key)
		// A close frame may have arrived while the row was still untracked (the
		// DROPPED → parked path). Consume it now with the real exit + ×pv P&L.
		if exitPx, okp := takePricedClose(acct, sym, side, nowMs); okp {
			pv := market.FuturesPointValue(sym)
			if pv <= 0 {
				pv = 1
			}
			pnl := 0.0
			if side == "LONG" {
				pnl = (exitPx - avg) * qty * pv
			} else {
				pnl = (avg - exitPx) * qty * pv
			}
			if closed, perr := st.Position().ClosePosition(row.ID, exitPx, "reconcile_priced", pnl, 0, "sync"); perr == nil && closed {
				if OnPositionClosed != nil {
					OnPositionClosed(traderID, row.ID)
				}
				logger.Infof("💵 reconcile: untracked %s %s closed with parked exit %.2f pnl=%.2f (was untracked when the frame arrived)", sym, side, exitPx, pnl)
			} else if perr != nil {
				logger.Warnf("ninjatrader/tcp: reconcile untracked priced-close failed (row %d): %v", row.ID, perr)
			}
		}
	}
	// Prune untracked debounces for positions NT8 no longer holds (closed before
	// materialization) so the map can't leak.
	for key := range t.untrackedSince {
		if !seenHeld[key] {
			delete(t.untrackedSince, key)
		}
	}
}

// ── F3 (LONDON-FORENSICS 2026-08-28) — armed-fill lineage stamping ──────────
// The fill-time stamp (armed executor) runs when the order_update frame lands,
// but a reconcile-materialized position row does not exist yet at that instant
// — live proof: pos #567, the first live armed fill, landed with
// plan_version 0 / plan_band "" / adherence grade F. The materialization site
// now stamps from the armed ledger, and the repair pass back-fills the class.

// StampArmedLineageIfMatched stamps one position row with its armed-fill plan
// linkage when a matching FILLED ledger row exists: same trader, same side, and
// entry price within one tick of the ledger's entry. Returns (true, signalID)
// when stamped — the signalID is the armed ledger's entry identity, persisted
// as the row's entry_order_id (GAR-F1) so move_stop/trailing can address the
// position on the wire.
// armedFillPriceFor resolves the TRUE fill price of a filled ledger row.
// PRE-SUNDAY F2 (2026-08-28): entry_px DRIFTS on re-arm — the v6 re-spec
// overwrote the v1 entry that actually filled (#568: row entry 29702 vs real
// fill 29642.00; #570: row entry 29480 vs fill 29463.25). The authoritative
// value is fill_price (written at fill time) or the "fill@…" reason.
func armedFillPriceFor(r store.ArmedOrderDB) float64 {
	if r.FillPrice > 0 {
		return r.FillPrice
	}
	if i := strings.Index(r.StateReason, "fill@"); i >= 0 {
		rest := r.StateReason[i+len("fill@"):]
		end := 0
		for end < len(rest) && ((rest[end] >= '0' && rest[end] <= '9') || rest[end] == '.') {
			end++
		}
		if v, err := strconv.ParseFloat(rest[:end], 64); err == nil && v > 0 {
			return v
		}
	}
	return r.EntryPx
}

func StampArmedLineageIfMatched(st *store.Store, traderID string, posID int64, sym, side string, entryPx float64) (bool, string) {
	rows, err := st.ArmedOrders().ListFilled(traderID, 20)
	if err != nil || len(rows) == 0 {
		return false, ""
	}
	if r, ok := matchArmedFillByPrice(rows, sym, side, entryPx); ok {
		return stampArmedLineageFromRow(st, posID, r)
	}
	return false, ""
}

// matchArmedFillByPrice is the price matcher: the first row, in the order
// given, of the same side whose true fill price is within one tick of entryPx.
func matchArmedFillByPrice(rows []store.ArmedOrderDB, sym, side string, entryPx float64) (store.ArmedOrderDB, bool) {
	tick := market.FuturesTickSize(sym)
	if tick <= 0 {
		tick = 0.25
	}
	for _, r := range rows {
		if !strings.EqualFold(r.Side, side) {
			continue
		}
		fillPx := armedFillPriceFor(r)
		if fillPx < entryPx-tick || fillPx > entryPx+tick {
			continue
		}
		return r, true
	}
	return store.ArmedOrderDB{}, false
}

// stampArmedLineageFromRow writes one FILLED ledger row's plan linkage and
// signal identity onto a position row — the stamp body StampArmedLineageIfMatched
// runs on a price match, and W1b E15 runs on EXACT ring evidence (the fill's own
// signal) without any price guess. Returns (true, signalID) when stamped.
func stampArmedLineageFromRow(st *store.Store, posID int64, r store.ArmedOrderDB) (bool, string) {
	tradeDate := r.PlanID
	if i := strings.Index(r.PlanID, ":"); i > 0 {
		tradeDate = r.PlanID[:i]
	}
	if err := st.Position().SetPlanLinkFull(posID, r.Version, r.Scenario, true, "armed_fill", r.PlanID, tradeDate, r.Session); err != nil {
		logger.Warnf("🧩 reconcile: armed lineage stamp failed (pos %d): %v", posID, err)
		return false, ""
	}
	// GAR-F1 — the materialized row gets the armed ledger's signal identity
	// so move_stop/trailing can find the live bracket (the #566 dead cell).
	if r.SignalID != "" {
		if err := st.Position().SetEntryOrderID(posID, r.SignalID); err != nil {
			logger.Warnf("🧩 reconcile: armed entry-order-id stamp failed (pos %d): %v", posID, err)
		}
	}
	// PRE-REOPEN F4 — the fill-time stamp deferred (position row didn't
	// exist yet) leaves a stamp_pending marker on the ledger row; the
	// materialization completes the stamp NOW and clears it.
	if strings.HasSuffix(r.StateReason, ";stamp_pending") {
		_ = st.ArmedOrders().SetState(r.ID, "filled", strings.TrimSuffix(r.StateReason, ";stamp_pending"))
	}
	// F3 GAP (2026-09-03, found via nofx-89's 09-01 audit): fill_quantity is
	// stamped HERE too. The fill-time stamp in stampArmedFillLineage returns
	// early on this very path — the position row is not materialized when the
	// fill frame lands — so stamping only there covered the minority case.
	// The 09-01 audit recorded 584 of 586 armed fills carrying
	// ";stamp_pending", and armed row 35 today took the same path and still
	// reads fill_quantity=0 with the stamp live.
	if qty := st.Position().QuantityOf(posID); qty > 0 {
		if err := st.ArmedOrders().SetFillQuantity(r.ID, int(qty)); err != nil {
			logger.Warnf("🧩 reconcile: armed fill-quantity stamp failed (row %d): %v", r.ID, err)
		}
	}
	logger.Infof("🧩 reconcile: armed-fill lineage stamped — pos %d ← %s v%d %s (fill %.2f, entry_id %s)", posID, r.PlanID, r.Version, r.Scenario, armedFillPriceFor(r), r.SignalID)
	return true, r.SignalID
}

// RepairArmedLineage back-fills plan linkage for this trader's positions that
// have none (plan_version = 0). Idempotent: a stamped row leaves the scan.
func RepairArmedLineage(st *store.Store, traderID string) int {
	rows, err := st.Position().ListUnlinked(traderID, 200)
	if err != nil {
		logger.Warnf("🧩 reconcile: armed-lineage repair scan failed: %v", err)
		return 0
	}
	n := 0
	for _, p := range rows {
		if p.PlanVersion != 0 {
			continue
		}
		if stamped, _ := StampArmedLineageIfMatched(st, traderID, p.ID, p.Symbol, p.Side, p.EntryPrice); stamped {
			n++
			// REGRADE RESET (owner ruling 2026-09-03) — keyed on "lineage
			// was just stamped on this row", NEVER on a grade letter.
			//
			// The predecessor read `AdherenceGrade == "F"`. That is not an
			// impossible value but a SUBSET: an uncited close grades base D
			// and steps to F under either penalty (InNoTrade, !InKillzone),
			// so the repair silently SUCCEEDED on penalised uncited rows
			// (566, 571 → F) and silently FAILED on clean ones (580 → D) —
			// which is why it survived, since a spot-check lands on a
			// working case.
			//
			// Keying on the stamp is also the only correct rule: if lineage
			// was just written onto this row, whatever grade it carries was
			// computed WITHOUT that lineage and is stale by construction,
			// whatever letter it happens to be. A row nothing stamps is not
			// touched — 580 is uncited and has EARNED its D.
			if p.Status == "CLOSED" && p.AdherenceGrade != "" {
				if err := st.Position().SetAdherence(p.ID, ""); err != nil {
					logger.Warnf("🩹 RepairArmedLineage: adherence reset failed (pos %d): %v", p.ID, err)
				} else {
					logger.Infof("🩹 RepairArmedLineage: pos %d lineage stamped (v%d %s) — grade %q cleared for regrading",
						p.ID, p.PlanVersion, p.CitedScenarioID, p.AdherenceGrade)
				}
			}
		}
	}
	return n
}

// UntrackedGraceMs is how long an NT8-held position with no open DB row must
// persist before the reconciler materializes it (W-EXEC-TRUTH W0 (c): the
// pre-open reconcile treats a ledger fill younger than twice this as not yet
// materialized, so it explains the position instead of flattening it).
const UntrackedGraceMs = untrackedGraceMs
