// M2 fold — the R1a shell-twin contract pin.
//
// Every deploy script reads env as the twin chain ${VL_X:-${NOFX_X:-default}}:
// the old name keeps working on machines configured under it until R5 removes
// the NOFX halves. A chain that loses its NOFX half is silent behaviour change:
// the old-name config is ignored, and the value falls to a DIFFERENT default.
// postboot-check.sh:36 was the survivor this pin exists to catch — it had no
// test, and no deploy test turned RED when its twin half was stripped.
//
// The pin is table-driven over the deploy scripts that READ env, and it fails
// when any ${VL_X:- ...} in one of them has no ${NOFX_X:- ...} chain in the
// SAME file. Stripping the NOFX half is the mutant; this test must go RED.
package deploy

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestEnvTwinChainsKeepBothHalves(t *testing.T) {
	scripts := []string{
		"vl-lock.sh",
		"vl-claim.sh",
		"vl-db-backup.sh",
		"vl-clock-guard.sh",
		"postboot-check.sh",
	}
	vlChain := regexp.MustCompile(`\$\{VL_([A-Z0-9_]+):-`)
	for _, s := range scripts {
		b, err := os.ReadFile(s)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		src := string(b)
		seen := map[string]bool{}
		for _, m := range vlChain.FindAllStringSubmatch(src, -1) {
			if seen[m[1]] {
				continue
			}
			seen[m[1]] = true
			if !strings.Contains(src, "${NOFX_"+m[1]+":-") {
				t.Errorf("%s reads ${VL_%s:- without the ${NOFX_%s:- half of the twin —\n"+
					"stripping the NOFX half makes old-name configurations silently read a different value", s, m[1], m[1])
			}
		}
	}
}
