import type { MentorTuning } from '../../types'

import { EffectiveLine } from './EffectiveChip'
import type { EffectiveKnob } from '../../lib/api/strategyEffective'

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
  | 'orb_isb_fraction_outside'
  | 'isb_stack_cancel_candle_2m'
  | 'isb_stack_tiny_stop_pts'
  | 'evening_slow_pts'
  | 'evening_slow_from_hour'
  | 'evening_slow_window_min'
  | 'near_box_room_multiple'
  | 'phl_target_shy_pts'
  | 'isb_old_extreme_pts'
  | 'isb_in_range_gap_pts'

type BoolKey =
  | 'orb_gate_enabled'
  | 'isb_reverse_ema9_enabled'
  | 'htf_gate_news_only'
  | 'exec_2m_after_30m'
  | '15m_early_read'
  | 'trigger_escalation'
  | 'news_wait_both_lines'
  | 'news_bars_excluded'
  | 'level_entries_need_trigger_side'
  | 'trendline_is_target'
  | 'trendline_between_boxes'
  | 'leg_reset_on_wave_break'
  | 'flag_wall_target'
  | 'box_new_zone_after_escape'
  | 'orb_escape_completely'
  | 'wick_microscalp'
  | 'wick_safe_play'
  | 'near_box_room_phl'
  | 'near_box_between_exempt'
  | 'mentor_break_direction_lock'
  | 'mentor_small_range_guard'
  | 'mentor_mid_range_reject_side'

type EnumKey =
  | '15m_ib_requires_5m_isb'
  | 'mtf_confluence_source'
  | 'tf_conflict_end'
  | 'isb_box_escape'
  | 'ema34_loss_relock'
  | 'd43_trend_source'
  | 'trendline_entry_touch'
  | 'phl_flat_low'
  | 'isb_reverse_touch'
  | 'uno_key_levels'
  | 'session_open_levels'
  | 'level_arm_line_cancel'
  | 'phl_pullback_low'
  | 'mentor_5m_box_source'

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
  early15m: {
    zh: '15 分钟提前读取：形成中的 15 分钟内的 2 根 5 分钟 K 线在上一根 15 分钟内 → 临时 15 分钟 ISB 箱体（只拒绝）',
    en: 'Early 15m read: 2 five-minute candles of the forming 15m inside the last 15m → a provisional 15m ISB box (refusals only)',
    es: 'Lectura temprana de 15m: 2 velas de 5m del 15m en formación dentro del último 15m → caja ISB 15m provisional (solo rechazos)',
  },
  mtfSource: {
    zh: '模式 C（15m + 5m 共振）的读取来源',
    en: 'Mode C (15m + 5m confluence) reads',
    es: 'El modo C (confluencia 15m + 5m) lee',
  },
  mtfStanding: {
    zh: '仍然有效的 15m + 5m ISB 箱体',
    en: 'the standing 15m + 5m ISB boxes',
    es: 'las cajas ISB 15m + 5m vigentes',
  },
  mtfLastPair: {
    zh: '最近收盘的 5m 与 15m K 线对（#60 之前）',
    en: 'the latest closed 5m and 15m candle pairs (before #60)',
    es: 'los últimos pares de velas 5m y 15m cerradas (antes de #60)',
  },
  trigEscalation: {
    zh: '触发线升级（默认关，10 月课程只读 5 分钟）：5 分钟 ISB 交叉 → 15 分钟触发线，再交叉 → 30 分钟',
    en: 'Trigger escalation (default OFF, October reads the 5m only): crossing 5m ISBs → the 15m trigger line, crossing 15m → the 30m',
    es: 'Escalado del disparador (por defecto OFF, octubre lee solo el 5m): ISB de 5m cruzados → línea de 15m, 15m cruzados → 30m',
  },
  htfNews: {
    zh: '4h/1h 方向闸门仅限新闻 + 开盘窗口（开 = 默认；关 = 全天）',
    en: '4h/1h direction gate only in the news + open windows (ON = default, October: the 5m is the only big frame; OFF = all session)',
    es: 'Compuerta 4h/1h solo en las ventanas de noticias + apertura (ON = por defecto; OFF = toda la sesión)',
  },
  exec2m: {
    zh: '执行周期：09:00 CT 后为 2 分钟（08:30–09:00 为 1 分钟）— ISB + PHL/PLH、EMA 9/34、箱体、趋势线与 X8 离场',
    en: 'Execution frame: ON = 2m after 09:00 CT (1m 08:30–09:00) for ISB + PHL/PLH, EMA 9/34, boxes, trendlines and the X8 exit; OFF (default) = the 1m all day',
    es: 'Marco de ejecución: 2m después de las 09:00 CT (1m 08:30–09:00) para ISB + PHL/PLH, EMA 9/34, cajas, líneas de tendencia y la salida X8',
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
  isbEscapeClose5m: {
    zh: '5分钟收盘 — 1 分钟收盘即可交易，箱体仅在其自身周期收盘于箱外时删除（默认，m473）',
    en: 'close5m — a 1m close beyond the edge lets you trade; the box is deleted only when a candle of its own timeframe closes outside (default, m473)',
    es: 'cierre5m — un cierre de 1m fuera permite operar; la caja se borra solo cuando una vela de su propio marco cierra fuera (por defecto, m473)',
  },
  isbEscapeClose: {
    zh: '收盘 — 1 分钟收盘价越过边缘即删除箱体（#61–m472 默认）',
    en: 'close — the 1m close beyond the edge deletes the box (#61–m472 default)',
    es: 'cierre — el cierre de 1m fuera del borde borra la caja (por defecto #61–m472)',
  },
  box5mSource: {
    zh: '5 分钟 ISB 箱体是',
    en: 'The 5m ISB box is the',
    es: 'La caja ISB de 5m es la',
  },
  box5mMerge: {
    zh: '合并的内包线：收在箱体内的 5 分钟 K 线并入箱体（十月课程）',
    en: 'merged inside bar: 5m candles that close inside join the box (October course)',
    es: 'vela interior fusionada: las velas de 5m que cierran dentro se suman a la caja (curso de octubre)',
  },
  box5mNewest: {
    zh: '最新的 5 分钟内包线（替换旧箱体）',
    en: 'newest 5m inside bar (replaces the standing box, #61–#66)',
    es: 'vela interior de 5m más reciente (reemplaza la caja vigente)',
  },
  box5mFirst: {
    zh: '第一个箱体，直到突破（#61 之前）',
    en: 'first box, until it escapes (before #61)',
    es: 'primera caja, hasta que escape (antes de #61)',
  },
  breakLock: {
    zh: '突破后只做突破方向的形态，直到收盘回到箱体内',
    en: 'After a box break, only setups in the break direction until a close back inside',
    es: 'Tras romper una caja, solo setups en la dirección de la ruptura hasta un cierre de vuelta dentro',
  },
  smallRange: {
    zh: '区间太小（≤61.5 点且有两根 ≥30 点的 K 线）时区间内不入场（默认关闭）',
    en: 'No entry inside a range too small for its candles (≤ 61.50 pts with two ≥ 30-pt candles) — default OFF',
    es: 'Sin entrada dentro de un rango demasiado pequeño para sus velas (≤ 61,50 pts con dos velas ≥ 30 pts) — desactivado por defecto',
  },
  midRangeReject: {
    zh: '两个箱体之间：跟随最近一次箱体拒绝的方向',
    en: 'Between two boxes: trade WITH the last box rejection',
    es: 'Entre dos cajas: operar A FAVOR del último rechazo de caja',
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
  orbISBFrac: {
    zh: 'ORB 边缘 ISB 蜡烛在区间外的最小占比（0.01–1）',
    en: 'ORB-edge ISB candle fraction outside the range (0.01–1)',
    es: 'Fracción de la vela ISB fuera del rango ORB (0,01–1)',
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
  isbStack2m: {
    zh: '2分钟ISB：第几根K线仍未成交则取消（我们的数字，非课程）',
    en: '2m ISB: cancel on candle N if unfilled (ours, not the course)',
    es: 'ISB 2m: cancelar en la vela N si no se llena (nuestro número, no del curso)',
  },
  isbStackTiny: {
    zh: 'ISB：止损不超过此点数则不在取消K线取消（默认留空 = 0 = 关闭，10 月课程：第 4 根未成交 K 线即取消；12.5 = 9 月课程示例）',
    en: 'ISB: a stop at or under this many pts is not cancelled at the cancel candle (default blank = 0 = OFF, October: the 4th non-filling candle cancels; 12.5 = the September worked example)',
    es: 'ISB: un stop igual o menor a estos pts no se cancela en la vela de cancelación (por defecto vacío = 0 = OFF, octubre: la 4.ª vela sin ejecución cancela; 12.5 = el ejemplo del curso de septiembre)',
  },
  tlEntryTouch: {
    zh: '趋势线入场触碰（第 3 次触碰 = 测试）',
    en: 'Trendline entry touch (the 3rd touch = the test)',
    es: 'Toque de entrada en la línea de tendencia (el 3.er toque = la prueba)',
  },
  tlEntryFourth: {
    zh: '第 4 次触碰起入场（9 月课程，更严）',
    en: 'enter from the 4th touch (September course, stricter)',
    es: 'entrar desde el 4.º toque (curso de septiembre, más estricto)',
  },
  tlEntryThird: {
    zh: '第 3 次触碰即入场（10 月课程，默认）',
    en: 'enter on the 3rd touch (October course, default)',
    es: 'entrar en el 3.er toque (curso de octubre, por defecto)',
  },
  nearBoxBetween: {
    zh: '两个箱体之间免除"靠近箱体"（#60 之前；默认 关 = 按自身空间判断）',
    en: 'Exempt "between two boxes" from near-box (pre-#60; default OFF = judged on its own room)',
    es: 'Eximir "entre dos cajas" de la regla de caja cercana (antes de #60; por defecto OFF = según su propio espacio)',
  },
  phlFlat: {
    zh: 'PHL 平底读法',
    en: 'PHL flat-low read',
    es: 'Lectura de mínimo plano en PHL',
  },
  phlFlatNear: {
    zh: '平底 = FTGL，不做 PHL（课程）',
    en: 'a flat low is an FTGL, not a PHL (the course)',
    es: 'un mínimo plano es un FTGL, no un PHL (el curso)',
  },
  phlFlatStrict: {
    zh: '仅严格比较（#60 之前）',
    en: 'strict comparison only (pre-#60)',
    es: 'solo comparación estricta (antes de #60)',
  },
  isbRevTouch: {
    zh: '反向 ISB 的 EMA 9 触碰',
    en: 'Reverse ISB: the EMA 9 touch',
    es: 'ISB inverso: el toque de la EMA 9',
  },
  isbRevTouchPair: {
    zh: '母K线 + ISB 一组触及（课程）',
    en: 'the mother + ISB candle group reaches it (the course)',
    es: 'el grupo vela madre + ISB la alcanza (el curso)',
  },
  isbRevTouchIsb: {
    zh: '仅 ISB K线触及（#60 之前）',
    en: 'the ISB candle alone reaches it (pre-#60)',
    es: 'solo la vela ISB la alcanza (antes de #60)',
  },
  // m466 P2 (release #61) — levels and touches.
  unoKey: {
    zh: '关键位被突破后（Uno Reverse）',
    en: 'Key level that held, then broke (Uno Reverse)',
    es: 'Nivel clave que aguantó y luego se rompió (Uno Reverse)',
  },
  unoKeyOn: {
    zh: '角色翻转一次，回测拒绝可交易（课程）',
    en: 'the role flips once; the backtest reject trades (the course)',
    es: 'el rol cambia una vez; el rechazo del retest opera (el curso)',
  },
  unoKeyOff: {
    zh: '任何突破 = 当日仅 ISB（#61 之前）',
    en: 'any break = ISB-only for the day (pre-#61)',
    es: 'cualquier ruptura = solo ISB el resto del día (antes de #61)',
  },
  sessionOpen: {
    zh: '开盘K线影线 + 跳空边缘作为水平位',
    en: 'Session-open wick + gap edge as levels',
    es: 'Mecha de apertura + borde del gap como niveles',
  },
  sessionOpenOn: {
    zh: '开启（课程）',
    en: 'on (the course)',
    es: 'activado (el curso)',
  },
  sessionOpenOff: {
    zh: '关闭（#61 之前）',
    en: 'off (pre-#61)',
    es: 'desactivado (antes de #61)',
  },
  armLine: {
    zh: '趋势线 / EMA 34 挂单的取消读法',
    en: 'Trendline / EMA 34 resting order: cancel read',
    es: 'Orden en tendencia / EMA 34: lectura de cancelación',
  },
  armLineCurrent: {
    zh: '收盘穿过线的当前价格（课程）',
    en: 'a close through the line’s CURRENT price (the course)',
    es: 'un cierre a través del precio ACTUAL de la línea (el curso)',
  },
  armLinePlacement: {
    zh: '挂单时的价格（#61 之前）',
    en: 'the price at placement (pre-#61)',
    es: 'el precio al colocar la orden (antes de #61)',
  },
  phlPullback: {
    zh: 'PHL 更高低点的读法',
    en: 'PHL higher-low read',
    es: 'Lectura del mínimo más alto en PHL',
  },
  phlPullbackWhole: {
    zh: '整个回调的低点（课程）',
    en: 'the whole pullback’s low (the course)',
    es: 'el mínimo de todo el retroceso (el curso)',
  },
  phlPullbackSignal: {
    zh: '仅信号K线的低点（#61 之前）',
    en: 'the signal candle’s low only (pre-#61)',
    es: 'solo el mínimo de la vela de señal (antes de #61)',
  },
  eveSlowPts: {
    zh: '夜盘“慢”：高低点幅度低于此点数则用5分钟（我们的数字）',
    en: 'Evening "slow": night range under this many pts reads the 5m (ours, not the course)',
    es: 'Noche "lenta": rango bajo estos pts lee el 5m (nuestro número, no del curso)',
  },
  eveSlowFrom: {
    zh: '夜盘“慢”：起算小时（芝加哥时间，15 = 收盘，17 = 重开）',
    en: 'Evening "slow": range starts at this CT hour (15 = RTH close, 17 = reopen)',
    es: 'Noche "lenta": el rango empieza a esta hora CT (15 = cierre RTH, 17 = reapertura)',
  },
  eveSlowWindow: {
    zh: '夜盘“慢”：只看最近N分钟（0 = 从起算小时累计）',
    en: 'Evening "slow": only the last N minutes count (0 = cumulative since the start hour)',
    es: 'Noche "lenta": solo cuentan los últimos N minutos (0 = acumulado desde la hora de inicio)',
  },
  nearBoxRoom: {
    zh: '靠近箱体：最近箱体边距离须至少为 N × 风险（0 = 关；我们的数字）',
    en: 'Near a box: the nearest box edge must be at least N x risk away (0 = off; ours)',
    es: 'Cerca de una caja: el borde más cercano debe estar a al menos N x riesgo (0 = off; nuestro)',
  },
  nearBoxPhl: {
    zh: '靠近箱体的跳过规则同样适用于 PHL/PLH',
    en: 'The near-box skip also applies to PHL/PLH entries',
    es: 'La omisión por cercanía a la caja también aplica a entradas PHL/PLH',
  },
  phlShy: {
    zh: 'PHL/PLH 目标距旧高/低的点数（0 = 就在旧高/低；我们的数字）',
    en: 'PHL/PLH target sits this many pts short of the old high/low (0 = at it; ours)',
    es: 'El objetivo PHL/PLH queda a estos pts del máximo/mínimo previo (0 = en él; nuestro)',
  },
  isbOldExt: {
    zh: 'ISB 缩减仓位：K线触及旧高/低 ± 点数（我们的数字）',
    en: 'ISB size cut: candle reaches an old high/low within +/- pts (ours)',
    es: 'Recorte de tamaño ISB: la vela toca un máx/mín previo dentro de +/- pts (nuestro)',
  },
  isbInRange: {
    zh: 'ISB 缩减仓位：位于两个关键位之间且间距小于此点数（空 = 沿用乒乓最小间距）',
    en: 'ISB size cut: between two key levels closer than this many pts (blank = the ping-pong gap)',
    es: 'Recorte de tamaño ISB: entre dos niveles clave a menos de estos pts (vacío = la brecha ping-pong)',
  },
  wickMicroscalp: {
    zh: 'D4.3 影线微剥头皮（进阶）：5分钟同向影线区 + 1分钟 ISB/PHL，目标 = 附近5分钟影线',
    en: 'D4.3 wick microscalp (advanced): 5m same-way wick zone + 1m ISB/PHL, target = the nearby 5m wicks',
    es: 'D4.3 micro-scalp de mechas (avanzado): zona de mechas 5m + ISB/PHL 1m, objetivo = las mechas 5m cercanas',
  },
  wickSafePlay: {
    zh: 'D4.3 安全玩法：参考K线须收在区外（空单在箱下、多单在箱上）',
    en: 'D4.3 safe play: the reference candle must close outside the box (under it for a short, over it for a long)',
    es: 'D4.3 juego seguro: la vela de referencia debe cerrar fuera de la caja (debajo para corto, encima para largo)',
  },
  orbCompletely: {
    zh: '开盘区间：蜡烛（含影线）须完全在区间外才算突破',
    en: 'Opening range: the candle must be completely outside, wicks included',
    es: 'Rango de apertura: la vela debe estar completamente fuera, mechas incluidas',
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
  mtf_confluence_source: [
    { value: 'standing_boxes', label: COPY.mtfStanding },
    { value: 'last_pair', label: COPY.mtfLastPair },
  ],
  tf_conflict_end: [
    { value: 'first_box', label: COPY.tfConflictFirst },
    { value: 'both_boxes', label: COPY.tfConflictBoth },
  ],
  isb_box_escape: [
    { value: 'close5m', label: COPY.isbEscapeClose5m },
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
  trendline_entry_touch: [
    { value: 'fourth', label: COPY.tlEntryFourth },
    { value: 'third', label: COPY.tlEntryThird },
  ],
  phl_flat_low: [
    { value: 'flat_near', label: COPY.phlFlatNear },
    { value: 'strict', label: COPY.phlFlatStrict },
  ],
  isb_reverse_touch: [
    { value: 'pair', label: COPY.isbRevTouchPair },
    { value: 'isb_candle', label: COPY.isbRevTouchIsb },
  ],
  uno_key_levels: [
    { value: 'on', label: COPY.unoKeyOn },
    { value: 'off', label: COPY.unoKeyOff },
  ],
  session_open_levels: [
    { value: 'on', label: COPY.sessionOpenOn },
    { value: 'off', label: COPY.sessionOpenOff },
  ],
  level_arm_line_cancel: [
    { value: 'current_line', label: COPY.armLineCurrent },
    { value: 'placement_price', label: COPY.armLinePlacement },
  ],
  phl_pullback_low: [
    { value: 'pullback', label: COPY.phlPullbackWhole },
    { value: 'signal', label: COPY.phlPullbackSignal },
  ],
  mentor_5m_box_source: [
    { value: 'merge', label: COPY.box5mMerge },
    { value: 'newest', label: COPY.box5mNewest },
    { value: 'first', label: COPY.box5mFirst },
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
  htf_gate_news_only: true, // D2-G7 oct round 2: October — the 5m is the only big frame [OctD4.3-R9 @07:28]; the 4h/1h gate = news + open only; OFF = m462 all session (Sept D5.1-R9/R11)
  exec_2m_after_30m: false, // m472 A1 [Oct D4.2 @ 22:56-23:46]: the 1m all day
  day_gate_spent_pts: 265, // m465 (release #60) D5.1-R18: the worked example's −265 day was spent [D5.1 p1 @17:56–18:14]
  day_gate_target_cap_pts: 15,
  swing_max_stop_pts: 100,
  '15m_ib_requires_5m_isb': 'require_5m_isb', // (a)
  '15m_early_read': true, // m465 P1 — D4.2-R2/R11, D4.3-R21 (defensive: refusals only)
  mtf_confluence_source: 'standing_boxes', // m465 P1 — D4.2-R17
  trigger_escalation: false, // G3 oct round 2 — October: the 5m only [OctD4.3-R9 @07:28]; ON = Sept D3.4-R43 (m465 P1)
  tf_conflict_end: 'both_boxes', // (b) OQ-2 — course-defaults-30 [D4.2 p1 @07:54–08:02]
  news_wait_both_lines: true, // (b) OQ-6 — course-defaults-30 [D4.4 p1 @21:55–22:12]
  news_bars_excluded: true, // CE-4 — course-defaults-30 (release #29 rework: freeze the pre-print read)
  isb_box_escape: 'close5m', // m473 B3: a 1m close = permission, the box is deleted on a close of its own TF [Oct D4.3 @ 65:00-66:13]; was close (m466 G20/G49 [D4.1 p1 @20:36–21:03])
  ema34_loss_relock: 'until_zone_exit', // (a)
  orb_edge_veto_pts: 0, // (b) 0 = off
  orb_isb_fraction_outside: 0.8, // rule 5 (release #31) — default ON at 0.80
  d43_trend_source: 'structure', // (b) — course-defaults-30 [D4.3 @12:43–12:54]
  level_entries_need_trigger_side: false, // (b) owner ruled school 1
  trendline_is_target: true, // (a)
  trendline_between_boxes: false, // D3-G8 oct round 2 — October [OctD3.3-R59 @46:27]; was true (course-defaults-30)
  leg_reset_on_wave_break: true, // (a)
  flag_wall_target: true, // (b) X9 extra, ported from #416 — course-defaults-30 [X9 @05:59–06:12]
  box_new_zone_after_escape: true, // (a) B52: new zone after range escape [D3.4 p3 recovered2 @00:00–00:21]
  orb_escape_completely: false, // (b) release #52 B19: wick-inclusive ORB escape — body test until the owner rules
  wick_microscalp: false, // m465 P5: D4.3 advanced — "đừng có tập khúc này đầu liền" [D4.3 @04:55–05:17]
  wick_safe_play: false, // m465 P5: D4.3-R20 an optional extra confirm [D4.3 @14:07–14:27]
  // Release #52 W11 — invented constants, every one OURS (defaults unchanged).
  isb_stack_cancel_candle_2m: 4, // B8: the 1m course count reused on the 2m
  isb_stack_tiny_stop_pts: 0, // G2 oct round 2: OFF — October cancels at the 4th candle [OctD4.3-R57 @56:00]; 12.5 = Sept D5.1-R26 [D5.1 p2 @02:24–04:41]
  evening_slow_pts: 100, // B10
  evening_slow_from_hour: 15, // B10: the RTH close
  evening_slow_window_min: 0, // B10: 0 = cumulative since the start hour
  near_box_room_multiple: 2, // B11
  near_box_room_phl: true, // B11: the skip also gates PHL/PLH
  phl_target_shy_pts: 5, // C16
  isb_old_extreme_pts: 2, // B27
  isb_in_range_gap_pts: 0, // B27: 0 = inherit ping_pong_min_gap_pts
  // m465 P3 (release #60) — structure fidelity, the course from the full video read.
  trendline_entry_touch: 'third', // m472 A2 [Oct D3.2 @ 31:00-31:17] "chỉ cần 2 touch" (supersedes Sept D2.3-R7 'fourth')
  near_box_between_exempt: false, // D3.2-R29/R30 [D3.2 p1 @22:12–24:41]
  phl_flat_low: 'flat_near', // D2.2-R64 [D2.2 p3 @07:44–10:15]
  isb_reverse_touch: 'pair', // D5.4-R7 [D5.4 p1 @03:35–03:46]
  // m466 P2 (release #61) — levels and touches, the course.
  uno_key_levels: 'on', // G2 D1.3-R21 [D1.3 p1 @12:31–12:58]
  session_open_levels: 'on', // G10/G11 D3.1-R26/R20 [D3.1 p1 @09:41–12:22]
  level_arm_line_cancel: 'current_line', // G17 D2.3-R34 [D2.3 p1 @23:35–24:03]
  phl_pullback_low: 'pullback', // G41 D2.2-R59 [D2.2 p3 @08:25–09:25]
  // m466 P1 (release #61) — boxes, breaks, ranges.
  mentor_5m_box_source: 'merge', // ISB5-F1 OctD4.3-R59 [Oct D4.3 @ 58:00]
  mentor_break_direction_lock: true, // G21 D4.1-R31/R33 [D4.1 p1 @18:29–20:33]
  mentor_small_range_guard: false, // G33 D4.2-R33 [D4.2 p2 @06:16–06:30] — OFF since #61 final (count / margin unstated)
  mentor_mid_range_reject_side: true, // G43 D2.2-R71 [D2.2 p3 @14:28–15:58]
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
  // Effective rows for risk_control.mentor_tuning.<key> (prefix applied by the
  // caller). Optional: absent → no chips.
  eff?: (leaf: string) => EffectiveKnob | null | undefined
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
  eff,
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
      <EffectiveLine knob={eff?.(`mentor_tuning.${key}`)} />
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
        <EffectiveLine knob={eff?.(`mentor_tuning.${key}`)} />
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
        <EffectiveLine knob={eff?.(`mentor_tuning.${key}`)} />
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
          <EffectiveLine knob={eff?.('mentor_tuning.trigger_school')} />
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
        {boolField('15m_early_read', COPY.early15m)}
        {enumField(
          'mtf_confluence_source',
          COPY.mtfSource,
          ENUM_OPTIONS.mtf_confluence_source
        )}
        {boolField('trigger_escalation', COPY.trigEscalation)}
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
        {numField('orb_isb_fraction_outside', COPY.orbISBFrac)}
        {enumField('d43_trend_source', COPY.d43, ENUM_OPTIONS.d43_trend_source)}
        {boolField('level_entries_need_trigger_side', COPY.lvlTrigger)}
        {boolField('trendline_is_target', COPY.tlTarget)}
        {boolField('trendline_between_boxes', COPY.tlBetween)}
        {boolField('leg_reset_on_wave_break', COPY.legWave)}
        {boolField('flag_wall_target', COPY.flagWall)}
        {boolField('box_new_zone_after_escape', COPY.boxNewZone)}
        {boolField('orb_escape_completely', COPY.orbCompletely)}
        {boolField('wick_microscalp', COPY.wickMicroscalp)}
        {boolField('wick_safe_play', COPY.wickSafePlay)}
        {numField('isb_stack_cancel_candle_2m', COPY.isbStack2m, true)}
        {numField('isb_stack_tiny_stop_pts', COPY.isbStackTiny)}
        {numField('evening_slow_pts', COPY.eveSlowPts)}
        {numField('evening_slow_from_hour', COPY.eveSlowFrom, true)}
        {numField('evening_slow_window_min', COPY.eveSlowWindow, true)}
        {numField('near_box_room_multiple', COPY.nearBoxRoom)}
        {boolField('near_box_room_phl', COPY.nearBoxPhl)}
        {numField('phl_target_shy_pts', COPY.phlShy)}
        {numField('isb_old_extreme_pts', COPY.isbOldExt)}
        {numField('isb_in_range_gap_pts', COPY.isbInRange)}
        {enumField(
          'trendline_entry_touch',
          COPY.tlEntryTouch,
          ENUM_OPTIONS.trendline_entry_touch
        )}
        {boolField('near_box_between_exempt', COPY.nearBoxBetween)}
        {enumField('phl_flat_low', COPY.phlFlat, ENUM_OPTIONS.phl_flat_low)}
        {enumField(
          'isb_reverse_touch',
          COPY.isbRevTouch,
          ENUM_OPTIONS.isb_reverse_touch
        )}
        {enumField('uno_key_levels', COPY.unoKey, ENUM_OPTIONS.uno_key_levels)}
        {enumField(
          'session_open_levels',
          COPY.sessionOpen,
          ENUM_OPTIONS.session_open_levels
        )}
        {enumField(
          'level_arm_line_cancel',
          COPY.armLine,
          ENUM_OPTIONS.level_arm_line_cancel
        )}
        {enumField(
          'phl_pullback_low',
          COPY.phlPullback,
          ENUM_OPTIONS.phl_pullback_low
        )}
        {enumField(
          'mentor_5m_box_source',
          COPY.box5mSource,
          ENUM_OPTIONS.mentor_5m_box_source
        )}
        {boolField('mentor_break_direction_lock', COPY.breakLock)}
        {boolField('mentor_small_range_guard', COPY.smallRange)}
        {boolField('mentor_mid_range_reject_side', COPY.midRangeReject)}
      </div>
    </div>
  )
}
