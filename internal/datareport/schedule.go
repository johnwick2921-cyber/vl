package datareport

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"vl/internal/ownerpush"
	"vl/kernel"
	"vl/logger"
	"vl/safe"
)

const (
	// The report goes out once the 16:00 CT close has had ten minutes to settle
	// (the last bar persisted, the hole tracker's measure run) and before the
	// 17:00 CT session roll, which resets the gate-block table the report reads.
	sendAfterMinute = 16*60 + 10
	sendBeforeMin   = 17 * 60
	tickEvery       = time.Minute
	pushKind        = "data-report" // the owner-push budget bucket (key prefix before "|")
)

// Config wires the scheduler.
type Config struct {
	Sources Sources
	// MarkerDir holds one <session-day>.sent file per report sent, created with
	// O_EXCL: the claim that makes "once per session day" survive a restart. ""
	// = no persistence (the in-memory set still makes it once per process).
	MarkerDir string
	// Push is the owner-push public API; nil = ownerpush.Push.
	Push func(key, text string) bool
	// Now is the clock; nil = time.Now.
	Now func() time.Time
}

// Scheduler sends the nightly report.
type Scheduler struct {
	cfg  Config
	mu   sync.Mutex
	done map[string]bool // session keys sent (or claimed) by this process
	skip map[string]bool // session keys with nothing to report (a closed day)
}

// NewScheduler builds a scheduler without starting it (tests drive Tick).
func NewScheduler(cfg Config) *Scheduler {
	if cfg.Push == nil {
		cfg.Push = ownerpush.Push
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Scheduler{cfg: cfg, done: map[string]bool{}, skip: map[string]bool{}}
}

// Start runs the scheduler on its own goroutine and returns the stop func.
func Start(cfg Config) (stop func()) {
	s := NewScheduler(cfg)
	quit := make(chan struct{})
	var once sync.Once
	safe.GoNamed("datareport-nightly", func() {
		t := time.NewTicker(tickEvery)
		defer t.Stop()
		for {
			select {
			case <-quit:
				return
			case <-t.C:
				s.Tick()
			}
		}
	})
	return func() { once.Do(func() { close(quit) }) }
}

// Tick sends the report if the clock is inside the send window and this
// session day has not been sent. It reports whether a report went out.
func (s *Scheduler) Tick() bool {
	now := s.cfg.Now()
	ct := now.In(kernel.CTLocation())
	m := ct.Hour()*60 + ct.Minute()
	if m < sendAfterMinute || m >= sendBeforeMin {
		return false
	}
	sess := SessionFor(now)
	s.mu.Lock()
	if s.done[sess.Key] || s.skip[sess.Key] {
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()

	rep := Collect(s.cfg.Sources, sess)
	if !hasSession(rep) {
		// A day the CME never traded (weekend, holiday): nothing to report, and
		// nothing is claimed, so a calendar that is wrong can still be re-read.
		s.mu.Lock()
		s.skip[sess.Key] = true
		s.mu.Unlock()
		return false
	}
	if !s.claim(sess.Key) {
		return false
	}
	s.send(sess.Key, rep)
	return true
}

// hasSession reports whether the CME had any expected minutes in the session
// for at least one symbol, or any trade closed. A report with nothing in it is
// not sent; a report whose symbol list failed IS sent (the n/a is the news).
func hasSession(r Report) bool {
	if r.SymbolsErr != "" || len(r.Trades) > 0 {
		return true
	}
	for _, s := range r.Symbols {
		if s.Expected > 0 {
			return true
		}
	}
	return false
}

// claim makes the session day ours: in memory, and (when MarkerDir is set) on
// disk with O_EXCL. A claim that loses the race or finds the marker returns
// false. A marker dir that cannot be written does NOT stop the report (a
// missed report is worse than a duplicate after a restart): it is logged and
// the in-memory claim stands.
func (s *Scheduler) claim(key string) bool {
	s.mu.Lock()
	if s.done[key] {
		s.mu.Unlock()
		return false
	}
	s.done[key] = true
	s.mu.Unlock()
	if s.cfg.MarkerDir == "" {
		return true
	}
	if err := os.MkdirAll(s.cfg.MarkerDir, 0o755); err != nil {
		logger.Warnf("datareport: marker dir %s unusable (%v) — sending once for this process only", s.cfg.MarkerDir, err)
		return true
	}
	f, err := os.OpenFile(filepath.Join(s.cfg.MarkerDir, key+".sent"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return false // sent by an earlier run of the bot for this session day
		}
		logger.Warnf("datareport: marker for %s not written (%v) — sending once for this process only", key, err)
		return true
	}
	_, _ = fmt.Fprintf(f, "%s\n", s.cfg.Now().UTC().Format(time.RFC3339))
	_ = f.Close()
	return true
}

func (s *Scheduler) send(key string, rep Report) []bool {
	return pushParts(s.cfg.Push, key, rep)
}

// pushParts logs the whole report (so a lost push is still on record) and
// sends each part under its own key.
func pushParts(push func(key, text string) bool, key string, rep Report) []bool {
	lines := rep.Lines()
	for _, l := range lines {
		logger.Infof("📊 data report %s: %s", key, l)
	}
	parts := Parts(lines)
	acc := make([]bool, len(parts))
	for i, p := range parts {
		acc[i] = push(fmt.Sprintf("%s|%s|%d", pushKind, key, i+1), p)
	}
	return acc
}

// SendNow builds and sends the report for the session ending at the most recent
// 16:00 CT close, on demand. It uses its own key family (the call time) so it
// is never swallowed by the once-per-day dedupe, and it does not claim the
// day's marker. Returns the report and whether each part was accepted.
func SendNow(src Sources, push func(key, text string) bool, now time.Time) (Report, []bool) {
	if push == nil {
		push = ownerpush.Push
	}
	sess := SessionFor(now)
	rep := Collect(src, sess)
	return rep, pushParts(push, fmt.Sprintf("%s-ondemand-%d", sess.Key, now.Unix()), rep)
}
