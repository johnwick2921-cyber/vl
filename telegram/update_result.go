package telegram

// W-ADDON-ONE-CLICK "click and walk away" (owner order 2026-10-17): the
// update's RESULT reaches the owner through TWO channels — this Telegram
// message and the page banner (web/src/pages/UpdatesPage.tsx). Both use the
// plan's exact plain words.

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"vl/internal/ownerpush"
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

// ownerSendTimeout bounds EVERY Telegram call the owner/update sender makes
// (getMe at construction and the sendMessage): tgbotapi.NewBotAPI uses an
// http.Client with no timeout, so a peer that accepts and then says nothing
// would hold the single owner-push worker forever (m438). A var so the pin can
// shorten it.
var ownerSendTimeout = 12 * time.Second

// ownerBotEndpoint is the Bot API URL template; a var so the pins can aim the
// REAL sender at a local server.
var ownerBotEndpoint = tgbotapi.APIEndpoint

// botTokenInURL matches the "bot<id>:<secret>" path segment a *url.Error carries
// in its text.
var botTokenInURL = regexp.MustCompile(`bot\d+:[A-Za-z0-9_\-]+`)

// redactTelegramErr renders err for the log with the bot token removed. The
// library returns a *url.Error whose text is the full request URL —
// `Post "https://api.telegram.org/bot<token>/getMe": dial tcp …` — so a plain
// %v of a network failure wrote the live token into the journal (m438).
func redactTelegramErr(err error, token string) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if token != "" {
		msg = strings.ReplaceAll(msg, token, "[redacted]")
	}
	return botTokenInURL.ReplaceAllString(msg, "bot[redacted]")
}

// NotifyUpdateResult sends one owner-facing update result to the owner's bound
// Telegram chat (the bot's existing sender). Unbound, no token, or no text is
// a no-op — the message is advisory; the page banner is the authoritative
// surface. It resolves the token and the bound chat from the store at call
// time, never caches them. Every call is time-bounded (ownerSendTimeout) and
// every logged error has the bot token removed.
func NotifyUpdateResult(st *store.Store, text string) {
	_ = SendOwnerText(st, text) // advisory: the update page banner is authoritative
}

// SendOwnerText is the sender behind NotifyUpdateResult and NotifyOwner, and it
// RETURNS the outcome (m442 N4: the old void sender made a failed getMe /
// sendMessage / timeout indistinguishable from a delivery, so a lost owner push
// was never retried). nil = delivered; ownerpush.ErrNotBound = no token, no
// bound chat or no text (nothing to send, nothing to retry); any other error =
// the send failed and is token-free (redactTelegramErr).
func SendOwnerText(st *store.Store, text string) error {
	if text == "" {
		return ownerpush.ErrNotBound
	}
	dbCfg, err := st.TelegramConfig().Get()
	if err != nil || dbCfg.BotToken == "" {
		logger.Debugf("telegram update result: bot disabled (no token) - not sent")
		return ownerpush.ErrNotBound
	}
	chatID, err := st.TelegramConfig().GetBoundChatID()
	if err != nil || chatID == 0 {
		logger.Debugf("telegram update result: no bound chat - not sent")
		return ownerpush.ErrNotBound
	}
	token := dbCfg.BotToken
	bot, err := tgbotapi.NewBotAPIWithClient(token, ownerBotEndpoint, &http.Client{Timeout: ownerSendTimeout})
	if err != nil {
		msg := redactTelegramErr(err, token)
		logger.Errorf("telegram update result: bot api: %s", msg)
		return fmt.Errorf("telegram bot api: %s", msg)
	}
	if _, err := bot.Send(tgbotapi.NewMessage(chatID, text)); err != nil {
		msg := redactTelegramErr(err, token)
		logger.Errorf("telegram update result: send: %s", msg)
		return fmt.Errorf("telegram send: %s", msg)
	}
	return nil
}
