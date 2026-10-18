package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestFriendManifestFriendModeOnly pins the B2 gate: the friend manifest routes
// exist only in friend mode (VL_FRIEND_INSTANCE=1); the owner's bot answers 404.
func TestFriendManifestFriendModeOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	old := friendManifestDir
	friendManifestDir = dir
	t.Cleanup(func() { friendManifestDir = old })

	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"addon_build":"37"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	do := func(t *testing.T, want int) {
		t.Helper()
		srv := &Server{}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/friend/manifest", nil)
		srv.handleFriendManifest(c)
		if w.Code != want {
			t.Fatalf("friend manifest code = %d, want %d (friend mode env=%q)", w.Code, want, os.Getenv("VL_FRIEND_INSTANCE"))
		}
	}

	t.Setenv("VL_FRIEND_INSTANCE", "")
	do(t, http.StatusNotFound) // owner's bot: 404

	t.Setenv("VL_FRIEND_INSTANCE", "1")
	do(t, http.StatusOK) // friend bot: served
}

func TestFriendManifestSigAndArtifact(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	old := friendManifestDir
	friendManifestDir = dir
	t.Cleanup(func() { friendManifestDir = old })

	if err := os.WriteFile(filepath.Join(dir, "manifest.json.sig"), []byte("-----BEGIN SSH SIGNATURE-----\nAAAA\n-----END SSH SIGNATURE-----\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "VL.AddOn.dll"), []byte("MZfake"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VL_FRIEND_INSTANCE", "1")

	srv := &Server{}

	sig := httptest.NewRecorder()
	c1, _ := gin.CreateTestContext(sig)
	c1.Request = httptest.NewRequest(http.MethodGet, "/api/friend/manifest.sig", nil)
	srv.handleFriendManifestSig(c1)
	if sig.Code != http.StatusOK {
		t.Fatalf("manifest.sig code = %d, want 200", sig.Code)
	}

	addon := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(addon)
	c2.Request = httptest.NewRequest(http.MethodGet, "/api/friend/artifact/addon", nil)
	c2.Params = gin.Params{{Key: "name", Value: "addon"}}
	srv.handleFriendArtifact(c2)
	if addon.Code != http.StatusOK {
		t.Fatalf("artifact/addon code = %d, want 200", addon.Code)
	}

	// A non-whitelisted artifact name is refused (no traversal).
	bad := httptest.NewRecorder()
	c3, _ := gin.CreateTestContext(bad)
	c3.Request = httptest.NewRequest(http.MethodGet, "/api/friend/artifact/../../etc/passwd", nil)
	c3.Params = gin.Params{{Key: "name", Value: "../../etc/passwd"}}
	srv.handleFriendArtifact(c3)
	if bad.Code != http.StatusNotFound {
		t.Fatalf("a traversal artifact name must 404, got %d", bad.Code)
	}
}
