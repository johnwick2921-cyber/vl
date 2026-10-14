package kernel

import (
	"fmt"
	"time"
)

// P5.5 — ADHERENCE GRADE (A–F) per closed trade: did the trade FOLLOW the plan?
// Deterministic, computed from the plan-link facts, and SEPARATE from P&L (a
// well-followed plan can still lose; a lucky off-plan trade still grades low).
// The learning loop's v1 — it measures discipline, not outcome.

// AdherenceInput are the plan-link facts of a closed trade.
type AdherenceInput struct {
	Cited      bool // the entry cited a plan scenario at all
	Matched    bool // the cited scenario's direction matched the trade side
	OffPlan    bool // no scenario cited (traded outside the plan)
	InNoTrade  bool // entered inside a no-trade window (first 5m / lunch / blackout)
	InKillzone bool // entered inside an active killzone (the plan's high-prob window)
	// Band (B3/F6, forward-only): the entry's structural verdict against the
	// cited scenario — "" legacy/unknown | "ok" | "off_band" | "struct".
	Band string
	// Trigger (R22 F1/F8): did the cited scenario's REQUIRED trigger (a sweep
	// setup, or an arm authored wait_confirm) exist at the entry fill?
	// "" = not required / not verifiable (no change) | TriggerMet | TriggerMissed.
	Trigger string
}

// Adherence trigger verdicts (AdherenceInput.Trigger).
const (
	TriggerMet    = "met"
	TriggerMissed = "missed"
)

// gradeLetters is the A→F ladder; stepDown moves toward F.
var gradeLetters = []string{"A", "B", "C", "D", "F"}

func stepDown(grade string, n int) string {
	idx := len(gradeLetters) - 1
	for i, g := range gradeLetters {
		if g == grade {
			idx = i
			break
		}
	}
	idx += n
	if idx > len(gradeLetters)-1 {
		idx = len(gradeLetters) - 1
	}
	if idx < 0 {
		idx = 0
	}
	return gradeLetters[idx]
}

// GradeAdherence returns the A–F grade + the reasons. Base grade from citation/
// direction, then penalties for trading a no-trade window or outside a killzone.
func GradeAdherence(in AdherenceInput) (string, []string) {
	var reasons []string
	var base string
	switch {
	case in.OffPlan || !in.Cited:
		base = "D"
		reasons = append(reasons, "off-plan (no scenario cited)")
	case in.Matched && in.Trigger == TriggerMissed:
		// R22 F8: #649 graded "A (followed the plan)" although its sweep never
		// happened. An entry before the scenario's own trigger is not the plan.
		base = "C"
		reasons = append(reasons, "cited + direction matched, but the scenario's trigger had NOT happened at the entry")
	case in.Matched && in.Band == "off_band":
		// B3 (F6): same direction but the entry sits outside the cited
		// scenario's activation band — discipline says B, not A.
		base = "B"
		reasons = append(reasons, "cited + direction matched, but entry outside the scenario's activation band")
	case in.Matched && in.Band == "struct":
		base = "B"
		reasons = append(reasons, "cited + direction matched, but SL/TP inconsistent with the scenario's stated structure")
	case in.Matched:
		base = "A"
		reasons = append(reasons, "cited a scenario, direction matched")
	default:
		base = "C"
		reasons = append(reasons, "cited a scenario but the direction mismatched")
	}

	grade := base
	if in.InNoTrade {
		grade = stepDown(grade, 1)
		reasons = append(reasons, "entered in a no-trade window")
	}
	if !in.InKillzone {
		grade = stepDown(grade, 1)
		reasons = append(reasons, "entered outside a killzone")
	}
	return grade, reasons
}

// AdherenceSummary rolls a set of grades into counts + a GPA (A=4…F=0), for the
// trade-review panel + the weekly digest.
type AdherenceSummary struct {
	Counts map[string]int `json:"counts"`
	Total  int            `json:"total"`
	GPA    float64        `json:"gpa"`
}

var gradePoints = map[string]float64{"A": 4, "B": 3, "C": 2, "D": 1, "F": 0}

// SummarizeAdherence aggregates grades into counts + GPA.
func SummarizeAdherence(grades []string) AdherenceSummary {
	s := AdherenceSummary{Counts: map[string]int{}}
	var sum float64
	for _, g := range grades {
		if _, ok := gradePoints[g]; !ok {
			continue
		}
		s.Counts[g]++
		s.Total++
		sum += gradePoints[g]
	}
	if s.Total > 0 {
		s.GPA = sum / float64(s.Total)
	}
	return s
}

// SessionWindowFacts derives the two entry-window adherence inputs from an entry
// timestamp: was it inside an active killzone, and inside a no-trade sub-window
// (first 5m of the session, or lunch 12:00–13:30 CT). Returns (false,false) when
// no session is active at t.
func SessionWindowFacts(reg SessionRegistry, t time.Time) (inKillzone, inNoTrade bool) {
	sess, ok := reg.ActiveSession(t)
	if !ok {
		return false, false
	}
	inKillzone = sess.InKillzone(t)
	// ONE DEFINITION (owner ruling 2026-09-02): the grader used to carry its own
	// copies of "12:00"/"13:30" and its own first-5m helper, so it could score
	// against windows the gate did not enforce. Both now resolve here.
	if InLunchNoTrade(t) {
		inNoTrade = true
	}
	if InFirstNoTradeMinutes(sess.WindowStartCT, t) {
		inNoTrade = true
	}
	return inKillzone, inNoTrade
}

// inFirst5m reports whether t is within the first 5 minutes of a CT window start.
func inFirst5m(startCT string, t time.Time) bool {
	loc := CTLocation()
	var sh, sm int
	if _, err := fmt.Sscanf(startCT, "%d:%d", &sh, &sm); err != nil {
		return false
	}
	ct := t.In(loc)
	startMin := sh*60 + sm
	nowMin := ct.Hour()*60 + ct.Minute()
	d := nowMin - startMin
	return d >= 0 && d < 5
}

// AdherenceLabel is a short human label for a grade.
func AdherenceLabel(grade string) string {
	switch grade {
	case "A":
		return "followed the plan"
	case "B":
		return "mostly on-plan"
	case "C":
		return "loose adherence"
	case "D":
		return "off-plan"
	case "F":
		return "ignored the plan"
	default:
		return fmt.Sprintf("grade %s", grade)
	}
}
