package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Review fix (tests lens P2): the scorecard fixture must not be narrowed to help the run pass.
// D3.2-EX4's note states no time of day ("time of day not stated -> whole RTH day"), so the window
// stays 08:30-15:00 (Oct D3.2 @19:59-21:00). The EMA34 family is a deliberate harness rule (a
// refused trade is judged only against arms AT the EMA 34 line, Oct D5.3 @08:09-08:37): the
// matcher is pinned here so a change to it is visible.
func TestOctFixtureEX4WindowNotNarrowed(t *testing.T) {
	raw, err := os.ReadFile("testdata/oct_video_trades.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Trades []octTrade `json:"trades"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		var arr []octTrade
		if err2 := json.Unmarshal(raw, &arr); err2 != nil {
			t.Fatal(err)
		}
		f.Trades = arr
	}
	for _, tr := range f.Trades {
		if tr.ID == "D3.2-EX4" {
			if tr.Start != "08:30" || tr.End != "15:00" {
				t.Fatalf("D3.2-EX4 window %s-%s, want 08:30-15:00 (time of day not stated in the note)", tr.Start, tr.End)
			}
			return
		}
	}
	t.Fatal("D3.2-EX4 not in fixture")
}

func TestOctFamilyEMA34MatcherPin(t *testing.T) {
	rev := octArm{Setup: "ISB", Reason: "reverse ISB at the EMA 9"}
	at34 := octArm{Setup: "ISB", Reason: "trade at ema 34 touch"}
	if octFamilyMatch("EMA34", rev) {
		t.Fatal("EMA34 must not match a reverse ISB at the EMA 9")
	}
	if !octFamilyMatch("EMA34", at34) {
		t.Fatal("EMA34 must match a trade at the EMA 34 line")
	}
	if !octFamilyMatch("EMA", rev) {
		t.Fatal("legacy EMA family still matches reverse ISB")
	}
}

// Every signoff-pending id must be a listed accepted miss (a listing is not a close), and the
// D3.3-E8 / D2.2-E11 reasons must keep naming what actually refuses/contradicts (review fixes).
func TestOctSignoffPendingAreListedAndReasonsHonest(t *testing.T) {
	for _, id := range octSignoffPending {
		if _, ok := acceptedMisses[id]; !ok {
			t.Errorf("%s is signoff-pending but not in acceptedMisses", id)
		}
	}
	if r := acceptedMisses["D3.3-E8"]; !containsAll(r, "NOT CLOSED", "no inside bar") {
		t.Errorf("D3.3-E8 reason must say it is open and quote the note: %q", r)
	}
	if r := acceptedMisses["D2.2-E11"]; !containsAll(r, "isbrev_stop_over_max", "R47", "1 MNQ") {
		t.Errorf("D2.2-E11 reason must name the real refusals and R47's 1 MNQ allowance: %q", r)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, x := range subs {
		if !strings.Contains(s, x) {
			return false
		}
	}
	return true
}
