# VL friend hosting — owner one-time setup (B6 steps 1–3)

One-time, ~20 minutes, all on your side. After this, adding/removing a friend is
one click on the **Friends** page (or `provision.sh`). No secrets are written in
this file — you type them into your own files and never into chat.

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

Put the token where the owner bot reads it (the owner types it; no lane ever sees
it). The box's Tailscale address (`tailscale ip -4`) goes into **both** env
files — the bot reads it, and so does the updater:

```sh
# 1) the bot's own env
echo 'TAILSCALE_API_KEY=<paste the token here>' >> /home/hoang/vl/.env
echo 'TUNNEL_IP=<box tailscale address>'       >> /home/hoang/vl/.env

# 2) the updater worker's env (already holds VL_RELEASE_DIR + VL_CUTOVER_TOKEN)
echo 'TUNNEL_IP=<box tailscale address>'       >> /home/hoang/.config/vl-updater/env
```

Why both: the bot probes each friend's HTTP API at `http://<TUNNEL_IP>:<web_port>`
(`api/handler_friends.go`), and the updater uses it as the friend host
(`cmd/vl-updater/main.go:191` `FriendHost`). **Do not restart `vl` by hand** —
these values load when you press **Update**.

Verify: `tailscale status` from WSL lists your box.

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
Replace the file's `tagOwners` and `acls` with the shape below. Add one `accept`
entry per friend; a friend's device is tagged `tag:friend-<name>` and may reach
**only** `tag:owner-box` on **its own two ports**.

```json
{
  "tagOwners": {
    "tag:owner-box": ["autogroup:admin"],
    "tag:friend":    ["autogroup:admin"]
  },
  "acls": [
    {
      "action": "accept",
      "src": ["tag:friend-tom"],
      "dst": ["tag:owner-box:18081", "tag:owner-box:36981"]
    },
    {
      "action": "accept",
      "src": ["tag:friend-alice"],
      "dst": ["tag:owner-box:18082", "tag:owner-box:36982"]
    }
  ]
}
```

Rules to keep when you edit:

- **One `accept` line per friend**, that friend's tag → `tag:owner-box:<web>,<nt>`.
- **Nothing else** — no catch-all `src: ["*"]`, no `dst` that isn't `tag:owner-box`
  with that friend's two ports.

**The friend's auth key (`TS_AUTHKEY`) is distinct from the API token (§1).**
Create it once per friend (or reuse one tagged key) at
https://login.tailscale.com/admin/settings/keys → **Generate auth key**, tagged
`tag:friend`. Pass it to provisioning, where it is folded into the **one-time
install code** (`provision.sh` `TS_AUTHKEY`); the friend's `vl-friend` setup runs
`tailscale up --authkey=…` (`cmd/vl-friend/flow.go`), so the device joins the
tailnet **pre-tagged**. The API token is only for the Friends page's
device-delete path; the auth key is only for device login — they are not
interchangeable. The owner box must carry `tag:owner-box` (tag it once in the
machines page).

**Isolation is only as strong as the WHOLE file.** The `accept` entries above
restrict *Tailscale* traffic for the listed tags, but they are additive: any
**existing** broad grant already in your ACL (for example a default `*` accept,
or a tag with a wide `dst`) still authorizes that traffic — adding these lines
does not remove it. The Hyper-V firewall rule from §3 is separate and also
additive. Before you add a friend, confirm no other line in the ACL lets that
device reach more than its two ports.

Verify (WSL, from the box):

```sh
tailscale status            # your box and the friend's PC are listed
tailscale ping <friend-pc>  # replies over the tailnet
```

---

## 5. Verify each step

| Step | How to verify |
|---|---|
| 1 API token | `curl -s "https://api.tailscale.com/api/v2/tailnet/-/devices" -u "<token>:"` returns a JSON list (HTTP 200) |
| 2 TUNNEL_IP | both `/home/hoang/vl/.env` and `/home/hoang/.config/vl-updater/env` contain `TUNNEL_IP=<box address>` |
| 3 Windows Tailscale | the PC appears at …/admin/machines (already verified — no reinstall) |
| 4 auth key | the install code carries `TS_AUTHKEY`; the friend's `vl-friend` runs `tailscale up --authkey=…` |
| 5 Hyper-V firewall | `Get-NetFirewallHyperVRule -Name "VL-friends"` shows `Action=Allow`, `LocalPorts {18081, 36981}` |
| 6 ACL | in …/admin/acls, `tailscale status` on a friend device shows the two ports reachable and **nothing else** (`nmap -p18081,36981,22,80 <box-ip>` from the friend's PC: 18081/36981 open, 22/80 closed) |

The `vl-updater` service is **already installed and enabled** — do **not** re-run
`deploy/install-autostart.sh` (the script restarts the service; re-running is not
needed and can disturb a running updater). Windows **sleep = never** should
already be set; confirm it, don't change anything else.

---

## 6. B8 known blockers (do not hide these)

- **Revoke does not actually delete the friend's Tailscale device as shipped.**
  `provision.sh` writes `tailscale_device_id: ""` and nothing populates it, so
  the Revoke API call surfaces `no_device_id` and the device remains on the
  tailnet until removed manually from the admin console. The device-delete code
  path exists (`api/handler_friends.go` `tailscaleDeleteDevice`, BasicAuth) but
  is not wired to a real device id yet.
- The friend's device tag depends on the `TS_AUTHKEY` passed at provisioning — a
  missing or untagged auth key leaves the device with the default tag, and the
  §4 ACL will not match it.
