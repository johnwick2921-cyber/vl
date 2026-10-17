// Command planner_ab (planner-speed wave phase 2, 2026-08-31) — the OFFLINE
// fast-vs-max A/B: loads the newest VERBATIM rejected planner prompt from the
// rejected-prompt store, calls the provider DIRECTLY (no live system path) in
// reasoning=max vs reasoning=fast (enabled/low), and runs each returned plan
// through the offline schema gate (ParsePlanDocCapped + caps validator).
// Prints a table: mode × wall time × completion tokens × reasoning chars ×
// legal-plan yes/no × first defect. Requires DEEPSEEK_API_KEY in the env.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"nofx/config"
	"nofx/crypto"
	"nofx/kernel"
	"nofx/store"

	"github.com/joho/godotenv"
)

const systemPrompt = "You are a disciplined CME index-futures day-plan reasoner. Output ONLY the single JSON object requested — reasoning first, then the answer fields. No prose outside the JSON."

type abRow struct {
	Mode           string
	WallS          float64
	TTFBS          float64
	CompletionTok  int
	PromptTok      int
	ReasoningChars int
	Finish         string
	Legal          bool
	Defect         string
	Err            string
}

func callProvider(key, mode, effort, prompt string) (abRow, string) {
	body := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.5,
		"max_tokens":  65536,
		"stream":      false,
		"thinking":    map[string]any{"type": mode},
	}
	if effort != "" {
		body["reasoning_effort"] = effort
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", "https://api.deepseek.com/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return abRow{Mode: mode + "/" + effort, Err: err.Error()}, ""
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return abRow{Mode: mode + "/" + effort, WallS: time.Since(start).Seconds(), Err: err.Error()}, ""
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return abRow{Mode: mode + "/" + effort, WallS: time.Since(start).Seconds(), Err: fmt.Sprintf("status %d: %.200s", resp.StatusCode, b)}, ""
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	_ = json.Unmarshal(b, &parsed)
	row := abRow{Mode: mode + "/" + effort, WallS: time.Since(start).Seconds(), PromptTok: parsed.Usage.PromptTokens, CompletionTok: parsed.Usage.CompletionTokens}
	if len(parsed.Choices) > 0 {
		row.ReasoningChars = len(parsed.Choices[0].Message.ReasoningContent)
		if parsed.Choices[0].FinishReason != nil {
			row.Finish = *parsed.Choices[0].FinishReason
		}
		// Offline schema gate: parse + caps, byte-identical to the live loop.
		content := parsed.Choices[0].Message.Content
		if _, perr := kernel.ParsePlanDocCapped(content, 12, 5); perr != nil {
			row.Legal = false
			row.Defect = perr.Error()
		} else {
			row.Legal = true
		}
		return row, content
	}
	return row, ""
}

func main() {
	_ = godotenv.Load()
	config.Init()
	cs, err := crypto.NewCryptoService()
	if err != nil {
		fmt.Fprintln(os.Stderr, "crypto:", err)
		os.Exit(2)
	}
	crypto.SetGlobalCryptoService(cs)

	st, err := store.New(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "store:", err)
		os.Exit(2)
	}
	defer st.Close()
	row, err := st.PlannerRejected().Latest()
	if err != nil || row == nil {
		fmt.Fprintln(os.Stderr, "no stored rejected prompt:", err)
		os.Exit(2)
	}
	model, err := st.AIModel().GetAnyEnabled()
	if err != nil || model == nil {
		fmt.Fprintln(os.Stderr, "no enabled AI model:", err)
		os.Exit(2)
	}
	key := model.APIKey.String()
	if key == "" {
		fmt.Fprintln(os.Stderr, "enabled AI model has no API key")
		os.Exit(2)
	}
	fmt.Printf("model=%s stored prompt: id=%d session=%s attempt=%d chars=%d reason=%s\n",
		model.CustomModelName, row.ID, row.Session, row.Attempt, len(row.PromptText), row.RejectReason)

	var rows []abRow
	for _, cfg := range []struct{ mode, effort, label string }{
		{"enabled", "max", "max"},
		{"enabled", "low", "fast"},
	} {
		fmt.Printf("running %s...\n", cfg.label)
		r, content := callProvider(key, cfg.mode, cfg.effort, row.PromptText)
		r.Mode = cfg.label
		if r.Legal && content != "" {
			// Facts-validator pass is NOT runnable offline (facts are not
			// stored with the prompt — a known gap); schema+parse only.
			fmt.Printf("  %s: legal=%v wall=%.1fs\n", cfg.label, r.Legal, r.WallS)
		}
		rows = append(rows, r)
	}

	fmt.Println("\nmode | wall_s | completion_tok | prompt_tok | reasoning_chars | finish | legal | first_defect")
	for _, r := range rows {
		fmt.Printf("%-5s | %6.1f | %14d | %10d | %15d | %6s | %-5v | %.80s\n",
			r.Mode, r.WallS, r.CompletionTok, r.PromptTok, r.ReasoningChars, r.Finish, r.Legal, r.Defect)
	}
}
