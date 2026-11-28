package datareport

import (
	"fmt"
	"os"
	"testing"
	"time"

	"gorm.io/gorm"

	"vl/store/sqlitedriver"
)

// REAL-DATA PROOF (manual, skipped unless DATAREPORT_PROOF_DB names a COPY of a
// production database - never the live file). It opens the copy read-only and
// prints the nightly report for the sessions named in DATAREPORT_PROOF_DAYS
// (comma-separated 16:10 CT instants, "2026-10-28T16:10"), so the numbers can be
// compared against direct sqlite3 queries.
func TestRealDataProofPrintsTheReportFromACopy(t *testing.T) {
	path := os.Getenv("DATAREPORT_PROOF_DB")
	if path == "" {
		t.Skip("DATAREPORT_PROOF_DB not set")
	}
	db, err := gorm.Open(sqlitedriver.GormDialector("file:"+path+"?mode=ro"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range splitComma(os.Getenv("DATAREPORT_PROOF_DAYS")) {
		at, err := time.ParseInLocation("2006-01-02T15:04", d, ct(2026, 1, 1, 0, 0).Location())
		if err != nil {
			t.Fatal(err)
		}
		t0 := time.Now()
		rep := Collect(Sources{DB: db, BootAt: at}, SessionFor(at))
		fmt.Printf("=== %s (collected in %s)\n", d, time.Since(t0).Round(time.Millisecond))
		for i, p := range Parts(rep.Lines()) {
			fmt.Printf("--- part %d (%d runes)\n%s\n", i+1, len([]rune(p)), p)
		}
	}
}

func splitComma(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
