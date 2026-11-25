package api

import (
	"encoding/json"
	"testing"

	storepkg "vl/store"
)

// m426 (E3 reader): /api/positions/history serialises updated_at/created_at in
// milliseconds even for a legacy second-valued row. Mutant: drop the
// PositionEpochMs normalisation in annotatePositionHistory -> the seconds value
// reaches the wire -> FAIL.
func TestM426PositionHistoryNormalisesLegacySecondRows(t *testing.T) {
	legacy := &storepkg.TraderPosition{Status: "CLOSED", UpdatedAt: 1_792_000_000, CreatedAt: 1_791_000_000}
	modern := &storepkg.TraderPosition{Status: "CLOSED", UpdatedAt: 1_792_000_000_123, CreatedAt: 1_791_000_000_456}
	rows, _ := annotatePositionHistory([]*storepkg.TraderPosition{legacy, modern})
	b, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out[0]["updated_at"] != float64(1_792_000_000_000) || out[0]["created_at"] != float64(1_791_000_000_000) {
		t.Fatalf("legacy second row not normalised: %v", out[0])
	}
	if out[1]["updated_at"] != float64(1_792_000_000_123) || out[1]["created_at"] != float64(1_791_000_000_456) {
		t.Fatalf("millisecond row must pass through: %v", out[1])
	}
	if legacy.UpdatedAt != 1_792_000_000 {
		t.Fatal("the store row must not be mutated (no DB rewrite)")
	}
}
