// Studio controls for the release #25 course-exits knobs (lane
// mentor-course-exits, owner "do all now" 2026-10-14). Each is a top-level
// risk_control field; "default" REMOVES the key so the server runs the
// knob-rule default (trader/mentor_course_exits.go). Applied when the strategy
// is saved (the running trader reloads on save).

import { EffectiveLine } from './EffectiveChip'
import type { EffectiveKnob } from '../../lib/api/strategyEffective'

type Copy = { zh: string; en: string; es: string }

export type CourseExitsBoolKey =
  | 'mentor_swing_ema34_is_first_target'
  | 'mentor_swing_runner'
  | 'mentor_resonance_runner_tp_old_high'
  | 'mentor_be_before_partial'
  | 'mentor_isb_c3_partial_any_profit'
  | 'mentor_reverse_isb_as_phl'
  | 'mentor_box_fresh_rth_pair'
  | 'mentor_resonance_raw_isb'
  | 'mentor_2m_reduce_size'
  | 'mentor_swing_isb_keep'
  | 'mentor_stop_size_ladder'

export type CourseExitsNumKey =
  | 'mentor_swing4h_contracts'
  | 'mentor_isb_max_pts'
  | 'mentor_keypair_candle_ratio'
  | 'mentor_box_max_height_pts'
  | 'mentor_swing_max_stop_pts'
  | 'mentor_news_read_lead_min'
  | 'mentor_wide_stop_contracts'
  | 'mentor_wide_stop_max_pts'

// m466 P3 + P4 (release #61): the string-enum knobs rendered from ENUM_KEYS below.
export type CourseExitsEnumKey =
  | 'mentor_open5m_break'
  | 'mentor_news_wait_mode'
  | 'mentor_swing_isb_close_back'
  | 'mentor_swing_runner_tp'

export type CourseExitsKey =
  | CourseExitsBoolKey
  | CourseExitsNumKey
  | CourseExitsEnumKey
  | 'mentor_ema9_visit_cap'
  | 'mentor_isb_loss_box'

// The knob-rule defaults the server resolves for an ABSENT key
// (course-defaults-30: swing_runner, be_before_partial and reverse_isb_as_phl
// are now ON; swing4h_contracts 2; isb_max_pts 20 since m466 G23 D4.1-R12).
export const MENTOR_COURSE_EXITS_DEFAULTS = {
  mentor_swing_ema34_is_first_target: true,
  mentor_swing_runner: true,
  mentor_resonance_runner_tp_old_high: false,
  mentor_be_before_partial: true,
  mentor_isb_c3_partial_any_profit: true,
  mentor_reverse_isb_as_phl: true,
  mentor_box_fresh_rth_pair: true,
  // m465 P4 (release #60): D2.4-R2 raw inside-bar resonance + D3.1-R30 2m size
  // cut — both ON by default (the course is explicit).
  mentor_resonance_raw_isb: true,
  mentor_2m_reduce_size: true,
  mentor_ema9_visit_cap: 'shared',
  mentor_swing4h_contracts: 2,
  // m466 G23 (D4.1-R12 "inside bar mà nó hai mươi mấy điểm… đừng vô" [D4.1 p1
  // @05:41–06:08]): the ISB ceiling is 20 (PHL/PLH keep 25).
  mentor_isb_max_pts: 20,
  mentor_keypair_candle_ratio: 1,
  mentor_box_max_height_pts: 0,
  // m463 (D5.2-R31, owner order 2026-11-01 "full of it" — reverses m186): a
  // swing stop ABOVE this is refused, "100 điểm quá nhiều rủi ro… đừng vô".
  // answers-1112 Q13: 90 → 80 ("7-80 điểm … cứ vô 1-2" [Sept D5.2 p2
  // @05:25-05:33]); 90 reverts.
  mentor_swing_max_stop_pts: 80,
  // m465 P2 (D4.2-R27 "tôi bọc cái khu vực này lại. Tôi sẽ không trade bên
  // trong" [D4.2 p2 @03:43–04:07]): an ISB loss boxes the whole 5m range.
  mentor_isb_loss_box: 'range',
  // m466 P3 (release #61): G55 D4.4-R40 the opening 5m range breaks on a 1m
  // close; G56 D4.4-R22 the news wait reads BOTH lines; G58 D4.4-R6 the 1h/4h
  // read is taken 30 min before the print.
  mentor_open5m_break: '1m_close',
  mentor_news_wait_mode: 'both',
  mentor_news_read_lead_min: 30,
  // m466 P4 (release #61, swing D5.2): G63 R26 — the inside bar alone after an
  // invalid cross ("đợi đóng ngược lại cũng được nhưng hơi lâu" [D5.2 p3
  // @04:49–05:09]); G66 R25 — the inside-bar order survives a close through
  // ("Để cái cây inside bar này cho em" [D5.2 p2 @10:23–10:51]); G65 R6 — the
  // runner has no TP, held by the 4h ("gồng theo khung 4 giờ" [D5.2 p1
  // @05:40–06:14]).
  mentor_swing_isb_close_back: 'not_required',
  mentor_swing_isb_keep: true,
  mentor_swing_runner_tp: 'none',
  // Stop-to-size ladder (owner order 2026-11-10): a big non-swing stop is
  // sized DOWN, not refused — "tầm 20 điểm, 30 điểm… Tôi vô size 3"
  // [OctD2.2-R73 @01:12:32]; "40 điểm… Trade vẫn được. 1 MNQ" [OctD2.3-R47
  // @48:12]; above 40 not traded.
  mentor_stop_size_ladder: true,
  mentor_wide_stop_contracts: 1,
  mentor_wide_stop_max_pts: 40,
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
    zh: '共振：runner 止盈封顶在前高之外（关 = 不封顶，课程默认）',
    en: 'Resonance: cap the runner TP at the level past the old high (OFF = no cap, the course default)',
    es: 'Resonancia: limitar el TP del runner al nivel tras el máximo anterior (OFF = sin tope, el curso por defecto)',
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
    zh: 'RTH 新箱体取代夜盘箱体(同方向)',
    en: 'A fresh RTH FTGH/FTGL replaces the evening box of its role',
    es: 'Un FTGH/FTGL nuevo de RTH reemplaza la caja nocturna de su rol',
  },
  mentor_resonance_raw_isb: {
    zh: '共振：PHL/PLH 成交后 3 根内出现同向 inside bar 即启动（无需该 ISB 入场）',
    en: 'Resonance: any same-way inside bar within 3 candles of a PHL/PLH fill arms it (no ISB entry needed)',
    es: 'Resonancia: cualquier inside bar en la misma dirección dentro de 3 velas tras el fill PHL/PLH la activa (sin entrada ISB)',
  },
  mentor_2m_reduce_size: {
    zh: '2 分钟（或 5 分钟）入场的 ISB/PHL/PLH 减仓（减仓档）',
    en: 'A 2m (or 5m) ISB/PHL/PLH entry takes the reduced size',
    es: 'Una entrada ISB/PHL/PLH en 2m (o 5m) usa el tamaño reducido',
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
  mentor_isb_loss_box: {
    zh: 'ISB 亏损后封锁的区域',
    en: 'ISB loss box (what an ISB stop-out blocks)',
    es: 'Caja de pérdida ISB (qué bloquea un stop de ISB)',
  },
  range: {
    zh: 'range — 整个持仓的 5 分钟区间（默认，课程）',
    en: 'range — the whole 5m range that held the trade (default, the course)',
    es: 'range — todo el rango de 5m que contuvo la operación (por defecto, el curso)',
  },
  band: {
    zh: 'band — 仅入场到止损的区间（#60 之前）',
    en: 'band — the entry-to-stop band only (pre-#60)',
    es: 'band — solo la banda entrada-stop (antes de #60)',
  },
  mentor_swing_isb_keep: {
    zh: 'SWING：inside bar 挂单在 5 分钟收盘穿线后仍保留（课程默认）',
    en: 'SWING: the inside-bar order survives a later 5m close through the line (the course default)',
    es: 'SWING: la orden del inside bar sobrevive a un cierre de 5m a través de la línea (el curso por defecto)',
  },
  mentor_swing_isb_close_back: {
    zh: 'SWING：失效后的 inside bar 是否需要收回线内',
    en: 'SWING: does the inside bar after an invalid cross need a close back?',
    es: 'SWING: ¿el inside bar tras un cruce inválido necesita cerrar de vuelta?',
  },
  not_required: {
    zh: 'not_required — 只要 inside bar 即可（默认，课程）',
    en: 'not_required — the inside bar alone (default, the course)',
    es: 'not_required — basta el inside bar (por defecto, el curso)',
  },
  required: {
    zh: 'required — K1 与 inside bar 都需收回（#61 之前）',
    en: 'required — candle 1 and the inside bar both close back (pre-#61)',
    es: 'required — la vela 1 y el inside bar cierran de vuelta (antes de #61)',
  },
  mentor_swing_runner_tp: {
    zh: 'SWING：runner 止盈',
    en: 'SWING: runner take-profit',
    es: 'SWING: take-profit del runner',
  },
  none: {
    zh: 'none — 无止盈，按 4 小时持有（默认，课程）',
    en: 'none — no TP, held by the 4h (default, the course)',
    es: 'none — sin TP, se mantiene por la 4h (por defecto, el curso)',
  },
  capped: {
    zh: 'capped — max(100, 2 × 第一目标距离)（#61 之前）',
    en: 'capped — max(100, 2 × the first-target distance) (pre-#61)',
    es: 'capped — max(100, 2 × la distancia al primer objetivo) (antes de #61)',
  },
  mentor_swing4h_contracts: {
    zh: 'SWING 口数（默认 2；runner 需 ≥ 2）',
    en: 'SWING contracts (default 2; a runner needs ≥ 2)',
    es: 'Contratos SWING (por defecto 2; un runner necesita ≥ 2)',
  },
  mentor_isb_max_pts: {
    zh: '最大止损（仅 ISB；点，0 → 20；PHL/PLH 仍为 25）',
    en: 'Max stop (ISB only; pts, 0 → 20; PHL/PLH keep 25)',
    es: 'Stop máximo (solo ISB; pts, 0 → 20; PHL/PLH mantienen 25)',
  },
  mentor_news_read_lead_min: {
    zh: '新闻前读取 1h/4h 线（分钟，默认 30；0 = 07:20 开始）',
    en: 'Read the 1h/4h lines before the news (min, default 30; 0 = from 07:20)',
    es: 'Leer las líneas 1h/4h antes de la noticia (min, por defecto 30; 0 = desde 07:20)',
  },
  mentor_open5m_break: {
    zh: '开盘 5 分钟区间突破',
    en: 'Opening 5m range break',
    es: 'Ruptura del rango de apertura de 5m',
  },
  '1m_close': {
    zh: '1m_close — 1 分钟 K 线收在区间外（默认，课程）',
    en: '1m_close — a 1m candle closes outside (default, the course)',
    es: '1m_close — una vela de 1m cierra fuera (por defecto, el curso)',
  },
  '5m_close': {
    zh: '5m_close — 5 分钟 K 线收在区间外（#61 之前）',
    en: '5m_close — a 5m candle closes outside (pre-#61)',
    es: '5m_close — una vela de 5m cierra fuera (antes de #61)',
  },
  mentor_news_wait_mode: {
    zh: '新闻后等待：收盘需在哪些线之外',
    en: 'News wait: the close must be beyond',
    es: 'Espera de noticias: el cierre debe estar más allá de',
  },
  both: {
    zh: 'both — 4h 线和新闻前 1h 线（默认，课程）',
    en: 'both — the 4h AND the pre-print 1h line (default, the course)',
    es: 'both — la línea 4h Y la 1h previa (por defecto, el curso)',
  },
  '4h_only': {
    zh: '4h_only — 只看 4h 线（#61 之前）',
    en: '4h_only — the 4h line only (pre-#61)',
    es: '4h_only — solo la línea 4h (antes de #61)',
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
  mentor_swing_max_stop_pts: {
    zh: 'SWING 最大止损（点，超过即不进场；0 → 80）',
    en: 'SWING max stop (pts, above it = do not enter; 0 → 80)',
    es: 'Stop máximo SWING (pts, por encima = no entrar; 0 → 80)',
  },
  mentor_stop_size_ladder: {
    zh: '止损-口数阶梯：大止损减仓而不是拒绝（20–30 → 减仓档，30–上限 → 宽止损口数；SWING 除外）',
    en: 'Stop-size ladder: a big stop is sized DOWN, not refused (20–30 → reduced, 30–max → wide-stop contracts; not the swing)',
    es: 'Escalera stop-tamaño: un stop grande reduce el tamaño, no se rechaza (20–30 → reducido, 30–máx → contratos de stop amplio; no el swing)',
  },
  mentor_wide_stop_contracts: {
    zh: '宽止损口数（止损 30 点以上；0 → 1）',
    en: 'Wide-stop contracts (stop above 30 pts; 0 → 1)',
    es: 'Contratos de stop amplio (stop sobre 30 pts; 0 → 1)',
  },
  mentor_wide_stop_max_pts: {
    zh: '阶梯最大止损（点，超过即不进场；0 → 40）',
    en: 'Ladder max stop (pts, above it = do not enter; 0 → 40)',
    es: 'Stop máximo de la escalera (pts, por encima = no entrar; 0 → 40)',
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
  'mentor_resonance_raw_isb',
  'mentor_2m_reduce_size',
  'mentor_swing_isb_keep',
  'mentor_stop_size_ladder',
]

const NUM_KEYS: CourseExitsNumKey[] = [
  'mentor_swing4h_contracts',
  'mentor_isb_max_pts',
  'mentor_keypair_candle_ratio',
  'mentor_box_max_height_pts',
  'mentor_swing_max_stop_pts',
  'mentor_news_read_lead_min',
  'mentor_wide_stop_contracts',
  'mentor_wide_stop_max_pts',
]

// m466 P3 + P4 (release #61): the enum selects; the first option is the
// default. One key per line (guideTruth T16 reads the Studio controls from
// these lines).
const ENUM_KEYS: CourseExitsEnumKey[] = [
  'mentor_open5m_break',
  'mentor_news_wait_mode',
  'mentor_swing_isb_close_back',
  'mentor_swing_runner_tp',
]

const ENUM_OPTIONS: Record<CourseExitsEnumKey, (keyof typeof COPY)[]> = {
  mentor_open5m_break: ['1m_close', '5m_close'], // P3 G55 D4.4-R40
  mentor_news_wait_mode: ['both', '4h_only'], // P3 G56 D4.4-R22
  mentor_swing_isb_close_back: ['not_required', 'required'], // P4 G63 D5.2-R26
  mentor_swing_runner_tp: ['none', 'capped'], // P4 G65 D5.2-R6
}

export type CourseExitsValues = Partial<
  Record<CourseExitsBoolKey, boolean> &
    Record<CourseExitsNumKey, number> & {
      mentor_ema9_visit_cap: string
      mentor_isb_loss_box: string
      mentor_open5m_break: string
      mentor_news_wait_mode: string
      mentor_swing_isb_close_back: string
      mentor_swing_runner_tp: string
    }
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
  // Effective rows for the risk_control.mentor_* course-exit leaves (prefix
  // applied by the caller). Optional: absent → no chips.
  eff?: (leaf: string) => EffectiveKnob | null | undefined
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
  eff,
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
              <EffectiveLine knob={eff?.(key)} />
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
          <EffectiveLine knob={eff?.('mentor_ema9_visit_cap')} />
        </label>
        <label
          className="text-xs flex flex-col gap-1"
          style={{ color: '#EAECEF' }}
        >
          {tr(COPY.mentor_isb_loss_box, language)}
          <select
            disabled={disabled}
            data-testid="mentor-course-mentor_isb_loss_box"
            value={values.mentor_isb_loss_box ?? ''}
            onChange={(e) =>
              onChange(
                'mentor_isb_loss_box',
                e.target.value === '' ? undefined : e.target.value
              )
            }
            className="px-2 py-1 rounded"
            style={box}
          >
            <option value="">{`${tr(COPY.deflt, language)}: ${tr(COPY.range, language)}`}</option>
            <option value="range">{tr(COPY.range, language)}</option>
            <option value="band">{tr(COPY.band, language)}</option>
          </select>
          <EffectiveLine knob={eff?.('mentor_isb_loss_box')} />
        </label>
        {ENUM_KEYS.map((key) => (
          <label
            key={key}
            className="text-xs flex flex-col gap-1"
            style={{ color: '#EAECEF' }}
          >
            {tr(COPY[key], language)}
            <select
              disabled={disabled}
              data-testid={`mentor-course-${key}`}
              value={values[key] ?? ''}
              onChange={(e) =>
                onChange(
                  key,
                  e.target.value === '' ? undefined : e.target.value
                )
              }
              className="px-2 py-1 rounded"
              style={box}
            >
              <option value="">{`${tr(COPY.deflt, language)}: ${tr(COPY[ENUM_OPTIONS[key][0]], language)}`}</option>
              {ENUM_OPTIONS[key].map((o) => (
                <option key={o} value={o}>
                  {tr(COPY[o], language)}
                </option>
              ))}
            </select>
            <EffectiveLine knob={eff?.(key)} />
          </label>
        ))}
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
              step={
                key === 'mentor_swing4h_contracts' ||
                key === 'mentor_wide_stop_contracts'
                  ? 1
                  : 'any'
              }
              disabled={disabled}
              data-testid={`mentor-course-${key}`}
              value={values[key] ?? ''}
              placeholder={`${tr(COPY.deflt, language)}: ${MENTOR_COURSE_EXITS_DEFAULTS[key]}`}
              onChange={(e) => {
                const raw = e.target.value.trim()
                const n = Number(raw)
                const v =
                  key === 'mentor_swing4h_contracts' ||
                  key === 'mentor_wide_stop_contracts'
                    ? Math.round(n)
                    : n // an int field on the server
                onChange(key, raw === '' || Number.isNaN(n) ? undefined : v)
              }}
              className="px-2 py-1 rounded"
              style={box}
            />
            <EffectiveLine knob={eff?.(key)} />
          </label>
        ))}
      </div>
    </div>
  )
}
