package datareport

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ReadRestoreResult reads the NEWEST result file deploy/backup-restore-test.sh
// wrote into dir. Result files are append-only, one per run, named by UTC
// timestamp (the script never overwrites or removes one), so the greatest name
// is the latest run. Format of the single line inside:
//
//	RESTORE-TEST <OK|FAIL> ts=<unix seconds> src=<backup file> ...
//
// A missing dir / no file / unparsable line = Have=false (printed n/a); it
// never invents a status. A result older than 8 days is flagged OVERDUE: the
// weekly timer should have written a newer one.
func ReadRestoreResult(dir string, now time.Time) RestoreResult {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return RestoreResult{}
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), restoreResultSuffix) {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return RestoreResult{}
	}
	sort.Strings(names)
	raw, err := os.ReadFile(filepath.Join(dir, names[len(names)-1]))
	if err != nil {
		return RestoreResult{}
	}
	line := strings.TrimSpace(strings.SplitN(string(raw), "\n", 2)[0])
	f := strings.Fields(line)
	if len(f) < 2 || f[0] != "RESTORE-TEST" || (f[1] != "OK" && f[1] != "FAIL") {
		return RestoreResult{}
	}
	res := RestoreResult{Have: true, Status: f[1], Raw: line}
	for _, kv := range f[2:] {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		switch k {
		case "ts":
			if sec, err := strconv.ParseInt(v, 10, 64); err == nil {
				res.At = time.Unix(sec, 0)
			}
		case "src":
			res.Src = v
		}
	}
	if res.At.IsZero() {
		return RestoreResult{} // a result with no time cannot be placed: n/a, not "OK of unknown date"
	}
	res.Overdue = now.Sub(res.At) > restoreOverdueAfter
	return res
}
