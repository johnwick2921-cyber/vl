# VL friend hosting — owner one-time setup (B6 steps 1–3)

One-time, ~20 minutes, all on your side. After this, adding/removing a friend
starts from the **Friends** page (or `provision.sh`) — but it is **not** fully
one-click yet; see the B8 blockers in §6. No secrets are written in this file —
you type them into your own files and never into chat.

Ports (B2 contract): friend **N** gets web `18080+N` and NinjaTrader `36980+N`,
bound **only** to your box's Tailscale address.

**Assumptions (sourced):** the box already runs Tailscale with WSL mirrored
networking (verified); the `vl-updater` user service is already installed and
enabled. You are only adding the friend-specific values below. **Do not manually
restart `vl`** — the new values load when you press **Update**.

---

## 1. Tailscale account + an API access token (full-permission static token)

You need two things from Tailscale:

1. **A Tailscale account** (free plan is enough): https://login.tailscale.com/start
2. **An API access token**, read by the Friends page. Create it **once**:

   1. Open https://login.tailscale.com/admin/settings/keys
   2. Click **Generate API access token** (not "auth key" — that one is for
      device login, and the friend's device auth key comes from the install
      code, see §4 below).
   3. Name it `vl-friends` and copy it. It is shown **once**.

   **What this token is:** a Tailscale **API access token** from the Keys page is
   **full-permission**. That page has no scope checkboxes — the generated token
   can read and write the whole tailnet (devices, ACL, everything). The bot
   reads this static token and authenticates to the Tailscale API with HTTP
   **BasicAuth** (token as the username, empty password).
   Source: https://tailscale.com/docs/reference/tailscale-api

   **What it does as shipped:** the code has a device-delete path
   (`DELETE /api/v2/device/<id>`, `api/handler_friends.go`
   `tailscaleDeleteDevice`), but the registry's `tailscale_device_id` is **never
   populated** (`provision.sh` writes `""`), so on Revoke the bot surfaces
   `no_device_id` and **does not actually delete the device** — a known B8
   blocker (see §6). It never edits the ACL and never adds devices.

   **Do NOT** paste an OAuth / trust-credential **client secret** as this token.
   Fine-grained scopes exist only on OAuth/trust credentials, which need token
   exchange and expiry handling that the bot does not implement. No OAuth
   automation is added in this release.
   Source: https://tailscale.com/docs/reference/trust-credentials

Put the values where the owner bot reads them (the owner types them; no lane ever
sees them). Edit each file with a **private editor** — never with a shell `echo`
or a command line, so the secret never lands in shell history. **Back up the file
first** and **update the existing key in place** (do not append a duplicate line):

- `/home/hoang/vl/.env` — the bot's env. Set (or update): `TAILSCALE_API_KEY`,
  `TUNNEL_IP`, and `TS_AUTHKEY` (the auth key for the friend you are provisioning
  — see §4's boot-time limitation).
- `/home/hoang/.config/vl-updater/env` — the updater worker's env (already holds
  `VL_RELEASE_DIR` + `VL_CUTOVER_TOKEN`). Set (or update): `TUNNEL_IP`.

Why `TUNNEL_IP` in both: the bot probes each friend's HTTP API at
`http://<TUNNEL_IP>:<web_port>` (`api/handler_friends.go`), and the updater uses
it as the friend host (`cmd/vl-updater/main.go:191` `FriendHost`). **Do not
restart `vl` by hand** — these values load when you press **Update**.

Verify without exposing the token: the Friends page reports
`tailscale_key_set: true` — that only proves `TAILSCALE_API_KEY` is **non-empty**
in the bot's env, **not** that the token is valid or authorized
(`api/handler_friends.go` reports `strings.TrimSpace(os.Getenv(...)) != ""`).

---

## 2. Tailscale on Windows — already in place (verify, don't reinstall)

The box already runs the Windows Tailscale client and WSL **mirrored
networking** (`.wslconfig` has `networkingMode=mirrored`); this is verified. No
reinstall is needed — just confirm the Windows PC appears as a node at
https://login.tailscale.com/admin/machines, on the same account as §1.

---

## 3. The ONE admin PowerShell line (Hyper-V firewall)

WSL mirrored mode blocks inbound to WSL unless the Hyper-V firewall allows it.
Open **only the ports you are actually using** — start with the first friend's
two ports. `LocalPorts` is a `String[]`: give each port as its own element, not
one comma-delimited string, and do not pre-open nine friends' worth of ports.
Run once, in an **admin** PowerShell:

```powershell
New-NetFirewallHyperVRule -Name "VL-friends" -DisplayName "VL friend bots" -Direction Inbound -VMCreatorId "{40E0AC32-46A5-438A-A0B2-2B479E8F2E90}" -Protocol TCP -LocalPorts "18081","36981" -Action Allow
```

Add one more `-LocalPorts` element per friend as you add them (friend 2 →
`"18082","36982"`, and so on). Reference — `New-NetFirewallHyperVRule`
(PowerShell `NetSecurity` module):
https://learn.microsoft.com/en-us/powershell/module/netsecurity/new-netfirewallhypervrule

That line **adds** an inbound allow for exactly those ports and nothing else. It
does **not** revoke or narrow any other rule already on your box — if some other
rule already allows inbound to WSL, that grant still applies. The Tailscale-side
restriction is §4; this firewall rule is not it.

Verify (admin PowerShell):

```powershell
Get-NetFirewallHyperVRule -Name "VL-friends" | Select-Object DisplayName, Direction, Action, LocalPorts
```

---

## 4. The Tailscale ACL (each friend reaches ONLY their two ports)

Admin console → **Access Controls** (https://login.tailscale.com/admin/acls/file).
**Do not blindly overwrite the policy.** First export your current ACL from the
admin policy editor (copy it to a backed-up private file), keep every unrelated
grant, and read the whole file for broad grants that would also authorize this
friend's device (e.g. a catch-all `*` or a wide `dst`) — adjust those safely.
Then add the friend-specific entries below. The JSON is an **example** of the
entries to add, not a full policy to paste over yours.

```json
{
  "tagOwners": {
    "tag:owner-box": ["autogroup:admin"],
    "tag:friend-tom": ["autogroup:admin"]
  },
  "acls": [
    {
      "action": "accept",
      "src": ["tag:friend-tom"],
      "dst": ["tag:owner-box:18081", "tag:owner-box:36981"]
    }
  ]
}
```

This is an **example for one friend (`tom`)**. For each extra friend, declare a
matching `tag:friend-<name>` in `tagOwners` AND use that exact tag in its own
`accept` entry and its auth key — the tag must match in all three places.

Rules to keep when you edit:

- **One `accept` line per friend**, that friend's tag → `tag:owner-box:<web>,<nt>`.
- **Nothing else** — no catch-all `src: ["*"]`, no `dst` that isn't `tag:owner-box`
  with that friend's two ports.

**The friend's auth key (`TS_AUTHKEY`) is distinct from the API token (§1).**
For each friend, create **one auth key tagged exactly `tag:friend-<name>`**
(e.g. `tag:friend-tom`) at https://login.tailscale.com/admin/settings/keys →
**Generate auth key**. Do **not** reuse one tagged key across friends — the tag
must match the `tagOwners` entry and the `accept` `src` above, or the policy
will never match. Pass the key to provisioning, where it is folded into the
**one-time install code** (`provision.sh` `TS_AUTHKEY`); the friend's `vl-friend`
setup runs `tailscale up --authkey=…` (`cmd/vl-friend/flow.go`), so the device
joins the tailnet **pre-tagged**. The API token is only for the Friends page's
device-delete path; the auth key is only for device login — they are not
interchangeable. The owner box must carry `tag:owner-box` (tag it once in the
machines page).

**Current limitation (single, boot-time `TS_AUTHKEY`):** the Friends page runs
`provision.sh` with the bot's **process environment** (`cmd.Env = os.Environ()`,
`api/handler_friends.go`), so it inherits whatever `TS_AUTHKEY` the bot loaded
**at boot**. Privately editing `.env` between friends is **not hot-active** — the
running bot keeps the old value until an Update/restart. The registry stores no
per-friend auth keys (the key is embedded in the install code once). This setup
is limited to the **first named friend** until per-friend key handling is
implemented; do not share a tagged auth key between friends.
`rotate` later issues a new **connect key** but does **not** re-issue a Tailscale
auth key (see §6).

**Isolation is only as strong as the WHOLE file.** The `accept` entries above
restrict *Tailscale* traffic for the listed tags, but they are additive: any
**existing** broad grant already in your ACL (for example a default `*` accept,
or a tag with a wide `dst`) still authorizes that traffic — adding these lines
does not remove it. The Hyper-V firewall rule from §3 is separate and also
additive. Before you add a friend, confirm no other line in the ACL lets that
device reach more than its two ports.

Verify in two separate parts:

1. **Policy proof** — the admin policy editor (your exported, backed-up copy)
   shows the friend's tag has an `accept` only to `tag:owner-box:18081` / `36981`
   and no catch-all reaches it. This is the actual isolation; `tailscale status` /
   `tailscale ping` only prove reachability, not that other ports are closed.
2. **Connection probes** — from the friend's PC: the friend's web port `18081`
   and NinjaTrader port `36981` connect (positive); a **different** port (e.g.
   the owner's `8080`, or another friend's `18082`) is refused (negative). A
   sample of a few ports is not proof of "nothing else" — the policy proof is.

---

## 5. Verify each step

| Step | How to verify |
|---|---|
| 1 API token | the Friends page reports `tailscale_key_set: true` — non-empty only, not proof of valid credentials |
| 2 TUNNEL_IP | both `/home/hoang/vl/.env` and `/home/hoang/.config/vl-updater/env` contain `TUNNEL_IP=<box address>` |
| 3 Windows Tailscale | the PC appears at …/admin/machines (already verified — no reinstall) |
| 4 auth key | the install code carries `TS_AUTHKEY`; the friend's `vl-friend` runs `tailscale up --authkey=…` |
| 5 Hyper-V firewall | `Get-NetFirewallHyperVRule -Name "VL-friends"` shows `Action=Allow`, `LocalPorts {18081, 36981}` |
| 6 ACL | policy proof (admin policy editor export) + a negative probe from the friend's PC to a different port (see §4 "Verify") |

The `vl-updater` service is **already installed and enabled** — do **not** re-run
`deploy/install-autostart.sh` (the script restarts the service; re-running is not
needed and can disturb a running updater). Windows **sleep = never** should
already be set; confirm it, don't change anything else.

---

## 6. B8 known blockers (do not hide these)

- **Revoke does not delete the Tailscale device as shipped.** `provision.sh`
  writes `tailscale_device_id: ""` and nothing populates it, so Revoke surfaces
  `no_device_id` and the device stays on the tailnet until removed manually from
  the admin console. The delete code path exists (`api/handler_friends.go`
  `tailscaleDeleteDevice`, BasicAuth) but is not wired to a real device id yet.
- **Re-add after revoke is refused for the same name.** Revoke sets the registry
  entry to `state=revoked` but keeps it; `provision.sh` refuses a name already
  in `friends.json` ("friend 'X' is already in the registry"). Same-name re-add
  is **blocked pending implementation** — **never delete registry entries**;
  preserve the registry and the archived friend folder.
- **Rotate is incomplete as shipped.** Three problems, until a production-path
  fix/test lands: (a) it writes a new key to the friend's `.env`, but the running
  friend bot has the connect key **cached in its process** — a `.env` edit does
  not invalidate it, so the old key stays in force until the bot restarts; (b)
  after Revoke the friend's `.env` was **moved to trash** (`remove.sh`), so
  rotate's `.env` write fails; (c) the rotated install code does **not** carry a
  Tailscale auth key (`TSAuthKey` is not set in the rotate path), so it cannot
  re-provision a device onto the tailnet.
- The friend's device tag depends on the `TS_AUTHKEY` passed at provisioning — a
  missing or untagged auth key leaves the device with the default tag, and the
  §4 ACL will not match it.
