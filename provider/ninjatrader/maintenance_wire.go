package ninjatrader

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// ── W-ONE-BUTTON M2 site 7 — maintenance frames + the per-connection record ─
//
// The installation hold lives in a file the provider package never reads; the
// trader package hands the server a source (SetMaintenanceSource) exactly as it
// hands it the entry-hold check. The server pushes the hold to the AddOn and
// records what the AddOn answers on the CURRENT connection's record, which a
// reconnect replaces: an ack, a hello, an epoch from connection N is never
// reported for connection N+1 (CTO ruling Q3 — the verifier binds to this
// record, never to FarSideBuildID()).

// Package vars so tests can compress them.
var (
	maintenanceTick   = time.Second     // how often the hold source is polled
	maintenanceResend = 5 * time.Second // re-send cadence while held (fresh census per ack)
)

// wireMonoBase anchors every *_mono_ms field on the process's monotonic clock
// (M1 §7: a WSL wall-clock step moved start times by 151 s; the monotonic
// reading of time.Now() survives it).
var wireMonoBase = time.Now()

func monoMs(t time.Time) int64 { return t.Sub(wireMonoBase).Milliseconds() }

// ConnectionRecord is what the server knows about ONE accepted AddOn
// connection. Hello and Ack are nil until received on this connection.
type ConnectionRecord struct {
	AcceptSeq    uint64                 `json:"accept_seq"`     // 1, 2, 3 … per accept, per process
	AcceptMonoMs int64                  `json:"accept_mono_ms"` // monotonic ms since process start
	RemotePort   int                    `json:"remote_port"`
	Hello        *HelloPayload          `json:"hello"`
	HelloMonoMs  int64                  `json:"hello_mono_ms,omitempty"`
	Ack          *MaintenanceAckPayload `json:"maintenance_ack"`
	AckMonoMs    int64                  `json:"ack_mono_ms,omitempty"`
	// DisconnectedMonoMs is when THIS connection closed, on the monotonic
	// clock (UPDATER-NT8-CLOSED: the nt8_absent verdict is measured from
	// this stamp, never inferred from a stale ack). Zero = never connected
	// or still connected.
	DisconnectedMonoMs int64 `json:"disconnected_mono_ms,omitempty"`
	// Listening / ListenMonoMs are SERVER facts (not per-connection), filled in
	// by ConnectionRecord(): the listener is up, and since when on the
	// monotonic clock. They let a bot that booted with NT8 closed measure
	// "no AddOn connection since the server began listening" (LinkDownSince).
	Listening    bool  `json:"listening,omitempty"`
	ListenMonoMs int64 `json:"listen_mono_ms,omitempty"`
	// What this connection was last TOLD (so a release goes only to a
	// connection that was held, and a resend keeps the census fresh).
	SentHeld   bool   `json:"sent_held"`
	SentJobID  string `json:"sent_job_id,omitempty"`
	SentMonoMs int64  `json:"sent_mono_ms,omitempty"`
}

type maintenanceWire struct {
	src atomic.Value // func() (held bool, jobID string)
	// tick / resend are copied from the package vars at Start, so a test that
	// compresses them never races a previous server's loop.
	tick, resend time.Duration
	mu           sync.Mutex
	seq          uint64
	conn         net.Conn // the connection rec describes
	rec          ConnectionRecord
	// listening/listenMono: the listener came up (set once, in Start).
	listening  bool
	listenMono int64

	sinkMu sync.Mutex
	sinks  map[string]func(DroppedEntry) // owner → drop sink
}

// SetMaintenanceSource wires the installation hold (trader.maintenanceWireState).
// Unwired = never held = no maintenance frame, ever (byte-identical wire).
func (s *TCPServer) SetMaintenanceSource(fn func() (held bool, jobID string)) {
	if fn != nil {
		s.maint.src.Store(fn)
	}
}

// ConnectionRecord returns a copy of the current connection's record and
// whether that connection is still the connected client. (rec, false) after a
// disconnect: the record is history, not a live AddOn.
func (s *TCPServer) ConnectionRecord() (ConnectionRecord, bool) {
	s.connMu.Lock()
	cur := s.conn
	s.connMu.Unlock()
	s.maint.mu.Lock()
	defer s.maint.mu.Unlock()
	rec := s.maint.rec
	rec.Listening, rec.ListenMonoMs = s.maint.listening, s.maint.listenMono
	if rec.Hello != nil {
		h := *rec.Hello
		rec.Hello = &h
	}
	if rec.Ack != nil {
		// A deep copy that keeps nil nil AND [] [] (absent ≠ [] both ways —
		// append([]T(nil), empty...) would turn an enumerated [] into "absent").
		a := *rec.Ack
		if rec.Ack.Connections != nil {
			a.Connections = make([]CensusConnection, len(rec.Ack.Connections))
			copy(a.Connections, rec.Ack.Connections)
		}
		if rec.Ack.Accounts != nil {
			a.Accounts = make([]CensusAccount, len(rec.Ack.Accounts))
			copy(a.Accounts, rec.Ack.Accounts)
		}
		rec.Ack = &a
	}
	return rec, cur != nil && cur == s.maint.conn
}

// beginConnectionRecord starts a fresh record for an accepted connection.
func (s *TCPServer) beginConnectionRecord(c net.Conn, now time.Time) {
	port := 0
	if ta, ok := c.RemoteAddr().(*net.TCPAddr); ok {
		port = ta.Port
	}
	s.maint.mu.Lock()
	s.maint.seq++
	s.maint.conn = c
	s.maint.rec = ConnectionRecord{AcceptSeq: s.maint.seq, AcceptMonoMs: monoMs(now), RemotePort: port}
	s.maint.mu.Unlock()
}

func (s *TCPServer) recordHello(c net.Conn, p HelloPayload, now time.Time) {
	s.maint.mu.Lock()
	defer s.maint.mu.Unlock()
	if s.maint.conn != c {
		return
	}
	s.maint.rec.Hello = &p
	s.maint.rec.HelloMonoMs = monoMs(now)
}

func (s *TCPServer) recordMaintenanceAck(c net.Conn, p MaintenanceAckPayload, now time.Time) bool {
	s.maint.mu.Lock()
	defer s.maint.mu.Unlock()
	if s.maint.conn != c {
		return false
	}
	s.maint.rec.Ack = &p
	s.maint.rec.AckMonoMs = monoMs(now)
	return true
}

// pushMaintenance tells connection c the hold state when it must: held and
// (not yet told, a different job, or the resend interval elapsed); or released
// after having been told held. Anything else sends nothing.
func (s *TCPServer) pushMaintenance(c net.Conn, now time.Time) {
	fn, _ := s.maint.src.Load().(func() (bool, string))
	if fn == nil || c == nil {
		return
	}
	held, job := fn()
	if !held {
		job = ""
	}
	s.maint.mu.Lock()
	if s.maint.conn != c {
		s.maint.mu.Unlock()
		return
	}
	r := s.maint.rec
	changed := held != r.SentHeld || job != r.SentJobID
	resend := s.maint.resend
	if resend <= 0 {
		resend = 5 * time.Second
	}
	due := held && (changed || monoMs(now)-r.SentMonoMs >= resend.Milliseconds())
	if !due && !(changed && !held) {
		s.maint.mu.Unlock()
		return
	}
	s.maint.mu.Unlock()

	s.writeMu.Lock()
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err := WriteFrame(c, FrameMaintenance, MaintenancePayload{Held: held, JobID: job})
	s.writeMu.Unlock()
	if err != nil {
		s.logger.Warn("tcp_server: write maintenance frame", "err", err)
		return
	}
	s.maint.mu.Lock()
	if s.maint.conn == c {
		s.maint.rec.SentHeld, s.maint.rec.SentJobID, s.maint.rec.SentMonoMs = held, job, monoMs(now)
	}
	s.maint.mu.Unlock()
	if changed {
		s.logger.Info("tcp_server: 🔒 maintenance frame sent", "held", held, "job_id", job)
	}
}

// maintenanceLoop polls the hold source and pushes changes/resends to the
// connected AddOn. Exits with ctx (not wg-tracked, like livenessReporter).
func (s *TCPServer) maintenanceLoop(ctxDone <-chan struct{}, tick time.Duration) {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctxDone:
			return
		case now := <-t.C:
			// A hold that lands while NT8 is down must not leave entries parked
			// in the queue for as long as it stays down.
			if s.entryHeld() && s.PendingSignalCount() > 0 {
				s.dropQueuedWhileHeld()
			}
			s.connMu.Lock()
			c := s.conn
			s.connMu.Unlock()
			s.pushMaintenance(c, now)
		}
	}
}

// AckAge is how long ago this record's maintenance_ack arrived, on the
// monotonic clock; ok=false when this connection has not acked.
func (r ConnectionRecord) AckAge() (time.Duration, bool) {
	if r.Ack == nil {
		return 0, false
	}
	return time.Duration(monoMs(time.Now())-r.AckMonoMs) * time.Millisecond, true
}

// DisconnectedAt is when THIS connection closed, on the process's monotonic
// clock (wireMonoBase). ok=false when the record has no disconnect stamp
// (never connected, or still connected).
func (r ConnectionRecord) DisconnectedAt() (time.Time, bool) {
	if r.DisconnectedMonoMs <= 0 {
		return time.Time{}, false
	}
	return wireMonoBase.Add(time.Duration(r.DisconnectedMonoMs) * time.Millisecond), true
}

// LinkDownSince is when the AddOn link went down, for the nt8_absent verdict
// (UPDATER-NT8-CLOSED: measured, never inferred). connected is the live-link
// flag ConnectionRecord returned beside this record. ok=false (fail closed)
// when the link is up or the stamp cannot be measured:
//   - a connection that closed: its disconnect stamp (a reconnect replaces the
//     record, so the stamp never outlives the link it measured);
//   - NEVER connected since the server began listening (AcceptSeq 0): the
//     moment the listener came up. A bot that booted with NT8 closed has no
//     disconnect stamp to measure from, but "no AddOn connection since boot"
//     is itself a measured fact — without it the closed-NT8 install is
//     unreachable after any bot/WSL restart with NT8 off (job de4cf900,
//     2026-10-04 07:27: preflight refused "api_positions … NT8 account
//     positions unknown" and the owner had to open NT8);
//   - a record that WAS accepted but carries no stamp while not connected is
//     not a shape closeConn writes: still unmeasured, still refused.
func (r ConnectionRecord) LinkDownSince(connected bool) (time.Time, bool) {
	if connected {
		return time.Time{}, false
	}
	if t, ok := r.DisconnectedAt(); ok {
		return t, true
	}
	if r.AcceptSeq == 0 && r.Listening {
		return wireMonoBase.Add(time.Duration(r.ListenMonoMs) * time.Millisecond), true
	}
	return time.Time{}, false
}

// MaintenanceAckMaxAge is the oldest ack the installation gate accepts: three
// resend intervals (the AddOn re-acks every resend while held).
func MaintenanceAckMaxAge() time.Duration { return 3 * maintenanceResend }

// DroppedEntry is one queued entry the maintenance hold removed from the
// reconnect queue (W-ONE-BUTTON M2, CTO condition 1 on M-2).
type DroppedEntry struct {
	SignalID string
	TraderID string
	Account  string
	Symbol   string
	Side     string
	// Attempted: a write of this signal's frame was STARTED on some connection
	// before the drop (timedSignal.attempted). false = zero bytes were ever
	// written — the only case a caller may settle as "never sent".
	Attempted bool
	// Own (M2.1, review 2 N2): dropped INSIDE the SendSignal call of this same
	// signal — its caller learns of it from the returned error (ErrEntryHeld /
	// ErrEntryDropAmbiguous) and records nothing on success paths. false = it
	// was queued earlier and its caller already recorded it as sent.
	Own bool
}

// AddDroppedEntrySink registers fn (under owner; a re-registration replaces
// it) to hear every entry the hold drops. Called outside every server lock.
func (s *TCPServer) AddDroppedEntrySink(owner string, fn func(DroppedEntry)) {
	s.maint.sinkMu.Lock()
	defer s.maint.sinkMu.Unlock()
	if s.maint.sinks == nil {
		s.maint.sinks = map[string]func(DroppedEntry){}
	}
	if fn == nil {
		delete(s.maint.sinks, owner)
		return
	}
	s.maint.sinks[owner] = fn
}

// dispatchDrops hands each drop to every sink. Never called under a lock the
// sinks could need (they write the ledger and log).
func (s *TCPServer) dispatchDrops(ds []DroppedEntry) {
	if len(ds) == 0 {
		return
	}
	s.maint.sinkMu.Lock()
	fns := make([]func(DroppedEntry), 0, len(s.maint.sinks))
	for _, fn := range s.maint.sinks {
		fns = append(fns, fn)
	}
	s.maint.sinkMu.Unlock()
	for _, d := range ds {
		for _, fn := range fns {
			fn(d)
		}
	}
}

// dropQueuedWhileHeld empties the reconnect queue while the installation is
// held — connected or not — logs and reports every entry, and returns their
// ids. Nothing is ever re-queued.
func (s *TCPServer) dropQueuedWhileHeld() map[string]bool { return s.dropQueuedWhileHeldFor("") }

// dropQueuedWhileHeldFor is dropQueuedWhileHeld inside the SendSignal call of
// signal own (that entry's drop is reported Own).
func (s *TCPServer) dropQueuedWhileHeldFor(own string) map[string]bool {
	s.pendingMu.Lock()
	queued := append([]timedSignal(nil), s.pending...)
	s.pending = s.pending[:0]
	s.pendingMu.Unlock()
	return s.reportDrops(queued, own)
}

func (s *TCPServer) reportDrops(queued []timedSignal, own string) map[string]bool {
	if len(queued) == 0 {
		return nil
	}
	ids := make(map[string]bool, len(queued))
	ds := make([]DroppedEntry, 0, len(queued))
	for _, q := range queued {
		sig := q.payload
		// signal id → attempted; a duplicate id is attempted if ANY copy was.
		ids[sig.SignalID] = ids[sig.SignalID] || q.attempted
		ds = append(ds, DroppedEntry{SignalID: sig.SignalID, TraderID: sig.TraderID, Account: sig.Account,
			Symbol: sig.Symbol, Side: sig.Side, Attempted: q.attempted, Own: own != "" && sig.SignalID == own})
		s.logger.Warn("tcp_server: 🔒 maintenance hold — queued entry DROPPED, not sent",
			"signal_id", sig.SignalID, "symbol", sig.Symbol, "side", sig.Side, "attempted", q.attempted)
	}
	s.dispatchDrops(ds)
	return ids
}

// FeedDroppedEntryForTest dispatches d to the registered drop sinks exactly as
// a held queue drop does (the *ForTest seam family, like FeedOrderUpdateForTest).
func (s *TCPServer) FeedDroppedEntryForTest(d DroppedEntry) { s.dispatchDrops([]DroppedEntry{d}) }

// ErrEntryDropAmbiguous is returned by SendSignal when its OWN entry was
// dropped by the maintenance hold AFTER a write of it had been started (by a
// concurrent flush): it may have reached NT8. Deliberately NOT a hold refusal
// (IsMaintenanceHold is false), so callers keep their records pending — an
// ambiguous send — and the installation gate stays closed (review F3; CTO
// condition 1 on M-2: only a never-attempted drop is "never sent").
var ErrEntryDropAmbiguous = errors.New("entry dropped by the maintenance hold after a write of it was started — it may have reached NT8")

// ownDropError is SendSignal's verdict on its own entry, from the drop report
// (signal id → attempted): not dropped → nil; dropped, never attempted →
// ErrEntryHeld (provably unsent); dropped after an attempt → ErrEntryDropAmbiguous.
func ownDropError(signalID string, drops map[string]bool) error {
	attempted, dropped := drops[signalID]
	switch {
	case !dropped:
		return nil
	case attempted:
		return fmt.Errorf("tcp_server: signal %s: %w", signalID, ErrEntryDropAmbiguous)
	}
	return fmt.Errorf("tcp_server: signal %s not sent: %w", signalID, ErrEntryHeld)
}
