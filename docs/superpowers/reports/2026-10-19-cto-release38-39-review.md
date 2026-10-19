# CTO release #38 completion and #39 pre-integration review

Recorded 2026-10-19 by CTO (Codex). This is a checkpoint, not #39 release approval.

## Release #38 — directly verified [A]

- Published tag `v2026.10.18.2`; GitHub assets include manifest.json, manifest.json.sig, and v2026.10.18.2.tar.gz.
- Journal at 07:57:46 CT: BOOT INTEGRITY OK, revision `93fe38652791`, expected revision matches, goldens PASS.
- Job `4abb03f7422d2ab15de4f6b0798ebdb7`: state complete, phase done, source `93fe3865279175ee4fa1809a9bdc02b081417dea`, ended 12:57:48 UTC.
- Worker-swap receipt: old `61ab21c7e9dd866f88934cc7678858d085a988cf`, new `93fe3865279175ee4fa1809a9bdc02b081417dea`, ok=true.
- Marker commit `8399ae4721af35fa4fcb0e54e7abfdaf7cd494e7` pushed to dev under cto-codex-r38-marker lock; lock released afterward. Unrelated untracked .cgcignore preserved.
- Lock utility currently removes lock files on release. This invocation used narrowly scoped command adapters that preserved displaced metadata and moved lock files to ~/trash/2026-10-19; no files deleted. Adapter source retained at /tmp/vl-cto-marker.mWTXdq/bin/.
- No operator service restart or owner database access was performed. Owner pressed Update.

## Release #39 — review-back at 005d3304f

Target branch: fix/updater-win-tool-paths, reviewed commit `005d3304f3f1602e0a79f2a0a964d31c702ed32e`. Integration must start from post-marker dev `8399ae4721af35fa4fcb0e54e7abfdaf7cd494e7`.

1. **Installer distro parser [A, reproduced].** deploy/install-updater-worker.sh uses field 3 of a backslash-separated UNC path. Both wsl.localhost and wsl$ forms put the server in field 3 and distro in field 4. The actual installer, exercised in a disposable HOME with fake wslpath/git and stopped before service installation, persisted the server instead of the distro. Evidence: /tmp/r39-installer-probe.4rKLoW/result.json. Require exact-value production-writer regressions.
2. **Timeout safety coverage [A source inspection; independent DS-107 review].** Production restart has a bounded exit wait, but the added test covers eventual success only. Require old process never exits -> error/clean revert, no second Start-Process, timeout receipt; dropping the timeout return must fail the test. No actual NT8 invocation.
3. **Existing worker Windows executable resolution [A source trace].** internal/updaterworker/addon_steps.go winSeam calls worker.go execWindows, which sends bare executable names to exec.CommandContext. nt8_restart.go uses powershell.exe/tasklist.exe through this path. addonbuild fallback does not cover this adapter. Require adapter-level resolution and harmless-stub tests preserving explicit paths.

Bootstrap limitation: worker_self_update.go swaps the updater only after the new bot boot is verified, and only when VL_UPDATER_SELF_UPDATE=1. It does not rerun the installer. A release containing a fixed worker cannot be assumed to repair a failure that prevents the old worker reaching that swap.

DS-105 owns fixes. DS-107 owns independent re-review, integration, full B7 gates, and PR. CTO owns final verified merge/tag. Owner alone approves release environment. Required tag: v2026.10.19.1. No #39 pass or live-install claim is made here.

## Friend-PC B8 prerequisites — investigation open

- Referenced deploy/friends/OWNER-SETUP.md is absent from dev at this checkpoint [A].
- Windows Tailscale executable exists; WSL networkingMode=mirrored; a Tailscale address is visible on eth0 [A]. This does not prove remote reachability or ACL isolation.
- vl.service is enabled [A]. install-autostart.sh stops/restarts the bot, so it must not be blindly rerun as a setup prerequisite.
- B8 requires a real friend PC and the friend-clicked SIM round trip. No synthetic result substitutes for it.
- Revoke moves the instance directory, while resume starts the original unit and rotate needs the original .env [A source trace]. Exact revoke->resume/new key recovery remains for isolated reproduction; not asserted working.

Follow docs/superpowers/AUDIT-CHECKLIST.md and the owner no-delete, SIM-only, no-service-restart rules. B8 investigation is separate from #39 scope.

## Checkpoint update — 08:14 CT

- Parser fix `098c90ef7f70cd7a8a50d01ac466e948afd45580`: independent CTO production-script verification [A], six cases pass (both UNC roots, no trailing slash, failed/empty discovery, unexpected host). Evidence /tmp/r39-installer-fixed.whyh6u/result.json. DS-107 independently passed the increment. Timeout pin and Windows execution adapter remain open at this checkpoint.
- Friend distribution `20261018084531`: ssh-keygen verification against the public key embedded in cmd/vl-friend/friend_pubkey.go succeeds [A]. VL.AddOn.dll, vl-friend.exe and VL-Setup.exe all match the signed manifest hashes [A]. This proves file integrity only; it is not a real Windows installation result.
- DS-101/104 reproduced in isolated fixtures: removing a friend moves its directory, resume/rotate lack their required files, and provision rejects the remaining revoked registry name. Device ID remains empty, so remote removal reports no_device_id. These are lane-reported fixture results backed by matching source inspection; no real friend was revoked.
- Running friend key is loaded from process environment; changing .env alone does not establish immediate old-key rejection. Real connection-path invalidation tests remain required.
- Production Windows installer source calls `tailscaleInstall` from flow.go, while run_windows.go directly executes the supplied name and has no dispatcher for that symbolic command. DS-108 is assigned to verify clean-PC behavior. Desktop shortcut targets msedge, so automatic app-update invocation also needs verification.
- Owner setup handover references an unmerged runbook (feat/friend-runbook). Actual source needs TUNNEL_IP in both bot and updater environments, plus enrollment credentials; no manual service restart is authorized. Existing Tailscale installation and enabled vl service mean those steps should not be blindly repeated.
