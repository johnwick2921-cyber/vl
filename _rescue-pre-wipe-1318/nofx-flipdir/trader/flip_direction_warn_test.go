package trader

import (
	"strings"
	"testing"

	"nofx/kernel"
	"nofx/logger"

	"github.com/sirupsen/logrus"
)

// warnHook captures WARN-level entries from the package logger so a test can
// see what warnFlipDeathSanity wrote to the journal.
type warnHook struct{ msgs []string }

func (h *warnHook) Levels() []logrus.Level { return []logrus.Level{logrus.WarnLevel} }
func (h *warnHook) Fire(e *logrus.Entry) error {
	h.msgs = append(h.msgs, e.Message)
	return nil
}

func captureWarns(t *testing.T) *warnHook {
	t.Helper()
	h := &warnHook{}
	logger.Log.AddHook(h)
	t.Cleanup(func() {
		// logrus has no RemoveHook; replace the level's hook list.
		hooks := logger.Log.Hooks[logrus.WarnLevel]
		kept := hooks[:0]
		for _, x := range hooks {
			if x != h {
				kept = append(kept, x)
			}
		}
		logger.Log.Hooks[logrus.WarnLevel] = kept
	})
	return h
}

// W-FLIP-DIRECTION (2026-09-17) — plans ALREADY in the store were written
// before the validator learned direction; the read-path sanity pass makes an
// inverted flip visible in the journal (WARN, never a reject).
func TestWarnFlipDeathSanity_InvertedFlipDirectionWarns(t *testing.T) {
	h := captureWarns(t)
	at := &AutoTrader{id: "tfd", exchange: "ninjatrader"}
	d := &kernel.PlanDoc{
		Bias:            kernel.PlanBias{Direction: "short"},
		Levels:          []kernel.PlanLevel{{Price: 29474.90, Label: "ONL"}, {Price: 29604.25, Label: "PDH"}},
		DeathStructured: &kernel.PlanCondition{Price: 29604.25, Side: "above", Rule: "2x5m"},
		FlipStructured:  &kernel.PlanCondition{Price: 29474.90, Side: "below", Rule: "2x5m", FlipTo: "long"},
	}
	at.warnFlipDeathSanity(d)
	joined := strings.Join(h.msgs, "\n")
	if !strings.Contains(joined, "contradicts bias short") {
		t.Fatalf("an inverted flip must WARN in the journal; got warns:\n%s", joined)
	}
}

func TestWarnFlipDeathSanity_CorrectFlipDirectionSilent(t *testing.T) {
	h := captureWarns(t)
	at := &AutoTrader{id: "tfd2", exchange: "ninjatrader"}
	d := &kernel.PlanDoc{
		Bias:            kernel.PlanBias{Direction: "short"},
		Levels:          []kernel.PlanLevel{{Price: 29474.90, Label: "ONL"}, {Price: 29604.25, Label: "PDH"}},
		DeathStructured: &kernel.PlanCondition{Price: 29474.90, Side: "below", Rule: "2x5m"},
		FlipStructured:  &kernel.PlanCondition{Price: 29604.25, Side: "above", Rule: "2x5m", FlipTo: "long"},
	}
	at.warnFlipDeathSanity(d)
	for _, m := range h.msgs {
		if strings.Contains(m, "contradicts bias") {
			t.Fatalf("a correctly-pointed flip must not WARN about direction: %s", m)
		}
	}
}
