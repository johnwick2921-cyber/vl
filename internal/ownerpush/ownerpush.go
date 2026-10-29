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
//   - a FAILED send is not a delivered send (m442 N4): the sender returns an
//     error, the key is un-seen, the budget slot refunded, and the message is
//     HELD with exponential backoff until it goes out or goes stale. Success
//     is logged; sent / failed / skipped counters ride /api/health.
//   - held + deduped state survives a restart (m442 #24): a small atomic file
//     under data/ keeps the held (and in-flight) P0s and the seen keys.
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
	"errors"
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
	// retryBackoffBase / retryBackoffMax: a FAILED send is re-offered after
	// base*2^(failures-1), capped (30s, 1m, 2m, 4m, 8m, 15m, 15m ...). Retry
	// is driven by the trader beat (60 s) and by the pusher's own 30 s loop.
	retryBackoffBase = 30 * time.Second
	retryBackoffMax  = 15 * time.Minute
	retryLoopEvery   = 30 * time.Second
	// defaultSendTimeout bounds one send; the Telegram client has its own
	// shorter HTTP timeout, this is the worker's own backstop.
	defaultSendTimeout = 30 * time.Second
)

// ErrNotBound is what a sender returns when there is nowhere to send (no bot
// token, no bound chat). It is a SKIP, not a failure: the key stays seen, the
// message is not retried, and the skipped counter moves.
var ErrNotBound = errors.New("owner push: no Telegram chat bound")

// Limits renders the budget numbers FROM THE CONSTANTS, for the boot line and
// anything else that states them (m442 #27: the line said 8/hour while the code
// was 3 per kind and 30 total).
func Limits() string {
	return fmt.Sprintf("<=%d/hour per kind, <=%d/hour in total", maxPerKindPerHour, maxPerHour)
}

type heldMsg struct {
	key, text string
	at        time.Time // first offered: the holdTTL base, kept across retries
	attempts  int       // failed sends so far
	notBefore time.Time // backoff: Retry skips this message until then
	admitAt   time.Time // when the budget slot was spent (refunded on a failed send)
}

// Stats are the pusher's delivery counters. Counters record, never infer.
type Stats struct {
	Sent         int    `json:"sent"`
	Failed       int    `json:"failed"`
	Skipped      int    `json:"skipped_unbound"`
	Dropped      int    `json:"dropped"`
	Held         int    `json:"held"`
	Stale        int    `json:"stale_discarded"`
	LastError    string `json:"last_error,omitempty"`
	LastSentUnix int64  `json:"last_sent_unix,omitempty"`
	LastFailUnix int64  `json:"last_failed_unix,omitempty"`
}

// Pusher is one push channel. Production uses the process default (Start /
// Push); tests build their own with New and install it with Use.
type Pusher struct {
	mu       sync.Mutex
	send     func(text string) error
	q        chan heldMsg
	inflight map[string]heldMsg // admitted, not yet delivered / failed
	stats    Stats
	state    *stateFile // nil = memory only
	storms   map[string]*stormState
	seen     map[string]time.Time
	sentAt   []time.Time
	kindAt   map[string][]time.Time // per-kind send times, for the per-kind budget
	held     []heldMsg              // dropped by the budget / full queue, still eligible
	dropped  int
	pending  sync.WaitGroup

	sendTimeout time.Duration

	now     func() time.Time
	enabled func() bool // false = refuse (friend instance)
}

// New builds a pusher around a sender that cannot fail (tests, legacy shape).
// A nil send is a permanent no-op pusher.
func New(send func(text string)) *Pusher {
	if send == nil {
		return NewE(nil)
	}
	return NewE(func(text string) error { send(text); return nil })
}

// NewE builds a pusher around send and starts its worker. send returns the
// delivery error: nil = delivered, ErrNotBound = nowhere to send (skipped),
// anything else = failed and retried with backoff. A nil send is a permanent
// no-op pusher.
func NewE(send func(text string) error) *Pusher {
	p := &Pusher{
		send:        send,
		q:           make(chan heldMsg, queueCap),
		inflight:    map[string]heldMsg{},
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
	for m := range p.q {
		p.deliver(m)
		p.pending.Done()
	}
}

// deliver runs the send under a deadline; a panic in the sender must never take
// the worker (or the bot) down, and a sender that never returns (a connection
// that accepts and then says nothing) must never wedge the single worker: it is
// abandoned after sendTimeout and the next message goes out. The OUTCOME is
// recorded (m442 N4): success is logged and counted; a failure, a panic or a
// timeout un-sees the key and holds the message for a backed-off retry. An
// abandoned send that completes late may deliver a duplicate - a duplicate
// P0 is the cheaper error than a lost one.
func (p *Pusher) deliver(m heldMsg) {
	p.mu.Lock()
	d := p.sendTimeout
	p.mu.Unlock()
	if d <= 0 {
		d = defaultSendTimeout
	}
	res := make(chan error, 1)
	safe.GoNamed("owner-push-send", func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("owner push: sender panicked: %v", r)
				res <- fmt.Errorf("sender panicked: %v", r)
			}
		}()
		res <- p.send(m.text)
	})
	timer := time.NewTimer(d)
	defer timer.Stop()
	var err error
	select {
	case err = <-res:
	case <-timer.C:
		logger.Errorf("owner push: a send ran past its %s deadline - abandoned, the worker moves on", d)
		err = fmt.Errorf("send exceeded its %s deadline", d)
	}
	switch {
	case err == nil:
		p.onSent(m)
	case errors.Is(err, ErrNotBound):
		p.onSkipped(m)
	default:
		p.onFailed(m, err)
	}
}

func (p *Pusher) onSent(m heldMsg) {
	p.mu.Lock()
	delete(p.inflight, m.key)
	p.stats.Sent++
	p.stats.LastSentUnix = p.now().Unix()
	p.persistLocked()
	p.mu.Unlock()
	logger.Infof("owner push: SENT %q (attempt %d)", m.key, m.attempts+1)
}

func (p *Pusher) onSkipped(m heldMsg) {
	p.mu.Lock()
	delete(p.inflight, m.key)
	p.stats.Skipped++
	p.persistLocked()
	p.mu.Unlock()
	logger.Debugf("owner push: %q not sent - no Telegram chat bound", m.key)
}

// onFailed is the N4 fix: the send did not reach the owner, so the key is no
// longer "spoken for", the budget slot is refunded and the message is held for
// a backed-off retry (unless Cancel withdrew it meanwhile).
func (p *Pusher) onFailed(m heldMsg, err error) {
	msg := Scrub(err.Error())
	p.mu.Lock()
	_, live := p.inflight[m.key]
	delete(p.inflight, m.key)
	p.stats.Failed++
	p.stats.LastError = msg
	p.stats.LastFailUnix = p.now().Unix()
	if live { // not withdrawn by Cancel
		delete(p.seen, m.key)
		kind := kindOf(m.key)
		p.sentAt = removeOnce(p.sentAt, m.admitAt)
		p.kindAt[kind] = removeOnce(p.kindAt[kind], m.admitAt)
		m.attempts++
		m.notBefore = p.now().Add(backoffFor(m.attempts))
		p.holdMsgLocked(m)
	}
	p.persistLocked()
	p.mu.Unlock()
	logger.Errorf("owner push: SEND FAILED %q (attempt %d): %s - held for retry", m.key, m.attempts, msg)
}

func backoffFor(failures int) time.Duration {
	d := retryBackoffBase
	for i := 1; i < failures && d < retryBackoffMax; i++ {
		d *= 2
	}
	if d > retryBackoffMax {
		d = retryBackoffMax
	}
	return d
}

func removeOnce(ts []time.Time, at time.Time) []time.Time {
	for i, t := range ts {
		if t.Equal(at) {
			return append(ts[:i:i], ts[i+1:]...)
		}
	}
	return ts
}

// Stats returns the delivery counters (a copy).
func (p *Pusher) Stats() Stats {
	if p == nil {
		return Stats{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.stats
	st.Dropped = p.dropped
	st.Held = len(p.held)
	return st
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

// admitLocked tries to enqueue m at `now`. "" = accepted; else the reason it was
// refused (the caller holds the message). The caller holds p.mu.
func (p *Pusher) admitLocked(m heldMsg, now time.Time) string {
	key := m.key
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
	m.text = Scrub(m.text)
	m.admitAt = now
	p.pending.Add(1)
	select {
	case p.q <- m:
	default:
		p.pending.Done()
		return fmt.Sprintf("queue full (%d)", queueCap)
	}
	p.seen[key] = now
	p.inflight[key] = m
	p.sentAt = append(p.sentAt, now)
	p.kindAt[kind] = append(p.kindAt[kind], now)
	p.persistLocked()
	return ""
}

// holdLocked remembers a refused message so Retry can offer it again. One slot
// per key (a repeat replaces the text, keeps the age); bounded at holdCap (the
// oldest goes first).
func (p *Pusher) holdLocked(key, text string, now time.Time) {
	p.holdMsgLocked(heldMsg{key: key, text: text, at: now})
}

func (p *Pusher) holdMsgLocked(m heldMsg) {
	for i := range p.held {
		if p.held[i].key == m.key {
			p.held[i].text = m.text
			if m.attempts > p.held[i].attempts {
				p.held[i].attempts = m.attempts
				p.held[i].notBefore = m.notBefore
			}
			p.persistLocked()
			return
		}
	}
	if len(p.held) >= holdCap {
		p.held = p.held[1:]
	}
	p.held = append(p.held, m)
	p.persistLocked()
}

func (p *Pusher) unholdLocked(key string) {
	out := p.held[:0]
	for _, h := range p.held {
		if h.key != key {
			out = append(out, h)
		}
	}
	p.held = out
	p.persistLocked()
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
	if why := p.admitLocked(heldMsg{key: key, text: text, at: now}, now); why != "" {
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
			logger.Warnf("owner push: held %q is older than %s - discarded as stale", h.key, holdTTL)
			p.stats.Stale++
			continue
		}
		if _, dup := p.seen[h.key]; dup {
			continue
		}
		if now.Before(h.notBefore) { // backing off after a failed send
			keep = append(keep, h)
			continue
		}
		if why := p.admitLocked(h, now); why != "" {
			keep = append(keep, h)
			continue
		}
		accepted++
	}
	p.held = keep
	p.persistLocked()
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
	delete(p.inflight, key) // a queued copy that then fails is NOT re-held
	if _, ok := p.seen[key]; !ok {
		p.seen[key] = p.now()
	}
	p.persistLocked()
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
// call wins). main.go calls it only on the owner's box. statePath ("" = memory
// only) is the small file that keeps held + deduped state across a restart; the
// returned BootInfo says how the previous process ended. Production also runs a
// 30 s retry loop, so a failed send is re-offered even with no trader beating.
func Start(send func(text string) error, statePath string) BootInfo {
	defMu.Lock()
	defer defMu.Unlock()
	if def != nil {
		return BootInfo{}
	}
	def = NewE(send)
	info := BootInfo{PrevExit: PrevExitNone}
	if statePath != "" {
		info = def.EnableState(statePath)
	}
	p := def
	safe.GoNamed("owner-push-retry", func() {
		t := time.NewTicker(retryLoopEvery)
		defer t.Stop()
		for range t.C {
			p.Retry()
		}
	})
	return info
}

// StatsSnapshot is the process default's delivery counters; ok=false when no
// pusher runs (friend instance, never Started) - absent, never a fabricated 0.
func StatsSnapshot() (Stats, bool) {
	defMu.RLock()
	p := def
	defMu.RUnlock()
	if p == nil || p.send == nil {
		return Stats{}, false
	}
	return p.Stats(), true
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
