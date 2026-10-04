package api

// W-ONE-BUTTON M3 red-team fold (red-3 #7) at the production router: an
// install whose MAC is over the untagged release|job|exp layout — a device-key
// MAC minted for anything but an install — is the uniform 403; the tagged
// code the attended CLI mints reaches the stub (422).

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"testing"
	"time"

	"vl/internal/updateauth"
)

func TestInstallRefusesAMACOverAnUntaggedMessage(t *testing.T) {
	e := newUpdEnv(t)
	key := mustKey(t, e.dataDir)
	exp := time.Now().Unix() + 120
	for i, layout := range []string{"%s|%s|%d", "vl-update-rollback/v1|" + updAdminID + "|%s|%s|%d"} {
		job := fmt.Sprintf("untagged-mac-%04d", i)
		m := hmac.New(sha256.New, key)
		fmt.Fprintf(m, layout, updRelease, job, exp)
		body := grantBody(updateauth.Grant{ReleaseID: updRelease, JobID: job, ExpiresAt: exp, HMAC: hex.EncodeToString(m.Sum(nil))})
		if w := e.do("POST", "/api/updates/install", body); w.Code != http.StatusForbidden || w.Body.String() != forbiddenBody {
			t.Errorf("MAC over layout %q = %d %s, want 403 %s", layout, w.Code, w.Body.String(), forbiddenBody)
		}
	}
	// positive control: the production minter's code is authorized
	if w := e.do("POST", "/api/updates/install", grantBody(e.grant(updRelease))); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("positive control: minted grant = %d %s, want 422", w.Code, w.Body.String())
	}
}
