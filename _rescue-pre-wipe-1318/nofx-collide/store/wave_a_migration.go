package store

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ── WAVE A / D1e + D2c — THE RECORD, MIGRATED HONESTLY ───────────────────────
//
// Two one-boot migrations, both flag-guarded, both backed up first, both
// counted on the boot line. NOTHING IS EVER DELETED and nothing is silently
// repaired: a row the fixed recorder could not have produced is MARKED, so no
// later reader can mistake it for evidence.
//
// A30 THREE-STATE HONESTY governs the classifier. Exactly one of the three
// dispatch buckets is decidable from the table itself:
//
//   invalid:duplicate      DECIDABLE — the episode key repeats; every copy
//                          after the first id is marked.
//   invalid:pre_formation  NOT DECIDABLE, and it reads 0 for that reason and
//                          not because there were none. The old recorder never
//                          stored a formation time (the column is new in this
//                          wave), so no legacy row carries the fact the test
//                          would need. We KNOW from the level semantics that
//                          all 14 RTH-L episodes are pre-formation — but
//                          re-deriving that per kind inside a migration is
//                          guessing, which A24 forbids. They are marked
//                          legacy:unverified instead, which excludes them just
//                          as firmly and claims nothing.
//   legacy:unverified      Everything else the old recorder wrote.
//
// No legacy row is ever marked "valid". Certification is something only the
// fixed recorder can confer.

// WaveARecordMigrateEnabled arms the one-boot migration. Same shape as
// ADHERENCE_REGRADE (store/adherence_regrade.go:59) — one implementation of
// "is this flag on" (A24), copied because the house uses env, not a knob.
func WaveARecordMigrateEnabled() bool {
	v := strings.TrimSpace(os.Getenv("WAVE_A_RECORD_MIGRATE"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// BackupBeforeWaveA copies the DB before any guarded write. No backup, no
// write — the same contract BackupBeforeRegrade enforces.
func BackupBeforeWaveA(dbPath, stamp string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	dir := filepath.Join(home, "nofx-backups", "wave-a-record")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}
	dst := filepath.Join(dir, stamp+".db")
	if _, err := os.Stat(dst); err == nil {
		return dst, nil // already taken for this stamp — idempotent
	}
	cmd := exec.Command("sqlite3", dbPath, fmt.Sprintf(".backup '%s'", dst))
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("sqlite3 backup: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return dst, nil
}

// WaveACounts is what one migration run measured. Counts, never rates (A24).
type WaveACounts struct {
	TouchRows        int64 // rows considered
	TouchDuplicate   int64 // marked invalid:duplicate
	TouchLegacy      int64 // marked legacy:unverified
	TouchPreFormMark int64 // marked invalid:pre_formation (see the note above)
	MAEZeroed        int64 // trader_positions.mae 0 -> NULL
	MFEZeroed        int64 // trader_positions.mfe 0 -> NULL
	MAEZeroIDs       []int64
}

// PendingWaveAWork reports what the migration WOULD do, without writing, so the
// boot line can show the count whether or not the flag is armed.
func (s *Store) PendingWaveAWork() WaveACounts {
	var c WaveACounts
	if s == nil || s.gdb == nil {
		return c
	}
	_ = s.TouchOutcomes()
	_ = s.Position()
	s.gdb.Model(&TouchOutcomeRow{}).Where("COALESCE(validity,'') = ''").Count(&c.TouchRows)
	s.gdb.Model(&TraderPosition{}).Where("status = ? AND mae = 0", "CLOSED").Count(&c.MAEZeroed)
	s.gdb.Model(&TraderPosition{}).Where("status = ? AND mfe = 0", "CLOSED").Count(&c.MFEZeroed)
	_ = s.gdb.Model(&TraderPosition{}).Where("status = ? AND mae = 0", "CLOSED").
		Order("id ASC").Pluck("id", &c.MAEZeroIDs).Error
	return c
}

// RunWaveARecordMigration classifies every unclassified touch_outcomes row and
// converts the pre-E4 excursion zeros to NULL. Idempotent: a second run finds
// nothing to do, because it only touches rows whose validity is still empty and
// zeros that are still zeros.
func (s *Store) RunWaveARecordMigration() (WaveACounts, error) {
	if s == nil || s.gdb == nil {
		return WaveACounts{}, nil
	}
	// The sub-stores AutoMigrate lazily, so touching them first guarantees the
	// tables exist before the migration reads or writes them. Without this the
	// migration fails with "no such table" on a store whose touch recorder has
	// not been used yet — which is exactly a fresh boot.
	_ = s.TouchOutcomes()
	_ = s.Position()
	c := s.PendingWaveAWork()

	// (1) DUPLICATES — decidable. Every copy after the first id for an episode
	// key is a re-recording of one real episode.
	dupTx := s.gdb.Exec(`
		UPDATE touch_outcomes SET validity = ?
		WHERE COALESCE(validity,'') = ''
		  AND id NOT IN (
		      SELECT MIN(id) FROM touch_outcomes
		      GROUP BY trader_id, symbol, level_kind, level_price, opened_at_ms
		  )`, ValidityDuplicate)
	if dupTx.Error != nil {
		return c, fmt.Errorf("mark duplicates: %w", dupTx.Error)
	}
	c.TouchDuplicate = dupTx.RowsAffected

	// (2) EVERYTHING ELSE the old recorder wrote is UNVERIFIED — never "valid".
	// It was still scanned over a whole 33 h void scope with a day-scoped
	// ordinal, so its ordinal and session strata are not usable either.
	leg := s.gdb.Exec(`UPDATE touch_outcomes SET validity = ? WHERE COALESCE(validity,'') = ''`, ValidityLegacy)
	if leg.Error != nil {
		return c, fmt.Errorf("mark legacy: %w", leg.Error)
	}
	c.TouchLegacy = leg.RowsAffected

	// (3) NULL IS NOT ZERO (D2c, class 40's shape). The pre-E4 path wrote 0
	// where it could not compute; E4 (trader/auto_trader_clock.go:757-759) now
	// leaves NULL and says so in its own words. Every remaining 0 predates that
	// fix, so it means UNMEASURED. A genuinely zero excursion written from here
	// on stays 0.0 and is distinguishable.
	mae := s.gdb.Model(&TraderPosition{}).Where("status = ? AND mae = 0", "CLOSED").
		Update("mae", nil)
	if mae.Error != nil {
		return c, fmt.Errorf("nullify mae: %w", mae.Error)
	}
	c.MAEZeroed = mae.RowsAffected
	mfe := s.gdb.Model(&TraderPosition{}).Where("status = ? AND mfe = 0", "CLOSED").
		Update("mfe", nil)
	if mfe.Error != nil {
		return c, fmt.Errorf("nullify mfe: %w", mfe.Error)
	}
	c.MFEZeroed = mfe.RowsAffected
	return c, nil
}

// WaveARecordBootLine is D6's line. EVERY figure is READ — from the table when
// the migration has run, from the pending count when it has not. The flag state
// is printed so a reader can tell "0 because nothing to do" from "0 because the
// migration is not armed" (A24: a plausible zero is a trap).
func (s *Store) WaveARecordBootLine(armed bool, ran WaveACounts, backup string) string {
	v := s.TouchOutcomes().CountByValidity()
	get := func(k string) int64 { return v[k] }
	total := int64(0)
	for _, n := range v {
		total += n
	}
	uncertified := get("")
	state := "not armed"
	if armed {
		state = "armed"
		if backup != "" {
			state = "armed backup=" + filepath.Base(backup)
		}
	}
	exTotal, exBackfilled, exUnresolved, exErr := s.TradeExcursions().Counts()
	ex := fmt.Sprintf("(backfilled=%d unresolvable=%d)", exBackfilled, exUnresolved)
	if exErr != nil {
		ex = "(count failed)"
	}
	ar := s.AcceptedRisk()
	// The ledger census, READ — leg 4 of the cutover gate compares the broker's
	// book against these rows, so the boot says out loud how many are live.
	ac := s.ArmedOrders().StateCensus()
	// cancel-confirmation (2026-09-06): a cancel_pending row is NON-TERMINAL —
	// its order may still be at the broker — so it counts as LIVE here. Without
	// this it would appear in no bucket at all and the census would read 0 live
	// while an order rested.
	live := int64(0)
	for armState, count := range ac {
		if !IsTerminalArmState(armState) {
			live += count
		}
	}
	return fmt.Sprintf(
		"record: touches=%d (valid=%d no_formation=%d invalid:pre_formation=%d invalid:dup=%d legacy=%d unclassified=%d) · excursions=%d %s · exit-cause=broker · accepted-risk rows=%d (with broker stop=%d) · mae/mfe 0→NULL=%d/%d · arms live=%d (superseded=%d cancelled=%d filled=%d) · migration %s",
		total, get(ValidityValid), get(ValidityNoFormation), get(ValidityPreFormation),
		get(ValidityDuplicate), get(ValidityLegacy), uncertified,
		exTotal, ex, ar.Count(), ar.WithAcceptedStop(),
		ran.MAEZeroed, ran.MFEZeroed,
		live, ac["superseded"], ac["cancelled"], ac["filled"],
		state) + "\n" + s.ArmedOrders().PlacementCensusLine()
}
