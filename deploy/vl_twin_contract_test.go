// R5 — the shell-twin contract pin, INVERTED for the end state.
//
// Until R5 every deploy script read env as the twin chain (the retired name
// assembled at runtime in this file, never written out).
// R5 removes the retired halves: a chain that KEEPS one is a silent behaviour
// change in reverse — a machine still configured under the old name would keep
// working, and the census (zero allow-list) would never see the value read.
// Re-adding a retired half is the mutant; this test must go RED.
//
// The pin is table-driven over the deploy scripts that READ env, and it fails
// when any ${VL_X:- ...} in one of them still has a retired-name chain in
// the SAME file.
package deploy

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestEnvReadsCarryNoRetiredHalves(t *testing.T) {
	scripts := []string{
		"vl-lock.sh",
		"vl-claim.sh",
		"vl-db-backup.sh",
		"vl-clock-guard.sh",
		"postboot-check.sh",
	}
	vlChain := regexp.MustCompile(`\$\{VL_([A-Z0-9_]+):-`)
	retired := strings.ToUpper("no" + "fx")
	for _, s := range scripts {
		b, err := os.ReadFile(s)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		src := string(b)
		for _, m := range vlChain.FindAllStringSubmatch(src, -1) {
			if strings.Contains(src, "${"+retired+"_"+m[1]+":-") {
				t.Errorf("%s reads ${VL_%s:- with a retired ${%s_%s:- half —\n"+
					"R5 removed every retired env read; re-adding one is the mutant", s, m[1], retired, m[1])
			}
		}
	}
}
