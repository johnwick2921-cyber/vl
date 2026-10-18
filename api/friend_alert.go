package api

// W-FRIEND-HOSTED Segment 6 (DS-106): owner-facing Telegram alerts for friend
// problems (bot down, NT disconnected > 5 min, add-on out of date) AND the
// relay of DS-105's friend update receipts (CTO ruling 2026-10-18 item 3: the
// updater keeps the job file as the durable record and never opens the owner
// DB; this monitor only READS the receipts and sends them through the owner's
// Telegram).
//
// The send path mirrors telegram/update_result.go's sender (resolve the token
// + bound chat at call time, never cache, no-op when unbound) but lives in
// package api because api cannot import the telegram package (telegram/bot.go
// imports api, which would cycle).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vl/internal/updaterjob"
	"vl/logger"
	"vl/store"
	"vl/trader"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// notifyFriendAlert sends one owner-facing friend alert to the owner's bound
// Telegram chat. Empty text, no bot token, or no bound chat is a no-op — the
// Friends page is the authoritative surface; this is advisory.
func notifyFriendAlert(st *store.Store, text string) {
	if text == "" || st == nil {
		return
	}
	dbCfg, err := st.TelegramConfig().Get()
	if err != nil || dbCfg.BotToken == "" {
		logger.Debugf("friend alert: bot disabled (no token) — not sent")
		return
	}
	chatID, err := st.TelegramConfig().GetBoundChatID()
	if err != nil || chatID == 0 {
		logger.Debugf("friend alert: no bound chat — not sent")
		return
	}
	bot, err := tgbotapi.NewBotAPI(dbCfg.BotToken)
	if err != nil {
		logger.Errorf("friend alert: bot api: %v", err)
		return
	}
	if _, err := bot.Send(tgbotapi.NewMessage(chatID, text)); err != nil {
		logger.Errorf("friend alert: send: %v", err)
	}
}

// ── relay of DS-105's friend update receipts ────────────────────────────────
//
// The updater worker persists per-friend outcomes in the job file as
// friends:[{name,state,rev,note}] (B4 Segment 5; CTO ruling 2026-10-18 item 3).
// This relay reads the RAW job file JSON for that one key — deliberately NOT
// through updaterjob.Read's strict decoder, so the relay never depends on the
// updater job schema's exact field set and stays correct whichever merge order
// the integrator picks.

// friendJobReceipt is the contract shape of one friends[] entry.
type friendJobReceipt struct {
	Name  string `json:"name"`
	State string `json:"state"`
	Rev   string `json:"rev"`
	Note  string `json:"note,omitempty"`
}

// friendJobProbe is the tolerant subset of a job file this relay reads.
type friendJobProbe struct {
	JobID     string             `json:"job_id"`
	CreatedAt time.Time          `json:"created_at"`
	Friends   []friendJobReceipt `json:"friends"`
}

// latestFriendJobReceipts returns the newest job file's friends[] receipts, or
// nil when there is no data dir, no jobs dir, or no job carries receipts.
// Read-only: it never writes and never opens the database.
func latestFriendJobReceipts() []friendJobReceipt {
	dataDir := trader.MaintenanceDataDir()
	if dataDir == "" {
		return nil
	}
	jobsDir, err := updaterjob.JobsDir(dataDir)
	if err != nil {
		return nil
	}
	ents, err := os.ReadDir(jobsDir)
	if err != nil {
		return nil
	}
	var best *friendJobProbe
	for _, ent := range ents {
		name := ent.Name()
		if name == ".jobs.lock" || (strings.HasPrefix(name, ".job-") && strings.HasSuffix(name, ".tmp")) {
			continue
		}
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(jobsDir, name))
		if err != nil {
			continue
		}
		var p friendJobProbe
		if err := json.Unmarshal(b, &p); err != nil {
			continue
		}
		if len(p.Friends) == 0 {
			continue
		}
		if best == nil || p.CreatedAt.After(best.CreatedAt) {
			best = &p
		}
	}
	if best == nil {
		return nil
	}
	return best.Friends
}

// friendReceiptMessage renders one receipt in the plan's plain words. A
// "skipped" (paused) friend and an unknown state render "" — nothing is
// fabricated, and the caller must not send an empty message.
func friendReceiptMessage(rc friendJobReceipt) string {
	switch rc.State {
	case "restarted":
		return fmt.Sprintf("✅ Friend %q updated — now running %s.", rc.Name, rc.Rev)
	case "deferred":
		msg := fmt.Sprintf("⏳ Friend %q update deferred (not flat).", rc.Name)
		if rc.Note != "" {
			msg += " " + rc.Note
		}
		return msg
	case "restored-restart":
		return fmt.Sprintf("⚠️ Friend %q update recovered with an extra restart (%s).", rc.Name, rc.Rev)
	case "failed":
		msg := fmt.Sprintf("⚠️ Friend %q update failed.", rc.Name)
		if rc.Note != "" {
			msg += " " + rc.Note
		}
		return msg
	default:
		return ""
	}
}

// relayFriendReceipts returns the owner-facing messages for friend update
// receipts NOT yet relayed (deduped in-memory per friend on the receipt's
// key). Pure except for the in-memory dedup map.
func (s *Server) relayFriendReceipts() []string {
	var msgs []string
	for _, rc := range latestFriendJobReceipts() {
		key := fmt.Sprintf("%s|%s|%s|%s", rc.Name, rc.State, rc.Rev, rc.Note)
		stt := s.friendAlertStateFor(rc.Name)
		if key == stt.lastRelayedReceipt {
			continue
		}
		if msg := friendReceiptMessage(rc); msg != "" {
			stt.lastRelayedReceipt = key
			msgs = append(msgs, msg)
		}
	}
	return msgs
}
