import { useState, type ReactNode } from 'react'
import { Shield, AlertTriangle } from 'lucide-react'
import type { RiskControlConfig } from '../../types'
import {
  RISK_DEFAULT_MAX_CONTRACTS_PER_ORDER,
  RISK_DEFAULT_MAX_POSITIONS,
  RISK_DEFAULT_TRAILING_ARM_POINTS,
} from '../../riskControlDefaults'
import { riskControl, ts } from '../../i18n/strategy-translations'
import type { StudioEffective } from '../../lib/api/strategyEffective'
import {
  EffectiveLine,
  EffectiveSavedNote,
  effectiveValueText,
} from './EffectiveChip'
import { MentorModeToggle } from './MentorModeToggle'
import { MentorTuningPanel } from './MentorTuningPanel'

// ClampedNumberInput edits a single clamped number (e.g. min R/R). It holds the
// RAW typed text in local state WHILE editing — so clearing + retyping work — and
// parses/clamps to [min,max] ONLY on blur/commit (Enter blurs). On an empty/invalid
// commit it keeps the existing saved value (NOT a hardcoded default), so the field
// no longer snaps back to the default the moment it's cleared. Mirrors the shipped
// IndicatorEditor PeriodInput fix. The saved shape (a number) is unchanged.
function ClampedNumberInput({
  value,
  fallback,
  min,
  max,
  step,
  disabled,
  onCommit,
  testId,
}: {
  value: number | undefined
  fallback: number
  min: number
  max: number
  step?: number
  disabled?: boolean
  onCommit: (n: number) => void
  testId?: string
}) {
  const [draft, setDraft] = useState<string | null>(null)
  const saved = String(value ?? fallback)
  const shown = draft ?? saved

  const commit = () => {
    if (draft === null) return
    let n = parseFloat(draft.trim())
    if (isNaN(n)) n = value ?? fallback // empty/invalid → keep the saved value
    n = Math.min(max, Math.max(min, n)) // clamp to match the backend ClampLimits
    onCommit(n)
    setDraft(null)
  }

  return (
    <input
      type="number"
      value={shown}
      onFocus={() => !disabled && setDraft(saved)}
      onChange={(e) => !disabled && setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === 'Enter') (e.target as HTMLInputElement).blur()
      }}
      disabled={disabled}
      min={min}
      max={max}
      step={step}
      data-testid={testId}
      className="w-20 px-3 py-2 rounded ml-2"
      style={{
        background: '#1E2329',
        border: '1px solid #2B3139',
        color: '#EAECEF',
      }}
    />
  )
}

interface RiskControlEditorProps {
  config: RiskControlConfig
  onChange: (config: RiskControlConfig) => void
  disabled?: boolean
  language: string
  // CME futures (e.g. MNQ) size by contract count, not exchange leverage, and
  // settle in USD — so the leverage tiers are hidden and the value currency shows "USD".
  isFutures?: boolean
  // W1 (g) — the server's effective rows for the SAVED strategy (GET
  // /api/strategies/:id/effective). Optional: absent → no chips, and a row the
  // map does not carry renders nothing extra.
  effective?: StudioEffective
  // Shown in the Mentor mode confirmation.
  strategyName?: string
}

const RC_PATH = 'ai_config.risk_control.'

// A small on/off switch (Chunk 6). Writes a guardrail's `…_enabled` flag — these
// toggles only set kernel-gate guardrail config; they have NO path to the broker-
// layer live-account block, which stays hard-blocked regardless.
function Toggle({
  on,
  onChange,
  disabled,
  testId,
}: {
  on: boolean
  onChange: (v: boolean) => void
  disabled?: boolean
  testId?: string
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      disabled={disabled}
      data-testid={testId}
      onClick={() => !disabled && onChange(!on)}
      className="relative inline-block w-9 h-5 rounded-full transition-colors shrink-0"
      style={{
        background: on ? '#0ECB81' : '#2B3139',
        opacity: disabled ? 0.5 : 1,
      }}
    >
      <span
        className="absolute top-0.5 w-4 h-4 rounded-full bg-white transition-all"
        style={{ left: on ? '18px' : '2px' }}
      />
    </button>
  )
}

// A guardrail row: label + on/off toggle + the value input (dimmed when off).
function GuardrailRow({
  label,
  enabled,
  onToggle,
  disabled,
  children,
  testId,
}: {
  label: string
  enabled: boolean
  onToggle: (v: boolean) => void
  disabled?: boolean
  children: ReactNode
  testId?: string
}) {
  return (
    <div
      className="p-4 rounded-lg"
      style={{
        background: '#0B0E11',
        border: '1px solid #2B3139',
        opacity: enabled ? 1 : 0.55,
      }}
    >
      <div className="flex items-center justify-between gap-2 mb-2">
        <label className="text-sm" style={{ color: '#EAECEF' }}>
          {label}
        </label>
        <Toggle
          on={enabled}
          onChange={onToggle}
          disabled={disabled}
          testId={testId}
        />
      </div>
      {children}
    </div>
  )
}

// 6.4 (final-bundle 2026-08-19, ruling B): the size-cap "enabled" toggles were
// DEAD controls — zero Go readers, the clamps are deliberately always-on venue
// safety (census #53 register rows 1-2). The fake toggles are gone; the row
// states the truth instead. Old stored *_enabled values are ignored harmlessly
// (schema-tolerant: the Go fields still parse, nothing reads them).
function AlwaysOnRow({
  label,
  badge,
  children,
}: {
  label: string
  badge: string
  children: ReactNode
}) {
  return (
    <div
      className="p-4 rounded-lg"
      style={{ background: '#0B0E11', border: '1px solid #2B3139' }}
    >
      <div className="flex items-center justify-between gap-2 mb-2">
        <label className="text-sm" style={{ color: '#EAECEF' }}>
          {label}
        </label>
        <span
          className="text-[10px] font-bold px-2 py-0.5 rounded-full"
          style={{
            color: '#0ECB81',
            background: 'rgba(14,203,129,0.12)',
            border: '1px solid rgba(14,203,129,0.3)',
          }}
        >
          {badge}
        </span>
      </div>
      {children}
    </div>
  )
}

export function RiskControlEditor({
  config,
  onChange,
  disabled,
  language,
  isFutures = false,
  effective,
  strategyName,
}: RiskControlEditorProps) {
  // W1 (g) — one server row per schema path; undefined → no chip.
  const eff = (leaf: string) => effective?.byPath[RC_PATH + leaf]
  const onOff = ts(riskControl.effOnOff, language)
  const valueLbl = ts(riskControl.effValue, language)
  // The futures risk panel prints the server's resolved value (or n/a).
  const maxContractsText = effectiveValueText(eff('max_contracts_per_order'))
  const notionalText = effectiveValueText(eff('max_notional_leverage'))
  const updateField = <K extends keyof RiskControlConfig>(
    key: K,
    value: RiskControlConfig[K]
  ) => {
    if (!disabled) {
      onChange({ ...config, [key]: value })
    }
  }

  return (
    <div className="space-y-6">
      {effective && <EffectiveSavedNote language={language} />}
      <MentorModeToggle
        on={config.mentor_mode === true}
        onChange={(v) => updateField('mentor_mode', v)}
        disabled={disabled}
        language={language}
        strategyName={strategyName}
        orderGate={effective?.mentorPlace}
        windowStart={config.mentor_window_start}
        windowMinutes={config.mentor_window_minutes}
        onWindowStartChange={(v) => updateField('mentor_window_start', v)}
        onWindowMinutesChange={(v) => updateField('mentor_window_minutes', v)}
      />
      {config.mentor_mode === true && (
        <MentorTuningPanel
          tuning={config.mentor_tuning}
          onChange={(v) => updateField('mentor_tuning', v)}
          disabled={disabled}
          language={language}
        />
      )}
      {/* Hold discipline (hold-lock) — applies to futures + crypto; default OFF */}
      <div
        className="p-4 rounded-lg"
        style={{ background: '#0B0E11', border: '1px solid #2B3139' }}
      >
        <div className="flex items-center justify-between gap-2">
          <label className="text-sm font-medium" style={{ color: '#EAECEF' }}>
            🔒 {ts(riskControl.holdDiscipline, language)}
          </label>
          <Toggle
            on={config.hold_discipline === true}
            onChange={(v) => updateField('hold_discipline', v)}
            disabled={disabled}
          />
        </div>
        <p className="text-xs mt-2" style={{ color: '#848E9C' }}>
          {ts(riskControl.holdDisciplineDesc, language)}
        </p>
        <EffectiveLine knob={eff('hold_discipline')} />
      </div>

      {/* Auto-breakeven — NT8 futures; default OFF */}
      <div
        className="p-4 rounded-lg"
        style={{ background: '#0B0E11', border: '1px solid #2B3139' }}
      >
        <div className="flex items-center justify-between gap-2">
          <label className="text-sm font-medium" style={{ color: '#EAECEF' }}>
            🎯 {ts(riskControl.breakeven, language)}
          </label>
          <Toggle
            on={config.breakeven_enabled === true}
            onChange={(v) => updateField('breakeven_enabled', v)}
            disabled={disabled}
          />
        </div>
        <p className="text-xs mt-2" style={{ color: '#848E9C' }}>
          {ts(riskControl.breakevenDesc, language)}
        </p>
        <div className="flex items-center gap-2 mt-2">
          <span className="text-xs" style={{ color: '#848E9C' }}>
            {ts(riskControl.breakevenTrigger, language)}
          </span>
          <ClampedNumberInput
            value={config.breakeven_trigger_points}
            fallback={50}
            min={1}
            max={1000}
            step={5}
            disabled={disabled || config.breakeven_enabled !== true}
            onCommit={(n) => updateField('breakeven_trigger_points', n)}
          />
        </div>
        <EffectiveLine knob={eff('breakeven_enabled')} label={onOff} />
        <EffectiveLine
          knob={eff('breakeven_trigger_points')}
          label={ts(riskControl.breakevenTrigger, language)}
        />
      </div>

      {/* Trailing profit (Phase 3B) — NT8 futures; mechanical, default OFF */}
      <div
        className="p-4 rounded-lg"
        style={{ background: '#0B0E11', border: '1px solid #2B3139' }}
      >
        <div className="flex items-center justify-between gap-2">
          <label className="text-sm font-medium" style={{ color: '#EAECEF' }}>
            📈 {ts(riskControl.trailing, language)}
          </label>
          <Toggle
            on={config.trailing_enabled === true}
            onChange={(v) => updateField('trailing_enabled', v)}
            disabled={disabled}
          />
        </div>
        <p className="text-xs mt-2" style={{ color: '#848E9C' }}>
          {ts(riskControl.trailingDesc, language)}
        </p>
        <div className="flex flex-wrap items-center gap-3 mt-2">
          <span className="text-xs" style={{ color: '#848E9C' }}>
            {ts(riskControl.trailingMult, language)}
          </span>
          <ClampedNumberInput
            value={config.trailing_atr_mult}
            fallback={2.0}
            min={0.5}
            max={10}
            step={0.5}
            disabled={disabled || config.trailing_enabled !== true}
            onCommit={(n) => updateField('trailing_atr_mult', n)}
          />
          <span className="text-xs" style={{ color: '#848E9C' }}>
            {ts(riskControl.trailingPeriod, language)}
          </span>
          <ClampedNumberInput
            value={config.trailing_atr_period}
            fallback={14}
            min={5}
            max={50}
            step={1}
            disabled={disabled || config.trailing_enabled !== true}
            onCommit={(n) => updateField('trailing_atr_period', n)}
          />
        </div>
        <div className="flex flex-wrap items-center gap-3 mt-2">
          <span className="text-xs" style={{ color: '#848E9C' }}>
            {ts(riskControl.trailingArm, language)}
          </span>
          <select
            data-testid="trailing-arm"
            value={config.trailing_arm || 'after_breakeven'}
            onChange={(e) => updateField('trailing_arm', e.target.value)}
            disabled={disabled || config.trailing_enabled !== true}
            className="bg-[#181C21] text-xs text-[#EAECEF] border border-[#2B3139] rounded px-2 py-1"
          >
            <option value="after_breakeven">
              {ts(riskControl.trailingArmBE, language)}
            </option>
            <option value="after_trigger_points">
              {ts(riskControl.trailingArmPts, language)}
            </option>
            <option value="immediate">
              {ts(riskControl.trailingArmNow, language)}
            </option>
          </select>
          {config.trailing_arm === 'after_trigger_points' && (
            <ClampedNumberInput
              value={config.trailing_arm_points}
              fallback={RISK_DEFAULT_TRAILING_ARM_POINTS}
              min={1}
              max={1000}
              step={5}
              disabled={disabled || config.trailing_enabled !== true}
              onCommit={(n) => updateField('trailing_arm_points', n)}
              testId="trailing-arm-points-input"
            />
          )}
        </div>
        <EffectiveLine knob={eff('trailing_enabled')} label={onOff} />
        <EffectiveLine
          knob={eff('trailing_atr_mult')}
          label={ts(riskControl.trailingMult, language)}
        />
        <EffectiveLine
          knob={eff('trailing_atr_period')}
          label={ts(riskControl.trailingPeriod, language)}
        />
        <EffectiveLine
          knob={eff('trailing_arm')}
          label={ts(riskControl.trailingArm, language)}
        />
        <EffectiveLine
          knob={eff('trailing_arm_points')}
          label={ts(riskControl.trailingArmPts, language)}
        />
      </div>

      {/* Position Limits */}
      <div>
        <div className="flex items-center gap-2 mb-4">
          <Shield className="w-5 h-5" style={{ color: '#F0B90B' }} />
          <h3 className="font-medium" style={{ color: '#EAECEF' }}>
            {ts(riskControl.positionLimits, language)}
          </h3>
        </div>

        <div className="grid grid-cols-1 gap-4 mb-4">
          <div
            className="p-4 rounded-lg"
            style={{ background: '#0B0E11', border: '1px solid #0ECB81' }}
          >
            <label className="block text-sm mb-1" style={{ color: '#EAECEF' }}>
              {ts(riskControl.maxPositions, language)}
            </label>
            <p className="text-xs mb-2" style={{ color: '#848E9C' }}>
              {ts(riskControl.maxPositionsDesc, language)}
            </p>
            <div className="flex items-center gap-3">
              {/* User-set + code-enforced. ClampLimits bounds this to [1,3] on
                  save AND at decision time (store/strategy.go ClampLimits, const
                  MaxPositions=3 = CEILING) — the onChange clamp keeps the shown
                  value equal to the saved value (no "typed 5, saved 3" surprise).
                  An UNSET knob shows RISK_DEFAULT_MAX_POSITIONS — the ClampLimits
                  FLOOR, which is what the runtime resolves, not the ceiling. To
                  allow >3, raise the MaxPositions const (token-cost decision). */}
              <input
                type="number"
                value={config.max_positions ?? RISK_DEFAULT_MAX_POSITIONS}
                data-testid="max-positions-input"
                onChange={(e) =>
                  updateField(
                    'max_positions',
                    Math.min(3, Math.max(1, parseInt(e.target.value) || 1))
                  )
                }
                disabled={disabled}
                min={1}
                max={3}
                step={1}
                className="w-20 px-3 py-2 rounded font-mono"
                style={{
                  background: '#1E2329',
                  border: '1px solid #2B3139',
                  color: '#EAECEF',
                }}
              />
              <span className="text-xs" style={{ color: '#848E9C' }}>
                user-set · enforced (range 1–3)
              </span>
            </div>
            <EffectiveLine knob={eff('max_positions')} />
          </div>
        </div>

        {/* Trading Leverage (Exchange) — crypto-only; CME futures have no
            exchange leverage (contract sizing handles exposure). */}
        {!isFutures && (
          <>
            <div className="mb-2">
              <p
                className="text-xs font-medium mb-2"
                style={{ color: '#F0B90B' }}
              >
                {ts(riskControl.tradingLeverage, language)}
              </p>
            </div>
            <div className="grid grid-cols-2 gap-4 mb-4">
              <div
                className="p-4 rounded-lg"
                style={{ background: '#0B0E11', border: '1px solid #2B3139' }}
              >
                <label
                  className="block text-sm mb-1"
                  style={{ color: '#EAECEF' }}
                >
                  {ts(riskControl.btcEthLeverage, language)}
                </label>
                <p className="text-xs mb-2" style={{ color: '#848E9C' }}>
                  {ts(riskControl.btcEthLeverageDesc, language)}
                </p>
                <div className="flex items-center gap-2">
                  <input
                    type="range"
                    value={config.btc_eth_max_leverage ?? 5}
                    onChange={(e) =>
                      updateField(
                        'btc_eth_max_leverage',
                        parseInt(e.target.value)
                      )
                    }
                    disabled={disabled}
                    min={1}
                    max={20}
                    className="flex-1 accent-yellow-500"
                  />
                  <span
                    className="w-12 text-center font-mono"
                    style={{ color: '#F0B90B' }}
                  >
                    {config.btc_eth_max_leverage ?? 5}x
                  </span>
                </div>
              </div>

              <div
                className="p-4 rounded-lg"
                style={{ background: '#0B0E11', border: '1px solid #2B3139' }}
              >
                <label
                  className="block text-sm mb-1"
                  style={{ color: '#EAECEF' }}
                >
                  {ts(riskControl.altcoinLeverage, language)}
                </label>
                <p className="text-xs mb-2" style={{ color: '#848E9C' }}>
                  {ts(riskControl.altcoinLeverageDesc, language)}
                </p>
                <div className="flex items-center gap-2">
                  <input
                    type="range"
                    value={config.altcoin_max_leverage ?? 5}
                    onChange={(e) =>
                      updateField(
                        'altcoin_max_leverage',
                        parseInt(e.target.value)
                      )
                    }
                    disabled={disabled}
                    min={1}
                    max={20}
                    className="flex-1 accent-yellow-500"
                  />
                  <span
                    className="w-12 text-center font-mono"
                    style={{ color: '#F0B90B' }}
                  >
                    {config.altcoin_max_leverage ?? 5}x
                  </span>
                </div>
              </div>
            </div>
          </>
        )}

        {/* Position Value Ratio — crypto-only (CODE ENFORCED for crypto). CME
            futures do NOT use a USD value-ratio: they size by contract count + the
            editable equity×N notional cap (Prop-Firm Guardrails section below). So
            these crypto tiles are HIDDEN on futures — showing "CODE ENFORCED" /
            "System enforced" there would be false (the futures gate uses equity×N,
            not these ratios). Crypto keeps them (true for crypto). */}
        {!isFutures && (
          <>
            <div className="mb-2">
              <p className="text-xs font-medium" style={{ color: '#0ECB81' }}>
                {ts(riskControl.positionValueRatio, language)}
              </p>
              <p className="text-xs mt-1" style={{ color: '#848E9C' }}>
                {ts(riskControl.positionValueRatioDesc, language)}
              </p>
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div
                className="p-4 rounded-lg"
                style={{ background: '#0B0E11', border: '1px solid #0ECB81' }}
              >
                <label
                  className="block text-sm mb-1"
                  style={{ color: '#EAECEF' }}
                >
                  {ts(riskControl.btcEthPositionValueRatio, language)}
                </label>
                <p className="text-xs mb-2" style={{ color: '#848E9C' }}>
                  {ts(riskControl.btcEthPositionValueRatioDesc, language)}
                </p>
                <div className="flex items-center gap-2">
                  <span
                    className="w-12 text-center font-mono"
                    style={{ color: '#0ECB81' }}
                  >
                    {config.btc_eth_max_position_value_ratio ?? 5}x
                  </span>
                  <span className="text-xs" style={{ color: '#848E9C' }}>
                    System enforced
                  </span>
                </div>
              </div>

              <div
                className="p-4 rounded-lg"
                style={{ background: '#0B0E11', border: '1px solid #0ECB81' }}
              >
                <label
                  className="block text-sm mb-1"
                  style={{ color: '#EAECEF' }}
                >
                  {ts(riskControl.altcoinPositionValueRatio, language)}
                </label>
                <p className="text-xs mb-2" style={{ color: '#848E9C' }}>
                  {ts(riskControl.altcoinPositionValueRatioDesc, language)}
                </p>
                <div className="flex items-center gap-2">
                  <span
                    className="w-12 text-center font-mono"
                    style={{ color: '#0ECB81' }}
                  >
                    {config.altcoin_max_position_value_ratio ?? 1}x
                  </span>
                  <span className="text-xs" style={{ color: '#848E9C' }}>
                    System enforced
                  </span>
                </div>
              </div>
            </div>
          </>
        )}
      </div>

      {/* Risk Parameters */}
      <div>
        <div className="flex items-center gap-2 mb-4">
          <AlertTriangle className="w-5 h-5" style={{ color: '#F6465D' }} />
          <h3 className="font-medium" style={{ color: '#EAECEF' }}>
            {ts(riskControl.riskParameters, language)}
          </h3>
        </div>

        <div className="grid grid-cols-2 gap-4">
          <div
            className="p-4 rounded-lg"
            style={{ background: '#0B0E11', border: '1px solid #2B3139' }}
          >
            <label className="block text-sm mb-1" style={{ color: '#EAECEF' }}>
              {ts(riskControl.minRiskReward, language)}
            </label>
            <p className="text-xs mb-2" style={{ color: '#848E9C' }}>
              {ts(riskControl.minRiskRewardDesc, language)}
            </p>
            <div className="flex items-center">
              <span style={{ color: '#848E9C' }}>1:</span>
              <ClampedNumberInput
                value={config.min_risk_reward_ratio}
                fallback={3}
                min={1}
                max={10}
                step={0.5}
                disabled={disabled}
                onCommit={(n) => updateField('min_risk_reward_ratio', n)}
              />
            </div>
            <EffectiveLine knob={eff('min_risk_reward_ratio')} />
          </div>

          {/* Max Margin Usage — crypto-only + ADVICE-ONLY. Stored as a 0-1
              fraction, shown/edited as a percent. The value is NOT gate-enforced:
              it only feeds the AI prompt (engine_prompt.go) as a hint — no code
              blocks a trade for it (hence the honest "AI-guided" label). The
              onChange clamps to [0.1,1.0] = [10,100]%, matching ClampLimits
              (store/strategy.go) so the shown value always equals the saved value.
              Hidden on futures (like the PVR tiles): "% margin usage" is a crypto-
              margin concept; futures margin is a per-contract bond, surfaced in the
              Futures Risk panel below. */}
          {!isFutures && (
            <div
              className="p-4 rounded-lg"
              style={{ background: '#0B0E11', border: '1px solid #0ECB81' }}
            >
              <label
                className="block text-sm mb-1"
                style={{ color: '#EAECEF' }}
              >
                {ts(riskControl.maxMarginUsage, language)}
              </label>
              <p className="text-xs mb-2" style={{ color: '#848E9C' }}>
                {ts(riskControl.maxMarginUsageDesc, language)}
              </p>
              <div className="flex items-center gap-2">
                <input
                  type="number"
                  value={Math.round((config.max_margin_usage ?? 0.9) * 100)}
                  onChange={(e) =>
                    updateField(
                      'max_margin_usage',
                      Math.min(
                        1,
                        Math.max(0.1, (parseFloat(e.target.value) || 90) / 100)
                      )
                    )
                  }
                  disabled={disabled}
                  min={10}
                  max={100}
                  step={5}
                  className="w-20 px-3 py-2 rounded font-mono"
                  style={{
                    background: '#1E2329',
                    border: '1px solid #2B3139',
                    color: '#EAECEF',
                  }}
                />
                <span style={{ color: '#848E9C' }}>%</span>
                <span className="text-xs" style={{ color: '#F6465D' }}>
                  AI-guided (not enforced)
                </span>
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Entry Requirements */}
      <div>
        <div className="flex items-center gap-2 mb-4">
          <Shield className="w-5 h-5" style={{ color: '#0ECB81' }} />
          <h3 className="font-medium" style={{ color: '#EAECEF' }}>
            {ts(riskControl.entryRequirements, language)}
          </h3>
        </div>

        <div className="grid grid-cols-2 gap-4">
          {/* Min position size — crypto-only. DEAD on futures: the min-size gate
              (enforceMinPositionSize) runs ONLY in the crypto sizing branch;
              futures size by contract count via futuresOrderQuantity, so this USD
              floor has no futures reader. Hidden on futures like the leverage/PVR
              tiles — showing an "enforced" USD floor there would be false. Crypto
              keeps it (real). */}
          {!isFutures && (
            <div
              className="p-4 rounded-lg"
              style={{ background: '#0B0E11', border: '1px solid #0ECB81' }}
            >
              <label
                className="block text-sm mb-1"
                style={{ color: '#EAECEF' }}
              >
                {ts(riskControl.minPositionSize, language)}
              </label>
              <p className="text-xs mb-2" style={{ color: '#848E9C' }}>
                {ts(riskControl.minPositionSizeDesc, language)}
              </p>
              <div className="flex items-center gap-2">
                {/* User-set + code-enforced. ClampLimits bounds this to [10,1000]
                    on save AND at decision time; the trader gate
                    (enforceMinPositionSize) plus the kernel reject-floor (12 gen /
                    the kernel reject-floor (engine_position.go) remains as defense-in-depth, so
                    a user value only ever RAISES the effective minimum — never
                    below the floor. The onChange clamp keeps shown == saved. */}
                <input
                  type="number"
                  value={config.min_position_size ?? 12}
                  onChange={(e) =>
                    updateField(
                      'min_position_size',
                      Math.min(
                        1000,
                        Math.max(10, parseFloat(e.target.value) || 12)
                      )
                    )
                  }
                  disabled={disabled}
                  min={10}
                  max={1000}
                  step={1}
                  className="w-24 px-3 py-2 rounded font-mono"
                  style={{
                    background: '#1E2329',
                    border: '1px solid #2B3139',
                    color: '#EAECEF',
                  }}
                />
                <span className="ml-1" style={{ color: '#848E9C' }}>
                  USD
                </span>
                <span className="text-xs" style={{ color: '#848E9C' }}>
                  user-set · enforced
                </span>
              </div>
            </div>
          )}

          <div
            className="p-4 rounded-lg"
            style={{ background: '#0B0E11', border: '1px solid #2B3139' }}
          >
            <label className="block text-sm mb-1" style={{ color: '#EAECEF' }}>
              {ts(riskControl.minConfidence, language)}
            </label>
            <p className="text-xs mb-2" style={{ color: '#848E9C' }}>
              {ts(riskControl.minConfidenceDesc, language)}
              {/* W1 (g): the literal ' · unset/0 → default 60' hint is gone —
                  the chip below prints what the server resolves for the saved
                  row (value · origin · scope), never a typed default. */}
            </p>
            <div className="flex items-center gap-2">
              <input
                type="range"
                value={config.min_confidence ?? 60}
                onChange={(e) =>
                  updateField('min_confidence', parseInt(e.target.value))
                }
                disabled={disabled}
                min={50}
                max={100}
                className="flex-1 accent-green-500"
              />
              <span
                className="w-12 text-center font-mono"
                style={{ color: '#0ECB81' }}
              >
                {config.min_confidence ?? 60}
              </span>
            </div>
            <EffectiveLine knob={eff('min_confidence')} />
          </div>
        </div>
      </div>

      {/* === Prop-Firm Guardrails (Strategy Studio Phase 1, Chunk 6) === */}
      <div>
        <div className="flex items-center justify-between gap-3 mb-1">
          <div className="flex items-center gap-2">
            <Shield className="w-5 h-5" style={{ color: '#F0B90B' }} />
            <h3 className="font-medium" style={{ color: '#EAECEF' }}>
              {ts(riskControl.guardrailsTitle, language)}
            </h3>
          </div>
          {/* MASTER SWITCH */}
          <div className="flex items-center gap-2">
            <span className="text-xs" style={{ color: '#EAECEF' }}>
              {ts(riskControl.masterSwitch, language)}
            </span>
            <Toggle
              on={config.guardrails_enabled ?? true}
              onChange={(v) => updateField('guardrails_enabled', v)}
              disabled={disabled}
            />
          </div>
        </div>
        <p className="text-xs mb-4" style={{ color: '#848E9C' }}>
          {ts(riskControl.guardrailsDesc, language)}{' '}
          {ts(riskControl.masterSwitchDesc, language)}
        </p>
        <EffectiveLine
          knob={eff('guardrails_enabled')}
          label={ts(riskControl.masterSwitch, language)}
          className="-mt-3 mb-4"
        />

        <div className="grid grid-cols-2 gap-4">
          <GuardrailRow
            label={ts(riskControl.dailyLossLimit, language)}
            enabled={config.daily_loss_enabled ?? true}
            onToggle={(v) => updateField('daily_loss_enabled', v)}
            disabled={disabled}
          >
            <input
              type="number"
              value={config.daily_loss_limit_usd ?? ''}
              placeholder="e.g. 500"
              onChange={(e) =>
                updateField(
                  'daily_loss_limit_usd',
                  parseFloat(e.target.value) || 0
                )
              }
              disabled={disabled}
              min={0}
              className="w-full px-3 py-2 rounded font-mono"
              style={{
                background: '#1E2329',
                border: '1px solid #2B3139',
                color: '#EAECEF',
              }}
            />
            {/* 6.5 — the silent env fallback, stated (only enforced while the
                guardrails master is ON) */}
            {!config.daily_loss_limit_usd && (
              <p className="text-xs mt-1" style={{ color: '#848E9C' }}>
                empty/0 → $500 env default (RISK_MAX_DAILY_LOSS_USD) while the
                master switch is ON
              </p>
            )}
            <EffectiveLine knob={eff('daily_loss_enabled')} label={onOff} />
            <EffectiveLine
              knob={eff('daily_loss_limit_usd')}
              label={valueLbl}
            />
          </GuardrailRow>

          <GuardrailRow
            label={ts(riskControl.dailyProfitTarget, language)}
            enabled={config.daily_profit_enabled ?? false}
            onToggle={(v) => updateField('daily_profit_enabled', v)}
            disabled={disabled}
          >
            <input
              type="number"
              value={config.daily_profit_target_usd ?? ''}
              placeholder="e.g. 1000"
              onChange={(e) =>
                updateField(
                  'daily_profit_target_usd',
                  parseFloat(e.target.value) || 0
                )
              }
              disabled={disabled}
              min={0}
              className="w-full px-3 py-2 rounded font-mono"
              style={{
                background: '#1E2329',
                border: '1px solid #2B3139',
                color: '#EAECEF',
              }}
            />
            <EffectiveLine knob={eff('daily_profit_enabled')} label={onOff} />
            <EffectiveLine
              knob={eff('daily_profit_target_usd')}
              label={valueLbl}
            />
          </GuardrailRow>

          <GuardrailRow
            label={ts(riskControl.maxDailyTrades, language)}
            enabled={config.max_daily_trades_enabled ?? false}
            onToggle={(v) => updateField('max_daily_trades_enabled', v)}
            disabled={disabled}
          >
            <input
              type="number"
              value={config.max_daily_trades ?? ''}
              placeholder="e.g. 5"
              onChange={(e) =>
                updateField('max_daily_trades', parseInt(e.target.value) || 0)
              }
              disabled={disabled}
              min={0}
              className="w-full px-3 py-2 rounded font-mono"
              style={{
                background: '#1E2329',
                border: '1px solid #2B3139',
                color: '#EAECEF',
              }}
            />
            <EffectiveLine
              knob={eff('max_daily_trades_enabled')}
              label={onOff}
            />
            <EffectiveLine knob={eff('max_daily_trades')} label={valueLbl} />
          </GuardrailRow>

          {/* W1 (settings truth, 2026-09-23) — PRESENCE-AWARE. absent/null =
              INHERIT (env BREAKER_HALT_N, else 8 — the breaker is ON), an
              explicit 0 = OFF, N = N. The old row showed an absent breaker as
              OFF while the runtime enforced 8, its OFF wrote a 0 no save could
              store, and ON wrote a literal 2. OFF now writes 0; ON writes null
              (inherit — a PUT keeps absent keys, so only null clears a 0);
              clearing the box writes null, never 0. */}
          <GuardrailRow
            label={ts(riskControl.consecutiveLossHalt, language)}
            enabled={config.consecutive_loss_halt !== 0}
            onToggle={(v) => updateField('consecutive_loss_halt', v ? null : 0)}
            disabled={disabled}
            testId="breaker-toggle"
          >
            <input
              type="number"
              data-testid="breaker-halt-input"
              value={config.consecutive_loss_halt ?? ''}
              placeholder={language === 'zh' ? '继承' : 'inherit'}
              onChange={(e) => {
                const raw = e.target.value.trim()
                if (raw === '') {
                  updateField('consecutive_loss_halt', null)
                  return
                }
                const n = parseInt(raw, 10)
                if (!Number.isNaN(n)) {
                  updateField('consecutive_loss_halt', Math.max(0, n))
                }
              }}
              disabled={disabled}
              min={0}
              className="w-full px-3 py-2 rounded font-mono"
              style={{
                background: '#1E2329',
                border: '1px solid #2B3139',
                color: '#EAECEF',
              }}
            />
            <EffectiveLine knob={eff('consecutive_loss_halt')} />
          </GuardrailRow>

          {isFutures && (
            <GuardrailRow
              label={ts(riskControl.reentryCooldown, language)}
              enabled={(config.reentry_cooldown_minutes ?? 0) > 0}
              onToggle={(v) =>
                updateField('reentry_cooldown_minutes', v ? 20 : 0)
              }
              disabled={disabled}
            >
              <input
                type="number"
                value={config.reentry_cooldown_minutes || ''}
                placeholder="e.g. 20"
                onChange={(e) =>
                  updateField(
                    'reentry_cooldown_minutes',
                    parseInt(e.target.value) || 0
                  )
                }
                disabled={disabled}
                min={0}
                className="w-full px-3 py-2 rounded font-mono"
                style={{
                  background: '#1E2329',
                  border: '1px solid #2B3139',
                  color: '#EAECEF',
                }}
              />
              <EffectiveLine knob={eff('reentry_cooldown_minutes')} />
            </GuardrailRow>
          )}

          <GuardrailRow
            label={ts(riskControl.consistencyPctField, language)}
            enabled={config.consistency_enabled ?? false}
            onToggle={(v) => updateField('consistency_enabled', v)}
            disabled={disabled}
          >
            <input
              type="number"
              value={config.consistency_max_day_pct ?? ''}
              placeholder="e.g. 50"
              onChange={(e) =>
                updateField(
                  'consistency_max_day_pct',
                  parseFloat(e.target.value) || 0
                )
              }
              disabled={disabled}
              min={0}
              max={100}
              className="w-full px-3 py-2 rounded font-mono"
              style={{
                background: '#1E2329',
                border: '1px solid #2B3139',
                color: '#EAECEF',
              }}
            />
            <EffectiveLine knob={eff('consistency_enabled')} label={onOff} />
            <EffectiveLine
              knob={eff('consistency_max_day_pct')}
              label={valueLbl}
            />
          </GuardrailRow>

          <AlwaysOnRow
            label={ts(riskControl.maxContractsField, language)}
            badge={ts(riskControl.alwaysActive, language)}
          >
            <input
              type="number"
              value={config.max_contracts_per_order ?? ''}
              placeholder={String(RISK_DEFAULT_MAX_CONTRACTS_PER_ORDER)}
              data-testid="max-contracts-input"
              onChange={(e) =>
                updateField(
                  'max_contracts_per_order',
                  parseInt(e.target.value) || 0
                )
              }
              disabled={disabled}
              min={0}
              className="w-full px-3 py-2 rounded font-mono"
              style={{
                background: '#1E2329',
                border: '1px solid #2B3139',
                color: '#EAECEF',
              }}
            />
            <EffectiveLine knob={eff('max_contracts_per_order')} />
          </AlwaysOnRow>

          <AlwaysOnRow
            label={ts(riskControl.notionalCapField, language)}
            badge={ts(riskControl.alwaysActive, language)}
          >
            <input
              type="number"
              value={config.max_notional_leverage ?? ''}
              placeholder="20"
              onChange={(e) =>
                updateField(
                  'max_notional_leverage',
                  parseFloat(e.target.value) || 0
                )
              }
              disabled={disabled}
              min={0}
              className="w-full px-3 py-2 rounded font-mono"
              style={{
                background: '#1E2329',
                border: '1px solid #2B3139',
                color: '#EAECEF',
              }}
            />
            <EffectiveLine knob={eff('max_notional_leverage')} />
          </AlwaysOnRow>

          <div className="col-span-2">
            <GuardrailRow
              label={`${ts(riskControl.blackoutStart, language)} / ${ts(riskControl.blackoutEnd, language)}`}
              enabled={config.blackout_enabled ?? false}
              onToggle={(v) => updateField('blackout_enabled', v)}
              disabled={disabled}
            >
              <div className="flex items-center gap-3">
                <input
                  type="text"
                  value={config.blackout_start_ct ?? ''}
                  placeholder="13:25"
                  onChange={(e) =>
                    updateField('blackout_start_ct', e.target.value)
                  }
                  disabled={disabled}
                  className="w-24 px-3 py-2 rounded font-mono"
                  style={{
                    background: '#1E2329',
                    border: '1px solid #2B3139',
                    color: '#EAECEF',
                  }}
                />
                <span style={{ color: '#848E9C' }}>→</span>
                <input
                  type="text"
                  value={config.blackout_end_ct ?? ''}
                  placeholder="13:35"
                  onChange={(e) =>
                    updateField('blackout_end_ct', e.target.value)
                  }
                  disabled={disabled}
                  className="w-24 px-3 py-2 rounded font-mono"
                  style={{
                    background: '#1E2329',
                    border: '1px solid #2B3139',
                    color: '#EAECEF',
                  }}
                />
                <span className="text-xs" style={{ color: '#848E9C' }}>
                  CT (HH:MM)
                </span>
              </div>
              <EffectiveLine knob={eff('blackout_enabled')} label={onOff} />
              <EffectiveLine
                knob={eff('blackout_start_ct')}
                label={ts(riskControl.blackoutStart, language)}
              />
              <EffectiveLine
                knob={eff('blackout_end_ct')}
                label={ts(riskControl.blackoutEnd, language)}
              />
            </GuardrailRow>
          </div>
        </div>

        {/* Futures risk framing — futures path only (the real size knobs are
            max-contracts + the notional cap; crypto keeps its leverage/PVR tiers). */}
        {isFutures && (
          <div
            className="mt-4 p-4 rounded-lg"
            style={{ background: '#0B0E11', border: '1px solid #F0B90B' }}
          >
            <p
              className="text-xs font-medium mb-2"
              style={{ color: '#F0B90B' }}
            >
              {ts(riskControl.futuresRiskTitle, language)}
            </p>
            <div
              className="grid grid-cols-3 gap-3 text-xs"
              style={{ color: '#848E9C' }}
            >
              <div>
                <span style={{ color: '#EAECEF' }}>
                  {ts(riskControl.estContracts, language)}:{' '}
                </span>
                {/* W1 (g): the server's resolved ceiling for the SAVED row
                    (unset → the Stage-A cap), never the old '?? 10' literal;
                    unknown → n/a. */}
                <span data-testid="futures-panel-contracts">
                  {maxContractsText !== null ? `≤ ${maxContractsText}` : 'n/a'}
                </span>
              </div>
              <div>
                <span style={{ color: '#EAECEF' }}>
                  {ts(riskControl.notionalCapField, language)}:{' '}
                </span>
                <span data-testid="futures-panel-notional">
                  {notionalText !== null ? `equity × ${notionalText}` : 'n/a'}
                </span>
              </div>
              <div>
                <span style={{ color: '#EAECEF' }}>
                  {ts(riskControl.marginPerContract, language)}:{' '}
                </span>
                MNQ ≈ $2/pt
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
