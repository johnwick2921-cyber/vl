# VL friend provisioning (W-FRIEND-HOSTED)

One command creates a ready, empty VL bot for a friend. The friend's bot is the
**same program** as the owner's (`/home/hoang/vl/vl-bin`), run from its own folder
so it reads its own `.env` and database — it can never see the owner's traders,
keys or settings.

## Add a friend

```sh
TUNNEL_IP=<your box's Tailscale address> bash deploy/friends/provision.sh tom
```

`provision.sh` prints a **one-time install code** to stdout. Send it to the friend.
The code is shown once and never stored in the clear — the connect key lives only
in the friend's `.env` (mode 0600); `friends.json` stores only its SHA-256.

The friend then runs `VL-Setup.exe` (segment 8) with that code.

## What it creates

- `/home/hoang/vl-friends/tom/` — `.env` (0600), `data/`, and symlinks to the
  owner's `web/dist` and `deploy/RELEASE` (same build, always).
- `/home/hoang/vl-friends/friends.json` — the registry (name, ports, key SHA-256,
  state).
- `vl-friend@tom.service` — a **user** unit (no sudo), enabled + started.

Ports (B2): friend N gets web `18080+N` and NinjaTrader `36980+N`, bound **only**
to the tunnel IP. `.env` sets `JWT_SECRET`, `NT_CONNECT_KEY`, `API_SERVER_PORT`,
`API_SERVER_HOST`, `NT_TCP_LISTEN_ADDR`, `VL_FRIEND_INSTANCE=1`, `VL_FRIEND_NAME`.

## Remove a friend

```sh
bash deploy/friends/remove.sh tom
```

Stops the unit, **moves** the folder to `~/trash/<date>/friend-tom` (never
deletes), and marks the registry entry `revoked`.

## Friend sweep

After each install the updater restarts every active, flat friend and records `friends:[{name,state,rev,note}]`.
`TUNNEL_IP` is the box's Tailscale address (set it in `~/.config/vl-updater/env`); with `TUNNEL_IP` unset and active friends present the sweep records a synthetic `{name:"*", state:"failed", note:"friend sweep disabled: TUNNEL_IP unset"}` entry (and the same for an unreadable registry) that the owner's Telegram alert relays.

## Tests

```sh
bash deploy/friends/provision_test.sh
```

Drives the production scripts on a `/tmp` root: name validation, port collision,
`.env` mode and contents (no owner values), duplicate refusal, and remove-moves.
