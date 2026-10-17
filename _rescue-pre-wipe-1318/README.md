# Rescue — pre-wipe snapshot (13:18 CDT 2026-10-16)

Owner order 2026-10-17 JOB 2.1: work that existed ONLY on this machine is committed here so that it lives on GitHub. Nothing was deleted to make this.

- Source: the worktrees restored from the 13:18 CDT snapshot. DS-108 ran a read-only salvage audit; the full table is in SALVAGE-REPORT.md.
- Each folder name is a worktree name. The paths inside it mirror that worktree's tree. The content is byte-exact: the commit was made with `--no-verify`, so the formatters did not rewrite anything.
- Included: files whose content was found on NO origin ref, i.e. class (b) NEWER, 668 files.
- Excluded: 16 files that gitleaks or the release secret scanner flagged. They are listed in EXCLUDED-secret-scan-false-positives-kept-local.txt, and the CTO judged them to be the deprecated public nofxos key, test fixtures or brand-scope hashes. They are kept locally under /home/hoang/fleet-1017/salvage/ and are not pushed.
- The folder starts with "_", so the Go tool ignores it. This branch is an archive and is never merged.
- The ~/vl main tree had NO class (b) files: its 87 untracked entries were 86 build/rollback leftovers (moved to ~/trash/2026-10-17/vl-untracked/) plus .cgcignore (kept).
