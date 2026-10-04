package mentor

import "testing"

// TestSwingZoneKnobDefaultOff — swing_respects_5m_zone defaults false: §8
// is a self-contained 4h → 5m procedure and nothing in D5.2 ties it to the
// 5m trigger lines [C].
func TestSwingZoneKnobDefaultOff(t *testing.T) {
	if DefaultSwingCfg().Respects5mZone {
		t.Fatal("Respects5mZone must default false [C] not stated in the method")
	}
}
