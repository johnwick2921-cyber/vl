package trader

import (
	"testing"

	"vl/kernel/mentor"
	"vl/market"
	"vl/store"
)

// m465 P4 (release #60) — D2.4-R2: the resonance ("setup gồng lời") is a RAW
// same-way inside bar within 3 candles after a PHL/PLH fill: "sau khi nó khớp
// lệnh của mình, trong vòng 3 cây nến sau đó xuất hiện một setup Inside bar
// cùng chiều, thì đó là một setup cộng hưởng" [D2.4 p1 @01:37, @04:54]; the
// direction must match — "inside bar cho lệnh long, cùng chiều nha anh chị,
// chứ không phải inside bar lệnh short" [D2.4-R3 p1 @03:56, @04:42–04:44]. The
// course names no gate on that inside bar: it is a HOLD signal, not an entry,
// so an ISB whose own entry the bot refused (here: its stop is above
// mentor_isb_max_pts) still arms the resonance. Production path: the closed-bar
// exit drive (mentorExitDrive → mentorResonanceRawTick).

const m60Min = int64(60_000)

// m60Bar is one CLOSED 1m bar at minute i (a real-looking epoch base).
func m60Bar(i int, o, c, h, l float64) market.Kline {
	open := int64(1_793_000_000_000) + int64(i)*m60Min
	return market.Kline{OpenTime: open, CloseTime: open + m60Min - 1, Open: o, Close: c, High: h, Low: l, Final: true}
}

// m60PHLLong registers an open PHL long: entry 29600, stop 29560 (R 40),
// target 29700 — one bracket, a 1m entry (TrailTFMin tf).
func m60PHLLong(at *AutoTrader, tf int) *mentorLivePos {
	p := &mentorLivePos{
		Pos: mentorPosition{Symbol: "MNQ", Side: "long", Origin: "PHL", Entry: 29600, Stop: 29560,
			Target: 29700, R: 40, Mode: "B", Leg1: 1, Leg1TP: 29640},
		Legs:       [3]mentorLeg{{SignalID: "sig-phl", Qty: 1, TP: 29700, Stop: 29560, Final: true}},
		TrailTFMin: tf,
	}
	at.mentorRegisterLivePos("sig-phl", p)
	return p
}

// m60DriveSeams makes the account read OPEN long for the whole run.
func m60DriveSeams(t *testing.T) {
	t.Helper()
	oldS, oldQ := mentorDriveOpenSides, mentorDriveOpenQty
	mentorDriveOpenSides = func(*AutoTrader) (map[string]bool, bool) { return map[string]bool{"long": true}, true }
	mentorDriveOpenQty = func(*AutoTrader) (map[string]float64, bool) { return map[string]float64{"long": 1}, true }
	t.Cleanup(func() { mentorDriveOpenSides, mentorDriveOpenQty = oldS, oldQ })
}

// m60Drive feeds the bars to the PRODUCTION exit drive one closed bar at a time
// (the bars slice grows like the live feed); the first `pre` bars are history
// before the fill and are not driven.
func m60Drive(at *AutoTrader, bars []market.Kline, pre int) {
	for i := pre; i < len(bars); i++ {
		at.mentorExitDrive(bars[:i+1])
	}
}

// TestM60ResonanceArmsOnRawInsideBar — a long PHL fill, then a same-colour
// (green candle 1) inside bar at candle 2 after the fill whose own ISB entry the
// bot refused (its stop, 35 pts, is above mentor_isb_max_pts 25) → resonance
// ARMED (mode A), counted resonance_armed_raw_isb, nothing placed and no stop
// moved by the arm. Opposite colour → no. The inside bar at candle 4 → no.
// Named RED: no raw-ISB hook in the exit drive → the position stays B.
func TestM60ResonanceArmsOnRawInsideBar(t *testing.T) {
	pre := []market.Kline{m60Bar(0, 29595, 29600, 29602, 29590)}
	fill := m60Bar(1, 29600, 29605, 29608, 29596) // the PHL fill candle

	t.Run("same colour at candle 2 → armed", func(t *testing.T) {
		at, moves := newDriveAT(t)
		m60DriveSeams(t)
		p := m60PHLLong(at, 1)
		c1 := m60Bar(2, 29590, 29618, 29619, 29575) // green candle 1 (44-pt range)
		c2 := m60Bar(3, 29612, 29600, 29615, 29580) // body 29600–29612 inside c1 → ISB long
		if !mentor.IsISB(c1, c2) || mentor.ISBDirection(c1) != mentor.SideLong {
			t.Fatal("fixture: c2 must be a long inside bar of c1")
		}
		if isbStop := c2.High - c2.Low; isbStop <= mentor.DefaultConfig().CourseExits.ISBMaxStopPts {
			t.Fatalf("fixture: the ISB stop %.1f must be above mentor_isb_max_pts (its entry is refused)", isbStop)
		}
		bars := append(append([]market.Kline{}, pre...), fill, c1, c2)
		m60Drive(at, bars, len(pre))
		if p.Pos.Mode != "A-resonance" {
			t.Fatalf("a same-way raw inside bar at candle 2 after the PHL fill must arm resonance; mode=%q", p.Pos.Mode)
		}
		if n := at.mentorCountSnapshot()["resonance_armed_raw_isb"]; n != 1 {
			t.Fatalf("resonance_armed_raw_isb = %d, want 1", n)
		}
		if n := len(moves()); n != 0 {
			t.Fatalf("arming must not move a stop (BE is deferred to the drive): %d moves", n)
		}
		// Idempotent: the same pair (or any later one) never re-arms.
		if at.mentorResonanceRawTick(nil, p, c1, c2) {
			t.Fatal("an armed position must not arm again")
		}
		if n := at.mentorCountSnapshot()["resonance_armed_raw_isb"]; n != 1 {
			t.Fatalf("re-arm: resonance_armed_raw_isb = %d, want 1", n)
		}
	})

	t.Run("opposite colour → no", func(t *testing.T) {
		at, _ := newDriveAT(t)
		m60DriveSeams(t)
		p := m60PHLLong(at, 1)
		c1 := m60Bar(2, 29618, 29590, 29619, 29575) // RED candle 1
		c2 := m60Bar(3, 29612, 29600, 29615, 29580) // inside → an ISB SHORT
		bars := append(append([]market.Kline{}, pre...), fill, c1, c2)
		m60Drive(at, bars, len(pre))
		if p.Pos.Mode != "B" {
			t.Fatalf("an inside bar for the SHORT must not arm a long's resonance [D2.4-R3]; mode=%q", p.Pos.Mode)
		}
		if n := at.mentorCountSnapshot()["resonance_armed_raw_isb"]; n != 0 {
			t.Fatalf("resonance_armed_raw_isb = %d, want 0", n)
		}
	})

	t.Run("candle 3 → armed (window edge)", func(t *testing.T) {
		at, _ := newDriveAT(t)
		m60DriveSeams(t)
		p := m60PHLLong(at, 1)
		bars := append(append([]market.Kline{}, pre...), fill,
			m60Bar(2, 29606, 29610, 29611, 29605), // breaks out of the fill candle — no ISB
			m60Bar(3, 29610, 29614, 29615, 29609), // green, breaks out — no ISB
			m60Bar(4, 29613, 29611, 29614, 29610), // body inside candle 2 → ISB long at candle 3
		)
		m60Drive(at, bars, len(pre))
		if p.Pos.Mode != "A-resonance" {
			t.Fatalf("a same-way inside bar at candle 3 must arm; mode=%q", p.Pos.Mode)
		}
	})

	t.Run("candle 4 → no", func(t *testing.T) {
		at, _ := newDriveAT(t)
		m60DriveSeams(t)
		p := m60PHLLong(at, 1)
		bars := append(append([]market.Kline{}, pre...), fill,
			m60Bar(2, 29606, 29610, 29611, 29605),
			m60Bar(3, 29610, 29614, 29615, 29609),
			m60Bar(4, 29614, 29618, 29619, 29613),     // green, breaks out — no ISB
			m60Bar(5, 29617, 29615, 29618.5, 29614), // body inside candle 3 → ISB long at candle 4
		)
		for i := 2; i < len(bars); i++ {
			if mentor.IsISB(bars[i-1], bars[i]) && i < 5 {
				t.Fatalf("fixture: bar %d must not be an inside bar", i)
			}
		}
		m60Drive(at, bars, len(pre))
		if p.Pos.Mode != "B" {
			t.Fatalf("an inside bar at candle 4 is outside the 3-candle window; mode=%q", p.Pos.Mode)
		}
	})

	t.Run("knob off → no", func(t *testing.T) {
		at, _ := newDriveAT(t)
		off := false
		at.config.StrategyConfig.RiskControl.MentorResonanceRawISB = &off
		m60DriveSeams(t)
		p := m60PHLLong(at, 1)
		bars := append(append([]market.Kline{}, pre...), fill,
			m60Bar(2, 29590, 29618, 29619, 29575), m60Bar(3, 29612, 29600, 29615, 29580))
		m60Drive(at, bars, len(pre))
		if p.Pos.Mode != "B" {
			t.Fatalf("mentor_resonance_raw_isb=false must leave the raw inside bar alone; mode=%q", p.Pos.Mode)
		}
	})

	t.Run("an ISB fill never resonates", func(t *testing.T) {
		at, _ := newDriveAT(t)
		m60DriveSeams(t)
		p := m60PHLLong(at, 1)
		p.Pos.Origin = "ISB"
		bars := append(append([]market.Kline{}, pre...), fill,
			m60Bar(2, 29590, 29618, 29619, 29575), m60Bar(3, 29612, 29600, 29615, 29580))
		m60Drive(at, bars, len(pre))
		if p.Pos.Mode != "B" {
			t.Fatalf("only a PHL/PLH fill can resonate; mode=%q", p.Pos.Mode)
		}
	})
}

// m60Two returns the two CLOSED 1m bars that aggregate to one 2m candle
// (o, c, h, l) starting at minute i (i even → bucket-aligned).
func m60Two(i int, o, c, h, l float64) []market.Kline {
	mid := (o + c) / 2
	return []market.Kline{m60Bar(i, o, mid, h, l), m60Bar(i+1, mid, c, maxF(mid, c), minF(mid, c))}
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// TestM60ResonanceRawCountsTheEntryFrame — a 2m PHL entry counts 2m candles
// ("nếu như mình trade khung nào thì mình gồng theo khung đó" [X8 @18:02–18:14])
// and reads the inside bar on the 2m: candle 2 after the fill → armed.
func TestM60ResonanceRawCountsTheEntryFrame(t *testing.T) {
	at, _ := newDriveAT(t)
	m60DriveSeams(t)
	p := m60PHLLong(at, 2)
	var bars []market.Kline
	bars = append(bars, m60Two(0, 29595, 29600, 29602, 29590)...) // history
	pre := len(bars)
	bars = append(bars, m60Two(2, 29600, 29605, 29608, 29596)...) // the fill's 2m candle
	bars = append(bars, m60Two(4, 29590, 29618, 29619, 29575)...) // green 2m candle 1
	bars = append(bars, m60Two(6, 29612, 29600, 29615, 29580)...) // 2m inside bar at candle 2
	m60Drive(at, bars, pre)
	if p.Pos.Mode != "A-resonance" {
		t.Fatalf("a same-way 2m inside bar at 2m candle 2 must arm; mode=%q (frames after fill %d)", p.Pos.Mode, p.FramesAfterFill)
	}
	if prev, cur, ok := mentorResonanceRawPair(bars, 2); !ok || prev.Open != 29590 || cur.Open != 29612 {
		t.Fatalf("2m pair = %+v / %+v ok=%v", prev, cur, ok)
	}
	// on a 1m bar that does not close a 2m candle there is no pair.
	if _, _, ok := mentorResonanceRawPair(bars[:len(bars)-1], 2); ok {
		t.Fatal("a half-built 2m candle must not form a pair")
	}
}

// TestM60ResonanceRawKnobDefaultOn — nil → ON (course explicit).
func TestM60ResonanceRawKnobDefaultOn(t *testing.T) {
	if !mentorResonanceRawISBOn(nil) || !mentorResonanceRawISBOn(&store.RiskControlConfig{}) {
		t.Fatal("mentor_resonance_raw_isb must default ON")
	}
	off := false
	if mentorResonanceRawISBOn(&store.RiskControlConfig{MentorResonanceRawISB: &off}) {
		t.Fatal("an explicit false must turn it off")
	}
	if !mentor2mReduceSizeOn(nil) || mentor2mReduceSizeOn(&store.RiskControlConfig{Mentor2mReduceSize: &off}) {
		t.Fatal("mentor_2m_reduce_size: nil → ON, false → OFF")
	}
}
