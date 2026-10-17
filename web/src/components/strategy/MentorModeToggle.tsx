import { useState } from 'react'

import { EffectiveLine } from './EffectiveChip'
import type { EffectiveKnob } from '../../lib/api/strategyEffective'

// Studio control for the per-strategy `mentor_mode` flag (risk_control.mentor_mode,
// default OFF). It only edits the strategy config; the Studio's normal Save
// persists it and reloads the running trader(s) bound to the strategy (api/strategy.go
// handleUpdateStrategy), so no restart is needed. Order placement has its own
// server-side gate (env MENTOR_PLACE=1) which this component only DISPLAYS.

type Copy = { zh: string; en: string; es: string }

const COPY = {
  title: {
    zh: '🧑‍🏫 导师模式',
    en: '🧑‍🏫 Mentor mode',
    es: '🧑‍🏫 Modo mentor',
  },
  desc: {
    zh: '开启后，入场信号来自导师评估器，AI 不再下入场单。默认关闭。保存策略后生效（运行中的交易员会自动重载，无需重启）。',
    en: 'When ON, entries come from the mentor evaluator and the AI places no entries. Default OFF. Takes effect when you save the strategy (the running trader reloads on save — no restart needed). SIM only.',
    es: 'Con ON, las entradas vienen del evaluador mentor y la IA no abre entradas. Por defecto OFF. Se aplica al guardar la estrategia (el trader en ejecución se recarga, sin reiniciar). Solo SIM.',
  },
  gateDry: {
    zh: '下单：模拟演练（MENTOR_PLACE 未开启）',
    en: 'Orders: DRY RUN (MENTOR_PLACE off)',
    es: 'Órdenes: SIMULACRO (MENTOR_PLACE apagado)',
  },
  gateOn: {
    zh: '下单：SIM 订单已开启（MENTOR_PLACE=1）',
    en: 'Orders: SIM orders ON (MENTOR_PLACE=1)',
    es: 'Órdenes: órdenes SIM ACTIVAS (MENTOR_PLACE=1)',
  },
  gateUnknown: {
    zh: '下单：状态不可用',
    en: 'Orders: gate status unavailable',
    es: 'Órdenes: estado no disponible',
  },
  confirmTitle: {
    zh: '为策略开启导师模式？',
    en: 'Turn Mentor mode ON for this strategy?',
    es: '¿Activar el modo mentor en esta estrategia?',
  },
  confirmStrategy: { zh: '策略', en: 'Strategy', es: 'Estrategia' },
  confirmOk: { zh: '开启', en: 'Turn ON', es: 'Activar' },
  confirmCancel: { zh: '取消', en: 'Cancel', es: 'Cancelar' },
  windowStart: {
    zh: '交易窗口开始（CT）',
    en: 'Trading window start (CT)',
    es: 'Inicio de la ventana (CT)',
  },
  windowLength: {
    zh: '窗口长度',
    en: 'Window length',
    es: 'Duración de la ventana',
  },
  windowNone: {
    zh: '无窗口（收盘规则仍适用）',
    en: 'No window (EOD rules still apply)',
    es: 'Sin ventana (rigen las reglas de cierre)',
  },
  windowMinutes: { zh: '分钟', en: 'min', es: 'min' },
  windowBadStart: {
    zh: '请按 HH:MM 输入时间（00:00–23:59）。',
    en: 'Enter the time as HH:MM (00:00–23:59).',
    es: 'Introduce la hora como HH:MM (00:00–23:59).',
  },
  windowPreviewAny: {
    zh: '无时间窗口 — 15:45 CT 起不再新开入场，14:45 CT 收盘平仓仍然适用',
    en: 'No time window — the 15:45 CT entry cutoff and the 14:45 CT EOD flat still apply',
    es: 'Sin ventana horaria — siguen vigentes el corte de entradas de las 15:45 CT y el cierre EOD de las 14:45 CT',
  },
  windowPreview: {
    zh: '导师交易时段',
    en: 'Mentor trades',
    es: 'El mentor opera',
  },
  windowNote: {
    zh: '窗口之外不开新入场；SWING 不受窗口限制。与开关一样，保存后生效。',
    en: 'Outside the window no new entries; the SWING setup ignores the window. Saved and applied like the switch above.',
    es: 'Fuera de la ventana no hay entradas nuevas; el SWING ignora la ventana. Se guarda y aplica como el interruptor.',
  },
  stopAfterWin: {
    zh: '盈利后当日停止',
    en: 'Stop for the day after a winning trade',
    es: 'Detenerse el día tras una operación ganadora',
  },
  stopAfterWinHelp: {
    zh: '默认关闭。开启后：导师交易以盈利平仓且当日净盈利，则在下一个纽约开盘（08:30 CT）前不再新开导师入场；已挂的入场单会被撤销 [D1.2 p1 @20:46]。',
    en: 'OFF by default. ON: after a mentor trade closes in profit and the day is net positive, no new mentor entries until the next NY open (08:30 CT); resting entry orders are cancelled [D1.2 p1 @20:46].',
    es: 'OFF por defecto. ON: tras una operación mentor cerrada con ganancia y el día en positivo neto, no hay nuevas entradas mentor hasta la próxima apertura de NY (08:30 CT); las órdenes de entrada en reposo se cancelan [D1.2 p1 @20:46].',
  },
  stopAfterLoss: {
    zh: '亏损后当日停止',
    en: 'Stop for the day after a losing trade',
    es: 'Detenerse el día tras una operación perdedora',
  },
  stopAfterLossHelp: {
    zh: '默认关闭。开启后：导师交易今日以净亏损平仓，则在 17:00 CT 前不再新开导师入场；已挂的入场单会被撤销 [D1.2 @23:34]。',
    en: 'OFF by default. ON: after a mentor trade closes today with a net loss, no new mentor entries until 17:00 CT; resting entry orders are cancelled [D1.2 @23:34].',
    es: 'OFF por defecto. ON: tras una operación mentor cerrada hoy con pérdida neta, no hay nuevas entradas mentor hasta las 17:00 CT; las órdenes de entrada en reposo se cancelan [D1.2 @23:34].',
  },
  trailMode: {
    zh: '剩余仓位出场（移动止损）',
    en: 'Runner exit (trail mode)',
    es: 'Salida del runner (modo trailing)',
  },
  trailModeCandle: {
    zh: 'candle — +0.5R 时保本（在第 1 腿 1:1 止盈成交前），随后跟随每根已收盘 K 线（默认）',
    en: 'candle — break-even at +0.5R (before leg 1’s 1:1 fills), then each closed candle (default)',
    es: 'candle — break-even a +0.5R (antes del 1:1 de la pierna 1), luego cada vela cerrada (por defecto)',
  },
  trailModeOff: {
    zh: 'off — 旧规则（到目标一半距离时保本）',
    en: 'off — the older rule (break-even at half the distance to the target)',
    es: 'off — la regla anterior (break-even a mitad de distancia al objetivo)',
  },
  trailModeHelp: {
    zh: '默认 candle（course-defaults-30，owner "fix all"）：当某根已收盘 K 线收盘价 ≥ +0.5R 时，两条腿的止损先移到开仓价（mentor_be_before_partial 默认开启，先于第 1 腿 1:1 止盈成交 —— 反转了 owner 裁定 R3）；之后在 NinjaTrader 确认第 1 腿 1:1 止盈成交后，剩余仓位按入场所用周期的每根已收盘 K 线（通常 1 分钟；exec_2m_after_30m 开启时在 2 分钟图上入场的 ISB 为 2 分钟）移到其低点下方 1 tick（多）/高点上方 1 tick（空），只收紧不放宽。',
    en: 'Default candle (course-defaults-30, owner "fix all"): on the first closed candle whose close is ≥ +0.5R, BOTH legs’ stops move to break-even FIRST (mentor_be_before_partial ON by default, before leg 1’s 1:1 partial — this reverses owner ruling R3); then, once NinjaTrader CONFIRMS leg 1’s 1:1 take-profit filled, the runner trails 1 tick beyond each closed candle of the frame the entry was read on (the 1m; the 2m for an ISB entered on the 2m while exec_2m_after_30m is ON) — only ever tighter.',
    es: 'Por defecto candle (course-defaults-30, owner "fix all"): en la primera vela cerrada cuyo cierre es ≥ +0.5R, el stop de AMBAS piernas pasa primero a break-even (mentor_be_before_partial ON por defecto, antes del parcial 1:1 de la pierna 1 — esto revierte la decisión R3); luego, cuando NinjaTrader CONFIRMA el take-profit 1:1 de la pierna 1, el runner se arrastra 1 tick más allá de cada vela cerrada del marco en que se leyó la entrada (1m; 2m para un ISB que entró en 2m con exec_2m_after_30m activado) — solo se ajusta, nunca se amplía.',
  },
  readOnlyKnobs: {
    zh: '生效中的导师旋钮（只读）',
    en: 'Effective mentor knobs (read-only)',
    es: 'Knobs mentor efectivos (solo lectura)',
  },
  trailTF: {
    zh: '移动止损周期',
    en: 'Trail timeframe',
    es: 'Marco del trailing',
  },
  baseContracts: {
    zh: '基础口数',
    en: 'Base contracts',
    es: 'Contratos base',
  },
  confContracts: {
    zh: '汇聚口数',
    en: 'Confluence contracts',
    es: 'Contratos confluencia',
  },
  bigContracts: {
    zh: '大单口数',
    en: 'Big contracts',
    es: 'Contratos grandes',
  },
  reducedContracts: {
    zh: '缩减口数',
    en: 'Reduced contracts',
    es: 'Contratos reducidos',
  },
  spentContracts: {
    zh: 'spent 日口数',
    en: 'Spent-day contracts',
    es: 'Contratos día agotado',
  },
  maxContracts: {
    zh: '口数上限',
    en: 'Max contracts',
    es: 'Contratos máximos',
  },
  staleBlock: {
    zh: '数据过期拦截',
    en: 'Stale-data block',
    es: 'Bloqueo por datos obsoletos',
  },
  dayOffRecheck: {
    zh: 'DayOff 复核',
    en: 'Day-off recheck',
    es: 'Revisión día libre',
  },
  legBudget: {
    zh: '腿预算',
    en: 'Leg budget',
    es: 'Presupuesto de pierna',
  },
  legReset: {
    zh: '腿重置',
    en: 'Leg reset',
    es: 'Reinicio de pierna',
  },
  lvlRevisit: {
    zh: '价位重访距离',
    en: 'Level revisit distance',
    es: 'Distancia de revisita',
  },
  emaCross: {
    zh: 'EMA34 30m 交叉上限',
    en: 'EMA 34 cross cap (30m)',
    es: 'Tope de cruces EMA 34 (30m)',
  },
  locTrigger: {
    zh: '价位 5m 触发过滤',
    en: 'Location trigger filter',
    es: 'Filtro de gatillo por ubicación',
  },
  lossDeparture: {
    zh: '亏损离场距离',
    en: 'Loss departure distance',
    es: 'Distancia de salida en pérdida',
  },
} satisfies Record<string, Copy>

// Stored runner-exit values (trader/mentor_trail_r22.go mentorTrailMode): an
// absent value is the shipped default, candle.
export const MENTOR_TRAIL_MODE_DEFAULT = 'candle'
const TRAIL_MODES = ['candle', 'off'] as const

// mentorTrailModeShown mirrors the server resolver: absent → candle, and any
// value outside candle/off is shown as candle — the server fails closed to it.
export function mentorTrailModeShown(stored?: string): string {
  const v = (stored || '').trim().toLowerCase()
  return (TRAIL_MODES as readonly string[]).includes(v)
    ? v
    : MENTOR_TRAIL_MODE_DEFAULT
}

// Stored defaults (trader/mentor_tick.go mentorWindowDefaultStart/Minutes). An
// unset or 0 length resolves to 60; the stored value -1 means "no window".
export const MENTOR_WINDOW_DEFAULT_START = '08:30'
export const MENTOR_WINDOW_DEFAULT_MINUTES = 60
export const MENTOR_WINDOW_NONE = -1
const WINDOW_LENGTHS = [30, 60, 90, 120]
const HHMM = /^([01]\d|2[0-3]):[0-5]\d$/

export function mentorWindowPreview(
  start: string,
  minutes: number,
  language: string
): string {
  if (minutes < 0) return tr(COPY.windowPreviewAny, language)
  const [h, m] = start.split(':').map(Number)
  const total = (h * 60 + m + minutes) % (24 * 60)
  const pad = (n: number) => String(n).padStart(2, '0')
  const end = `${pad(Math.floor(total / 60))}:${pad(total % 60)}`
  return `${tr(COPY.windowPreview, language)} ${start}–${end} CT`
}

function tr(entry: Copy, language: string): string {
  return (entry as unknown as Record<string, string>)[language] ?? entry.en
}

interface MentorModeToggleProps {
  on: boolean
  onChange: (on: boolean) => void
  disabled?: boolean
  language: string
  strategyName?: string
  // The server's MENTOR_PLACE gate; undefined = unknown (older server / failed read).
  orderGate?: boolean
  // Stored window knobs (mentor_window_start / mentor_window_minutes); absent = default.
  windowStart?: string
  windowMinutes?: number
  onWindowStartChange: (start: string) => void
  onWindowMinutesChange: (minutes: number) => void
  // Stop-after-win knob (mentor_done_after_win); absent = OFF.
  stopAfterWin?: boolean
  onStopAfterWinChange: (on: boolean) => void
  // Stop-after-loss knob (mentor_stop_after_loss); absent = OFF.
  stopAfterLoss?: boolean
  onStopAfterLossChange: (on: boolean) => void
  // Runner exit knob (mentor_trail_mode); absent = candle.
  trailMode?: string
  onTrailModeChange?: (mode: string) => void
  // The server's effective rows (GET /api/strategies/:id/effective). Optional:
  // absent → no chips. Leaves are risk_control.<leaf> (the RC_PATH prefix is
  // applied by the caller).
  eff?: (leaf: string) => EffectiveKnob | null | undefined
}

export function MentorModeToggle({
  on,
  onChange,
  disabled,
  language,
  strategyName,
  orderGate,
  windowStart,
  windowMinutes,
  onWindowStartChange,
  onWindowMinutesChange,
  stopAfterWin,
  onStopAfterWinChange,
  stopAfterLoss,
  onStopAfterLossChange,
  trailMode,
  onTrailModeChange,
  eff,
}: MentorModeToggleProps) {
  const [confirming, setConfirming] = useState(false)
  const [startDraft, setStartDraft] = useState<string | null>(null)
  const shownStart = (windowStart || '').trim() || MENTOR_WINDOW_DEFAULT_START
  const shownMinutes = windowMinutes
    ? windowMinutes
    : MENTOR_WINDOW_DEFAULT_MINUTES
  const startInvalid = startDraft !== null && !HHMM.test(startDraft)
  const lengths =
    WINDOW_LENGTHS.includes(shownMinutes) || shownMinutes < 0
      ? WINDOW_LENGTHS
      : [...WINDOW_LENGTHS, shownMinutes].sort((a, b) => a - b)
  const gateText =
    orderGate === undefined
      ? tr(COPY.gateUnknown, language)
      : orderGate
        ? tr(COPY.gateOn, language)
        : tr(COPY.gateDry, language)

  const click = () => {
    if (disabled) return
    if (on) {
      onChange(false) // turning OFF is immediate
      return
    }
    setConfirming(true)
  }

  return (
    <div
      className="p-4 rounded-lg"
      style={{ background: '#0B0E11', border: '1px solid #2B3139' }}
      data-testid="mentor-mode-block"
    >
      <div className="flex items-center justify-between gap-2">
        <label className="text-sm font-medium" style={{ color: '#EAECEF' }}>
          {tr(COPY.title, language)}
        </label>
        <button
          type="button"
          role="switch"
          aria-checked={on}
          aria-label={tr(COPY.title, language)}
          disabled={disabled}
          data-testid="mentor-mode-toggle"
          onClick={click}
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
      </div>
      <p className="text-xs mt-2" style={{ color: '#848E9C' }}>
        {tr(COPY.desc, language)}
      </p>
      <p
        className="text-xs mt-2 font-mono"
        style={{ color: orderGate ? '#F0B90B' : '#848E9C' }}
        data-testid="mentor-order-gate"
      >
        {gateText}
      </p>
      <EffectiveLine knob={eff?.('mentor_mode')} />
      <div
        className="mt-3 pt-3 flex flex-col gap-2"
        style={{ borderTop: '1px solid #2B3139' }}
      >
        <div className="flex flex-wrap items-center gap-3">
          <label className="text-xs" style={{ color: '#EAECEF' }}>
            {tr(COPY.windowStart, language)}
            <input
              type="time"
              data-testid="mentor-window-start"
              disabled={disabled}
              value={startDraft ?? shownStart}
              onChange={(e) => {
                if (disabled) return
                const v = e.target.value
                setStartDraft(v)
                if (HHMM.test(v)) {
                  setStartDraft(null)
                  onWindowStartChange(v)
                }
              }}
              onBlur={() => setStartDraft(null)}
              className="ml-2 px-2 py-1 rounded"
              style={{
                background: '#1E2329',
                border: '1px solid #2B3139',
                color: '#EAECEF',
              }}
            />
          </label>
          <label className="text-xs" style={{ color: '#EAECEF' }}>
            {tr(COPY.windowLength, language)}
            <select
              data-testid="mentor-window-minutes"
              disabled={disabled}
              value={shownMinutes < 0 ? MENTOR_WINDOW_NONE : shownMinutes}
              onChange={(e) =>
                !disabled && onWindowMinutesChange(Number(e.target.value))
              }
              className="ml-2 px-2 py-1 rounded"
              style={{
                background: '#1E2329',
                border: '1px solid #2B3139',
                color: '#EAECEF',
              }}
            >
              {lengths.map((n) => (
                <option key={n} value={n}>
                  {n} {tr(COPY.windowMinutes, language)}
                </option>
              ))}
              <option value={MENTOR_WINDOW_NONE}>
                {tr(COPY.windowNone, language)}
              </option>
            </select>
          </label>
        </div>
        {startInvalid && (
          <p
            className="text-xs"
            style={{ color: '#F6465D' }}
            role="alert"
            data-testid="mentor-window-start-error"
          >
            {tr(COPY.windowBadStart, language)}
          </p>
        )}
        <p
          className="text-xs font-mono"
          style={{ color: '#EAECEF' }}
          data-testid="mentor-window-preview"
        >
          {mentorWindowPreview(shownStart, shownMinutes, language)}
        </p>
        <p className="text-xs" style={{ color: '#848E9C' }}>
          {tr(COPY.windowNote, language)}
        </p>
        <EffectiveLine knob={eff?.('mentor_window_start')} label="start" />
        <EffectiveLine knob={eff?.('mentor_window_minutes')} label="minutes" />
      </div>
      <div
        className="mt-3 pt-3 flex flex-col gap-2"
        style={{ borderTop: '1px solid #2B3139' }}
      >
        <div className="flex items-center justify-between gap-2">
          <label className="text-xs" style={{ color: '#EAECEF' }}>
            {tr(COPY.stopAfterWin, language)}
          </label>
          <button
            type="button"
            role="switch"
            aria-checked={stopAfterWin === true}
            aria-label={tr(COPY.stopAfterWin, language)}
            disabled={disabled}
            data-testid="mentor-stop-after-win"
            onClick={() => {
              if (disabled) return
              onStopAfterWinChange(!(stopAfterWin === true))
            }}
            className="relative inline-block w-9 h-5 rounded-full transition-colors shrink-0"
            style={{
              background: stopAfterWin === true ? '#0ECB81' : '#2B3139',
              opacity: disabled ? 0.5 : 1,
            }}
          >
            <span
              className="absolute top-0.5 w-4 h-4 rounded-full bg-white transition-all"
              style={{ left: stopAfterWin === true ? '18px' : '2px' }}
            />
          </button>
        </div>
        <p className="text-xs" style={{ color: '#848E9C' }}>
          {tr(COPY.stopAfterWinHelp, language)}
        </p>
      </div>
      <div
        className="mt-3 pt-3 flex flex-col gap-2"
        style={{ borderTop: '1px solid #2B3139' }}
      >
        <div className="flex items-center justify-between gap-2">
          <label className="text-xs" style={{ color: '#EAECEF' }}>
            {tr(COPY.stopAfterLoss, language)}
          </label>
          <button
            type="button"
            role="switch"
            aria-checked={stopAfterLoss === true}
            aria-label={tr(COPY.stopAfterLoss, language)}
            disabled={disabled}
            data-testid="mentor-stop-after-loss"
            onClick={() => {
              if (disabled) return
              onStopAfterLossChange(!(stopAfterLoss === true))
            }}
            className="relative inline-block w-9 h-5 rounded-full transition-colors shrink-0"
            style={{
              background: stopAfterLoss === true ? '#0ECB81' : '#2B3139',
              opacity: disabled ? 0.5 : 1,
            }}
          >
            <span
              className="absolute top-0.5 w-4 h-4 rounded-full bg-white transition-all"
              style={{ left: stopAfterLoss === true ? '18px' : '2px' }}
            />
          </button>
        </div>
        <p className="text-xs" style={{ color: '#848E9C' }}>
          {tr(COPY.stopAfterLossHelp, language)}
        </p>
        <EffectiveLine knob={eff?.('mentor_stop_after_loss')} />
      </div>
      <div
        className="mt-3 pt-3 flex flex-col gap-2"
        style={{ borderTop: '1px solid #2B3139' }}
      >
        <label className="text-xs" style={{ color: '#EAECEF' }}>
          {tr(COPY.trailMode, language)}
          <select
            data-testid="mentor-trail-mode"
            disabled={disabled || !onTrailModeChange}
            value={mentorTrailModeShown(trailMode)}
            onChange={(e) => !disabled && onTrailModeChange?.(e.target.value)}
            className="ml-2 px-2 py-1 rounded"
            style={{
              background: '#1E2329',
              border: '1px solid #2B3139',
              color: '#EAECEF',
            }}
          >
            {TRAIL_MODES.map((m) => (
              <option key={m} value={m}>
                {tr(
                  m === 'off' ? COPY.trailModeOff : COPY.trailModeCandle,
                  language
                )}
              </option>
            ))}
          </select>
        </label>
        <p className="text-xs" style={{ color: '#848E9C' }}>
          {tr(COPY.trailModeHelp, language)}
        </p>
        <EffectiveLine knob={eff?.('mentor_trail_mode')} />
      </div>
      {confirming && (
        <div
          role="alertdialog"
          aria-label={tr(COPY.confirmTitle, language)}
          data-testid="mentor-confirm"
          className="mt-3 p-3 rounded"
          style={{ background: '#1E2329', border: '1px solid #F0B90B' }}
        >
          <p className="text-sm font-medium" style={{ color: '#EAECEF' }}>
            {tr(COPY.confirmTitle, language)}
          </p>
          <p className="text-xs mt-1" style={{ color: '#EAECEF' }}>
            {tr(COPY.confirmStrategy, language)}: {strategyName || '—'}
          </p>
          <p className="text-xs mt-1" style={{ color: '#EAECEF' }}>
            {gateText}
          </p>
          <div className="flex gap-2 mt-3">
            <button
              type="button"
              data-testid="mentor-confirm-ok"
              className="px-3 py-1 rounded text-xs font-semibold"
              style={{ background: '#0ECB81', color: '#0B0E11' }}
              onClick={() => {
                setConfirming(false)
                onChange(true)
              }}
            >
              {tr(COPY.confirmOk, language)}
            </button>
            <button
              type="button"
              data-testid="mentor-confirm-cancel"
              className="px-3 py-1 rounded text-xs"
              style={{ background: '#2B3139', color: '#EAECEF' }}
              onClick={() => setConfirming(false)}
            >
              {tr(COPY.confirmCancel, language)}
            </button>
          </div>
        </div>
      )}
      {eff && (
        <div
          className="mt-3 pt-3 flex flex-col gap-1"
          style={{ borderTop: '1px solid #2B3139' }}
          data-testid="mentor-readonly-knobs"
        >
          <div className="text-xs font-medium" style={{ color: '#EAECEF' }}>
            {tr(COPY.readOnlyKnobs, language)}
          </div>
          <ReadOnlyRow
            leaf="mentor_trail_tf"
            label={tr(COPY.trailTF, language)}
            eff={eff}
          />
          <ReadOnlyRow
            leaf="mentor_base_contracts"
            label={tr(COPY.baseContracts, language)}
            eff={eff}
          />
          <ReadOnlyRow
            leaf="mentor_confluence_contracts"
            label={tr(COPY.confContracts, language)}
            eff={eff}
          />
          <ReadOnlyRow
            leaf="mentor_big_contracts"
            label={tr(COPY.bigContracts, language)}
            eff={eff}
          />
          <ReadOnlyRow
            leaf="mentor_reduced_contracts"
            label={tr(COPY.reducedContracts, language)}
            eff={eff}
          />
          <ReadOnlyRow
            leaf="mentor_spent_day_contracts"
            label={tr(COPY.spentContracts, language)}
            eff={eff}
          />
          <ReadOnlyRow
            leaf="mentor_max_contracts"
            label={tr(COPY.maxContracts, language)}
            eff={eff}
          />
          <ReadOnlyRow
            leaf="mentor_stale_data_block"
            label={tr(COPY.staleBlock, language)}
            eff={eff}
          />
          <ReadOnlyRow
            leaf="mentor_day_off_recheck"
            label={tr(COPY.dayOffRecheck, language)}
            eff={eff}
          />
          <ReadOnlyRow
            leaf="mentor_leg_budget_enabled"
            label={tr(COPY.legBudget, language)}
            eff={eff}
          />
          <ReadOnlyRow
            leaf="mentor_leg_reset_on"
            label={tr(COPY.legReset, language)}
            eff={eff}
          />
          <ReadOnlyRow
            leaf="mentor_lvl_revisit_min_pts"
            label={tr(COPY.lvlRevisit, language)}
            eff={eff}
          />
          <ReadOnlyRow
            leaf="mentor_ema_max_cross_30m"
            label={tr(COPY.emaCross, language)}
            eff={eff}
          />
          <ReadOnlyRow
            leaf="mentor_loc_trigger_filter"
            label={tr(COPY.locTrigger, language)}
            eff={eff}
          />
          <ReadOnlyRow
            leaf="mentor_loss_departure_pts"
            label={tr(COPY.lossDeparture, language)}
            eff={eff}
          />
        </div>
      )}
    </div>
  )
}

function ReadOnlyRow({
  leaf,
  label,
  eff,
}: {
  leaf: string
  label: string
  eff: (leaf: string) => EffectiveKnob | null | undefined
}) {
  return (
    <div className="flex flex-wrap items-baseline gap-2 text-[11px]">
      <span style={{ color: '#848E9C' }}>{label}</span>
      <EffectiveLine knob={eff(leaf)} className="mt-0" />
    </div>
  )
}
