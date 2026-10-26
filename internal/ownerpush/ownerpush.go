// Package ownerpush is the OUTBOUND-ONLY owner notification channel (release
// #52, m432). On 2026-10-25 17:00 CT -> 2026-10-26 03:40 CT the bot was blind
// for ~10.5 hours (NT8 linked, its Tradovate data connection off, no bars) and
// the owner learned of it only when the CTO looked: every "FEED DOWN" went to
// the in-app alert center and the log. This is the missing leg: a few ops
// alerts that matter WHILE THE BOT CANNOT TRADE are pushed to the owner's
// bound Telegram chat.
//
// What it is NOT: an inbound surface. It sends text and nothing else; it adds
// no command, no handler, no listener (the Telegram H1 password-takeover chain
// lives on the inbound side and is untouched).
//
// Guarantees, each pinned in ownerpush_test.go:
//   - never blocks the caller: Push is a non-blocking enqueue on a bounded
//     queue; the send runs on one worker goroutine. A full queue DROPS (counted),
//     it never waits.
//   - deduped: one message per key (an episode id) per dedupeTTL.
//   - rate-limited: at most maxPerHour messages per hour, whatever the keys.
//   - no-op when nothing is wired (no sender = unbound / unconfigured): the
//     sender itself is a no-op when no Telegram chat is bound.
//   - never on a friend instance (VL_FRIEND_INSTANCE): a friend bot has no
//     Telegram (main.go never starts it), so nothing here ever posts for one.
//   - no secrets: text is scrubbed of long token-like runs and key=value
//     credentials, and capped at maxTextRunes.
package ownerpush

import (
	"regexp"
	"strings"
	"sync"
	"time"

	"vl/internal/friendmode"
	"vl/logger"
)

const (
	queueCap     = 16
	maxPerHour   = 8
	dedupeTTL    = 24 * time.Hour
	maxTextRunes = 600
)

// Pusher is one push channel. Production uses the process default (Start /
// Push); tests build their own with New and install it with Use.
type Pusher struct {
	mu      sync.Mutex
	send    func(text string)
	q       chan string
	seen    map[string]time.Time
	sentAt  []time.Time
	dropped int
	pending sync.WaitGroup

	now     func() time.Time
	enabled func() bool // false = refuse (friend instance)
}

// New builds a pusher around send and starts its worker. A nil send is a
// permanent no-op pusher.
func New(send func(text string)) *Pusher {
	p := &Pusher{
		send:    send,
		q:       make(chan string, queueCap),
		seen:    map[string]time.Time{},
		now:     time.Now,
		enabled: func() bool { return !friendmode.Enabled() },
	}
	go p.worker()
	return p
}

// SetClockForTest swaps the pusher's clock. TEST-ONLY.
func (p *Pusher) SetClockForTest(now func() time.Time) {
	p.mu.Lock()
	p.now = now
	p.mu.Unlock()
}

// SetEnabledForTest swaps the friend-instance check. TEST-ONLY.
func (p *Pusher) SetEnabledForTest(enabled func() bool) {
	p.mu.Lock()
	p.enabled = enabled
	p.mu.Unlock()
}

func (p *Pusher) worker() {
	for text := range p.q {
		p.deliver(text)
		p.pending.Done()
	}
}

// deliver runs the send; a panic in the sender must never take the worker (or
// the bot) down.
func (p *Pusher) deliver(text string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("owner push: sender panicked: %v", r)
		}
	}()
	p.send(text)
}

// Flush waits until every accepted message has been handed to the sender.
// Tests only; production never waits on the push.
func (p *Pusher) Flush() { p.pending.Wait() }

// Dropped is how many accepted-by-key messages were refused by the rate limit
// or a full queue (never the dedupe: a duplicate is not a drop).
func (p *Pusher) Dropped() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dropped
}

// Push enqueues one owner message under an episode key. It returns whether the
// message was accepted for sending. It never blocks and never panics.
func (p *Pusher) Push(key, text string) bool {
	if p == nil || p.send == nil || strings.TrimSpace(text) == "" || key == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.enabled != nil && !p.enabled() {
		return false
	}
	now := p.now()
	for k, at := range p.seen { // age out old episode keys
		if now.Sub(at) > dedupeTTL {
			delete(p.seen, k)
		}
	}
	if _, dup := p.seen[key]; dup {
		return false
	}
	live := p.sentAt[:0]
	for _, at := range p.sentAt {
		if now.Sub(at) < time.Hour {
			live = append(live, at)
		}
	}
	p.sentAt = live
	if len(p.sentAt) >= maxPerHour {
		p.dropped++
		logger.Warnf("owner push: rate limit (%d/hour) — dropped %q", maxPerHour, key)
		return false
	}
	p.pending.Add(1)
	select {
	case p.q <- Scrub(text):
	default:
		p.pending.Done()
		p.dropped++
		logger.Warnf("owner push: queue full (%d) — dropped %q", queueCap, key)
		return false
	}
	p.seen[key] = now
	p.sentAt = append(p.sentAt, now)
	return true
}

var (
	longRun    = regexp.MustCompile(`[A-Za-z0-9_\-]{32,}`)
	credential = regexp.MustCompile(`(?i)(token|secret|password|passwd|api[_-]?key|authorization|bearer)\s*[:=]\s*\S+`)
)

// Scrub removes credential-shaped text and caps the length. Alert bodies are
// built from fixed templates plus times/prices, so this is a backstop, not the
// primary control.
func Scrub(s string) string {
	s = credential.ReplaceAllString(s, "$1=[redacted]")
	s = longRun.ReplaceAllString(s, "[redacted]")
	r := []rune(s)
	if len(r) > maxTextRunes {
		s = string(r[:maxTextRunes]) + "…"
	}
	return s
}

var (
	defMu sync.RWMutex
	def   *Pusher
)

// Start installs the process-default pusher around send (idempotent: the first
// call wins). main.go calls it only on the owner's box.
func Start(send func(text string)) {
	defMu.Lock()
	defer defMu.Unlock()
	if def == nil {
		def = New(send)
	}
}

// Push sends through the process default. No default (never Started, or a
// friend instance) = false, a no-op.
func Push(key, text string) bool {
	defMu.RLock()
	p := def
	defMu.RUnlock()
	return p.Push(key, text)
}

// Use installs p as the process default and returns the restore. TEST-ONLY.
func Use(p *Pusher) (restore func()) {
	defMu.Lock()
	prev := def
	def = p
	defMu.Unlock()
	return func() {
		defMu.Lock()
		def = prev
		defMu.Unlock()
	}
}
