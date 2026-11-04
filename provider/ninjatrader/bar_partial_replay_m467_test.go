package ninjatrader

import (
	"testing"
	"time"

	"vl/internal/datatruth/checks"
)

// m467 (release #62) - THE 11-03 18:07 CT cross_frame PAUSE, from the real rows.
//
// data.db (MNQ 12-26), read with sqlite3 -readonly:
//
//	5m 17:55 live_partial  o 28356.25 h 28362.75 l 28328.50 c 28333.25 v 4666
//	1m 17:55 live          o 28356.25 h 28362.75 l 28331.25 c 28337.75 v 2166
//	1m 17:56 -- missing from the store (the ring held it: the check's 1m-built
//	           volume 9621 minus the four stored minutes = 2500; its OHLC below is
//	           SYNTHESISED inside the logged built range, low 28310.75 / close 28324.50)
//	1m 17:57 historical    o 28330.75 h 28333.50 l 28310.75 c 28320.00 v 1969
//	1m 17:58 historical    o 28319.25 h 28331.75 l 28310.75 c 28324.00 v 1466
//	1m 17:59 live          o 28324.25 h 28328.00 l 28312.00 c 28324.50 v 1520
//
// The 5m 17:55 bar in the RING was the live snapshot the AddOn never finalised.
// A replay of the window (what a re-request brings) carried NT8's own closed 5m
// bar - and mergeSeedKeepingLive kept the ring's "live" bar over it, because the
// ring stamps every streamed bar live whether or not it was ever final. So even
// a re-request could not clear the pause: it stood until the bar aged out.

const m467T0555 = int64(1793750100000) // 2026-11-03 17:55 CT (23:55Z)

func m467Ring(t *testing.T) *BarCache {
	t.Helper()
	c := NewBarCache(2500)
	m := int64(60_000)
	// 5m: 17:45 final, 17:50 + 17:55 never finalised (stored live_partial), 18:00 forming
	five := []Bar{
		{T: 1793749500000, O: 28333.75, H: 28356.25, L: 28306.0, C: 28340.5, V: 10442, Final: true},
		{T: 1793749800000, O: 28340.25, H: 28368.75, L: 28331.5, C: 28357.5, V: 7178},
		{T: m467T0555, O: 28356.25, H: 28362.75, L: 28328.5, C: 28333.25, V: 4666},
		{T: 1793750400000, O: 28324.5, H: 28380.75, L: 28317.0, C: 28373.0, V: 10424},
	}
	for _, b := range five {
		b.T += 5 * m // the wire is close-stamped
		c.Upsert("MNQ", "5m", []Bar{b})
	}
	one := []Bar{
		{T: 1793749500000, O: 28333.75, H: 28337.25, L: 28306.0, C: 28332.25, V: 3851},
		{T: 1793749560000, O: 28333.25, H: 28336.25, L: 28322.0, C: 28332.75, V: 1499},
		{T: 1793749620000, O: 28333.25, H: 28353.5, L: 28322.25, C: 28344.25, V: 2367},
		{T: 1793749680000, O: 28344.25, H: 28356.25, L: 28336.5, C: 28338.25, V: 1490},
		{T: 1793749740000, O: 28337.5, H: 28352.5, L: 28332.25, C: 28340.5, V: 1235},
		{T: 1793749800000, O: 28340.25, H: 28346.0, L: 28338.0, C: 28343.0, V: 844},
		{T: 1793749860000, O: 28342.25, H: 28346.0, L: 28331.5, C: 28337.75, V: 1078},
		{T: 1793749920000, O: 28337.25, H: 28348.0, L: 28334.5, C: 28347.75, V: 730},
		{T: 1793749980000, O: 28347.75, H: 28362.0, L: 28347.5, C: 28359.75, V: 1381},
		{T: 1793750040000, O: 28360.25, H: 28368.75, L: 28347.5, C: 28357.5, V: 3145},
		{T: 1793750100000, O: 28356.25, H: 28362.75, L: 28331.25, C: 28337.75, V: 2166},
		{T: 1793750160000, O: 28337.75, H: 28339.0, L: 28318.0, C: 28330.5, V: 2500}, // synthesised (see header)
		{T: 1793750220000, O: 28330.75, H: 28333.5, L: 28310.75, C: 28320.0, V: 1969},
		{T: 1793750280000, O: 28319.25, H: 28331.75, L: 28310.75, C: 28324.0, V: 1466},
		{T: 1793750340000, O: 28324.25, H: 28328.0, L: 28312.0, C: 28324.5, V: 1520},
		{T: 1793750400000, O: 28324.5, H: 28351.25, L: 28317.0, C: 28346.75, V: 2484},
		{T: 1793750460000, O: 28345.75, H: 28348.75, L: 28325.5, C: 28327.75, V: 1343},
		{T: 1793750520000, O: 28328.25, H: 28357.75, L: 28327.75, C: 28350.5, V: 2054},
		{T: 1793750580000, O: 28351.25, H: 28368.0, L: 28348.25, C: 28365.75, V: 1973},
		{T: 1793750640000, O: 28365.25, H: 28380.75, L: 28360.75, C: 28373.0, V: 2570},
		{T: 1793750700000, O: 28372.5, H: 28388.75, L: 28372.0, C: 28387.5, V: 1808},
	}
	for _, b := range one {
		b.T += m
		b.Final = true
		c.Upsert("MNQ", "1m", []Bar{b})
	}
	return c
}

func m467Mismatches(t *testing.T, c *BarCache) []checks.FrameMismatch {
	t.Helper()
	src := &DataTruthSources{Cache: c}
	m1, _ := src.Bars("MNQ", 1)
	m5, _ := src.Bars("MNQ", 5)
	now := time.UnixMilli(1793750843801) // 18:07:23.801 CT, the pause's own instant
	mm, _, _ := checks.CompareFrames(m1, m5, 5, now, src.Open(), checks.DefaultCross())
	return mm
}

// MUTANT (RED before m467): mergeSeedKeepingLive keeps EVERY existing live bar
// -> after the replay the 5m 17:55 still reads low 28328.50 / close 28333.25 and
// the cross_frame mismatch (and the pause) stands.
func TestReplayReplacesTheNeverFinalisedBarAndCrossFrameClears(t *testing.T) {
	c := m467Ring(t)
	mm := m467Mismatches(t, c)
	if len(mm) != 1 || mm[0].T != m467T0555 {
		t.Fatalf("baseline: the 11-03 5m 17:55 mismatch must reproduce, got %+v", mm)
	}
	before := c.PartialsReplacedByReplay()
	// NT8's replay of the window: its closed 5m bars, built from the same ticks
	replay := []Bar{
		{T: 1793749500000, O: 28333.75, H: 28356.25, L: 28306.0, C: 28340.5, V: 10442},
		{T: 1793749800000, O: 28340.25, H: 28368.75, L: 28331.5, C: 28357.5, V: 7178},
		{T: m467T0555, O: 28356.25, H: 28362.75, L: 28310.75, C: 28324.5, V: 9621},
		{T: 1793750400000, O: 28324.5, H: 28370.0, L: 28317.0, C: 28365.0, V: 9000}, // the forming bar at the request
	}
	for i := range replay {
		replay[i].T += 5 * 60_000
	}
	c.SeedHistorical("MNQ", "5m", replay)
	if mm := m467Mismatches(t, c); len(mm) != 0 {
		t.Fatalf("after NT8's replay the cross_frame check must measure clear by itself, got %+v", mm)
	}
	got := c.Get("MNQ", "5m")
	byT := map[int64]Bar{}
	for _, b := range got {
		byT[b.T] = b
	}
	if b := byT[m467T0555]; b.Source != BarSourceHistorical || b.L != 28310.75 || b.V != 9621 {
		t.Fatalf("the never-finalised 17:55 must be REPLACED by NT8's bar: %+v", b)
	}
	if b := byT[1793749500000]; b.Source != BarSourceLive || !b.Final {
		t.Fatalf("a FINAL live bar is never replaced by a replay: %+v", b)
	}
	if b := byT[1793750400000]; b.Source != BarSourceLive || b.H != 28380.75 {
		t.Fatalf("the ring's NEWEST (forming) bar is never replaced by a replay: %+v", b)
	}
	if n := c.PartialsReplacedByReplay() - before; n != 2 {
		t.Fatalf("replaced count = %d, want 2 (17:50 and 17:55)", n)
	}
}

// A replay on ANOTHER price scale (the 09-10 back-adjust: ~290 pt) never
// replaces even a never-finalised live bar.
func TestOffScaleReplayNeverReplacesAPartial(t *testing.T) {
	c := m467Ring(t)
	off := []Bar{{T: m467T0555 + 5*60_000, O: 28646.25, H: 28652.75, L: 28600.75, C: 28614.5, V: 9621}}
	c.SeedHistorical("MNQ", "5m", off)
	for _, b := range c.Get("MNQ", "5m") {
		if b.T == m467T0555 && b.Source != BarSourceLive {
			t.Fatalf("an off-scale replay replaced the live partial: %+v", b)
		}
	}
}
