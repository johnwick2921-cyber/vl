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
//   - rate-limited PER KIND (the key's prefix before "|"): at most
//     maxPerKindPerHour per kind per hour, so one flooding kind can never use
//     up the budget feed-down / trader-down / protection pushes need (#53 m438;
//     the shared 8/hour budget let a naked-position retry loop silence FEED
//     DOWN). maxPerHour is only a backstop across all kinds.
//   - a DROPPED message stays eligible: the rate limit / full queue HOLDS it
//     (bounded, holdTTL) and Retry re-offers it when the budget reopens. An
//     alert row is written once, so without this a dropped P0 was lost for good.
//   - the worker can never hang: each send runs under a deadline (sendTimeout);
//     a send that outlives it is abandoned and the worker moves on.
//   - no-op when nothing is wired (no sender = unbound / unconfigured): the
//     sender itself is a no-op when no Telegram chat is bound.
//   - never on a friend instance (VL_FRIEND_INSTANCE): a friend bot has no
//     Telegram (main.go never starts it), so nothing here ever posts for one.
//   - no secrets: text is scrubbed of long token-like runs and key=value
//     credentials, and capped at maxTextRunes.
package ownerpush

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"vl/internal/friendmode"
	"vl/logger"
	"vl/safe"
)

const (
	queueCap = 16
	// maxPerKindPerHour is the per-kind budget; maxPerHour the all-kinds
	// backstop. The backstop is sized above (kinds the owner is pushed) x
	// (per-kind budget) so it only ever binds for unknown kinds: for the known
	// ones the per-kind budget binds first and no kind can starve another.
	maxPerKindPerHour = 3
	maxPerHour        = 30
	dedupeTTL         = 24 * time.Hour
	maxTextRunes      = 600
	// holdTTL / holdCap bound the held (dropped-but-eligible) messages: older
	// than holdTTL a message is stale news and is discarded (counted + logged).
	holdTTL = 90 * time.Minute
	holdCap = 32
	// defaultSendTimeout bounds one send; the Telegram client has its own
	// shorter HTTP timeout, this is the worker's own backstop.
	defaultSendTimeout = 30 * time.Second
)

type heldMsg struct {
	key, text string
	at        time.Time
}

// Pusher is one push channel. Production uses the process default (Start /
// Push); tests build their own with New and install it with Use.
type Pusher struct {
	mu      sync.Mutex
	send    func(text string)
	q       chan string
	seen    map[string]time.Time
	sentAt  []time.Time
	kindAt  map[string][]time.Time // per-kind send times, for the per-kind budget
	held    []heldMsg              // dropped by the budget / full queue, still eligible
	dropped int
	pending sync.WaitGroup

	sendTimeout time.Duration

	now     func() time.Time
	enabled func() bool // false = refuse (friend instance)
}

// New builds a pusher around send and starts its worker. A nil send is a
// permanent no-op pusher.
func New(send func(text string)) *Pusher {
	p := &Pusher{
		send:        send,
		q:           make(chan string, queueCap),
		seen:        map[string]time.Time{},
		kindAt:      map[string][]time.Time{},
		now:         time.Now,
		sendTimeout: defaultSendTimeout,
		enabled:     func() bool { return !friendmode.Enabled() },
	}
	safe.GoNamed("owner-push", p.worker)
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

// SetSendTimeoutForTest swaps the per-send deadline. TEST-ONLY.
func (p *Pusher) SetSendTimeoutForTest(d time.Duration) {
	p.mu.Lock()
	p.sendTimeout = d
	p.mu.Unlock()
}

func (p *Pusher) worker() {
	for text := range p.q {
		p.deliver(text)
		p.pending.Done()
	}
}

// deliver runs the send under a deadline; a panic in the sender must never take
// the worker (or the bot) down, and a sender that never returns (a connection
// that accepts and then says nothing) must never wedge the single worker: it is
// abandoned after sendTimeout and the next message goes out.
func (p *Pusher) deliver(text string) {
	p.mu.Lock()
	d := p.sendTimeout
	p.mu.Unlock()
	if d <= 0 {
		d = defaultSendTimeout
	}
	done := make(chan struct{})
	safe.GoNamed("owner-push-send", func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("owner push: sender panicked: %v", r)
			}
		}()
		p.send(text)
	})
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		logger.Errorf("owner push: a send ran past its %s deadline — abandoned, the worker moves on", d)
	}
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

// kindOf is the budget bucket of a key: the part before the first "|"
// ("feed-down|ep1" -> "feed-down"). A key with no "|" shares the "other" bucket
// (an unknown shape never gets a bucket of its own to flood with).
func kindOf(key string) string {
	if i := strings.IndexByte(key, '|'); i > 0 {
		return key[:i]
	}
	return "other"
}

// admitLocked tries to enqueue text under key at `now`. "" = accepted; else the
// reason it was refused (the caller holds the message). The caller holds p.mu.
func (p *Pusher) admitLocked(key, text string, now time.Time) string {
	live := p.sentAt[:0]
	for _, at := range p.sentAt {
		if now.Sub(at) < time.Hour {
			live = append(live, at)
		}
	}
	p.sentAt = live
	kind := kindOf(key)
	kl := p.kindAt[kind][:0]
	for _, at := range p.kindAt[kind] {
		if now.Sub(at) < time.Hour {
			kl = append(kl, at)
		}
	}
	p.kindAt[kind] = kl
	if len(kl) >= maxPerKindPerHour {
		return fmt.Sprintf("%s budget (%d/hour)", kind, maxPerKindPerHour)
	}
	if len(p.sentAt) >= maxPerHour {
		return fmt.Sprintf("rate limit (%d/hour)", maxPerHour)
	}
	p.pending.Add(1)
	select {
	case p.q <- Scrub(text):
	default:
		p.pending.Done()
		return fmt.Sprintf("queue full (%d)", queueCap)
	}
	p.seen[key] = now
	p.sentAt = append(p.sentAt, now)
	p.kindAt[kind] = append(p.kindAt[kind], now)
	return ""
}

// holdLocked remembers a refused message so Retry can offer it again. One slot
// per key (a repeat replaces); bounded at holdCap (the oldest goes first).
func (p *Pusher) holdLocked(key, text string, now time.Time) {
	for i := range p.held {
		if p.held[i].key == key {
			p.held[i].text = text
			return
		}
	}
	if len(p.held) >= holdCap {
		p.held = p.held[1:]
	}
	p.held = append(p.held, heldMsg{key: key, text: text, at: now})
}

func (p *Pusher) unholdLocked(key string) {
	out := p.held[:0]
	for _, h := range p.held {
		if h.key != key {
			out = append(out, h)
		}
	}
	p.held = out
}

// Push enqueues one owner message under an episode key. It returns whether the
// message was accepted for sending. It never blocks and never panics. A message
// refused by the budget or a full queue is HELD and re-offered by Retry.
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
	if why := p.admitLocked(key, text, now); why != "" {
		p.dropped++
		p.holdLocked(key, text, now)
		logger.Warnf("owner push: %s — held %q for retry", why, key)
		return false
	}
	p.unholdLocked(key)
	return true
}

// Retry re-offers the held messages (oldest first) once the budget has room.
// Messages older than holdTTL are discarded. It returns how many were accepted.
// Production calls it from the trader's 60 s beat; it never blocks.
func (p *Pusher) Retry() int {
	if p == nil || p.send == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.enabled != nil && !p.enabled() {
		return 0
	}
	now := p.now()
	var keep []heldMsg
	accepted := 0
	for _, h := range p.held {
		if now.Sub(h.at) > holdTTL {
			logger.Warnf("owner push: held %q is older than %s — discarded as stale", h.key, holdTTL)
			continue
		}
		if _, dup := p.seen[h.key]; dup {
			continue
		}
		if why := p.admitLocked(h.key, h.text, now); why != "" {
			keep = append(keep, h)
			continue
		}
		accepted++
	}
	p.held = keep
	return accepted
}

// Held is how many dropped messages are still eligible for a retry.
func (p *Pusher) Held() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.held)
}

// Cancel withdraws a held message and marks its key as spoken for, so a
// message that is no longer news (a FEED DOWN whose FEED BACK already went out)
// is never delivered late.
func (p *Pusher) Cancel(key string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.unholdLocked(key)
	if _, ok := p.seen[key]; !ok {
		p.seen[key] = p.now()
	}
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

// Retry re-offers the process default's held messages. No default = 0.
func Retry() int {
	defMu.RLock()
	p := def
	defMu.RUnlock()
	return p.Retry()
}

// Cancel withdraws a held message on the process default. No default = no-op.
func Cancel(key string) {
	defMu.RLock()
	p := def
	defMu.RUnlock()
	p.Cancel(key)
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
