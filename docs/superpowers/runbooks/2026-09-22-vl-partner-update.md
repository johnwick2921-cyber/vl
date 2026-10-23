# VL update for Tin and Binnie — September 22, 2026 (build step corrected 2026-09-26)

Prepared on a fresh clone of `johnwick2921-cyber/vlautoagenttraderv1`, based on
published main `78d35c25eca2e51853339bc5cf10ec30340cf40d`. Shared source changes
from nofx `8dcaca66` through `0960a6ac` were applied using format-patch → am.
The partner baseline matched every pre-existing file touched by that patch;
partner-specific files outside the patch were preserved. This is a partner
commit history, not a merge or reset to the unrelated nofx history.

## Included

- Picture HTF SIM strategy, native bar completion evidence, broker event
  handling and reconciliation, and its UI/configuration surfaces.
- AddOn `2026-09-20-p1`.
- Roll-aware charts and the Planner candle-response correction (PR 179).
- Shared tests, Guide content, and the other changes in the cumulative patch.

## Update each partner machine

1. Record the running Go revision, received AddOn build, selected strategy,
   and current positions/orders. Back up the database using SQLite backup
   and preserve the existing binary, frontend, RELEASE, and AddOn sources.
2. Require a fresh flat gate: broker and store positions flat, no working
   entries or orphan protective orders, and pending arms retired through
   the supported control path. Prevent re-arming during the attended update.
3. Fetch the approved partner branch and update by fast-forward/merge without
   overwriting `.env`, local data, credentials, account bindings, or saved
   strategy settings. This package does not change those values.
4. Use NT8's own trace to identify its loaded user-data AddOns directory.
   Back up the old sources under their build ID; copy the updated repository
   C# files there, compare hashes, F5 compile, and fully restart NT8.
   Verify a received frame carrying `2026-09-20-p1`; source files alone are
   not a receipt that the compiled AddOn is running.
5. **Build at the BUILD commit named by deploy/RELEASE — never at the branch
   tip.** The sync branch's tip is a RELEASE-stamp commit whose only change
   is the stamp; the build commit is its parent (for the boot-3 sync:
   build commit `66a0111c`, stamp tip `3ab8652`). A binary built at the tip
   carries `vcs.revision=<tip sha>`, which fails the boot-integrity prefix
   match against deploy/RELEASE (`kernel/boot_integrity.go:144-147`) and the
   bot REFUSES trading. Exact procedure:

   ```bash
   rm -rf /tmp/vlbuild && git clone <partner-repo-url> /tmp/vlbuild
   cd /tmp/vlbuild
   BUILD_COMMIT="$(sed -n 1p deploy/RELEASE)"   # the sha the branch says to build
   git checkout "$BUILD_COMMIT"                  # the build commit, NOT the tip
   GOMAXPROCS=4 nice -n 19 ionice -c 3 go build -o nofx-bin .
   # ONE-LINE CHECK — mismatch here means the bot would refuse trading at boot:
   [ "$(go version -m nofx-bin | sed -n 's/.*vcs.revision=\([0-9a-f]*\).*/\1/p')" = \
     "$(sed -n 1p deploy/RELEASE)" ] || { echo "BOOT WOULD REFUSE: binary rev != RELEASE"; exit 1; }
   go version -m nofx-bin | grep -E 'vcs.revision|vcs.modified'   # modified MUST be false
   ```
   Require `vcs.modified=false`. Derive GUIDE_BUILT_REV from that binary and
   then build the frontend with it:

   ```bash
   cd web && npm ci && npx tsc --noEmit && \
     VITE_GUIDE_BUILT_REV="$(go version -m ../nofx-bin | sed -n 's/.*vcs.revision=\([0-9a-f]*\).*/\1/p')" npm run build && cd ..
   ```

   The nofx source SHA and the partner binary SHA are different; do not copy
   Hoang's SHA into the partner's release stamp.
6. **Write deploy/RELEASE at cutover, like nofx does** — nofx's
   `deploy/leveltruth-cutover.sh:29` runs `echo -n "$BUILD_SHA" > deploy/RELEASE`
   at cutover time, and the marker commit carrying that RELEASE line is pushed
   only AFTER the boot. The partner flow aligns the same way: the
   RELEASE-stamp commit must NOT sit on the approved branch before the
   machines build. Cutover in the owner's attended safe window using the
   machine's existing v6 cutover path (`deploy/cutover.sh`); write RELEASE
   from the built binary's own revision at that moment, and push the stamp
   commit only after the boot is verified green. Verify boot integrity,
   health revision, UI bundle, and AddOn receipt. Restore the saved
   binary/RELEASE/frontend if verification fails.
7. Picture HTF defaults OFF. If the partner requests activation, verify the
   selected SIM account and at least 120 completed native 4H bars for the
   resolved contract in the evaluator cache, including completion evidence.
   Count bars, not detected levels. DB rows alone are not cache readiness.
   Native history must not create retroactive live entries. Enable through
   Strategy Studio only after readiness passes.
8. Return actual receipts: Go revision, AddOn build, data readiness, saved
   mode/runtime mode, and first natural entry/fill/protective-order frames.
   State pending evidence explicitly; installing code is not trade proof.

## Boot-3 addendum (2026-09-26, DS-103 verify-0926)

The approved post-boot-3 sync is vlauto PR #12, branch
`sync/nofx-9c106d0b-20260925`: build commit `66a0111c` (the commit named by
deploy/RELEASE), stamp tip `3ab8652` (child of the build commit, changes only
deploy/RELEASE). A clean-clone build at the tip was measured on 2026-09-26:
`vcs.revision=3ab8652…, vcs.modified=false`, which does NOT prefix-match
deploy/RELEASE=`66a0111c` → `kernel/boot_integrity.go:144-147` would refuse
trading. The one-line check in step 5 catches this before the cutover.
nofx avoids the trap because its RELEASE file is written AT cutover and its
marker commit (which changes RELEASE) lands AFTER the boot — the partner flow
above is aligned to the same order.

Neither partner machine was accessed or deployed by preparation of this branch.
The owner performs the partner-repository push under the standing repo rule.
