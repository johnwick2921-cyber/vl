package api

// W-FRIEND-HOSTED Segment 6 (DS-106): owner-facing Telegram alerts for friend
// problems (bot down, NT disconnected > 5 min, add-on out of date).
//
// This mirrors telegram/update_result.go's sender (resolve the token + bound
// chat at call time, never cache, no-op when unbound) but lives in package api
// because api cannot import the telegram package (telegram/bot.go imports api,
// which would cycle).

import (
	"vl/logger"
	"vl/store"

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
