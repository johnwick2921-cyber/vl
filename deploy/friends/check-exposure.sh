#!/usr/bin/env bash
# check-exposure.sh — B5 S2 tooling (W-FRIEND-HOSTED, DS-106).
#
# READ-ONLY: it runs `ss -ltnp` and TCP probes. It never changes any socket,
# file or service.
#
# Two modes:
#
#   1. Bind check (default) — the OWNER box's own view. For every friend port
#      named, it reads the listening socket and FAILS (exit 1) if the port is
#      bound to the all-interfaces address (0.0.0.0 / :: / *), is not listening
#      at all, or is bound to any address OTHER than --tunnel-ip. A friend port
#      must never be reachable from the LAN; binding exactly to the tunnel IP is
#      what makes that true. --tunnel-ip is REQUIRED in bind mode (exit 2 without
#      it); owner ports (--owner-ports) are checked for wildcard only.
#
#      check-exposure.sh --tunnel-ip <tunnel ip> --ports 18081,18082 --owner-ports 8080,36974
#
#   2. Probe mode — run FROM a source host (a LAN machine, or the friend's
#      Tailscale device) to see which of the box's ports are reachable from
#      there. It prints reachable/unreachable per port and exits 0 (the
#      judgement is the OWNER-SETUP runbook's, not the script's).
#
#      check-exposure.sh --probe <host> --ports 18081 --owner-ports 8080,36974
#
# OWNER-SETUP verify step (B6 step 2, B5 S1/S2 — copy this verbatim):
#   # on the owner box (every friend port must be "ok: ... bound to <tunnel>")
#   bash deploy/friends/check-exposure.sh --tunnel-ip <tunnel ip> --ports <friend ports> --owner-ports 8080,36974
#   # from a LAN machine (every friend port must be UNREACHABLE)
#   bash deploy/friends/check-exposure.sh --probe <owner lan ip> --ports <friend ports> --owner-ports 8080,36974
#   # from the friend's Tailscale device (reachable ONLY for ITS two ports)
#   bash deploy/friends/check-exposure.sh --probe <tunnel ip> --ports <its ports> --owner-ports 8080,36974
set -euo pipefail

PORTS=""
SRC=""
MODE="bind"
OWNER_PORTS="8080,36974"
TUNNEL_IP=""

usage() {
  sed -n '2,40p' "$0" >&2
  exit 2
}

while [ $# -gt 0 ]; do
  case "$1" in
    --ports)       PORTS="${2:?check-exposure: --ports needs a comma list}"; shift 2 ;;
    --owner-ports) OWNER_PORTS="${2:?check-exposure: --owner-ports needs a comma list}"; shift 2 ;;
    --probe)       MODE="probe"; SRC="${2:?check-exposure: --probe needs a HOST}"; shift 2 ;;
    --tunnel-ip)   TUNNEL_IP="${2:?check-exposure: --tunnel-ip needs an IP}"; shift 2 ;;
    -h|--help)     usage ;;
    *)             echo "check-exposure: unknown arg $1" >&2; usage ;;
  esac
done

[ -n "$PORTS" ] || { echo "check-exposure: --ports is required" >&2; usage; }
# R9: in bind mode the tunnel IP is MANDATORY - the friend ports must bind
# EXACTLY to it; without it we cannot tell a LAN address from the tunnel one.
if [ "$MODE" = "bind" ] && [ -z "$TUNNEL_IP" ]; then
  echo "check-exposure: --tunnel-ip <ip> is required in bind mode" >&2
  exit 2
fi

# all_ports: the friend ports plus the owner ports, de-duplicated and sorted.
all_ports() {
  printf '%s %s\n' "$PORTS" "$OWNER_PORTS" \
    | tr ',' ' ' \
    | tr ' ' '\n' \
    | grep -E '^[0-9]{1,5}$' \
    | sort -un
}

# is_owner_port: true when $1 is one of --owner-ports (checked for wildcard only).
is_owner_port() {
  case ",$OWNER_PORTS," in *",$1,"*) return 0 ;; *) return 1 ;; esac
}

# bind_check reads ss for one port (EVERY line, not just the first — a port can
# be bound on several addresses). A missing listener is a FAIL; a wildcard bind
# (0.0.0.0 / :: / *) is a FAIL (S1); and for a FRIEND port (require_tunnel=1) the
# bound address must EQUAL $TUNNEL_IP exactly (U18). Owner ports are wildcard-only.
bind_check() {
  local port="$1" require_tunnel="${2:-0}" lines line addr
  lines="$(ss -ltnH "sport = :$port" 2>/dev/null)" || true
  if [ -z "$lines" ]; then
    echo "FAIL: port $port is not listening"
    return 1
  fi
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    addr="$(printf '%s' "$line" | awk '{print $4}' | sed 's/:[0-9][0-9]*$//')"
    addr="${addr#\[}"; addr="${addr%\]}"
    case "$addr" in
      0.0.0.0|::|\*)
        echo "FAIL: port $port is bound to $addr (all interfaces — reachable from the LAN)"
        return 1 ;;
    esac
    if [ "$require_tunnel" = "1" ] && [ "$addr" != "$TUNNEL_IP" ]; then
      echo "FAIL: port $port is bound to $addr, not the tunnel IP $TUNNEL_IP"
      return 1
    fi
  done <<< "$lines"
  echo "ok:   port $port bound to $addr"
  return 0
}

# probe_port TCP-connects to host:port with a short timeout and reports.
probe_port() {
  local host="$1" port="$2"
  if timeout 3 bash -c "exec 3<>/dev/tcp/$host/$port" 2>/dev/null; then
    echo "reachable:   tcp $host:$port"
  else
    echo "unreachable: tcp $host:$port"
  fi
}

FAIL=0
for p in $(all_ports); do
  if [ "$MODE" = "probe" ]; then
    probe_port "$SRC" "$p"
  elif is_owner_port "$p"; then
    bind_check "$p" 0 || FAIL=1
  else
    bind_check "$p" 1 || FAIL=1
  fi
done
exit "$FAIL"
