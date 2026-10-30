package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"vl/config"
	"vl/internal/datatruth"
	"vl/internal/ownerpush"
	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
	"vl/telemetry"
	"vl/trader"
	ntTrader "vl/trader/ninjatrader"
)

// handleHealth (P2-6) reports REAL state, not a liveness constant: DB ping,
// NT8 feed status + last-bar age per running trader, running-trader count, and
// a non-null time. 200 while the API can answer; 503 only when the DATABASE is
// dead (the API cannot serve anything real without it). NT8 down is 200 with
// status "degraded" — cutover scripts read `revision` from this endpoint and
// must keep working while the AddOn is restarting.
// healthNow is the clock seam for handleHealth (tests pin an open / closed CME instant).
var healthNow = time.Now

func (s *Server) handleHealth(c *gin.Context) {
	now := healthNow()

	dbStatus := "ok"
	if s.store == nil || s.store.DB() == nil {
		dbStatus = "unavailable"
	} else if err := s.store.DB().Ping(); err != nil {
		dbStatus = "dead"
	}

	// deadManHealthInfo is the per-trader /api/health `dead_man` field (W26).
	type deadManHealthInfo struct {
		Blocked bool  `json:"blocked"`
		SinceMs int64 `json:"since_ms"`
		Probes  int   `json:"probes"`
	}
	type nt8Info struct {
		Link       string `json:"link"`
		FeedStatus string `json:"feed_status"`
		// FeedMode is the broker market-data mode from the feed_status frame:
		// "realtime" | "delayed" | "unknown". "" is projected to "unknown" —
		// a missing mode is NEVER reported as realtime (P0 feed freshness).
		FeedMode     string `json:"feed_mode"`
		LastBarAgeMs *int64 `json:"last_bar_age_ms"` // absent = no bar available
		// W26 deadman-escalation: entries blocked by the dead-man reconcile gate
		// and the current blocked episode. READ values; since_ms = 0 when not
		// blocked; probes = consecutive failed reconcile probes in this episode.
		DeadMan *deadManHealthInfo `json:"dead_man,omitempty"`
		// m420b/B6: feedStale is internal (surfaced as the top-level feed_stale).
		feedStale bool
		staleSecs int64
	}
	cmeOpen := kernel.IsCMEOpen(now) // READ from the calendar, never a literal
	nt8 := map[string]nt8Info{}
	running := 0
	if s.traderManager != nil {
		for id, at := range s.traderManager.GetAllTraders() {
			if !at.HealthIsRunning() {
				continue
			}
			running++
			info := nt8Info{Link: "unknown", FeedStatus: "UNKNOWN (no feed_status received)"}
			// P-D stale link latch: the link field follows the REAL TCP socket
			// (HealthLinkConnected), never the latched edge-triggered
			// feed_status — the last feed_status frame can outlive the socket
			// by hours and must not read as "up". The latched string is still
			// surfaced as the last-known feed status.
			if connected, ok := at.HealthLinkConnected(); ok {
				if connected {
					info.Link = "up"
				} else {
					info.Link = "down"
				}
			}
			if st := at.HealthFeedStatus(); st != "" {
				info.FeedStatus = st
			}
			// P0 feed freshness — broker mode, never fabricated realtime.
			info.FeedMode = "unknown"
			if mode := at.HealthFeedMode(); mode == "realtime" || mode == "delayed" {
				info.FeedMode = mode
			}
			if ageMs, ok := at.HealthLastBarAgeMs(now); ok {
				info.LastBarAgeMs = &ageMs
				// m420b/B6: stale only while CME is open, and the age never
				// counts closed-market time (clamped to the open span).
				if cmeOpen {
					newest := now.Add(-time.Duration(ageMs) * time.Millisecond)
					_, clamped := trader.ClampFeedGap(now, newest.Add(time.Minute)) // OpenTime + 1m = NT8's own stamp
					info.feedStale = clamped > trader.FeedDownAfter()
					info.staleSecs = int64(clamped / time.Second)
				}
			} else if cmeOpen && info.Link != "up" {
				// m426: CME open, the NT8 link not up and NO bar at all (a boot
				// with NT8 closed) is a dead feed, not a healthy one. There is
				// no age to report, so feed_stale_secs stays 0/absent.
				info.feedStale = true
			}
			if blocked, sinceMs, probes := at.HealthDeadMan(); blocked {
				info.DeadMan = &deadManHealthInfo{Blocked: true, SinceMs: sinceMs, Probes: probes}
			}
			nt8[id] = info
		}
	}

	status := "ok"
	code := http.StatusOK
	switch dbStatus {
	case "dead":
		status = "error"
		code = http.StatusServiceUnavailable
	case "unavailable":
		status = "degraded"
	}
	// m420b/B6: the NT8 leg degrades the top-level status ONLY while CME is
	// open (feed stale or a dead-man block; a down link shows up as a stale feed). A Disconnected
	// weekend is the market being closed, not the bot being unwell.
	feedStale := false
	var staleSecs int64
	if code == http.StatusOK {
		for _, info := range nt8 {
			if info.feedStale {
				feedStale = true
				if info.staleSecs > staleSecs {
					staleSecs = info.staleSecs
				}
			}
			if cmeOpen && (info.feedStale || info.DeadMan != nil) {
				status = "degraded"
			}
		}
	}

	body := gin.H{
		"status":          status,
		"time":            now.UTC().Format(time.RFC3339),
		"revision":        kernel.RunningRevision(),
		"db":              dbStatus,
		"nt8":             nt8,
		"traders_running": running,
		"cme_open":        cmeOpen,
		"feed_stale":      feedStale,
		// m386: the Friends page flag for the web (nav + route). READ value; the
		// web hides the Friends surface when false. The owner API 404s the same way.
		"friends_page": config.FriendsPageEnabled(),
		// Release #22 OPS-SAFETY: the bot's own wall-vs-monotonic clock-step
		// counters (backward/forward, largest, last, whether NEW entries are
		// held right now). Read values; last_* absent until a step is seen.
		"clock": kernel.ClockStepSnapshot(),
	}
	// m420b: the web banner's extra facts, READ from the calendar / the clamped
	// gap. feed_stale_secs only while feed_stale; cme_reopens_at only while closed.
	if feedStale {
		body["feed_stale_secs"] = staleSecs
	}
	if !cmeOpen {
		body["cme_reopens_at"] = kernel.NextCMEOpen(now).UTC().Format(time.RFC3339)
	}
	// GAP-BACKFILL (2026-10-14): the open 1m bar holes the bot is refilling
	// from NT8 by itself, and the refill counters. READ values: [] = the
	// tracker is wired and no hole is open; ABSENT = no tracker (no store), so
	// nothing is known — never a fabricated empty list (A24).
	if holes, ok := ntTrader.BarHolesSnapshot(); ok {
		body["bars_holes"] = holes
		body["bars_hole_counts"] = telemetry.BarsHoleCountsSnapshot()
		// m444: the persistent gap audit. Its hole figures are re-derived from
		// the STORE every pass, so unlike bars_hole_counts they survive a
		// restart; unfilled_holes_7d is the number the dashboard banner keys on.
		if audit, ok := ntTrader.BarGapAuditSnapshot(); ok {
			body["bars_hole_audit"] = audit
		}
	}
	// m445 DATA-INTEGRITY GATE: is NEW trading paused for missing data? READ from
	// the hole tracker over BOTH data windows (trader/ninjatrader/bar_integrity.go)
	// plus the stale-feed leg already computed above. ABSENT = no NT8 bridge in
	// this process (nothing to lose, nothing claimed); never a fabricated
	// "not paused" (A24). reasons are the classes; windows the holes' CT spans.
	// release #55: data_truth {check: {blocked, episodes, reopens, last_reason}} plus
	// the ledger / fill-price figures (ntwire.DataTruthStats). READ from the live
	// episodes; ABSENT until the checks are registered or required (a process
	// with no NT8 bridge claims nothing - A24).
	dtBlocked := []string{}
	dtScopes := map[string][]string{}
	dtReaders := map[string][]string{}
	if snap := datatruth.Snapshot(); len(snap) > 0 {
		for n, st := range snap {
			if st.Blocked {
				dtBlocked = append(dtBlocked, n)
				if len(st.Scopes) > 0 {
					dtScopes[n] = st.Scopes
				}
				if len(st.Readers) > 0 {
					dtReaders[n] = st.Readers
				}
			}
		}
		sort.Strings(dtBlocked)
		body["data_truth"] = ntwire.DataTruthStats(now)
	}
	if views, armed := ntTrader.TradingPausedSnapshot(now); armed {
		// Round 4, D3: a pause on a symbol NO running trader trades is not a pause
		// of anything (an unfilled hole on a subscribed-but-untraded ES painted the
		// banner red for an MNQ trader). The "" symbol (the detector itself is not
		// ready) always counts.
		traded := map[string]bool{}
		if s.traderManager != nil {
			for _, at := range s.traderManager.GetAllTraders() {
				if at.HealthIsRunning() {
					if sym := at.HealthIntegritySymbol(); sym != "" {
						traded[strings.ToUpper(sym)] = true
					}
				}
			}
		}
		paused, reasons, windows := false, []string{}, []string{}
		scopes := []string{}
		noteScope := func(sc string) {
			if sc == "" {
				return
			}
			for _, x := range scopes {
				if x == sc {
					return
				}
			}
			scopes = append(scopes, sc)
		}
		shown := make([]ntTrader.PausedView, 0, len(views))
		seen := map[string]bool{}
		for _, v := range views {
			if v.Symbol != "" && !traded[strings.ToUpper(v.Symbol)] {
				continue
			}
			shown = append(shown, v)
			if !v.Paused {
				continue
			}
			for _, r := range v.Reasons {
				// an UNKNOWN detector is moot while CME is closed (nothing can
				// open then); a real hole is shown whenever it exists
				if r.Class == ntTrader.IntegrityClassUnknown && !cmeOpen {
					continue
				}
				paused = true
				// every blocked leg, not only the widest one (round 5, DEF-B)
				noteScope(v.Scope)
				for _, sc := range v.Scopes {
					noteScope(sc)
				}
				if !seen[r.Class] {
					seen[r.Class] = true
					reasons = append(reasons, r.Class)
				}
				windows = append(windows, r.Window...)
			}
		}
		views = shown
		// m445 round 2 (reviewer A, D1): the MENTOR's seeded-series leg is
		// per trader (its evaluator was seeded from the stitched history) — read
		// from each mentor trader, named by trader id.
		mentorSeed := []gin.H{}
		if s.traderManager != nil {
			ids := []string{}
			all := s.traderManager.GetAllTraders()
			for id := range all {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				if !all[id].HealthIsRunning() {
					continue
				}
				if w := all[id].HealthSeedSeriesPause(now); len(w) > 0 {
					paused = true
					noteScope("mentor_entries")
					if !seen[ntTrader.IntegrityClassHole] {
						seen[ntTrader.IntegrityClassHole] = true
						reasons = append(reasons, ntTrader.IntegrityClassHole)
					}
					windows = append(windows, w...)
					mentorSeed = append(mentorSeed, gin.H{"trader": id, "scope": "mentor_entries", "windows": w, "action": ntTrader.IntegrityActionSeeded})
				}
			}
		}
		// release #55: the m446/m447 data-truth checks pause NEW entries too. A
		// swing4h-only block pauses SWING4H entries alone; any other check, all.
		if len(dtBlocked) > 0 {
			paused = true
			reasons = append(reasons, "data_truth")
			// each blocking check names what it really pauses (review 4 D1): a block
			// scoped to the mentor or the AI readers, or to one trader/account, is not
			// "all entries" - a banner never claims more than the bot refuses.
			for _, n := range dtBlocked {
				noteScope(ntTrader.DataTruthPausedScope(n, dtReaders[n], dtScopes[n]))
			}
		}
		if feedStale {
			paused = true
			reasons = append(reasons, "stale_data")
		}
		body["trading_paused"] = gin.H{
			"data_truth_checks":  dtBlocked,
			"data_truth_scopes":  dtScopes,  // m451: who a scoped check's block is FOR; absent entry = the whole symbol
			"data_truth_readers": dtReaders, // m450: which entry paths a check's block concerns; absent entry = every path
			"paused":             paused,
			"reasons":            reasons,
			"windows":            windows,
			// the WIDEST scope that is paused and every scope that is (round 4,
			// D3): the banner must not say TRADING PAUSED when only SWING4H is
			"scope":       ntTrader.WidestPausedScope(scopes),
			"scopes":      scopes,
			"symbols":     views,
			"mentor_seed": mentorSeed,
			"counts":      telemetry.DataIntegrityCountsSnapshot(),
		}
	}
	// WAVE 26 — the stale-print guard's lifetime count, beside the hole counts:
	// how many live bars were quarantined (labelled mixed) after a reconnect
	// because their close was impossible against the neighbouring minutes.
	body["bars_stale_prints"] = ntwire.StalePrintQuarantined()
	// K1/K7 — the connect-key refusal counter, named as the protocol doc names
	// it (nt_connect_key_refused). 0 when no key is required or no server runs.
	body["nt_connect_key_refused"] = ntTrader.ConnectKeyRefusedCount()
	// m442 N4: the owner-push delivery counters (sent / failed / held ...). READ
	// values; ABSENT when no pusher runs (a friend instance) - never a fabricated 0.
	if st, ok := ownerpush.StatsSnapshot(); ok {
		body["owner_push"] = st
	}
	c.JSON(code, body)
}
