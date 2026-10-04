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
} satisfies Record<string, Copy>

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
}

export function MentorModeToggle({
  on,
  onChange,
  disabled,
  language,
  strategyName,
  orderGate,
}: MentorModeToggleProps) {
  const [confirming, setConfirming] = useState(false)
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
