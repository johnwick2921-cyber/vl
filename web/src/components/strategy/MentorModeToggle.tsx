import { useState } from 'react'

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
    zh: '无窗口（任何时间）',
    en: 'No window (any hour)',
    es: 'Sin ventana (cualquier hora)',
  },
  windowMinutes: { zh: '分钟', en: 'min', es: 'min' },
  windowBadStart: {
    zh: '请按 HH:MM 输入时间（00:00–23:59）。',
    en: 'Enter the time as HH:MM (00:00–23:59).',
    es: 'Introduce la hora como HH:MM (00:00–23:59).',
  },
  windowPreviewAny: {
    zh: '导师在任何时间交易',
    en: 'Mentor trades at any hour',
    es: 'El mentor opera a cualquier hora',
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
} satisfies Record<string, Copy>

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
  // Stop-after-loss knob (mentor_stop_after_loss); absent = OFF.
  stopAfterLoss?: boolean
  onStopAfterLossChange: (on: boolean) => void
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
  stopAfterLoss,
  onStopAfterLossChange,
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
    </div>
  )
}
