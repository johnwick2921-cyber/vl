package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"vl/branding"
	"vl/logger"
	"vl/safe"
)

type Scheduler struct {
	agent    *Agent
	logger   *slog.Logger
	stopCh   chan struct{}
	stopOnce sync.Once
}

func NewScheduler(a *Agent, l *slog.Logger) *Scheduler {
	return &Scheduler{agent: a, logger: l, stopCh: make(chan struct{})}
}

// schedulerStamps is the Scheduler's last-run state (P2-16). The daily
// report stamps the CALENDAR DAY it last ran for, and the cleanup stamps the
// last hour it ran in — a missed minute (pause, stall, restart) catches up on
// the next tick instead of being skipped for the day/hour.
type schedulerStamps struct {
	lastReportDay   string // "2006-01-02" of the last daily-report run
	lastCheckAt     time.Time
	lastCleanupHour int // -1 = never ran this process
}

// schedulerStep is the pure decision core of the ticker (tested at the
// production call site: Start consumes it). Returns which jobs this tick
// should run. The daily report runs on the FIRST tick at or after 21:00 whose
// calendar day differs from the stamp; the hourly cleanup runs once per
// hour-CHANGE (never only on :00); the risk check keeps its 4h elapsed
// cadence.
func schedulerStep(now time.Time, st *schedulerStamps) (daily, cleanup, risk bool) {
	// R22 P3-3: 21:00 is a CT time. now.Hour() read the clock's own zone (the
	// MACHINE zone for time.Now()), right only because the box runs in Chicago.
	now = now.In(logger.CTLocation())
	day := now.Format("2006-01-02")
	if now.Hour() >= 21 && st.lastReportDay != day {
		daily = true
		st.lastReportDay = day
	}
	if now.Sub(st.lastCheckAt) > 4*time.Hour {
		risk = true
		st.lastCheckAt = now
	}
	if now.Hour() != st.lastCleanupHour {
		cleanup = true
		st.lastCleanupHour = now.Hour()
	}
	return daily, cleanup, risk
}

func (s *Scheduler) Start(ctx context.Context) {
	safe.GoNamed("agent-scheduler", func() {
		// P2-16 — boundary-aligned ticker: the first tick lands on the next
		// minute boundary, then once per minute; the stamps inside make a
		// missed boundary a catch-up, never a skip.
		first := time.Until(time.Now().Truncate(time.Minute).Add(time.Minute))
		timer := time.NewTimer(first)
		defer timer.Stop()
		st := &schedulerStamps{lastCleanupHour: -1}
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stopCh:
				return
			case now := <-timer.C:
				daily, cleanup, risk := schedulerStep(now, st)
				if daily {
					s.dailyReport()
				}
				if risk {
					s.riskCheck()
				}
				if cleanup {
					if s.agent.pending != nil {
						s.agent.pending.CleanExpired()
					}
				}
				timer.Reset(time.Minute)
			}
		}
	})
}

func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
}

func (s *Scheduler) dailyReport() {
	if s.agent.traderManager == nil {
		return
	}

	traders := s.agent.traderManager.GetAllTraders()
	if len(traders) == 0 {
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📊 *"+branding.PersonaName()+" 每日报告 — %s*\n\n", time.Now().Format("2006-01-02")))

	totalPnL := 0.0
	for _, t := range traders {
		info, err := t.GetAccountInfo()
		if err != nil {
			continue
		}
		equity := toFloat(info["total_equity"])
		pnl := toFloat(info["unrealized_pnl"])
		sb.WriteString(fmt.Sprintf("• %s: $%.2f (P/L: $%.2f)\n", t.GetName(), equity, pnl))
		totalPnL += pnl
	}
	e := "📈"
	if totalPnL < 0 {
		e = "📉"
	}
	sb.WriteString(fmt.Sprintf("\n%s Total P/L: $%.2f", e, totalPnL))

	s.agent.notifyAll(sb.String())
}

func (s *Scheduler) riskCheck() {
	if s.agent.traderManager == nil {
		return
	}

	var alerts []string
	for _, t := range s.agent.traderManager.GetAllTraders() {
		positions, err := t.GetPositions()
		if err != nil {
			continue
		}
		for _, p := range positions {
			pnl := toFloat(p["unrealizedPnl"])
			size := toFloat(p["size"])
			if size == 0 {
				continue
			}
			entry := toFloat(p["entryPrice"])
			if entry > 0 {
				pnlPct := (pnl / (entry * size)) * 100
				if pnlPct < -5 {
					alerts = append(alerts, fmt.Sprintf("⚠️ *%s* %s: %.1f%% ($%.2f)",
						p["symbol"], p["side"], pnlPct, pnl))
				}
			}
		}
	}
	if len(alerts) > 0 {
		s.agent.notifyAll("🚨 *持仓风险提醒*\n\n" + strings.Join(alerts, "\n"))
	}
}
