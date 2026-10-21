#!/usr/bin/env bash
# provision_test.sh — pins for deploy/friends/provision.sh + remove.sh
# (W-FRIEND-HOSTED, segment 1). Drives the PRODUCTION scripts on a /tmp root.
#
# Fixtures:
#   F1 valid name provisions (folder, .env 0600, no owner values, registry)
#   F2 invalid names refused
#   F3 port collision refused (a fake ss reports the port busy)
#   F4 a second provision of the same name refused
#   F5 remove.sh MOVES to trash (never deletes) and marks revoked
#   F6 the install code round-trips: right fields, key matches .env, only the
#      key's SHA-256 is in friends.json
set -uo pipefail

PROVISION_SH="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/provision.sh"
REMOVE_SH="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/remove.sh"

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL %s\n     %s\n' "$1" "${2:-}"; }
check(){ if [ "$2" = "$3" ]; then ok "$1"; else bad "$1" "want [$3] got [$2]"; fi; }
has()  { case "$2" in *"$3"*) ok "$1";; *) bad "$1" "missing [$3] in: $2";; esac; }
hasnt(){ case "$2" in *"$3"*) bad "$1" "found [$3] in: $2";; *) ok "$1";; esac; }

[ -f "$PROVISION_SH" ] || { printf 'FAIL: %s missing\n' "$PROVISION_SH"; exit 1; }
[ -f "$REMOVE_SH" ] || { printf 'FAIL: %s missing\n' "$REMOVE_SH"; exit 1; }

WORK="$(mktemp -d -p /tmp)"
ROOT="$WORK/root"
trap 'rm -rf "${WORK:?}"' EXIT

# A fake `ss` so port checks are deterministic (PATH-scoped; provision.sh calls
# `ss -ltn` by name).
mkdir -p "$WORK/bin"
cat > "$WORK/bin/ss" <<'EOF'
#!/usr/bin/env bash
[ -n "${FAKE_SS_BUSY:-}" ] && echo "LISTEN 0 128 100.64.0.7:${FAKE_SS_BUSY} 0.0.0.0:*"
exit 0
EOF
chmod +x "$WORK/bin/ss"
export PATH="$WORK/bin:$PATH"

provision() { # provision <name> -> rc; stdout(install code) in $WORK/out, stderr in $WORK/err
  TUNNEL_IP=100.64.0.7 "$PROVISION_SH" "$1" --root "$ROOT" > "$WORK/out" 2> "$WORK/err"
  echo $?
}
remove() {    # remove <name> -> rc
  TRASH_DIR="$WORK/trash" "$REMOVE_SH" "$1" --root "$ROOT" > "$WORK/out" 2> "$WORK/err"
  echo $?
}

echo "== F1: valid name provisions =="
RC=$(provision alice)
CODE="$(cat "$WORK/out")"
check "F1 rc is 0" "$RC" "0"
[ -d "$ROOT/alice/data" ] && ok "F1 data/ exists" || bad "F1 data/ exists" "missing $ROOT/alice/data"
check "F1 .env mode is 600" "$(stat -c '%a' "$ROOT/alice/.env" 2>/dev/null)" "600"
ENV="$(cat "$ROOT/alice/.env")"
has    "F1 .env JWT_SECRET set"        "$ENV" "JWT_SECRET="
has    "F1 .env NT_CONNECT_KEY set"    "$ENV" "NT_CONNECT_KEY="
has    "F1 .env DATA_ENCRYPTION_KEY set" "$ENV" "DATA_ENCRYPTION_KEY="
has    "F1 .env RSA_PRIVATE_KEY set"   "$ENV" "RSA_PRIVATE_KEY="
has    "F1 .env web port 18081"        "$ENV" "API_SERVER_PORT=18081"
has    "F1 .env host = tunnel IP"      "$ENV" "API_SERVER_HOST=100.64.0.7"
has    "F1 .env nt bind tunnel:36981"  "$ENV" "NT_TCP_LISTEN_ADDR=100.64.0.7:36981"
has    "F1 .env friend mode on"        "$ENV" "VL_FRIEND_INSTANCE=1"
has    "F1 .env friend name"           "$ENV" "VL_FRIEND_NAME=alice"
hasnt  "F1 no insecure default JWT"    "$ENV" "default-jwt-secret-change-in-production"
for owner_key in TELEGRAM_BOT_TOKEN ALPACA_API_KEY ALPACA_SECRET_KEY TWELVEDATA_API_KEY DATABENTO_API_KEY TAILSCALE_API_KEY; do
  hasnt "F1 no owner key $owner_key" "$ENV" "$owner_key"
done
has    "F1 symlink web/dist"           "$(readlink "$ROOT/alice/web/dist")" "/home/hoang/vl/web/dist"
has    "F1 symlink deploy/RELEASE"     "$(readlink "$ROOT/alice/deploy/RELEASE")" "/home/hoang/vl/deploy/RELEASE"

echo "== F2: invalid names refused =="
for bad_name in "Tom" "t" "toolongname1234567890" "to m" "tom!" ".." "." "a/b"; do
  RC=$(provision "$bad_name")
  check "F2 refuses '$bad_name'" "$([ "$RC" -ne 0 ] && echo nonzero || echo zero)" "nonzero"
done
RC=$(provision "")
check "F2 refuses empty name" "$([ "$RC" -ne 0 ] && echo nonzero || echo zero)" "nonzero"

echo "== F3: port collision refused =="
export FAKE_SS_BUSY=18082
RC=$(provision bob)  # alice holds N=1 → bob wants web 18082
unset FAKE_SS_BUSY
check "F3 rc nonzero" "$([ "$RC" -ne 0 ] && echo nonzero || echo zero)" "nonzero"
has   "F3 names the busy port" "$(cat "$WORK/err")" "in use"

echo "== F4: second provision of the same name refused =="
RC=$(provision alice)
check "F4 rc nonzero" "$([ "$RC" -ne 0 ] && echo nonzero || echo zero)" "nonzero"
has   "F4 names the existing friend" "$(cat "$WORK/err")" "already"

echo "== F5: remove.sh moves, never deletes =="
RC=$(remove alice)
check "F5 rc is 0" "$RC" "0"
[ ! -e "$ROOT/alice" ] && ok "F5 folder left the root" || bad "F5 folder left the root" "$ROOT/alice still present"
[ -d "$WORK/trash/friend-alice" ] && ok "F5 moved to trash" || bad "F5 moved to trash" "no $WORK/trash/friend-alice"
[ -f "$WORK/trash/friend-alice/.env" ] && ok "F5 .env survives the move" || bad "F5 .env survives the move" "missing .env in trash"
has "F5 registry state revoked" "$(cat "$ROOT/friends.json")" '"state": "revoked"'

echo "== F6: install code round-trips =="
# fresh friend — alice's revoked entry still holds N=1, so carol gets N=2
RC=$(provision carol)
CODE="$(cat "$WORK/out")"
check "F6 rc is 0" "$RC" "0"
WEBPORT="$(grep '^API_SERVER_PORT=' "$ROOT/carol/.env" | cut -d= -f2)"
NTPORT="$(grep '^NT_TCP_LISTEN_ADDR=' "$ROOT/carol/.env" | cut -d= -f2 | sed 's/.*://')"
check "F6 carol web port 18082" "$WEBPORT" "18082"
check "F6 carol nt port 36982"  "$NTPORT"  "36982"
DECODED="$(python3 - "$CODE" <<'PY'
import base64, json, sys
c = sys.argv[1] + "=" * (-len(sys.argv[1]) % 4)
print(json.dumps(json.loads(base64.urlsafe_b64decode(c))))
PY
)"
KEY="$(grep '^NT_CONNECT_KEY=' "$ROOT/carol/.env" | cut -d= -f2)"
has    "F6 code name"       "$DECODED" '"name": "carol"'
has    "F6 code host"       "$DECODED" '"host": "100.64.0.7"'
has    "F6 code nt_port matches .env"  "$DECODED" "\"nt_port\": $NTPORT"
has    "F6 code web_port matches .env" "$DECODED" "\"web_port\": $WEBPORT"
has    "F6 code connect_key matches .env" "$DECODED" "\"connect_key\": \"$KEY\""
SHA="$(printf '%s' "$KEY" | sha256sum | awk '{print $1}')"
has    "F6 registry stores only sha256" "$(cat "$ROOT/friends.json")" "\"key_sha256\": \"$SHA\""
hasnt  "F6 registry never stores the clear key" "$(cat "$ROOT/friends.json")" "$KEY"

echo "== F7: a symlinked name resolves outside the root -> refused =="
ln -s /home/hoang "$ROOT/evil"
RC=$(provision evil)
check "F7 provision refuses symlinked name" "$([ "$RC" -ne 0 ] && echo nonzero || echo zero)" "nonzero"
RC=$(remove evil)
check "F7 remove refuses symlinked name"    "$([ "$RC" -ne 0 ] && echo nonzero || echo zero)" "nonzero"
has   "F7 remove names the escape" "$(cat "$WORK/err")" "outside"

echo "== F8: per-friend encryption keys valid + distinct (F2 boot gap) =="
# carol (N=2) is the first fresh provision; dave (N=3) the second.
RC=$(provision dave)
check "F8 rc is 0" "$RC" "0"
dk_of()  { sed -n 's/^DATA_ENCRYPTION_KEY=\(.*\)$/\1/p' "$ROOT/$1/.env"; }
rsa_of() { sed -n 's/^RSA_PRIVATE_KEY="\(.*\)"$/\1/p' "$ROOT/$1/.env"; }
DK_CAROL="$(dk_of carol)"; DK_DAVE="$(dk_of dave)"
RSA_CAROL="$(rsa_of carol)"; RSA_DAVE="$(rsa_of dave)"
[ -n "$DK_CAROL" ] && ok "F8 data key present (carol)" || bad "F8 data key present (carol)" "empty"
[ -n "$DK_DAVE" ]  && ok "F8 data key present (dave)"  || bad "F8 data key present (dave)"  "empty"
[ "$DK_CAROL" != "$DK_DAVE" ] && ok "F8 data keys differ" || bad "F8 data keys differ" "identical"
[ "$RSA_CAROL" != "$RSA_DAVE" ] && ok "F8 RSA keys differ" || bad "F8 RSA keys differ" "identical"
for who in carol dave; do
  n="$(printf '%s' "$(dk_of "$who")" | base64 -d 2>/dev/null | wc -c)"
  check "F8 $who data key decodes to 32 bytes" "$n" "32"
  printf '%s' "$(rsa_of "$who")" | python3 -c 'import sys; sys.stdout.write(sys.stdin.read().replace("\\n","\n"))' > "$WORK/rsa-$who.pem"
  if openssl pkey -inform PEM -noout -in "$WORK/rsa-$who.pem" >/dev/null 2>&1; then
    ok "F8 $who RSA key is valid PEM"
  else
    bad "F8 $who RSA key is valid PEM" "openssl pkey refused"
  fi
done

echo "== F9: CONNECT_KEY / TS_AUTHKEY never in python3 argv (S10b) =="
# Wrap `python3` (PATH-scoped, same dir as the ss stub) to record its argv; the
# real python3 is resolved BEFORE the wrapper shadows it. The fix must hand the
# two secrets via the environment, so the clear key / auth key must never appear
# in any helper's argv (which `ps` and /proc/<pid>/cmdline would expose).
REAL_PY3="$(command -v python3)"
PYLOG="$WORK/py-argv.log"
: > "$PYLOG"
cat > "$WORK/bin/python3" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$@" >> "$PYLOG"
exec "$REAL_PY3" "\$@"
EOF
chmod +x "$WORK/bin/python3"
export PATH="$WORK/bin:$PATH"
export TS_AUTHKEY="tskey-auth-s10b-test123456789"
RC=$(provision eve)
check "F9 rc is 0" "$RC" "0"
EVE_KEY="$(grep '^NT_CONNECT_KEY=' "$ROOT/eve/.env" | cut -d= -f2)"
ARGV_ALL="$(cat "$PYLOG")"
hasnt "F9 connect key absent from every python3 argv" "$ARGV_ALL" "$EVE_KEY"
hasnt "F9 ts_authkey absent from every python3 argv" "$ARGV_ALL" "tskey-auth-s10b-test123456789"
CODE="$(cat "$WORK/out")"
DECODED="$(python3 - "$CODE" <<'PY'
import base64, json, sys
c = sys.argv[1] + "=" * (-len(sys.argv[1]) % 4)
print(json.dumps(json.loads(base64.urlsafe_b64decode(c))))
PY
)"
has "F9 code connect_key matches .env" "$DECODED" "\"connect_key\": \"$EVE_KEY\""
has "F9 code ts_authkey matches input" "$DECODED" '"ts_authkey": "tskey-auth-s10b-test123456789"'
unset TS_AUTHKEY

printf '\n== %d pass / %d fail ==\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ] || exit 1

