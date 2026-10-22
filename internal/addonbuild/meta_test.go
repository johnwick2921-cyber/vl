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

// PIN m357 B — a DLL whose bytes hold MORE THAN ONE DISTINCT id-shaped value is
// refused (a stale id in a comment or resource must never be picked by offset).
// The SAME id appearing in both the raw and UTF-16 forms is one id, not two.
// MUTANT: return the FIRST match in buildIDFromBytes → the two-id case yields
// the stale id → RED.
func TestBuildIDFromDLLRefusesTwoDistinctIDs(t *testing.T) {
	dir := t.TempDir()
	two := filepath.Join(dir, "two.dll")
	if err := os.WriteFile(two, []byte("stale 2026-10-10-z in a comment, real 2026-10-21-c"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildIDFromDLL(two); err == nil {
		t.Fatal("a DLL with two distinct build ids must be refused")
	}
	// The UTF-16 form repeats the same id: still one id, no refusal.
	same := filepath.Join(dir, "same.dll")
	utf16 := make([]byte, 0, 2*len("2026-10-21-c"))
	for i := 0; i < len("2026-10-21-c"); i++ {
		utf16 = append(utf16, "2026-10-21-c"[i], 0)
	}
	// a raw ASCII copy of the SAME id too — same value, not two distinct.
	body := append([]byte("build=2026-10-21-c "), utf16...)
	if err := os.WriteFile(same, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := BuildIDFromDLL(same); err != nil || got != "2026-10-21-c" {
		t.Fatalf("BuildIDFromDLL(same id twice) = %q, %v; want 2026-10-21-c, nil", got, err)
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
