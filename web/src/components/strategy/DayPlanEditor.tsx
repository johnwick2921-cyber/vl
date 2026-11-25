// P4.5 — the Strategy Studio "Day Plan" block (futures-visible, per-strategy).
// Same visual grammar as RiskControlEditor: config in, onChange writes the whole
// slice back up, save→hot-reload persists it (no new persistence mechanics).
// Additive + defaults-off: absent day_plan leaves a strategy byte-identical, and
// plan_enabled=false is the master switch. Killzones are shown as ACTIVE windows.

import { useEffect, useState } from 'react'
import type { Language } from '../../i18n/translations'
import { tp } from '../../i18n/plan-translations'
import { pt } from '../../i18n/pep-translations'
import type {
  DayPlanConfig,
  DayPlanSessionOverride,
} from '../../types/strategy'
import {
  SESSION_BANDS,
  londonDSTWarning,
  type SessionName,
} from '../plan/sessionConfig'
import type {
  EffectiveKnob,
  StudioEffective,
} from '../../lib/api/strategyEffective'
import { EffectiveLine, EffectiveSavedNote } from './EffectiveChip'

interface Props {
  config?: DayPlanConfig
  onChange: (config: DayPlanConfig) => void
  disabled?: boolean
  language: Language
  // W1 (g) — the server's effective rows for the SAVED strategy: strategy-level
  // rows from the session-less read, `day_plan.sessions.<leaf>` rows from the
  // read that named that session. Optional: absent → no chips.
  effective?: StudioEffective
  // PEP — the strategy's pep.enabled. The server refuses pep together with the
  // day plan (pep_mode_conflict_day_plan); the selector blocks Pep while the plan
  // is ON, and this is the other direction: with Pep ON the plan cannot be turned
  // ON here (it can always be turned OFF), and a warning says why before Save.
  pepOn?: boolean
}

const DP_PATH = 'day_plan.'
const DP_SESSION_PATH = 'day_plan.sessions.'

// Mirror of Go DefaultDayPlanConfig() — enabling the master switch materializes
// this whole block so a first-time save writes the spec defaults.
const DEFAULT_DAY_PLAN: DayPlanConfig = {
  plan_enabled: false,
  plan_mode: 'advisory',
  planner_timeframes: ['D', '4h', '1h', '15m'],
  proximity_filter_atr: 1.5,
  max_levels: 8,
  htf_seats: 2,
  replan_cap: 2,
  sessions_enabled: ['NY'],
  approval_required: false,
  // W-KNOB-PRUNE (2026-09-18): the folded knobs (scenario_cap 3,
  // acceptance_rule 1×5m, evening_digest off, realign_cap 5,
  // wake_min_interval_min 30, wake_on_level_events ON) are NOT seeded — the
  // engine's constants apply unless a stored value exists; the removed ones
  // (last_entry_ct, eod_flat_ct, seat_1h_zone, htf_score_multiplier) are gone.
  // R4 (2026-08-25) — scenario quality floor DEFAULT C (no restriction).
  min_scenario_quality: 'C',
  // ONE SETUP (dispatch 102, 2026-09-10) — arm only the single best reject
  // (fade) setup. Pointer-bool mirrors Go: absent = ON; grade floor B.
  one_setup_enabled: true,
  one_setup_min_grade: 'B',
  // W-PICTURE-HTF (2026-09-20) — the deterministic two-picture mode.
  // Disabled by default; knobs inherit the Go resolved defaults when blank.
  picture_htf: { enabled: false },
}

// C3 — the legacy day-scoped clock controls (last_entry_ct / eod_flat_ct) were
// HIDDEN on 2026-08-26 and the fields DELETED by W-KNOB-PRUNE (2026-09-18):
// both were unreachable since the P2 session-scope rework.

const ALL_SESSIONS: SessionName[] = ['NY', 'ASIA', 'LONDON']

// The structure-summary timeframes the planner may read (mockup: Daily…5m).
const PLANNER_TFS = ['D', '4h', '1h', '15m', '5m']

// ── primitives ──
function Toggle({
  on,
  onChange,
  disabled,
  testId,
  ariaLabel,
}: {
  on: boolean
  onChange: (v: boolean) => void
  disabled?: boolean
  /** stable hook for tests/Playwright */
  testId?: string
  /** accessible name — a bare role=switch announces as "switch" with no label */
  ariaLabel?: string
}) {
  return (
    <button
      role="switch"
      aria-checked={on}
      data-testid={testId}
      aria-label={ariaLabel}
      disabled={disabled}
      onClick={() => !disabled && onChange(!on)}
      style={{
        width: 40,
        height: 22,
        borderRadius: 999,
        background: on ? 'var(--vl-long)' : 'var(--vl-faint)',
        opacity: disabled ? 0.5 : 1,
        position: 'relative',
        transition: 'background 150ms',
      }}
    >
      <span
        style={{
          position: 'absolute',
          top: 2,
          left: on ? 20 : 2,
          width: 18,
          height: 18,
          borderRadius: 999,
          background: 'var(--vl-ivory)',
          transition: 'left 150ms',
        }}
      />
    </button>
  )
}

function Segmented({
  options,
  value,
  onChange,
  disabled,
  testId,
}: {
  options: { key: string; label: string }[]
  value?: string
  onChange: (v: string) => void
  disabled?: boolean
  /** stable hook for tests (S layering, 2026-08-27) */
  testId?: string
}) {
  return (
    <div
      className="inline-flex"
      data-testid={testId}
      style={{
        border: '1px solid var(--vl-hair)',
        borderRadius: 'var(--vl-radius-chip)',
        overflow: 'hidden',
      }}
    >
      {options.map((o) => {
        const active = value === o.key
        return (
          <button
            key={o.key}
            disabled={disabled}
            onClick={() => !disabled && onChange(o.key)}
            className="text-[10px] font-semibold uppercase tracking-wide px-2 py-1"
            style={{
              background: active ? 'var(--vl-gold-dim)' : 'transparent',
              color: active ? 'var(--vl-gold)' : 'var(--vl-muted)',
              opacity: disabled ? 0.5 : 1,
            }}
          >
            {o.label}
          </button>
        )
      })}
    </div>
  )
}

function NumberField({
  value,
  min,
  max,
  step = 1,
  onChange,
  onClear,
  placeholder,
  disabled,
  testId,
}: {
  value?: number | null
  min: number
  max: number
  step?: number
  onChange: (v: number) => void
  /** W1 — a presence-aware knob: clearing the box calls this (write null =
   *  inherit). Without it a cleared box is ignored, as before. */
  onClear?: () => void
  placeholder?: string
  disabled?: boolean
  testId?: string
}) {
  return (
    <input
      type="number"
      value={value ?? ''}
      min={min}
      max={max}
      step={step}
      disabled={disabled}
      placeholder={placeholder}
      data-testid={testId}
      onChange={(e) => {
        if (e.target.value.trim() === '' && onClear) {
          onClear()
          return
        }
        const n = parseFloat(e.target.value)
        if (!Number.isNaN(n)) onChange(Math.min(max, Math.max(min, n)))
      }}
      className="vl-num text-[12px] w-16 px-1.5 py-0.5"
      style={{
        background: 'var(--vl-card-2)',
        border: '1px solid var(--vl-hair)',
        borderRadius: 'var(--vl-radius-chip)',
        color: 'var(--vl-ivory)',
        opacity: disabled ? 0.5 : 1,
      }}
    />
  )
}

// NumField — an UNCLAMPED, blank-able numeric knob (blank = inherit the Go
// resolved default). Used by the two-picture knob row.
function NumField({
  label,
  value,
  placeholder,
  onChange,
  disabled,
  knob,
}: {
  label: string
  value?: number
  placeholder: string
  onChange: (v: number | undefined) => void
  disabled?: boolean
  /** W1 (g) — the server's effective row; absent → the field renders as before */
  knob?: EffectiveKnob | null
}) {
  const field = (
    <label
      className="flex items-center justify-between gap-3 py-1.5 text-[11px]"
      style={{ color: 'var(--vl-muted)' }}
    >
      <span style={{ fontFamily: 'var(--vl-font-ui)' }}>{label}</span>
      <input
        type="number"
        step="any"
        value={value ?? ''}
        placeholder={placeholder}
        disabled={disabled}
        onChange={(e) => {
          const raw = e.target.value
          onChange(raw === '' ? undefined : Number(raw))
        }}
        className="vl-num text-[12px] w-16 px-1.5 py-0.5 text-right"
        style={{
          background: 'var(--vl-card-2)',
          border: '1px solid var(--vl-hair)',
          borderRadius: 'var(--vl-radius-chip)',
          color: 'var(--vl-ivory)',
          opacity: disabled ? 0.5 : 1,
        }}
      />
    </label>
  )
  if (!knob) return field
  return (
    <div>
      {field}
      <EffectiveLine knob={knob} className="-mt-1 justify-end" />
    </div>
  )
}

function FieldRow({
  label,
  children,
  knob,
}: {
  label: string
  children: React.ReactNode
  /** W1 (g) — the server's effective row, rendered UNDER the row. Absent → the
   *  row renders exactly as before (nothing extra). */
  knob?: EffectiveKnob | null
}) {
  const row = (
    <div className="flex items-center justify-between gap-3 py-1.5">
      <span
        className="text-[11px]"
        style={{ color: 'var(--vl-muted)', fontFamily: 'var(--vl-font-ui)' }}
      >
        {label}
      </span>
      <div className="flex items-center gap-2">{children}</div>
    </div>
  )
  if (!knob) return row
  return (
    <div>
      {row}
      <EffectiveLine knob={knob} className="-mt-1 pb-1 justify-end" />
    </div>
  )
}

const MODE_OPTS = (language: Language) => [
  { key: 'advisory', label: tp('modeAdvisory', language) },
  { key: 'direction', label: tp('modeDirection', language) },
  { key: 'strict', label: tp('modeStrict', language) },
]

// S (2026-08-27) — plan-mode layering honesty, part 1b: a stored per-session
// override EQUAL to the strategy-level value was never a deliberate override —
// migrate it to inherit (drop the field). Pure + idempotent; the component
// re-emits the cleaned config on mount so the next save persists the migration.
// W-T1-CURRENCIES — "usd, eur ," → ["USD","EUR"]; the one parser the field
// and its resync share.
function parseT1Currencies(text: string): string[] {
  return text
    .split(',')
    .map((c) => c.trim().toUpperCase())
    .filter((c) => c.length > 0)
}

function migrateEqualOverrides(config?: DayPlanConfig): DayPlanConfig {
  const base = config ?? DEFAULT_DAY_PLAN
  const list = base.sessions
  if (!list || list.length === 0) return base
  const globalMode = base.plan_mode ?? 'advisory'
  const globalQuality = base.min_scenario_quality ?? 'C'
  let changed = false
  const sessions = list.map((s) => {
    let next = s
    if (s.plan_mode !== undefined && s.plan_mode === globalMode) {
      next = { ...next, plan_mode: undefined }
      changed = true
    }
    if (
      s.min_scenario_quality !== undefined &&
      s.min_scenario_quality === globalQuality
    ) {
      next = { ...next, min_scenario_quality: undefined }
      changed = true
    }
    return next
  })
  return changed ? { ...base, sessions } : base
}

export function DayPlanEditor({
  config,
  onChange,
  disabled,
  language,
  effective,
  pepOn = false,
}: Props) {
  const cfg = migrateEqualOverrides(config ?? DEFAULT_DAY_PLAN)
  // W1 (g) — strategy-level rows come from the session-less read; per-session
  // rows from the read for THAT session. No row → no chip.
  const dpEff = (leaf: string) => effective?.byPath[DP_PATH + leaf]
  const sessEff = (s: SessionName, leaf: string) =>
    effective?.bySession[s]?.[DP_SESSION_PATH + leaf]
  const enabled = cfg.plan_enabled === true
  const [openSession, setOpenSession] = useState<SessionName | null>('NY')

  // S (2026-08-27) — push the equals-global migration upstream once so the
  // parent (and the next save) persists the cleaned config.
  useEffect(() => {
    if (config && migrateEqualOverrides(config) !== config) {
      onChange(migrateEqualOverrides(config))
    }
  }, [])

  // W-T1-CURRENCIES — the comma-separated text the owner is typing; the
  // parsed, upper-cased list is what persists (empty → field absent = USD).
  const [t1Text, setT1Text] = useState((cfg.t1_currencies ?? []).join(','))
  // Review of #171: the editor is not keyed by strategy id, so a strategy
  // switch must resync the text or the previous strategy's currency filter
  // (a no-trade gate) is shown — and could be saved — onto the next one. Only
  // an EXTERNAL change resets it: while the owner types, the parsed list and
  // the saved list agree and a trailing comma survives.
  useEffect(() => {
    const saved = (cfg.t1_currencies ?? []).join(',')
    if (saved !== parseT1Currencies(t1Text).join(',')) setT1Text(saved)
  }, [config])

  const update = <K extends keyof DayPlanConfig>(
    key: K,
    value: DayPlanConfig[K]
  ) => {
    if (disabled) return
    onChange({ ...cfg, [key]: value })
  }

  // per-session override helpers (⚪ inherit = field absent, 🔸 override = set)
  const sessionOf = (s: SessionName): DayPlanSessionOverride | undefined =>
    cfg.sessions?.find((x) => x.session === s)
  const setSessionField = <K extends keyof DayPlanSessionOverride>(
    s: SessionName,
    field: K,
    value: DayPlanSessionOverride[K]
  ) => {
    const list = [...(cfg.sessions ?? [])]
    let i = list.findIndex((x) => x.session === s)
    if (i < 0) {
      list.push({ session: s })
      i = list.length - 1
    }
    list[i] = { ...list[i], [field]: value }
    update('sessions', list)
  }
  const clearSessionField = (
    s: SessionName,
    field: keyof DayPlanSessionOverride
  ) => {
    const list = (cfg.sessions ?? []).map((x) =>
      x.session === s ? { ...x, [field]: undefined } : x
    )
    update('sessions', list)
  }

  // Mirrors trader.sessionRunnable: an explicit per-session enable wins; otherwise
  // inherit the registry default AND the sessions_enabled subset (default [NY]).
  const sessionRunnableUI = (s: SessionName): boolean => {
    const ov = cfg.sessions?.find((x) => x.session === s)
    if (ov?.enable !== undefined) return ov.enable
    const band = SESSION_BANDS.find((b) => b.name === s)
    const subset = cfg.sessions_enabled ?? ['NY']
    return !!band?.enabled && subset.some((x) => x.toUpperCase() === s)
  }

  // planner-reads multiselect: toggle a TF in/out, preserving PLANNER_TFS order.
  const toggleTimeframe = (tf: string) => {
    const cur =
      cfg.planner_timeframes ?? DEFAULT_DAY_PLAN.planner_timeframes ?? []
    const next = cur.includes(tf)
      ? cur.filter((t) => t !== tf)
      : PLANNER_TFS.filter((t) => t === tf || cur.includes(t))
    update('planner_timeframes', next)
  }

  const bodyDisabled = disabled || !enabled

  // S (2026-08-27) — sessions that carry a plan_mode override DIFFERENT from
  // the global row (equals-global already migrated to inherit above).
  const overriddenPlanSessions = (cfg.sessions ?? [])
    .filter((s) => s.plan_mode !== undefined)
    .map((s) => s.session)

  return (
    <div
      className="flex flex-col gap-2"
      style={{ fontFamily: 'var(--vl-font-ui)' }}
    >
      {effective && <EffectiveSavedNote language={language} />}
      {/* master switch */}
      <label className="text-sm p-2">
        {language === 'zh'
          ? '区域外止损缓冲（点；留空使用实测默认值）'
          : language === 'id'
            ? 'Buffer stop di luar zona (poin; kosong memakai hasil pengukuran)'
            : 'Stop buffer beyond zone (points; blank uses measured default)'}
        <input
          aria-label="Structural stop buffer"
          type="number"
          min="0.25"
          step="0.25"
          disabled={disabled}
          value={cfg.structural_stop?.buffer_points ?? ''}
          onChange={(e) =>
            update('structural_stop', {
              ...cfg.structural_stop,
              buffer_points:
                e.target.value === '' ? undefined : Number(e.target.value),
            })
          }
        />
      </label>
      <EffectiveLine
        knob={dpEff('structural_stop.buffer_points')}
        className="-mt-2 px-2"
      />
      <FieldRow
        label={tp('enableDayPlan', language)}
        knob={dpEff('plan_enabled')}
      >
        <Toggle
          on={enabled}
          onChange={(v) => {
            // Never turn the plan ON under Pep (the server refuses the save);
            // turning it OFF is always allowed, so a loaded both-ON state can
            // be repaired from here.
            if (v && pepOn) return
            update('plan_enabled', v)
          }}
          disabled={disabled}
        />
      </FieldRow>
      {pepOn && (
        <div
          role="alert"
          data-testid="dayplan-pep-warning"
          className="text-[11px] px-2"
          style={{ color: 'var(--vl-warn)' }}
        >
          {enabled
            ? pt('dayPlanPepBoth', language)
            : pt('dayPlanPepBlocks', language)}
        </div>
      )}

      <div
        style={{ opacity: bodyDisabled ? 0.55 : 1 }}
        className="flex flex-col gap-2"
      >
        {/* planner model (pinned id shown) */}
        <FieldRow label={tp('plannerModel', language)}>
          <input
            type="text"
            value={cfg.planner_model ?? ''}
            placeholder="inherit primary"
            disabled={bodyDisabled}
            onChange={(e) => update('planner_model', e.target.value)}
            className="vl-num text-[11px] w-40 px-1.5 py-0.5 text-right"
            style={{
              background: 'var(--vl-card-2)',
              border: '1px solid var(--vl-hair)',
              borderRadius: 'var(--vl-radius-chip)',
              color: 'var(--vl-ivory)',
            }}
          />
        </FieldRow>

        {/* plan mode segmented */}
        <FieldRow label={tp('planMode', language)} knob={dpEff('plan_mode')}>
          <Segmented
            options={MODE_OPTS(language)}
            value={cfg.plan_mode ?? 'advisory'}
            onChange={(v) => update('plan_mode', v)}
            disabled={bodyDisabled}
          />
        </FieldRow>
        {/* S (2026-08-27) — the global row states its LIVE effect: when any
            session overrides, the effective mode differs per session. */}
        {overriddenPlanSessions.length > 0 && (
          <div
            data-testid="plan-mode-override-warning"
            className="text-[10px] px-1"
            style={{
              color: 'var(--vl-gold)',
              fontFamily: 'var(--vl-font-ui)',
            }}
          >
            {tp('planModeOverriddenIn', language, {
              mode: (cfg.plan_mode ?? 'advisory').toUpperCase(),
              sessions: overriddenPlanSessions.join(', '),
            })}
          </div>
        )}

        {/* planner reads — an EDITABLE timeframe multiselect (which structure
            TFs the planner summarizes). Applies at the NEXT read, never mid-plan. */}
        <div className="py-1.5">
          <span className="text-[11px]" style={{ color: 'var(--vl-muted)' }}>
            {tp('plannerReads', language)}
          </span>
          <div className="mt-1 flex flex-wrap gap-1">
            {PLANNER_TFS.map((tf) => {
              const on = (
                cfg.planner_timeframes ??
                DEFAULT_DAY_PLAN.planner_timeframes ??
                []
              ).includes(tf)
              return (
                <button
                  key={tf}
                  type="button"
                  role="switch"
                  aria-checked={on}
                  disabled={bodyDisabled}
                  onClick={() => toggleTimeframe(tf)}
                  className="vl-num text-[10px] px-2 py-1"
                  style={{
                    color: on ? 'var(--vl-gold)' : 'var(--vl-faint)',
                    border: `1px solid ${on ? 'var(--vl-gold-line)' : 'var(--vl-hair)'}`,
                    background: on ? 'var(--vl-gold-dim)' : 'transparent',
                    borderRadius: 'var(--vl-radius-chip)',
                    opacity: bodyDisabled ? 0.5 : 1,
                    cursor: bodyDisabled ? 'not-allowed' : 'pointer',
                  }}
                >
                  {tf}
                </button>
              )
            })}
          </div>
          <EffectiveLine knob={dpEff('planner_timeframes')} />
        </div>

        {/* one-line regime (read-only, AUTO — auto-computed, not a setting) */}
        <FieldRow label={tp('regime', language)}>
          <span
            className="text-[10px] uppercase cursor-help"
            style={{ color: 'var(--vl-faint)' }}
            title={tp('autoTooltip', language)}
          >
            {tp('autoLabel', language)}
          </span>
        </FieldRow>

        {/* filters */}
        <div
          className="mt-1 pt-2"
          style={{ borderTop: '1px solid var(--vl-hair)' }}
        >
          <span
            className="text-[10px] uppercase tracking-widest"
            style={{ color: 'var(--vl-faint)' }}
          >
            {tp('filters', language)}
          </span>
          <FieldRow
            label={tp('proximity', language)}
            knob={dpEff('proximity_filter_atr')}
          >
            <input
              type="range"
              min={0.1}
              max={3.0}
              step={0.1}
              value={cfg.proximity_filter_atr ?? 1.5}
              disabled={bodyDisabled}
              onChange={(e) =>
                update('proximity_filter_atr', parseFloat(e.target.value))
              }
              style={{ width: 90 }}
            />
            <span
              className="vl-num text-[11px]"
              style={{ color: 'var(--vl-muted)' }}
            >
              {(cfg.proximity_filter_atr ?? 1.5).toFixed(1)}×
            </span>
          </FieldRow>
          <FieldRow
            label={tp('maxLevels', language)}
            knob={dpEff('max_levels')}
          >
            <NumberField
              value={cfg.max_levels ?? 8}
              min={3}
              max={12}
              onChange={(v) => update('max_levels', v)}
              disabled={bodyDisabled}
            />
          </FieldRow>
          <FieldRow label={tp('htfSeats', language)} knob={dpEff('htf_seats')}>
            <NumberField
              value={cfg.htf_seats ?? 2}
              min={0}
              max={6}
              onChange={(v) => update('htf_seats', v)}
              disabled={bodyDisabled}
            />
          </FieldRow>
          {/* W-KNOB-PRUNE (2026-09-18) — removed controls: Max scenarios
              (folded, 3 unless stored), HTF score multiplier (constant 1.0),
              Structure map (folded, advisory, stored value honoured), Freshness
              by TF (folded, stored value honoured), Acceptance rule (one rule),
              Digest (folded), Re-align cap (folded into re-plans), the five
              wake toggles (one switch below), Min wake interval (30 unless
              stored), 1h seat guarantee (unconditional). Stored values pass
              through untouched on save. */}
          <FieldRow
            label={tp('flipReread', language)}
            knob={dpEff('flip_reread')}
          >
            <Toggle
              on={cfg.flip_reread === true}
              onChange={(v) => update('flip_reread', v)}
              disabled={bodyDisabled}
              testId="flip-reread-toggle"
            />
          </FieldRow>
          {/* W-DEATH-REREAD (2026-09-18) — default ON: the toggle reads ON
              unless the strategy saved an explicit false. */}
          <FieldRow
            label={tp('deathReread', language)}
            knob={dpEff('death_reread')}
          >
            <Toggle
              on={cfg.death_reread !== false}
              onChange={(v) => update('death_reread', v)}
              disabled={bodyDisabled}
              testId="death-reread-toggle"
            />
          </FieldRow>
          <FieldRow
            label={tp('t1Currencies', language)}
            knob={dpEff('t1_currencies')}
          >
            <input
              type="text"
              data-testid="t1-currencies-input"
              value={t1Text}
              placeholder="USD"
              disabled={bodyDisabled}
              onChange={(e) => {
                setT1Text(e.target.value)
                const list = parseT1Currencies(e.target.value)
                update('t1_currencies', list.length > 0 ? list : undefined)
              }}
              className="vl-num text-[11px] w-40 px-1.5 py-0.5 text-right"
              style={{
                background: 'var(--vl-card-2)',
                border: '1px solid var(--vl-hair)',
                borderRadius: 'var(--vl-radius-chip)',
                color: 'var(--vl-ivory)',
              }}
            />
          </FieldRow>
          {/* W1 (settings truth) — presence-aware: absent/null = the shipped
              default 2 (shown as an EMPTY box with an inherit placeholder, not
              a fake 2); 0 = no re-plan and is stored; clearing writes null. */}
          <FieldRow
            label={tp('maxReplans', language)}
            knob={dpEff('replan_cap')}
          >
            <NumberField
              value={cfg.replan_cap}
              min={0}
              max={4}
              onChange={(v) => update('replan_cap', v)}
              onClear={() => update('replan_cap', null)}
              placeholder={`${tp('inherit', language)} (2)`}
              disabled={bodyDisabled}
              testId="replan-cap-strategy"
            />
          </FieldRow>
          <FieldRow
            label={tp('approval', language)}
            knob={dpEff('approval_required')}
          >
            <Toggle
              on={cfg.approval_required === true}
              onChange={(v) => update('approval_required', v)}
              disabled={bodyDisabled}
            />
          </FieldRow>

          {/* Release #25 (STRICT-12345 F-2/F-3/F-5/F-7) — the loss-fix
              switches. Every one ships OFF (absent = off, COMMON-25 case b);
              the owner turns them on here. None is seeded in DEFAULT_DAY_PLAN. */}
          <div
            className="mt-1 pt-2"
            style={{ borderTop: '1px solid var(--vl-hair)' }}
            data-testid="loss-fix-switches"
          >
            <span
              className="text-[10px] uppercase tracking-widest"
              style={{ color: 'var(--vl-faint)' }}
            >
              {tp('lossFixHeader', language)}
            </span>
            <FieldRow
              label={tp('plannerRoomGate', language)}
              knob={dpEff('planner_room_gate')}
            >
              <Segmented
                testId="planner-room-gate"
                options={[
                  { key: 'off', label: tp('roomGateOff', language) },
                  { key: 'refuse', label: tp('roomGateRefuse', language) },
                  {
                    key: 'tp1_at_obstacle',
                    label: tp('roomGateTp1', language),
                  },
                ]}
                value={cfg.planner_room_gate || 'off'}
                onChange={(v) => update('planner_room_gate', v)}
                disabled={bodyDisabled}
              />
            </FieldRow>
            <FieldRow
              label={tp('plannerRoomMinR', language)}
              knob={dpEff('planner_room_min_r')}
            >
              <NumberField
                testId="planner-room-min-r"
                value={cfg.planner_room_min_r}
                min={0.1}
                max={10}
                step={0.1}
                placeholder="1.0"
                onChange={(v) => update('planner_room_min_r', v)}
                onClear={() => update('planner_room_min_r', undefined)}
                disabled={
                  bodyDisabled ||
                  !cfg.planner_room_gate ||
                  cfg.planner_room_gate === 'off'
                }
              />
            </FieldRow>
            <FieldRow
              label={tp('plannerExitOnThesisDeath', language)}
              knob={dpEff('planner_exit_on_thesis_death')}
            >
              <Segmented
                testId="planner-exit-on-thesis-death"
                options={[
                  { key: 'off', label: tp('thesisOff', language) },
                  {
                    key: 'invalidation_only',
                    label: tp('thesisInvalidationOnly', language),
                  },
                  { key: 'all', label: tp('thesisAll', language) },
                ]}
                value={cfg.planner_exit_on_thesis_death || 'off'}
                onChange={(v) => update('planner_exit_on_thesis_death', v)}
                disabled={bodyDisabled}
              />
            </FieldRow>
            <FieldRow
              label={tp('plannerNoChaseEnforce', language)}
              knob={dpEff('planner_no_chase_enforce')}
            >
              <Toggle
                testId="planner-no-chase-enforce-toggle"
                on={cfg.planner_no_chase_enforce === true}
                onChange={(v) => update('planner_no_chase_enforce', v)}
                disabled={bodyDisabled}
              />
            </FieldRow>
            <FieldRow
              label={tp('plannerMomentumIntoZone', language)}
              knob={dpEff('planner_momentum_into_zone_max_atr')}
            >
              <NumberField
                testId="planner-momentum-into-zone"
                value={cfg.planner_momentum_into_zone_max_atr}
                min={0}
                max={10}
                step={0.1}
                placeholder="0"
                onChange={(v) =>
                  update('planner_momentum_into_zone_max_atr', v)
                }
                onClear={() =>
                  update('planner_momentum_into_zone_max_atr', undefined)
                }
                disabled={bodyDisabled}
              />
            </FieldRow>
            <FieldRow
              label={tp('plannerAtrFloorCap', language)}
              knob={dpEff('planner_atr_floor_cap_at_invalidation')}
            >
              <Toggle
                testId="planner-atr-floor-cap-toggle"
                on={cfg.planner_atr_floor_cap_at_invalidation === true}
                onChange={(v) =>
                  update('planner_atr_floor_cap_at_invalidation', v)
                }
                disabled={bodyDisabled}
              />
            </FieldRow>
            <FieldRow
              label={tp('plannerStaleZonesMaxAtr', language)}
              knob={dpEff('planner_stale_zones_max_atr')}
            >
              <NumberField
                testId="planner-stale-zones-max-atr"
                value={cfg.planner_stale_zones_max_atr}
                min={0}
                max={50}
                step={0.5}
                placeholder="0"
                onChange={(v) => update('planner_stale_zones_max_atr', v)}
                onClear={() => update('planner_stale_zones_max_atr', undefined)}
                disabled={bodyDisabled}
              />
            </FieldRow>
          </div>

          {/* W6 (2026-08-25) — planner wake-up: level-event wakes. W-KNOB-PRUNE
              (2026-09-18): ONE switch (absent = ON, mirrors Go pointer-bool)
              replaces the five per-class toggles. */}
          <div
            className="mt-1 pt-2"
            style={{ borderTop: '1px solid var(--vl-hair)' }}
          >
            <span
              className="text-[10px] uppercase tracking-widest"
              style={{ color: 'var(--vl-faint)' }}
            >
              {tp('wakeHeader', language)}
            </span>
            <FieldRow
              label={tp('wakeOnLevelEvents', language)}
              knob={dpEff('wake_on_level_events')}
            >
              <Toggle
                on={cfg.wake_on_level_events !== false}
                onChange={(v) => update('wake_on_level_events', v)}
                disabled={bodyDisabled}
                testId="wake-on-level-events-toggle"
              />
            </FieldRow>
            <FieldRow
              label={tp('minScenarioQuality', language)}
              knob={dpEff('min_scenario_quality')}
            >
              <Segmented
                options={[
                  { key: 'A', label: 'A' },
                  { key: 'B', label: 'B' },
                  { key: 'C', label: 'C' },
                ]}
                value={cfg.min_scenario_quality ?? 'C'}
                onChange={(v) => update('min_scenario_quality', v)}
                disabled={bodyDisabled}
              />
            </FieldRow>
            {/* ONE SETUP (dispatch 102) — arm only the single best reject (fade)
                setup. Pointer-bool mirrors Go: absent = ON; grade floor B. */}
            <FieldRow
              label={tp('oneSetup', language)}
              knob={dpEff('one_setup_enabled')}
            >
              <Toggle
                testId="one-setup-toggle"
                on={cfg.one_setup_enabled !== false}
                onChange={(v) => update('one_setup_enabled', v)}
                disabled={bodyDisabled}
              />
            </FieldRow>
            <FieldRow
              label={tp('oneSetupMinGrade', language)}
              knob={dpEff('one_setup_min_grade')}
            >
              <Segmented
                testId="one-setup-min-grade"
                options={[
                  { key: 'A', label: 'A' },
                  { key: 'B', label: 'B' },
                  { key: 'C', label: 'C' },
                ]}
                value={cfg.one_setup_min_grade ?? 'B'}
                onChange={(v) => update('one_setup_min_grade', v)}
                disabled={bodyDisabled}
              />
            </FieldRow>
            {/* W-PICTURE-HTF (2026-09-20) — the two-picture mode. The toggle
                materializes the knob row; blanks inherit the Go resolved
                defaults (min_rr inherits the risk-control floor). */}
            <div
              className="mt-1 pt-2"
              style={{ borderTop: '1px solid var(--vl-hair)' }}
            >
              <span
                className="text-[10px] uppercase tracking-widest"
                style={{ color: 'var(--vl-faint)' }}
              >
                {tp('pictureHtf', language)}
              </span>
              <FieldRow
                label={tp('pictureHtfEnable', language)}
                knob={dpEff('picture_htf.enabled')}
              >
                <Toggle
                  testId="picture-htf-toggle"
                  on={cfg.picture_htf?.enabled === true}
                  onChange={(v) =>
                    update('picture_htf', {
                      ...cfg.picture_htf,
                      enabled: v,
                    })
                  }
                  disabled={bodyDisabled}
                />
              </FieldRow>
              {/* W-EXEC-TRUTH W5 — the switch is a SOURCE selector: a Picture
                  setup is recorded as a Day Plan scenario and trades only
                  through the plan's armed executor. */}
              <p
                data-testid="picture-htf-source-hint"
                className="text-[10px] mb-1"
                style={{
                  color: 'var(--vl-faint)',
                  fontFamily: 'var(--vl-font-ui)',
                }}
              >
                {tp('pictureHtfSourceHint', language)}
              </p>
              {cfg.picture_htf?.enabled === true && (
                <div className="flex flex-col gap-2 ml-1">
                  <NumField
                    label={tp('pictureTickSize', language)}
                    knob={dpEff('picture_htf.tick_size')}
                    value={cfg.picture_htf?.tick_size}
                    placeholder="0.25"
                    onChange={(v) =>
                      update('picture_htf', {
                        ...cfg.picture_htf,
                        tick_size: v,
                      })
                    }
                    disabled={bodyDisabled}
                  />
                  <NumField
                    label={tp('picturePivotWindow', language)}
                    knob={dpEff('picture_htf.pivot_window')}
                    value={cfg.picture_htf?.pivot_window}
                    placeholder="120"
                    onChange={(v) =>
                      update('picture_htf', {
                        ...cfg.picture_htf,
                        pivot_window: v,
                      })
                    }
                    disabled={bodyDisabled}
                  />
                  <NumField
                    label={tp('pictureSwingLookback', language)}
                    knob={dpEff('picture_htf.swing_lookback')}
                    value={cfg.picture_htf?.swing_lookback}
                    placeholder="24"
                    onChange={(v) =>
                      update('picture_htf', {
                        ...cfg.picture_htf,
                        swing_lookback: v,
                      })
                    }
                    disabled={bodyDisabled}
                  />
                  <NumField
                    label={tp('pictureEntryWindowSec', language)}
                    knob={dpEff('picture_htf.entry_window_sec')}
                    value={cfg.picture_htf?.entry_window_sec}
                    placeholder="360"
                    onChange={(v) =>
                      update('picture_htf', {
                        ...cfg.picture_htf,
                        entry_window_sec: v,
                      })
                    }
                    disabled={bodyDisabled}
                  />
                  <NumField
                    label={tp('pictureFreshnessSec', language)}
                    knob={dpEff('picture_htf.freshness_sec')}
                    value={cfg.picture_htf?.freshness_sec}
                    placeholder="30"
                    onChange={(v) =>
                      update('picture_htf', {
                        ...cfg.picture_htf,
                        freshness_sec: v,
                      })
                    }
                    disabled={bodyDisabled}
                  />
                  <NumField
                    label={tp('pictureMinRR', language)}
                    knob={dpEff('picture_htf.min_rr')}
                    value={cfg.picture_htf?.min_rr}
                    placeholder="inherit"
                    onChange={(v) =>
                      update('picture_htf', { ...cfg.picture_htf, min_rr: v })
                    }
                    disabled={bodyDisabled}
                  />
                </div>
              )}
            </div>
          </div>
        </div>

        {/* sessions accordion */}
        <div
          className="mt-1 pt-2"
          style={{ borderTop: '1px solid var(--vl-hair)' }}
        >
          <span
            className="text-[10px] uppercase tracking-widest"
            style={{ color: 'var(--vl-faint)' }}
          >
            {tp('sessionsHeader', language)}
          </span>
          {ALL_SESSIONS.map((s) => {
            const ov = sessionOf(s)
            const band = SESSION_BANDS.find((b) => b.name === s)
            const isOpen = openSession === s
            const dst = s === 'LONDON' && londonDSTWarning()
            return (
              <div
                key={s}
                className="mt-1"
                style={{
                  border: '1px solid var(--vl-hair)',
                  borderRadius: 'var(--vl-radius-inner)',
                }}
              >
                <div className="w-full flex items-center gap-2 px-2.5 py-1.5">
                  {/* PART A — the per-session ENABLE toggle. Explicit ON/OFF is
                      authoritative for this strategy (🔸override); clearing it
                      returns the row to ⚪inherit (registry + sessions_enabled).
                      ASIA/LONDON inherit OFF, so they stay off until switched on. */}
                  <Toggle
                    testId={`session-enable-${s}`}
                    ariaLabel={`${s} session enabled`}
                    on={sessionRunnableUI(s)}
                    onChange={(on) => setSessionField(s, 'enable', on)}
                    disabled={bodyDisabled}
                  />
                  <button
                    onClick={() => setOpenSession(isOpen ? null : s)}
                    className="flex-1 flex items-center justify-between"
                    aria-expanded={isOpen}
                    disabled={bodyDisabled}
                  >
                    <span
                      className="text-[11px] font-semibold uppercase tracking-wide"
                      style={{ color: 'var(--vl-ivory)' }}
                    >
                      {tp(
                        s === 'ASIA'
                          ? 'sessionAsia'
                          : s === 'LONDON'
                            ? 'sessionLondon'
                            : 'sessionNY',
                        language
                      )}
                    </span>
                    <span className="flex items-center gap-2">
                      {ov?.enable !== undefined && (
                        <span
                          data-testid={`session-enable-chip-${s}`}
                          className="text-[9px]"
                          style={{ color: 'var(--vl-gold)' }}
                          title="explicit override — clear to inherit"
                        >
                          🔸
                        </span>
                      )}
                      <span
                        className="text-[10px]"
                        style={{ color: 'var(--vl-faint)' }}
                      >
                        {isOpen ? '▾' : '▸'}
                      </span>
                    </span>
                  </button>
                </div>
                <EffectiveLine
                  knob={sessEff(s, 'enable')}
                  className="-mt-1 px-2.5 pb-1"
                />
                {isOpen && (
                  <div className="px-2.5 pb-2 flex flex-col gap-1">
                    {/* ACTIVE windows (killzones, spec wording) */}
                    <div
                      className="text-[10px]"
                      style={{ color: 'var(--vl-faint)' }}
                    >
                      {band?.killzones.map((kz) => (
                        <span key={kz.name} className="vl-num mr-2">
                          {String(Math.floor(kz.startMin / 60)).padStart(
                            2,
                            '0'
                          )}
                          :{String(kz.startMin % 60).padStart(2, '0')}–
                          {String(Math.floor(kz.endMin / 60)).padStart(2, '0')}:
                          {String(kz.endMin % 60).padStart(2, '0')}
                        </span>
                      ))}
                      <span
                        className="uppercase"
                        style={{ color: 'var(--vl-gold)' }}
                      >
                        {tp('windows', language)}
                      </span>
                    </div>
                    {dst && (
                      <div
                        className="text-[10px]"
                        style={{ color: 'var(--vl-short)' }}
                      >
                        ⚠ {tp('dstWarning', language)}
                      </div>
                    )}
                    {/* S (2026-08-27) — tri-state rows: inherit (default,
                        stores NOTHING) / explicit values. The old ⚪/🔸 toggle
                        was honest but the equals-global case made it look like
                        a deliberate override when it wasn't. */}
                    <TriStateRow
                      label={tp('minGrade', language)}
                      overridden={ov?.min_grade !== undefined}
                      language={language}
                      knob={sessEff(s, 'min_grade')}
                    >
                      <Segmented
                        testId={`session-min-grade-${s}`}
                        options={[
                          { key: 'inherit', label: tp('inherit', language) },
                          { key: 'A', label: 'A' },
                          { key: 'B', label: 'B' },
                          { key: 'C', label: 'C' },
                        ]}
                        value={ov?.min_grade ?? 'inherit'}
                        onChange={(v) =>
                          v === 'inherit'
                            ? clearSessionField(s, 'min_grade')
                            : setSessionField(s, 'min_grade', v)
                        }
                        disabled={bodyDisabled}
                      />
                    </TriStateRow>
                    <TriStateRow
                      label={tp('minScenarioQuality', language)}
                      overridden={ov?.min_scenario_quality !== undefined}
                      language={language}
                      knob={sessEff(s, 'min_scenario_quality')}
                    >
                      <Segmented
                        testId={`session-quality-${s}`}
                        options={[
                          { key: 'inherit', label: tp('inherit', language) },
                          { key: 'A', label: 'A' },
                          { key: 'B', label: 'B' },
                          { key: 'C', label: 'C' },
                        ]}
                        value={ov?.min_scenario_quality ?? 'inherit'}
                        onChange={(v) =>
                          v === 'inherit'
                            ? clearSessionField(s, 'min_scenario_quality')
                            : setSessionField(s, 'min_scenario_quality', v)
                        }
                        disabled={bodyDisabled}
                      />
                    </TriStateRow>
                    <TriStateRow
                      label={tp('maxTrades', language)}
                      overridden={ov?.max_trades !== undefined}
                      language={language}
                      knob={sessEff(s, 'max_trades')}
                    >
                      <div className="flex items-center gap-1">
                        <Segmented
                          testId={`session-max-trades-${s}`}
                          options={[
                            { key: 'inherit', label: tp('inherit', language) },
                            {
                              key: 'custom',
                              label: tp('customValue', language),
                            },
                          ]}
                          value={
                            ov?.max_trades === undefined ? 'inherit' : 'custom'
                          }
                          onChange={(v) =>
                            v === 'inherit'
                              ? clearSessionField(s, 'max_trades')
                              : setSessionField(s, 'max_trades', 3)
                          }
                          disabled={bodyDisabled}
                        />
                        {ov?.max_trades !== undefined && (
                          <NumberField
                            value={ov.max_trades}
                            min={0}
                            max={20}
                            onChange={(v) =>
                              setSessionField(s, 'max_trades', v)
                            }
                            disabled={bodyDisabled}
                          />
                        )}
                      </div>
                    </TriStateRow>
                    <TriStateRow
                      label={tp('planMode', language)}
                      overridden={ov?.plan_mode !== undefined}
                      language={language}
                      knob={sessEff(s, 'plan_mode')}
                    >
                      <Segmented
                        testId={`session-plan-mode-${s}`}
                        options={[
                          { key: 'inherit', label: tp('inherit', language) },
                          ...MODE_OPTS(language),
                        ]}
                        value={ov?.plan_mode ?? 'inherit'}
                        onChange={(v) =>
                          v === 'inherit'
                            ? clearSessionField(s, 'plan_mode')
                            : setSessionField(s, 'plan_mode', v)
                        }
                        disabled={bodyDisabled}
                      />
                    </TriStateRow>
                    <OverrideRow
                      label={tp('maxReplans', language)}
                      overridden={ov?.replan_cap !== undefined}
                      onToggle={(on) =>
                        on
                          ? // W1 — seed the EFFECTIVE strategy value (the
                            // override starts equal to what it inherits),
                            // not a literal 2.
                            setSessionField(
                              s,
                              'replan_cap',
                              cfg.replan_cap ?? 2
                            )
                          : clearSessionField(s, 'replan_cap')
                      }
                      disabled={bodyDisabled}
                      language={language}
                      knob={sessEff(s, 'replan_cap')}
                    >
                      <NumberField
                        value={ov?.replan_cap}
                        min={0}
                        max={4}
                        onChange={(v) => setSessionField(s, 'replan_cap', v)}
                        disabled={bodyDisabled}
                      />
                    </OverrideRow>
                    {/* acceptance_rule override row removed (W-KNOB-PRUNE):
                        one rule exists; a stored override is read by nothing. */}
                  </div>
                )}
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}

// S (2026-08-27) — tri-state override row: the control is ALWAYS visible and
// its first option is "inherit" (stores nothing). ⚪/🔸 chip reports the state.
function TriStateRow({
  label,
  overridden,
  language,
  children,
  knob,
}: {
  label: string
  overridden: boolean
  language: Language
  children: React.ReactNode
  /** W1 (g) — the per-session effective row; absent → nothing extra */
  knob?: EffectiveKnob | null
}) {
  const row = (
    <div className="flex items-center justify-between gap-2 py-1">
      <span
        className="inline-flex items-center gap-1.5 text-[11px]"
        style={{ color: overridden ? 'var(--vl-gold)' : 'var(--vl-faint)' }}
        title={overridden ? tp('override', language) : tp('inherit', language)}
      >
        <span aria-hidden>{overridden ? '🔸' : '⚪'}</span>
        <span>{label}</span>
      </span>
      <div className="flex items-center gap-1">{children}</div>
    </div>
  )
  if (!knob) return row
  return (
    <div>
      {row}
      <EffectiveLine knob={knob} className="-mt-0.5 pb-1 justify-end" />
    </div>
  )
}

// ⚪ inherit / 🔸 override chip + (when overridden) the inline control.
function OverrideRow({
  label,
  overridden,
  onToggle,
  disabled,
  language,
  children,
  knob,
}: {
  label: string
  overridden: boolean
  onToggle: (on: boolean) => void
  disabled?: boolean
  language: Language
  children: React.ReactNode
  /** W1 (g) — the per-session effective row, shown whether or not the row is
   *  overridden (an inheriting row still has an effective value). */
  knob?: EffectiveKnob | null
}) {
  const row = (
    <div className="flex items-center justify-between gap-2 py-1">
      <button
        onClick={() => !disabled && onToggle(!overridden)}
        disabled={disabled}
        className="inline-flex items-center gap-1.5 text-[11px]"
        style={{ color: overridden ? 'var(--vl-gold)' : 'var(--vl-faint)' }}
        title={overridden ? tp('override', language) : tp('inherit', language)}
      >
        <span aria-hidden>{overridden ? '🔸' : '⚪'}</span>
        <span>{label}</span>
      </button>
      {overridden && <div className="flex items-center gap-1">{children}</div>}
    </div>
  )
  if (!knob) return row
  return (
    <div>
      {row}
      <EffectiveLine knob={knob} className="-mt-0.5 pb-1 justify-end" />
    </div>
  )
}
