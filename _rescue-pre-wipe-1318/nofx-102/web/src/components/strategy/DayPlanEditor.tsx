// P4.5 — the Strategy Studio "Day Plan" block (futures-visible, per-strategy).
// Same visual grammar as RiskControlEditor: config in, onChange writes the whole
// slice back up, save→hot-reload persists it (no new persistence mechanics).
// Additive + defaults-off: absent day_plan leaves a strategy byte-identical, and
// plan_enabled=false is the master switch. Killzones are shown as ACTIVE windows.

import { useEffect, useState } from 'react'
import type { Language } from '../../i18n/translations'
import { tp } from '../../i18n/plan-translations'
import type {
  DayPlanConfig,
  DayPlanSessionOverride,
} from '../../types/strategy'
import {
  SESSION_BANDS,
  londonDSTWarning,
  type SessionName,
} from '../plan/sessionConfig'

interface Props {
  config?: DayPlanConfig
  onChange: (config: DayPlanConfig) => void
  disabled?: boolean
  language: Language
}

// Mirror of Go DefaultDayPlanConfig() — enabling the master switch materializes
// this whole block so a first-time save writes the spec defaults.
const DEFAULT_DAY_PLAN: DayPlanConfig = {
  plan_enabled: false,
  plan_mode: 'advisory',
  planner_timeframes: ['D', '4h', '1h', '15m'],
  proximity_filter_atr: 1.5,
  max_levels: 8,
  scenario_cap: 3,
  htf_seats: 2,
  htf_score_multiplier: 1.2,
  acceptance_rule: '5m_close',
  replan_cap: 2,
  sessions_enabled: ['NY'],
  approval_required: false,
  evening_digest: true,
  last_entry_ct: '13:00',
  eod_flat_ct: '14:45',
  realign_cap: 5,
  // W6 (2026-08-25) — wake knobs: ON except HTF OBs; 10-min spacing.
  wake_on_15m_zone: true,
  wake_on_htf_zone: true,
  wake_on_htf_ob: false,
  wake_on_seated_invalidation: true,
  wake_on_ifvg: true,
  wake_min_interval_min: 30,
  // 1h wave (2026-08-25) — 1h S/D seat guarantee DEFAULT ON.
  seat_1h_zone: true,
  // R4 (2026-08-25) — scenario quality floor DEFAULT C (no restriction).
  min_scenario_quality: 'C',
  // ONE SETUP (dispatch 102, 2026-09-10) — arm only the single best live
  // setup. Pointer-bool mirrors Go: absent = ON; grade floor B.
  one_setup_enabled: true,
  one_setup_min_grade: 'B',
}

// C3 — the legacy day-scoped clock controls (last_entry_ct / eod_flat_ct) were
// HIDDEN on 2026-08-26: both are unreachable since the P2 session-scope rework
// (trader/auto_trader_clock.go: "nothing evaluates it"). Stored values survive.

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
  disabled,
  testId,
}: {
  value?: number
  min: number
  max: number
  step?: number
  onChange: (v: number) => void
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
      data-testid={testId}
      onChange={(e) => {
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

function FieldRow({
  label,
  children,
}: {
  label: string
  children: React.ReactNode
}) {
  return (
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

export function DayPlanEditor({ config, onChange, disabled, language }: Props) {
  const cfg = migrateEqualOverrides(config ?? DEFAULT_DAY_PLAN)
  const enabled = cfg.plan_enabled === true
  const [openSession, setOpenSession] = useState<SessionName | null>('NY')

  // S (2026-08-27) — push the equals-global migration upstream once so the
  // parent (and the next save) persists the cleaned config.
  useEffect(() => {
    if (config && migrateEqualOverrides(config) !== config) {
      onChange(migrateEqualOverrides(config))
    }
  }, [])

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
      <FieldRow label={tp('enableDayPlan', language)}>
        <Toggle
          on={enabled}
          onChange={(v) => update('plan_enabled', v)}
          disabled={disabled}
        />
      </FieldRow>

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
        <FieldRow label={tp('planMode', language)}>
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
          <FieldRow label={tp('proximity', language)}>
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
          <FieldRow label={tp('maxLevels', language)}>
            <NumberField
              value={cfg.max_levels ?? 8}
              min={3}
              max={12}
              onChange={(v) => update('max_levels', v)}
              disabled={bodyDisabled}
            />
          </FieldRow>
          <FieldRow label={tp('maxScenarios', language)}>
            <NumberField
              value={cfg.scenario_cap ?? 3}
              min={1}
              max={5}
              onChange={(v) => update('scenario_cap', v)}
              disabled={bodyDisabled}
            />
          </FieldRow>
          <FieldRow label={tp('htfSeats', language)}>
            <NumberField
              value={cfg.htf_seats ?? 2}
              min={0}
              max={6}
              onChange={(v) => update('htf_seats', v)}
              disabled={bodyDisabled}
            />
          </FieldRow>
          <FieldRow label={tp('htfScoreMultiplier', language)}>
            <NumberField
              value={cfg.htf_score_multiplier ?? 1.2}
              min={1.0}
              max={1.5}
              step={0.1}
              onChange={(v) => update('htf_score_multiplier', v)}
              disabled={bodyDisabled}
            />
          </FieldRow>
          <FieldRow label={tp('structureMap', language)}>
            <Toggle
              on={cfg.structure_map === true}
              onChange={(v) => update('structure_map', v)}
              disabled={bodyDisabled}
              testId="structure-map-toggle"
            />
          </FieldRow>
          <FieldRow label={tp('freshByTf', language)}>
            <Toggle
              on={cfg.levels_fresh_by_tf === true}
              onChange={(v) => update('levels_fresh_by_tf', v)}
              disabled={bodyDisabled}
              testId="fresh-by-tf-toggle"
            />
          </FieldRow>
          <FieldRow label={tp('flipReread', language)}>
            <Toggle
              on={cfg.flip_reread === true}
              onChange={(v) => update('flip_reread', v)}
              disabled={bodyDisabled}
              testId="flip-reread-toggle"
            />
          </FieldRow>
          <FieldRow label={tp('maxReplans', language)}>
            <NumberField
              value={cfg.replan_cap ?? 2}
              min={0}
              max={4}
              onChange={(v) => update('replan_cap', v)}
              disabled={bodyDisabled}
            />
          </FieldRow>
          <FieldRow label={tp('acceptance', language)}>
            <Segmented
              options={[{ key: '5m_close', label: '1×5m' }]}
              value={cfg.acceptance_rule ?? '5m_close'}
              onChange={(v) => update('acceptance_rule', v)}
              disabled={bodyDisabled}
            />
          </FieldRow>
          <FieldRow label={tp('approval', language)}>
            <Toggle
              on={cfg.approval_required === true}
              onChange={(v) => update('approval_required', v)}
              disabled={bodyDisabled}
            />
          </FieldRow>
          <FieldRow label={tp('digest', language)}>
            <Toggle
              on={cfg.evening_digest !== false}
              onChange={(v) => update('evening_digest', v)}
              disabled={bodyDisabled}
            />
          </FieldRow>

          {/* C3 — last_entry_ct / eod_flat_ct are LEGACY day-scoped clock
              fields, UNREACHABLE since the P2 session-scope rework
              (trader/auto_trader_clock.go:257-317: "nothing evaluates it").
              The live gates read the session registry instead. Hidden, not
              removed, so old stored values survive untouched. */}
          <FieldRow label={tp('realignCap', language)}>
            <NumberField
              value={cfg.realign_cap ?? 5}
              min={0}
              max={10}
              onChange={(v) => update('realign_cap', v)}
              disabled={bodyDisabled}
            />
          </FieldRow>

          {/* W6 (2026-08-25) — planner wake-up knobs: level-event wakes.
              Absent = ON (mirrors Go pointer-bool defaults); HTF OBs OFF. */}
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
            <FieldRow label={tp('wakeOn15mZone', language)}>
              <Toggle
                on={cfg.wake_on_15m_zone !== false}
                onChange={(v) => update('wake_on_15m_zone', v)}
                disabled={bodyDisabled}
              />
            </FieldRow>
            <FieldRow label={tp('wakeOnHTFZone', language)}>
              <Toggle
                on={cfg.wake_on_htf_zone !== false}
                onChange={(v) => update('wake_on_htf_zone', v)}
                disabled={bodyDisabled}
              />
            </FieldRow>
            <FieldRow label={tp('wakeOnHTFOB', language)}>
              <Toggle
                on={cfg.wake_on_htf_ob === true}
                onChange={(v) => update('wake_on_htf_ob', v)}
                disabled={bodyDisabled}
              />
            </FieldRow>
            <FieldRow label={tp('wakeOnSeatedInvalidation', language)}>
              <Toggle
                on={cfg.wake_on_seated_invalidation !== false}
                onChange={(v) => update('wake_on_seated_invalidation', v)}
                disabled={bodyDisabled}
              />
            </FieldRow>
            <FieldRow label={tp('wakeOnIFVG', language)}>
              <Toggle
                on={cfg.wake_on_ifvg !== false}
                onChange={(v) => update('wake_on_ifvg', v)}
                disabled={bodyDisabled}
              />
            </FieldRow>
            <FieldRow label={tp('wakeMinInterval', language)}>
              <NumberField
                value={cfg.wake_min_interval_min ?? 30}
                min={5}
                max={120}
                onChange={(v) => update('wake_min_interval_min', v)}
                disabled={bodyDisabled}
              />
            </FieldRow>
            <FieldRow label={tp('seat1HZone', language)}>
              <Toggle
                on={cfg.seat_1h_zone !== false}
                onChange={(v) => update('seat_1h_zone', v)}
                disabled={bodyDisabled}
              />
            </FieldRow>
            <FieldRow label={tp('minScenarioQuality', language)}>
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
            {/* ONE SETUP (dispatch 102) — arm only the single best live setup.
                Pointer-bool mirrors Go: absent = ON; grade floor B. */}
            <FieldRow label={tp('oneSetup', language)}>
              <Toggle
                testId="one-setup-toggle"
                on={cfg.one_setup_enabled !== false}
                onChange={(v) => update('one_setup_enabled', v)}
                disabled={bodyDisabled}
              />
            </FieldRow>
            <FieldRow label={tp('oneSetupMinGrade', language)}>
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
                          ? setSessionField(s, 'replan_cap', 2)
                          : clearSessionField(s, 'replan_cap')
                      }
                      disabled={bodyDisabled}
                      language={language}
                    >
                      <NumberField
                        value={ov?.replan_cap}
                        min={0}
                        max={4}
                        onChange={(v) => setSessionField(s, 'replan_cap', v)}
                        disabled={bodyDisabled}
                      />
                    </OverrideRow>
                    <OverrideRow
                      label={tp('acceptance', language)}
                      overridden={ov?.acceptance_rule !== undefined}
                      onToggle={(on) =>
                        on
                          ? setSessionField(s, 'acceptance_rule', '5m_close')
                          : clearSessionField(s, 'acceptance_rule')
                      }
                      disabled={bodyDisabled}
                      language={language}
                    >
                      <Segmented
                        options={[{ key: '5m_close', label: '1×5m' }]}
                        value={ov?.acceptance_rule}
                        onChange={(v) =>
                          setSessionField(s, 'acceptance_rule', v)
                        }
                        disabled={bodyDisabled}
                      />
                    </OverrideRow>
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
}: {
  label: string
  overridden: boolean
  language: Language
  children: React.ReactNode
}) {
  return (
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
}

// ⚪ inherit / 🔸 override chip + (when overridden) the inline control.
function OverrideRow({
  label,
  overridden,
  onToggle,
  disabled,
  language,
  children,
}: {
  label: string
  overridden: boolean
  onToggle: (on: boolean) => void
  disabled?: boolean
  language: Language
  children: React.ReactNode
}) {
  return (
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
}
