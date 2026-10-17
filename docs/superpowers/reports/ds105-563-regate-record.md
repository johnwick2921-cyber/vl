# DS-105 — #563 leg-3 bracket re-gate record (post-wipe [B] bridge record)

CTO accepted the verdict as a [B] bridge record on 2026-10-17 06:17Z (the worktree and
/tmp gate logs were lost in the 10-16 19:05–19:14 wipe, so no on-disk log survives).

- **Base:** `origin/dev` @ `20216fce7` (merged `fix/leg3-bracket @3a40ed3b2` on top, local
  merge head `8b4c18ad4`, the only conflict was `kernel/mentor/eval.go` — resolved keep-both:
  `nextLevelBeyondNonEMA` + `runnerPartialLine`).
- **Mutants (33, independently re-run against the merged head):** 31 RED · 1 SURVIVED (M12 —
  "leg2 for a 1-contract runner": inert, because `leg2Qty = runner / 2` and a runner of 1
  yields 0, so `runner >= 1` changes nothing) · 1 APPLY-FAILED (M18 — "rebuild never sees leg 3":
  the anchored string no longer matches; the D4 fold rewrote `leg3Live` to read the book's
  `-sl3/-tp3` instead of the authored `r.Leg2Qty`, so the mutant is n/a).
- **Gates (all green):** `go build`/`go vet`; `go test ./...` CT and TZ=UTC; `-race` on
  trader/kernel/mentor/trader-ninjatrader/provider-ninjatrader (0 races); FULL_DAY_REPLAY
  byte-identical dev vs merged (25 summary lines); `tsc`; vitest 117 files / 1144 tests.
- **C# AddOn compile** (Windows dotnet 9 + NT8 DLLs): clean; planted typo `orders.Add(sl3x)`
  → `CS0103` → real gate proven, then restored clean.
- **Fold items D1–D10** verified [A] at the production call sites.

**Verdict: PASS.** #563 has since been merged into release #32; the CTO re-runs the full suite
on the final #32 head and the NT8 F5 + restart check at install time.
