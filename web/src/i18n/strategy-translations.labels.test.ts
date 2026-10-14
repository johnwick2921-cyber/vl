// FIX-LABELS guard tests (DS-103): pin the Studio labels to the code's truth so
// they cannot drift again. Code truth for the coin_source fields is the
// CoinSourceConfig struct comments at store/strategy.go:1905-1930.
import { describe, expect, it } from 'vitest'
import {
  coinSource,
  gridConfig,
  indicator,
  promptSections,
  riskControl,
} from './strategy-translations'
import { planStrings } from './plan-translations'
import { translations } from './translations'

// The source-type value set the code accepts (store/strategy.go source_type
// comment: "static" only). A label that drops or adds a value would
// misdescribe what the engine will use.
const sourceTypeValues = ['static']

describe('coinSource label truth (pinned to store/strategy.go:1905-1930)', () => {
  it('sourceType label names exactly the one code value', () => {
    for (const lang of ['en', 'zh', 'es'] as const) {
      const text = coinSource.sourceType[lang]
      for (const v of sourceTypeValues) {
        expect(text.toLowerCase()).toContain(v)
      }
    }
  })

  it('staticDesc ties the list to source_type = static', () => {
    expect(coinSource.staticDesc.en).toContain('static')
  })

  it('excludedCoinsDesc says all sources, not a trading guarantee', () => {
    expect(coinSource.excludedCoinsDesc.en).toContain('all coin sources')
    expect(coinSource.excludedCoinsDesc.en.toLowerCase()).not.toContain(
      'will not be traded'
    )
  })
})
// Indicator block descs pin the period defaults the code ships
// (store/strategy.go:1950-1958 + DefaultConfig at store/strategy.go:2205).
describe('indicator label truth (pinned to store/strategy.go:1936-1958)', () => {
  it('ema desc names the code default periods 20, 50 (never 50/200)', () => {
    expect(indicator.emaDesc.en).toContain('20, 50')
    expect(indicator.emaDesc.en).not.toContain('200')
  })

  it('rsi desc names the code default periods 7, 14', () => {
    expect(indicator.rsiDesc.en).toContain('7, 14')
  })

  it('atr desc names the code default period 14', () => {
    expect(indicator.atrDesc.en).toContain('14')
  })

  it('boll desc names period 20 and the fixed std-dev multiplier 2', () => {
    expect(indicator.bollDesc.en).toContain('20')
    expect(indicator.bollDesc.en).toContain('fixed at 2')
  })

  it('svp desc states default OFF (code comment: default OFF)', () => {
    expect(indicator.svpDesc.en.toLowerCase()).toContain('default off')
  })

  it('rawKlines desc states always enabled (force-set on mount)', () => {
    expect(indicator.rawKlinesDesc.en.toLowerCase()).toContain('always enabled')
  })
})
// Grid descs pin the code ranges/defaults (store/strategy.go:1860-1914).
describe('gridConfig label truth (pinned to store/strategy.go:1860-1914)', () => {
  it('leverage desc states the code range 1-20, never 1-5', () => {
    expect(gridConfig.leverageDesc.en).toContain('1-20')
    expect(gridConfig.leverageDesc.en).not.toContain('1-5')
  })

  it('atrMultiplier desc states the code default 2.0', () => {
    expect(gridConfig.atrMultiplierDesc.en).toContain('2.0')
  })

  it('upper/lower bound descs say 0 = auto-calculate from ATR', () => {
    expect(gridConfig.upperPriceDesc.en).toContain('ATR')
    expect(gridConfig.lowerPriceDesc.en).toContain('ATR')
  })

  it('directionBiasRatio desc states the code default 0.7 = 70%/30%', () => {
    expect(gridConfig.directionBiasRatio.en).toContain('0.7')
    expect(gridConfig.directionBiasRatio.en).toContain('70%/30%')
  })
})

// Risk descs pin the 0B seam truth (trader/exit_mechs_suspend.go
// exitMechsSuspended: env EXIT_MECHS_SUSPENDED unset = SUSPENDED, =0 = ACTIVE).
// Release #22 (UI-GUIDE-TRUTH T2): the old text said "the running binary does
// not move the stop" while the live boot read BE=on · seam=ACTIVE — the desc
// must name BOTH switches, never claim one fixed state.
describe('riskControl seam truth (pinned to exit_mechs_suspend.go 0B)', () => {
  it('breakeven desc names both switches and the env seam', () => {
    expect(riskControl.breakevenDesc.en).toContain('EXIT_MECHS_SUSPENDED=0')
    expect(riskControl.breakevenDesc.en).toContain('SUSPENDED (0B)')
    expect(riskControl.breakevenDesc.en).not.toContain(
      'the running binary does not move the stop'
    )
  })

  it('trailing desc names both switches and the code file', () => {
    expect(riskControl.trailingDesc.en).toContain('EXIT_MECHS_SUSPENDED=0')
    expect(riskControl.trailingDesc.en).toContain('auto_trader_trailing.go')
  })
})

// Risk-control guardrail label truth (pinned to the RiskControlConfig struct
// comments at store/strategy.go:2020-2113).
describe('riskControl guardrail label truth (pinned to store/strategy.go:2020-2113)', () => {
  it('leverage descs state AI-guided with the 1-20 save clamp', () => {
    expect(riskControl.btcEthLeverageDesc.en).toContain('1–20')
    expect(riskControl.altcoinLeverageDesc.en).toContain('1–20')
  })

  it('position-value descs state CODE ENFORCED with defaults 5 (BTC/ETH) and 1 (alt)', () => {
    expect(riskControl.btcEthPositionValueRatioDesc.en).toContain('default 5')
    expect(riskControl.altcoinPositionValueRatioDesc.en).toContain('default 1')
  })

  it('daily loss limit states default ON + env fallback; profit target default OFF', () => {
    expect(riskControl.dailyLossLimit.en).toContain('default ON')
    expect(riskControl.dailyLossLimit.en).toContain('RISK_MAX_DAILY_LOSS_USD')
    expect(riskControl.dailyProfitTarget.en).toContain('default OFF')
  })

  it('max daily trades states default OFF', () => {
    expect(riskControl.maxDailyTrades.en).toContain('default OFF')
  })

  it('consecutive-loss halt states presence-aware inherit 8 / explicit 0 / not master-gated', () => {
    const en = riskControl.consecutiveLossHalt.en
    expect(en).toContain('presence-aware')
    expect(en).toContain('8')
    expect(en).toContain('0 = OFF')
    expect(en).toContain('NOT gated')
  })

  it('re-entry cooldown states default 20, 0 = OFF, not master-gated', () => {
    const en = riskControl.reentryCooldown.en
    expect(en).toContain('default 20')
    expect(en).toContain('0 = OFF')
    expect(en).toContain('NOT gated')
  })

  it('max contracts states default 2 always-on; notional cap default 20 backstop', () => {
    expect(riskControl.maxContractsField.en).toContain('default 2')
    expect(riskControl.maxContractsField.en).toContain('always-on')
    expect(riskControl.notionalCapField.en).toContain('default 20')
    expect(riskControl.notionalCapField.en).toContain('safety backstop')
  })

  it('blackout start/end state toggle default OFF + NT8 SL/TP + Chicago window', () => {
    expect(riskControl.blackoutStart.en).toContain('default OFF')
    expect(riskControl.blackoutStart.en).toContain('NT8-side SL/TP')
    expect(riskControl.blackoutEnd.en).toContain('America/Chicago')
  })

  it('consistency cap states default OFF + prior-day profit requirement', () => {
    expect(riskControl.consistencyPctField.en).toContain('default OFF')
    expect(riskControl.consistencyPctField.en).toContain('prior-day profit')
  })

  it('hold discipline desc states Emergency Flat + drawdown bypass + default OFF', () => {
    const en = riskControl.holdDisciplineDesc.en
    expect(en.toLowerCase()).toContain('emergency flat')
    expect(en).toContain('drawdown')
    expect(en.toLowerCase()).toContain('default off')
  })
})

// Day-plan label truth (pinned to store/strategy.go:968-1184 + resolve_source.go
// shipped defaults + structural_geometry.go C5).
describe('dayPlan label truth (pinned to store/strategy.go + resolve_source.go)', () => {
  it('htfSeats label states unset = default 2 (legacy constant) and 0 = none', () => {
    expect(planStrings.htfSeats.en).toContain('default 2')
    expect(planStrings.htfSeats.en).toContain('0 = no HTF seating')
  })

  it('plannerModel label states empty falls back to the primary model (RECON #9)', () => {
    expect(planStrings.plannerModel.en).toContain('strategy primary model')
    expect(planStrings.plannerModel.en).toContain('RECON #9')
  })
})

// Prompt-section + language + custom-prompt truth (store/strategy.go:721-723,
// 1915-1923, 792).
describe('prompt section + language label truth', () => {
  it('the three prompt-section descs name the editable System Prompt sections', () => {
    for (const k of [
      promptSections.roleDefinitionDesc,
      promptSections.entryStandardsDesc,
      promptSections.decisionProcessDesc,
    ]) {
      expect(k.en).toContain('editable System Prompt section')
    }
    expect(promptSections.roleDefinitionDesc.en).toContain('identity')
    expect(promptSections.entryStandardsDesc.en).toContain('signal')
    expect(promptSections.decisionProcessDesc.en).toContain('decision')
  })

  it('custom prompt desc states appended to the System Prompt', () => {
    expect(translations.en.strategyStudio.customPromptDesc).toContain(
      'appended'
    )
    expect(translations.en.strategyStudio.customPromptDesc).toContain(
      'System Prompt'
    )
  })

  it('language label states prompt + data formatting with en/zh', () => {
    expect(translations.en.language).toContain('prompt')
    expect(translations.en.language).toContain('en/zh')
    expect(translations.zh.language).toContain('提示词')
  })
})

// Picture HTF labels pin the code defaults (store/strategy.go:921-922
// PictureHtfDefaultEntryWindowSec=360, PictureHtfDefaultFreshnessSec=30).
describe('picture HTF label truth (pinned to store/strategy.go:921-922)', () => {
  it('entry window label states default 360', () => {
    expect(planStrings.pictureEntryWindowSec.en).toContain('360')
  })

  it('freshness label states default 30', () => {
    expect(planStrings.pictureFreshnessSec.en).toContain('30')
  })
})
