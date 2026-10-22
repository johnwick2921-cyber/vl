package addonbuild

import (
	"os"
	"path/filepath"
	"testing"
)

func fakeDLLBytes(id string) []byte {
	b := make([]byte, 0, 2*len(id)+2)
	for i := 0; i < len(id); i++ {
		b = append(b, id[i], 0)
	}
	return append(b, 0x00, 0x00)
}

func writeDLL(t *testing.T, path, id string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, fakeDLLBytes(id), 0o644); err != nil {
		t.Fatal(err)
	}
}

// PIN m343 BuildIDFromDLL — the UTF-16LE scan reads the embedded VL_BUILD_ID
// (the 10-21 forensics used the same scan). MUTANT: drop the even-offset scan →
// "" → RED.
func TestBuildIDFromDLLReadsUTF16(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "VL.AddOn.dll")
	writeDLL(t, p, "2026-10-18-a")
	got, err := BuildIDFromDLL(p)
	if err != nil {
		t.Fatalf("BuildIDFromDLL: %v", err)
	}
	if got != "2026-10-18-a" {
		t.Fatalf("BuildIDFromDLL = %q, want 2026-10-18-a", got)
	}
	// A DLL with no build id returns "".
	empty := filepath.Join(dir, "other.dll")
	if err := os.WriteFile(empty, []byte("not a dll"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := BuildIDFromDLL(empty); err != nil || got != "" {
		t.Fatalf("BuildIDFromDLL(no id) = %q, %v; want \"\", nil", got, err)
	}
}

// PIN m343 Place records the backup proof — the sidecar next to the backed-up
// DLL holds the OLD DLL's sha256 + build id. MUTANT: drop WriteAddonBackupMeta
// in Place → no sidecar → RED.
func TestPlaceWritesBackupMeta(t *testing.T) {
	docs := t.TempDir()
	custom := filepath.Join(docs, "bin", "Custom")
	writeDLL(t, filepath.Join(custom, "VL.AddOn.dll"), "2026-10-18-a")
	newDll := filepath.Join(docs, "new", "VL.AddOn.dll")
	writeDLL(t, newDll, "2026-10-21-c")
	newSum, err := sha256File(newDll)
	if err != nil {
		t.Fatal(err)
	}

	backupDir, err := Place(Config{DocsDir: docs}, newDll, newSum, "job1")
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if backupDir == "" {
		t.Fatal("Place must return a backup dir when an old DLL existed")
	}
	meta, err := ReadAddonBackupMeta(backupDir)
	if err != nil {
		t.Fatalf("ReadAddonBackupMeta: %v", err)
	}
	oldSum, err := sha256File(filepath.Join(backupDir, "VL.AddOn.dll"))
	if err != nil {
		t.Fatal(err)
	}
	if meta.Sha256 != oldSum {
		t.Fatalf("meta sha256 = %q, want the backed-up DLL's %q", meta.Sha256, oldSum)
	}
	if meta.BuildID != "2026-10-18-a" {
		t.Fatalf("meta build_id = %q, want 2026-10-18-a", meta.BuildID)
	}
}
