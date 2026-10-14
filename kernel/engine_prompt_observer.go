package kernel

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"vl/mcp"
)

// Phase 3 — IN-POSITION WATCHER (final-bundle 2026-08-19). Owner ruling:
// in-position the AI is WATCH-ONLY — zero order authority. The observer prompt
// is THESIS-ANCHORED: the model judges the ORIGINAL stated entry thesis against
// fresh market data; it never re-underwrites the whole market and its response
// can never carry an action.

// ObserverInput is everything the observer prompt needs about the held position
// and its original entry decision. Thesis/levels come verbatim from the entry.
type ObserverInput struct {
	Symbol          string
	Side            string // LONG | SHORT
	EntryPrice      float64
	StopLoss        float64 // the ORIGINAL stated stop
	TakeProfit      float64
	Thesis          string // the entry decision's reasoning, verbatim
	EntryConfidence int
	AgeMinutes      int
	PnLPoints       float64
	MFEPoints       float64
	MAEPoints       float64
	CurrentStop     float64 // best-known working stop (BE/trail included)
	BreakevenFired  bool
	TrailLevel      float64 // 0 = trail not armed
	PrevStatus      string  // last accepted status (context, not authority)
	// G8 (regime wave 2026-08-21) — the Go-computed structure line (trends +
	// swings + latest events); the observer judges the conflict question
	// against THIS machine truth, not its own re-derivation.
	StructureLine string
	// TouchLines (T2 telemetry addendum, 2026-08-26) — the live TOUCH lines
	// for active level episodes (facts, advisory). Empty → section omitted.
	TouchLines string
}

// ObserverAssessment is the schema-enforced watch response.
type ObserverAssessment struct {
	ThesisStatus      string `json:"thesis_status"` // intact | weakening | invalidated
	InvalidationCited string `json:"invalidation_cited,omitempty"`
	Note              string `json:"note"`
	Confidence        int    `json:"confidence"`
	// G8 (regime wave 2026-08-21) — the observer's structure verdict:
	// none | warning | confirmed (judged against the machine structure line).
	StructureConflict string `json:"structure_conflict"`
	// ActionIgnored is set by the PARSER (never the model): the response carried
	// an action-like field, which the watcher ignores and logs.
	ActionIgnored bool `json:"-"`
}

// BuildObserverSystemPrompt renders the watch-only system prompt. The CT clock
// line comes FIRST (tz contract — helpers from kernel/tz.go only).
func (e *StrategyEngine) BuildObserverSystemPrompt(in ObserverInput, snapshotMs int64) string {
	var sb strings.Builder
	// Clock line first: the observer must know WHEN it is looking.
	if snapshotMs > 0 {
		sb.WriteString(fmt.Sprintf("Clock: %s · Snapshot: %s\n\n",
			ClockCTSeconds(time.Now()), ClockCTSeconds(time.UnixMilli(snapshotMs))))
	} else {
		sb.WriteString(fmt.Sprintf("Clock: %s\n\n", ClockCTSeconds(time.Now())))
	}
	sb.WriteString("# ROLE: POSITION OBSERVER (WATCH-ONLY)\n\n")
	sb.WriteString("You are OBSERVING an open futures position. You have ZERO order authority: you cannot close, resize, or modify anything. The resting bracket and mechanical rails manage the trade. Your ONLY job is to judge whether the ORIGINAL entry thesis still holds.\n\n")
	sb.WriteString("## THE POSITION\n")
	dir := strings.ToUpper(in.Side)
	sb.WriteString(fmt.Sprintf("- %s %s · entry %.2f · stop %.2f · target %.2f\n", dir, in.Symbol, in.EntryPrice, in.StopLoss, in.TakeProfit))
	sb.WriteString(fmt.Sprintf("- age %d min · PnL %+.1f pts · MFE %+.1f / MAE %+.1f pts\n", in.AgeMinutes, in.PnLPoints, in.MFEPoints, in.MAEPoints))
	stopLine := fmt.Sprintf("- working stop %.2f", in.CurrentStop)
	if in.BreakevenFired {
		stopLine += " (breakeven fired)"
	}
	if in.TrailLevel > 0 {
		stopLine += fmt.Sprintf(" · trail armed @ %.2f", in.TrailLevel)
	}
	sb.WriteString(stopLine + "\n")
	if in.PrevStatus != "" {
		sb.WriteString(fmt.Sprintf("- your previous assessment: %s\n", in.PrevStatus))
	}
	sb.WriteString("\n## THE ORIGINAL ENTRY DECISION (verbatim — this is the ONLY thesis you judge)\n")
	// R22 P2: a plan-arm entry has NO AI confidence — say n/a, never a fabricated 0.
	conf := "n/a (no AI confidence — e.g. a plan-arm entry)"
	if in.EntryConfidence > 0 {
		conf = fmt.Sprintf("%d", in.EntryConfidence)
	}
	sb.WriteString(fmt.Sprintf("- direction: %s · stated stop: %.2f · stated target: %.2f · entry confidence: %s\n", dir, in.StopLoss, in.TakeProfit, conf))
	thesis := strings.TrimSpace(in.Thesis)
	if thesis == "" {
		thesis = "(no stated thesis recorded — judge only the stated stop/target levels)"
	}
	sb.WriteString("```\n" + thesis + "\n```\n\n")
	if line := strings.TrimSpace(in.StructureLine); line != "" {
		sb.WriteString("## MACHINE STRUCTURE (Go-computed — the conflict question below is judged against THIS, cite the event)\n")
		sb.WriteString(line + "\n\n")
	}
	// T2 (2026-08-26) — live touch telemetry (facts, advisory): one TOUCH line
	// per active episode, nearest 2. Zero order authority.
	if line := strings.TrimSpace(in.TouchLines); line != "" {
		sb.WriteString("## TOUCH (live level reactions — facts, advisory)\n")
		sb.WriteString(line + "\n\n")
	}
	sb.WriteString("## YOUR QUESTIONS (answer EXACTLY these)")
	sb.WriteString("\n1. Has the ORIGINAL stated invalidation condition triggered? Judge only against the stated thesis above — do NOT re-analyze the market for new trades, do NOT propose actions, do NOT second-guess the stop/target placement.")
	sb.WriteString("\n2. Does current machine structure CONTRADICT the position's direction? structure_conflict: none | warning | confirmed — 'confirmed' requires confidence ≥70 AND a cited structure event (BOS/CHoCH/MSS) against the direction; 'warning' = opposed trend without a decisive event.\n\n")
	sb.WriteString("## RESPONSE — one JSON object, nothing else\n")
	sb.WriteString("{\n  \"thesis_status\": \"intact\" | \"weakening\" | \"invalidated\",\n  \"invalidation_cited\": \"REQUIRED iff invalidated — quote the stated condition that triggered\",\n  \"structure_conflict\": \"none\" | \"warning\" | \"confirmed\",\n  \"note\": \"one short paragraph\",\n  \"confidence\": 0-100\n}\n")
	sb.WriteString("Any action-like field (action/close/open/etc.) in your response is IGNORED and logged.\n")
	return sb.String()
}

// ParseObserverAssessment extracts the schema-enforced watch response. It never
// invents: a response with no parseable object errors out (the watcher records
// it as unparseable — no rail movement). Action-like fields are flagged.
func ParseObserverAssessment(raw string) (*ObserverAssessment, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object in observer response")
	}
	blob := raw[start : end+1]
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(blob), &probe); err != nil {
		return nil, fmt.Errorf("observer JSON unparseable: %w", err)
	}
	var a ObserverAssessment
	if err := json.Unmarshal([]byte(blob), &a); err != nil {
		return nil, fmt.Errorf("observer schema mismatch: %w", err)
	}
	switch a.ThesisStatus {
	case "intact", "weakening", "invalidated":
	default:
		return nil, fmt.Errorf("thesis_status %q not in enum", a.ThesisStatus)
	}
	switch a.StructureConflict {
	case "", "none", "warning", "confirmed":
		if a.StructureConflict == "" {
			a.StructureConflict = "none"
		}
	default:
		return nil, fmt.Errorf("structure_conflict %q not in enum", a.StructureConflict)
	}
	if a.Confidence < 0 {
		a.Confidence = 0
	}
	if a.Confidence > 100 {
		a.Confidence = 100
	}
	for _, k := range []string{"action", "close", "open", "symbol_action", "order"} {
		if _, has := probe[k]; has {
			a.ActionIgnored = true
		}
	}
	return &a, nil
}

// CallObserver performs ONE model call for a watch cycle (no schema-retry loop —
// an unparseable watch read is recorded and skipped, never retried into cost).
func CallObserver(mcpClient mcp.AIClient, systemPrompt, userPrompt string) (raw string, dur time.Duration, err error) {
	start := time.Now()
	resp, err := mcpClient.CallWithMessages(systemPrompt, userPrompt)
	return resp, time.Since(start), err
}
