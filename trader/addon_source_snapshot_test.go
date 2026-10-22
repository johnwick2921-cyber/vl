// m337 — the AddOn source is snapshotted against VL_BUILD_ID.
//
// The class-6 hole this closes: #45 shipped a source change to
// ninjascript/VLTraderTCPClient.cs (m227 bug-hunt #4: order_update de-dupe keys
// on state+filled) WITHOUT bumping VL_BUILD_ID, so the #43 and #45 AddOns
// reported the same build id and a bot could print match=yes against the older
// DLL. This pin records a sha256 over every ninjascript/*.cs file PAIRED with
// the VL_BUILD_ID the snapshot was taken at: if the .cs text changes without the
// build id moving, the test fails and tells the author to bump it.
package trader

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
)

// addonSourceSnapshot pairs the recorded sha256 over ninjascript/*.cs with the
// VL_BUILD_ID the snapshot was taken at. Bumping the AddOn means BOTH the C#
// VL_BUILD_ID and these two constants move together.
const (
	addonSourceSnapshotBuild = "2026-10-22-a"
	addonSourceSnapshotSHA   = "da0a55eb4cc8defc7a1274b093e0a414615ba3d7be50337e3d3d651578095162"
)

const addonSourceDir = "../ninjascript"

// addonSourceFilesSHA256 hashes every ninjascript/*.cs file in lexical order,
// concatenated raw. The set is globbed, so an ADDED or REMOVED .cs also moves
// the hash — the snapshot covers the whole AddOn source, not one file.
func addonSourceFilesSHA256() (string, error) {
	matches, err := filepath.Glob(addonSourceDir + "/*.cs")
	if err != nil {
		return "", err
	}
	sort.Strings(matches)
	h := sha256.New()
	for _, path := range matches {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// addonBuildIDFromSource reads VL_BUILD_ID straight out of the C# source so the
// pin cannot go green on a typo in a restated copy (A24).
func addonBuildIDFromSource() string {
	b, err := os.ReadFile(addonSourceDir + "/VLTraderTCPClient.cs")
	if err != nil {
		return ""
	}
	m := regexp.MustCompile(`VL_BUILD_ID\s*=\s*"([^"]+)"`).FindStringSubmatch(string(b))
	if m == nil {
		return ""
	}
	return m[1]
}

// TestAddonSourceSnapshotTracksBuildID — a recorded sha256 of ninjascript/*.cs
// paired with VL_BUILD_ID. If the .cs text changes without the build id moving,
// FAIL and tell the author to bump VL_BUILD_ID; if the build id DID move, FAIL
// and tell the author to record the new sha. Mutant: change a .cs comment
// without bumping → RED.
func TestAddonSourceSnapshotTracksBuildID(t *testing.T) {
	sum, err := addonSourceFilesSHA256()
	if err != nil {
		t.Fatalf("cannot hash ninjascript/*.cs: %v", err)
	}
	if sum == addonSourceSnapshotSHA {
		return // the AddOn source is exactly the snapshot
	}
	id := addonBuildIDFromSource()
	if id == addonSourceSnapshotBuild {
		t.Fatalf("ninjascript/*.cs changed but VL_BUILD_ID is still %q — bump VL_BUILD_ID in VLTraderTCPClient.cs (and update addonSourceSnapshotSHA to %s)",
			id, sum)
	}
	t.Fatalf("ninjascript/*.cs changed and VL_BUILD_ID moved to %q — record it: addonSourceSnapshotBuild=%q and addonSourceSnapshotSHA=%s",
		id, id, sum)
}
