// Studio controls for the release #25 course-exits knobs (lane
// mentor-course-exits, owner "do all now" 2026-10-14). Each is a top-level
// risk_control field; "default" REMOVES the key so the server runs the
// knob-rule default (trader/mentor_course_exits.go). Applied when the strategy
// is saved (the running trader reloads on save).

type Copy = { zh: string; en: string; es: string }

export type CourseExitsBoolKey =
  | 'mentor_swing_ema34_is_first_target'
  | 'mentor_swing_runner'
  | 'mentor_resonance_runner_tp_old_high'
  | 'mentor_be_before_partial'
  | 'mentor_isb_c3_partial_any_profit'
  | 'mentor_reverse_isb_as_phl'
  | 'mentor_box_fresh_rth_pair'

export type CourseExitsNumKey =
  | 'mentor_swing4h_contracts'
  | 'mentor_isb_max_pts'
  | 'mentor_keypair_candle_ratio'
  | 'mentor_box_max_height_pts'

export type CourseExitsKey =
  | CourseExitsBoolKey
  | CourseExitsNumKey
  | 'mentor_ema9_visit_cap'

// The knob-rule defaults the server resolves for an ABSENT key
// (course-defaults-30: swing_runner, be_before_partial and reverse_isb_as_phl
// are now ON; swing4h_contracts 2; isb_max_pts 30).
export const MENTOR_COURSE_EXITS_DEFAULTS = {
  mentor_swing_ema34_is_first_target: true,
  mentor_swing_runner: true,
  mentor_resonance_runner_tp_old_high: true,
  mentor_be_before_partial: true,
  mentor_isb_c3_partial_any_profit: true,
  mentor_reverse_isb_as_phl: true,
  mentor_box_fresh_rth_pair: false,
  mentor_ema9_visit_cap: 'shared',
  mentor_swing4h_contracts: 2,
  mentor_isb_max_pts: 30,
  mentor_keypair_candle_ratio: 1,
  mentor_box_max_height_pts: 0,
} as const

const COPY = {
  title: {
    zh: '🧑‍🏫 课程出场与规则开关（#25）',
    en: '🧑‍🏫 Course exits & rules switches (#25)',
    es: '🧑‍🏫 Interruptores de salidas y reglas del curso (#25)',
  },
  desc: {
    zh: '每项都是独立开关。“默认”= 不保存该键，服务器按规则使用默认值。保存策略后生效。',
    en: 'Each item is its own switch. "default" = the key is not stored and the server runs the ruled default. Applied when you save the strategy.',
    es: 'Cada elemento es su propio interruptor. "por defecto" = la clave no se guarda y el servidor usa el valor dictado. Se aplica al guardar la estrategia.',
  },
  deflt: { zh: '默认', en: 'default', es: 'por defecto' },
  on: { zh: '开', en: 'ON', es: 'ON' },
  off: { zh: '关', en: 'OFF', es: 'OFF' },
  mentor_swing_ema34_is_first_target: {
    zh: 'SWING：5 分钟 EMA 34 是第一目标（不是障碍）',
    en: 'SWING: the 5m EMA 34 is the first target (not an obstacle)',
    es: 'SWING: la EMA 34 de 5m es el primer objetivo (no un obstáculo)',
  },
  mentor_swing_runner: {
    zh: 'SWING：保留 runner（需 ≥ 2 口）',
    en: 'SWING: keep a runner (needs ≥ 2 contracts)',
    es: 'SWING: mantener un runner (≥ 2 contratos)',
  },
  mentor_resonance_runner_tp_old_high: {
    zh: '共振：runner 止盈移到前高之外',
    en: 'Resonance: move the runner TP past the old high',
    es: 'Resonancia: mover el TP del runner más allá del máximo anterior',
  },
  mentor_be_before_partial: {
    zh: '1:1 部分止盈前，到一半就保本（课程默认）',
    en: 'Break-even at half-way BEFORE the 1:1 partial (the course default)',
    es: 'Break-even a mitad de camino ANTES del parcial 1:1 (el curso por defecto)',
  },
  mentor_isb_c3_partial_any_profit: {
    zh: 'ISB 第 3 根 K 线收盘有利润即部分止盈',
    en: 'ISB candle-3 partial at any profit',
    es: 'Parcial ISB en la vela 3 con cualquier ganancia',
  },
  mentor_reverse_isb_as_phl: {
    zh: '反向 ISB 按 PHL/PLH 处理',
    en: 'Treat the reverse ISB as a PHL/PLH',
    es: 'Tratar el ISB inverso como PHL/PLH',
  },
  mentor_box_fresh_rth_pair: {
    zh: '08:30 CT 重新画 RTH 箱体',
    en: 'Fresh FTGH/FTGL pair at the 08:30 CT RTH open',
    es: 'Nuevo par FTGH/FTGL en la apertura RTH 08:30 CT',
  },
  mentor_ema9_visit_cap: {
    zh: 'EMA 9 触碰次数上限',
    en: 'EMA 9 visit cap',
    es: 'Tope de visitas a la EMA 9',
  },
  shared: {
    zh: 'shared — 共用一个每日额度（默认）',
    en: 'shared — one daily budget (default)',
    es: 'shared — un solo cupo diario (por defecto)',
  },
  per_level: {
    zh: 'per_level — 每个价位单独计数',
    en: 'per_level — each EMA 9 price counts on its own',
    es: 'per_level — cada precio de la EMA 9 cuenta aparte',
  },
  exempt: {
    zh: 'exempt — 不限次数（同 EMA 34）',
    en: 'exempt — never capped (like EMA 34)',
    es: 'exempt — sin tope (como la EMA 34)',
  },
  mentor_swing4h_contracts: {
    zh: 'SWING 口数（默认 2；runner 需 ≥ 2）',
    en: 'SWING contracts (default 2; a runner needs ≥ 2)',
    es: 'Contratos SWING (por defecto 2; un runner necesita ≥ 2)',
  },
  mentor_isb_max_pts: {
    zh: 'ISB 最大止损（点，0 → 30）',
    en: 'ISB max stop (pts, 0 → 30)',
    es: 'Stop máximo ISB (pts, 0 → 30)',
  },
  mentor_keypair_candle_ratio: {
    zh: '价位间距 / K 线比例（默认 1）',
    en: 'Key-level gap ÷ candle ratio (default 1)',
    es: 'Relación hueco de niveles ÷ vela (por defecto 1)',
  },
  mentor_box_max_height_pts: {
    zh: '箱体最大高度（点，0 = 关）',
    en: 'Max box height (pts, 0 = off)',
    es: 'Altura máxima de caja (pts, 0 = off)',
  },
} satisfies Record<string, Copy>

function tr(entry: Copy, language: string): string {
  return (entry as unknown as Record<string, string>)[language] ?? entry.en
}

const BOOL_KEYS: CourseExitsBoolKey[] = [
  'mentor_swing_ema34_is_first_target',
  'mentor_swing_runner',
  'mentor_resonance_runner_tp_old_high',
  'mentor_be_before_partial',
  'mentor_isb_c3_partial_any_profit',
  'mentor_reverse_isb_as_phl',
  'mentor_box_fresh_rth_pair',
]

const NUM_KEYS: CourseExitsNumKey[] = [
  'mentor_swing4h_contracts',
  'mentor_isb_max_pts',
  'mentor_keypair_candle_ratio',
  'mentor_box_max_height_pts',
]

export type CourseExitsValues = Partial<
  Record<CourseExitsBoolKey, boolean> &
    Record<CourseExitsNumKey, number> & { mentor_ema9_visit_cap: string }
>

interface Props {
  values: CourseExitsValues
  // undefined = remove the key (the server default applies)
  onChange: (
    key: CourseExitsKey,
    value: boolean | number | string | undefined
  ) => void
  disabled?: boolean
  language: string
}

const box = {
  background: '#1E2329',
  border: '1px solid #2B3139',
  color: '#EAECEF',
}

export function MentorCourseExitsPanel({
  values,
  onChange,
  disabled,
  language,
}: Props) {
  return (
    <div
      className="p-4 rounded-lg"
      style={{ background: '#0B0E11', border: '1px solid #2B3139' }}
      data-testid="mentor-course-exits-block"
    >
      <div className="text-sm font-medium" style={{ color: '#EAECEF' }}>
        {tr(COPY.title, language)}
      </div>
      <p className="text-xs mt-2" style={{ color: '#848E9C' }}>
        {tr(COPY.desc, language)}
      </p>
      <div className="mt-3 grid gap-3 sm:grid-cols-2">
        {BOOL_KEYS.map((key) => {
          const cur = values[key]
          const val = cur === undefined ? 'default' : cur ? 'on' : 'off'
          return (
            <label
              key={key}
              className="text-xs flex flex-col gap-1"
              style={{ color: '#EAECEF' }}
            >
              {tr(COPY[key], language)}
              <select
                disabled={disabled}
                data-testid={`mentor-course-${key}`}
                value={val}
                onChange={(e) => {
                  const v = e.target.value
                  onChange(key, v === 'default' ? undefined : v === 'on')
                }}
                className="px-2 py-1 rounded"
                style={box}
              >
                <option value="default">{`${tr(COPY.deflt, language)}: ${tr(
                  MENTOR_COURSE_EXITS_DEFAULTS[key] ? COPY.on : COPY.off,
                  language
                )}`}</option>
                <option value="on">{tr(COPY.on, language)}</option>
                <option value="off">{tr(COPY.off, language)}</option>
              </select>
            </label>
          )
        })}
        <label
          className="text-xs flex flex-col gap-1"
          style={{ color: '#EAECEF' }}
        >
          {tr(COPY.mentor_ema9_visit_cap, language)}
          <select
            disabled={disabled}
            data-testid="mentor-course-mentor_ema9_visit_cap"
            value={values.mentor_ema9_visit_cap ?? ''}
            onChange={(e) =>
              onChange(
                'mentor_ema9_visit_cap',
                e.target.value === '' ? undefined : e.target.value
              )
            }
            className="px-2 py-1 rounded"
            style={box}
          >
            <option value="">{`${tr(COPY.deflt, language)}: ${tr(COPY.shared, language)}`}</option>
            <option value="shared">{tr(COPY.shared, language)}</option>
            <option value="per_level">{tr(COPY.per_level, language)}</option>
            <option value="exempt">{tr(COPY.exempt, language)}</option>
          </select>
        </label>
        {NUM_KEYS.map((key) => (
          <label
            key={key}
            className="text-xs flex flex-col gap-1"
            style={{ color: '#EAECEF' }}
          >
            {tr(COPY[key], language)}
            <input
              type="number"
              min={0}
              step={key === 'mentor_swing4h_contracts' ? 1 : 'any'}
              disabled={disabled}
              data-testid={`mentor-course-${key}`}
              value={values[key] ?? ''}
              placeholder={`${tr(COPY.deflt, language)}: ${MENTOR_COURSE_EXITS_DEFAULTS[key]}`}
              onChange={(e) => {
                const raw = e.target.value.trim()
                const n = Number(raw)
                const v = key === 'mentor_swing4h_contracts' ? Math.round(n) : n // an int field on the server
                onChange(key, raw === '' || Number.isNaN(n) ? undefined : v)
              }}
              className="px-2 py-1 rounded"
              style={box}
            />
          </label>
        ))}
      </div>
    </div>
  )
}
