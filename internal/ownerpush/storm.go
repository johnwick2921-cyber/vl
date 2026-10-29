package ownerpush

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// m442 item 6 - REFUSAL STORM ALERT. The owner's core complaint: problems sat in
// the logs for days (95 refused break-evens since 10-23) because each refusal
// was ONE log line and nobody reads a log line. The same refusal class repeating
// for one position / trader is a storm, and it is pushed to the owner ONCE per
// episode.
//
//	threshold   StormThreshold refusals of one (class, trader, subject) within
//	            StormWindow  -> one push
//	episode     runs until a full StormWindow passes with no further refusal of
//	            that scope; the next storm after that is a new episode (new push)
//	budget      each class is its own push kind (3/hour), so a storm of one class
//	            cannot starve FEED DOWN or another class
//
// Counters record, never infer: NoteRefusal is called AT the refusal site.

const (
	// StormThreshold / StormWindow are the documented numbers (guide: Status ->
	// Owner push). Constants, so the guide and the code cannot drift silently.
	StormThreshold = 3
	StormWindow    = 10 * time.Minute
	stormMaxScopes = 256
)

// Refusal classes pushed as storms. The strings are the keys' kind suffix.
const (
	ClassStopWiden      = "stop-widen"
	ClassStopNotApplied = "stop-move-not-applied"
	ClassCloseRejected  = "close-rejected"
	ClassSwingBE        = "swing-be-refused"
	ClassSwingClose     = "swing-close-refused"
)

var classLabel = map[string]string{
	ClassStopWiden:      "stop-widen REFUSED (the bot tried to move a stop the wrong way)",
	ClassStopNotApplied: "STOP MOVE NOT APPLIED at NT8",
	ClassCloseRejected:  "exit REJECTED by NT8 (position_close_rejected)",
	ClassSwingBE:        "swing break-even refused",
	ClassSwingClose:     "swing hold-close refused",
}

// Refusal is one refusal event at a refusal site.
type Refusal struct {
	Class    string // one of the Class* constants (unknown classes still work)
	TraderID string // the owning trader id ("" = unknown)
	Subject  string // the position / signal / arm the refusal is about
	Detail   string // the refusal's own words (short)
}

type stormState struct {
	events  []time.Time
	active  bool
	started time.Time
	last    time.Time
}

var (
	labelerMu sync.RWMutex
	labeler   func(traderID string) string
)

// SetTraderLabeler registers the trader-id -> display-name lookup (the trader
// package owns the registry; ownerpush must not import it). A nil/absent
// labeler or a "" answer falls back to the id's first 8 characters.
func SetTraderLabeler(fn func(traderID string) string) {
	labelerMu.Lock()
	labeler = fn
	labelerMu.Unlock()
}

func traderLabel(id string) string {
	labelerMu.RLock()
	fn := labeler
	labelerMu.RUnlock()
	if fn != nil {
		if s := strings.TrimSpace(fn(id)); s != "" {
			return s
		}
	}
	id = strings.TrimSpace(id)
	switch {
	case id == "":
		return "n/a"
	case len(id) > 8:
		return id[:8]
	}
	return id
}

var tokenish = regexp.MustCompile(`[A-Za-z0-9_\-]{24,}`)

// shorten keeps a long id readable AND below the scrubber's token-shape cutoff
// (32), which would otherwise print it as "[redacted]".
func shorten(s string) string {
	return tokenish.ReplaceAllStringFunc(s, func(t string) string {
		return t[:8] + ".." + t[len(t)-6:]
	})
}

func cleanScope(s string) string {
	return strings.NewReplacer("|", "/", "\n", " ").Replace(strings.TrimSpace(s))
}

// NoteRefusal records one refusal and pushes ONE owner message when the scope
// crosses the threshold. It reports whether THIS call fired the push. Never
// blocks; a nil pusher / friend instance is a no-op.
func (p *Pusher) NoteRefusal(r Refusal) bool {
	if p == nil || p.send == nil || r.Class == "" {
		return false
	}
	scope := cleanScope(r.TraderID) + "|" + cleanScope(r.Subject)
	skey := r.Class + "|" + scope

	p.mu.Lock()
	if p.enabled != nil && !p.enabled() {
		p.mu.Unlock()
		return false
	}
	now := p.now()
	if p.storms == nil {
		p.storms = map[string]*stormState{}
	}
	// bound memory: forget idle scopes
	if len(p.storms) >= stormMaxScopes {
		for k, st := range p.storms {
			if now.Sub(st.last) > StormWindow {
				delete(p.storms, k)
			}
		}
	}
	st := p.storms[skey]
	if st == nil {
		st = &stormState{}
		p.storms[skey] = st
	}
	if st.active && now.Sub(st.last) > StormWindow { // a full quiet window ends the episode
		st.active = false
		st.events = nil
	}
	live := st.events[:0]
	for _, at := range st.events {
		if now.Sub(at) <= StormWindow {
			live = append(live, at)
		}
	}
	st.events = append(live, now)
	st.last = now
	n := len(st.events)
	fire := !st.active && n >= StormThreshold
	if fire {
		st.active = true
		st.started = now
	}
	started := st.started
	p.mu.Unlock()
	if !fire {
		return false
	}

	label := classLabel[r.Class]
	if label == "" {
		label = r.Class
	}
	detail := strings.TrimSpace(r.Detail)
	if detail != "" {
		detail = " Last: " + detail + "."
	}
	text := fmt.Sprintf("⚠️ vl REFUSAL STORM - %s, %d times in %d min, trader %s, %s.%s It repeats; it will not fix itself - read the bot log.",
		label, n, int(StormWindow/time.Minute), traderLabel(r.TraderID), shorten(strings.TrimSpace(r.Subject)), shorten(detail))
	key := fmt.Sprintf("refusal-%s|%s|%d", r.Class, scope, started.UnixMilli())
	return p.Push(key, text)
}

// NoteRefusal routes through the process default. No default = false (no-op).
func NoteRefusal(r Refusal) bool {
	defMu.RLock()
	p := def
	defMu.RUnlock()
	return p.NoteRefusal(r)
}
