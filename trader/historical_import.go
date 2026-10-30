package trader

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"vl/kernel"
	"vl/logger"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	ninjatrader "vl/trader/ninjatrader"
)

// ── HISTORY IMPORT (wave 101, 2026-09-11) ────────────────────────────────────
//
// Pulls YEARS of per-contract minute history from NT8 through the EXISTING
// wire, on the running bot (the wire holds exactly one client, so the import
// rides the process). Every imported bar is stamped with the contract the
// AddOn actually served (echoed by the C# side from the instrument's real
// ContractName — never derived from a date) and source=historical_import, and
// is written ONLY through store.ImportBars: no upsert, a key collision keeps
// the existing row and is counted. A bar is never deleted and never rewritten.
//
// A10: warn, never panic. A28: one clock per import run. A9: every pull logs
// its contract, row count, first and last bar, and what it skipped.

// HistoryImportSeamOn resolves the env gate (HISTORICAL_IMPORT_SEAM, default
// OFF). The importer refuses to run without it — an import is a store write,
// and store writes are gated.
func HistoryImportSeamOn() bool {
	v := strings.TrimSpace(os.Getenv("HISTORICAL_IMPORT_SEAM"))
	return v == "on" || v == "1" || strings.EqualFold(v, "true")
}

// historyImportTimeout resolves the per-request deadline (minutes; default 45 —
// a provider download for a missing range can take a while; a local-db
// BarsRequest returns in seconds).
func historyImportTimeout() time.Duration {
	if v := strings.TrimSpace(os.Getenv("HISTORICAL_IMPORT_TIMEOUT_MIN")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Minute
		}
	}
	return 45 * time.Minute
}

// ImportResult is the three-state outcome of ONE (contract, timeframe) pull:
// imported/skipped counts are the store's own, `unavailable` names a pull the
// AddOn could not serve and why, `rows`-level facts (first/last bar) are
// READ back from the store after the write, never echoed from the tool.
type ImportResult struct {
	Contract    string `json:"contract"`
	Timeframe   string `json:"timeframe"`
	Status      string `json:"status"` // imported | skipped_only | unavailable | partial
	Imported    int64  `json:"imported"`
	Skipped     int64  `json:"skipped"`
	FirstBarMs  int64  `json:"first_bar_ms"`
	LastBarMs   int64  `json:"last_bar_ms"`
	Unavailable string `json:"unavailable,omitempty"`
	ElapsedSec  int64  `json:"elapsed_sec"`
}

// ImportNamedContractHistory pulls one named contract over [fromMs, toMs) for
// each requested timeframe, sequentially, through the live wire, and writes
// every bar through store.ImportBars. Returns one ImportResult per tf; a
// partial result means some chunks landed before a mid-stream failure and the
// store keeps them (imported history is additive).
func ImportNamedContractHistory(ctx context.Context, st *store.Store, symbol, contract string, tfs []string, fromMs, toMs int64) ([]ImportResult, error) {
	if st == nil {
		return nil, fmt.Errorf("historical import: store required")
	}
	if !HistoryImportSeamOn() {
		return nil, fmt.Errorf("historical import: seam is OFF — set HISTORICAL_IMPORT_SEAM=on to write history (wave 101)")
	}
	if strings.TrimSpace(contract) == "" {
		return nil, fmt.Errorf("historical import: a contract must be NAMED — importing by date inference is the 09-10 bug (wave 101)")
	}
	srv, err := ninjatrader.TCPServer()
	if err != nil || srv == nil {
		return nil, fmt.Errorf("historical import: tcp server unavailable: %v", err)
	}
	start := time.Now() // A28 — ONE clock for this run's elapsed accounting
	var out []ImportResult
	for _, tf := range tfs {
		res := ImportResult{Contract: strings.TrimSpace(contract), Timeframe: strings.TrimSpace(tf)}
		tfStart := start
		imported, skipped, firstMs, lastMs, unavail := pullOne(ctx, srv, st, symbol, strings.TrimSpace(contract), strings.TrimSpace(tf), fromMs, toMs)
		res.ElapsedSec = int64(time.Since(tfStart).Seconds())
		res.Imported, res.Skipped, res.FirstBarMs, res.LastBarMs, res.Unavailable = imported, skipped, firstMs, lastMs, unavail
		switch {
		case unavail != "":
			res.Status = "unavailable"
		case imported == 0 && skipped > 0:
			res.Status = "skipped_only"
		case imported > 0 && skipped > 0:
			res.Status = "partial"
		default:
			res.Status = "imported"
		}
		out = append(out, res)
		logger.Infof("📚 history import: %s %s [%s, %s) → status=%s imported=%d skipped=%d first=%s last=%s unavailable=%q",
			res.Contract, res.Timeframe, msToLabel(fromMs), msToLabel(toMs), res.Status, imported, skipped,
			msToLabel(firstMs), msToLabel(lastMs), unavail)
	}
	_ = start
	return out, nil
}

// pullOne runs ONE (contract, tf, window) pull end-to-end.
func pullOne(ctx context.Context, srv *ntwire.TCPServer, st *store.Store, symbol, contract, tf string, fromMs, toMs int64) (imported, skipped int64, firstMs, lastMs int64, unavail string) {
	if tf == "" {
		return 0, 0, 0, 0, "empty timeframe"
	}
	reqID := newRequestID()
	deadline := time.Now().Add(historyImportTimeout())
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	dataCh, errCh := srv.SubscribeBarsHistoryFor(reqID)
	defer srv.UnsubscribeBarsHistoryFor(reqID)

	req := ntwire.BarsHistoryRequestPayload{
		RequestID: reqID, Symbol: symbol, Contract: contract,
		Timeframe: tf, FromMs: fromMs, ToMs: toMs,
	}
	if err := srv.SendBarsHistoryRequest(req); err != nil {
		return 0, 0, 0, 0, fmt.Sprintf("wire unavailable: %v", err)
	}

	var first, last int64
	for {
		select {
		case <-ctx.Done():
			return imported, skipped, first, last, fmt.Sprintf("cancelled after %d chunks", imported)
		case errP := <-errCh:
			return imported, skipped, first, last, errP.Reason
		case <-time.After(time.Until(deadline)):
			return imported, skipped, first, last, "timeout waiting for the AddOn"
		case p, ok := <-dataCh:
			if !ok {
				return imported, skipped, first, last, "channel closed before the last chunk"
			}
			if p.Contract != "" && !strings.EqualFold(p.Contract, contract) {
				// A24: the C# side names the contract it actually served. A
				// mismatch means the request resolved somewhere else — refuse,
				// never stamp the wrong name.
				return imported, skipped, first, last, fmt.Sprintf("contract mismatch: asked %s, AddOn served %s — nothing written", contract, p.Contract)
			}
			if len(p.Bars) > 0 {
				rows, chunkFirst, chunkLast := importRowsFromChunk(symbol, tf, contract, p.Bars)
				if first == 0 || chunkFirst < first {
					first = chunkFirst
				}
				if chunkLast > last {
					last = chunkLast
				}
				ins, skp, err := st.BarHistory().ImportBars(rows)
				if err != nil {
					logger.Warnf("📚 history import: %s %s chunk write failed: %v — keeping what landed", contract, tf, err)
					return imported, skipped, first, last, err.Error()
				}
				imported += ins
				skipped += skp
			}
			if p.Last {
				return imported, skipped, first, last, ""
			}
		}
	}
}

// importRowsFromChunk turns ONE chunk of AddOn bars into store rows, OPEN-stamped.
//
// m460 (2026-10-30): NT8's Bars.GetTime(i) is the bar's CLOSE (VLHistoryPull.cs
// sends it verbatim), while every row in the bars table is keyed by the bar's
// OPEN (the canonical time contract in provider/ninjatrader/bar_cache.go). The
// live ingest converts at the door (openStampBars + reanchorStubBars); this
// importer stored b.T raw, so every imported row of 2022-2026 sat one full
// timeframe late (1m volume peaked at label 08:31 CT, live at 08:30; the 5m
// label 08:30 equalled the sum of the 1m labels 08:26..08:30). This helper is
// the importer's twin of the live door: same two functions, same order. Rows it
// returns carry Convention=store.ConventionEpochFloorOpen so a later migration
// run can never shift a row that is already right.
//
// first/last are the OPEN stamps of the chunk (the A9 log line names open times).
func importRowsFromChunk(symbol, tf, contract string, bars []ntwire.Bar) (rows []store.BarHistoryDB, first, last int64) {
	opened := ntwire.OpenStampBars(bars, tf) // close-stamp -> OPEN-stamp, a copy
	for i := range opened {
		if o := kernel.ReanchorCMEStub(tf, opened[i].T); o != opened[i].T { // #34: 2h+ 16:00-close stub
			opened[i].T = o
		}
	}
	sort.SliceStable(opened, func(i, j int) bool { return opened[i].T < opened[j].T })
	rows = make([]store.BarHistoryDB, 0, len(opened))
	for _, b := range opened {
		if first == 0 || b.T < first {
			first = b.T
		}
		if b.T > last {
			last = b.T
		}
		rows = append(rows, store.BarHistoryDB{
			Symbol: symbol, TF: tf, OpenTimeMs: b.T,
			O: b.O, H: b.H, L: b.L, C: b.C, V: b.V,
			Convention: store.ConventionEpochFloorOpen, // intraday buckets align on epoch floors, stamped at the OPEN
			Contract:   contract,
			Source:     store.BarSourceHistoricalImport,
		})
	}
	return rows, first, last
}

// newRequestID is a random hex correlation id — the C# side keys its in-flight
// pulls by it.
func newRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("imp-%d", time.Now().UnixNano())
	}
	return "imp-" + hex.EncodeToString(b)
}

// msToLabel renders an epoch-ms for the A9 log line ("n/a" when unknown — a
// field the process cannot know yet prints n/a, never a plausible zero).
func msToLabel(ms int64) string {
	if ms <= 0 {
		return "n/a"
	}
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04Z")
}
