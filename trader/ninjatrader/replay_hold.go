package ninjatrader

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"vl/logger"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
)

// replayHold keeps a replay's rows OUT of the store until the ring has judged
// the replay against the first live bar that follows it.
//
// WHY A SECOND DOOR. The upsert rule stops a replay from overwriting a live
// row. It does nothing for a minute no live bar ever wrote — a restart gap,
// the seconds between a reconnect and its first live bar. A replay on the
// wrong scale (the 2026-09-10 finding: facts 16516009 vs 16518205, ~290 pts
// apart for the same minute) filled those minutes with rows on another price
// scale, labelled 'historical', and every later boot's rehydrate would have
// carried them back into the ring as the oldest bars it holds. So the store
// never receives a replay row until the ring's SeedVerdict says the replay
// and the live feed agree; an off-scale replay is discarded here and said so
// once.
//
// The cost is honest: a minute only the replay saw stays EMPTY in the store
// until a replay on the live scale arrives. An empty minute reads as a gap;
// a wrong-scale minute reads as the largest move of the day.
type replayHold struct {
	mu        sync.Mutex
	pending   map[string][]store.BarHistoryDB // key symbol|tf, ascending by open time
	perKeyCap int
	// the boot line reads these
	written   map[string]int // rows released to the store, per key
	discarded map[string]int // rows discarded as off-scale, per key
	overflow  map[string]int // rows dropped because the hold was full, per key
}

func newReplayHold(perKeyCap int) *replayHold {
	if perKeyCap <= 0 {
		perKeyCap = 4096
	}
	return &replayHold{
		pending: map[string][]store.BarHistoryDB{}, perKeyCap: perKeyCap,
		written: map[string]int{}, discarded: map[string]int{}, overflow: map[string]int{},
	}
}

func holdKey(symbol, tf string) string { return symbol + "|" + tf }

// add queues replay rows for one key. The newest rows win when the hold is
// full — a replay is delivered oldest-first, so the tail is the part that
// abuts the live feed and is the part the verdict is about.
func (h *replayHold) add(symbol, tf string, rows []store.BarHistoryDB) {
	if h == nil || len(rows) == 0 {
		return
	}
	k := holdKey(symbol, tf)
	h.mu.Lock()
	defer h.mu.Unlock()
	merged := append(h.pending[k], rows...)
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].OpenTimeMs < merged[j].OpenTimeMs })
	// One row per minute, the LAST delivered wins: the replay frame and the
	// boot backfill both hand the ring's seed to the hold.
	dedup := merged[:0]
	for i, r := range merged {
		if i+1 < len(merged) && merged[i+1].OpenTimeMs == r.OpenTimeMs {
			continue
		}
		dedup = append(dedup, r)
	}
	merged = dedup
	if len(merged) > h.perKeyCap {
		h.overflow[k] += len(merged) - h.perKeyCap
		merged = merged[len(merged)-h.perKeyCap:]
	}
	h.pending[k] = merged
}

// resolve applies the ring's verdict for one key. It is called on EVERY live
// frame for the key, before the frame's own rows are written, and is a no-op
// when nothing is held or the ring has not judged the seed yet.
//
//   - checked && !offScale → the held rows are written as 'historical' (the
//     upsert rule still keeps every live row that exists).
//   - checked && offScale  → the held rows are discarded, once, loudly.
//   - !checked             → keep holding; the live frame is written regardless.
func (h *replayHold) resolve(symbol, tf string, checked, offScale bool, insert func([]store.BarHistoryDB) error) {
	if h == nil {
		return
	}
	k := holdKey(symbol, tf)
	h.mu.Lock()
	rows := h.pending[k]
	if len(rows) == 0 || !checked {
		h.mu.Unlock()
		return
	}
	delete(h.pending, k)
	if offScale {
		h.discarded[k] += len(rows)
		h.mu.Unlock()
		logger.Warnf("🧯 replay HELD OUT of the store: %s %s — %d replay row(s) DISCARDED, the ring found this replay on another price scale than the live feed (bar-source wave; the P0 above has the closes). The store keeps only what traded live.", symbol, tf, len(rows))
		return
	}
	h.mu.Unlock()
	if err := insert(rows); err != nil {
		logger.Warnf("bars: replay release %s %s failed: %v — %d row(s) NOT written", symbol, tf, err, len(rows))
		return
	}
	h.mu.Lock()
	h.written[k] += len(rows)
	h.mu.Unlock()
	logger.Infof("📼 replay verified on the live scale: %s %s — %d row(s) released to the store as historical (live rows, where they exist, are kept by the upsert rule)", symbol, tf, len(rows))
	// N1 (release #23): the store now holds history the mentor's seed may not
	// have read — announce it; the mentor tick re-seeds its state from the store.
	NoteHistoryRefill(symbol, tf, len(rows), fmt.Sprintf("replay released %d %s %s row(s) to the store", len(rows), symbol, tf))
}

// heldCount is what the boot line reads.
func (h *replayHold) heldCount(symbol string) (held, written, discarded, overflow int) {
	if h == nil {
		return 0, 0, 0, 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for k, rows := range h.pending {
		if strings.HasPrefix(k, symbol+"|") {
			held += len(rows)
		}
	}
	for k, n := range h.written {
		if strings.HasPrefix(k, symbol+"|") {
			written += n
		}
	}
	for k, n := range h.discarded {
		if strings.HasPrefix(k, symbol+"|") {
			discarded += n
		}
	}
	for k, n := range h.overflow {
		if strings.HasPrefix(k, symbol+"|") {
			overflow += n
		}
	}
	return
}

// line renders the hold's part of the 📼 boot line.
func (h *replayHold) line(symbol string) string {
	held, written, discarded, overflow := h.heldCount(symbol)
	s := fmt.Sprintf("replay-hold: held=%d released=%d discarded=%d", held, written, discarded)
	if overflow > 0 {
		s += fmt.Sprintf(" overflow-dropped=%d", overflow)
	}
	return s
}

// the process-wide hold; the persist wire owns it
var barReplayHold = newReplayHold(2 * ntwire.DefaultBarCacheMaxBars)
