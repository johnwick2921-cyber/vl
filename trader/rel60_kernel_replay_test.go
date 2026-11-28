package trader

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"testing"
	"time"

	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
)

// TestRel60KernelReplay — the release #60 integration's before/after harness
// (opt-in, READ-ONLY; compiles unchanged at dev c6777e230 so the SAME file is
// run at the base and at the merged head). It ticks the PRODUCTION evaluator
// config (AutoTrader.mentorEvaluatorConfig over a stored strategy JSON — the
// owner's stored values, resolved by the same resolvers as live) over an
// exported MNQ 1m tape the way the live loop does (one Tick per closed 1m bar,
// the last kernel.AISVPBarCount=2000 bars as the window), sizes every ENTRY
// intent through the PRODUCTION sizing call site (mentorSizeFor) and writes
// one JSON line per entry to REL60_REPLAY_OUT. The diff (APPEARS /
// DISAPPEARS / CHANGED, matched to trader_positions) is done outside.
//
// Limits [B]: RealFillOnly stays true and no broker fill is fed, so the leg
// budget / loss box / done-after-win never see a fill (the same blind spot as
// TestM60P1TapeReplayDiff); the strategy is today's stored row, not the row as
// it stood on each day.
//
//	REL60_REPLAY_CSV=/path/mnq_1m.csv        ot,o,h,l,c,v
//	REL60_REPLAY_STRATEGY=/path/strategy.json the stored strategies.config
//	REL60_REPLAY_FROM=2026-10-01             first CT date whose entries are written
//	REL60_REPLAY_OUT=/path/entries.jsonl
func TestRel60KernelReplay(t *testing.T) {
	path := os.Getenv("REL60_REPLAY_CSV")
	if path == "" {
		t.Skip("set REL60_REPLAY_CSV / REL60_REPLAY_STRATEGY / REL60_REPLAY_FROM / REL60_REPLAY_OUT")
	}
	ct, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	from, err := time.ParseInLocation("2006-01-02", os.Getenv("REL60_REPLAY_FROM"), ct)
	if err != nil {
		t.Fatalf("REL60_REPLAY_FROM: %v", err)
	}
	raw, err := os.ReadFile(os.Getenv("REL60_REPLAY_STRATEGY"))
	if err != nil {
		t.Fatal(err)
	}
	sc, err := (&store.Strategy{ID: "rel60-replay", Config: string(raw)}).ParseConfig()
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	at := &AutoTrader{id: "t-rel60-replay", config: AutoTraderConfig{StrategyConfig: sc}}
	cfg := at.mentorEvaluatorConfig()

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := csv.NewReader(f).ReadAll()
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	bars := make([]market.Kline, 0, len(rs))
	for _, r := range rs {
		ms, _ := strconv.ParseInt(r[0], 10, 64)
		pf := func(i int) float64 { v, _ := strconv.ParseFloat(r[i], 64); return v }
		bars = append(bars, market.Kline{OpenTime: ms, Open: pf(1), High: pf(2), Low: pf(3), Close: pf(4), Volume: pf(5), CloseTime: ms + 59_999})
	}

	out, err := os.Create(os.Getenv("REL60_REPLAY_OUT"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	w := bufio.NewWriter(out)
	defer w.Flush()
	enc := json.NewEncoder(w)

	type row struct {
		AtMs     int64   `json:"at_ms"`
		At       string  `json:"at"`
		Setup    string  `json:"setup"`
		Side     string  `json:"side"`
		Price    float64 `json:"price"`
		Stop     float64 `json:"stop"`
		Target   float64 `json:"target"`
		Conf     bool    `json:"conf"`
		ExecTF   int     `json:"exec_tf"`
		ExpiryMs int64   `json:"expiry_ms"`
		Size     int     `json:"size"`
		Tier     string  `json:"tier"`
		Anchor   string  `json:"anchor"`
		Reason   string  `json:"reason"`
	}
	e := mentor.New(cfg)
	n := 0
	for i := 1; i < len(bars); i++ {
		lo := i + 1 - 2000
		if lo < 0 {
			lo = 0
		}
		for _, in := range e.Tick(bars[lo:i+1], mentor.BarCloseInstant(bars[i])) {
			if in.Action == mentor.ActionMoveStopBE {
				e.SwingBEApplied(in.ArmID)
			}
			if in.Action == mentor.ActionClosePosition {
				e.SwingEnded(in.ArmID)
			}
			if (in.Action != mentor.PlaceStopEntry && in.Action != mentor.PlaceStopLimitEntry) || bars[i].OpenTime < from.UnixMilli() {
				continue
			}
			size, _ := at.mentorSizeFor(in, mentorExtraFor(in, false))
			_ = enc.Encode(row{
				AtMs: bars[i].OpenTime, At: time.UnixMilli(bars[i].OpenTime).In(ct).Format("2006-01-02 15:04"),
				Setup: in.Setup, Side: string(in.Side), Price: in.Price, Stop: in.Stop, Target: in.Target,
				Conf: in.Confluence, ExecTF: in.ExecTFMin, ExpiryMs: in.ExpiryMs,
				Size: size.Contracts, Tier: size.Tier, Anchor: in.AnchorKey, Reason: in.Reason,
			})
			n++
		}
	}
	keys := make([]string, 0, len(e.State.Refusals))
	for k := range e.State.Refusals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ref := map[string]int{}
	for _, k := range keys {
		ref[k] = e.State.Refusals[k]
	}
	_ = enc.Encode(map[string]any{"final_refusals": ref, "entries": n})
	t.Logf("rel60 replay: %d entry intents written", n)
}
