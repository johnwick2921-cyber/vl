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
  | 'orb_edge_veto_pts'

type BoolKey =
  | 'orb_gate_enabled'
  | 'isb_reverse_ema9_enabled'
  | 'htf_gate_news_only'
  | 'exec_2m_after_30m'
  | 'news_wait_both_lines'
  | 'news_bars_excluded'
  | 'level_entries_need_trigger_side'
  | 'trendline_is_target'
  | 'trendline_between_boxes'
  | 'leg_reset_on_wave_break'
  | 'flag_wall_target'
  | 'box_new_zone_after_escape'

type EnumKey =
  | '15m_ib_requires_5m_isb'
  | 'tf_conflict_end'
  | 'isb_box_escape'
  | 'ema34_loss_relock'
  | 'd43_trend_source'

type EnumOption<K extends EnumKey> = {
  value: NonNullable<MentorTuning[K]>
  label: Copy
}

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
  exec2m: {
    zh: '09:00 CT 后改用 2 分钟执行 ISB',
    en: 'Read the ISB on the 2m after 09:00 CT',
    es: 'Leer el ISB en 2m después de las 09:00 CT',
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
  // Course-entry knobs (release #25).
  ib15m: {
    zh: '15 分钟内包箱体内：1 分钟 ISB 的条件',
    en: 'Inside a 15m inside-bar box: what a 1m ISB needs',
    es: 'Dentro de una caja inside bar de 15m: qué necesita un ISB de 1m',
  },
  ib15mOff: {
    zh: '关 — 与 15 分钟同向的 1 分钟 ISB 直接放行（#25 之前）',
    en: 'off — a 1m ISB in the 15m direction passes (pre-#25)',
    es: 'off — pasa un ISB de 1m en la dirección de 15m (antes de #25)',
  },
  ib15mRequire: {
    zh: '需要与 15 分钟同向的 5 分钟 ISB',
    en: 'require a 5m ISB in the 15m direction',
    es: 'exigir un ISB de 5m en la dirección de 15m',
  },
  ib15mWait: {
    zh: '15 分钟箱体突破前不做 ISB',
    en: 'no ISB until the 15m box escapes',
    es: 'ningún ISB hasta que escape la caja de 15m',
  },
  tfConflict: {
    zh: '5 分钟 / 15 分钟箱体冲突何时结束',
    en: '5m / 15m box conflict ends when',
    es: 'El conflicto de cajas 5m / 15m termina cuando',
  },
  tfConflictFirst: {
    zh: '5 分钟箱体突破',
    en: 'the 5m box escapes',
    es: 'escapa la caja de 5m',
  },
  tfConflictBoth: {
    zh: '5 分钟与 15 分钟箱体都已突破',
    en: 'both the 5m and the 15m boxes escaped',
    es: 'escaparon las cajas de 5m y de 15m',
  },
  newsWait: {
    zh: '新闻日：08:30 CT 前须越过 4 小时触发线',
    en: 'News day: wait beyond the 4h trigger line until 08:30 CT',
    es: 'Día de noticias: esperar más allá de la línea 4h hasta las 08:30 CT',
  },
  newsBars: {
    zh: '新闻日：公布至 08:30 CT 的 K 线不计入 1h/4h 触发线',
    en: 'News day: print-to-08:30 CT bars do not move the 1h/4h lines',
    es: 'Día de noticias: las velas del dato a las 08:30 CT no mueven las líneas 1h/4h',
  },
  isbEscape: {
    zh: 'ISB 休息箱体突破判定',
    en: 'ISB rest box escape test',
    es: 'Prueba de escape de la caja de reposo ISB',
  },
  isbEscapeClose: {
    zh: '收盘 — 仅收盘价越过边缘',
    en: 'close — the close beyond the edge',
    es: 'cierre — el cierre fuera del borde',
  },
  isbEscapeBody: {
    zh: '实体 — 开盘价与收盘价都越过边缘',
    en: 'body — open AND close beyond the edge',
    es: 'cuerpo — apertura Y cierre fuera del borde',
  },
  emaRelock: {
    zh: 'EMA 34 亏损一次后何时恢复',
    en: 'EMA 34 comes back after a loss',
    es: 'La EMA 34 vuelve tras una pérdida',
  },
  emaRelockSession: {
    zh: '本交易日关闭，至 17:00 CT 换日',
    en: 'at the 17:00 CT roll (off for the session)',
    es: 'en el cambio de las 17:00 CT (apagada en la sesión)',
  },
  emaRelockZone: {
    zh: '一根已收盘 1 分钟 K 线完全离开亏损区',
    en: 'when a closed 1m candle leaves the loss zone',
    es: 'cuando una vela de 1m cerrada sale de la zona de pérdida',
  },
  orbEdge: {
    zh: 'ORB 边缘否决距离（点，0 = 关，0.25–50）',
    en: 'ORB edge veto (pts, 0 = off, 0.25–50)',
    es: 'Veto junto al borde ORB (pts, 0 = off, 0,25–50)',
  },
  d43: {
    zh: '箱体回踩的 5 分钟趋势依据',
    en: 'Box return: the 5m trend is read from',
    es: 'Retorno a caja: la tendencia de 5m se lee de',
  },
  d43Trigger: {
    zh: '5 分钟触发线',
    en: 'the 5m trigger line',
    es: 'la línea de disparo de 5m',
  },
  d43Structure: {
    zh: '5 分钟波段结构（高点与低点）',
    en: '5m swing structure (highs and lows)',
    es: 'estructura de swings de 5m (máximos y mínimos)',
  },
  lvlTrigger: {
    zh: '价位入场也需 5 分钟触发线方向（仅箱体保留流派 1）',
    en: 'Level entries need the 5m trigger side (school 1 only at boxes)',
    es: 'Las entradas en nivel exigen el lado del disparo de 5m (escuela 1 solo en cajas)',
  },
  tlTarget: {
    zh: '路径上的趋势线可作为目标',
    en: 'A trendline in the path is a target',
    es: 'Una línea de tendencia en el camino es un objetivo',
  },
  tlBetween: {
    zh: '两个箱体之间允许趋势线拒绝入场',
    en: 'A trendline reject may trade between two boxes',
    es: 'Un rechazo en línea de tendencia puede operar entre dos cajas',
  },
  legWave: {
    zh: '止损后突破亏损波段极值即重置腿',
    en: 'A stopped-out leg also resets on a break of the losing wave',
    es: 'Un tramo con stop también se reinicia al romper la onda perdedora',
  },
  flagWall: {
    zh: '旗形内入场：对侧旗形边作为更近的目标',
    en: 'Inside a flag: the opposite flag wall is a closer target',
    es: 'Dentro de una bandera: la pared opuesta es un objetivo más cercano',
  },
  boxNewZone: {
    zh: '箱体突破后在区间外绘制新箱体',
    en: 'After a box escapes, draw one new box outside the range',
    es: 'Tras un escape de caja, dibujar una caja nueva fuera del rango',
  },
} satisfies Record<string, Copy>

// The option lists of the string-valued knobs; "" (blank) = the ruled default.
const ENUM_OPTIONS: { [K in EnumKey]: ReadonlyArray<EnumOption<K>> } = {
  '15m_ib_requires_5m_isb': [
    { value: 'off', label: COPY.ib15mOff },
    { value: 'require_5m_isb', label: COPY.ib15mRequire },
    { value: 'no_trade_until_escape', label: COPY.ib15mWait },
  ],
  tf_conflict_end: [
    { value: 'first_box', label: COPY.tfConflictFirst },
    { value: 'both_boxes', label: COPY.tfConflictBoth },
  ],
  isb_box_escape: [
    { value: 'close', label: COPY.isbEscapeClose },
    { value: 'body', label: COPY.isbEscapeBody },
  ],
  ema34_loss_relock: [
    { value: 'session', label: COPY.emaRelockSession },
    { value: 'until_zone_exit', label: COPY.emaRelockZone },
  ],
  d43_trend_source: [
    { value: 'trigger_line', label: COPY.d43Trigger },
    { value: 'structure', label: COPY.d43Structure },
  ],
}

function tr(entry: Copy, language: string): string {
  return (entry as unknown as Record<string, string>)[language] ?? entry.en
}

// Ruled defaults, shown as placeholders (kernel/mentor DefaultConfig — OWNER
// RULING 2026-10-04: spent 300 / cap 15, swing max stop 100, ISB reverse ON).
// Release #25 course-entry knobs: (a) course, no ruling against → ON;
// (b) owner ruling against / ambiguous / open question → OFF.
export const MENTOR_TUNING_DEFAULTS = {
  trigger_school: 1,
  ping_pong_min_gap_pts: 50,
  ping_pong_candle_max_pts: 20,
  ping_pong_candle_lookback: 30,
  level_max_visits: 3,
  orb_gate_enabled: true,
  isb_reverse_ema9_enabled: true,
  htf_gate_news_only: false,
  exec_2m_after_30m: false,
  day_gate_spent_pts: 300,
  day_gate_target_cap_pts: 15,
  swing_max_stop_pts: 100,
  '15m_ib_requires_5m_isb': 'require_5m_isb', // (a)
  tf_conflict_end: 'first_box', // (b) OQ-2
  news_wait_both_lines: false, // (b) OQ-6
  news_bars_excluded: false, // (b)
  isb_box_escape: 'body', // (a)
  ema34_loss_relock: 'until_zone_exit', // (a)
  orb_edge_veto_pts: 0, // (b) 0 = off
  d43_trend_source: 'trigger_line', // (b)
  level_entries_need_trigger_side: false, // (b) owner ruled school 1
  trendline_is_target: true, // (a)
  trendline_between_boxes: false, // (b)
  leg_reset_on_wave_break: true, // (a)
  flag_wall_target: false, // (b) X9 extra, ported from #416
  box_new_zone_after_escape: true, // (a) B52: new zone after range escape [D3.4 p3 recovered2 @00:00–00:21]
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

  // A string-valued knob: "" = the ruled default (the key is removed).
  const enumField = <K extends EnumKey>(
    key: K,
    label: Copy,
    options: ReadonlyArray<EnumOption<K>>
  ) => {
    const def = options.find((o) => o.value === MENTOR_TUNING_DEFAULTS[key])
    return (
      <label
        className="text-xs flex flex-col gap-1"
        style={{ color: '#EAECEF' }}
      >
        {tr(label, language)}
        <select
          disabled={disabled}
          data-testid={`mentor-tuning-${key}`}
          value={tuning?.[key] ?? ''}
          onChange={(e) => {
            const v = e.target.value
            onChange(
              withTuning(tuning, {
                [key]: v === '' ? undefined : v,
              } as Partial<MentorTuning>)
            )
          }}
          className="px-2 py-1 rounded"
          style={box}
        >
          <option value="">{`${tr(COPY.deflt, language)}: ${
            def ? tr(def.label, language) : MENTOR_TUNING_DEFAULTS[key]
          }`}</option>
          {options.map((o) => (
            <option key={o.value} value={o.value}>
              {tr(o.label, language)}
            </option>
          ))}
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
        {boolField('exec_2m_after_30m', COPY.exec2m)}
        {numField('ping_pong_min_gap_pts', COPY.pingGap)}
        {numField('ping_pong_candle_max_pts', COPY.pingCandle)}
        {numField('ping_pong_candle_lookback', COPY.pingLookback, true)}
        {numField('level_max_visits', COPY.visits, true)}
        {numField('day_gate_spent_pts', COPY.spent)}
        {numField('day_gate_target_cap_pts', COPY.cap)}
        {numField('swing_max_stop_pts', COPY.swing)}
        {enumField(
          '15m_ib_requires_5m_isb',
          COPY.ib15m,
          ENUM_OPTIONS['15m_ib_requires_5m_isb']
        )}
        {enumField(
          'tf_conflict_end',
          COPY.tfConflict,
          ENUM_OPTIONS.tf_conflict_end
        )}
        {boolField('news_wait_both_lines', COPY.newsWait)}
        {boolField('news_bars_excluded', COPY.newsBars)}
        {enumField(
          'isb_box_escape',
          COPY.isbEscape,
          ENUM_OPTIONS.isb_box_escape
        )}
        {enumField(
          'ema34_loss_relock',
          COPY.emaRelock,
          ENUM_OPTIONS.ema34_loss_relock
        )}
        {numField('orb_edge_veto_pts', COPY.orbEdge)}
        {enumField('d43_trend_source', COPY.d43, ENUM_OPTIONS.d43_trend_source)}
        {boolField('level_entries_need_trigger_side', COPY.lvlTrigger)}
        {boolField('trendline_is_target', COPY.tlTarget)}
        {boolField('trendline_between_boxes', COPY.tlBetween)}
        {boolField('leg_reset_on_wave_break', COPY.legWave)}
        {boolField('flag_wall_target', COPY.flagWall)}
        {boolField('box_new_zone_after_escape', COPY.boxNewZone)}
      </div>
    </div>
  )
}
