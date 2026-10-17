package wireserver

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"vl/internal/safepath"

	"vl/internal/updaterwire"
)

// ── M3 worker channel, listening side — forged requests are refused and
// logged; every refusal has a positive control: the same request with the
// one defect removed reaches the handler. ─────────────────────────────────

type logSink struct {
	mu    sync.Mutex
	lines []string
}

func (s *logSink) logf(format string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines = append(s.lines, fmt.Sprintf(format, args...))
}

func (s *logSink) all() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.lines, "\n")
}

// dataDir returns a short temp <base>/data (sun_path is 107 bytes) and the
// socket path SocketPath resolves in it. The updater dir is NOT created:
// Listen creates it.
func dataDir(t *testing.T) (string, string) {
	t.Helper()
	base, err := os.MkdirTemp("", "m3s")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = safepath.RemoveAllTemp(base) })
	d := filepath.Join(base, "data")
	if err := os.Mkdir(d, 0o755); err != nil {
		t.Fatal(err)
	}
	sock, err := updaterwire.SocketPath(d)
	if err != nil {
		t.Fatal(err)
	}
	return d, sock
}

type served struct {
	l     *Listener
	calls atomic.Int32
	last  atomic.Value // updaterwire.Request
	logs  *logSink
	done  chan error
	once  sync.Once
}

// stop closes the listener and waits for Serve to return. Serve waits for
// every serveConn goroutine (defer wg.Wait()), so after stop no goroutine of
// this server reads a package seam, and the receive from done is the
// happens-before edge that lets a test change the seam (M3 race run at
// a13b866a: peerUID swapped under a live Serve). Idempotent; the cleanup
// calls it too.
func (s *served) stop(t *testing.T) {
	t.Helper()
	s.once.Do(func() {
		s.l.Close()
		select {
		case <-s.done:
		case <-time.After(5 * time.Second):
			t.Error("Serve did not return after Close")
		}
	})
}

func serve(t *testing.T, sock string) *served {
	t.Helper()
	s := &served{logs: &logSink{}, done: make(chan error, 1)}
	l, err := Listen(sock, s.logs.logf)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	s.l = l
	go func() {
		s.done <- l.Serve(func(r updaterwire.Request) updaterwire.Response {
			s.calls.Add(1)
			s.last.Store(r)
			return updaterwire.Response{OK: true, State: "idle"}
		})
	}()
	t.Cleanup(func() { s.stop(t) })
	return s
}

// rawExchange writes raw bytes on a fresh connection and returns everything
// the server sends back until it closes the connection.
func rawExchange(t *testing.T, sock string, raw []byte) []byte {
	t.Helper()
	c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	go func() { c.Write(raw) }()
	out, _ := io.ReadAll(c)
	return out
}

const validStatus = `{"v":1,"verb":"status","payload":{}}`

func TestServeStatusRoundTripViaDial(t *testing.T) {
	_, sock := dataDir(t)
	s := serve(t, sock)
	// the socket and its dir are owner-only
	if fi, err := os.Lstat(sock); err != nil || fi.Mode()&fs.ModeSocket == 0 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %v, %v; want a 0600 socket", fi.Mode(), err)
	}
	if fi, err := os.Lstat(filepath.Dir(sock)); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("updater dir mode = %v, %v; want 0700", fi.Mode().Perm(), err)
	}
	c, err := updaterwire.Dial(sock)
	if err != nil {
		t.Fatalf("the app's Dial must reach the worker's Listen: %v", err)
	}
	defer c.Close()
	for _, req := range []updaterwire.Request{
		updaterwire.NewStatus(""),
		updaterwire.NewInstall("v1.4.2", "job-0001abcd"),
		updaterwire.NewCancelBeforeBoundary("job-0001abcd"),
	} {
		resp, err := c.Do(req)
		if err != nil || !resp.OK || resp.State != "idle" {
			t.Fatalf("%s: resp=%+v err=%v", req.Verb, resp, err)
		}
		if got := s.last.Load().(updaterwire.Request); got.Verb != req.Verb {
			t.Fatalf("handler saw verb %q, want %q", got.Verb, req.Verb)
		}
	}
	if n := s.calls.Load(); n != 3 {
		t.Fatalf("handler calls = %d, want 3 (one per request on one connection)", n)
	}
	if l := s.logs.all(); l != "" {
		t.Fatalf("a valid exchange must log nothing, got %q", l)
	}
}

func TestServeRejectsForgedFrames(t *testing.T) {
	_, sock := dataDir(t)
	s := serve(t, sock)
	const marker = "FORGED-MARKER-7f3a"
	rejected, _ := updaterwire.EncodeResponse(updaterwire.RejectedResponse)
	cases := []struct {
		name, reason string
		raw          []byte
	}{
		{"unknown verb", "unknown_verb", []byte(`{"v":1,"verb":"exec","payload":{"cmd":"` + marker + `"}}` + "\n")},
		{"re-cased verb", "unknown_verb", []byte(`{"v":1,"verb":"STATUS","payload":{}}` + "\n")},
		{"extra top field", "unknown_field", []byte(`{"v":1,"verb":"status","payload":{},"x":"` + marker + `"}` + "\n")},
		{"extra install field", "unknown_field", []byte(`{"v":1,"verb":"install","payload":{"release_id":"v1.4.2","job_id":"job-0001abcd","url":"https://` + marker + `"}}` + "\n")},
		{"malformed json", "malformed", []byte(`{"v":1,"verb":` + marker + "\n")},
		{"trailing garbage", "trailing_data", []byte(validStatus + ` ` + marker + "\n")},
		{"path release id", "bad_payload", []byte(`{"v":1,"verb":"install","payload":{"release_id":"../` + marker + `","job_id":"job-0001abcd"}}` + "\n")},
		{"oversize frame", "oversize", append(append([]byte(`{"v":1,"verb":"status","payload":{"job_id":"`+marker), bytes.Repeat([]byte("a"), updaterwire.MaxFrameBytes)...), []byte("\"}}\n")...)},
		{"eof mid frame", "truncated", []byte(`{"v":1,"verb":"status","payload":{"` + marker)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := s.calls.Load()
			beforeLog := len(s.logs.all())
			var out []byte
			if c.reason == "truncated" {
				// half-close so the server sees EOF mid-frame
				conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: sock, Net: "unix"})
				if err != nil {
					t.Fatal(err)
				}
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				conn.Write(c.raw)
				conn.CloseWrite()
				out, _ = io.ReadAll(conn)
				conn.Close()
			} else {
				out = rawExchange(t, sock, c.raw)
			}
			if !bytes.Equal(out, rejected) {
				t.Fatalf("server answered %q, want exactly the rejected frame %q then close", out, rejected)
			}
			if s.calls.Load() != before {
				t.Fatal("the handler was called for a forged frame")
			}
			logged := s.logs.all()[beforeLog:]
			if !strings.Contains(logged, "updater-wire: rejected reason="+c.reason+" len=") {
				t.Fatalf("log = %q, want a rejected reason=%s line with a length+hash", logged, c.reason)
			}
			if strings.Contains(logged, marker) {
				t.Fatalf("the log echoes the forged payload: %q", logged)
			}
		})
	}
	// positive control: the valid frame on a fresh connection reaches the handler
	before := s.calls.Load()
	c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	c.Write([]byte(validStatus + "\n"))
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	if resp, err := updaterwire.DecodeResponse(bytes.TrimSuffix(line, []byte("\n"))); err != nil || !resp.OK {
		t.Fatalf("positive control: %q %v", line, err)
	}
	if s.calls.Load() != before+1 {
		t.Fatal("positive control: the valid frame did not reach the handler")
	}
}

func TestServeRejectsARefusedFrameAfterAValidOne(t *testing.T) {
	// a good frame does not buy trust for the next one on the same connection
	_, sock := dataDir(t)
	s := serve(t, sock)
	rejected, _ := updaterwire.EncodeResponse(updaterwire.RejectedResponse)
	ok, _ := updaterwire.EncodeResponse(updaterwire.Response{OK: true, State: "idle"})
	out := rawExchange(t, sock, []byte(validStatus+"\n"+`{"v":1,"verb":"exec","payload":{}}`+"\n"+validStatus+"\n"))
	if want := append(append([]byte{}, ok...), rejected...); !bytes.Equal(out, want) {
		t.Fatalf("got %q, want the ok frame, the rejected frame, then close (the third frame never served)", out)
	}
	if n := s.calls.Load(); n != 1 {
		t.Fatalf("handler calls = %d, want 1", n)
	}
}

func TestServeRejectsAnotherUIDPeer(t *testing.T) {
	// One server per seam value: the seam is set while no Serve runs, and
	// the server is stopped (Serve returned, every serveConn goroutine done)
	// before the seam changes again. Swapping peerUID under a live Serve was
	// a data race the M3 race run found at a13b866a (server.go serveConn
	// reads it per connection; the socket gives no happens-before edge).
	orig := peerUID
	t.Cleanup(func() { peerUID = orig }) // registered first, so it runs after every server's stop
	phase := func(seam func(*net.UnixConn) (uint32, error)) (*served, string) {
		t.Helper()
		peerUID = seam
		_, sock := dataDir(t)
		return serve(t, sock), sock
	}

	s, sock := phase(func(*net.UnixConn) (uint32, error) { return uint32(os.Geteuid()) + 1, nil })
	if out := rawExchange(t, sock, []byte(validStatus+"\n")); len(out) != 0 {
		t.Fatalf("another uid's peer must be closed before any read, got %q", out)
	}
	s.stop(t)
	if s.calls.Load() != 0 {
		t.Fatal("the handler was called for another uid's peer")
	}
	if l := s.logs.all(); !strings.Contains(l, fmt.Sprintf("updater-wire: rejected reason=peer_uid uid=%d", os.Geteuid()+1)) {
		t.Fatalf("log = %q, want the peer_uid refusal", l)
	}

	s, sock = phase(func(*net.UnixConn) (uint32, error) { return 0, errors.New("getsockopt failed") })
	if out := rawExchange(t, sock, []byte(validStatus+"\n")); len(out) != 0 {
		t.Fatalf("an unreadable SO_PEERCRED must be refused (fail closed), got %q", out)
	}
	s.stop(t)
	if s.calls.Load() != 0 {
		t.Fatal("the handler was called for a peer whose uid could not be read")
	}
	if l := s.logs.all(); !strings.Contains(l, "reason=peer_uid uid=unknown") {
		t.Fatalf("log = %q, want the unknown-uid refusal", l)
	}

	// positive control: the real SO_PEERCRED (our own uid) is served
	s, sock = phase(orig)
	ok, _ := updaterwire.EncodeResponse(updaterwire.Response{OK: true, State: "idle"})
	c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	c.Write([]byte(validStatus + "\n"))
	if line, err := bufio.NewReader(c).ReadBytes('\n'); err != nil || !bytes.Equal(line, ok) {
		t.Fatalf("positive control: got %q %v", line, err)
	}
	if s.calls.Load() != 1 {
		t.Fatal("positive control did not reach the handler")
	}
}

func TestServeRefusesAnInvalidHandlerResponse(t *testing.T) {
	_, sock := dataDir(t)
	sink := &logSink{}
	l, err := Listen(sock, sink.logf)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- l.Serve(func(r updaterwire.Request) updaterwire.Response {
			if r.Verb == updaterwire.VerbCancelBeforeBoundary {
				panic("boom")
			}
			return updaterwire.Response{OK: true, State: "idle\nFAKE LOG LINE"}
		})
	}()
	defer func() { l.Close(); <-done }()
	c, err := updaterwire.Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	resp, err := c.Do(updaterwire.NewStatus(""))
	if err != nil || resp != internalResponse {
		t.Fatalf("an invalid handler response must be replaced by %+v, got %+v %v", internalResponse, resp, err)
	}
	resp, err = c.Do(updaterwire.NewCancelBeforeBoundary("job-0001abcd"))
	if err != nil || resp != internalResponse {
		t.Fatalf("a handler panic must answer %+v and keep serving, got %+v %v", internalResponse, resp, err)
	}
	if strings.Contains(sink.all(), "FAKE LOG LINE") {
		t.Fatal("the handler's invalid text reached the log")
	}
}

func TestServeRefusesANilHandler(t *testing.T) {
	_, sock := dataDir(t)
	l, err := Listen(sock, (&logSink{}).logf)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.Serve(nil); !errors.Is(err, ErrNilHandler) {
		t.Fatalf("Serve(nil) = %v, want ErrNilHandler", err)
	}
}

func TestListenRefusesASymlinkAtTheSocketPath(t *testing.T) {
	_, sock := dataDir(t)
	if err := os.Mkdir(filepath.Dir(sock), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(filepath.Dir(filepath.Dir(sock)), "victim")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, sock); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(sock, nil); !errors.Is(err, updaterwire.ErrSymlink) {
		t.Fatalf("a symlink at the socket path must be refused with ErrSymlink, got %v", err)
	}
	if fi, err := os.Lstat(sock); err != nil || fi.Mode()&fs.ModeSymlink == 0 {
		t.Fatal("the refused symlink must be left in place")
	}
	if b, _ := os.ReadFile(target); string(b) != "keep" {
		t.Fatal("the symlink's target was touched")
	}
	// positive control: the one defect removed
	os.Remove(sock)
	l, err := Listen(sock, nil)
	if err != nil {
		t.Fatalf("positive control: %v", err)
	}
	l.Close()
}

func TestListenRefusesARegularFileAtTheSocketPath(t *testing.T) {
	_, sock := dataDir(t)
	if err := os.Mkdir(filepath.Dir(sock), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sock, []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(sock, nil); !errors.Is(err, updaterwire.ErrNotSocket) {
		t.Fatalf("a regular file at the socket path must be refused with ErrNotSocket, got %v", err)
	}
	if b, _ := os.ReadFile(sock); string(b) != "not a socket" {
		t.Fatal("the refused regular file was modified or removed")
	}
	os.Remove(sock)
	l, err := Listen(sock, nil)
	if err != nil {
		t.Fatalf("positive control: %v", err)
	}
	l.Close()
}

func TestListenRefusesALooseOrSymlinkedDir(t *testing.T) {
	_, sock := dataDir(t)
	dir := filepath.Dir(sock)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(sock, nil); !errors.Is(err, updaterwire.ErrLoosePerms) {
		t.Fatalf("a 0755 updater dir must be refused with ErrLoosePerms, got %v", err)
	}
	if _, err := os.Lstat(sock); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("no socket may be created in a loose dir")
	}
	// a symlinked updater dir (pointing at a private dir elsewhere)
	other := filepath.Join(filepath.Dir(dir), "elsewhere")
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	os.Remove(dir)
	if err := os.Symlink(other, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(sock, nil); !errors.Is(err, updaterwire.ErrSymlink) {
		t.Fatalf("a symlinked updater dir must be refused with ErrSymlink, got %v", err)
	}
	// positive control: a real 0700 dir
	os.Remove(dir)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := Listen(sock, nil)
	if err != nil {
		t.Fatalf("positive control: %v", err)
	}
	l.Close()
}

func TestListenRefusesADirOfAnotherUID(t *testing.T) {
	_, sock := dataDir(t)
	orig := geteuid
	t.Cleanup(func() { geteuid = orig })
	geteuid = func() int { return orig() + 1 }
	if _, err := Listen(sock, nil); !errors.Is(err, updaterwire.ErrForeignOwner) {
		t.Fatalf("an updater dir not owned by our uid must be refused, got %v", err)
	}
	geteuid = orig
	l, err := Listen(sock, nil)
	if err != nil {
		t.Fatalf("positive control: %v", err)
	}
	l.Close()
}

// staleSocket leaves a bound-then-closed socket file at sock (a crashed
// worker's leftover): the file stays, nothing answers.
func staleSocket(t *testing.T, sock string) {
	t.Helper()
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ln.SetUnlinkOnClose(false)
	ln.Close()
	if fi, err := os.Lstat(sock); err != nil || fi.Mode()&fs.ModeSocket == 0 {
		t.Fatal("stale socket fixture missing")
	}
}

func TestListenReplacesOnlyAStaleSocketOfOurs(t *testing.T) {
	_, sock := dataDir(t)
	if err := os.Mkdir(filepath.Dir(sock), 0o700); err != nil {
		t.Fatal(err)
	}
	staleSocket(t, sock)
	// a stale socket owned by another uid is refused and left in place
	orig := fileUID
	t.Cleanup(func() { fileUID = orig })
	fileUID = func(fi fs.FileInfo) (uint32, bool) { u, ok := orig(fi); return u + 1, ok }
	if _, err := Listen(sock, nil); !errors.Is(err, updaterwire.ErrForeignOwner) {
		t.Fatalf("another uid's socket at the path must be refused, got %v", err)
	}
	if _, err := os.Lstat(sock); err != nil {
		t.Fatal("another uid's socket must never be unlinked")
	}
	// positive control: our own stale socket is replaced
	fileUID = orig
	s := serve(t, sock)
	c, err := updaterwire.Dial(sock)
	if err != nil {
		t.Fatalf("positive control: dial after replacing our stale socket: %v", err)
	}
	c.Close()
	_ = s
}

func TestListenRefusesALiveSocketAndASecondWorker(t *testing.T) {
	_, sock := dataDir(t)
	if err := os.Mkdir(filepath.Dir(sock), 0o700); err != nil {
		t.Fatal(err)
	}
	// someone is live at the path without holding the worker lock
	squat, err := net.ListenUnix("unix", &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(sock, nil); !errors.Is(err, ErrInUse) {
		t.Fatalf("a live listener at the path must be refused with ErrInUse, got %v", err)
	}
	if _, err := os.Lstat(sock); err != nil {
		t.Fatal("a live socket must never be unlinked")
	}
	squat.Close()
	// positive control: once it is gone, Listen succeeds…
	s := serve(t, sock)
	// …and a second worker is refused by the lock while the first still serves
	if _, err := Listen(sock, nil); !errors.Is(err, ErrInUse) {
		t.Fatalf("a second worker must be refused with ErrInUse, got %v", err)
	}
	c, err := updaterwire.Dial(sock)
	if err != nil {
		t.Fatalf("the first worker must still serve: %v", err)
	}
	defer c.Close()
	if resp, err := c.Do(updaterwire.NewStatus("")); err != nil || !resp.OK {
		t.Fatalf("first worker status: %+v %v", resp, err)
	}
	_ = s
}

// The worker lock is its own guard, not a restatement of the live-socket
// probe: a second worker that is still STARTING (holds the lock, has not
// bound yet) leaves nothing at the path for a probe to find. Without the
// lock, both would clear the path and bind, and the first would be left
// serving an unlinked socket nobody can reach.
func TestListenRefusesWhileAnotherWorkerHoldsTheLock(t *testing.T) {
	_, sock := dataDir(t)
	dir := filepath.Dir(sock)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// another worker mid-start: lock held, no socket yet (flock is per open
	// file description, so this conflicts with Listen's own open)
	other, err := os.OpenFile(filepath.Join(dir, lockFileName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := syscall.Flock(int(other.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if l, err := Listen(sock, nil); !errors.Is(err, ErrInUse) {
		if err == nil {
			l.Close()
		}
		t.Fatalf("Listen while another worker holds the lock = %v, want ErrInUse", err)
	}
	if _, err := os.Lstat(sock); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("a refused Listen must not bind the socket")
	}
	// positive control: the other worker lets go ⇒ Listen succeeds
	if err := syscall.Flock(int(other.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	l, err := Listen(sock, nil)
	if err != nil {
		t.Fatalf("positive control after the lock is released: %v", err)
	}
	l.Close()
}

func TestListenRefusesNonCanonicalPaths(t *testing.T) {
	d, sock := dataDir(t)
	for _, p := range []string{
		"data/updater/" + updaterwire.SocketFileName,                     // relative
		filepath.Dir(sock) + "/../updater/" + updaterwire.SocketFileName, // unclean
		filepath.Join(d, updaterwire.UpdaterDirName, "other.sock"),       // wrong name
		filepath.Join(d, "elsewhere", updaterwire.SocketFileName),        // wrong dir
	} {
		if l, err := Listen(p, nil); err == nil {
			l.Close()
			t.Fatalf("Listen(%q) must refuse a non-canonical path", p)
		}
	}
	// the data dir must already exist — the worker never invents an install
	if _, err := Listen(filepath.Join(d+"-missing", updaterwire.UpdaterDirName, updaterwire.SocketFileName), nil); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a missing data dir must be refused, got %v", err)
	}
	if _, err := os.Stat(d + "-missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("Listen created a missing data dir")
	}
	l, err := Listen(sock, nil)
	if err != nil {
		t.Fatalf("positive control: %v", err)
	}
	l.Close()
	if _, err := os.Lstat(sock); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("Close must unlink the socket")
	}
}

func TestServeClosesAnIdleConnection(t *testing.T) {
	orig := idleTimeout
	t.Cleanup(func() { idleTimeout = orig })
	idleTimeout = 150 * time.Millisecond
	_, sock := dataDir(t)
	s := serve(t, sock)
	c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	c.Write([]byte(`{"v":1,"verb":`)) // half a frame, then silence
	out, _ := io.ReadAll(c)
	rejected, _ := updaterwire.EncodeResponse(updaterwire.RejectedResponse)
	if !bytes.Equal(out, rejected) {
		t.Fatalf("an idle half-frame must be rejected and closed, got %q", out)
	}
	if !strings.Contains(s.logs.all(), "reason=timeout") {
		t.Fatalf("log = %q, want reason=timeout", s.logs.all())
	}
	// positive control: the same half frame, completed inside the idle
	// window, is served — the timeout, not the split write, refused it
	pc, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	pc.SetDeadline(time.Now().Add(5 * time.Second))
	pc.Write([]byte(`{"v":1,"verb":`))
	pc.Write([]byte(`"status","payload":{}}` + "\n"))
	line, err := bufio.NewReader(pc).ReadBytes('\n')
	ok, _ := updaterwire.EncodeResponse(updaterwire.Response{OK: true, State: "idle"})
	if err != nil || !bytes.Equal(line, ok) {
		t.Fatalf("positive control: a split frame completed in time must be served, got %q %v", line, err)
	}
	if s.calls.Load() != 1 {
		t.Fatalf("handler calls = %d, want 1 (the positive control only)", s.calls.Load())
	}
}

// A peer that streams a line with no newline is cut off at the cap: the
// server answers the rejected frame after reading ~16 KiB, never buffers the
// whole stream. The stream is 1 MiB and half-closed, so a regressed cap
// reads it all and fails as reason=truncated instead of hanging.
func TestServeCutsOffAnEndlessLine(t *testing.T) {
	_, sock := dataDir(t)
	s := serve(t, sock)
	c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	go func() {
		c.Write(bytes.Repeat([]byte("a"), 1<<20))
		c.CloseWrite()
	}()
	out, _ := io.ReadAll(c)
	rejected, _ := updaterwire.EncodeResponse(updaterwire.RejectedResponse)
	if !bytes.Equal(out, rejected) {
		t.Fatalf("an endless line must get exactly the rejected frame, got %q", out)
	}
	if !strings.Contains(s.logs.all(), "updater-wire: rejected reason=oversize len=") {
		t.Fatalf("log = %q, want reason=oversize with a length+hash", s.logs.all())
	}
	if s.calls.Load() != 0 {
		t.Fatal("the handler was called for an endless line")
	}
	// positive control: a valid frame on a fresh connection is served
	cl, err := updaterwire.Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	if resp, err := cl.Do(updaterwire.NewStatus("")); err != nil || !resp.OK {
		t.Fatalf("positive control: %+v %v", resp, err)
	}
}

// Connections past the cap are closed unread and logged; the cap frees as
// connections end (positive control).
func TestServeCapsConcurrentConnections(t *testing.T) {
	orig := maxConns
	t.Cleanup(func() { maxConns = orig })
	maxConns = 1
	_, sock := dataDir(t)
	s := serve(t, sock)
	first, err := updaterwire.Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	if resp, err := first.Do(updaterwire.NewStatus("")); err != nil || !resp.OK {
		t.Fatalf("first connection: %+v %v", resp, err)
	}
	// the slot is held: a second connection is closed before any read
	if out := rawExchange(t, sock, []byte(validStatus+"\n")); len(out) != 0 {
		t.Fatalf("a connection past the cap must be closed unread, got %q", out)
	}
	if !strings.Contains(s.logs.all(), "updater-wire: rejected reason=busy") {
		t.Fatalf("log = %q, want reason=busy", s.logs.all())
	}
	if n := s.calls.Load(); n != 1 {
		t.Fatalf("handler calls = %d, want 1", n)
	}
	first.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c, err := updaterwire.Dial(sock)
		if err == nil {
			resp, derr := c.Do(updaterwire.NewStatus(""))
			c.Close()
			if derr == nil && resp.OK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("positive control: the slot never freed after the first connection closed")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Close ends live connections: Serve returns promptly even with an idle
// peer attached, and the shutdown is not logged as a refusal.
func TestCloseEndsIdleConnectionsPromptly(t *testing.T) {
	_, sock := dataDir(t)
	sink := &logSink{}
	l, err := Listen(sock, sink.logf)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- l.Serve(func(updaterwire.Request) updaterwire.Response {
			return updaterwire.Response{OK: true, State: "idle"}
		})
	}()
	c, err := updaterwire.Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if resp, err := c.Do(updaterwire.NewStatus("")); err != nil || !resp.OK {
		t.Fatalf("status: %+v %v", resp, err)
	}
	start := time.Now()
	l.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve after Close = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return within 2s of Close with an idle peer attached (idle timeout is 30s)")
	}
	if el := time.Since(start); el > time.Second {
		t.Fatalf("Serve took %v to return after Close", el)
	}
	if strings.Contains(sink.all(), "rejected") {
		t.Fatalf("a shutdown must not be logged as a refusal: %q", sink.all())
	}
	if _, err := c.Do(updaterwire.NewStatus("")); err == nil {
		t.Fatal("the idle connection must be closed by Close")
	}
}

// DialWorker resolves the socket from the data dir exactly as the worker's
// Listen path is built, and refuses a relative data dir.
func TestDialWorkerResolvesTheDataDir(t *testing.T) {
	d, sock := dataDir(t)
	serve(t, sock)
	c, err := updaterwire.DialWorker(d)
	if err != nil {
		t.Fatalf("positive control: DialWorker(%q): %v", d, err)
	}
	defer c.Close()
	if resp, err := c.Do(updaterwire.NewStatus("")); err != nil || !resp.OK {
		t.Fatalf("status: %+v %v", resp, err)
	}
	for _, bad := range []string{"", "data", "./data"} {
		if _, err := updaterwire.DialWorker(bad); !errors.Is(err, updaterwire.ErrBadDataDir) {
			t.Fatalf("DialWorker(%q) = %v, want ErrBadDataDir", bad, err)
		}
	}
}
