package kernel

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"vl/logger"
	"vl/market"
	"vl/store"
)

// ============================================================================
// Prompt Building - System Prompt
// ============================================================================

// BuildSystemPrompt builds System Prompt according to strategy configuration
func (e *StrategyEngine) BuildSystemPrompt(accountEquity float64, variant, symbol string) string {
	// CME futures use a dedicated futures system prompt that emits the SAME
	// <reasoning>/<decision> envelope this parser expects, but with futures
	// framing for the ACTIVE symbol. Early-return keeps the crypto assembly below
	// untouched (symbol is ignored on the crypto path).
	// "futures", "futures-balanced", "futures-aggressive", "futures-conservative"
	// all route to the futures builder; the suffix selects the sub-mode (balanced
	// = no extra block = byte-identical default). Crypto variants fall through.
	if isFut, mode := futuresVariantMode(variant); isFut {
		return e.buildFuturesPrompt(symbol, accountEquity, mode)
	}

	var sb strings.Builder
	riskControl := e.config.RiskControl
	promptSections := e.config.PromptSections

	// 0. Data Dictionary & Schema (ensure AI understands all fields)
	lang := e.GetLanguage()
	schemaPrompt := GetSchemaPrompt(lang)
	sb.WriteString(schemaPrompt)
	sb.WriteString("\n\n")
	sb.WriteString("---\n\n")

	// P0 timezone fix — the labelled clock goes FIRST, before any window
	// bound, so the model can never pair an unlabelled window with a UTC
	// clock (owner rule: CT is canonical everywhere).
	if e.clockContextLine != "" {
		sb.WriteString(e.clockContextLine)
		sb.WriteString("\n\n")
	}

	// 1. Role definition (editable)
	if promptSections.RoleDefinition != "" {
		sb.WriteString(promptSections.RoleDefinition)
		sb.WriteString("\n\n")
	} else {
		sb.WriteString("# You are a professional cryptocurrency trading AI\n\n")
		sb.WriteString("Your task is to make trading decisions based on provided market data.\n\n")
	}

	// 2. Trading mode variant
	switch strings.ToLower(strings.TrimSpace(variant)) {
	case "aggressive":
		sb.WriteString("## Mode: Aggressive\n- Prioritize capturing trend breakouts, can build positions in batches when confidence ≥ 70\n- Allow higher positions, but must strictly set stop-loss and explain risk-reward ratio\n\n")
	case "conservative":
		sb.WriteString("## Mode: Conservative\n- Only open positions when multiple signals resonate\n- Prioritize cash preservation, must pause for multiple periods after consecutive losses\n\n")
	case "scalping":
		sb.WriteString("## Mode: Scalping\n- Focus on short-term momentum, smaller profit targets but require quick action\n- If price doesn't move as expected within two bars, immediately reduce position or stop-loss\n\n")
	}

	// 3. Hard constraints (risk control)
	btcEthPosValueRatio := riskControl.BTCETHMaxPositionValueRatio
	if btcEthPosValueRatio <= 0 {
		btcEthPosValueRatio = 5.0
	}
	altcoinPosValueRatio := riskControl.AltcoinMaxPositionValueRatio
	if altcoinPosValueRatio <= 0 {
		altcoinPosValueRatio = 1.0
	}

	sb.WriteString("# Hard Constraints (Risk Control)\n\n")
	sb.WriteString("## CODE ENFORCED (Backend validation, cannot be bypassed):\n")
	sb.WriteString(fmt.Sprintf("- Max Positions: %d coins simultaneously\n", riskControl.MaxPositions))
	sb.WriteString(fmt.Sprintf("- Position Value Limit (Altcoins): max %.0f USDT (= equity %.0f × %.1fx)\n",
		accountEquity*altcoinPosValueRatio, accountEquity, altcoinPosValueRatio))
	sb.WriteString(fmt.Sprintf("- Position Value Limit (BTC/ETH): max %.0f USDT (= equity %.0f × %.1fx)\n",
		accountEquity*btcEthPosValueRatio, accountEquity, btcEthPosValueRatio))
	sb.WriteString(fmt.Sprintf("- Max Margin Usage: ≤%.0f%%\n", riskControl.MaxMarginUsage*100))
	sb.WriteString(fmt.Sprintf("- Min Position Size: ≥%.0f USDT\n\n", riskControl.MinPositionSize))

	sb.WriteString("## AI GUIDED (Recommended, you should follow):\n")
	sb.WriteString(fmt.Sprintf("- Trading Leverage: Altcoins max %dx | BTC/ETH max %dx\n",
		riskControl.AltcoinMaxLeverage, riskControl.BTCETHMaxLeverage))
	sb.WriteString(fmt.Sprintf("- Risk-Reward Ratio: ≥1:%.1f (take_profit / stop_loss)\n", riskControl.MinRiskRewardRatio))
	sb.WriteString(fmt.Sprintf("- Min Confidence: ≥%d to open position\n\n", riskControl.MinConfidence))

	// Position sizing guidance
	sb.WriteString("## Position Sizing Guidance\n")
	sb.WriteString("Calculate `position_size_usd` based on your confidence and the Position Value Limits above:\n")
	sb.WriteString("- High confidence (≥85): Use 80-100%% of max position value limit\n")
	sb.WriteString("- Medium confidence (70-84): Use 50-80%% of max position value limit\n")
	sb.WriteString("- Low confidence (60-69): Use 30-50%% of max position value limit\n")
	sb.WriteString(fmt.Sprintf("- Example: With equity %.0f and BTC/ETH ratio %.1fx, max is %.0f USDT\n",
		accountEquity, btcEthPosValueRatio, accountEquity*btcEthPosValueRatio))
	sb.WriteString("- **DO NOT** just use available_balance as position_size_usd. Use the Position Value Limits!\n\n")

	// 4. Trading frequency (editable)
	if promptSections.TradingFrequency != "" {
		sb.WriteString(promptSections.TradingFrequency)
		sb.WriteString("\n\n")
	} else {
		sb.WriteString("# ⏱️ Trading Frequency Awareness\n\n")
		sb.WriteString("- Excellent traders: 2-4 trades/day ≈ 0.1-0.2 trades/hour\n")
		sb.WriteString("- >2 trades/hour = Overtrading\n")
		sb.WriteString("- Single position hold time ≥ 30-60 minutes\n")
		sb.WriteString("If you find yourself trading every period → standards too low; if closing positions < 30 minutes → too impatient.\n\n")
	}

	// 5. Entry standards (editable)
	if promptSections.EntryStandards != "" {
		sb.WriteString(promptSections.EntryStandards)
		sb.WriteString("\n\nYou have the following indicator data:\n")
		e.writeAvailableIndicators(&sb)
		sb.WriteString(fmt.Sprintf("\n**Confidence ≥ %d** required to open positions.\n\n", riskControl.MinConfidence))
	} else {
		sb.WriteString("# 🎯 Entry Standards (Strict)\n\n")
		sb.WriteString("Only open positions when multiple signals resonate. You have:\n")
		e.writeAvailableIndicators(&sb)
		sb.WriteString(fmt.Sprintf("\nFeel free to use any effective analysis method, but **confidence ≥ %d** required to open positions; avoid low-quality behaviors such as single indicators, contradictory signals, sideways consolidation, reopening immediately after closing, etc.\n\n", riskControl.MinConfidence))
	}

	// 6. Decision process (editable)
	if promptSections.DecisionProcess != "" {
		sb.WriteString(promptSections.DecisionProcess)
		sb.WriteString("\n\n")
	} else {
		sb.WriteString("# 📋 Decision Process\n\n")
		sb.WriteString("1. Check positions → Should we take profit/stop-loss\n")
		sb.WriteString("2. Scan candidate coins + multi-timeframe → Are there strong signals\n")
		sb.WriteString("3. Write chain of thought first, then output structured JSON\n\n")
	}

	// 7. Output format
	sb.WriteString("# Output Format (Strictly Follow)\n\n")
	sb.WriteString("**Must use XML tags <reasoning> and <decision> to separate chain of thought and decision JSON, avoiding parsing errors**\n\n")
	// P0 2026-08-19 — decision-FIRST strict contract (same truncation guard as
	// the futures builder).
	sb.WriteString("Output the <decision> JSON block FIRST — it is MANDATORY and must appear in every response. THEN write <reasoning>: ≤200 words, decision-focused, no restating the input data. If you are running out of room, drop reasoning — never drop <decision>.\n\n")
	sb.WriteString("## Format Requirements\n\n")
	sb.WriteString("<reasoning>\n")
	sb.WriteString("Your chain of thought analysis...\n")
	sb.WriteString("- Briefly analyze your thinking process \n")
	sb.WriteString("</reasoning>\n\n")
	sb.WriteString("<decision>\n")
	sb.WriteString("Step 2: JSON decision array\n\n")
	sb.WriteString("```json\n[\n")
	// Use the actual configured position value ratio for BTC/ETH in the example
	examplePositionSize := accountEquity * btcEthPosValueRatio
	sb.WriteString(fmt.Sprintf("  {\"symbol\": \"BTCUSDT\", \"action\": \"open_short\", \"leverage\": %d, \"position_size_usd\": %.0f, \"stop_loss\": 97000, \"take_profit\": 91000, \"confidence\": 85, \"risk_usd\": 300},\n",
		riskControl.BTCETHMaxLeverage, examplePositionSize))
	sb.WriteString("  {\"symbol\": \"ETHUSDT\", \"action\": \"close_long\"}\n")
	sb.WriteString("]\n```\n")
	sb.WriteString("</decision>\n\n")
	sb.WriteString("## Field Description\n\n")
	sb.WriteString("- `action`: open_long | open_short | close_long | close_short | hold | wait\n")
	sb.WriteString(fmt.Sprintf("- `confidence`: 0-100 (opening recommended ≥ %d)\n", riskControl.MinConfidence))
	sb.WriteString("- Required when opening: leverage, position_size_usd, stop_loss, take_profit, confidence, risk_usd\n")
	sb.WriteString("- **IMPORTANT**: All numeric values must be calculated numbers, NOT formulas/expressions (e.g., use `27.76` not `3000 * 0.01`)\n\n")

	// 8. Custom Prompt
	if e.config.CustomPrompt != "" {
		sb.WriteString("# 📌 Personalized Trading Strategy\n\n")
		sb.WriteString(e.config.CustomPrompt)
		sb.WriteString("\n\n")
		sb.WriteString("Note: The above personalized strategy is a supplement to the basic rules and cannot violate the basic risk control principles.\n")
	}

	return sb.String()
}

// formatKlineTimeframes renders the "Available Data" timeframe summary line. It
// lists EVERY selected timeframe — matching what the fetch (engine_analysis.go
// reads SelectedTimeframes) and the per-timeframe user-prompt loop actually feed
// the AI — so the prompt no longer under-reports a 3-timeframe selection (e.g.
// 5m/15m/1h) as just "primary + longer" (2). For legacy configs that predate
// SelectedTimeframes (empty list) it falls back to the prior primary[+longer]
// wording, producing byte-identical output to the pre-fix code.
func formatKlineTimeframes(kline store.KlineConfig) string {
	if len(kline.SelectedTimeframes) > 0 {
		// B10 (audit F7): the primary_timeframe is ALWAYS fetched + rendered as a
		// table, so announce it too — even when the user left it out of
		// selected_timeframes — otherwise the model receives a table it was never
		// told about. Appended at the end (copy; never mutate the config slice).
		tfs := kline.SelectedTimeframes
		if kline.PrimaryTimeframe != "" {
			found := false
			for _, tf := range tfs {
				if tf == kline.PrimaryTimeframe {
					found = true
					break
				}
			}
			if !found {
				tfs = append(append([]string{}, tfs...), kline.PrimaryTimeframe)
			}
		}
		return fmt.Sprintf("- %s price series\n", strings.Join(tfs, ", "))
	}
	// Legacy fallback (no SelectedTimeframes): primary + optional longer — the
	// exact wording the code emitted before the SelectedTimeframes-aware fix.
	if kline.EnableMultiTimeframe {
		return fmt.Sprintf("- %s price series + %s K-line series\n", kline.PrimaryTimeframe, kline.LongerTimeframe)
	}
	return fmt.Sprintf("- %s price series\n", kline.PrimaryTimeframe)
}

func (e *StrategyEngine) writeAvailableIndicators(sb *strings.Builder) {
	indicators := e.config.Indicators
	kline := indicators.Klines

	sb.WriteString(formatKlineTimeframes(kline))

	if indicators.EnableEMA {
		sb.WriteString("- EMA indicators")
		if len(indicators.EMAPeriods) > 0 {
			sb.WriteString(fmt.Sprintf(" (periods: %v)", indicators.EMAPeriods))
		}
		sb.WriteString("\n")
	}

	if indicators.EnableMACD {
		sb.WriteString("- MACD indicators\n")
	}

	if indicators.EnableRSI {
		sb.WriteString("- RSI indicators")
		if len(indicators.RSIPeriods) > 0 {
			sb.WriteString(fmt.Sprintf(" (periods: %v)", indicators.RSIPeriods))
		}
		sb.WriteString("\n")
	}

	if indicators.EnableATR {
		sb.WriteString("- ATR indicators")
		if len(indicators.ATRPeriods) > 0 {
			sb.WriteString(fmt.Sprintf(" (periods: %v)", indicators.ATRPeriods))
		}
		sb.WriteString("\n")
	}

	if indicators.EnableBOLL {
		sb.WriteString("- Bollinger Bands (BOLL) - Upper/Middle/Lower bands")
		if len(indicators.BOLLPeriods) > 0 {
			sb.WriteString(fmt.Sprintf(" (periods: %v)", indicators.BOLLPeriods))
		}
		sb.WriteString("\n")
	}

	if indicators.EnableVolume {
		sb.WriteString("- Volume data\n")
	}

	if indicators.EnableOI {
		if e.isFuturesInstrument() {
			// W-NO-BINANCE A: the futures path reads no external market data;
			// say so instead of advertising a feed that is not there.
			sb.WriteString("- Open Interest (OI) data: n/a (no external market data on the futures path)\n")
		} else {
			sb.WriteString("- Open Interest (OI) data\n")
		}
	}

	if indicators.EnableFundingRate && !e.isFuturesInstrument() {
		sb.WriteString("- Funding rate\n")
	}
}

// ============================================================================
// Prompt Building - User Prompt
// ============================================================================

// BuildUserPrompt builds User Prompt based on strategy configuration
func (e *StrategyEngine) BuildUserPrompt(ctx *Context) string {
	var sb strings.Builder

	// System status
	sb.WriteString(fmt.Sprintf("Time: %s | Period: #%d | Runtime: %d minutes\n\n",
		ctx.CurrentTime, ctx.CallCount, ctx.RuntimeMinutes))

	// Account information
	sb.WriteString(fmt.Sprintf("Account: Equity %.2f | Balance %.2f (%.1f%%) | PnL %+.2f%% | Margin %.1f%% | Positions %d\n\n",
		ctx.Account.TotalEquity,
		ctx.Account.AvailableBalance,
		(ctx.Account.AvailableBalance/ctx.Account.TotalEquity)*100,
		ctx.Account.TotalPnLPct,
		ctx.Account.MarginUsedPct,
		ctx.Account.PositionCount))

	// Recently completed orders (placed before positions to ensure visibility)
	if len(ctx.RecentOrders) > 0 {
		sb.WriteString("## Recent Completed Trades\n")
		for i, order := range ctx.RecentOrders {
			if !order.Resolved {
				sb.WriteString(fmt.Sprintf("%d. #%d %s %s | Entry %.4f→? UNRESOLVED (exit unknown) | %s→%s (%s)\n",
					i+1, order.ID, order.Symbol, order.Side, order.EntryPrice,
					order.EntryTime, order.ExitTime, order.HoldDuration))
				continue
			}
			resultStr := "Profit"
			if order.RealizedPnL < 0 {
				resultStr = "Loss"
			}
			sb.WriteString(fmt.Sprintf("%d. %s %s | Entry %.4f Exit %.4f | %s: %+.2f USDT (%+.2f%%) | %s→%s (%s)\n",
				i+1, order.Symbol, order.Side,
				order.EntryPrice, order.ExitPrice,
				resultStr, order.RealizedPnL, order.PnLPct,
				order.EntryTime, order.ExitTime, order.HoldDuration))
		}
		sb.WriteString("\n")
	}

	// Historical trading statistics (helps AI understand past performance)
	// P&L-TRUTH WAVE: the block renders whenever there is ANY closed history —
	// a session with 0 resolved and K unresolved must say so, not go silent.
	if ctx.TradingStats != nil && (ctx.TradingStats.TotalTrades > 0 || ctx.TradingStats.UnresolvedExcluded > 0) {
		// Get language from strategy config
		lang := e.GetLanguage()

		// Win/Loss ratio
		var winLossRatio float64
		if ctx.TradingStats.AvgLoss > 0 {
			winLossRatio = ctx.TradingStats.AvgWin / ctx.TradingStats.AvgLoss
		}

		if lang == LangChinese {
			sb.WriteString("## 历史交易统计\n")
			sb.WriteString(fmt.Sprintf("已结算交易: %d 笔 | 盈利因子: %.2f | 夏普比率: %.2f | 盈亏比: %.2f\n",
				ctx.TradingStats.TotalTrades,
				ctx.TradingStats.ProfitFactor,
				ctx.TradingStats.SharpeRatio,
				winLossRatio))
			sb.WriteString(TrackRecordLine(ctx.TradingStats, LangChinese) + "\n")
			if ctx.TradingStats.ManualExcludedLine != "" { // RT-4: the manual rows' own counted line
				sb.WriteString(ctx.TradingStats.ManualExcludedLine + "\n")
			}
			sb.WriteString(fmt.Sprintf("平均盈利: +%.2f | 平均亏损: -%.2f | 最大回撤: %.1f%%\n",
				ctx.TradingStats.AvgWin,
				ctx.TradingStats.AvgLoss,
				ctx.TradingStats.MaxDrawdownPct))
			sb.WriteString(TrackRecordNote(LangChinese) + "\n")

			// Performance hints based on profit factor, sharpe, and drawdown
			if ctx.TradingStats.ProfitFactor >= 1.5 && ctx.TradingStats.SharpeRatio >= 1 {
				sb.WriteString("表现: 良好 - 保持当前策略\n")
			} else if ctx.TradingStats.ProfitFactor < 1 {
				sb.WriteString("表现: 需改进 - 提高盈亏比，优化止盈止损\n")
			} else if ctx.TradingStats.MaxDrawdownPct > 30 {
				sb.WriteString("表现: 风险偏高 - 减少仓位，控制回撤\n")
			} else {
				sb.WriteString("表现: 正常 - 有优化空间\n")
			}
		} else {
			sb.WriteString("## Historical Trading Statistics\n")
			sb.WriteString(fmt.Sprintf("Resolved Trades: %d | Profit Factor: %.2f | Sharpe: %.2f | Win/Loss Ratio: %.2f\n",
				ctx.TradingStats.TotalTrades,
				ctx.TradingStats.ProfitFactor,
				ctx.TradingStats.SharpeRatio,
				winLossRatio))
			// P&L-TRUTH WAVE: never a bare total — the figure, its resolved n and
			// the unresolved exclusion count, in one line the model can use.
			sb.WriteString(TrackRecordLine(ctx.TradingStats, LangEnglish) + "\n")
			if ctx.TradingStats.ManualExcludedLine != "" { // RT-4: the manual rows' own counted line
				sb.WriteString(ctx.TradingStats.ManualExcludedLine + "\n")
			}
			sb.WriteString(fmt.Sprintf("Avg Win: +%.2f | Avg Loss: -%.2f | Max Drawdown: %.1f%%\n",
				ctx.TradingStats.AvgWin,
				ctx.TradingStats.AvgLoss,
				ctx.TradingStats.MaxDrawdownPct))
			sb.WriteString(TrackRecordNote(LangEnglish) + "\n")

			// Performance hints based on profit factor, sharpe, and drawdown
			if ctx.TradingStats.ProfitFactor >= 1.5 && ctx.TradingStats.SharpeRatio >= 1 {
				sb.WriteString("Performance: GOOD - maintain current strategy\n")
			} else if ctx.TradingStats.ProfitFactor < 1 {
				sb.WriteString("Performance: NEEDS IMPROVEMENT - improve win/loss ratio, optimize TP/SL\n")
			} else if ctx.TradingStats.MaxDrawdownPct > 30 {
				sb.WriteString("Performance: HIGH RISK - reduce position size, control drawdown\n")
			} else {
				sb.WriteString("Performance: NORMAL - room for optimization\n")
			}
		}
		sb.WriteString("\n")
	}

	// Position information
	if len(ctx.Positions) > 0 {
		sb.WriteString("## Current Positions\n")
		for i, pos := range ctx.Positions {
			sb.WriteString(e.formatPositionInfo(i+1, pos, ctx))
		}
	} else {
		sb.WriteString("Current Positions: None\n\n")
	}

	// Candidate coins (exclude coins already in positions to avoid duplicate data)
	positionSymbols := make(map[string]bool)
	for _, pos := range ctx.Positions {

		normalizedSymbol := market.Normalize(pos.Symbol)
		positionSymbols[normalizedSymbol] = true
	}

	sb.WriteString(fmt.Sprintf("## Candidate Coins (%d coins)\n\n", len(ctx.MarketDataMap)))
	displayedCount := 0
	for _, coin := range ctx.CandidateCoins {
		// Skip if this coin is already a position (data already shown in positions section)
		normalizedCoinSymbol := market.Normalize(coin.Symbol)
		if positionSymbols[normalizedCoinSymbol] {
			continue
		}

		marketData, hasData := ctx.MarketDataMap[coin.Symbol]
		if !hasData {
			continue
		}
		displayedCount++

		sourceTags := e.formatCoinSourceTag(coin.Sources)
		sb.WriteString(fmt.Sprintf("### %d. %s%s\n\n", displayedCount, coin.Symbol, sourceTags))
		sb.WriteString(e.formatMarketData(marketData))

		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	// G2 (regime wave 2026-08-21) — the machine structure line, advisory:
	// per-TF trend + swings + the latest BOS/CHoCH/MSS/SWEEP event. The AI
	// judges; no gate lives here (G1/G4 consume the same snapshot in Go).
	if e.isFuturesInstrument() && len(ctx.Structure) > 0 {
		sb.WriteString(StructurePromptLine(ctx.Structure) + "\n\n")
		if strings.TrimSpace(ctx.StructureGap) != "" {
			sb.WriteString("BARS HOLE: " + ctx.StructureGap + "\n\n")
		}
	}

	sb.WriteString("---\n\n")
	sb.WriteString("Now please analyze and output your decision (Chain of Thought + JSON)\n")

	return sb.String()
}

func (e *StrategyEngine) formatPositionInfo(index int, pos PositionInfo, ctx *Context) string {
	var sb strings.Builder

	holdingDuration := ""
	if pos.UpdateTime > 0 {
		durationMs := time.Now().UnixMilli() - pos.UpdateTime
		durationMin := durationMs / (1000 * 60)
		if durationMin < 60 {
			holdingDuration = fmt.Sprintf(" | Holding Duration %d min", durationMin)
		} else {
			durationHour := durationMin / 60
			durationMinRemainder := durationMin % 60
			holdingDuration = fmt.Sprintf(" | Holding Duration %dh %dm", durationHour, durationMinRemainder)
		}
	}

	positionValue := pos.Quantity * pos.MarkPrice
	if positionValue < 0 {
		positionValue = -positionValue
	}

	sb.WriteString(fmt.Sprintf("%d. %s %s | Entry %.4f Current %.4f | Qty %.4f | Position Value %.2f USDT | PnL%+.2f%% | PnL Amount%+.2f USDT | Peak PnL%.2f%% | Leverage %dx | Margin %.0f | Liq Price %.4f%s\n\n",
		index, pos.Symbol, strings.ToUpper(pos.Side),
		pos.EntryPrice, pos.MarkPrice, pos.Quantity, positionValue, pos.UnrealizedPnLPct, pos.UnrealizedPnL, pos.PeakPnLPct,
		pos.Leverage, pos.MarginUsed, pos.LiquidationPrice, holdingDuration))

	if marketData, ok := ctx.MarketDataMap[pos.Symbol]; ok {
		sb.WriteString(e.formatMarketData(marketData))

		sb.WriteString("\n")
	}

	return sb.String()
}

func (e *StrategyEngine) formatCoinSourceTag(sources []string) string {
	if len(sources) > 1 {
		// Multiple signal source combination
		return " (Multiple sources)"
	} else if len(sources) == 1 {
		switch sources[0] {
		case "static":
			return " (Manual selection)"
		}
	}
	return ""
}

// ============================================================================
// Market Data Formatting
// ============================================================================

// isFuturesInstrument reports whether this engine's primary instrument is a CME
// futures contract. Crypto-only prompt content (funding rate — B8/audit F4) is
// suppressed when true: the futures instrument block states there is no funding
// rate, so emitting a "Funding rate" line was self-contradictory. Uses the
// configured static symbol.
func (e *StrategyEngine) isFuturesInstrument() bool {
	coins := e.config.CoinSource.StaticCoins
	return len(coins) > 0 && market.IsCMEFuturesSymbol(coins[0])
}

// formingBarLine (P10.2) renders the newest bar's honesty label for one
// timeframe: "current 5m bar: FORMING (closes 10:35 CT) — prior bars closed"
// while the bar is still open at the snapshot instant, else a CLOSED line
// naming the next close. Empty when snapshot/interval/bars are unavailable.
func formingBarLine(tf string, tfData *market.TimeframeSeriesData, snapshotMs int64) string {
	if snapshotMs <= 0 || tfData == nil || len(tfData.Klines) == 0 {
		return ""
	}
	iv := tfIntervalMs(tf)
	if iv <= 0 {
		return "" // only intraday TFs carry the label (1m/5m/15m…)
	}
	newest := tfData.Klines[len(tfData.Klines)-1]
	closeMs := newest.Time + iv
	if closeMs > snapshotMs {
		return fmt.Sprintf("current %s bar: FORMING (closes %s) — prior bars closed\n\n",
			tf, ClockCT(time.UnixMilli(closeMs)))
	}
	return fmt.Sprintf("current %s bar: CLOSED at %s (next close %s)\n\n",
		tf, ClockCT(time.UnixMilli(closeMs)), ClockCT(time.UnixMilli(closeMs+iv)))
}

// staleTFLabel (G7) appends a staleness warning under a TF table when the
// newest CLOSED bar is older than period + FLIP_EVAL_MAX_STALE_S — the same
// cap the flip/death evaluator uses, so the prompt and the evaluator can never
// disagree about what "stale" means. Futures only; renders nothing when fresh.
func staleTFLabel(tf string, tfData *market.TimeframeSeriesData, snapshotMs int64) string {
	if snapshotMs <= 0 || tfData == nil || len(tfData.Klines) == 0 {
		return ""
	}
	iv := tfIntervalMs(tf)
	if iv <= 0 {
		return ""
	}
	newest := tfData.Klines[len(tfData.Klines)-1]
	closeMs := newest.Time + iv
	if closeMs > snapshotMs {
		return "" // forming — not stale by definition
	}
	age := snapshotMs - closeMs
	capMs := FlipEvalMaxStaleMs()
	if age <= iv+capMs {
		return ""
	}
	return fmt.Sprintf("⚠️ %s data stale: newest close %s is %.0fs old (period %.0fs + cap %.0fs)\n\n",
		tf, ClockCT(time.UnixMilli(closeMs)), float64(age)/1000, float64(iv)/1000, float64(capMs)/1000)
}

func (e *StrategyEngine) formatMarketData(data *market.Data) string {
	var sb strings.Builder
	indicators := e.config.Indicators

	// Clearly label the coin symbol
	sb.WriteString(fmt.Sprintf("=== %s Market Data ===\n\n", data.Symbol))
	sb.WriteString(fmt.Sprintf("current_price = %.4f", data.CurrentPrice))

	if indicators.EnableEMA {
		if len(data.CurrentEMAByPeriod) > 0 {
			periods := make([]int, 0, len(data.CurrentEMAByPeriod))
			for p := range data.CurrentEMAByPeriod {
				periods = append(periods, p)
			}
			sort.Ints(periods)
			for _, p := range periods {
				v := data.CurrentEMAByPeriod[p]
				// F11d — never emit "current_ema200 = 0.000" on thin bar depth (an EMA
				// can't be 0 for a real instrument; 0 means calculateEMA had <period
				// bars). Omit it + log, rather than feeding the model a fake zero.
				if v <= 0 {
					// R22 F-2: an unwarmed EMA is printed n/a (L7 — a value not
					// known prints n/a), never an SMA under an EMA label.
					logger.Infof("ℹ️ current_ema%d n/a for %s — thin bar depth (needs %d bars for a warmed EMA).", p, data.Symbol, market.EMAWarmupBars(p))
					sb.WriteString(fmt.Sprintf(", current_ema%d = n/a", p))
					continue
				}
				sb.WriteString(fmt.Sprintf(", current_ema%d = %.3f", p, v))
			}
		} else if data.CurrentEMA20 > 0 {
			sb.WriteString(fmt.Sprintf(", current_ema20 = %.3f", data.CurrentEMA20))
		} else {
			logger.Infof("ℹ️ current_ema20 omitted for %s — thin bar depth (needs 20 bars); EMA not warmed up.", data.Symbol)
		}
	}

	if indicators.EnableMACD {
		sb.WriteString(fmt.Sprintf(", current_macd = %.3f", data.CurrentMACD))
	}

	if indicators.EnableRSI {
		// F9 — honest snapshot label: use the FIRST CONFIGURED RSI period (smallest,
		// deterministic) so hoang's [14] renders current_rsi14, not a hardcoded
		// current_rsi7. No configured periods → the legacy period-7 line.
		if len(data.CurrentRSIByPeriod) > 0 {
			periods := make([]int, 0, len(data.CurrentRSIByPeriod))
			for p := range data.CurrentRSIByPeriod {
				periods = append(periods, p)
			}
			sort.Ints(periods)
			p := periods[0]
			sb.WriteString(fmt.Sprintf(", current_rsi%d = %.3f", p, data.CurrentRSIByPeriod[p]))
		} else {
			sb.WriteString(fmt.Sprintf(", current_rsi7 = %.3f", data.CurrentRSI7))
		}
	}

	sb.WriteString("\n\n")

	// B8 (audit F4): funding rate is a crypto-perp concept — suppress it entirely on
	// CME futures even when the toggle is on (the instrument block states there is
	// none), so the prompt is no longer self-contradictory.
	fundingOn := indicators.EnableFundingRate && !e.isFuturesInstrument()
	if indicators.EnableOI || fundingOn {
		sb.WriteString(fmt.Sprintf("Additional data for %s:\n\n", data.Symbol))

		if indicators.EnableOI {
			if data.OpenInterest != nil {
				sb.WriteString(fmt.Sprintf("Open Interest: Latest: %.2f Average: %.2f\n\n",
					data.OpenInterest.Latest, data.OpenInterest.Average))
			} else {
				// W-NO-BINANCE A: ABSENT is n/a, never a fabricated 0.00 (the
				// futures path reads no external OI).
				sb.WriteString("Open Interest: n/a\n\n")
			}
		}

		if fundingOn {
			if data.FundingRateKnown {
				sb.WriteString(fmt.Sprintf("Funding Rate: %.2e\n\n", data.FundingRate))
			} else {
				// W-NO-BINANCE A (CTO F1): a funding fetch that did not succeed is
				// n/a, never a fabricated 0.00e+00.
				sb.WriteString("Funding Rate: n/a\n\n")
			}
		}
	}

	if len(data.TimeframeData) > 0 {
		timeframeOrder := []string{"1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h", "6h", "8h", "12h", "1d", "3d", "1w"}
		for _, tf := range timeframeOrder {
			if tfData, ok := data.TimeframeData[tf]; ok {
				sb.WriteString(fmt.Sprintf("=== %s Timeframe (oldest → latest) ===\n\n", strings.ToUpper(tf)))
				e.formatTimeframeSeriesData(&sb, tfData, indicators)
				// P10.3 — no-data honesty: a timeframe with
				// neither bars nor mid prices is stated, never
				// silently omitted (master-audit v1 finding 8.5).
				if len(tfData.Klines) == 0 && len(tfData.MidPrices) == 0 {
					sb.WriteString("⚠️ no data available for this timeframe this cycle (feed warming up or down) — do NOT infer prices.\n\n")
				}
				// P10.2 — prompt honesty (owner ruling: interval cadence runs
				// cycles MID-BAR): label the newest bar FORMING/CLOSED so the
				// AI knows what it is looking at and may itself choose to wait
				// for the close — its judgment now, not a code gate. Futures
				// only; snapshot 0 (tests/legacy) renders nothing.
				if e.isFuturesInstrument() {
					sb.WriteString(formingBarLine(tf, tfData, e.promptSnapshotMs))
					sb.WriteString(staleTFLabel(tf, tfData, e.promptSnapshotMs))
				}
			}
		}
	} else {
		// Compatible with old data format
		if data.IntradaySeries != nil {
			klineConfig := indicators.Klines
			sb.WriteString(fmt.Sprintf("Intraday series (%s intervals, oldest → latest):\n\n", klineConfig.PrimaryTimeframe))

			if len(data.IntradaySeries.MidPrices) > 0 {
				sb.WriteString(fmt.Sprintf("Mid prices: %s\n\n", formatFloatSlice(data.IntradaySeries.MidPrices)))
			}

			if indicators.EnableEMA && len(data.IntradaySeries.EMA20Values) > 0 {
				sb.WriteString(fmt.Sprintf("EMA indicators (20-period): %s\n\n", formatFloatSlice(data.IntradaySeries.EMA20Values)))
			}

			if indicators.EnableMACD && len(data.IntradaySeries.MACDValues) > 0 {
				sb.WriteString(fmt.Sprintf("MACD indicators: %s\n\n", formatFloatSlice(data.IntradaySeries.MACDValues)))
			}

			if indicators.EnableRSI {
				if len(data.IntradaySeries.RSI7Values) > 0 {
					sb.WriteString(fmt.Sprintf("RSI indicators (7-Period): %s\n\n", formatFloatSlice(data.IntradaySeries.RSI7Values)))
				}
				if len(data.IntradaySeries.RSI14Values) > 0 {
					sb.WriteString(fmt.Sprintf("RSI indicators (14-Period): %s\n\n", formatFloatSlice(data.IntradaySeries.RSI14Values)))
				}
			}

			if indicators.EnableVolume && len(data.IntradaySeries.Volume) > 0 {
				sb.WriteString(fmt.Sprintf("Volume: %s\n\n", formatFloatSlice(data.IntradaySeries.Volume)))
			}

			if indicators.EnableATR {
				sb.WriteString(fmt.Sprintf("3m ATR (14-period): %.3f\n\n", data.IntradaySeries.ATR14))
			}
		}

		if data.LongerTermContext != nil && indicators.Klines.EnableMultiTimeframe {
			sb.WriteString(fmt.Sprintf("Longer-term context (%s timeframe):\n\n", indicators.Klines.LongerTimeframe))

			if indicators.EnableEMA {
				sb.WriteString(fmt.Sprintf("20-Period EMA: %.3f vs. 50-Period EMA: %.3f\n\n",
					data.LongerTermContext.EMA20, data.LongerTermContext.EMA50))
			}

			if indicators.EnableATR {
				sb.WriteString(fmt.Sprintf("3-Period ATR: %.3f vs. 14-Period ATR: %.3f\n\n",
					data.LongerTermContext.ATR3, data.LongerTermContext.ATR14))
			}

			if indicators.EnableVolume {
				sb.WriteString(fmt.Sprintf("Current Volume: %.3f vs. Average Volume: %.3f\n\n",
					data.LongerTermContext.CurrentVolume, data.LongerTermContext.AverageVolume))
			}

			if indicators.EnableMACD && len(data.LongerTermContext.MACDValues) > 0 {
				sb.WriteString(fmt.Sprintf("MACD indicators: %s\n\n", formatFloatSlice(data.LongerTermContext.MACDValues)))
			}

			if indicators.EnableRSI && len(data.LongerTermContext.RSI14Values) > 0 {
				sb.WriteString(fmt.Sprintf("RSI indicators (14-Period): %s\n\n", formatFloatSlice(data.LongerTermContext.RSI14Values)))
			}
		}
	}

	return sb.String()
}

func (e *StrategyEngine) formatTimeframeSeriesData(sb *strings.Builder, data *market.TimeframeSeriesData, indicators store.IndicatorConfig) {
	if len(data.Klines) > 0 {
		FormatCandleTable(sb, data.Klines, true)
	} else if len(data.MidPrices) > 0 {
		sb.WriteString(fmt.Sprintf("Mid prices: %s\n\n", formatFloatSlice(data.MidPrices)))
		if indicators.EnableVolume && len(data.Volume) > 0 {
			sb.WriteString(fmt.Sprintf("Volume: %s\n\n", formatFloatSlice(data.Volume)))
		}
	}

	// W11 — the indicator-state lines are extracted to FormatIndicatorState so the
	// day-plan planner prompt can mirror the EXACT block the executor renders.
	FormatIndicatorState(sb, data, indicators)

	sb.WriteString("\n")
}

// FormatCandleTable renders a labelled "Time(CT) Open High Low Close [Volume]"
// table (oldest → latest) from klines — the ONE candle-table formatter the
// repo uses. Extracted (W2b, weekly-bias wave 2026-08-30) so the session
// planner's ## Candles block is rendered by the SAME code as the executor's
// per-timeframe tables (never a second formatter). The newest row carries the
// "  <- current" marker exactly as the executor always rendered it.
func FormatCandleTable(sb *strings.Builder, klines []market.KlineBar, volume bool) {
	FormatCandleTableNoted(sb, klines, volume, nil)
}

// FormatCandleTableNoted is the SAME formatter with a per-row note appended
// (BARS HORIZON, 2026-09-09 — D1). notes[i] is written verbatim after row i;
// a nil/short slice notes nothing, which is exactly what FormatCandleTable
// passes. Adding a second formatter here was not an option: the repo renders
// every candle table through this one function so a row can never be shaped
// two ways.
//
// A24: a note NEVER changes a rendered O/H/L/C/V. It is appended text only.
func FormatCandleTableNoted(sb *strings.Builder, klines []market.KlineBar, volume bool, notes []string) {
	if len(klines) == 0 {
		return
	}
	if volume {
		sb.WriteString("Time(CT)       Open      High      Low       Close     Volume\n")
	} else {
		sb.WriteString("Time(CT)       Open      High      Low       Close\n")
	}
	for i, k := range klines {
		t := time.Unix(k.Time/1000, 0).In(CTLocation())
		timeStr := TableTimeCT(t)
		marker := ""
		if i == len(klines)-1 {
			marker = "  <- current"
		}
		if i < len(notes) {
			marker += notes[i]
		}
		if volume {
			sb.WriteString(fmt.Sprintf("%-14s %-9.4f %-9.4f %-9.4f %-9.4f %-12.2f%s\n",
				timeStr, k.Open, k.High, k.Low, k.Close, k.Volume, marker))
		} else {
			sb.WriteString(fmt.Sprintf("%-14s %-9.4f %-9.4f %-9.4f %-9.4f%s\n",
				timeStr, k.Open, k.High, k.Low, k.Close, marker))
		}
	}
	sb.WriteString("\n")
}

// KlineBars converts bar-cache Klines to prompt-table KlineBars
// (Time = OpenTime) so the planner's candle tables and the executor's
// timeframe tables share the ONE formatter.
func KlineBars(klines []market.Kline) []market.KlineBar {
	out := make([]market.KlineBar, 0, len(klines))
	for _, k := range klines {
		out = append(out, market.KlineBar{Time: k.OpenTime, Open: k.Open, High: k.High, Low: k.Low, Close: k.Close, Volume: k.Volume})
	}
	return out
}

// FormatIndicatorState writes the toggle-gated, configured-period-aware indicator
// lines (EMA/MACD/RSI/ATR/BOLL) for one timeframe's series — the SAME indicator
// state the executor prompt renders. Extracted (W11) so the planner INDICATORS
// mirror is byte-identical to the executor's, never a re-derivation. Pure: reads
// only `data` + `indicators`; emits nothing for disabled toggles / empty series.
func FormatIndicatorState(sb *strings.Builder, data *market.TimeframeSeriesData, indicators store.IndicatorConfig) {
	if indicators.EnableEMA {
		if len(data.EMAByPeriod) > 0 {
			// Configured periods drive the labels + values (e.g. EMA9/EMA21/EMA200),
			// sorted ascending for stable output.
			periods := make([]int, 0, len(data.EMAByPeriod))
			for p := range data.EMAByPeriod {
				periods = append(periods, p)
			}
			sort.Ints(periods)
			for _, p := range periods {
				if len(data.EMAByPeriod[p]) > 0 {
					sb.WriteString(fmt.Sprintf("EMA%d: %s\n", p, formatFloatSlice(data.EMAByPeriod[p])))
				} else {
					// R22 F-2: configured but not warmed on this TF's depth — say
					// so; never drop the row silently, never print the SMA seed.
					sb.WriteString(fmt.Sprintf("EMA%d: n/a (not warmed — needs %d bars)\n", p, market.EMAWarmupBars(p)))
				}
			}
		} else {
			// Legacy fallback (no configured periods) — byte-identical to prior output.
			if len(data.EMA20Values) > 0 {
				sb.WriteString(fmt.Sprintf("EMA20: %s\n", formatFloatSlice(data.EMA20Values)))
			}
			if len(data.EMA50Values) > 0 {
				sb.WriteString(fmt.Sprintf("EMA50: %s\n", formatFloatSlice(data.EMA50Values)))
			}
		}
	}

	if indicators.EnableMACD && len(data.MACDValues) > 0 {
		sb.WriteString(fmt.Sprintf("MACD: %s\n", formatFloatSlice(data.MACDValues)))
	}

	if indicators.EnableRSI {
		if len(data.RSIByPeriod) > 0 {
			periods := make([]int, 0, len(data.RSIByPeriod))
			for p := range data.RSIByPeriod {
				periods = append(periods, p)
			}
			sort.Ints(periods)
			for _, p := range periods {
				if len(data.RSIByPeriod[p]) > 0 {
					sb.WriteString(fmt.Sprintf("RSI%d: %s\n", p, formatFloatSlice(data.RSIByPeriod[p])))
				}
			}
		} else {
			if len(data.RSI7Values) > 0 {
				sb.WriteString(fmt.Sprintf("RSI7: %s\n", formatFloatSlice(data.RSI7Values)))
			}
			if len(data.RSI14Values) > 0 {
				sb.WriteString(fmt.Sprintf("RSI14: %s\n", formatFloatSlice(data.RSI14Values)))
			}
		}
	}

	if indicators.EnableATR {
		if len(data.ATRByPeriod) > 0 {
			periods := make([]int, 0, len(data.ATRByPeriod))
			for p := range data.ATRByPeriod {
				periods = append(periods, p)
			}
			sort.Ints(periods)
			for _, p := range periods {
				sb.WriteString(fmt.Sprintf("ATR%d: %.4f\n", p, data.ATRByPeriod[p]))
			}
		} else if data.ATR14 > 0 {
			sb.WriteString(fmt.Sprintf("ATR14: %.4f\n", data.ATR14))
		}
	}

	if indicators.EnableBOLL {
		if len(data.BOLLByPeriod) > 0 {
			periods := make([]int, 0, len(data.BOLLByPeriod))
			for p := range data.BOLLByPeriod {
				periods = append(periods, p)
			}
			sort.Ints(periods)
			for _, p := range periods {
				if b := data.BOLLByPeriod[p]; b != nil && len(b.Upper) > 0 {
					sb.WriteString(fmt.Sprintf("BOLL%d Upper: %s\n", p, formatFloatSlice(b.Upper)))
					sb.WriteString(fmt.Sprintf("BOLL%d Middle: %s\n", p, formatFloatSlice(b.Middle)))
					sb.WriteString(fmt.Sprintf("BOLL%d Lower: %s\n", p, formatFloatSlice(b.Lower)))
				}
			}
		} else if len(data.BOLLUpper) > 0 {
			sb.WriteString(fmt.Sprintf("BOLL Upper: %s\n", formatFloatSlice(data.BOLLUpper)))
			sb.WriteString(fmt.Sprintf("BOLL Middle: %s\n", formatFloatSlice(data.BOLLMiddle)))
			sb.WriteString(fmt.Sprintf("BOLL Lower: %s\n", formatFloatSlice(data.BOLLLower)))
		}
	}
}

func formatFloatSlice(values []float64) string {
	strValues := make([]string, len(values))
	for i, v := range values {
		strValues[i] = fmt.Sprintf("%.4f", v)
	}
	return "[" + strings.Join(strValues, ", ") + "]"
}

// TrackRecordLine (P&L-TRUTH WAVE, 2026-09-01) — the ONE track-record line
// every prompt surface renders: the strict corrected figure, the resolved
// count it is measured over, and the unresolved exclusion count. Never a bare
// total; a resolved count of zero says so rather than rendering nothing.
func TrackRecordLine(st *TradingStats, lang Language) string {
	if st == nil {
		return ""
	}
	if lang == LangChinese {
		if st.TotalTrades == 0 {
			return fmt.Sprintf("战绩: 未结算 — 0 笔已结算交易（%d 笔未结算交易已排除 — 见注）", st.UnresolvedExcluded)
		}
		return fmt.Sprintf("战绩: %+.2f，基于 %d 笔已结算交易（%d 笔未结算交易已排除 — 见注）", st.TotalPnL, st.TotalTrades, st.UnresolvedExcluded)
	}
	if st.TotalTrades == 0 {
		return fmt.Sprintf("Track record: UNRESOLVED — 0 resolved trades (%d unresolved trades excluded — see note).", st.UnresolvedExcluded)
	}
	return fmt.Sprintf("Track record: %+.2f over %d resolved trades (%d unresolved trades excluded — see note).", st.TotalPnL, st.TotalTrades, st.UnresolvedExcluded)
}

// TrackRecordNote explains the exclusion in one line.
func TrackRecordNote(lang Language) string {
	if lang == LangChinese {
		return "注: 未结算交易没有已核实的出场成交，其盈亏未知 — 永远不计入、不折算为原始值。"
	}
	return "Note: an unresolved trade has no verified exit fill; its P&L is UNKNOWN and is never counted, never coerced to a raw value."
}
