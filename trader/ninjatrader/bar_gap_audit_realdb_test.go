package ninjatrader

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"vl/kernel"
	ntwire "vl/provider/ninjatrader"
	"vl/store"
	"vl/store/sqlitedriver"
)

// m444 item 5 — the audit run against a COPY of the live database. Skipped
// unless VL_AUDIT_REALDB names a copy (never the live file: this opens it
// read/write-capable like any gorm handle, so the caller copies first).
//
//	VL_AUDIT_REALDB=/tmp/x/copy.db VL_AUDIT_NOW="2026-10-29T08:10:00-05:00" \
//	  go test -count=1 -run TestAuditOnACopyOfTheLiveDB -v ./trader/ninjatrader
func TestAuditOnACopyOfTheLiveDB(t *testing.T) {
	path := os.Getenv("VL_AUDIT_REALDB")
	if path == "" {
		t.Skip("VL_AUDIT_REALDB not set")
	}
	db, err := gorm.Open(sqlitedriver.GormDialector(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	bh := store.NewBarHistoryStore(db)
	now := time.Now()
	if v := os.Getenv("VL_AUDIT_NOW"); v != "" {
		if now, err = time.Parse(time.RFC3339, v); err != nil {
			t.Fatal(err)
		}
	}
	var out []string
	tr := newBarHoleTracker(bh, func() *ntwire.TCPServer { return nil })
	tr.say = func(level, msg string) { out = append(out, level+" "+msg) }
	days := tr.auditDays
	for _, tg := range []auditTarget{{"MNQ", "MNQ 12-26"}, {"ES", "ES 12-26"}} {
		tr.checked[holeKey(tg.Symbol, tg.Contract)] = 1
	}
	start := time.Now()
	tr.auditPass(now)
	took := time.Since(start)
	fmt.Printf("AUDIT-RUN now=%s window=%d session day(s) from %s took=%s\n", now.In(kernel.CTLocation()).Format(time.RFC3339), days,
		kernel.TableTimeCT(auditWindowStart(now, days, tr.isOpen)), took)
	for _, h := range tr.snapshot() {
		fmt.Printf("HOLE %s %s %s missing=%d min state=%s source=%s\n", h.Symbol, h.TF, h.Window, h.MissingMin, h.State, h.Source)
	}
	a := tr.snapshotAudit()
	fmt.Printf("AUDIT-BLOCK passes=%d found=%d open=%d unfilled_7d=%d unfilled_missing_min=%d audited=%v\n", a.Passes, a.FoundByAudit, a.OpenHoles, a.UnfilledHoles7d, a.UnfilledMissing, a.Audited)
	for _, l := range out {
		if strings.Contains(l, "bar gap audit:") {
			fmt.Println(l)
		}
	}
}
