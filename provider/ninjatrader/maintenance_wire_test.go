package ninjatrader

import (
	"context"
	"encoding/json"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ── W-ONE-BUTTON M2 site 7 — the maintenance / maintenance_ack wire ────────
//
// Go → AddOn `maintenance {held, job_id}` is sent ONLY while the installation
// hold is present (on accept, on change, and re-sent every maintenanceResend so
// each ack carries a fresh census), plus ONE `held:false` release to a
// connection that was told it was held. With no hold file the wire is
// byte-identical to today: an old AddOn never sees an unknown frame.
//
// AddOn → Go `maintenance_ack` is recorded on the CURRENT connection's record
// only; a reconnect starts a fresh record (Q3: the verifier binds to the
// per-connection record, never to FarSideBuildID()).

type holdSrc struct {
	mu   sync.Mutex
	held bool
	job  string
}

func (h *holdSrc) set(held bool, job string) { h.mu.Lock(); h.held, h.job = held, job; h.mu.Unlock() }
func (h *holdSrc) get() (bool, string)       { h.mu.Lock(); defer h.mu.Unlock(); return h.held, h.job }

type wireClient struct {
	conn   net.Conn
	frames chan Envelope
}

func fastMaintenanceTimers(t *testing.T) {
	t.Helper()
	pt, pr := maintenanceTick, maintenanceResend
	maintenanceTick, maintenanceResend = 20*time.Millisecond, 150*time.Millisecond
	t.Cleanup(func() { maintenanceTick, maintenanceResend = pt, pr })
}

func startedServer(t *testing.T, src *holdSrc) *TCPServer {
	t.Helper()
	s := NewTCPServer(nil)
	s.SetAddrForTest("127.0.0.1:0")
	if src != nil {
		s.SetMaintenanceSource(src.get)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(); cancel() })
	return s
}

func dialWire(t *testing.T, s *TCPServer, hello HelloPayload) *wireClient {
	t.Helper()
	w := dialWireNoWait(t, s, hello)
	// GATE 3: in connect-key mode the conn is published only AFTER the hello is
	// authenticated, so IsConnected() flips true here (not at dial). Without a
	// key it is already true from accept; the wait is a no-op.
	if !waitConnected(s, 2*time.Second) {
		t.Fatal("dial: never became the connected client")
	}
	return w
}

// dialWireNoWait is dialWire WITHOUT the post-hello IsConnected wait — for a
// hello that is REFUSED (wrong/missing key), which never becomes "connected".
func dialWireNoWait(t *testing.T, s *TCPServer, hello HelloPayload) *wireClient {
	t.Helper()
	var c net.Conn
	var err error
	for i := 0; i < 100; i++ { // the single-client slot frees asynchronously after a close
		c, err = net.Dial("tcp", s.ListenAddrForTest().String())
		if err == nil {
			break
		}
		if c != nil {
			_ = c.Close()
			c = nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	if c == nil {
		t.Fatal("dial: never connected")
	}
	t.Cleanup(func() { _ = c.Close() })
	w := &wireClient{conn: c, frames: make(chan Envelope, 256)}
	go func() {
		for {
			env, err := ReadFrame(c)
			if err != nil {
				close(w.frames)
				return
			}
			w.frames <- env
		}
	}()
	if hello.ProtocolVersion == 0 {
		hello.ProtocolVersion = ProtocolVersion
	}
	if hello.Source == "" {
		hello.Source = "vltrader-addon"
	}
	if err := WriteFrame(c, FrameHello, hello); err != nil {
		t.Fatal(err)
	}
	return w
}

func waitConnected(s *TCPServer, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if s.IsConnected() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return s.IsConnected()
}

// next returns the next frame of type want within d (others skipped).
func (w *wireClient) next(want FrameType, d time.Duration) (Envelope, bool) {
	deadline := time.After(d)
	for {
		select {
		case env, ok := <-w.frames:
			if !ok {
				return Envelope{}, false
			}
			if env.Type == want {
				return env, true
			}
		case <-deadline:
			return Envelope{}, false
		}
	}
}

func maintenanceOf(t *testing.T, env Envelope) MaintenancePayload {
	t.Helper()
	var p MaintenancePayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNoMaintenanceFrameWithoutAHold(t *testing.T) {
	fastMaintenanceTimers(t)
	for name, src := range map[string]*holdSrc{"no source wired": nil, "source says not held": {}} {
		s := startedServer(t, src)
		w := dialWire(t, s, HelloPayload{})
		if env, ok := w.next(FrameMaintenance, 400*time.Millisecond); ok {
			t.Fatalf("%s: a maintenance frame reached the AddOn with no hold: %s", name, env.Payload)
		}
	}
}

func TestMaintenanceFrameOnAcceptWhileHeldAndResent(t *testing.T) {
	fastMaintenanceTimers(t)
	src := &holdSrc{}
	src.set(true, "job-w1")
	s := startedServer(t, src)
	w := dialWire(t, s, HelloPayload{})
	env, ok := w.next(FrameMaintenance, time.Second)
	if !ok {
		t.Fatal("a held installation must tell the AddOn on accept")
	}
	if p := maintenanceOf(t, env); !p.Held || p.JobID != "job-w1" {
		t.Fatalf("want held job-w1, got %+v", p)
	}
	if _, ok := w.next(FrameMaintenance, time.Second); !ok {
		t.Fatal("while held the frame must be re-sent (each ack carries a fresh census)")
	}
}

func TestMaintenanceReleaseSentOnceAfterAHold(t *testing.T) {
	fastMaintenanceTimers(t)
	src := &holdSrc{}
	src.set(true, "job-w2")
	s := startedServer(t, src)
	w := dialWire(t, s, HelloPayload{})
	if _, ok := w.next(FrameMaintenance, time.Second); !ok {
		t.Fatal("fixture: no held frame")
	}
	src.set(false, "")
	var release MaintenancePayload
	for {
		env, ok := w.next(FrameMaintenance, time.Second)
		if !ok {
			t.Fatal("the release (held:false) was never sent")
		}
		if p := maintenanceOf(t, env); !p.Held {
			release = p
			break
		}
	}
	if release.Held {
		t.Fatal("unreachable")
	}
	if env, ok := w.next(FrameMaintenance, 400*time.Millisecond); ok {
		t.Fatalf("after the release no further maintenance frame may be sent: %s", env.Payload)
	}
}

func TestMaintenanceAckIsRecordedOnTheCurrentConnectionOnly(t *testing.T) {
	fastMaintenanceTimers(t)
	s := startedServer(t, nil)
	w := dialWire(t, s, HelloPayload{})
	ack := MaintenanceAckPayload{Held: true, JobID: "job-w3", QueuedCommands: 2, BuildID: "2026-09-22-m2",
		Connections: []CensusConnection{{Sim: true, Connected: true}, {Sim: false, Connected: false}},
		Accounts:    []CensusAccount{{Sim: true, Positions: 0, Working: 1}}}
	if err := WriteFrame(w.conn, FrameMaintenanceAck, ack); err != nil {
		t.Fatal(err)
	}
	var rec ConnectionRecord
	var connected bool
	for i := 0; i < 200; i++ {
		rec, connected = s.ConnectionRecord()
		if rec.Ack != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !connected || rec.Ack == nil {
		t.Fatalf("the ack was not recorded on the current connection: %+v", rec)
	}
	if rec.Ack.JobID != "job-w3" || rec.Ack.QueuedCommands != 2 || len(rec.Ack.Connections) != 2 || len(rec.Ack.Accounts) != 1 || rec.AckMonoMs <= 0 {
		t.Fatalf("ack recorded wrongly: %+v / %+v", rec, *rec.Ack)
	}
	firstSeq := rec.AcceptSeq

	// Reconnect: a fresh record — the old connection's ack must not carry over.
	_ = w.conn.Close()
	for i := 0; i < 200 && s.IsConnected(); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	_ = dialWire(t, s, HelloPayload{})
	rec2, ok := s.ConnectionRecord()
	if !ok || rec2.AcceptSeq <= firstSeq {
		t.Fatalf("a reconnect must start a new record with a higher accept_seq: %+v (first %d)", rec2, firstSeq)
	}
	if rec2.Ack != nil {
		t.Fatal("an ack from a previous connection must never be reported for the current one")
	}
}

// m456-mainack — "connected" and "its record" must appear together. The accept
// path used to publish s.conn (IsConnected()==true) and only AFTERWARDS replace
// the connection record, so a reader that saw the connection could still get the
// PREVIOUS connection's record (old accept_seq, old ack, connected=false). That
// was the rare flake in TestMaintenanceAckIsRecordedOnTheCurrentConnectionOnly.
// The accept clock is the production seam that is evaluated inside that window;
// parking it holds the accept goroutine there deterministically.
func TestConnectedImpliesItsConnectionRecordExists(t *testing.T) {
	t.Run("no_key_publish_at_accept", func(t *testing.T) { connectedImpliesRecord(t, "") })
	// GATE 3: with a connect key the conn is published at the authenticated hello
	// (authenticateKeyHello) — the second site with the same publish-then-record order.
	t.Run("connect_key_publish_at_hello", func(t *testing.T) {
		t.Setenv("NT_CONNECT_KEY", "m456-test-key")
		connectedImpliesRecord(t, "m456-test-key")
	})
}

func connectedImpliesRecord(t *testing.T, key string) {
	t.Helper()
	fastMaintenanceTimers(t)
	s := startedServer(t, nil)
	w := dialWire(t, s, HelloPayload{ConnectKey: key})
	if err := WriteFrame(w.conn, FrameMaintenanceAck, MaintenanceAckPayload{Held: true, JobID: "old-link"}); err != nil {
		t.Fatal(err)
	}
	var rec ConnectionRecord
	for i := 0; i < 200; i++ {
		rec, _ = s.ConnectionRecord()
		if rec.Ack != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if rec.Ack == nil {
		t.Fatal("fixture: the first connection's ack was not recorded")
	}
	firstSeq := rec.AcceptSeq
	_ = w.conn.Close()
	for i := 0; i < 400 && s.IsConnected(); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if s.IsConnected() {
		t.Fatal("fixture: the first connection never retired")
	}

	var calls atomic.Int32
	inWindow, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unpark := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unpark) // a failing assertion must not leave the accept goroutine parked (Stop would hang)
	s.SetAcceptClockForTest(func() time.Time {
		if calls.Add(1) == 1 {
			close(inWindow)
			<-release // park the accept goroutine at the accept instant
		}
		return time.Now()
	})
	_ = dialWireNoWait(t, s, HelloPayload{ConnectKey: key})
	select {
	case <-inWindow:
	case <-time.After(5 * time.Second):
		t.Fatal("the accept clock was never consulted")
	}
	// Whenever the connection is visible as connected, its own record must be
	// the one ConnectionRecord returns — never the previous link's.
	check := func(stage string) {
		t.Helper()
		if !s.IsConnected() {
			return
		}
		r, ok := s.ConnectionRecord()
		if !ok || r.AcceptSeq <= firstSeq || r.Ack != nil {
			t.Fatalf("%s: IsConnected() is true but ConnectionRecord() is the previous connection's: ok=%v accept_seq=%d (first %d) ack=%v",
				stage, ok, r.AcceptSeq, firstSeq, r.Ack != nil)
		}
	}
	for end := time.Now().Add(300 * time.Millisecond); time.Now().Before(end); time.Sleep(time.Millisecond) {
		check("accept parked")
	}
	unpark()
	if !waitConnected(s, 2*time.Second) {
		t.Fatal("the new connection never became the connected client")
	}
	check("accept released")
	if r, ok := s.ConnectionRecord(); !ok || r.AcceptSeq <= firstSeq {
		t.Fatalf("after the accept completed the new record must be current: ok=%v accept_seq=%d (first %d)", ok, r.AcceptSeq, firstSeq)
	}
}

func TestHelloEpochFieldsAreRecordedPerConnection(t *testing.T) {
	s := startedServer(t, nil)
	w := dialWire(t, s, HelloPayload{BuildID: "2026-09-22-m2", NT8PID: 4242, NT8StartMs: 1790000000000,
		AssemblyMVID: "mvid-1", SourceHash: "sha-1", ActivationNonce: "nonce-1"})
	var rec ConnectionRecord
	for i := 0; i < 200; i++ {
		rec, _ = s.ConnectionRecord()
		if rec.Hello != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if rec.Hello == nil {
		t.Fatal("the hello was not recorded on the connection record")
	}
	h := rec.Hello
	if h.NT8PID != 4242 || h.NT8StartMs != 1790000000000 || h.AssemblyMVID != "mvid-1" || h.SourceHash != "sha-1" || h.ActivationNonce != "nonce-1" || h.BuildID != "2026-09-22-m2" {
		t.Fatalf("hello epoch fields not recorded: %+v", *h)
	}
	_, port, _ := net.SplitHostPort(w.conn.LocalAddr().String())
	if strconv.Itoa(rec.RemotePort) != port {
		t.Fatalf("remote_port %d is not the client's port %s", rec.RemotePort, port)
	}
	if rec.AcceptSeq == 0 || rec.AcceptMonoMs < 0 {
		t.Fatalf("accept_seq / accept_mono not set: %+v", rec)
	}
}

// The Go hello reply stays byte-identical (the new fields are omitempty).
func TestGoHelloReplyIsByteIdentical(t *testing.T) {
	s := startedServer(t, nil)
	w := dialWire(t, s, HelloPayload{})
	env, ok := w.next(FrameHello, time.Second)
	if !ok {
		t.Fatal("no hello reply")
	}
	if got, want := string(env.Payload), `{"protocol_version":3,"source":"vl-go"}`; got != want {
		t.Fatalf("the Go hello reply changed on the wire:\n got %s\nwant %s", got, want)
	}
}

// The accept path tells a held installation IMMEDIATELY (before the queued
// flush), not on the next poll: with the poll effectively off, the frame still
// arrives.
func TestMaintenanceFrameIsSentAtAcceptNotOnlyByThePoll(t *testing.T) {
	pt, pr := maintenanceTick, maintenanceResend
	maintenanceTick, maintenanceResend = time.Hour, time.Hour
	t.Cleanup(func() { maintenanceTick, maintenanceResend = pt, pr })
	src := &holdSrc{}
	src.set(true, "job-accept")
	s := startedServer(t, src)
	w := dialWire(t, s, HelloPayload{})
	env, ok := w.next(FrameMaintenance, time.Second)
	if !ok {
		t.Fatal("the accept path must push the hold without waiting for the poll")
	}
	if p := maintenanceOf(t, env); !p.Held || p.JobID != "job-accept" {
		t.Fatalf("want held job-accept, got %+v", p)
	}
}

// M2.1 (review item a) — an ENUMERATED empty census list is [] on the record,
// never nil: absent ≠ [] in BOTH directions. ConnectionRecord's copy used
// append([]T(nil), empty...), which is nil — so the gate read a census the
// AddOn DID take as "not enumerated".
func TestConnectionRecordKeepsAnEnumeratedEmptyCensusEmpty(t *testing.T) {
	s := startedServer(t, nil)
	w := dialWire(t, s, HelloPayload{})
	body := []byte(`{"held":true,"job_id":"j","queued_commands":0,"connections":[],"accounts":[]}`)
	var ack MaintenanceAckPayload
	if err := json.Unmarshal(body, &ack); err != nil {
		t.Fatal(err)
	}
	if ack.Connections == nil || ack.Accounts == nil {
		t.Fatal("fixture: [] must decode non-nil")
	}
	if err := WriteFrame(w.conn, FrameMaintenanceAck, ack); err != nil {
		t.Fatal(err)
	}
	var rec ConnectionRecord
	for i := 0; i < 200; i++ {
		if rec, _ = s.ConnectionRecord(); rec.Ack != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if rec.Ack == nil {
		t.Fatal("ack not recorded")
	}
	if rec.Ack.Connections == nil || rec.Ack.Accounts == nil {
		t.Fatalf("an enumerated empty census must stay [] (not nil = not enumerated): connections=%v accounts=%v", rec.Ack.Connections == nil, rec.Ack.Accounts == nil)
	}
}
