package deploy

// migrate_to_vl_test — the test suite for deploy/migrate-to-vl.sh.
//
// CENSUS LAW: this file holds ZERO occurrences of the pre-rename prefix in any
// casing. The token is assembled at runtime by oldName() below.
//
// The suite runs the script against a scratch HOME with PATH-injected fakes:
// systemctl, sudo, curl, go, journalctl (and a FAKE BOT inside the release
// dir). Real git, jq, mv, ln, realpath, sed, awk and friends come from the
// system PATH. Every scenario below is pinned by the rename plan's R2 section
// and by build rulings B2.

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	_ "vl/store/sqlitedriver" // the ONE sqlite registration site (census)
)

func oldName() string { return "no" + "fx" }

// buildRealActivate builds the production vl-activate CLI once per test process
// (canon 53: the migrate script is exercised against the REAL backup binary,
// not a printf fake).
var (
	activateOnce sync.Once
	activateBin  string
	activateErr  error
)

func buildRealActivate() (string, error) {
	activateOnce.Do(func() {
		wd, _ := os.Getwd()
		root := filepath.Join(wd, "..")
		dir, err := os.MkdirTemp("", "migrate-vl-activate-")
		if err != nil {
			activateErr = err
			return
		}
		bin := filepath.Join(dir, "vl-activate")
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/vl-activate")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			activateErr = fmt.Errorf("go build ./cmd/vl-activate: %v\n%s", err, out)
			return
		}
		activateBin = bin
	})
	return activateBin, activateErr
}

// makeFixtureDB writes a REAL sqlite database (the backup step runs integrity
// checks against it, so string bytes would be refused).
func makeFixtureDB(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open fixture db: %v", err)
	}
	if _, err := db.Exec("create table migrate_fixture(a int)"); err != nil {
		t.Fatalf("create fixture table: %v", err)
	}
	if _, err := db.Exec("insert into migrate_fixture(a) values (1),(2),(3)"); err != nil {
		t.Fatalf("seed fixture table: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close fixture db: %v", err)
	}
}

// ---------------------------------------------------------------------------
// fake environment
// ---------------------------------------------------------------------------

type fakeEnv struct {
	t       *testing.T
	root    string
	home    string
	bin     string
	state   string
	oldName string
	oldSHA  string // 40-hex, the DISK binary's rev (what is running today)
	old12   string
	newSHA  string // 40-hex = the --sha under test AND the tree's HEAD
	new12   string
	session string
	relDir  string
}

func newFakeEnv(t *testing.T) *fakeEnv {
	t.Helper()
	o := oldName()
	oldSHA := "a1111111111111111111111111111111111111"
	fe := &fakeEnv{
		t:       t,
		root:    t.TempDir(),
		oldName: o,
		oldSHA:  oldSHA,
		old12:   oldSHA[:12],
		session: "sess-test",
	}
	fe.home = filepath.Join(fe.root, "home")
	fe.bin = filepath.Join(fe.root, "bin")
	fe.state = filepath.Join(fe.root, "state")
	fe.relDir = filepath.Join(fe.root, "reldir")
	for _, d := range []string{fe.home, fe.bin, fe.state, fe.relDir,
		filepath.Join(fe.root, "etc", "systemd", "system"),
		filepath.Join(fe.root, "nvm", "versions", "node", "v20.9.0", "bin")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	fe.writeFakes()
	fe.writeOldTree()
	fe.writeReleaseDir()
	fe.writeLockHome()
	fe.writeGateDefault()
	fe.writeHealth(fe.old12)
	return fe
}

func (fe *fakeEnv) write(name, content string) {
	fe.t.Helper()
	p := name
	if !filepath.IsAbs(p) {
		p = filepath.Join(fe.root, name)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		fe.t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
	}
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		fe.t.Fatalf("write %s: %v", p, err)
	}
}

func (fe *fakeEnv) writeState(name, content string) {
	fe.t.Helper()
	p := filepath.Join(fe.state, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		fe.t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		fe.t.Fatalf("write %s: %v", p, err)
	}
}

func (fe *fakeEnv) readState(name string) string {
	fe.t.Helper()
	b, err := os.ReadFile(filepath.Join(fe.state, name))
	if err != nil {
		return ""
	}
	return string(b)
}

func (fe *fakeEnv) journal() []string {
	fe.t.Helper()
	f, err := os.Open(filepath.Join(fe.state, "journal.log"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}

func (fe *fakeEnv) journalHas(sub string) bool {
	for _, l := range fe.journal() {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// fakes
// ---------------------------------------------------------------------------

const fakeSystemctl = `#!/usr/bin/env bash
set -u
ST="$FAKE_STATE"
J="$ST/journal.log"
log(){ echo "$*" >> "$J"; }
unit_file(){ printf '%s' "$ST/units/$1"; }
is_active(){ [ -f "$ST/active/$1" ]; }
act(){ mkdir -p "$ST/active"; : > "$ST/active/$1"; }
deact(){ rm -f "$ST/active/$1"; }
set_mp(){ mkdir -p "$ST/mainpid"; printf '%s' "$1" > "$ST/mainpid/$2"; }
mp(){ cat "$ST/mainpid/$1" 2>/dev/null || echo 0; }
usr=0
if [ "${1:-}" = "--user" ]; then usr=1; shift; fi
cmd="${1:-}"
# The REAL bot appends to its log continuously, so its log mtime keeps advancing
# past the migration script's RUN_START mark. The fixture bot does not run during
# the already-migrated re-run, so the fake systemctl models that: whenever the
# script asks about unit vl, refresh the vl log's mtime. This keeps the re-run's
# file-clock vs file-clock freshness check (item 8) deterministic — without it a
# second boundary between the fixture's pre-write and RUN_MARK makes the log look
# stale forever (TestRerunAlreadyMigratedBranch failed 1/15 at 90.63 s).
vl_touch(){
  for f in "$HOME/vl/data/vl_"*.log; do [ -f "$f" ] && touch "$f"; done
}
case "$cmd" in
  cat)
    shift
    case "$1" in vl|vl.service) vl_touch ;; esac
    for n in "$1" "$1.service"; do
      if [ -f "$(unit_file "$n")" ]; then cat "$(unit_file "$n")"; exit 0; fi
    done
    exit 1 ;;
  is-active)
    case "$2" in vl|vl.service) vl_touch ;; esac
    is_active "$2" && exit 0 || exit 1 ;;
  show)
    u="${@: -1}"
    mp "$u"
    exit 0 ;;
  stop)
    shift
    for u in "$@"; do
      log "stop $u"
      # 6b fixture: the old worker creates a job in the instant before it
      # stops — step 1's re-scan must catch it.
      if [ "$u" = "${OLDNAME}-updater" ] && [ "${JOB_APPEARS:-0}" = "1" ]; then
        mkdir -p "$HOME/$OLDNAME/data/updater/jobs"
        printf '{"job_id":"late-job","state":"running","phase":"started","created_at":"%s"}\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$HOME/$OLDNAME/data/updater/jobs/late.json"
      fi
      deact "$u"; set_mp 0 "$u"
    done
    exit 0 ;;
  start)
    shift
    for u in "$@"; do log "start $u"; act "$u"; done
    exit 0 ;;
  disable)
    shift
    for u in "$@"; do log "disable $u"; deact "$u"; done
    exit 0 ;;
  enable)
    shift
    now=0
    [ "${1:-}" = "--now" ] && { now=1; shift; }
    for u in "$@"; do
      log "enable $u"
      if [ "$u" = "vl-updater" ] && [ -f "$ST/fail_enable_updater" ]; then exit 1; fi
      act "$u"
      if [ "$now" = 1 ] && [ "$u" = "vl" ]; then
        ( cd "$HOME/vl" && ./vl-bin )
      fi
      if [ "$now" = 1 ] && [ "$u" = "$OLDNAME" ]; then
        # the FAKE OLD BOT: a fresh old-prefix boot line + health (optionally
        # delayed so the rollback verify's poll is exercised).
        oldboot(){
          mkdir -p data
          echo "BOOT INTEGRITY OK — rev $OLD12" >> "data/${OLDNAME}_$(date +%F).log"
          echo "$OLD12" > "$FAKE_STATE/health_rev.txt"
        }
        if [ -n "${BOT_DELAY_S:-}" ]; then
          ( cd "$HOME/$OLDNAME" && sleep "$BOT_DELAY_S" && oldboot ) &
        else
          ( cd "$HOME/$OLDNAME" && oldboot )
        fi
      fi
      if [ "$now" = 1 ]; then set_mp 4242 "$u"; fi
    done
    exit 0 ;;
  daemon-reload) exit 0 ;;
  list-timers)
    printf 'NEXT  LEFT  LAST  PASSED  UNIT\n'
    printf 'in 4h 4h left n/a n/a vl-backup.timer\n'
    printf 'in 5h 5h left n/a n/a vl-clock-guard.timer\n'
    exit 0 ;;
  *) log "systemctl $*"; exit 0 ;;
esac
`

const fakeSudo = `#!/usr/bin/env bash
set -u
ST="$FAKE_STATE"
J="$ST/journal.log"
log(){ echo "$*" >> "$J"; }
case "${1:-}" in
  -v|-n) exit 0 ;;
  -k) exit 0 ;;
  systemctl) shift; exec "$FAKE_BIN/systemctl" "$@" ;;
  tee)
    shift
    path=""
    for a in "$@"; do case "$a" in /*) path="$a";; esac; done
    case "$path" in /etc/*) path="$FAKE_ETC${path#/etc}" ;; esac
    mkdir -p "$(dirname "$path")"
    cat > "$path"
    log "sudo tee $path"
    case "$path" in */systemd/system/*.service) mkdir -p "$ST/units"; cp "$path" "$ST/units/$(basename "$path")" ;; esac
    if [ "$(basename "$path")" = "vl.service" ] && [ -f "$ST/fail_tee_vl" ]; then exit 1; fi
    exit 0 ;;
  rm)
    log "sudo rm $*"
    exit 0 ;;
  *) log "sudo $*"; exit 0 ;;
esac
`

const fakeCurl = `#!/usr/bin/env bash
set -u
ST="$FAKE_STATE"
url=""; fmt=""
for a in "$@"; do
  case "$a" in http*) url="$a";; -w) fmt=1;; esac
done
case "$url" in
  *"/api/health")
    rev="$(cat "$ST/health_rev.txt" 2>/dev/null || true)"
    printf '{"status":"ok","revision":"%s"}\n' "$rev"
    exit 0 ;;
  *"/api/installation-gate")
    if [ "${GATE_PLANNER_FLIP:-0}" = "1" ]; then
      # first read answers READY (step 0); every later read answers the
      # NOT-ready payload (the gate changed while the operator typed sudo).
      if [ -f "$ST/gate_read_once" ]; then
        cat "$ST/gate2.json"
      else
        : > "$ST/gate_read_once"
        cat "$ST/gate.json"
      fi
    else
      cat "$ST/gate.json"
    fi
    exit 0 ;;
  *":3000"*)
    if [ -n "$fmt" ]; then printf '200'; else printf 'ok'; fi
    exit 0 ;;
  *)
    exit 7 ;;
esac
`

const fakeGo = `#!/usr/bin/env bash
set -u
ST="$FAKE_STATE"
if [ "${1:-}" = "version" ] && [ "${2:-}" = "-m" ]; then
  bin="${3:-}"
  base="$(basename "$bin")"
  case "$base" in
    *vl*) rev="$NEW_SHA"; mod="false" ;;
    *) rev="$OLD_SHA"; mod="false" ;;
  esac
  if [ -f "$ST/stamps/$base" ]; then
    rev="$(cut -d' ' -f1 "$ST/stamps/$base")"
    mod="$(cut -d' ' -f2 "$ST/stamps/$base")"
  fi
  printf 'path\t%s\nmod\t%s\n\tbuild\tvcs.revision=%s\n\tbuild\tvcs.modified=%s\n' "$bin" "$bin" "$rev" "$mod"
  exit 0
fi
exit 0
`

const fakeJournalctl = `#!/usr/bin/env bash
cat "$FAKE_STATE/journal_vl_updater.txt"
`

const fakeLockTool = `#!/usr/bin/env bash
set -u
ST="$FAKE_STATE"
case "${1:-}" in
  check)
    rc="$(cat "$ST/lock_check_rc" 2>/dev/null || echo 1)"
    exit "$rc" ;;
  heartbeat)
    now="$(grep -m1 '^heartbeat_epoch=' "$HOME/<OLD>-main.lock.d/meta" 2>/dev/null | cut -d= -f2-)"
    next=$(( ${now:-0} + 1 ))
    sed -i "s/^heartbeat_epoch=.*/heartbeat_epoch=$next/" "$HOME/<OLD>-main.lock.d/meta"
    exit 0 ;;
  *) exit 0 ;;
esac
`

const fakeVlLock = `#!/usr/bin/env bash
set -u
ST="$FAKE_STATE"
case "${1:-}" in
  release)
    echo "release ${2:-}" >> "$ST/release.log"
    rm -rf "$HOME/<OLD>-main.lock.d"
    exit 0 ;;
  *) exit 0 ;;
esac
`

const fakeBot = `#!/usr/bin/env bash
set -u
bootwrite(){
  m="$(tr -d '[:space:]' < deploy/RELEASE 2>/dev/null || true)"
  d="$(date +%F)"
  mkdir -p data
  if [ -n "$m" ] && [ "$(printf '%s' "$m" | cut -c1-12)" = "$BOT_SHA12" ]; then
    echo "BOOT INTEGRITY OK — rev $BOT_SHA12" >> "data/vl_$d.log"
    echo "$BOT_SHA12" > "$FAKE_STATE/health_rev.txt"
    if [ -n "${BOT_SKEW_MARK:-}" ]; then
      # JOB 8 fixture: pin the log's mtime to the step-3 mark's mtime while the
      # mark's CONTENT is realtime+2. File-clock vs file-clock compares equal
      # and passes; reading the content (the pre-item-8 bug) compares strictly
      # later and fails. Deterministic by construction — no second-boundary
      # races.
      printf '%s' "$(($(date +%s)+2))" > "$BOT_SKEW_MARK"
      touch -r "$BOT_SKEW_MARK" "data/vl_$d.log"
    fi
  else
    echo "BOOT INTEGRITY REFUSED — marker '$(printf '%s' "$m" | cut -c1-12)'" >> "data/vl_$d.log"
    echo "$OLD12" > "$FAKE_STATE/health_rev.txt"
  fi
}
if [ -n "${BOT_DELAY_S:-}" ]; then
  # ONLY the delay tests set BOT_DELAY_S. Every other forward test relies on the
  # synchronous path below: the boot line and health are written INLINE, before
  # the systemctl enable --now vl call returns, so verify's first probe finds
  # them and no 2 s wait_leg cost is paid. Do not introduce an async write here.
  ( sleep "$BOT_DELAY_S"; bootwrite ) &
else
  bootwrite
fi
if [ "${BOT_HELLO:-0}" = "1" ]; then
  sleep 1.5
  echo "NT8 hello — build 2026-09-30-test" >> "data/vl_$d.log"
fi
if [ "${BOT_TAMPER_MARKER:-0}" = "1" ]; then
  printf '%s\n' "$OLD12" > deploy/RELEASE
fi
if [ "${BOT_COMMIT:-0}" = "1" ]; then
  git config user.email test@test >/dev/null 2>&1
  git config user.name test >/dev/null 2>&1
  git commit --allow-empty -q -m "bot ahead"
fi
`

const fakeActivate = `#!/usr/bin/env bash
set -u
src=""; dest=""
while [ $# -gt 0 ]; do
  case "$1" in -db) src="$2"; shift 2;; -dest) dest="$2"; shift 2;; *) shift;; esac
done
if [ -f "$src" ] && [ "${ACTIVATE_ZERO:-0}" != "1" ]; then
  sz="$(stat -c%s "$src")"
  mkdir -p "$(dirname "$dest")"
  cp "$src" "$dest"
  if [ "${ACTIVATE_NONNUMERIC:-0}" = "1" ]; then
    printf '{"step":"backup","started_at":"t","ended_at":"t","ok":true,"evidence":{"bytes":"abc","db":"%s","dest":"%s","integrity_check":"ok"}}\n' "$src" "$dest"
  else
    printf '{"step":"backup","started_at":"t","ended_at":"t","ok":true,"evidence":{"bytes":"%s","db":"%s","dest":"%s","integrity_check":"ok"}}\n' "$sz" "$src" "$dest"
  fi
else
  printf '{"step":"backup","started_at":"t","ended_at":"t","ok":false,"err":"fake activate zero","evidence":{}}\n'
fi
`

// fakeActivateOldShape prints the PRE-FIX top-level receipt shape (canon 53:
// the suite once proved self-consistency against this and missed the real
// nested evidence shape — the script must now refuse it).
const fakeActivateOldShape = `#!/usr/bin/env bash
set -u
src=""; dest=""
while [ $# -gt 0 ]; do
  case "$1" in -db) src="$2"; shift 2;; -dest) dest="$2"; shift 2;; *) shift;; esac
done
sz="$(stat -c%s "$src" 2>/dev/null || echo 0)"
printf '{"ok":true,"integrity_check":"ok","bytes":%s}\n' "$sz"
`

const fakePostboot = `#!/usr/bin/env bash
exit 0
`

const fakeNode = `#!/usr/bin/env bash
exit 0
`

func (fe *fakeEnv) writeFakes() {
	fe.t.Helper()
	fe.write("bin/systemctl", fakeSystemctl)
	fe.write("bin/sudo", fakeSudo)
	fe.write("bin/curl", fakeCurl)
	fe.write("bin/go", fakeGo)
	fe.write("bin/journalctl", fakeJournalctl)
	fe.write("bin/node", fakeNode)
	fe.write("bin/npm", fakeNode)
	fe.write("nvm/versions/node/v20.9.0/bin/node", fakeNode)
	fe.write("nvm/versions/node/v20.9.0/bin/npm", fakeNode)
}

func (fe *fakeEnv) writeOldTree() {
	fe.t.Helper()
	o := fe.oldName
	tree := filepath.Join(fe.home, o)
	for _, d := range []string{
		filepath.Join(tree, "deploy"),
		filepath.Join(tree, "deploy", "systemd-user"),
		filepath.Join(tree, "data", "updater", "jobs"),
		filepath.Join(tree, "web", "dist"),
		filepath.Join(fe.home, ".config", o+"-updater"),
		filepath.Join(fe.home, "bin"),
		filepath.Join(fe.home, ".config", "systemd", "user"),
		filepath.Join(fe.root, "origin.git"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			fe.t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", tree}, args...)...)
		cmd.Env = append(os.Environ(), "HOME="+fe.home)
		out, err := cmd.CombinedOutput()
		if err != nil {
			fe.t.Fatalf("git %v: %v (%s)", args, err, out)
		}
		return string(out)
	}
	// the remote origin is a bare repo so @{u} resolves for the release-lock
	// checks and the tree's HEAD tracks it.
	rb := exec.Command("git", "init", "-q", "--bare", filepath.Join(fe.root, "origin.git"))
	rb.Env = append(os.Environ(), "HOME="+fe.home)
	if out, err := rb.CombinedOutput(); err != nil {
		fe.t.Fatalf("git init --bare: %v (%s)", err, out)
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "test@test")
	git("config", "user.name", "test")
	fe.write(filepath.Join(tree, "deploy", "RELEASE"), fe.oldSHA+"\n")
	fe.write(filepath.Join(tree, o+"-bin"), "old binary bytes")
	makeFixtureDB(fe.t, filepath.Join(tree, "data", "data.db"))
	fe.write(filepath.Join(tree, "deploy", o+"-lock.sh"),
		strings.ReplaceAll(fakeLockTool, "<OLD>", o))
	fe.write(filepath.Join(tree, "deploy", "vl-lock.sh"),
		strings.ReplaceAll(fakeVlLock, "<OLD>", o))
	fe.write(filepath.Join(tree, "deploy", "vl.service"),
		"[Unit]\nDescription=vl\n[Service]\nUser=__VL_USER__\nWorkingDirectory=__VL_DIR__\nExecStart=__VL_DIR__/vl-bin\n")
	fe.write(filepath.Join(tree, "deploy", "vl-web.service"),
		"[Unit]\nDescription=vl web\n[Service]\nUser=__VL_USER__\nWorkingDirectory=__VL_DIR__\nEnvironment=PATH=__NODE_DIR__:bin\nExecStart=__NODE_DIR__/npm run dev\n")
	for _, u := range []string{"vl-backup.service", "vl-backup.timer", "vl-clock-guard.service", "vl-clock-guard.timer", "vl-updater.service"} {
		fe.write(filepath.Join(tree, "deploy", "systemd-user", u), "[Unit]\nDescription="+u+"\n")
	}
	fe.write(filepath.Join(tree, "deploy", "postboot-check.sh"), fakePostboot)
	git("add", "-A")
	git("commit", "-q", "-m", "tree")
	git("remote", "add", "origin", filepath.Join(fe.root, "origin.git"))
	git("push", "-q", "-u", "origin", "main")

	// the tree sits AT --sha (the new sha under test); the DISK binary is the
	// old sha (the running build). This is exactly the R2 pre-boot state.
	fe.newSHA = strings.TrimSpace(git("rev-parse", "HEAD"))
	fe.new12 = fe.newSHA[:12]

	// user units present by default (optional-unit scenarios delete them)
	for _, u := range []string{o + "-updater", o + "-backup.service", o + "-backup.timer", o + "-clock-guard.service", o + "-clock-guard.timer"} {
		fe.writeState("units/"+u, "[Unit]\nDescription="+u+"\n")
	}
	fe.writeState("units/"+o, "[Unit]\nDescription="+o+"\n")
	fe.writeState("units/"+o+"-web", "[Unit]\nDescription="+o+" web\n")
	// the updater env file holds the OLD-prefix keys (as the box does pre-R2)
	fe.write(filepath.Join(fe.home, ".config", o+"-updater", "env"),
		strings.ToUpper(o)+"_CUTOVER_TOKEN=x\n"+strings.ToUpper(o)+"_RELEASE_DIR="+filepath.Join(fe.home, o+"-releases")+"\n")
	fe.write(filepath.Join(fe.home, "bin", o+"-updater"), "old updater bytes")
	// running old units
	fe.writeState("active/"+o, "")
	fe.writeState("mainpid/"+o, "4321")
	fe.writeState("active/"+o+"-web", "")
	fe.writeState("mainpid/"+o+"-web", "4322")
	fe.writeState("active/"+o+"-updater", "")
	fe.writeState("mainpid/"+o+"-updater", "4330")
	fe.writeLockHome()
	fe.writeState("lock_check_rc", "1")
	fe.writeState("journal_vl_updater.txt",
		"🔧 vl-updater: serving 127.0.0.1:9 · install "+fe.home+"/vl · data "+fe.home+"/vl/data\n")
}

func (fe *fakeEnv) refreshNewSHA() {
	fe.t.Helper()
	fe.newSHA = strings.TrimSpace(fe.gitTree("rev-parse", "HEAD"))
	fe.new12 = fe.newSHA[:12]
}

func (fe *fakeEnv) gitTree(args ...string) string {
	fe.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", filepath.Join(fe.home, fe.oldName)}, args...)...)
	cmd.Env = append(os.Environ(), "HOME="+fe.home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fe.t.Fatalf("gitTree %v: %v (%s)", args, err, out)
	}
	return string(out)
}

func (fe *fakeEnv) writeLockHome() {
	fe.t.Helper()
	o := fe.oldName
	dir := filepath.Join(fe.home, o+"-main.lock.d")
	_ = os.MkdirAll(dir, 0o755)
	meta := fmt.Sprintf("session=%s\ntask=test\nheartbeat_epoch=%d\nexpiry_epoch=%d\n",
		fe.session, time.Now().Unix(), time.Now().Add(2*time.Hour).Unix())
	if err := os.WriteFile(filepath.Join(dir, "meta"), []byte(meta), 0o644); err != nil {
		fe.t.Fatalf("meta: %v", err)
	}
}

func (fe *fakeEnv) writeReleaseDir() {
	fe.t.Helper()
	fe.write(filepath.Join(fe.relDir, "vl-bin"), fakeBot)
	bin, err := buildRealActivate()
	if err != nil {
		fe.t.Fatalf("real vl-activate: %v", err)
	}
	b, err := os.ReadFile(bin)
	if err != nil {
		fe.t.Fatalf("read real vl-activate: %v", err)
	}
	fe.write(filepath.Join(fe.relDir, "vl-activate"), string(b))
	fe.write(filepath.Join(fe.relDir, "updater", "vl-updater"), "updater bytes")
	fe.write(filepath.Join(fe.relDir, "updater", "vl-updater-bootstrap"), "bootstrap bytes")
	fe.write(filepath.Join(fe.relDir, "web", "dist", "index.html"), "<html>vl</html>")
}

func (fe *fakeEnv) writeGateDefault() {
	fe.t.Helper()
	fe.writeState("gate.json", `{
  "ready": false,
  "job_id": "gate-1",
  "legs": [
    {"name": "trader_cutover:open_positions", "pass": true},
    {"name": "ledger_exposure", "pass": true},
    {"name": "planner_in_flight", "pass": true},
    {"name": "traders_nt8", "pass": true}
  ]
}`)
}

func (fe *fakeEnv) writeGate(content string) {
	fe.t.Helper()
	fe.writeState("gate.json", content)
}

func (fe *fakeEnv) writeHealth(rev string) {
	fe.t.Helper()
	fe.writeState("health_rev.txt", rev)
}

func (fe *fakeEnv) env() []string {
	o := fe.oldName
	O := strings.ToUpper(o)
	env := append([]string{}, os.Environ()...)
	env = append(env,
		"HOME="+fe.home,
		"PATH="+fe.bin+":/usr/bin:/bin",
		"FAKE_STATE="+fe.state,
		"FAKE_BIN="+fe.bin,
		"FAKE_ETC="+filepath.Join(fe.root, "etc"),
		"NEW_SHA="+fe.newSHA,
		"OLD_SHA="+fe.oldSHA,
		"OLD12="+fe.old12,
		"OLDNAME="+o,
		"BOT_SHA12="+fe.new12,
		"VL_CUTOVER_TOKEN=tok-test",
		O+"_CUTOVER_TOKEN=old-tok",
	)
	return env
}

func (fe *fakeEnv) run(t *testing.T, args ...string) (string, int) {
	t.Helper()
	wd, _ := os.Getwd()
	script := filepath.Join(wd, "migrate-to-vl.sh")
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Env = fe.env()
	cmd.Dir = fe.home
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run: %v", err)
		}
	}
	return string(out), code
}

func (fe *fakeEnv) argsForward() []string {
	return []string{
		"--session", fe.session,
		"--sha", fe.newSHA,
		"--release-dir", fe.relDir,
	}
}

func (fe *fakeEnv) homeTreeHash() string {
	fe.t.Helper()
	cmd := exec.Command("sh", "-c", `find . \( -type f -o -type l \) -print | sort | xargs sha256sum 2>/dev/null | sha256sum | cut -d' ' -f1`)
	cmd.Dir = fe.home
	out, err := cmd.Output()
	if err != nil {
		fe.t.Fatalf("tree hash: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func mustContain(t *testing.T, out, sub string) {
	t.Helper()
	if !strings.Contains(out, sub) {
		t.Fatalf("output missing %q\n--- output ---\n%s", sub, out)
	}
}

func mustNotContain(t *testing.T, out, sub string) {
	t.Helper()
	if strings.Contains(out, sub) {
		t.Fatalf("output must NOT contain %q\n--- output ---\n%s", sub, out)
	}
}

func runWithEnv(t *testing.T, fe *fakeEnv, extra []string, args ...string) (string, int) {
	t.Helper()
	wd, _ := os.Getwd()
	cmd := exec.Command("bash", append([]string{filepath.Join(wd, "migrate-to-vl.sh")}, args...)...)
	cmd.Env = append(fe.env(), extra...)
	cmd.Dir = fe.home
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run: %v", err)
		}
	}
	return string(out), code
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestForwardPath(t *testing.T) {
	fe := newFakeEnv(t)
	out, code := fe.run(t, fe.argsForward()...)
	if code != 0 {
		t.Fatalf("forward exit %d\n%s", code, out)
	}
	mustContain(t, out, "boot line OK")
	mustContain(t, out, "DONE — the box runs vl")
	b, _ := os.ReadFile(filepath.Join(fe.home, "vl", "deploy", "RELEASE"))
	if strings.TrimSpace(string(b)) != fe.newSHA {
		t.Fatalf("RELEASE marker = %q, want %s", b, fe.newSHA)
	}
	if _, err := os.Stat(filepath.Join(fe.home, "vl", "deploy", "RELEASE.pre-vl."+fe.old12)); err != nil {
		t.Fatalf("saved marker missing: %v", err)
	}
	oldBin := filepath.Join(fe.home, "vl", fe.oldName+"-bin.old."+fe.old12)
	if _, err := os.Stat(oldBin); err != nil {
		t.Fatalf("parked old binary missing: %v", err)
	}
	// the old binary is parked AFTER verify, never before: the park line must
	// print after the boot-line leg in the script's own output
	iVerify := strings.Index(out, "boot line OK")
	iPark := strings.Index(out, "parked the old binary")
	if iVerify < 0 || iPark < 0 || iVerify > iPark {
		t.Fatalf("old binary parked before verify (verify@%d park@%d)", iVerify, iPark)
	}
	link, err := os.Readlink(filepath.Join(fe.home, fe.oldName))
	if err != nil || link != filepath.Join(fe.home, "vl") {
		t.Fatalf("symlink = %q (%v)", link, err)
	}
	if _, err := os.Stat(filepath.Join(fe.home, fe.oldName+"-main.lock.d", "meta")); err != nil {
		t.Fatalf("lock home gone after a default (keep) run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fe.home, ".local", "state", "vl-migrate", fe.session)); err != nil {
		t.Fatalf("state file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fe.home, "bin", fe.oldName+"-updater.old."+fe.old12)); err != nil {
		t.Fatalf("parked old updater missing: %v", err)
	}
	envb, _ := os.ReadFile(filepath.Join(fe.home, ".config", "vl-updater", "env"))
	if !strings.Contains(string(envb), "VL_RELEASE_DIR="+fe.home+"/vl-releases") {
		t.Fatalf("env rewrite missing: %s", envb)
	}
	j := fe.journal()
	idxEnable, idxDisable := -1, -1
	for i, l := range j {
		if strings.Contains(l, "enable vl") && idxEnable < 0 {
			idxEnable = i
		}
		if strings.Contains(l, "disable "+fe.oldName) && idxDisable < 0 {
			idxDisable = i
		}
	}
	if idxEnable < 0 || idxDisable < 0 || idxEnable > idxDisable {
		t.Fatalf("journal order wrong: enable vl @%d disable old @%d\n%v", idxEnable, idxDisable, j)
	}
}

func TestReleaseLockSuccess(t *testing.T) {
	fe := newFakeEnv(t)
	out, code := fe.run(t, append(fe.argsForward(), "--release-lock")...)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(fe.home, fe.oldName+"-main.lock.d")); !os.IsNotExist(err) {
		t.Fatalf("lock home should be gone after --release-lock")
	}
	if !strings.Contains(fe.readState("release.log"), fe.session) {
		t.Fatalf("release not logged: %q", fe.readState("release.log"))
	}
}

func TestReleaseLockRefusedMarkerMismatch(t *testing.T) {
	fe := newFakeEnv(t)
	out, code := runWithEnv(t, fe, []string{"BOT_TAMPER_MARKER=1"}, append(fe.argsForward(), "--release-lock")...)
	if code == 0 {
		t.Fatalf("expected refusal, got success\n%s", out)
	}
	mustContain(t, out, "--release-lock refused")
	if _, err := os.Stat(filepath.Join(fe.home, fe.oldName+"-main.lock.d", "meta")); err != nil {
		t.Fatalf("lock must stay held on refusal: %v", err)
	}
}

func TestReleaseLockRefusedHeadAhead(t *testing.T) {
	fe := newFakeEnv(t)
	out, code := runWithEnv(t, fe, []string{"BOT_COMMIT=1"}, append(fe.argsForward(), "--release-lock")...)
	if code == 0 {
		t.Fatalf("expected refusal, got success\n%s", out)
	}
	mustContain(t, out, "not on its upstream")
}

func TestVerifyFailureAutoRollback(t *testing.T) {
	fe := newFakeEnv(t)
	out, code := runWithEnv(t, fe, []string{"BOT_SHA12=" + strings.Repeat("c", 12), "VL_MIGRATE_VERIFY_WAIT_S=6"}, fe.argsForward()...)
	if code == 0 {
		t.Fatalf("expected verify failure, got success\n%s", out)
	}
	mustContain(t, out, "automatic rollback")
	mustContain(t, out, "rollback DONE")
	if _, err := os.Stat(filepath.Join(fe.home, fe.oldName, fe.oldName+"-bin")); err != nil {
		t.Fatalf("old binary not restored: %v", err)
	}
	if fi, err := os.Lstat(filepath.Join(fe.home, fe.oldName)); err != nil {
		t.Fatalf("old tree missing: %v", err)
	} else if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("old tree is still a symlink")
	}
	if !fe.journalHas("enable " + fe.oldName) {
		t.Fatalf("old units were not re-enabled\n%v", fe.journal())
	}
	b, _ := os.ReadFile(filepath.Join(fe.home, fe.oldName, "deploy", "RELEASE"))
	if strings.TrimSpace(string(b))[:12] != fe.old12 {
		t.Fatalf("marker not restored: %s", b)
	}
}

func TestStepErrorsAutoRollback(t *testing.T) {
	o := oldName()
	cases := []struct {
		name  string
		setup func(fe *fakeEnv)
		extra []string
		want  string
	}{
		{
			name: "step1 DB receipt zero bytes",
			setup: func(fe *fakeEnv) {
				fe.write(filepath.Join(fe.relDir, "vl-activate"), fakeActivate)
			},
			extra: []string{"ACTIVATE_ZERO=1"},
			want:  "receipt is not ok:true",
		},
		{
			name: "step2 env rewrite fails after the moves",
			setup: func(fe *fakeEnv) {
				p := filepath.Join(fe.home, ".config", o+"-updater", "env")
				_ = os.Remove(p)
				if err := os.MkdirAll(p, 0o755); err != nil {
					t.Fatal(err)
				}
			},
			want: "automatic rollback",
		},
		{
			name: "step3 vl.service template missing",
			setup: func(fe *fakeEnv) {
				fe.writeState("fail_tee_vl", "1")
			},
			want: "automatic rollback",
		},
		{
			name: "step4 vl-updater.service template missing",
			setup: func(fe *fakeEnv) {
				fe.writeState("fail_enable_updater", "1")
			},
			want: "automatic rollback",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fe := newFakeEnv(t)
			if tc.setup != nil {
				tc.setup(fe)
			}
			out, code := runWithEnv(t, fe, tc.extra, fe.argsForward()...)
			if code == 0 {
				t.Fatalf("expected failure, got success\n%s", out)
			}
			mustContain(t, out, tc.want)
			mustContain(t, out, "rollback DONE")
			if !fe.journalHas("enable " + o) {
				t.Fatalf("old units not re-enabled\n%v", fe.journal())
			}
			if fi, err := os.Lstat(filepath.Join(fe.home, o)); err != nil {
				t.Fatalf("old tree gone: %v", err)
			} else if fi.Mode()&os.ModeSymlink != 0 {
				t.Fatalf("old tree still a symlink after rollback")
			}
			// the rollback restored the parked updater and the RELEASE marker
			if _, err := os.Stat(filepath.Join(fe.home, "bin", o+"-updater")); err != nil {
				t.Fatalf("old updater not restored: %v", err)
			}
			b, _ := os.ReadFile(filepath.Join(fe.home, o, "deploy", "RELEASE"))
			if strings.TrimSpace(string(b))[:12] != fe.old12 {
				t.Fatalf("marker not restored after rollback: %s", b)
			}
		})
	}
}

func TestExplicitRollbackFromPartialState(t *testing.T) {
	o := oldName()
	fe := newFakeEnv(t)
	p := filepath.Join(fe.home, ".config", o+"-updater", "env")
	_ = os.Remove(p)
	_ = os.MkdirAll(p, 0o755)
	_, _ = fe.run(t, fe.argsForward()...) // fails at step 2 and auto-rolls back
	out, code := fe.run(t, "--session", fe.session, "--rollback")
	if code != 0 {
		t.Fatalf("rollback exit %d\n%s", code, out)
	}
	mustContain(t, out, "rollback DONE")
	if fi, err := os.Lstat(filepath.Join(fe.home, o)); err != nil {
		t.Fatalf("old tree gone: %v", err)
	} else if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("old tree still a symlink")
	}
}

func TestRollbackRefusesWithoutStateFile(t *testing.T) {
	fe := newFakeEnv(t)
	out, code := fe.run(t, "--session", fe.session, "--rollback")
	if code == 0 {
		t.Fatalf("expected refusal, got success\n%s", out)
	}
	mustContain(t, out, "no state file")
}

func TestRollbackLockCheckedFirst(t *testing.T) {
	cases := []struct {
		name  string
		setup func(fe *fakeEnv)
		want  string
	}{
		{
			name: "foreign session",
			setup: func(fe *fakeEnv) {
				dir := filepath.Join(fe.home, fe.oldName+"-main.lock.d")
				meta := fmt.Sprintf("session=other\ntask=x\nheartbeat_epoch=%d\n", time.Now().Unix())
				_ = os.WriteFile(filepath.Join(dir, "meta"), []byte(meta), 0o644)
			},
			want: "needs the lock held",
		},
		{
			name: "no meta",
			setup: func(fe *fakeEnv) {
				_ = os.Remove(filepath.Join(fe.home, fe.oldName+"-main.lock.d", "meta"))
			},
			want: "has no meta",
		},
		{
			name: "stale check rc 2",
			setup: func(fe *fakeEnv) {
				fe.writeState("lock_check_rc", "2")
			},
			want: "check rc: 2",
		},
		{
			name: "free check rc 0",
			setup: func(fe *fakeEnv) {
				fe.writeState("lock_check_rc", "0")
			},
			want: "check rc: 0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fe := newFakeEnv(t)
			stateDir := filepath.Join(fe.home, ".local", "state", "vl-migrate")
			_ = os.MkdirAll(stateDir, 0o755)
			_ = os.WriteFile(filepath.Join(stateDir, fe.session),
				[]byte("session="+fe.session+"\nsha="+fe.newSHA+"\nold_sha="+fe.oldSHA+"\nold12="+fe.old12+"\nts=20260930-000000\nno_updater=0\n"), 0o600)
			tc.setup(fe)
			out, code := fe.run(t, "--session", fe.session, "--rollback")
			if code == 0 {
				t.Fatalf("expected refusal, got success\n%s", out)
			}
			mustContain(t, out, tc.want)
			for _, l := range fe.journal() {
				if strings.HasPrefix(l, "stop ") {
					t.Fatalf("rollback stopped something before the lock check: %v", fe.journal())
				}
			}
		})
	}
}

func TestKeeperBeatsThroughMoveAndRollback(t *testing.T) {
	fe := newFakeEnv(t)
	o := fe.oldName
	metaPath := filepath.Join(fe.home, o+"-main.lock.d", "meta")
	readEpoch := func() string {
		b, _ := os.ReadFile(metaPath)
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "heartbeat_epoch=") {
				return strings.TrimPrefix(l, "heartbeat_epoch=")
			}
		}
		return ""
	}
	before := readEpoch()
	stop := make(chan struct{})
	go func() {
		tick := time.NewTicker(150 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				cmd := exec.Command("bash", filepath.Join(fe.home, o, "deploy", o+"-lock.sh"), "heartbeat", fe.session)
				cmd.Env = fe.env()
				_ = cmd.Run()
			}
		}
	}()
	defer close(stop)
	out, code := fe.run(t, fe.argsForward()...)
	if code != 0 {
		t.Fatalf("forward exit %d\n%s", code, out)
	}
	mid := readEpoch()
	out2, code2 := fe.run(t, "--session", fe.session, "--rollback")
	if code2 != 0 {
		t.Fatalf("rollback exit %d\n%s", code2, out2)
	}
	time.Sleep(400 * time.Millisecond) // at least two keeper beats after step 3
	after := readEpoch()
	if before == mid || mid == after {
		t.Fatalf("heartbeat_epoch did not advance through the move and back (before=%s mid=%s after=%s)", before, mid, after)
	}
}

func TestBackupServiceStoppedBeforeMove(t *testing.T) {
	fe := newFakeEnv(t)
	o := fe.oldName
	fe.writeState("mainpid/"+o+"-backup.service", "4444")
	fe.writeState("active/"+o+"-backup.service", "")
	out, code := fe.run(t, fe.argsForward()...)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	j := fe.journal()
	idxStopBackup, idxEnableVl := -1, -1
	for i, l := range j {
		if strings.Contains(l, "stop "+o+"-backup.service") && idxStopBackup < 0 {
			idxStopBackup = i
		}
		if strings.Contains(l, "enable vl") && idxEnableVl < 0 {
			idxEnableVl = i
		}
	}
	if idxStopBackup < 0 || idxEnableVl < 0 || idxStopBackup > idxEnableVl {
		t.Fatalf("backup service not stopped before the move (stop@%d enable@%d)\n%v", idxStopBackup, idxEnableVl, j)
	}
}

func TestRerunAlreadyMigratedBranch(t *testing.T) {
	fe := newFakeEnv(t)
	o := fe.oldName
	// a box that has the optional releases/inbox dirs: they are moved at step 2,
	// so after the first run EVERY vl path in (d) exists and the re-run takes
	// the already-migrated branch
	for _, d := range []string{o + "-releases", o + "-inbox"} {
		if err := os.MkdirAll(filepath.Join(fe.home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	out, code := fe.run(t, fe.argsForward()...)
	if code != 0 {
		t.Fatalf("first run exit %d\n%s", code, out)
	}
	// the operator starts unit vl manually; the fake bot boots again, fresh.
	// DS-101 P3-1: a post-R2-shaped box has NO ~/vl-main.lock.d (Z18 — it must
	// not exist until R5); the already-migrated branch must still fire.
	logf := filepath.Join(fe.home, "vl", "data", "vl_"+time.Now().Format("2006-01-02")+".log")
	_ = os.MkdirAll(filepath.Dir(logf), 0o755)
	f, _ := os.OpenFile(logf, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	_, _ = fmt.Fprintf(f, "BOOT INTEGRITY OK — rev %s\n", fe.new12)
	_ = f.Close()
	fe.writeHealth(fe.new12)
	j0 := len(fe.journal())
	out2, code2 := fe.run(t, fe.argsForward()...)
	if code2 != 0 {
		t.Fatalf("re-run exit %d\n%s", code2, out2)
	}
	mustContain(t, out2, "already migrated")
	if len(fe.journal()) != j0 {
		t.Fatalf("the verify-only re-run touched the units: %v", fe.journal()[j0:])
	}
	if _, err := os.Lstat(filepath.Join(fe.home, "vl", "vl-bin")); err != nil {
		t.Fatalf("vl tree damaged by re-run: %v", err)
	}
}

func TestStep0Refusals(t *testing.T) {
	o := oldName()
	cases := []struct {
		name  string
		setup func(fe *fakeEnv)
		args  func(fe *fakeEnv) []string
		extra []string
		want  string
	}{
		{"missing old dir", func(fe *fakeEnv) { _ = os.RemoveAll(filepath.Join(fe.home, o)) }, nil, nil, "is missing"},
		{"vl present", func(fe *fakeEnv) { _ = os.MkdirAll(filepath.Join(fe.home, "vl"), 0o755) }, nil, nil, "already exists"},
		{"vl lock home present", func(fe *fakeEnv) { _ = os.MkdirAll(filepath.Join(fe.home, "vl-main.lock.d"), 0o755) }, nil, nil, "already exists"},
		{"foreign session", func(fe *fakeEnv) {
			_ = os.WriteFile(filepath.Join(fe.home, o+"-main.lock.d", "meta"), []byte("session=other\n"), 0o644)
		}, nil, nil, "names a different session"},
		{"no meta", func(fe *fakeEnv) { _ = os.Remove(filepath.Join(fe.home, o+"-main.lock.d", "meta")) }, nil, nil, "has no meta"},
		{"stale lock", func(fe *fakeEnv) { fe.writeState("lock_check_rc", "2") }, nil, nil, "STALE"},
		{"incomplete lock", func(fe *fakeEnv) { fe.writeState("lock_check_rc", "3") }, nil, nil, "INCOMPLETE"},
		{"abandoned lock", func(fe *fakeEnv) { fe.writeState("lock_check_rc", "4") }, nil, nil, "ABANDONED"},
		{"free lock", func(fe *fakeEnv) { fe.writeState("lock_check_rc", "0") }, nil, nil, "not 'held'"},
		{"no token", nil, nil, nil, "token"},
		{"failing gate leg", func(fe *fakeEnv) {
			fe.writeGate(`{"ready":false,"job_id":"g","legs":[{"name":"trader_cutover:open_positions","pass":true},{"name":"ledger_exposure","pass":false},{"name":"planner_in_flight","pass":true},{"name":"traders_nt8","pass":true}]}`)
		}, nil, nil, "failing installation-gate legs"},
		{"NT8 closed text on a stale cutover leg", func(fe *fakeEnv) {
			fe.writeGate(`{"ready":false,"job_id":"g","legs":[{"name":"trader_cutover:working_orders","pass":false},{"name":"ledger_exposure","pass":true},{"name":"planner_in_flight","pass":true},{"name":"traders_nt8","pass":true}]}`)
		}, nil, nil, "NT8 looks closed"},
		{"OLD_SHA health mismatch", func(fe *fakeEnv) { fe.writeHealth("dddddddddddd") }, nil, nil, "mismatch"},
		{"OLD_SHA marker mismatch", func(fe *fakeEnv) {
			_ = os.WriteFile(filepath.Join(fe.home, o, "deploy", "RELEASE"), []byte("eeeeeeeeeeee\n"), 0o644)
			fe.gitTree("add", "-A")
			fe.gitTree("commit", "-q", "-m", "marker changed")
			fe.refreshNewSHA()
		}, nil, nil, "mismatch"},
		{"neither health nor marker", func(fe *fakeEnv) {
			fe.writeHealth("")
			fe.gitTree("rm", "-q", "deploy/RELEASE")
			fe.gitTree("commit", "-q", "-m", "marker gone")
			fe.refreshNewSHA()
		}, nil, nil, "neither /api/health nor the install marker"},
		{"non-terminal job in flight", func(fe *fakeEnv) {
			fe.write(filepath.Join(fe.home, o, "data", "updater", "jobs", "j1.json"),
				`{"schema":1,"job_id":"j1","release_id":"r","state":"downloaded","phase":"started","attempts":1,"created_at":"`+time.Now().Format(time.RFC3339)+`","updated_at":"`+time.Now().Format(time.RFC3339)+`","transitions":[],"receipts":[]}`)
		}, nil, nil, "in flight: j1"},
		{"vl unit already loaded", func(fe *fakeEnv) {
			fe.writeState("units/vl", "[Unit]\nDescription=vl\n")
		}, nil, nil, "unit vl or vl-web is already loaded"},
		{"release dir inside install", nil, func(fe *fakeEnv) []string {
			return []string{"--session", fe.session, "--sha", fe.newSHA, "--release-dir", filepath.Join(fe.home, o, "inside"), "--dry-run"}
		}, nil, "inside the install tree"},
		{"vcs.modified true", func(fe *fakeEnv) {
			fe.writeState("stamps/vl-bin", fe.newSHA+" true")
		}, nil, nil, "vcs.modified=true"},
		{"missing bootstrap", func(fe *fakeEnv) {
			_ = os.Remove(filepath.Join(fe.relDir, "updater", "vl-updater-bootstrap"))
		}, nil, nil, "missing updater/vl-updater-bootstrap"},
		{"missing old updater config", func(fe *fakeEnv) {
			_ = os.RemoveAll(filepath.Join(fe.home, ".config", o+"-updater"))
		}, nil, nil, "trio is incomplete"},
		{"old updater with --no-updater", nil, func(fe *fakeEnv) []string {
			return append(fe.argsForward(), "--dry-run", "--no-updater")
		}, nil, "partner path"},
		{"node not found", nil, nil, []string{"VL_MIGRATE_NODE=/nonexistent"}, "node not found at /nonexistent/node"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fe := newFakeEnv(t)
			if tc.setup != nil {
				tc.setup(fe)
			}
			args := append(fe.argsForward(), "--dry-run")
			if tc.args != nil {
				args = tc.args(fe)
			}
			var out string
			var code int
			if tc.name == "no token" {
				env := fe.env()
				var filtered []string
				for _, e := range env {
					if strings.HasPrefix(e, "VL_CUTOVER_TOKEN=") || strings.HasPrefix(e, strings.ToUpper(o)+"_CUTOVER_TOKEN=") {
						continue
					}
					filtered = append(filtered, e)
				}
				wd, _ := os.Getwd()
				cmd := exec.Command("bash", append([]string{filepath.Join(wd, "migrate-to-vl.sh")}, args...)...)
				cmd.Env = filtered
				cmd.Dir = fe.home
				b, err := cmd.CombinedOutput()
				out = string(b)
				code = 0
				if ee, ok := err.(*exec.ExitError); ok {
					code = ee.ExitCode()
				} else if err != nil {
					t.Fatalf("run: %v", err)
				}
			} else {
				out, code = runWithEnv(t, fe, tc.extra, args...)
			}
			if code == 0 {
				t.Fatalf("expected refusal, got success\n%s", out)
			}
			mustContain(t, out, tc.want)
		})
	}
}

func TestNoUpdaterPartnerPath(t *testing.T) {
	o := oldName()
	fe := newFakeEnv(t)
	_ = os.RemoveAll(filepath.Join(fe.home, ".config", o+"-updater"))
	_ = os.Remove(filepath.Join(fe.home, "bin", o+"-updater"))
	_ = os.Remove(filepath.Join(fe.state, "units", o+"-updater"))
	out, code := fe.run(t, append(fe.argsForward(), "--no-updater")...)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	mustContain(t, out, "updater: n/a (--no-updater)")
	if _, err := os.Stat(filepath.Join(fe.home, "bin", "vl-updater")); !os.IsNotExist(err) {
		t.Fatalf("vl-updater must not be installed with --no-updater")
	}
}

func TestOptionalUnitsAbsent(t *testing.T) {
	o := oldName()
	fe := newFakeEnv(t)
	for _, u := range []string{o + "-backup.service", o + "-backup.timer", o + "-clock-guard.service", o + "-clock-guard.timer"} {
		_ = os.Remove(filepath.Join(fe.state, "units", u))
	}
	out, code := fe.run(t, fe.argsForward()...)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	for _, l := range fe.journal() {
		if strings.Contains(l, "stop "+o+"-backup") || strings.Contains(l, "stop "+o+"-clock-guard") {
			t.Fatalf("absent optional units were stopped: %v", fe.journal())
		}
	}
}

func TestDBBackupAbsoluteAndReceipt(t *testing.T) {
	fe := newFakeEnv(t)
	out, code := fe.run(t, fe.argsForward()...)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	// The REAL vl-activate printed the production receipt shape and the script
	// parsed it (canon 53): the step-1 line carries the numeric byte count.
	re := regexp.MustCompile(`DB backed up to .* \(bytes=([0-9]+)\)`)
	m := re.FindStringSubmatch(out)
	if m == nil || m[1] == "0" {
		t.Fatalf("no 'DB backed up … (bytes=N)' with N>0 in output\n%s", out)
	}
	// The copy under <old>-backups/pre-vl-rename-<ts>/data.db is a REAL sqlite
	// DB (integrity_check=ok was required for the run to pass).
	dests, err := filepath.Glob(filepath.Join(fe.home, "no"+"fx"+"-backups", "pre-vl-rename-*", "data.db"))
	if err != nil || len(dests) != 1 {
		t.Fatalf("backup dest glob: %v (%d matches)", err, len(dests))
	}
	db, err := sql.Open("sqlite", dests[0])
	if err != nil {
		t.Fatalf("backup does not open as sqlite: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("select count(*) from migrate_fixture").Scan(&n); err != nil || n != 3 {
		t.Fatalf("backup is not the fixture DB (count=%d err=%v)", n, err)
	}
}

// TestDBBackupReceiptOldTopLevelShapeRefused: the pre-fix top-level receipt
// shape (what the suite once printed as its fake) must now be REFUSED.
func TestDBBackupReceiptOldTopLevelShapeRefused(t *testing.T) {
	fe := newFakeEnv(t)
	fe.write(filepath.Join(fe.relDir, "vl-activate"), fakeActivateOldShape)
	out, code := fe.run(t, fe.argsForward()...)
	if code == 0 {
		t.Fatalf("expected refusal on the old top-level receipt shape, got success\n%s", out)
	}
	mustContain(t, out, "receipt is not ok:true")
	if strings.Contains(out, "step 2") {
		t.Fatalf("must not proceed past step 1 on an unknown receipt shape\n%s", out)
	}
}

// TestDBBackupReceiptNonNumericBytesRefused: a real-shaped receipt whose
// evidence.bytes is not a numeric string must be REFUSED (m4 target).
func TestDBBackupReceiptNonNumericBytesRefused(t *testing.T) {
	fe := newFakeEnv(t)
	fe.write(filepath.Join(fe.relDir, "vl-activate"), fakeActivate)
	out, code := runWithEnv(t, fe, []string{"ACTIVATE_NONNUMERIC=1"}, fe.argsForward()...)
	if code == 0 {
		t.Fatalf("expected refusal on non-numeric bytes, got success\n%s", out)
	}
	mustContain(t, out, "receipt is not ok:true")
	if strings.Contains(out, "step 2") {
		t.Fatalf("must not proceed past step 1 on non-numeric bytes\n%s", out)
	}
}

func TestVlWebRenderedWithoutPlaceholders(t *testing.T) {
	fe := newFakeEnv(t)
	out, code := fe.run(t, fe.argsForward()...)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	b, err := os.ReadFile(filepath.Join(fe.root, "etc", "systemd", "system", "vl-web.service"))
	if err != nil {
		t.Fatalf("vl-web.service not written: %v", err)
	}
	if strings.Contains(string(b), "__") {
		t.Fatalf("vl-web.service still has placeholders:\n%s", b)
	}
}

func TestDryRunChangesNothing(t *testing.T) {
	fe := newFakeEnv(t)
	before := fe.homeTreeHash()
	out, code := fe.run(t, append(fe.argsForward(), "--dry-run")...)
	if code != 0 {
		t.Fatalf("dry-run exit %d\n%s", code, out)
	}
	mustContain(t, out, "would sudo -v")
	mustContain(t, out, "DRY RUN PASSED")
	after := fe.homeTreeHash()
	if before != after {
		t.Fatalf("dry-run changed the HOME tree")
	}
	if _, err := os.Stat(filepath.Join(fe.home, ".local", "state", "vl-migrate", fe.session)); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote the state file")
	}
}

func TestDryRunFailsOnRefusal(t *testing.T) {
	fe := newFakeEnv(t)
	fe.writeGate(`{"ready":false,"job_id":"g","legs":[{"name":"trader_cutover:open_positions","pass":true},{"name":"ledger_exposure","pass":true},{"name":"planner_in_flight","pass":true},{"name":"traders_nt8","pass":false}]}`)
	out, code := fe.run(t, append(fe.argsForward(), "--dry-run")...)
	if code == 0 {
		t.Fatalf("dry-run must exit non-zero on a refusal\n%s", out)
	}
	mustContain(t, out, "failing installation-gate legs")
}

func TestNT8Legs(t *testing.T) {
	base := `"ready":false,"job_id":"g","legs":[{"name":"trader_cutover:open_positions","pass":true},{"name":"ledger_exposure","pass":true},{"name":"planner_in_flight","pass":true},{"name":"traders_nt8","pass":true}]`
	cases := []struct {
		name     string
		gate     string
		botExtra string
		want     string
		notWant  []string
	}{
		{
			name:    "absent key prints n/a no wait",
			gate:    `{` + base + `}`,
			want:    "NT8: n/a (no NT8 wire)",
			notWant: []string{"NT8: not seen in", "NT8: hello seen"},
		},
		{
			name:    "eligible true prints n/a no wait",
			gate:    `{"nt8_absent":{"eligible":true,"ready":false,"build_id":"b1"},` + base + `}`,
			want:    "NT8: n/a (eligible — link down long enough; no wait)",
			notWant: []string{"NT8: not seen in", "NT8: hello seen"},
		},
		{
			name:     "eligible false waits for the hello",
			gate:     `{"nt8_absent":{"eligible":false,"ready":false,"build_id":"b2"},` + base + `}`,
			botExtra: "BOT_HELLO=1",
			want:     "NT8: hello seen (build=b2)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fe := newFakeEnv(t)
			fe.writeGate(tc.gate)
			out, code := runWithEnv(t, fe, []string{tc.botExtra}, fe.argsForward()...)
			if code != 0 {
				t.Fatalf("exit %d\n%s", code, out)
			}
			mustContain(t, out, tc.want)
			for _, nw := range tc.notWant {
				mustNotContain(t, out, nw)
			}
			// No wall-clock bound: item 2 (real vl-activate backup) and item 3
			// (wait_leg's 2 s first-probe cost) legitimately lengthen the WHOLE
			// forward run. What "no wait" means is measured on the NT8 leg's own
			// output — the want/notWant assertions above.
		})
	}
}

func TestReleaseRootSymlinkRefusedAtStep2(t *testing.T) {
	// The release-root invariant is re-checked AFTER the move (step 2). A
	// release dir reached through a symlink passes step 0 but must refuse at
	// step 2 and roll back — this is the mutant "skip the step-2 re-check".
	fe := newFakeEnv(t)
	real := filepath.Join(fe.root, "realrel")
	if err := os.Rename(fe.relDir, real); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, fe.relDir); err != nil {
		t.Fatal(err)
	}
	out, code := runWithEnv(t, fe, nil,
		"--session", fe.session, "--sha", fe.newSHA, "--release-dir", fe.relDir)
	if code == 0 {
		t.Fatalf("expected refusal at step 2, got success\n%s", out)
	}
	mustContain(t, out, "symlink anywhere in it is refused")
	mustContain(t, out, "rollback DONE")
}

func TestNoOldNameLiteralInMyFiles(t *testing.T) {
	o := oldName()
	wd, _ := os.Getwd()
	root := filepath.Join(wd, "..")
	for _, rel := range []string{
		"deploy/migrate-to-vl.sh",
		filepath.Join("docs", "superpowers", "runbooks", "2026-09-30-vl-rename-boot.md"),
		"deploy/migrate_to_vl_test.go",
	} {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		if strings.Contains(strings.ToLower(string(b)), o) {
			t.Fatalf("%s holds the pre-rename token in some casing — the census law forbids it", rel)
		}
	}
}

func TestRecoveryNeededJobListedNotRefused(t *testing.T) {
	o := oldName()
	fe := newFakeEnv(t)
	fe.write(filepath.Join(fe.home, o, "data", "updater", "jobs", "r1.json"),
		`{"schema":1,"job_id":"r1","release_id":"r","state":"recovery_needed","phase":"started","attempts":1,"created_at":"`+time.Now().Format(time.RFC3339)+`","updated_at":"`+time.Now().Format(time.RFC3339)+`","transitions":[],"receipts":[]}`)
	out, code := fe.run(t, append(fe.argsForward(), "--dry-run")...)
	if code != 0 {
		t.Fatalf("recovery_needed must not refuse (exit %d)\n%s", code, out)
	}
	mustContain(t, out, "recovery_needed (listed, left alone)")
}

// TestGateChangedAfterStep0RefusesBeforeTheSystemStop (6a/RA1): the gate
// answers READY at step 0 and NOT ready on the step-1 re-read — the run must
// refuse BEFORE the system stop (the bot keeps trading until that stop).
func TestGateChangedAfterStep0RefusesBeforeTheSystemStop(t *testing.T) {
	o := oldName()
	fe := newFakeEnv(t)
	fe.writeGateDefault()
	fe.writeState("gate2.json", `{
  "ready": false,
  "job_id": "gate-2",
  "legs": [
    {"name": "trader_cutover:open_positions", "pass": true},
    {"name": "ledger_exposure", "pass": true},
    {"name": "planner_in_flight", "pass": false},
    {"name": "traders_nt8", "pass": true}
  ]
}`)
	out, code := runWithEnv(t, fe, []string{"GATE_PLANNER_FLIP=1"}, fe.argsForward()...)
	if code == 0 {
		t.Fatalf("expected refusal, got success\n%s", out)
	}
	mustContain(t, out, "gate changed since step 0")
	mustContain(t, out, "automatic rollback")
	mustContain(t, out, "rollback DONE")
	for _, l := range fe.journal() {
		if l == "stop "+o || l == "stop "+o+"-web" {
			t.Fatalf("the system units must never be stopped after the gate flipped\n%v", fe.journal())
		}
	}
	if _, err := os.Lstat(filepath.Join(fe.home, o)); err != nil {
		t.Fatalf("old tree missing: %v", err)
	} else if fi, _ := os.Lstat(filepath.Join(fe.home, o)); fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("old tree is a symlink — the move must not have happened")
	}
}

// TestUpdaterJobAppearingAfterStep0RefusesBeforeTheSystemStop (6b/RA2): the old
// worker creates a job in the instant before it stops — the step-1 re-scan must
// refuse before the system stop.
func TestUpdaterJobAppearingAfterStep0RefusesBeforeTheSystemStop(t *testing.T) {
	o := oldName()
	fe := newFakeEnv(t)
	out, code := runWithEnv(t, fe, []string{"JOB_APPEARS=1"}, fe.argsForward()...)
	if code == 0 {
		t.Fatalf("expected refusal, got success\n%s", out)
	}
	mustContain(t, out, "updater job appeared since step 0: late-job")
	mustContain(t, out, "automatic rollback")
	mustContain(t, out, "rollback DONE")
	for _, l := range fe.journal() {
		if l == "stop "+o || l == "stop "+o+"-web" {
			t.Fatalf("the system units must never be stopped after the job appeared\n%v", fe.journal())
		}
	}
}

// TestForwardDelayedBootReachesDoneWithoutRollback: the bot writes its boot
// line and health only after a delay — the verify poll must wait, then DONE,
// with NO rollback (B2).
func TestForwardDelayedBootReachesDoneWithoutRollback(t *testing.T) {
	fe := newFakeEnv(t)
	out, code := runWithEnv(t, fe, []string{"BOT_DELAY_S=4", "VL_MIGRATE_VERIFY_WAIT_S=20"}, fe.argsForward()...)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	mustContain(t, out, "boot line OK after")
	mustContain(t, out, "DONE — the box runs vl")
	if strings.Contains(out, "automatic rollback") {
		t.Fatalf("a boot that comes up in seconds must not roll back\n%s", out)
	}
}

// TestForwardBootLinePassesWithLogMtimeEqualToTheStepMark (JOB 8): the CTO's
// two-clock probe found ~0.42% of writes get an mtime in the PREVIOUS second
// relative to a realtime read. Deterministic fixture: the fake bot rewrites the
// step-3 mark with realtime+2 and pins the log's mtime to the mark's mtime.
// File-clock vs file-clock compares equal and MUST pass; the pre-item-8
// realtime epoch (mutant m8: cat the mark's content) compares strictly later
// and MUST fail.
func TestForwardBootLinePassesWithLogMtimeEqualToTheStepMark(t *testing.T) {
	fe := newFakeEnv(t)
	mark := filepath.Join(t.TempDir(), "step3mark")
	out, code := runWithEnv(t, fe, []string{
		"VL_MIGRATE_STEP3_MARK=" + mark,
		"BOT_SKEW_MARK=" + mark,
	}, fe.argsForward()...)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	mustContain(t, out, "boot line OK after")
	mustNotContain(t, out, "BOOT INTEGRITY leg FAIL")
}

// TestRollbackDelayedOldBootSaysDone: after a failed verify the rollback's old
// bot comes up only after a delay — the rollback poll must wait and report
// "rollback DONE" (B3).
func TestRollbackDelayedOldBootSaysDone(t *testing.T) {
	fe := newFakeEnv(t)
	// Shrink the real-clock waits: the pin is that the rollback poll WAITS for a
	// delayed old boot (B3), which holds at any poll interval. 1 s poll × 2 s
	// deadline × 1 s boot delay keeps the poll's first probe a miss and the
	// second a hit, so the wait is still genuinely exercised.
	out, code := runWithEnv(t, fe, []string{
		"BOT_SHA12=" + strings.Repeat("c", 12),
		"BOT_DELAY_S=1",
		"VL_MIGRATE_VERIFY_WAIT_S=2",
		"VL_MIGRATE_POLL_S=1",
	}, fe.argsForward()...)
	if code == 0 {
		t.Fatalf("expected verify failure, got success\n%s", out)
	}
	mustContain(t, out, "automatic rollback")
	// B3 pin: the rollback poll must WAIT for the delayed old boot, then DONE.
	mustContain(t, out, "rollback: OLD boot line OK after")
	mustContain(t, out, "rollback DONE")
}

// migrateScript returns the migrate-to-vl.sh source (the test runs from the
// deploy package dir).
func migrateScript(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("migrate-to-vl.sh")
	if err != nil {
		t.Fatalf("read migrate-to-vl.sh: %v", err)
	}
	return string(b)
}

// extractShellFunc pulls one shell function body (from the line holding
// `name()` to its closing `}`) VERBATIM out of the migrate script, so the seam
// pins below run the production bytes — not a re-typed copy.
func extractShellFunc(t *testing.T, src, name string) string {
	t.Helper()
	lines := strings.Split(src, "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), name+"()") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("function %s() not found in migrate-to-vl.sh", name)
	}
	for end := start; end < len(lines); end++ {
		if strings.TrimSpace(lines[end]) == "}" {
			return strings.Join(lines[start:end+1], "\n")
		}
	}
	t.Fatalf("function %s() has no closing brace", name)
	return ""
}

// envWithout drops every VL_MIGRATE_POLL_S=… entry so "unset" is truly absent.
func envWithout(key string) []string {
	var out []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, key+"=") {
			out = append(out, e)
		}
	}
	return out
}

// TestPollSecsSeamDefaultsAndFallsBack pins the two VL_MIGRATE_POLL_S guards
// (CTO pre-PR check): (a) unset → the shipped 2 s poll; (b) a non-numeric or
// ≤0 value → the same 2 s default, never 0 (a 0-interval poll would busy-loop
// wait_leg). A positive integer is honoured. The poll_secs body is extracted
// from the script so this runs the production bytes.
func TestPollSecsSeamDefaultsAndFallsBack(t *testing.T) {
	fn := extractShellFunc(t, migrateScript(t), "poll_secs")
	poll := func(envVal string, unset bool) string {
		t.Helper()
		cmd := exec.Command("bash", "-c", fn+"\nprintf '%s' \"$(poll_secs)\"")
		if unset {
			cmd.Env = envWithout("VL_MIGRATE_POLL_S")
		} else {
			cmd.Env = append(envWithout("VL_MIGRATE_POLL_S"), "VL_MIGRATE_POLL_S="+envVal)
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("poll_secs probe (env=%q unset=%v) failed: %v\n%s", envVal, unset, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	// (a) unset → the shipped default (byte-identical 2 s poll).
	if got := poll("", true); got != "2" {
		t.Fatalf("unset VL_MIGRATE_POLL_S must poll 2 s, got %q", got)
	}
	// (b) non-numeric or ≤0 → default 2, never 0 / a busy loop.
	for _, bad := range []string{"", "abc", "0", "00", "-1", "1.5", "2s", " "} {
		if got := poll(bad, false); got != "2" {
			t.Fatalf("VL_MIGRATE_POLL_S=%q must fall back to 2, got %q", bad, got)
		}
	}
	// a positive integer is honoured (the test seam actually shrinks the poll).
	if got := poll("1", false); got != "1" {
		t.Fatalf("VL_MIGRATE_POLL_S=1 must poll 1 s, got %q", got)
	}
	if got := poll("7", false); got != "7" {
		t.Fatalf("VL_MIGRATE_POLL_S=7 must poll 7 s, got %q", got)
	}
}
