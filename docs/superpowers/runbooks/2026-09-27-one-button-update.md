names rewritten to vl on 2026-09-30 (VL rename)
# One-button update: operator runbook (UPDATER-USABLE-V1, 2026-09-27)

**What this is:** the owner-side procedure for the one-button update — from a
signed release to the bot running it, using only this box. The enrollment and
the `authorize` mechanics live in
`docs/superpowers/runbooks/2026-09-24-m3-update-enrollment.md`; this file is the
whole loop, first-time setup through recovery.

**Status note (reads the truth, not a hope):** while
`INSTALL_AUTHZ_UNDER_REVIEW` is `true` in `web/src/lib/api/updates.ts` (the CTO
flips it in a separate commit after his adversarial pass), the Updates page
shows "install authorization under review" and no install can be started from
the page, whatever the server answers. Everything below still works end to end
on the CLI; the button lights up the moment that constant flips and the server
answers `install_enabled: true` + `worker_listening: true`.

## 1. First-time setup (once)

1. **Release keypair** (the CTO adds the public half; you keep the private):
   generate an ed25519 keypair for releases. The allowed-signers line goes into
   `deploy/release_allowed_signers` in the repo (the CTO commits it — do not
   hand-edit it yourself); the private key goes into the GitHub Environment
   `release` as the secret `RELEASE_SIGNING_KEY`. The workflow refuses to run a
   release without both (missing file or missing secret = REFUSED).
2. **GitHub Environment `release`:** repo → Settings → Environments → `release`:
   add yourself as a REQUIRED REVIEWER. The signing key lives ONLY there.
3. **Enroll this box:** see the M3 enrollment runbook —
   `go run ./cmd/vl-updater-bootstrap --install-dir <bot folder> enroll <your exact account email>`.
4. **Turn the glue on:** `VL_UPDATER=1` (exactly 1) in the bot's environment,
   then restart the bot. Unset, nothing installs and the verifier is the stub.
5. **Worker install** (no privilege escalation anywhere):
   - create `~/.config/vl-updater/env`, mode 0600, with exactly two lines:
     `VL_RELEASE_DIR=/absolute/path/outside/vl` (never inside `~/vl` —
     the worker refuses it, and so does the installer) and
     `VL_CUTOVER_TOKEN=<a fresh gate-jwt>`.
   - run `deploy/install-updater-worker.sh <40-hex sha>` from a checkout — it
     builds `~/bin/vl-updater` from that exact commit in a throwaway clone
     (proves `vcs.modified=false` and `vcs.revision=<sha>`), installs the
     systemd --user unit, and never prints the token.
6. **Start the worker attended:** `systemctl --user start vl-updater`, then
   open Settings → Updates: once the page reads install_enabled and
   worker_listening both true (and the review constant is off), the button is
   live.

**The token clock:** `VL_CUTOVER_TOKEN` is a gate-jwt with a **24-hour**
lifetime (`auth/auth.go:227`). There is no longer-lived token type. Re-mint it
and replace the env line **before each install window**, then
`systemctl --user restart vl-updater`. An `authorize` grant is valid **5
minutes**, single use.

## 2. Every-release loop

1. **Tag + approve.** The release is cut from an APPROVED tag only
   (`release.yml` refuses anything else). Approve the `release` Environment run.
   The workflow proves the tree clean, builds the bot, the web assets, and the
   two updater binaries (`updater/vl-updater`,
   `updater/vl-updater-bootstrap`), stages the allow-list, scans for secrets,
   and signs the manifest.
2. **Download to the local inbox:** `gh release download <release_id> --dir <VL_RELEASE_INBOX>`
   — `vl-updater fetch` reads `<inbox>/<release_id>.tar.gz`; there is no
   network fetch.
3. **Fetch (attended, verifies):** `vl-updater fetch <release_id>`. It checks
   the signature and every file against the INSTALL's
   `deploy/release_allowed_signers`, materializes the release under
   `VL_RELEASE_DIR/<source_sha>`, and writes the verdict into
   `data/updater/verdicts/`. No verdict, no install (`422 release not
   verified`).
4. **Authorize (attended, single use):**
   `go run ./cmd/vl-updater-bootstrap --install-dir <bot folder> authorize <release_id>`,
   type `AUTHORIZE <release_id>`. It prints ONE JSON line —
   `{release_id, job_id, expires_at, hmac}` — valid 5 minutes, single use.
5. **Paste + Update now.** Paste that line into the Updates page box and press
   **Update now**. The exact parsed body is POSTed; the page shows the 202
   `job_id` and then the job's states/timestamps/blockers from the worker's own
   job file, and offers the receipt download when the API does. Any refusal
   (400/403/409/422/503) shows the server's own text, and the code is spent
   either way — authorize a new one.
6. **Watch.** The job runs its states until it parks at `nt8_updated`; the
   maintenance hold covers the bot meanwhile.
7. **Update the AddOn and F5 in NT8** when the job says `nt8_updated`. Copy the
   AddOn source from the RELEASE folder the job materialized — never from
   `~/vl/ninjascript/`: the activation swaps ONLY `vl-bin`, `web/dist` and
   `deploy/RELEASE`, so `~/vl/ninjascript/` still holds the previous build and
   copying it leaves the AddOn on the old build id (`match=NO` forever; the
   2026-10-04 checklist had this wrong). At the park the install has NOT been
   activated yet, so `deploy/RELEASE` still names the OLD release — take the
   folder from the job file instead:
   `REL=$(jq -r .release.dir ~/vl/data/updater/jobs/<job_id>.json)`,
   `grep -n 'VL_BUILD_ID  *=' "$REL/ninjascript/VLTraderTCPClient.cs"` (must print the
   release's build id), then
   `cp "$REL/ninjascript/VLTraderTCPClient.cs" "/mnt/c/Users/<you>/Documents/NinjaTrader 8/bin/Custom/AddOns/"`,
   F5 in NT8, full NT8 restart. (After a completed NT8-closed install,
   `$(cat ~/vl/deploy/RELEASE)` names the new release and gives the same folder.)
8. **Resume (attended):** `vl-updater resume <job>` — a TERMINAL only: it
   refuses when stdin is not a terminal (a pipe or a script cannot resume),
   prints the job's state and blocker, and makes you type the job id back.
9. **The RELEASE marker commit.** After the cutover, the deploy lane writes the
   post-update `deploy/RELEASE` marker commit per the boot procedure — that is
   a deploy-lane step, never yours from the Updates page.

### The NT8-closed flow (preferred — UPDATER-NT8-CLOSED, owner ruling 22:1x CT 09-27)

Close NT8 FIRST, then run the loop above. With NT8 closed nothing can trade
(every account is NT8 SIM and Sim101 executes INSIDE NT8), so the updater
PROCEEDS instead of waiting for an AddOn that cannot answer:

- The drain takes the **nt8_absent** path — taken only when ALL hold: the
  AddOn link has been down **≥60 s continuously** (measured from the bot's
  per-connection record, never from a stale ack), the hold is present, the
  entry barrier is drained, nothing is queued or in flight, no planner read
  is claimed, the ledger has no placed/pending/working arm or picture row,
  and every bound trading account is SIM-tradeable. The receipt records the
  path and each leg's evidence; the job file says `nt8_absent (link down
  since <ts>)`.
- **When the closed-NT8 path is reachable** (the link-down start is MEASURED,
  never assumed — `ConnectionRecord.LinkDownSince`): (1) NT8 was connected to
  THIS bot process and then closed — the start is the disconnect; or (2) the bot
  booted with NT8 already closed and no AddOn has connected since its listener
  came up (after a bot or WSL restart with NT8 off) — the start is the moment the
  bot began listening, so the path is eligible after 60 s of bot uptime. Before
  this was written (2) was unreachable: the never-connected bot had no
  disconnect stamp, preflight fell to the connected-world legs and refused
  "trader_cutover … api_positions: NT8 account positions unknown" (job de4cf900,
  2026-10-04 07:27) until NT8 was opened. If the link has been down less than
  60 s the job is refused at preflight with that blocker: wait, then re-run.
  With NT8 OPEN instead, take the normal path: flat → the job parks at
  `nt8_updated` → copy + F5 + restart (step 7) → `vl-updater resume` (step 8).
- If NT8 reconnects DURING the job, the normal ack/census legs apply again
  from that moment — the absent verdict is revoked, never grandfathered.
- The NT8 step with NT8 absent: **no `.cs` change → nt8_skipped** (as
  today); **a `.cs` change → the Go side completes and the job records
  "AddOn F5 owed at next NT8 start"** — it does NOT park (NT8 is closed;
  nobody can F5 now). `boot_verified` skips the AddOn-ack wait in this mode
  and records that it did.
- The simple owner flow: **close NT8 → authorize → paste → Update now →
  wait for complete → open NT8** (and F5 first if the job recorded it).
- With NT8 OPEN, live (non-SIM) connections may stay connected: the census
  admits them when **every account is flat** — the census proves
  `positions=0` and `working=0` across ALL accounts, live ones included
  (owner ruling 2026-09-28; the updater never disconnects anything). Any
  open position or working order on ANY account — live or SIM — still
  refuses, as does any connection in a transitional state. The owner flow
  is the same open or closed: **just flat**, then update.

## 3. Recovery

- **`recovery_needed`:** `vl-updater recovery <job>` prints the manual steps
  for that job. Do them in order, then resume.
- **Rollback:** restore the prior binary per the boot procedure's rollback
  (the `vl-bin.old.*` the cutover kept), then RELEASE + restart — the
  deploy-lane runbook owns this; do not improvise it here.
- **Token expiry:** a stale `VL_CUTOVER_TOKEN` shows up as 401s from the app;
  re-mint it (step 1.5) and restart the worker. An expired authorize grant is
  a `403` — authorize a new one (the old one stays spent).

## 4. Warnings (named, not implied)

- **NEVER put `VL_RELEASE_DIR` in the bot's `.env`.** It belongs ONLY in
  `~/.config/vl-updater/env`, the worker's own file. The bot's environment
  must not see it; the worker refuses a release root inside the install anyway.
- **Never hand-edit `deploy/release_allowed_signers`.** The CTO adds your key.
- **No privilege escalation, no sudo, ever.** The worker runs as your own user
  from `~/bin`; a root-owned `data/updater` locks the bot out.
- **The worker never runs an update step by itself unattended:** `fetch`,
  `authorize`, the F5 and `resume` are all attended by you, in that order.
- **Loopback-direct only.** The Updates page works when you open the bot at
  `127.0.0.1`/`localhost` **on port 8080**, signed in as the enrolled account —
  not through a proxy, a tunnel, the LAN name, or the `:3000` dev server
  (the page says so plainly instead of a bare refusal; the origin check
  itself is unchanged).
