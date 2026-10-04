package mentor

import "testing"

// TestNearBoxRefusal — Day-3 row 24 [D3.2 p1 @ 21:53–23:08]: a setup whose
// nearest box edge IN the trade direction is closer than roomMultiple × risk
// is refused; row 25 [@ 23:14–24:03]: a setup between two boxes is exempt.
// The CTO's pin numbers: 10 pts under an FTGH with a 6 pt risk (2R = 12) →
// refused; 20 pts → allowed; between two boxes → allowed.
func TestNearBoxRefusal(t *testing.T) {
	cases := []struct {
		name         string
		boxes        []Box
		price        float64
		side         Side
		roomMultiple float64
		risk         float64
		wantRefuse   bool
	}{
		{
			name:         "10 pts under FTGH, 6 pt risk, 2R=12 → refused (pin)",
			boxes:        []Box{{Kind: FTGH, Top: 110, Bottom: 100}},
			price:        90, // 10 pts under the FTGH bottom edge
			side:         SideLong,
			roomMultiple: 2,
			risk:         6,
			wantRefuse:   true,
		},
		{
			name:         "20 pts under FTGH, 6 pt risk, 2R=12 → allowed (pin)",
			boxes:        []Box{{Kind: FTGH, Top: 130, Bottom: 110}},
			price:        90, // 20 pts under the FTGH bottom edge
			side:         SideLong,
			roomMultiple: 2,
			risk:         6,
			wantRefuse:   false,
		},
		{
			name:         "between two boxes → exempt (row 25 pin)",
			boxes:        []Box{{Kind: FTGH, Top: 110, Bottom: 100}, {Kind: FTGL, Top: 85, Bottom: 80}},
			price:        90,
			side:         SideLong,
			roomMultiple: 2,
			risk:         6,
			wantRefuse:   false,
		},
		{
			name:         "inside one box, 3 pts under its top, 6 pt risk → refused (F1 pin)",
			boxes:        []Box{{Kind: FTGH, Top: 100, Bottom: 90}},
			price:        97, // 3 pts under the top, INSIDE the box
			side:         SideLong,
			roomMultiple: 2,
			risk:         6,
			wantRefuse:   true,
		},
		{
			name:         "far box below + near box above (price inside the near box) → refused (F1 pin)",
			boxes:        []Box{{Kind: FTGH, Top: 100, Bottom: 90}, {Kind: FTGL, Top: 70, Bottom: 60}},
			price:        95, // inside the near box (90 < 95 < 100); far box wholly below
			side:         SideLong,
			roomMultiple: 2,
			risk:         6,
			wantRefuse:   true,
		},
		{
			name:         "box wholly above + box wholly below → allowed (F1 pin)",
			boxes:        []Box{{Kind: FTGH, Top: 110, Bottom: 100}, {Kind: FTGL, Top: 80, Bottom: 70}},
			price:        95,
			side:         SideLong,
			roomMultiple: 2,
			risk:         6,
			wantRefuse:   false,
		},
		{
			name:         "short: 10 pts above FTGL, 6 pt risk → refused",
			boxes:        []Box{{Kind: FTGL, Top: 100, Bottom: 95}},
			price:        110, // 10 pts above the FTGL top edge
			side:         SideShort,
			roomMultiple: 2,
			risk:         6,
			wantRefuse:   true,
		},
		{
			name:         "short: 20 pts above FTGL, 6 pt risk → allowed",
			boxes:        []Box{{Kind: FTGL, Top: 100, Bottom: 95}},
			price:        120, // 20 pts above the FTGL top edge
			side:         SideShort,
			roomMultiple: 2,
			risk:         6,
			wantRefuse:   false,
		},
		{
			name:         "no edge in the trade direction → allowed",
			boxes:        []Box{{Kind: FTGL, Top: 80, Bottom: 70}}, // box entirely below
			price:        90,
			side:         SideLong,
			roomMultiple: 2,
			risk:         6,
			wantRefuse:   false,
		},
		{
			name:         "no boxes → allowed",
			boxes:        nil,
			price:        90,
			side:         SideLong,
			roomMultiple: 2,
			risk:         6,
			wantRefuse:   false,
		},
		{
			name:         "room 0 disables the check",
			boxes:        []Box{{Kind: FTGH, Top: 110, Bottom: 100}},
			price:        90,
			side:         SideLong,
			roomMultiple: 0,
			risk:         6,
			wantRefuse:   false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			refuse, why := nearBoxRefusal(c.boxes, c.price, c.side, c.roomMultiple, c.risk)
			if refuse != c.wantRefuse {
				t.Fatalf("nearBoxRefusal(%+v, price=%.0f, side=%s, room=%g, risk=%g) = (%v, %q), want refuse=%v",
					c.boxes, c.price, c.side, c.roomMultiple, c.risk, refuse, why, c.wantRefuse)
			}
		})
	}
}
