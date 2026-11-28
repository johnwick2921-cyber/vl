package api

import (
	"sync"
	"time"

	"vl/internal/updaterwire"
	"vl/safe"
	"vl/trader"
)

// m420b/B4 — the cached updater check behind GET /api/updates.
//
// UpdateBadge was permanently "Unknown" because /api/updates never sent
// update_available. The answer lives in the worker (the bot makes no release-
// source network call), so the bot caches the last check it relayed — from
// the manual POST /api/updates/check and from a background loop — with a
// timestamp, and affirms update_available / latest_tag only while that cache
// is fresh. A stale or absent cache means the keys are omitted: Unknown is the
// honest answer, never a stale "up to date".

const (
	// updateCheckFreshFor — the cache is affirmed for 15 minutes.
	updateCheckFreshFor = 15 * time.Minute
	// updateCheckEvery — the background loop's period.
	updateCheckEvery = 10 * time.Minute
	// updateCheckRateLimitedBackoff — after the worker answers rate_limited
	// the loop stays silent this long (it never hammers a limited source).
	updateCheckRateLimitedBackoff = 30 * time.Minute
)

// updateCheckCache is the last relayed check result (zero value = none yet).
type updateCheckCache struct {
	mu           sync.Mutex
	at           time.Time
	available    bool
	tag          string
	have         bool
	rateLimitEnd time.Time
}

// note records a worker check answer. Only up_to_date / verified_ready carry a
// verdict; rate_limited arms the backoff; anything else leaves the cache alone
// (so it ages out instead of being overwritten with a guess).
func (c *updateCheckCache) note(state string, d checkDetail, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch state {
	case "up_to_date":
		c.at, c.available, c.tag, c.have = now, false, d.Tag, true
	case "verified_ready":
		c.at, c.available, c.tag, c.have = now, true, d.Tag, true
	case "rate_limited":
		c.rateLimitEnd = now.Add(updateCheckRateLimitedBackoff)
	}
}

// fresh returns the cached verdict iff it is younger than updateCheckFreshFor.
func (c *updateCheckCache) fresh(now time.Time) (available bool, tag string, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.have || now.Sub(c.at) >= updateCheckFreshFor || now.Before(c.at) {
		return false, "", false
	}
	return c.available, c.tag, true
}

// limited reports whether the background loop must stay silent.
func (c *updateCheckCache) limited(now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return now.Before(c.rateLimitEnd)
}

// workerInstallState asks the worker (already dialled) for its overall status
// and maps it to the badge vocabulary: busy -> "installing"; idle / parked /
// stopped pass through. "" = unreadable (the key is then omitted).
func workerInstallState(wc *updaterwire.Client) string {
	resp, err := wc.DoWithTimeout(updaterwire.NewStatus(""), workerProbeTimeout)
	if err != nil || !resp.OK {
		return ""
	}
	if resp.State == "busy" {
		return "installing"
	}
	return resp.State
}

// relayCheckState dials the worker and relays one check, returning the state
// and detail. ok=false when the worker is unreachable or refused.
func relayCheckState() (state string, d checkDetail, ok bool) {
	conn, err := updaterwire.DialWorker(trader.MaintenanceDataDir())
	if err != nil {
		return "", d, false
	}
	defer conn.Close()
	resp, err := conn.DoWithTimeout(updaterwire.NewCheck(), checkRelayTimeout)
	if err != nil || !resp.OK {
		return "", d, false
	}
	return resp.State, checkDetailFrom(resp.Detail), true
}

// runBackgroundUpdateCheck does one loop beat: skip while rate limited, else
// relay a check and cache it.
func (s *Server) runBackgroundUpdateCheck(now time.Time) {
	if s.updCheck.limited(now) {
		return
	}
	state, d, ok := relayCheckState()
	if !ok {
		return
	}
	s.updCheck.note(state, d, time.Now())
}

// startUpdateCheckLoop starts the 10-minute background check (only when the
// updater is on and this is not a friend instance); idempotent.
func (s *Server) startUpdateCheckLoop() {
	if !s.updaterOn || s.friendMode {
		return
	}
	s.updCheckOnce.Do(func() {
		s.updCheckStop = make(chan struct{})
		stop := s.updCheckStop
		safe.GoNamed("updates-check-loop", func() {
			s.runBackgroundUpdateCheck(time.Now())
			t := time.NewTicker(updateCheckEvery)
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case <-t.C:
					s.runBackgroundUpdateCheck(time.Now())
				}
			}
		})
	})
}

func (s *Server) stopUpdateCheckLoop() {
	s.updCheckOnce.Do(func() {}) // a Shutdown before Start must not let a later Start run
	if s.updCheckStop != nil {
		select {
		case <-s.updCheckStop:
		default:
			close(s.updCheckStop)
		}
	}
}
