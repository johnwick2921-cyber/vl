import type { MentorTuning } from '../../types'

// Studio controls for risk_control.mentor_tuning — the mentor-method numbers
// the owner may tune. A blank field = "use the ruled default" (the key is
// removed from the stored config); the server fails closed to the default on an
// out-of-range value. Like the mentor switch, a change applies when the
// strategy is saved (the running trader reloads on save).

type Copy = { zh: string; en: string; es: string }

type NumKey =
  | 'ping_pong_min_gap_pts'
  | 'ping_pong_candle_max_pts'
  | 'ping_pong_candle_lookback'
  | 'level_max_visits'
  | 'day_gate_spent_pts'
  | 'day_gate_target_cap_pts'
  | 'swing_max_stop_pts'

type BoolKey =
  | 'orb_gate_enabled'
  | 'isb_reverse_ema9_enabled'
  | 'htf_gate_news_only'

const COPY = {
  title: {
    zh: '🧑‍🏫 导师方法参数',
    en: '🧑‍🏫 Mentor method numbers',
    es: '🧑‍🏫 Números del método mentor',
  },
  desc: {
    zh: '留空 = 使用已裁定的默认值。保存策略后生效。超出范围的值会退回默认值。',
    en: 'Blank = the ruled default. Applied when you save the strategy. An out-of-range value falls back to the default.',
    es: 'Vacío = el valor por defecto dictado. Se aplica al guardar la estrategia. Un valor fuera de rango vuelve al valor por defecto.',
  },
  school: {
    zh: '触发流派',
    en: 'Trigger school',
    es: 'Escuela de disparo',
  },
  school1: {
    zh: '1 — 触及价位即入场（导师本人，默认）',
    en: '1 — enter at the level (his own school, default)',
    es: '1 — entrar en el nivel (su escuela, por defecto)',
  },
  school2: {
    zh: '2 — 等待 5 分钟触发',
    en: '2 — wait for the 5m trigger',
    es: '2 — esperar el disparo de 5m',
  },
  deflt: { zh: '默认', en: 'default', es: 'por defecto' },
  on: { zh: '开', en: 'ON', es: 'ON' },
  off: { zh: '关', en: 'OFF', es: 'OFF' },
  orb: {
    zh: 'ORB 入场闸门',
    en: 'ORB entry gate',
    es: 'Compuerta ORB',
  },
  isbRev: {
    zh: 'EMA 9 反向 ISB',
    en: 'Reverse ISB at EMA 9',
    es: 'ISB inverso en EMA 9',
  },
  htfNews: {
    zh: '4h/1h 方向闸门仅限新闻窗口',
    en: '4h/1h direction gate only in the news window',
    es: 'Compuerta 4h/1h solo en la ventana de noticias',
  },
  pingGap: {
    zh: '箱体乒乓最小宽度（点）',
    en: 'Ping-pong minimum gap (pts)',
    es: 'Ancho mínimo ping-pong (pts)',
  },
  pingCandle: {
    zh: '乒乓最大 K 线（点）',
    en: 'Ping-pong candle maximum (pts)',
    es: 'Vela máxima ping-pong (pts)',
  },
  pingLookback: {
    zh: '乒乓 K 线回看根数',
    en: 'Ping-pong candle look-back (bars)',
    es: 'Velas de revisión ping-pong',
  },
  visits: {
    zh: '每日价位最多次数（0 = 不限）',
    en: 'Maximum visits per level per day (0 = no cap)',
    es: 'Visitas máximas por nivel y día (0 = sin tope)',
  },
  spent: {
    zh: '耗尽日阈值（点）',
    en: 'Spent-day threshold (pts)',
    es: 'Umbral de día agotado (pts)',
  },
  cap: {
    zh: '耗尽日目标上限 / 止损上限（点）',
    en: 'Spent-day target cap / stop cap (pts)',
    es: 'Tope de objetivo / stop en día agotado (pts)',
  },
  swing: {
    zh: 'SWING4H 最大止损（点，≥ 此值则跳过）',
    en: 'SWING4H max stop (pts, skipped at or above)',
    es: 'Stop máximo SWING4H (pts, se omite en o sobre)',
  },
} satisfies Record<string, Copy>

function tr(entry: Copy, language: string): string {
  return (entry as unknown as Record<string, string>)[language] ?? entry.en
}

// Ruled defaults, shown as placeholders (kernel/mentor DefaultConfig — OWNER
// RULING 2026-10-04: spent 300 / cap 15, swing max stop 100, ISB reverse ON).
export const MENTOR_TUNING_DEFAULTS = {
  trigger_school: 1,
  ping_pong_min_gap_pts: 50,
  ping_pong_candle_max_pts: 20,
  ping_pong_candle_lookback: 30,
  level_max_visits: 3,
  orb_gate_enabled: true,
  isb_reverse_ema9_enabled: true,
  htf_gate_news_only: false,
  day_gate_spent_pts: 300,
  day_gate_target_cap_pts: 15,
  swing_max_stop_pts: 100,
} as const

// Remove a key when the value is undefined; keep an empty block out of the
// stored config entirely.
export function withTuning(
  cur: MentorTuning | undefined,
  patch: Partial<MentorTuning>
): MentorTuning | undefined {
  const next: Record<string, unknown> = { ...(cur ?? {}), ...patch }
  for (const k of Object.keys(next)) {
    if (next[k] === undefined) delete next[k]
  }
  return Object.keys(next).length ? (next as MentorTuning) : undefined
}

interface Props {
  tuning?: MentorTuning
  onChange: (next: MentorTuning | undefined) => void
  disabled?: boolean
  language: string
}

const box = {
  background: '#1E2329',
  border: '1px solid #2B3139',
  color: '#EAECEF',
}

export function MentorTuningPanel({
  tuning,
  onChange,
  disabled,
  language,
}: Props) {
  const numField = (key: NumKey, label: Copy, integer = false) => (
    <label className="text-xs flex flex-col gap-1" style={{ color: '#EAECEF' }}>
      {tr(label, language)}
      <input
        type="number"
        min={0}
        step={integer ? 1 : 'any'}
        disabled={disabled}
        data-testid={`mentor-tuning-${key}`}
        value={tuning?.[key] ?? ''}
        placeholder={`${tr(COPY.deflt, language)}: ${MENTOR_TUNING_DEFAULTS[key]}`}
        onChange={(e) => {
          const raw = e.target.value.trim()
          const n = Number(raw)
          onChange(
            withTuning(tuning, {
              [key]: raw === '' || Number.isNaN(n) ? undefined : n,
            } as Partial<MentorTuning>)
          )
        }}
        className="px-2 py-1 rounded"
        style={box}
      />
    </label>
  )

  const boolField = (key: BoolKey, label: Copy) => {
    const cur = tuning?.[key]
    const val = cur === undefined ? 'default' : cur ? 'on' : 'off'
    return (
      <label
        className="text-xs flex flex-col gap-1"
        style={{ color: '#EAECEF' }}
      >
        {tr(label, language)}
        <select
          disabled={disabled}
          data-testid={`mentor-tuning-${key}`}
          value={val}
          onChange={(e) => {
            const v = e.target.value
            onChange(
              withTuning(tuning, {
                [key]: v === 'default' ? undefined : v === 'on',
              } as Partial<MentorTuning>)
            )
          }}
          className="px-2 py-1 rounded"
          style={box}
        >
          <option value="default">{`${tr(COPY.deflt, language)}: ${tr(
            MENTOR_TUNING_DEFAULTS[key] ? COPY.on : COPY.off,
            language
          )}`}</option>
          <option value="on">{tr(COPY.on, language)}</option>
          <option value="off">{tr(COPY.off, language)}</option>
        </select>
      </label>
    )
  }

  return (
    <div
      className="p-4 rounded-lg"
      style={{ background: '#0B0E11', border: '1px solid #2B3139' }}
      data-testid="mentor-tuning-block"
    >
      <div className="text-sm font-medium" style={{ color: '#EAECEF' }}>
        {tr(COPY.title, language)}
      </div>
      <p className="text-xs mt-2" style={{ color: '#848E9C' }}>
        {tr(COPY.desc, language)}
      </p>
      <div className="mt-3 grid gap-3 sm:grid-cols-2">
        <label
          className="text-xs flex flex-col gap-1"
          style={{ color: '#EAECEF' }}
        >
          {tr(COPY.school, language)}
          <select
            disabled={disabled}
            data-testid="mentor-tuning-trigger_school"
            value={tuning?.trigger_school ?? ''}
            onChange={(e) => {
              const v = e.target.value
              onChange(
                withTuning(tuning, {
                  trigger_school: v === '' ? undefined : (Number(v) as 1 | 2),
                })
              )
            }}
            className="px-2 py-1 rounded"
            style={box}
          >
            <option value="">{`${tr(COPY.deflt, language)}: ${tr(COPY.school1, language)}`}</option>
            <option value="1">{tr(COPY.school1, language)}</option>
            <option value="2">{tr(COPY.school2, language)}</option>
          </select>
        </label>
        {boolField('orb_gate_enabled', COPY.orb)}
        {boolField('isb_reverse_ema9_enabled', COPY.isbRev)}
        {boolField('htf_gate_news_only', COPY.htfNews)}
        {numField('ping_pong_min_gap_pts', COPY.pingGap)}
        {numField('ping_pong_candle_max_pts', COPY.pingCandle)}
        {numField('ping_pong_candle_lookback', COPY.pingLookback, true)}
        {numField('level_max_visits', COPY.visits, true)}
        {numField('day_gate_spent_pts', COPY.spent)}
        {numField('day_gate_target_cap_pts', COPY.cap)}
        {numField('swing_max_stop_pts', COPY.swing)}
      </div>
    </div>
  )
}
