package addonbuild

import (
	"fmt"
	"os"
	"testing"
)

// legacyBuildIDFromBytes is a VERBATIM copy of the #46.1 (2a81551e9)
// buildIDFromBytes — the reader that is INSTALLED while #48.2 ships. It must
// stay byte-for-byte so the gates below prove the NEW artifact is still
// readable by the OLD reader. DO NOT "fix" this copy; it pins a real client.
func legacyBuildIDFromBytes(b []byte) (string, error) {
	var only string
	have := false
	record := func(m string) error {
		if m == "" {
			return nil
		}
		if !have {
			only, have = m, true
			return nil
		}
		if m != only {
			return fmt.Errorf("the DLL holds more than one distinct build id (%q and %q) — refusing to guess", only, m)
		}
		return nil
	}

	if err := legacyRecordIDs(buildIDRe.FindAllString(string(b), -1), record); err != nil {
		return "", err
	}
	if len(b) >= 2 {
		even := make([]byte, 0, len(b)/2)
		odd := make([]byte, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			even = append(even, b[i])
			odd = append(odd, b[i+1])
		}
		if err := legacyRecordIDs(buildIDRe.FindAllString(string(even), -1), record); err != nil {
			return "", err
		}
		if err := legacyRecordIDs(buildIDRe.FindAllString(string(odd), -1), record); err != nil {
			return "", err
		}
	}
	if !have {
		return "", nil
	}
	return only, nil
}

func legacyRecordIDs(matches []string, record func(string) error) error {
	for _, m := range matches {
		if err := record(m); err != nil {
			return err
		}
	}
	return nil
}

// utf16le renders s as UTF-16LE bytes (the #Blob heap string form).
func utf16le(s string) []byte {
	b := make([]byte, 0, 2*len(s))
	for i := 0; i < len(s); i++ {
		b = append(b, s[i], 0)
	}
	return b
}

// TestLegacyReaderRefusesTheAdjacentConstBlob — m400: the #46.1 reader must
// REFUSE the const-anchor blob layout. With VL_BUILD_ID_ANCHOR a const, its
// #Blob entry (compressed size byte 0x30) sat immediately after the id blob
// (no terminal byte), so the even/odd scan read the id as "2026-10-22-c0" —
// the '0' IS the 0x30 size byte — and the anchor's own id as "2026-10-22-c":
// two distinct, refused. MUTANT: drop the 0x30 size byte → one id → RED.
func TestLegacyReaderRefusesTheAdjacentConstBlob(t *testing.T) {
	var heap []byte
	heap = append(heap, 0x18)                                   // id blob: 24 bytes
	heap = append(heap, utf16le("2026-10-22-c")...)             // the VL_BUILD_ID const
	heap = append(heap, 0x30)                                   // anchor blob: 48 bytes — the trailing '0'
	heap = append(heap, utf16le("VL_BUILD_ID=2026-10-22-c")...) // the const anchor

	if _, err := legacyBuildIDFromBytes(heap); err == nil {
		t.Fatal("the #46.1 reader must refuse the adjacent const-anchor blob — it reads 2026-10-22-c0 and 2026-10-22-c")
	}
}

// TestCheckDLLReadersAgree — m400 gate, driven by deploy/addon/check-dll-readers.sh:
// reads CHECK_DLL (an absolute path), runs the #46.1 legacy reader and the
// current reader over it, and fails unless BOTH return the same single id (and
// neither errors). Skipped when CHECK_DLL is unset — it is a script gate, not a
// normal unit test.
func TestCheckDLLReadersAgree(t *testing.T) {
	p := os.Getenv("CHECK_DLL")
	if p == "" {
		t.Skip("set CHECK_DLL to run the two-reader agreement gate")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	legacyID, lerr := legacyBuildIDFromBytes(b)
	if lerr != nil {
		t.Fatalf("legacy (#46.1) reader refused the NEW artifact: %v — the installed updater would fail this install", lerr)
	}
	newID, nerr := BuildIDFromDLL(p)
	if nerr != nil {
		t.Fatalf("current reader refused: %v", nerr)
	}
	if legacyID == "" || legacyID != newID {
		t.Fatalf("readers disagree: legacy=%q current=%q — the installed updater would refuse this install", legacyID, newID)
	}
}
