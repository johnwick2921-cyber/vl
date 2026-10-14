package telegram

import (
	"os"
	"strings"
	"sync"
	"time"
	"vl/api"
	"vl/branding"
	"vl/config"
	"vl/logger"
	"vl/mcp"
	_ "vl/mcp/provider"
	"vl/safe"
	"vl/store"
	"vl/telegram/agent"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Start initializes and runs the Telegram bot in a blocking supervisor loop.
// Supports hot-reload: when a signal is sent on reloadCh, the bot restarts
// with the latest token (re-read from DB or env). Must be called as a goroutine from main.go.
func Start(cfg *config.Config, st *store.Store, reloadCh <-chan struct{}) {
	for {
		token := resolveToken(cfg, st)
		if token == "" {
			logger.Info("Telegram bot disabled (no token configured), waiting for reload signal...")
			<-reloadCh
			continue
		}

		stopped := runBot(token, cfg, st)
		if !stopped {
			return
		}

		select {
		case <-reloadCh:
			logger.Info("Reloading Telegram bot with new token...")
		}
	}
}

// resolveToken returns the bot token from DB (configured via Web UI).
func resolveToken(cfg *config.Config, st *store.Store) string {
	dbCfg, err := st.TelegramConfig().Get()
	if err == nil && dbCfg.BotToken != "" {
		return dbCfg.BotToken
	}
	return ""
}

// runBot runs the bot until the updates channel closes (clean stop → true) or a fatal error (false).
func runBot(token string, cfg *config.Config, st *store.Store) bool {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		logger.Errorf("Telegram bot failed to start: %v", err)
		return false
	}
	logger.Infof("Telegram bot @%s started", bot.Self.UserName)

	// Allowed chat ID: read from DB binding (0 = unbound, first /start will bind).
	allowedChatID := int64(0)
	if id, err := st.TelegramConfig().GetBoundChatID(); err == nil && id != 0 {
		allowedChatID = id
	}

	// The bot's account binding — user, JWT, agent manager — resolved lazily;
	// ident.refresh() re-reads it (and re-mints a token the API would refuse).
	ident := newBotIdentity(st, cfg.APIServerPort)
	ident.refresh()

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	// awaitingLang is set only when the user explicitly runs /lang.
	awaitingLang := false

	for update := range updates {
		if update.Message == nil {
			continue
		}
		chatID := update.Message.Chat.ID
		text := strings.TrimSpace(update.Message.Text)

		// ── Language selection (triggered only by /lang) ──────────────────────
		if awaitingLang && chatID == allowedChatID {
			if lang := parseLangChoice(text); lang != "" {
				awaitingLang = false
				st.TelegramConfig().SetLanguage(lang) //nolint:errcheck
				sendMarkdownMsg(bot, chatID, statusMsg(st, ident.userID, cfg.APIServerPort, lang))
			} else {
				sendMarkdownMsg(bot, chatID, langMenuMsg())
			}
			continue
		}

		// ── /start ────────────────────────────────────────────────────────────
		if text == "/start" {
			ident.refresh()
			if ident.userID == "" {
				sendMsg(bot, chatID, ident.offMsg(
					"No account found.\nOpen the web dashboard to register, then send /start."))
				continue
			}
			if allowedChatID == 0 {
				// P2-12 (audit 0926-system): the first /start no longer binds
				// blindly — the chat must send back the one-time code issued in
				// the owner-authenticated app.
				sendMsg(bot, chatID,
					"Binding requires a one-time code.\nIn the web dashboard (owner session) open the Telegram bind dialog, copy the code, then send it here as:\n\n/bind <code>")
				continue
			} else if chatID != allowedChatID {
				sendMsg(bot, chatID, "This bot is already bound to another account.")
				continue
			} else {
				ident.agents.Reset(chatID)
			}
			lang := st.TelegramConfig().GetLanguage()
			sendMarkdownMsg(bot, chatID, statusMsg(st, ident.userID, cfg.APIServerPort, lang))
			continue
		}

		// ── /bind <code> ─────────────────────────────────────────────────────
		// P2-12: consume the one-time code; on a match this chat becomes the
		// bound account.
		if allowedChatID == 0 && strings.HasPrefix(text, "/bind ") {
			code := strings.TrimSpace(strings.TrimPrefix(text, "/bind "))
			if code == "" {
				sendMsg(bot, chatID, "Usage: /bind <code>")
				continue
			}
			ok, err := st.TelegramConfig().ConsumeBindCode(code)
			if err != nil {
				logger.Errorf("Failed to verify Telegram bind code: %v", err)
				sendMsg(bot, chatID, "Bind code check failed. Please try again.")
				continue
			}
			if !ok {
				sendMsg(bot, chatID, "Invalid or expired bind code.\nIssue a new one from the dashboard, then send /bind <code>.")
				continue
			}
			ident.refresh()
			if ident.userID == "" {
				sendMsg(bot, chatID, ident.offMsg("No account found.\nOpen the web dashboard to register, then try /bind again."))
				continue
			}
			username := update.Message.From.UserName
			if err := st.TelegramConfig().BindUser(chatID, "@"+username); err != nil {
				logger.Errorf("Failed to bind Telegram user: %v", err)
				sendMsg(bot, chatID, "Binding failed. Please try again.")
				continue
			}
			allowedChatID = chatID
			logger.Infof("Telegram bound to @%s (chatID: %d) via one-time code", username, chatID)
			sendMsg(bot, chatID, "Bound successfully. Welcome.")
			continue
		}

		// ── /lang ─────────────────────────────────────────────────────────────
		if text == "/lang" {
			awaitingLang = true
			sendMarkdownMsg(bot, chatID, langMenuMsg())
			continue
		}

		// ── /help ─────────────────────────────────────────────────────────────
		if text == "/help" {
			lang := st.TelegramConfig().GetLanguage()
			sendMarkdownMsg(bot, chatID, helpMsg(lang))
			continue
		}

		// ── Access control ────────────────────────────────────────────────────
		if allowedChatID != 0 && chatID != allowedChatID {
			sendMsg(bot, chatID, "Unauthorized.")
			continue
		}
		if allowedChatID == 0 {
			sendMsg(bot, chatID, "Send /start first.")
			continue
		}
		if text == "" {
			continue
		}

		// ── Refresh user before every AI call ────────────────────────────────
		ident.refresh()
		if ident.userID == "" {
			sendMsg(bot, chatID, ident.offMsg("No account found. Open the web dashboard to register."))
			continue
		}

		lang := st.TelegramConfig().GetLanguage()

		// ── Guard: show status if not ready for trading ───────────────────────
		if newLLMClient(st, ident.userID) == nil {
			sendMarkdownMsg(bot, chatID, statusMsg(st, ident.userID, cfg.APIServerPort, lang))
			continue
		}

		// ── AI agent ─────────────────────────────────────────────────────────
		// The manager is captured HERE, on the main loop, and handed in: the
		// next message's refresh() may replace ident.agents / ident.token
		// while this one is still being answered, so the goroutine reads no
		// field of ident (M3 ha verifier 3 — TestRunBotGoroutinesReadNoBotIdentityField).
		// A message in flight across a re-mint finishes on the manager, and
		// the token, it started with.
		agents := ident.agents
		safe.GoNet("tg-message-handler", "", func() {
			sent, err := bot.Send(tgbotapi.NewMessage(chatID, "⏳"))
			placeholderID := 0
			if err == nil {
				placeholderID = sent.MessageID
			}

			var (
				mu       sync.Mutex
				lastEdit time.Time
			)
			onChunk := func(accumulated string) {
				if placeholderID == 0 {
					return
				}
				mu.Lock()
				defer mu.Unlock()
				if accumulated != "⏳" && time.Since(lastEdit) < time.Second {
					return
				}
				lastEdit = time.Now()
				edit := tgbotapi.NewEditMessageText(chatID, placeholderID, accumulated)
				bot.Send(edit) //nolint:errcheck
			}

			reply := agents.Run(chatID, text, onChunk)

			if placeholderID != 0 {
				edit := tgbotapi.NewEditMessageText(chatID, placeholderID, reply)
				edit.ParseMode = "Markdown"
				if _, err := bot.Send(edit); err != nil {
					edit2 := tgbotapi.NewEditMessageText(chatID, placeholderID, reply)
					bot.Send(edit2) //nolint:errcheck
				}
			} else {
				msg := tgbotapi.NewMessage(chatID, reply)
				msg.ParseMode = "Markdown"
				if _, err := bot.Send(msg); err != nil {
					msg.ParseMode = ""
					bot.Send(msg) //nolint:errcheck
				}
			}
		})
	}

	return true
}

// botIdentity is the bot's account binding: the user it acts for, the JWT
// its agent's API tool calls with, and the agent manager built on that
// token. runBot calls refresh() at start, on /start and before every AI
// call — so the M3 red-team H2 re-mint (botTokenStale) is exercised where
// runBot reaches it (bot_token_test.go drives refresh against the
// production server). Its fields belong to runBot's main loop: refresh
// reassigns them, so the per-message goroutine gets the manager as a value
// captured before the go statement, and the closures refresh builds read
// locals, never the receiver (bot_goroutine_test.go pins both).
type botIdentity struct {
	st      *store.Store
	apiPort int
	userID  string
	email   string
	token   string
	agents  *agent.Manager
	// noOwner is why the last refresh resolved NO owner ("" = it did, or no
	// refresh ran yet); offMsg reads it on the main loop.
	noOwner string
}

func newBotIdentity(st *store.Store, apiPort int) *botIdentity {
	return &botIdentity{st: st, apiPort: apiPort}
}

// botSleep is the wait before refresh's one extra mint (PR #200 F7): a
// package variable so the pins drive that path without sleeping. It is
// called on runBot's main loop only.
var botSleep = time.Sleep

// botNow is the clock refresh computes that wait from (CTO 1790255882118): a
// seam, like botSleep, so the same-second pins step the clock
// deterministically instead of racing the machine's real second boundary. The
// mint's own iat stays time.Now() — auth pins that
// (TestServerNeverMintsAFutureIat, TestEveryMintEntryPointStampsNowNotTheFuture).
var botNow = time.Now

// botMint mints the bot's token. Production is agent.GenerateBotToken, whose
// iat is time.Now() (auth pins that); the same-second pins replace it with a
// mint stamped from botNow, so the whole-second boundary is the test's, not
// the machine's (CTO 1790255882118). refresh is its only caller
// (TestRunBotMintsOnlyThroughRefresh), and the production value is pinned by
// TestBotClockSeamsAreTheRealClockInProduction.
var botMint = agent.GenerateBotToken

// botNextSecondMargin is slack past the whole-second boundary, so a wall
// clock slewed a few milliseconds behind the monotonic one still reads the
// next second when the wait ends.
const botNextSecondMargin = 10 * time.Millisecond

// botUntilNextSecond is how long to wait from now until the wall clock is in
// the next whole second — the resolution of a JWT's iat (golang-jwt
// NumericDate) and of the API's retire rule (auth.RetiredBy).
func botUntilNextSecond(now time.Time) time.Duration {
	return now.Truncate(time.Second).Add(time.Second + botNextSecondMargin).Sub(now)
}

// refresh re-reads the box OWNER — store.ResolveOwner, the one resolver the
// owner-only API routes use too (owner-identity 2026-10-14; it was users[0],
// the first-created account, which on the live box is a test account that
// owns no trader) — and, when the owner changed or the current token would be
// refused, mints a new token and rebuilds the agent manager on it. false = no
// owner resolved (the bot then acts for NOBODY: every field cleared, nothing
// minted, the refusal logged and counted), or the mint failed, or no token
// the API would admit could be minted (PR #200 F7) — and on false nothing is
// installed.
func (b *botIdentity) refresh() bool {
	o := b.st.ResolveOwner()
	if !o.Resolved() {
		store.RecordOwnerRefusal("Telegram bot identity (no token minted)", o)
		b.actForNobody(o.Reason)
		return false
	}
	b.noOwner = ""
	u := *o.User
	source := o.Source
	// M3 red-team H2: the API refuses a token issued at or before the
	// account's last credential change on every route, so the SAME user
	// also re-mints when its token would be refused (botTokenStale).
	if u.ID == b.userID && !botTokenStale(b.token, u) {
		return true
	}
	// PR #200 F7 (CTO bridge msg 1790252194343, #22): a JWT's iat is whole
	// seconds, so a mint in the SAME second as the password change is
	// retired at birth (auth.RetiredBy), and one in the same second as a
	// blacklisted token's own mint is that token, byte for byte (F4b).
	// Re-check the fresh token with the predicate that sent us here; if the
	// API would refuse it, wait to the next whole second and mint ONCE more.
	// Still refused (a wall-clock step back, an epoch ahead of the clock) →
	// fail closed: false, nothing installed — refresh never reports success
	// with a token the API refuses.
	var newToken string
	for attempt := 1; ; attempt++ {
		tok, err := botMint(u.ID)
		if err != nil {
			logger.Errorf("Failed to generate bot JWT for user %s: %v", u.ID, err)
			b.dropIfUserChanged(u.ID)
			return false
		}
		if !botTokenStale(tok, u) {
			newToken = tok
			break
		}
		if attempt == 2 {
			logger.Warnf("Bot: a token minted for %s after waiting to the next second would still be refused (credential epoch not behind the clock?) — not installed; the next message retries", u.ID)
			b.dropIfUserChanged(u.ID)
			return false
		}
		botSleep(botUntilNextSecond(botNow()))
	}
	prev := b.userID
	b.userID = u.ID
	b.email = u.Email
	b.token = newToken
	// The LLM factory runs on the per-message goroutine (Manager.Run →
	// agent.New / Agent.Run) while the next refresh rewrites b's fields on
	// the main loop: it closes over locals, never b.<field>
	// (TestBotIdentityClosuresReadNoReceiverField).
	st, userID := b.st, b.userID
	b.agents = agent.NewManager(b.apiPort, b.token, b.email, b.userID,
		func() mcp.AIClient { return newLLMClient(st, userID) },
		api.GetAPIDocs(),
	)
	// The email is never logged (owner-identity): the id and the rule that
	// resolved it are enough to say who the bot acts for.
	switch prev {
	case "":
		logger.Infof("Bot: resolved user %s (owner source %s)", b.userID, source)
	case b.userID:
		logger.Infof("Bot: token re-minted for %s — the previous one would be refused (credential change or expiry)", b.userID)
	default:
		logger.Infof("Bot: user changed → %s (owner source %s)", b.userID, source)
	}
	return true
}

// dropIfUserChanged is refresh's fail-closed path on a USER change: when the
// box's owner is no longer the one the bot holds a token for, and no token
// the API admits could be minted for the new one, the bot acts for NOBODY —
// never for the previous user, whose token may still be admitted (PR #200 F7
// verify note 3; TestBotRefreshFailingClosedOnAUserChangeActsForNobody). The
// same user keeps its fields: its token is the one being replaced, and the
// next message retries.
func (b *botIdentity) dropIfUserChanged(ownerID string) {
	if b.userID == ownerID {
		return
	}
	if b.userID != "" {
		logger.Warnf("Bot: owner changed %s → %s and no admitted token could be minted — acting for no account until a refresh succeeds", b.userID, ownerID)
	}
	b.userID, b.email, b.token, b.agents = "", "", "", nil
}

// actForNobody is refresh's fail-closed path when NO owner resolves (traders
// split across users, the traders' user missing, no users, a store error):
// the bot holds no identity at all — never the previous owner's, whose token
// may still be admitted — until an owner resolves again
// (TestBotRefreshActsForNobodyWhenNoOwnerResolves).
func (b *botIdentity) actForNobody(reason string) {
	if b.userID != "" {
		logger.Warnf("Bot: no owner resolved — dropping the identity of %s; acting for no account until an owner resolves", b.userID)
	}
	b.userID, b.email, b.token, b.agents = "", "", "", nil
	b.noOwner = reason
}

// offMsg is what a chat is told while the bot acts for nobody: the
// registration hint when the box simply has no account yet, otherwise that no
// owner resolved (the reason itself stays in the log — an unbound chat may be
// anyone).
func (b *botIdentity) offMsg(registerHint string) string {
	if b.noOwner == "" || b.noOwner == store.OwnerReasonNoUsers {
		return registerHint
	}
	return "No owner account is resolved, so the bot is off.\nThe 👤 owner line in the VL log says why."
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func sendMsg(bot *tgbotapi.BotAPI, chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	bot.Send(msg) //nolint:errcheck
}

func sendMarkdownMsg(bot *tgbotapi.BotAPI, chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	if _, err := bot.Send(msg); err != nil {
		plain := tgbotapi.NewMessage(chatID, text)
		bot.Send(plain) //nolint:errcheck
	}
}

// ── LLM client ───────────────────────────────────────────────────────────────

func newLLMClient(st *store.Store, userID string) mcp.AIClient {
	// 1. Prefer the model explicitly configured for Telegram (Settings → Telegram → AI Model)
	if tgCfg, err := st.TelegramConfig().Get(); err == nil && tgCfg.ModelID != "" {
		if model, err := st.AIModel().Get(userID, tgCfg.ModelID); err == nil && model.Enabled {
			apiKey := string(model.APIKey)
			if apiKey != "" {
				client := clientForProvider(model.Provider)
				client.SetAPIKey(apiKey, model.CustomAPIURL, model.CustomModelName)
				logger.Infof("Telegram agent: provider=%s user=%s", model.Provider, userID)
				return client
			}
		}
	}

	// 2. Fall back to first enabled model
	if model, err := st.AIModel().GetDefault(userID); err == nil {
		apiKey := string(model.APIKey)
		if apiKey != "" {
			client := clientForProvider(model.Provider)
			client.SetAPIKey(apiKey, model.CustomAPIURL, model.CustomModelName)
			logger.Infof("Telegram agent: provider=%s user=%s", model.Provider, userID)
			return client
		}
	}

	// 3. Environment variable fallback
	for _, pair := range []struct{ provider, key, url string }{
		{"deepseek", os.Getenv("DEEPSEEK_API_KEY"), mcp.DefaultDeepSeekBaseURL},
		{"openai", os.Getenv("OPENAI_API_KEY"), ""},
		{"claude", os.Getenv("ANTHROPIC_API_KEY"), ""},
	} {
		if pair.key != "" {
			client := clientForProvider(pair.provider)
			client.SetAPIKey(pair.key, pair.url, "")
			return client
		}
	}
	return nil
}

func clientForProvider(provider string) mcp.AIClient {
	client := mcp.NewAIClientByProvider(provider)
	if client == nil {
		client = mcp.NewAIClientByProvider("deepseek")
	}
	return client
}

// ── Status message ────────────────────────────────────────────────────────────

// statusMsg is the single entry-point message shown after /start.
// It checks what's configured and shows either a setup prompt or the ready state.
func statusMsg(st *store.Store, userID string, apiPort int, lang string) string {
	webURL := "http://localhost:3000"

	// Determine what's missing.
	hasModel := false
	if _, err := st.AIModel().GetDefault(userID); err == nil {
		hasModel = true
	}

	hasExchange := false
	if exchanges, err := st.Exchange().List(userID); err == nil {
		for _, e := range exchanges {
			if e.Enabled {
				hasExchange = true
				break
			}
		}
	}

	if !hasModel || !hasExchange {
		missing := ""
		if lang == "zh" {
			if !hasModel {
				missing += "\n❌ AI 模型 → 设置 → AI 模型 → 添加"
			}
			if !hasExchange {
				missing += "\n❌ 交易所 → 设置 → 交易所 → 添加"
			}
			return "⚙️ *需要完成初始配置*\n\n打开 Web 管理界面完成配置：\n→ " + webURL + "\n" + missing + "\n\n配置完成后发送 /start"
		}
		if !hasModel {
			missing += "\n❌ AI Model → Settings → AI Models → Add"
		}
		if !hasExchange {
			missing += "\n❌ Exchange → Settings → Exchanges → Add"
		}
		return "⚙️ *Setup required*\n\nOpen the web dashboard to complete setup:\n→ " + webURL + "\n" + missing + "\n\nSend /start when done."
	}

	// All configured — show ready state.
	if lang == "zh" {
		return `✅ *` + branding.ProductName() + ` 就绪，开始交易吧！*

直接告诉我你想做什么：

📊 "查看我的持仓"
💰 "账户余额多少"
🤖 "帮我创建 MNQ 趋势策略并启动"
⏹ "停止所有交易员"

/help 查看更多 · /lang 切换语言`
	}
	return `✅ *` + branding.ProductName() + ` is ready!*

Just tell me what you want:

📊 "Show my positions"
💰 "What's my balance?"
🤖 "Create an MNQ trend strategy and start it"
⏹ "Stop all traders"

/help for more · /lang to change language`
}

// ── Language ──────────────────────────────────────────────────────────────────

func langMenuMsg() string {
	return "🌐 *Choose your language*\n\n1 — English\n2 — 中文\n\nReply with 1 or 2"
}

func parseLangChoice(text string) string {
	switch strings.TrimSpace(text) {
	case "1", "en", "EN", "English", "english":
		return "en"
	case "2", "zh", "ZH", "中文", "chinese", "Chinese":
		return "zh"
	}
	return ""
}

// ── Help ──────────────────────────────────────────────────────────────────────

func helpMsg(lang string) string {
	if lang == "zh" {
		return `*` + branding.ProductName() + ` 使用指南*

*查询*
• "查看我的持仓"
• "账户余额多少"
• "列出我的交易员"

*创建 & 启动*
• "帮我创建 MNQ 趋势策略并跑起来"
• "保守型策略，只交易 MNQ"

*控制*
• "启动交易员"
• "暂停交易员"
• "停止所有交易"

*命令*
/start — 刷新状态
/lang  — 切换语言
/help  — 帮助`
	}
	return `*` + branding.ProductName() + ` Help*

*Query*
• "Show my positions"
• "What's my balance?"
• "List my traders"

*Create & start*
• "Create an MNQ trend strategy and start it"
• "Conservative strategy, MNQ only"

*Control*
• "Start trader"
• "Stop trader"
• "Stop all trading"

*Commands*
/start — refresh status
/lang  — change language
/help  — show this`
}
