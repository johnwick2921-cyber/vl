package telegram

// W-ADDON-ONE-CLICK "click and walk away" (owner order 2026-10-17): the
// update's RESULT reaches the owner through TWO channels — this Telegram
// message and the page banner (web/src/pages/UpdatesPage.tsx). Both use the
// plan's exact plain words.

import (
	"vl/logger"
	"vl/store"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// UpdateResultMessage renders the owner-facing result of an update job in the
// plan's plain words (PLAN-update-walk-away.md H4 + self-recovery). `state`
// is the worker's terminal state string ("complete", "rolled_back",
// "recovery_needed", or the worker's "reverted_clean"). Empty tags/reasons
// render as empty; an unknown state renders "" (the caller must not send an
// empty message — nothing is fabricated).
func UpdateResultMessage(state, newTag, oldTag, reason string) string {
	switch state {
	case "complete":
		return "Update done — running " + newTag
	case "rolled_back", "reverted_clean", "refused":
		// refused and reverted_clean both mean "nothing was swapped, the old
		// build never moved" — the same plain words as a proven rollback.
		msg := "Update could not finish — nothing changed, still running " + oldTag + ", trading normal."
		if reason != "" {
			msg += " Reason: " + reason
		}
		return msg
	case "recovery_needed":
		msg := "⚠️ Update needs your attention — the automatic recovery failed."
		if reason != "" {
			msg += " Reason: " + reason
		}
		return msg
	default:
		return ""
	}
}

// NotifyUpdateResult sends one owner-facing update result to the owner's bound
// Telegram chat (the bot's existing sender). Unbound, no token, or no text is
// a no-op — the message is advisory; the page banner is the authoritative
// surface. It resolves the token and the bound chat from the store at call
// time, never caches them.
func NotifyUpdateResult(st *store.Store, text string) {
	if text == "" {
		return
	}
	dbCfg, err := st.TelegramConfig().Get()
	if err != nil || dbCfg.BotToken == "" {
		logger.Debugf("telegram update result: bot disabled (no token) — not sent")
		return
	}
	chatID, err := st.TelegramConfig().GetBoundChatID()
	if err != nil || chatID == 0 {
		logger.Debugf("telegram update result: no bound chat — not sent")
		return
	}
	bot, err := tgbotapi.NewBotAPI(dbCfg.BotToken)
	if err != nil {
		logger.Errorf("telegram update result: bot api: %v", err)
		return
	}
	if _, err := bot.Send(tgbotapi.NewMessage(chatID, text)); err != nil {
		logger.Errorf("telegram update result: send: %v", err)
	}
}
