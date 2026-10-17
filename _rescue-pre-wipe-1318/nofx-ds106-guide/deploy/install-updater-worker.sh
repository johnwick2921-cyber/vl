#!/usr/bin/env bash
# UPDATER-USABLE-V1 — install the updater worker as a systemd --user unit.
#
# What this does, in order (every refusal is final and writes nothing):
#   1. The source must be a NAMED, CLEAN sha: 40 hex. The binary is built
#      from that exact commit in a THROWAWAY clone — the live install
#      (~/vl) is never checked out, never modified, never built over.
#   2. %h/.config/vl-updater/env (mode 0600) must exist and set
#      VL_RELEASE_DIR (absolute, OUTSIDE the install — the same containment
#      the worker enforces, internal/updaterworker/releaseroot.go) and
#      VL_CUTOVER_TOKEN (non-empty; never printed here).
#   3. go build -trimpath, then `go version -m` must report vcs.modified=false
#      and vcs.revision=<sha> — a dirty or mis-labelled build is refused.
#   4. The unit template at THAT sha is copied into ~/.config/systemd/user/
#      and systemctl --user daemon-reload + enable run. The worker is NOT
#      started here: start it attended (systemctl --user start vl-updater).
#      serve refuses root, the bot's cgroup, and a set TZ — this unit avoids
#      all three by construction.
#
# The token: VL_CUTOVER_TOKEN is the cutover-worker credential that
# `vl-updater-bootstrap --install-dir <bot> enroll <owner-email>` writes into
# this file (P-E): a long-lived machine token the API admits ONLY on
# GET /api/maintenance and GET /api/installation-gate (auth.ScopeCutoverWorker).
# Never hand-mint a gate-jwt here; never print the token (pinned: the deploy
# test refuses a run whose output carries the token value).
#
# No privilege escalation anywhere in this script. Never run while the bot
# serves a hold — this script only writes ~/bin and ~/.config/systemd/user,
# but an install needs the worker and the bot quiet in the right order (see
# the runbook).
set -uo pipefail

SHA="${1:-}"
# Shell twins VL_ → NOFX_ → default; the install default is $HOME/vl
# (plan v7 FINAL D2 item 7 :34). The repo URL STAYS the pre-rename owner repo
# until the R4 commit (Z14). R5 removes the NOFX twins.
REPO_URL="${VL_UPDATER_BUILD_REPO:-${NOFX_UPDATER_BUILD_REPO:-https://github.com/johnwick2921-cyber/nofx}}"
INSTALL_DIR="${VL_UPDATER_INSTALL_DIR:-${NOFX_UPDATER_INSTALL_DIR:-$HOME/vl}}"

usage() {
  echo "usage: install-updater-worker.sh <40-hex sha>" >&2
  echo "  builds ~/bin/vl-updater from that exact commit and installs the systemd --user unit" >&2
  echo "  env: VL_/NOFX_UPDATER_BUILD_REPO (default $REPO_URL), VL_/NOFX_UPDATER_INSTALL_DIR (default \$HOME/vl)" >&2
}
[ -n "$SHA" ] || { usage; exit 2; }
printf '%s' "$SHA" | grep -Eqx '[0-9a-f]{40}' || {
  echo "install-updater-worker: REFUSED — the source must be a named 40-hex sha (got: $SHA)" >&2
  exit 2
}
[ -n "${HOME:-}" ] || { echo "install-updater-worker: REFUSED — no HOME" >&2; exit 2; }

ENV_FILE="$HOME/.config/vl-updater/env"
[ -f "$ENV_FILE" ] || {
  echo "install-updater-worker: REFUSED — $ENV_FILE does not exist." >&2
  echo "  create it (mode 0600, owner you) with exactly two lines (VL_ names win):" >&2
  echo "    VL_RELEASE_DIR=/absolute/path/outside/vl   (or the pre-rename key — R5 removes)" >&2
  echo "    VL_CUTOVER_TOKEN=<written by: vl-updater-bootstrap --install-dir <bot> enroll <owner-email>>" >&2
  exit 2
}
[ "$(stat -c '%a' "$ENV_FILE" 2>/dev/null)" = "600" ] || {
  echo "install-updater-worker: REFUSED — $ENV_FILE must be mode 0600" >&2
  exit 2
}

# Read the two required values WITHOUT printing them. The VL_ key wins when
# non-empty; the pre-rename key is the fallback (R5 removes it).
release_dir="$(awk -F= '$1=="VL_RELEASE_DIR"{print $2}' "$ENV_FILE" | tail -1)"
[ -n "$release_dir" ] || release_dir="$(awk -F= '$1=="NOFX_RELEASE_DIR"{print $2}' "$ENV_FILE" | tail -1)"
token_ok=no
grep -Eq '^(VL|NOFX)_CUTOVER_TOKEN=.+' "$ENV_FILE" && token_ok=yes
{ [ -n "$release_dir" ] && [ "$token_ok" = "yes" ]; } || {
  echo "install-updater-worker: REFUSED — $ENV_FILE must set VL_RELEASE_DIR/NOFX_RELEASE_DIR and VL_CUTOVER_TOKEN/NOFX_CUTOVER_TOKEN (both non-empty)" >&2
  exit 2
}
case "$release_dir" in
  /*) : ;;
  *) echo "install-updater-worker: REFUSED — VL_RELEASE_DIR must be an absolute path" >&2; exit 2 ;;
esac
# The containment check, on resolved paths, element-wise ("$install"/*) —
# never a string prefix (releaseroot.go's PathWithin is the worker's own one;
# this is the install-time mirror).
real_root="$(realpath -m "$release_dir" 2>/dev/null || echo "$release_dir")"
real_install="$(realpath -m "$INSTALL_DIR" 2>/dev/null || echo "$INSTALL_DIR")"
case "$real_root" in
  "$real_install"|"$real_install"/*)
    echo "install-updater-worker: REFUSED — VL_RELEASE_DIR/NOFX_RELEASE_DIR ($release_dir) must be OUTSIDE the install ($INSTALL_DIR)" >&2
    exit 2 ;;
esac
echo "install-updater-worker: env ok (release dir outside the install; token present, not shown)"
echo "install-updater-worker: note — VL_/NOFX_CUTOVER_TOKEN is the cutover-worker credential enroll writes (auth.ScopeCutoverWorker); never a hand-minted gate-jwt"a hand-minted gate-jwt"ended install window — no longer-lived token type exists"

BUILD_DIR="$(mktemp -d /tmp/vl-updater-build.XXXXXX)" || { echo "install-updater-worker: REFUSED — cannot make a build dir" >&2; exit 2; }
trap 'rm -rf "$BUILD_DIR"' EXIT
echo "install-updater-worker: cloning $REPO_URL (the live install is never touched)"
git clone -q "$REPO_URL" "$BUILD_DIR/src" 2>/dev/null || { echo "install-updater-worker: REFUSED — clone failed ($REPO_URL)" >&2; exit 2; }
git -C "$BUILD_DIR/src" checkout -q --detach "$SHA" 2>/dev/null || { echo "install-updater-worker: REFUSED — $SHA is not in $REPO_URL" >&2; exit 2; }
[ -z "$(git -C "$BUILD_DIR/src" status --porcelain)" ] || { echo "install-updater-worker: REFUSED — checkout not clean" >&2; exit 2; }
( cd "$BUILD_DIR/src" && go build -trimpath -o "$BUILD_DIR/vl-updater" ./cmd/vl-updater ) || {
  echo "install-updater-worker: REFUSED — build failed at $SHA" >&2
  exit 2
}
go version -m "$BUILD_DIR/vl-updater" > "$BUILD_DIR/vcs.txt" 2>/dev/null || { echo "install-updater-worker: REFUSED — cannot read the build stamp" >&2; exit 2; }
grep -q 'vcs.modified=false' "$BUILD_DIR/vcs.txt" || { echo "install-updater-worker: REFUSED — vcs.modified is not false" >&2; exit 2; }
grep -q "vcs.revision=$SHA" "$BUILD_DIR/vcs.txt" || { echo "install-updater-worker: REFUSED — the binary does not carry the named sha" >&2; exit 2; }

mkdir -p "$HOME/bin"
install -m 0755 "$BUILD_DIR/vl-updater" "$HOME/bin/vl-updater" || { echo "install-updater-worker: REFUSED — cannot install $HOME/bin/vl-updater" >&2; exit 2; }
mkdir -p "$HOME/.config/systemd/user"
cp "$BUILD_DIR/src/deploy/systemd-user/vl-updater.service" "$HOME/.config/systemd/user/vl-updater.service"
if command -v systemctl >/dev/null 2>&1 && systemctl --user daemon-reload 2>/dev/null; then
  if systemctl --user enable vl-updater >/dev/null 2>&1; then
    echo "install-updater-worker: unit installed and enabled; start it attended: systemctl --user start vl-updater"
  else
    echo "install-updater-worker: unit installed (enable skipped); start it attended: systemctl --user start vl-updater"
  fi
else
  echo "install-updater-worker: note — systemctl unavailable; the unit file is installed at $HOME/.config/systemd/user/vl-updater.service"
fi
echo "install-updater-worker: DONE — $HOME/bin/vl-updater from $SHA (clean, stamped); unit installed"
