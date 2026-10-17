package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"vl/internal/installpath"
	"vl/internal/updaterjob"
	"vl/internal/updaterwire"
	"vl/internal/updaterwire/wireserver"
	"vl/internal/updaterworker"
	"vl/internal/updaterworker/releasefixture"
)

const testJob = "job-u4-cli0abcd"

// errHalfBuilt is a factory's error returned WITH a non-nil adapter (U4F defect 4).
var errHalfBuilt = errors.New("adapter half-built")

// install makes a short temp installation (the socket path must fit sun_path)
// with a bot database, and returns its dir and data dir.
func install(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("DB_PATH", "x")
	os.Unsetenv("DB_PATH") // the operator's shell exports none
	root, err := os.MkdirTemp("", "u4c-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data", "data.db"), []byte("SQLite format 3\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	// D8 (verifier): the temp install names its OWN API port — a closed
	// loopback port — so the production HTTPApp can never target the live
	// bot's :8080, even on a path that one day sends a request.
	port := closedPort(t)
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(fmt.Sprintf("API_SERVER_PORT=%d\n", port)), 0o600); err != nil {
		t.Fatal(err)
	}
	if tg, err := updaterworker.ResolveTarget(root); err != nil || tg.Port != port || tg.Port == 8080 {
		t.Fatalf("temp install target = %+v, %v; want port %d (never 8080)", tg, err, port)
	}
	return root, filepath.Join(root, "data")
}

// closedPort is a loopback port nothing listens on (bound, then released).
func closedPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func runCLI(t *testing.T, stdin io.Reader, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	if stdin == nil {
		stdin = strings.NewReader("")
	}
	rc := run(args, stdin, &out, &errb)
	return rc, out.String(), errb.String()
}

// productionNewLibrary / productionNewReverifier are the adapters main.go
// wires, captured at package init before any test swaps a seam — the tests
// below restore THESE, never a stand-in.
var (
	productionNewLibrary    = newLibrary
	productionNewReverifier = newReverifier
)

// L4: serve refuses and writes NOTHING — no updater dir, no socket, no job,
// no hold — unless BOTH production adapters are wired. Both now are: the
// release re-proof (U4N item A: updaterworker.NewReleaseReverifier) and the
// activation library (updaterworker.NewActivationLibrary, one-line delegations
// to vl/internal/activation, possible since 103's D4 fix made activation
// register the ONE sqlite driver store/sqlitedriver owns). So the production
// wiring passes the adapter precondition and serve proceeds to its NEXT
// precondition (a home for ~/vl-backups/updater — unset here, so it still
// refuses and still writes nothing). Every row with a missing or erroring
// adapter keeps refusing, naming which adapter is missing.
func TestServeRefusesUnlessBothAdaptersAreWired(t *testing.T) {
	inst, data := install(t)
	t.Setenv(updaterworker.CutoverTokenEnv, "tok-cli-never-printed-51c2")
	t.Setenv("HOME", "")                       // serve's next precondition after the adapters: refuses, writes nothing
	checkProcess = func() error { return nil } // not root, not the bot's cgroup, no TZ
	t.Cleanup(func() { checkProcess = updaterworker.CheckProcess })
	tg, err := updaterworker.ResolveTarget(inst)
	if err != nil {
		t.Fatal(err)
	}
	// the PRODUCTION re-proof adapter is wired: it answers from the verdict
	// files (none here: the updaterjob reader's not-exist), never "not wired"
	rel, err := newReverifier(tg)
	if err != nil || rel == nil {
		t.Fatalf("newReverifier = %v, %v; want the production re-proof adapter", rel, err)
	}
	if _, err := rel.Verdict("v1.2.0"); !errors.Is(err, updaterjob.ErrVerdict) || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the wired re-proof's Verdict of an unfetched release = %v, want updaterjob.ErrVerdict wrapping fs.ErrNotExist", err)
	}
	// the PRODUCTION library adapter is wired: the activation delegations
	if lib, err := newLibrary(); err != nil || lib == nil || fmt.Sprintf("%T", lib) != "updaterworker.activationLibrary" {
		t.Fatalf("newLibrary = %T, %v; want the activation library adapter (updaterworker.activationLibrary), wired", lib, err)
	}
	defer func() { newLibrary, newReverifier = productionNewLibrary, productionNewReverifier }()
	notWired := func() (updaterworker.Library, error) { return nil, updaterworker.ErrNotWired }
	// a serve that WRONGLY starts must end at once and touch no real home:
	// its context is already cancelled (HOME stays "" from above)
	done, cancel := context.WithCancel(context.Background())
	cancel()
	serveContext = func() (context.Context, context.CancelFunc) { return done, cancel }
	t.Cleanup(func() {
		serveContext = func() (context.Context, context.CancelFunc) {
			return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		}
	})
	for _, c := range []struct {
		name    string
		lib     func() (updaterworker.Library, error)
		rel     func(updaterworker.Target) (updaterworker.Reverifier, error)
		refused bool   // refused at the adapter precondition
		want    string // the refusal line names this
	}{
		// the production wiring: both adapters wired → past the adapters, to the next precondition
		{"production: both adapters wired", productionNewLibrary, productionNewReverifier, false,
			"vl-updater serve: no home directory for ~/vl-backups/updater"},
		{"library errors, re-proof wired", notWired, productionNewReverifier, true,
			"activation library adapter: missing (updaterworker: not wired yet) · release re-proof adapter: wired"},
		{"library wired, re-proof missing", productionNewLibrary,
			func(updaterworker.Target) (updaterworker.Reverifier, error) { return nil, updaterworker.ErrNotWired }, true,
			"activation library adapter: wired · release re-proof adapter: missing (updaterworker: not wired yet)"},
		{"both missing", notWired,
			func(updaterworker.Target) (updaterworker.Reverifier, error) { return nil, updaterworker.ErrNotWired }, true,
			"activation library adapter: missing (updaterworker: not wired yet) · release re-proof adapter: missing"},
		{"library nil without an error", func() (updaterworker.Library, error) { return nil, nil }, productionNewReverifier, true,
			"activation library adapter: missing (nil) · release re-proof adapter: wired"},
		// U4F defect 4: a factory that hands back an adapter AND an error is a
		// half-built adapter — the error alone refuses, whatever came with it
		{"library returns an adapter AND an error", func() (updaterworker.Library, error) { return testLib{}, errHalfBuilt },
			updaterworker.NewReleaseReverifier, true,
			"activation library adapter: missing (" + errHalfBuilt.Error() + ") · release re-proof adapter: wired"},
		{"re-proof returns an adapter AND an error", func() (updaterworker.Library, error) { return testLib{}, nil },
			func(updaterworker.Target) (updaterworker.Reverifier, error) { return testRel{}, errHalfBuilt }, true,
			"activation library adapter: wired · release re-proof adapter: missing (" + errHalfBuilt.Error() + ")"},
		{"re-proof nil without an error", productionNewLibrary,
			func(updaterworker.Target) (updaterworker.Reverifier, error) { return nil, nil }, true,
			"activation library adapter: wired · release re-proof adapter: missing (nil)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			newLibrary, newReverifier = c.lib, c.rel
			rc, out, errs := runCLI(t, nil, "--install-dir", inst, "serve")
			if rc != 2 || out != "" || !strings.Contains(errs, c.want) || strings.Contains(errs, "adapter: missing") != c.refused {
				t.Fatalf("serve = %d %q %q; want rc 2 naming %q (refused at the adapters: %v)", rc, out, errs, c.want, c.refused)
			}
			if strings.Contains(errs, "tok-cli-never-printed") {
				t.Fatal("serve printed the cutover token")
			}
			if _, err := os.Stat(filepath.Join(data, "updater")); !os.IsNotExist(err) {
				t.Fatalf("a refused serve created %s (%v)", filepath.Join(data, "updater"), err)
			}
		})
	}
	newLibrary, newReverifier = productionNewLibrary, productionNewReverifier
	// fetch is wired (U4N item B) but, with no inbox configured, refuses
	// before it reads or writes anything (TestFetchRefusesWithoutItsInputs)
	t.Setenv(releaseInboxEnv, "")
	rc, _, errs := runCLI(t, nil, "--install-dir", inst, "fetch", "v1.2.0")
	if rc != 2 || !strings.Contains(errs, releaseInboxEnv+" is not set") {
		t.Fatalf("fetch = %d %q", rc, errs)
	}
	if _, err := os.Stat(filepath.Join(data, "updater")); !os.IsNotExist(err) {
		t.Fatalf("a refused serve/fetch created %s (%v)", filepath.Join(data, "updater"), err)
	}
	// and serve without the token refuses before anything else
	os.Unsetenv(updaterworker.CutoverTokenEnv)
	if rc, _, errs := runCLI(t, nil, "--install-dir", inst, "serve"); rc != 2 || !strings.Contains(errs, updaterworker.CutoverTokenEnv+" is not set") {
		t.Fatalf("serve without a token = %d %q", rc, errs)
	}
}

// PIN (the worker never runs as root): every subcommand refuses root before
// it resolves anything; serve also runs the worker's process refusals (root,
// the bot's cgroup, TZ) at its call site.
func TestEverySubcommandRefusesRoot(t *testing.T) {
	inst, _ := install(t)
	geteuid = func() int { return 0 }
	t.Cleanup(func() { geteuid = os.Geteuid })
	for _, args := range [][]string{{"serve"}, {"fetch", "v1.2.0"}, {"status"}, {"status", testJob}, {"resume", testJob}, {"recovery", testJob}} {
		rc, _, errs := runCLI(t, nil, append([]string{"--install-dir", inst}, args...)...)
		if rc != 2 || !strings.Contains(errs, "refusing to run as root") {
			t.Fatalf("%v as root = %d %q", args, rc, errs)
		}
	}
	geteuid = os.Geteuid
	checkProcess = func() error { return updaterworker.ErrBotCgroup }
	t.Cleanup(func() { checkProcess = updaterworker.CheckProcess })
	t.Setenv(updaterworker.CutoverTokenEnv, "tok")
	if rc, _, errs := runCLI(t, nil, "--install-dir", inst, "serve"); rc != 2 || !strings.Contains(errs, "bot's service control group") {
		t.Fatalf("serve inside the bot's cgroup = %d %q", rc, errs)
	}
}

// resume is attended: a terminal and the job id typed back, then exactly one
// resume frame for that job over the worker socket. A mistyped id, or no
// terminal, sends nothing.
func TestResumeSendsOneFrameOnlyAfterTheTypedJobID(t *testing.T) {
	inst, data := install(t)
	j, err := updaterjob.New(testJob, "v1.2.0", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := updaterjob.Write(data, j); err != nil {
		t.Fatal(err)
	}
	path, err := updaterwire.SocketPath(data)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := wireserver.Listen(path, func(string, ...any) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var mu sync.Mutex
	var got []updaterwire.Request
	go ln.Serve(func(r updaterwire.Request) updaterwire.Response {
		mu.Lock()
		got = append(got, r)
		mu.Unlock()
		return updaterwire.Response{OK: true, State: "resuming"}
	})
	isTerminal = func(io.Reader) bool { return true }
	t.Cleanup(func() { isTerminal = stdinIsTerminal })

	if rc, _, errs := runCLI(t, strings.NewReader("job-u4-wrong000\n"), "--install-dir", inst, "resume", testJob); rc != 2 || !strings.Contains(errs, "does not match") {
		t.Fatalf("mistyped resume = %d %q", rc, errs)
	}
	isTerminal = func(io.Reader) bool { return false }
	if rc, _, errs := runCLI(t, strings.NewReader(testJob+"\n"), "--install-dir", inst, "resume", testJob); rc != 2 || !strings.Contains(errs, "from a terminal") {
		t.Fatalf("resume without a terminal = %d %q", rc, errs)
	}
	mu.Lock()
	if len(got) != 0 {
		t.Fatalf("a refused resume sent %v", got)
	}
	mu.Unlock()
	isTerminal = func(io.Reader) bool { return true }
	rc, out, errs := runCLI(t, strings.NewReader(testJob+"\n"), "--install-dir", inst, "resume", testJob)
	if rc != 0 || !strings.HasSuffix(out, "resuming\n") {
		t.Fatalf("resume = %d %q %q", rc, out, errs)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0].Verb != updaterwire.VerbResume || got[0].Resume == nil || got[0].Resume.JobID != testJob {
		t.Fatalf("the worker received %+v, want exactly one resume for %s", got, testJob)
	}
}

// status <job> and recovery <job> read the job file (no worker needed);
// absent values print n/a.
func TestStatusAndRecoveryReadTheJobFile(t *testing.T) {
	inst, data := install(t)
	j, _ := updaterjob.New(testJob, "v1.2.0", time.Now())
	if err := updaterjob.Write(data, j); err != nil {
		t.Fatal(err)
	}
	rc, out, errs := runCLI(t, nil, "--install-dir", inst, "status", testJob)
	if rc != 0 || !strings.Contains(out, `"state": "requested"`) || !strings.Contains(out, `"blocker": "n/a"`) {
		t.Fatalf("status job = %d %q %q", rc, out, errs)
	}
	rc, out, _ = runCLI(t, nil, "--install-dir", inst, "recovery", testJob)
	if rc != 0 || !strings.Contains(out, "not recovery_needed; nothing to do") {
		t.Fatalf("recovery of a live job = %d %q", rc, out)
	}
	if rc, _, errs := runCLI(t, nil, "--install-dir", inst, "status"); rc != 1 || !strings.Contains(errs, "no worker answers") {
		t.Fatalf("status with no worker = %d %q", rc, errs)
	}
	if rc, _, errs := runCLI(t, nil, "--install-dir", inst, "status", "../../etc/passwd"); rc != 1 || !strings.Contains(errs, "invalid job id") {
		t.Fatalf("status of a forged id = %d %q", rc, errs)
	}
}

// testLib / testRel stand in for the two adapters the fold lands; every
// side effect refuses (serve here only proves its wiring).
type testLib struct{}

func (testLib) Resolve(string) (updaterworker.Release, error) { return updaterworker.Release{}, errNo }
func (testLib) Stage(updaterworker.Release) (updaterworker.Receipt, error) {
	return updaterworker.Receipt{}, errNo
}
func (testLib) Backup(string, string) (updaterworker.Receipt, error) {
	return updaterworker.Receipt{}, errNo
}
func (testLib) Snapshot(updaterworker.Release, string) (updaterworker.Receipt, error) {
	return updaterworker.Receipt{}, errNo
}
func (testLib) Activate(updaterworker.Release, updaterworker.Release, updaterworker.Identity) (updaterworker.Identity, updaterworker.Receipt, error) {
	return updaterworker.Identity{}, updaterworker.Receipt{}, errNo
}
func (testLib) Watch(updaterworker.Release, updaterworker.Identity, updaterworker.WatchOpts) (updaterworker.Receipt, error) {
	return updaterworker.Receipt{}, errNo
}
func (testLib) RollbackTo(updaterworker.Release, updaterworker.Release, updaterworker.Identity) (updaterworker.Identity, updaterworker.Receipt, error) {
	return updaterworker.Identity{}, updaterworker.Receipt{}, errNo
}
func (testLib) CurrentIdentity() (updaterworker.Identity, error) {
	return updaterworker.Identity{}, errNo
}

type testRel struct{}

func (testRel) Verdict(string) (updaterworker.Verdict, error) { return updaterworker.Verdict{}, errNo }
func (testRel) Rehash(updaterworker.Verdict) (int, error)     { return 0, errNo }
func (testRel) Reverify(updaterworker.Verdict) (updaterworker.ReleaseFacts, error) {
	return updaterworker.ReleaseFacts{}, errNo
}

var errNo = errors.New("test adapter: refused")

// serve, once its adapters exist, starts the worker (sweep), listens on the
// installation's socket, prints its start line READ (n/a when absent),
// answers the verbs, refuses an install whose release has no verdict, and
// ends cleanly on the signal — removing the socket.
func TestServeWiresTheWorkerBehindTheSocket(t *testing.T) {
	inst, data := install(t)
	t.Setenv(updaterworker.CutoverTokenEnv, "tok-serve-never-printed-9e1f")
	t.Setenv("HOME", t.TempDir())
	checkProcess = func() error { return nil }
	newLibrary = func() (updaterworker.Library, error) { return testLib{}, nil }
	newReverifier = func(updaterworker.Target) (updaterworker.Reverifier, error) { return testRel{}, nil }
	ctx, cancel := context.WithCancel(context.Background())
	serveContext = func() (context.Context, context.CancelFunc) { return ctx, cancel }
	t.Cleanup(func() {
		checkProcess = updaterworker.CheckProcess
		newLibrary, newReverifier = productionNewLibrary, productionNewReverifier
		serveContext = func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }
	})
	var errb syncBuffer
	rc := make(chan int, 1)
	go func() { rc <- run([]string{"--install-dir", inst, "serve"}, strings.NewReader(""), io.Discard, &errb) }()
	var c *updaterwire.Client
	for deadline := time.Now().Add(10 * time.Second); c == nil; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("serve never listened: %s", errb.String())
		}
		c, _ = updaterwire.DialWorker(data)
	}
	defer c.Close()
	if resp, err := c.Do(updaterwire.NewStatus("")); err != nil || resp.State != "idle" {
		t.Fatalf("status = %+v, %v", resp, err)
	}
	if resp, err := c.Do(updaterwire.NewInstall("v1.2.0", testJob)); err != nil || resp.Error != "release not verified" {
		t.Fatalf("install without a verdict = %+v, %v", resp, err)
	}
	cancel()
	if got := <-rc; got != 0 {
		t.Fatalf("serve ended %d: %s", got, errb.String())
	}
	line := errb.String()
	if !strings.Contains(line, "🔧 vl-updater: serving ") || !strings.Contains(line, "active=n/a · recovery_needed=n/a · stale_at_start=n/a") {
		t.Fatalf("start line: %q", line)
	}
	if strings.Contains(line, "tok-serve-never-printed") {
		t.Fatal("serve printed the token")
	}
	if p, _ := updaterwire.SocketPath(data); fileExists(p) {
		t.Fatal("the socket survives serve")
	}
}

// PIN (U4F, the containment class applied to the backup root): serve refuses
// — and writes nothing — when ~/vl-backups/updater is inside the install,
// by path elements (<install>/..h is inside) and after resolving symlinks
// (a HOME named through a symlink to the install is inside too). A snapshot
// that lives inside the install it restores dies with it.
func TestServeRefusesABackupRootInsideTheInstall(t *testing.T) {
	inst, data := install(t)
	t.Setenv(updaterworker.CutoverTokenEnv, "tok-backup-never-printed-3c7d")
	checkProcess = func() error { return nil }
	newLibrary = func() (updaterworker.Library, error) { return testLib{}, nil }
	newReverifier = func(updaterworker.Target) (updaterworker.Reverifier, error) { return testRel{}, nil }
	done, cancel := context.WithCancel(context.Background())
	cancel() // a serve that WRONGLY starts ends at once
	serveContext = func() (context.Context, context.CancelFunc) { return done, cancel }
	t.Cleanup(func() {
		checkProcess = updaterworker.CheckProcess
		newLibrary = func() (updaterworker.Library, error) { return nil, updaterworker.ErrNotWired }
		newReverifier = updaterworker.NewReleaseReverifier
		serveContext = func() (context.Context, context.CancelFunc) {
			return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		}
	})
	link := filepath.Join(t.TempDir(), "instlink")
	if err := os.Symlink(inst, link); err != nil {
		t.Fatal(err)
	}
	for name, home := range map[string]string{
		"HOME inside the install":                     filepath.Join(inst, "h"),
		"HOME named ..h inside the install":           filepath.Join(inst, "..h"),
		"HOME through a symlink to the install":       filepath.Join(link, "h"),
		"HOME through a symlink, not yet created too": filepath.Join(link, "h", "not", "yet"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", home)
			rc, out, errs := runCLI(t, nil, "--install-dir", inst, "serve")
			if rc != 2 || !strings.Contains(errs, "is inside the install") || out != "" {
				t.Fatalf("serve with HOME=%s = %d %q %q; want refused naming the backup root inside the install", home, rc, out, errs)
			}
			if _, err := os.Stat(filepath.Join(data, "updater")); !os.IsNotExist(err) {
				t.Fatalf("a refused serve created %s (%v)", filepath.Join(data, "updater"), err)
			}
		})
	}
	// U4F verify note 4 (probe B): a DANGLING symlink among the not-yet-created
	// elements is not plain text to append — it would pass as "outside" while
	// pointing into the install — so every unresolved element is Lstat'ed and
	// a symlink there refuses (dangling into the install, dangling anywhere,
	// a loop, and one deeper under the not-yet-created rest).
	tmp := t.TempDir()
	for name, mk := range map[string]func() string{
		"HOME a dangling symlink to <install>/h": func() string {
			l := filepath.Join(tmp, "dangle")
			if err := os.Symlink(filepath.Join(inst, "h"), l); err != nil {
				t.Fatal(err)
			}
			return l
		},
		"HOME a dangling symlink to nowhere": func() string {
			l := filepath.Join(tmp, "nowhere")
			if err := os.Symlink(filepath.Join(tmp, "no", "such", "dir"), l); err != nil {
				t.Fatal(err)
			}
			return l
		},
		"HOME a symlink loop": func() string {
			l := filepath.Join(tmp, "loop")
			if err := os.Symlink(l, l); err != nil {
				t.Fatal(err)
			}
			return l
		},
		"HOME below a dangling symlink to <install>/h": func() string {
			l := filepath.Join(tmp, "dangle2")
			if err := os.Symlink(filepath.Join(inst, "h2"), l); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(l, "deeper")
		},
	} {
		t.Run(name, func(t *testing.T) {
			home := mk()
			t.Setenv("HOME", home)
			rc, out, errs := runCLI(t, nil, "--install-dir", inst, "serve")
			if rc != 2 || !strings.Contains(errs, "cannot be checked against the install") || out != "" {
				t.Fatalf("serve with HOME=%s = %d %q %q; want refused: the backup root cannot be checked against the install", home, rc, out, errs)
			}
			if _, err := os.Stat(filepath.Join(data, "updater")); !os.IsNotExist(err) {
				t.Fatalf("a refused serve created %s (%v)", filepath.Join(data, "updater"), err)
			}
		})
	}
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func fileExists(p string) bool { _, err := os.Lstat(p); return err == nil }

// PIN (verifier D1): a second serve that cannot take the worker lock writes
// NOTHING to the running worker's job — the lock is taken BEFORE the start
// sweep and before the runner exists. Two cases: a job the sweep would mark
// stale (a synchronous write) and a fresh job the runner would advance (the
// test's serve context is never cancelled by serve, so a runner started by
// the refused serve would keep going and be seen).
func TestASecondServeWithoutTheWorkerLockWritesNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		age  time.Duration
	}{{"stale", 31 * time.Minute}, {"fresh", 0}} {
		t.Run(tc.name, func(t *testing.T) {
			inst, data := install(t)
			t.Setenv(updaterworker.CutoverTokenEnv, "tok-second-serve-never-printed-3d")
			t.Setenv("HOME", t.TempDir())
			checkProcess = func() error { return nil }
			newLibrary = func() (updaterworker.Library, error) { return testLib{}, nil }
			newReverifier = func(updaterworker.Target) (updaterworker.Reverifier, error) { return testRel{}, nil }
			ctx, cancel := context.WithCancel(context.Background())
			serveContext = func() (context.Context, context.CancelFunc) { return ctx, func() {} }
			t.Cleanup(func() {
				cancel()
				checkProcess = updaterworker.CheckProcess
				newLibrary, newReverifier = productionNewLibrary, productionNewReverifier
				serveContext = func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }
			})
			j, err := updaterjob.New(testJob, "v1.2.0", time.Now().Add(-tc.age))
			if err != nil {
				t.Fatal(err)
			}
			if err := updaterjob.Write(data, j); err != nil {
				t.Fatal(err)
			}
			jobFile, err := updaterjob.Path(data, testJob)
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(jobFile)
			if err != nil {
				t.Fatalf("job file: %v", err)
			}
			// the FIRST worker: holds the socket and its lock
			path, err := updaterwire.SocketPath(data)
			if err != nil {
				t.Fatal(err)
			}
			ln, err := wireserver.Listen(path, func(string, ...any) {})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { ln.Close() })
			rc, _, errs := runCLI(t, nil, "--install-dir", inst, "serve")
			if rc != 2 || !strings.Contains(errs, "already in use") {
				t.Fatalf("second serve = %d %q, want refused: the worker socket is in use", rc, errs)
			}
			for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
				after, err := os.ReadFile(jobFile)
				if err != nil || !bytes.Equal(after, before) {
					k, _ := updaterjob.Read(data, testJob)
					t.Fatalf("the refused second serve wrote the running worker's job: now %s/%s attempts %d (%v)", k.State, k.Phase, k.Attempts, err)
				}
			}
		})
	}
}

// fetchRig is a temp installation whose deploy/release_allowed_signers trusts
// r's signer, a local inbox holding r's archive as <release_id>.tar.gz
// (release.yml's asset name), and a release root OUTSIDE the install — the
// three environment knobs set the way the operator sets them.
type fetchRig struct {
	inst, data, inbox, root string
	r                       releasefixture.Release
	instArg                 string // --install-dir as the operator types it ("" = inst)
}

const fetchID = "v0.0.2-u4n"

var fetchSHA = strings.Repeat("d4e5f6a7", 5) // 40 lowercase hex

func newFetchRig(t *testing.T) fetchRig {
	t.Helper()
	r := releasefixture.Build(t, fetchID, fetchSHA, releasefixture.Opts{})
	inst, data := install(t)
	signers, err := os.ReadFile(r.Signers)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(inst, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(updaterworker.ReleaseAllowedSignersPath(inst), signers, 0o644); err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	f := fetchRig{inst: inst, data: data, inbox: filepath.Join(base, "inbox"), root: filepath.Join(base, "releases"), r: r}
	for _, d := range []string{f.inbox, f.root} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	arch, err := os.ReadFile(r.Archive)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.inbox, fetchID+".tar.gz"), arch, 0o644); err != nil {
		t.Fatal(err)
	}
	f.env(t, f.inbox, f.root)
	return f
}

// env sets NOFX_RELEASE_INBOX and NOFX_RELEASE_DIR ("" unsets) and resets the
// one release-dir resolver (a sync.Once in production).
func (f fetchRig) env(t *testing.T, inbox, root string) {
	t.Helper()
	t.Setenv(releaseInboxEnv, inbox)
	t.Setenv("NOFX_RELEASE_DIR", root)
	installpath.ResetReleaseDirForTest()
	t.Cleanup(installpath.ResetReleaseDirForTest)
}

// PIN (U4N item B): `vl-updater fetch <release_id>` — the production entry —
// verifies a REAL archive (3a's package.sh + manifest.sh, the real ssh-keygen
// signature, tar) from the local inbox against the INSTALL's allowed-signers,
// materializes it under NOFX_RELEASE_DIR/<source_sha>, writes the verdict
// into the installation's data dir, and prints the release id and source sha
// (never a key). The wired re-proof adapter then re-proves what it wrote.
func TestFetchVerifiesALocalReleaseEndToEnd(t *testing.T) {
	f := newFetchRig(t)
	rc, out, errs := runCLI(t, nil, "--install-dir", f.inst, "fetch", fetchID)
	if rc != 0 {
		t.Fatalf("fetch = %d %q %q", rc, out, errs)
	}
	if !strings.Contains(out, "release "+fetchID+" verified: source "+fetchSHA+" ·") {
		t.Fatalf("fetch printed %q; want the release id %s and source sha %s", out, fetchID, fetchSHA)
	}
	pub := strings.Fields(f.r.Signer.Pub)[1]
	if strings.Contains(out+errs, pub) || strings.Contains(out+errs, "ssh-ed25519") || strings.Contains(out+errs, "PRIVATE KEY") {
		t.Fatalf("fetch printed key material: %q %q", out, errs)
	}
	v, err := updaterjob.ReadVerdict(f.data, fetchID)
	if err != nil {
		t.Fatalf("no verdict in the installation's data dir: %v", err)
	}
	if v.SourceSHA != fetchSHA || v.ReleaseDir != filepath.Join(f.root, fetchSHA) || v.SignerFingerprint != f.r.FP {
		t.Fatalf("verdict = %+v; want source %s at %s signed by %s", v, fetchSHA, filepath.Join(f.root, fetchSHA), f.r.FP)
	}
	tg, err := updaterworker.ResolveTarget(f.inst)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := newReverifier(tg)
	if err != nil {
		t.Fatal(err)
	}
	mv, err := rel.Verdict(fetchID)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := rel.Rehash(mv); err != nil || n != v.Artifacts {
		t.Fatalf("re-proof Rehash = %d, %v", n, err)
	}
	if facts, err := rel.Reverify(mv); err != nil || facts.SourceSHA != fetchSHA || facts.AddonBuildID != releasefixture.BuildID {
		t.Fatalf("re-proof Reverify = %+v, %v", facts, err)
	}
	// written once: a second fetch of the same id refuses and changes nothing
	before, _ := os.ReadFile(filepath.Join(f.data, "updater", "verdicts", fetchID+".json"))
	if rc, _, errs := runCLI(t, nil, "--install-dir", f.inst, "fetch", fetchID); rc == 0 || !strings.Contains(errs, "already exists") {
		t.Fatalf("second fetch = %d %q", rc, errs)
	}
	if after, _ := os.ReadFile(filepath.Join(f.data, "updater", "verdicts", fetchID+".json")); !bytes.Equal(before, after) {
		t.Fatal("a refused second fetch rewrote the verdict")
	}
}

// PIN (U4F defect 6, probe P7; a fail-closed default named for the CTO): a
// release fetched — through the production entry — into root A is NOT
// re-provable once NOFX_RELEASE_DIR names root B: the wired re-proof's
// Verdict refuses (so the install verb, the verify step, the nt8 rule, the
// resume and the boot check all refuse), and with the knob back at A it
// re-proves again. The verdict's release_dir must be <the current resolved
// release root>/<source sha>.
func TestAVerdictIsReprovedOnlyUnderTheCurrentReleaseRoot(t *testing.T) {
	f := newFetchRig(t)
	if rc, out, errs := runCLI(t, nil, "--install-dir", f.inst, "fetch", fetchID); rc != 0 {
		t.Fatalf("fetch = %d %q %q", rc, out, errs)
	}
	tg, err := updaterworker.ResolveTarget(f.inst)
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "other-root")
	if err := os.Mkdir(other, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, root string
		ok         bool
	}{
		{"the root it was fetched into", f.root, true},
		{"another root (the operator moved NOFX_RELEASE_DIR)", other, false},
		{"unset", "", false},
		{"back at the root it was fetched into", f.root, true},
	} {
		f.env(t, f.inbox, c.root)
		rel, err := newReverifier(tg)
		if err != nil {
			t.Fatal(err)
		}
		v, err := rel.Verdict(fetchID)
		if c.ok {
			if err != nil || v.ReleaseDir != filepath.Join(f.root, fetchSHA) {
				t.Fatalf("%s: Verdict = %+v, %v; want the verdict at %s", c.name, v, err, filepath.Join(f.root, fetchSHA))
			}
			continue
		}
		if !errors.Is(err, updaterworker.ErrReleaseRoot) || v != (updaterworker.Verdict{}) {
			t.Fatalf("%s: Verdict = %+v, %v; want refused with ErrReleaseRoot and no verdict", c.name, v, err)
		}
	}
}

// PIN (U4G defect 1): when the moved-TO release directory ALREADY EXISTS (the
// operator moved the DIRECTORY, not just the knob), the old text's first step
// deleted the verdict and its second step then refused "release directory
// already exists" — the operator was left with no verdict and an unusable
// release. The text must name the move-aside case BEFORE the rm (or say the
// directory need not move at all).
func TestAMovedReleaseDirectoryRefusalOrdersMoveAsideBeforeTheRm(t *testing.T) {
	f := newFetchRig(t)
	if rc, out, errs := runCLI(t, nil, "--install-dir", f.inst, "fetch", fetchID); rc != 0 {
		t.Fatalf("fetch into A = %d %q %q", rc, out, errs)
	}
	tg, err := updaterworker.ResolveTarget(f.inst)
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	b := filepath.Join(base, "root-b")
	// MOVE the release root directory itself, so <want> = B/<sha> now exists.
	if out, err := exec.Command("mv", f.root, b).CombinedOutput(); err != nil {
		t.Fatalf("mv %s %s: %v %s", f.root, b, err, out)
	}
	f.env(t, f.inbox, b)
	rel, err := newReverifier(tg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = rel.Verdict(fetchID)
	if err == nil {
		t.Fatal("Verdict under the moved directory was accepted")
	}
	msg := err.Error()
	if !strings.Contains(msg, "ALREADY EXISTS") {
		t.Fatalf("the refusal must name the existing moved-to dir:\n%s", msg)
	}
	moveAside := strings.Index(msg, "move ")
	rmAt := strings.Index(msg, "rm ")
	if moveAside < 0 || rmAt < moveAside {
		t.Fatalf("the move-aside case must come BEFORE the rm (move at %d, rm at %d):\n%s", moveAside, rmAt, msg)
	}
}

// PIN (U4F verify note 2): the refusal after the operator moved
// NOFX_RELEASE_DIR names BOTH steps — remove the old verdict by hand (a
// re-fetch alone refuses: the verdict is written once), then re-fetch — as
// exact guidance, and FOLLOWING it works end to end through the production
// entry: fetch into root A, move the knob to root B, run the two printed
// commands as printed, and the wired re-proof's Verdict is ok under B.
func TestAMovedReleaseRootRefusalNamesBothStepsAndFollowingThemWorks(t *testing.T) {
	f := newFetchRig(t)
	if rc, out, errs := runCLI(t, nil, "--install-dir", f.inst, "fetch", fetchID); rc != 0 {
		t.Fatalf("fetch into A = %d %q %q", rc, out, errs)
	}
	tg, err := updaterworker.ResolveTarget(f.inst)
	if err != nil {
		t.Fatal(err)
	}
	b := filepath.Join(t.TempDir(), "root-b")
	if err := os.Mkdir(b, 0o755); err != nil {
		t.Fatal(err)
	}
	f.env(t, f.inbox, b)
	rel, err := newReverifier(tg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = rel.Verdict(fetchID)
	vpath := filepath.Join(f.data, "updater", "verdicts", fetchID+".json")
	want := fmt.Sprintf("release root refused: the verdict for %s names the release dir %s, not %s under the current VL_RELEASE_DIR — "+
		"to use this release there: (1) remove the old verdict by hand: rm %s (2) then re-fetch it: vl-updater --install-dir %s fetch %s",
		fetchID, filepath.Join(f.root, fetchSHA), filepath.Join(b, fetchSHA), vpath, tg.InstallDir, fetchID)
	if err == nil || err.Error() != want {
		t.Fatalf("Verdict under the moved root = %v;\nwant exactly %q", err, want)
	}
	// follow the text, as printed
	msg := err.Error()
	i, k := strings.Index(msg, "by hand: "), strings.Index(msg, " (2) then re-fetch it: ")
	if i < 0 || k < i {
		t.Fatalf("no two steps in %q", msg)
	}
	rmCmd, fetchCmd := msg[i+len("by hand: "):k], strings.Fields(msg[k+len(" (2) then re-fetch it: "):])
	if out, err := exec.Command("/bin/sh", "-c", rmCmd).CombinedOutput(); err != nil {
		t.Fatalf("step 1 %q: %v %s", rmCmd, err, out)
	}
	if len(fetchCmd) == 0 || fetchCmd[0] != "vl-updater" {
		t.Fatalf("step 2 is not an vl-updater command: %q", fetchCmd)
	}
	if rc, out, errs := runCLI(t, nil, fetchCmd[1:]...); rc != 0 {
		t.Fatalf("step 2 %q = %d %q %q", fetchCmd, rc, out, errs)
	}
	v, err := rel.Verdict(fetchID)
	if err != nil || v.ReleaseDir != filepath.Join(b, fetchSHA) {
		t.Fatalf("Verdict after following the text = %+v, %v; want the verdict at %s", v, err, filepath.Join(b, fetchSHA))
	}
	if n, err := rel.Rehash(v); err != nil || n == 0 {
		t.Fatalf("re-proof Rehash under B = %d, %v", n, err)
	}
	if facts, err := rel.Reverify(v); err != nil || facts.SourceSHA != fetchSHA {
		t.Fatalf("re-proof Reverify under B = %+v, %v", facts, err)
	}
}

// PIN (U4N item B): fetch refuses — and writes NO verdict and NO release dir —
// without its knobs (C9: a local inbox, never a network source; the release
// root the one resolver reads), with a release root inside the install, a
// relative knob, no trust anchor in the install (C7), no archive in the
// inbox, or a forged release id.
func TestFetchRefusesWithoutItsInputs(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(t *testing.T, f *fetchRig) []string // returns the fetch operands
		want  string
	}{
		{"no inbox", func(t *testing.T, f *fetchRig) []string { f.env(t, "", f.root); return []string{fetchID} }, releaseInboxEnv + " is not set"},
		{"relative inbox", func(t *testing.T, f *fetchRig) []string { f.env(t, "inbox", f.root); return []string{fetchID} }, "must be an absolute path"},
		{"no release dir", func(t *testing.T, f *fetchRig) []string { f.env(t, f.inbox, ""); return []string{fetchID} }, "NOFX_RELEASE_DIR is not set"},
		{"relative release dir", func(t *testing.T, f *fetchRig) []string { f.env(t, f.inbox, "releases"); return []string{fetchID} }, "must be an absolute path"},
		{"release dir inside the install", func(t *testing.T, f *fetchRig) []string {
			in := filepath.Join(f.inst, "releases")
			if err := os.Mkdir(in, 0o755); err != nil {
				t.Fatal(err)
			}
			f.root = in
			f.env(t, f.inbox, in)
			return []string{fetchID}
		}, "outside the install"},
		// U4F defect 1: containment compares path ELEMENTS — "..rel" is a
		// directory INSIDE the install whose name merely starts with "..".
		{"release dir <install>/..rel", func(t *testing.T, f *fetchRig) []string {
			in := filepath.Join(f.inst, "..rel")
			if err := os.Mkdir(in, 0o755); err != nil {
				t.Fatal(err)
			}
			f.root = in
			f.env(t, f.inbox, in)
			return []string{fetchID}
		}, "outside the install"},
		// U4F defect 2: containment resolves symlinks on BOTH sides first —
		// text that looks outside can still point inside (probe P2).
		{"release dir through a symlink to the install", func(t *testing.T, f *fetchRig) []string {
			link := filepath.Join(t.TempDir(), "instlink")
			if err := os.Symlink(f.inst, link); err != nil {
				t.Fatal(err)
			}
			in := filepath.Join(f.inst, "rel")
			if err := os.Mkdir(in, 0o755); err != nil {
				t.Fatal(err)
			}
			f.root = in
			f.env(t, f.inbox, filepath.Join(link, "rel"))
			return []string{fetchID}
		}, "is not its own resolved path"},
		{"release dir through a symlinked ancestor outside the install", func(t *testing.T, f *fetchRig) []string {
			link := filepath.Join(t.TempDir(), "baselink")
			if err := os.Symlink(filepath.Dir(f.root), link); err != nil {
				t.Fatal(err)
			}
			f.env(t, f.inbox, filepath.Join(link, filepath.Base(f.root)))
			return []string{fetchID}
		}, "is not its own resolved path"},
		{"install dir named through a symlink, release dir inside the real install", func(t *testing.T, f *fetchRig) []string {
			link := filepath.Join(t.TempDir(), "instlink")
			if err := os.Symlink(f.inst, link); err != nil {
				t.Fatal(err)
			}
			in := filepath.Join(f.inst, "rel")
			if err := os.Mkdir(in, 0o755); err != nil {
				t.Fatal(err)
			}
			f.instArg, f.root = link, in
			f.env(t, f.inbox, in)
			return []string{fetchID}
		}, "outside the install"},
		{"no allowed-signers in the install", func(t *testing.T, f *fetchRig) []string {
			if err := os.Remove(updaterworker.ReleaseAllowedSignersPath(f.inst)); err != nil {
				t.Fatal(err)
			}
			return []string{fetchID}
		}, "no allowed-signers file"},
		// U4F defect 3: a symlinked <install>/deploy is never followed to a
		// trust anchor outside the install (probe P4)
		{"deploy/ in the install is a symlink", func(t *testing.T, f *fetchRig) []string {
			deploy := filepath.Join(f.inst, "deploy")
			elsewhere := filepath.Join(t.TempDir(), "deploy-elsewhere")
			if err := os.Rename(deploy, elsewhere); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(elsewhere, deploy); err != nil {
				t.Fatal(err)
			}
			return []string{fetchID}
		}, "allowed-signers file is unsafe"},
		{"no archive in the inbox", func(t *testing.T, f *fetchRig) []string {
			if err := os.Remove(filepath.Join(f.inbox, fetchID+".tar.gz")); err != nil {
				t.Fatal(err)
			}
			return []string{fetchID}
		}, "is not a regular file"},
		{"forged release id", func(t *testing.T, f *fetchRig) []string { return []string{"../" + fetchID} }, "invalid release id"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFetchRig(t)
			ops := c.setup(t, &f)
			inst := f.inst
			if f.instArg != "" {
				inst = f.instArg
			}
			rc, out, errs := runCLI(t, nil, append([]string{"--install-dir", inst, "fetch"}, ops...)...)
			if rc == 0 || !strings.Contains(errs, c.want) || out != "" {
				t.Fatalf("fetch = %d %q %q; want refused with %q", rc, out, errs, c.want)
			}
			if _, err := os.Lstat(filepath.Join(f.data, "updater", "verdicts", fetchID+".json")); !os.IsNotExist(err) {
				t.Fatalf("a refused fetch left a verdict (%v)", err)
			}
			if ents, _ := os.ReadDir(f.root); len(ents) != 0 {
				t.Fatalf("a refused fetch left %d entries in the release root", len(ents))
			}
		})
	}
}

// PIN (U4N item B): the release fixture is TEST support — the production
// binary's dependency graph never contains it (nor the testing package).
func TestTheUpdaterBinaryNeverLinksTheReleaseFixture(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "vl/cmd/vl-updater").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	deps := "\n" + string(out)
	if !strings.Contains(deps, "\nvl/internal/updaterworker\n") {
		t.Fatalf("the dependency listing is not the updater's (no vl/internal/updaterworker):\n%s", out)
	}
	for _, bad := range []string{"\nvl/internal/updaterworker/releasefixture\n", "\ntesting\n"} {
		if strings.Contains(deps, bad) {
			t.Fatalf("vl-updater links %q", strings.TrimSpace(bad))
		}
	}
}

// TestWarnSelfUpdateRestart (worker-self-update P1): the boot WARN is READ,
// never literal — n/a when systemctl is unavailable; silent when the knob is
// off.
func TestWarnSelfUpdateRestart(t *testing.T) {
	var buf bytes.Buffer
	t.Setenv("VL_UPDATER_SELF_UPDATE", "0")
	warnSelfUpdateRestart(&buf)
	if buf.Len() != 0 {
		t.Fatalf("knob off must be silent, got %q", buf.String())
	}
	t.Setenv("VL_UPDATER_SELF_UPDATE", "1")
	t.Setenv("PATH", t.TempDir()) // no systemctl on this PATH
	buf.Reset()
	warnSelfUpdateRestart(&buf)
	if !strings.Contains(buf.String(), "n/a") {
		t.Fatalf("unavailable systemctl must read n/a, got %q", buf.String())
	}
}
