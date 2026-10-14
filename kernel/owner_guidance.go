package kernel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"
)

// R22 R4 — THE STUDIO PROMPT REACHES THE PLANNER (owner ruling, 2026-10-14).
//
// The owner's Studio custom_prompt ("No trade inside sideway zone … respect the
// stop loss") was injected only into the EXECUTOR prompts
// (engine_prompt_futures.go "# Personalized Strategy", engine_prompt.go). Under
// plan_mode strict only the PLANNER creates entries, and PlannerInput.OwnerNote
// had no producer anywhere — the planner never read a word of it.
//
// The planner now carries it as PlannerInput.OwnerNote, rendered as a clearly
// delimited OWNER GUIDANCE block. The write site logs only its length and a
// short hash (OwnerGuidanceFingerprint) — never the text.

const (
	ownerGuidanceOpen  = "<<<OWNER_GUIDANCE"
	ownerGuidanceClose = "OWNER_GUIDANCE>>>"
)

// OwnerGuidanceBlock renders the owner's Studio custom prompt for the planner.
// The markers are stripped from the owner's text so the block cannot be closed
// early by its own content.
func OwnerGuidanceBlock(note string) string {
	note = strings.TrimSpace(strings.NewReplacer(ownerGuidanceOpen, "", ownerGuidanceClose, "").Replace(note))
	if note == "" {
		return ""
	}
	return "## OWNER GUIDANCE (the owner's Studio custom prompt — standing instructions for this strategy)\n" +
		"The text between the markers is the OWNER's guidance on which setups to author and which to avoid; weigh it when you choose scenarios. " +
		"It never overrides the machine rules in this prompt: the JSON contract, the validator, the R:R floor and the risk gates still decide.\n" +
		ownerGuidanceOpen + "\n" + note + "\n" + ownerGuidanceClose + "\n\n"
}

// OwnerGuidanceInPrompt returns the guidance text a rendered planner prompt
// carried (ok=false when the prompt carried none).
func OwnerGuidanceInPrompt(prompt string) (string, bool) {
	i := strings.Index(prompt, ownerGuidanceOpen+"\n")
	if i < 0 {
		return "", false
	}
	rest := prompt[i+len(ownerGuidanceOpen)+1:]
	j := strings.Index(rest, "\n"+ownerGuidanceClose)
	if j < 0 {
		return "", false
	}
	return rest[:j], true
}

// OwnerGuidanceFingerprint is the loggable read of what the planner was TOLD:
// the character length and a short sha256 of the guidance inside the rendered
// prompt — never the text itself. A read value, never a literal: a prompt with
// no guidance says so.
func OwnerGuidanceFingerprint(prompt string) string {
	g, ok := OwnerGuidanceInPrompt(prompt)
	if !ok {
		return "none (the planner prompt carried no Studio custom prompt)"
	}
	sum := sha256.Sum256([]byte(g))
	return fmt.Sprintf("len=%d chars sha256=%s", utf8.RuneCountInString(g), hex.EncodeToString(sum[:6]))
}
