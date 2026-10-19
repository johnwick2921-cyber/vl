# VL friend hosting — owner one-time setup (B6 steps 1–3)

One-time, ~20 minutes, all on your side. After this, adding/removing a friend is
one click on the **Friends** page (or `provision.sh`). No secrets are written in
this file — you type them into your own files and never into chat.

Ports (B2 contract): friend **N** gets web `18080+N` and NinjaTrader `36980+N`,
bound **only** to your box's Tailscale address. The ranges below cover friends
1–9.

---

## 1. Tailscale account + an API access token (full-permission static token)

You need two things from Tailscale:

1. **A Tailscale account** (free plan is enough): https://login.tailscale.com/start
2. **An API access token** so the Friends page can delete a friend's device when
   you **Revoke** it. Create it **once**:

   1. Open https://login.tailscale.com/admin/settings/keys
   2. Click **Generate API access token** (not "auth key" — that one is for
      device login, and the friend's device auth key comes from the install
      code, see §4 below).
   3. Name it `vl-friends` and copy it. It is shown **once**.

   **What this token is:** a Tailscale **API access token** from the Keys page is
   **full-permission**. That page has no scope checkboxes — the generated token
   can read and write the whole tailnet (devices, ACL, everything). The bot
   reads this static token and authenticates to the Tailscale API with HTTP
   **BasicAuth** (token as the username, empty password). In this release the
   bot uses it for exactly one thing: **deleting the friend's device on Revoke**.
   It never edits the ACL and never adds devices.

   **Do NOT** paste an OAuth / trust-credential **client secret** as this token.
   Fine-grained scopes exist only on OAuth/trust credentials, which need token
   exchange and expiry handling that the bot does not implement. No OAuth
   automation is added in this release.

Put the token where the owner bot reads it (the owner types it; no lane ever sees
it):

```sh
echo 'TAILSCALE_API_KEY=<paste the token here>' >> /home/hoang/vl/.env
```

Verify: `tailscale status` from WSL lists your box (if not, run `sudo tailscale
up`).

---

## 2. Tailscale on Windows

1. Download + run the Windows installer: https://tailscale.com/download/windows
2. Sign in with the same account as §1.
3. Confirm your Windows PC shows as a node in https://login.tailscale.com/admin/machines

Your box runs WSL in **mirrored networking** (`.wslconfig` has
`networkingMode=mirrored`), so the Windows Tailscale tunnel address reaches WSL
directly — that is why this step is on Windows.

---

## 3. The ONE admin PowerShell line (Hyper-V firewall)

WSL mirrored mode blocks inbound to WSL unless the Hyper-V firewall allows it.
Open **only** the friend ports (web 18081–18089 + NinjaTrader 36981–36989),
inbound, to WSL. Run once, in an **admin** PowerShell:

```powershell
New-NetFirewallHyperVRule -Name "VL-friends" -DisplayName "VL friend bots (web 18081-18089, NT 36981-36989)" -Direction Inbound -VMCreatorId "{40E0AC32-46A5-438A-A0B2-2B479E8F2E90}" -Protocol TCP -LocalPorts "18081-18089,36981-36989" -Action Allow
```

Reference — `New-NetFirewallHyperVRule` (PowerShell `NetSecurity` module):
https://learn.microsoft.com/en-us/powershell/module/netsecurity/new-netfirewallhypervrule

That one line **adds** an inbound allow for exactly those ports and nothing else.
It does **not** revoke or narrow any other rule already on your box — if some
other rule already allows inbound to WSL, that grant still applies. The
Tailscale-side restriction is §4; this firewall rule is not it.

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

**How the friend's device gets its tag:** the install code carries a Tailscale
**auth key** (`TS_AUTHKEY`, folded in at `provision.sh` time); the friend's
`vl-friend` setup runs `tailscale up --authkey=…`, so the device joins the
tailnet pre-tagged. The owner box must carry `tag:owner-box` (tag it once in the
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
| 2 Windows Tailscale | the PC appears at …/admin/machines |
| 3 Hyper-V firewall | `Get-NetFirewallHyperVRule -Name "VL-friends"` shows `Action=Allow`, ports `18081-18089,36981-36989` |
| 4 ACL | in …/admin/acls, `tailscale status` on a friend device shows the two ports reachable and **nothing else** (`nmap -p18081,36981,22,80 <box-ip>` from the friend's PC: 18081/36981 open, 22/80 closed) |

After all five pass: run the autostart step once (`sudo bash deploy/install-autostart.sh`)
and set Windows **sleep = never** — then the box keeps serving friends after a reboot.
