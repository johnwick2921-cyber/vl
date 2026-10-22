import { GUIDE_BUILT_REV, type GuideSection } from '../types'

export const updates: GuideSection = {
  id: 'updates',
  num: 16,
  title: 'Updates',
  tagline:
    'The one-click update: bot, web page and the NinjaTrader AddOn, in one press.',
  asBuiltRev: GUIDE_BUILT_REV,
  blocks: [
    { kind: 'h', text: 'The Updates page (Settings → Updates)' },
    {
      kind: 'p',
      text: 'Settings → Updates is the one screen for updating the bot. It shows what is running now (the revision, the guide build, the AddOn build the maintenance acknowledgement names), the update button, the maintenance hold and the installation gate, the update job, and the native-history readiness. It refreshes itself every ten seconds. Everything on it is READ from the bot’s own API and nothing else: a value the bot cannot tell you prints n/a, and an update job that has not run is absent — the page says "no update job", it does not draw an empty timeline.',
    },
    { kind: 'h', text: 'What one click of Update does' },
    {
      kind: 'p',
      text: 'Update installs EVERYTHING in one press: the Go bot, the web page, AND the NinjaTrader AddOn. There is no terminal, no agent, no F5 and no "resume" step. The bot first verifies and stages the release, then — only when the release’s NinjaScript differs from the AddOn the box is running — builds a new AddOn DLL on this box (dotnet build, in a temp folder, using the references from this box’s own NinjaTrader install). If that build fails, the update is REFUSED before anything is installed or held, and the page shows the compiler error. After the usual flat-and-SIM gate passes, the new DLL is placed: the old one is MOVED (never deleted) to a dated backup folder outside NinjaTrader’s load path, the new one is copied in, and its sha256 is verified. The bot then reads the build id stamped inside the placed file and checks it is the build this release expects (see “The AddOn file is checked after it is placed”). NinjaTrader is then restarted — only when it was open at the moment you pressed Update (see “NT8 closed when you press Update”) — and the bot first verifies NinjaTrader is actually running again (a bounded process check — a launch that exits immediately is not a success; one failed process check is retried until the wait runs out, it does not fail the job at once), then waits for the AddOn to reconnect on a NEW connection (a new accept_seq) whose hello names the NEW build id. The job completes only when that acknowledgement arrives — a timeout holds the job and shows a plain-words blocker, never a silent success. If NinjaTrader does not come back up after the restart, the job ends in recovery (the hold stays, with that exact reason) instead of pretending the restart worked. If an install is undone (“reverted clean”) after the updater had already closed NinjaTrader, the revert also opens NinjaTrader again, so you are never left with it closed; if it cannot, the job says so and you open NinjaTrader yourself. An update job is only valid within 30 minutes of pressing Update; an unfinished job older than that is stale.',
    },
    { kind: 'h', text: 'What you see while it runs' },
    {
      kind: 'p',
      text: 'The page shows each step in plain words: “Building the NinjaTrader AddOn…”, then “Restarting NinjaTrader…”, then “Waiting for NinjaTrader to reconnect…”, then “Done — NinjaTrader is running the new AddOn”. If a step fails or the reconnect times out (the wait is bounded, default 5 minutes), the job is held and the page shows the held job’s blocker text with a Retry button — you type the same password as Update and press Retry. There is no terminal instruction anywhere on the page.',
    },
    { kind: 'h', text: 'What each update-button state means' },
    {
      kind: 'p',
      text: 'How to update: press Check, type your VL login password, press Update now. The password is sent only in that one request and is never stored or shown. A fallback under Advanced lets you paste an install code — the one line vl-updater-bootstrap authorize prints, valid 5 minutes and single use — for a box that cannot reach the password flow.',
    },
    {
      kind: 'p',
      text: 'The update panel has two buttons. Check asks the bot whether an update exists and never installs anything — it reads Checking…, Up to date, or Check. Update now fires the install. The primary flow is the password field: type the owner password and Update installs the release the last Check affirmed. The paste box (under Advanced) takes the ONE line “vl-updater-bootstrap authorize <release_id>” prints — {release_id, job_id, expires_at, hmac}, valid 5 minutes and single use — and the page sends exactly those parsed fields; expires_at must stay an unquoted whole number of unix seconds, and the box refuses a quoted one with its own text before the server ever sees it. The primary Update now (the password button) is enabled only when the bot answers install_enabled true AND worker_listening true (both measured, never guessed) AND the review constant is off AND the last Check affirmed an update available and verified AND a password is typed — nothing is pasted for it. The Advanced Update now needs the same three conditions plus a valid pasted authorization, and no Check or password. Blocked: the install control is disabled — the bot did not answer install_enabled true, or the updater worker is not listening (worker_listening false), or the review constant is on. Installing…: the install was accepted and is running. A refusal (400/403/409/422/429/503 — 429 is the password lockout: repeated wrong passwords lock installs out for 15 minutes and the body carries the unlock time) shows the bot’s own text under the buttons, and the pasted code is spent either way — a retried code answers “job already used”. A grant is spent when the page POSTs it (5 minutes, single use); while the job waits for the AI plan the wait is job-internal — it never asks for a new authorization. If an AI plan read is in flight when the job reaches preflight, drain or gate, the update WAITS for it (the planner wait, default 15 minutes) and the page shows “waiting for the AI plan (started hh:mm:ss)” — the plan check is the FIRST one the preflight flat check reports, and every other check must still pass before the hold.',
    },
    { kind: 'h', text: 'Release-source states (Check)' },
    {
      kind: 'p',
      text: 'When the release source is enabled (VL_RELEASE_SOURCE), Check asks the bot whether a newer verified release exists. The page then shows one of: Update available vX — verified (downloaded and signature-verified, ready to install); rate limited, try later (GitHub answered 403/429); or the server’s own reason. The page renders each state ONLY when the API affirms it — it never invents one. While a job is downloading or verifying, the panel shows Downloading… / Verifying… from the server’s install_state.',
    },
    {
      kind: 'p',
      text: 'While an update job is getting ready — requested, downloaded, verified, or in its preflight flat check — the bot FREEZES NEW ENTRIES on every path (AI decisions, armed and mentor orders, Picture HTF, agent chat), so it no longer races the job’s flat check. Exits, stops, closes, protection and plan reads are never frozen. An entry already RESTING at NinjaTrader (a planner limit waiting at its price) is withdrawn too while the job is pending: the same entries-only cancel a --withdraw-entries hold sends, each cancel stays cancel_pending until NinjaTrader confirms it, and the stop and target of a filled position are never touched, so the preflight flat check is no longer refused for a resting entry. The freeze reads the updater’s own job files and clears by itself when the job ends (refused or cancelled — or held, when the hold takes over) or 30 minutes after the job was created, whichever comes first, so a stuck updater can never freeze trading for good. The maintenance panel shows “Install pending” with the job, its state and that latest clear time; GET /api/maintenance carries install_pending; a refusal logs “⏳ install pending … REFUSED” and counts as install_pending in the gate-block table. A job file the bot cannot read is skipped and counted (install_pending_unreadable) — the hold stays the hard stop. The freeze lives entirely in the bot and ships with it; it needs no updater change.',
    },
    { kind: 'h', text: 'The one-time migration (the only manual step, ever)' },
    {
      kind: 'p',
      text: 'The AddOn used to live as source files inside NinjaTrader’s AddOns folder, compiled by F5 into NinjaTrader.Custom.dll. With the new one-click build there is a ready-built VL.AddOn.dll, so those sources must leave — otherwise NinjaTrader would load the AddOn TWICE. The first one-click install MOVES the VL source files (and their .bak files) out of AddOns into a dated backup folder — never deletes them. NinjaTrader then needs to rebuild its Custom.dll without them: that is ONE F5, one time, ever (the bot refuses to start NinjaTrader while the AddOn type exists in BOTH dlls, so there is no silent double-load). From then on every update is fully automatic.',
    },
    { kind: 'h', text: 'The hold and the gate' },
    {
      kind: 'p',
      text: 'The maintenance panel shows the hold state (clear, held, unreadable, or unconfigured), whether the bot has drained its in-flight sends, and the AddOn’s acknowledgement of the current hold. Until an acknowledgement arrives the panel says "no ack yet" — it never turns a missing ack into "false". The installation gate is one verdict for the whole installation, not one trader: every leg the update needs must pass, and each leg’s exact reason text is shown. A leg that could not be evaluated fails. The hold is written and cleared only by the local operator tool or the updater itself — never by this page. When a job ends refused, rolled back or in recovery, the job panel shows a Reason row with the server’s exact text. The panel follows the job the hold names, or — for a job refused in preflight, which never writes the hold — the job the install-pending freeze names, and remembers the last one for this browser tab, so a reload does not lose a refused job.',
    },
    { kind: 'h', text: 'What a receipt proves — and what it cannot' },
    {
      kind: 'p',
      text: 'A receipt is the recorded proof of a completed update job: the states the job passed through, its timestamps, and whether it ended complete, rolled back, or in need of recovery. A receipt from an AddOn that was never restarted, or from an AddOn whose build does not match the release, never satisfies verification — with the one-click flow the AddOn is built, placed, restarted and acknowledged as part of the job itself, so the receipt’s AddOn step proves the box is running the new AddOn, not merely that the install reached a machine. The page shows the receipt download link only when the API offers one.',
    },
    { kind: 'h', text: 'NT8 closed when you press Update' },
    {
      kind: 'p',
      text: 'If NinjaTrader is closed when you press Update, and has been closed for at least 60 seconds, the job takes the “NT8 closed” path. The build and place steps still run: the new AddOn file is built, the old one is MOVED to the backup folder, the new one is copied in and checked. The updater does NOT restart NinjaTrader and does not wait for a reconnect. When the job says complete, YOU open NinjaTrader — it loads the new AddOn on that start. Nothing is owed and there is no "resume" step. If NinjaTrader is open, the updater closes and reopens it itself, and only when every trader is flat and on SIM. If NinjaTrader connects again before the hold is placed, the normal open path applies instead.',
    },
    { kind: 'h', text: 'The AddOn file is checked after it is placed' },
    {
      kind: 'p',
      text: 'The AddOn is one file with a build id stamped inside it, and that id is tied to its source code. Today’s AddOn is 2026-10-22-b. After the new file is placed, the bot reads the id back from the file itself and compares it with the build the release says it should have. A wrong or unreadable id is refused and the previous, already-verified AddOn file is put back. A backup is only put back if it still matches the proof recorded when it was saved. If that put-back also fails, the bad file is MOVED aside into the backup folder (never deleted) and the job ends “recovery needed”: the hold stays and you must look at it before anything else.',
    },
    {
      kind: 'p',
      text: 'After an install, the bot may run a catch-up that rebuilds the AddOn if the one running is out of date. A catch-up builds only from the release that is installed and verified, and it NEVER puts back an older AddOn: a build that is not newer than the running or placed one is refused and the AddOn is left alone. Once an install is verified (boot verified), nothing touches the AddOn again, so a late step cannot overwrite it.',
    },
    { kind: 'h', text: 'When an update reverts' },
    {
      kind: 'p',
      text: 'A job that ends “reverted clean” changed nothing: the old bot and the old AddOn keep running and the hold is released. Mornings can show one when Windows and WSL briefly fail to talk to each other (a message like “UtilAcceptVsock: accept4 failed 110”) during the AddOn build or the NinjaTrader restart. That is a temporary timeout, not a broken release. Press Update again, or close NinjaTrader first so the closed path (which skips the restart) is used.',
    },
    { kind: 'h', text: 'Native history readiness' },
    {
      kind: 'p',
      text: 'The readiness panel reports how far the native 4-hour history load has got. PivotWindow is read from the resolved strategy settings; the completed-bar target is PivotWindow + 4. Where the bot exposes no number — the completed count, the last progress, or a paused-while-loading flag — the panel prints n/a rather than computing or guessing one in the browser. Reload history asks the bot for a deep bars backfill through the existing backfill route.',
    },
    { kind: 'h', text: 'Enrolling this box for updates' },
    {
      kind: 'p',
      text: 'Updates stay OFF until the installation’s update administrator is enrolled, on the box itself, as the bot’s own user: `go run ./cmd/vl-updater-bootstrap --install-dir <the bot’s folder> enroll <your exact account email>`, then type the confirmation it asks for. No page and no API can enroll or reset it; the Updates routes only READ the two enrollment files on each request. Once enrolled, the Updates routes answer only a browser on this box that opens the bot directly at 127.0.0.1 or localhost — never through a proxy, a tunnel or the LAN address — signed in as the enrolled account. Every refusal is the same "forbidden"; the bot logs the reason as a warning the FIRST time that kind of refusal happens on that route, and every refusal after it at debug level — each one is still counted, per route and kind, in vl_updates_refused_total on the bot’s /metrics page. With VL_UPDATER unset (the default) the verifier is the stub and nothing installs.',
    },
    { kind: 'h', text: 'The updater glue: VL_UPDATER' },
    {
      kind: 'p',
      text: 'VL_UPDATER=1 in the bot’s environment (exactly 1 — anything else, or unset, is OFF) connects the Updates routes to the separate updater worker. OFF is the default and changes nothing: the verifier refuses every release, a job id always answers "not found", and no line is printed. ON, the bot prints ONE boot line — 📦 updater glue: on · verifier=verdict-file · worker=dial ok — where worker reads n/a when the worker was not listening at boot (it is started by hand, so that is normal, not a fault). ON, three things change. (1) A release counts as verified only when the attended `vl-updater fetch <release_id>` on the box has checked its signature and every file and written its verdict under data/updater/verdicts/; a release without its verdict answers 422 {"error":"release not verified"}. (2) An accepted install is HANDED OFF to the updater worker over its private socket (data/updater/worker.sock). The worker is a separate program the owner starts by hand, attended: `vl-updater serve`. The bot never runs an update step itself. If the worker is not running, or refuses, the install answers "installer unavailable" — and, as always, that one-time code is spent: authorize a new one. (3) The job and receipt routes read the worker’s own job file, so the Updates page shows the job’s state, the time each state was DONE (its timestamps, one row per state) and any blocker. The page keeps polling while the hold names the job — and keeps polling the last job id after the hold clears, so complete or rolled_back is the last thing it shows rather than a frozen snapshot. An unknown job, and a job file that cannot be read safely, both answer "not found" (the second is logged as an error on the box).',
    },
    { kind: 'h', text: 'The worker self-update: VL_UPDATER_SELF_UPDATE' },
    {
      kind: 'p',
      text: "VL_UPDATER_SELF_UPDATE=1 in the worker's env file (~/.config/vl-updater/env) makes the worker install the release's verified updater/vl-updater over its own binary after the new bot has PROVEN itself (the job reaches boot_verified). The swap is atomic (temp in the same directory, fsync, rename; the previous binary is kept as vl-updater.old.<sha>), is recorded in the job receipt, and only then does the worker exit so systemd restarts the unit on the new binary — which needs Restart=always in vl-updater.service (the owner step that ships with this knob). To turn it on, the owner adds exactly one line to ~/.config/vl-updater/env — VL_UPDATER_SELF_UPDATE=1 — and restarts the worker: systemctl --user restart vl-updater. OFF is the default: the step records \"self_update: off\" and the job completes exactly as before. The worker never swaps before boot_verified, never on a refused, rolled-back or recovery_needed job, and refuses with a named reason when the binary is not in the signed manifest, its sha256 does not match, or its build info does not carry the release's source sha.",
    },
    {
      kind: 'p',
      text: 'The worker accepts requests on its socket, from this box’s own user only: status, install, cancel-before-boundary (only before the hold is placed), resume and check. The worker checks the peer’s uid and the job state. The worker runs as a systemd --user unit (deploy/systemd-user/vl-updater.service, installed by deploy/install-updater-worker.sh from a named clean sha): its env file, ~/.config/vl-updater/env (mode 0600), carries VL_RELEASE_DIR (absolute, outside the install) and VL_CUTOVER_TOKEN — a gate-jwt with a 24-hour lifetime; refresh it before each attended install window, there is no longer-lived token type. The unit clears TZ and never joins the bot’s own cgroup — the worker refuses both. The status route measures worker_listening at request time — a bounded 250 ms dial of the worker socket, never a guess — and both install controls need install_enabled and worker_listening true and the review constant off; when the worker is not running the button reads Blocked and the page shows the exact reason "updater worker not running". The NinjaTrader path and the Windows user come from the worker environment with auto-detection as the default (VL_NT8_DOCS_DIR, VL_NT8_EXE) — the partner box has its own paths, so nothing is hardcoded to one machine.',
    },
    { kind: 'h', text: 'Changing the password (Settings → Account)' },
    {
      kind: 'p',
      text: 'Changing the password needs your CURRENT password as well as the new one: the bot checks it against the stored password and refuses a wrong one ("current password is incorrect", shown under the form). A signed-in session alone can no longer set a new password. Machine tokens — the Telegram bot’s and the gate-jwt tool’s — can never change a password, reset the account, log out, change the Telegram settings or reach Updates, whatever they carry. Beyond that, a machine token may only READ and REDUCE risk: it reads every page’s data, and it may stop a trader, pause a trader’s new entries, close a position or flatten a trader — nothing else. Each refusal is logged and counted in the gate-block table as machine_token_refused:<route>. A wrong current password is answered after a one-second pause and counted. A password change also un-enrolls Updates: every Updates route answers 403 until you re-enroll on the box. Locked out? Password reset by email is disabled; the fix is ONE statement on the box that sets the new password hash AND updated_at together (the runbook has it).',
    },
    {
      kind: 'p',
      text: 'A password change ends EVERY session signed in before it — on every page, not only on Settings — including the one you changed it from: the page signs you out and you sign in again with the new password. A token issued in the same second as the change is ended too. A session token whose account no longer exists (after a reset-account) is refused everywhere. The Telegram bot acts for the box OWNER — the account that owns every trader. The 👤 owner line at boot names the owner (first 8 characters of its id) and the rule that chose it. A session token stamped more than a minute ahead of the box’s clock is refused everywhere, so if the box’s clock steps BACK by more than a minute, sessions signed in during the skipped time are refused until the clock catches up. Thrown out right after a password change, with the bot’s log saying “credential epoch is Ns in the future — clock stepped back; sign-in refused until then”? The box’s clock moved backwards after the change: sign-in works again once the clock passes that moment.',
    },
  ],
}
