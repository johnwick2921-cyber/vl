// P4 — Day-Plan / Plan Card i18n (self-contained feature module, mirrors the
// strategy-translations.ts pattern). Every entry carries all three languages
// (en / zh / id) — the TranslationEntry type makes a missing language a compile
// error. Controlled-vocab instruction verbs stay English in stored values;
// only the UI labels here translate (design-system Open Question #4).

import type { Language } from './translations'

interface TranslationEntry {
  en: string
  zh: string
  id: string
}

type PlanKey = keyof typeof planStrings

export const planStrings = {
  // ── header ──
  title: { en: "Today's Plan", zh: '今日计划', id: 'Rencana Hari Ini' },
  edit: { en: 'Edit', zh: '编辑', id: 'Ubah' },
  reread: { en: 'Re-read', zh: '重新解读', id: 'Baca ulang' },
  approve: { en: 'Approve', zh: '批准', id: 'Setujui' },
  askPlanner: { en: 'Ask planner', zh: '询问规划师', id: 'Tanya perencana' },

  // ── lifecycle chips ──
  lifecycleActive: { en: 'ACTIVE', zh: '生效中', id: 'AKTIF' },
  lifecycleExpired: { en: 'EXPIRED', zh: '已过期', id: 'KEDALUWARSA' },
  lifecycleDied: { en: 'DIED', zh: '已失效', id: 'MATI' },
  lifecycleSuperseded: { en: 'SUPERSEDED', zh: '已替换', id: 'DIGANTI' },
  lifecycleNoTrade: { en: 'NO-TRADE', zh: '禁止交易', id: 'TANPA TRADE' },
  lifecycleDormant: { en: '⏸ DORMANT', zh: '⏸ 休眠', id: '⏸ DORMAN' },

  // ── card states ──
  noPlanYet: { en: 'No plan yet', zh: '尚无计划', id: 'Belum ada rencana' },
  noPlanYetHint: {
    en: 'The planner arms at the session open.',
    zh: '规划师将在时段开盘时生成计划。',
    id: 'Perencana aktif saat sesi dibuka.',
  },
  night: {
    en: 'Night — markets quiet',
    zh: '夜间 — 市场清淡',
    id: 'Malam — pasar sepi',
  },
  nightHint: {
    en: 'No active session. The plan resumes at the next session open.',
    zh: '当前无活跃时段，计划将于下一时段开盘恢复。',
    id: 'Tidak ada sesi aktif. Rencana lanjut pada sesi berikutnya.',
  },
  disabled: {
    en: 'Day Plan off',
    zh: '日计划已关闭',
    id: 'Rencana Harian nonaktif',
  },
  disabledHint: {
    en: 'Enable Day Plan in Strategy Studio.',
    zh: '在策略工作室中启用日计划。',
    id: 'Aktifkan Rencana Harian di Strategy Studio.',
  },
  errorFailClosed: {
    en: 'Plan read failed — sitting out',
    zh: '计划解读失败 — 本时段观望',
    id: 'Gagal baca rencana — libur sesi ini',
  },
  errorHint: {
    en: 'Fail-closed: no valid plan this session.',
    zh: '安全失败：本时段无有效计划。',
    id: 'Fail-closed: tidak ada rencana valid sesi ini.',
  },
  reading: {
    en: 'Reading the tape…',
    zh: '正在解读盘面…',
    id: 'Membaca pita…',
  },
  loading: { en: 'Loading plan…', zh: '加载计划中…', id: 'Memuat rencana…' },

  // ── bias ──
  bias: { en: 'Bias', zh: '倾向', id: 'Bias' },
  biasLong: { en: 'LONG', zh: '做多', id: 'LONG' },
  biasShort: { en: 'SHORT', zh: '做空', id: 'SHORT' },
  biasNeutral: { en: 'NEUTRAL', zh: '中性', id: 'NETRAL' },
  conviction: { en: 'conviction', zh: '信心', id: 'keyakinan' },
  flipPrefix: { en: 'Flips', zh: '反转条件', id: 'Berbalik' },

  // ── badges / banners ──
  warming: { en: 'WARMING {n}', zh: '预热中 {n}', id: 'PEMANASAN {n}' },
  uncalibrated: { en: 'UNCALIBRATED', zh: '未校准', id: 'BELUM DIKALIBRASI' },
  advisoryMode: {
    en: 'Advisory mode — the plan informs, the AI still decides.',
    zh: '顾问模式 — 计划仅供参考，仍由 AI 决策。',
    id: 'Mode saran — rencana memberi info, AI tetap memutuskan.',
  },
  rereadsLeft: {
    en: '{n} re-reads left',
    zh: '剩余 {n} 次重读',
    id: '{n} baca ulang tersisa',
  },

  // ── levels ──
  keyLevels: { en: 'Key Levels', zh: '关键价位', id: 'Level Kunci' },
  colPrice: { en: 'Price', zh: '价格', id: 'Harga' },
  colProvenance: { en: 'Source', zh: '来源', id: 'Sumber' },
  colGrade: { en: 'Grade', zh: '评级', id: 'Nilai' },
  colFresh: { en: 'State', zh: '状态', id: 'Status' },
  colInstruction: { en: 'Instruction', zh: '指令', id: 'Instruksi' },
  colDistance: { en: 'Dist', zh: '距离', id: 'Jarak' },
  freshFresh: { en: 'fresh', zh: '新鲜', id: 'segar' },
  freshTested: { en: 'tested', zh: '已测试', id: 'teruji' },
  freshConsumed: { en: 'consumed', zh: '已消耗', id: 'terpakai' },
  ownerLevel: { en: 'Owner level', zh: '所有者价位', id: 'Level pemilik' },
  hasNote: {
    en: 'Has a note for the AI',
    zh: '含给 AI 的备注',
    id: 'Ada catatan untuk AI',
  },
  noLevels: {
    en: 'No levels in this plan',
    zh: '此计划无价位',
    id: 'Tidak ada level di rencana ini',
  },
  // A levels-less plan must always say WHY. Silence here is what let a
  // born-dead plan loop look like a rendering bug for a whole session.
  noLevelsWhy: { en: 'Why', zh: '原因', id: 'Alasan' },
  noLevelsUnknown: {
    en: 'The planner returned zero levels and gave no reason — this is a fault, not a quiet session.',
    zh: '规划器返回了零个价位且未说明原因 — 这是故障，而非平静时段。',
    id: 'Planner mengembalikan nol level tanpa alasan — ini kesalahan, bukan sesi sepi.',
  },
  // ITEM 15 — version history.
  historicalTitle: {
    en: 'HISTORICAL VERSION — read only',
    zh: '历史版本 — 只读',
    id: 'VERSI HISTORIS — hanya baca',
  },
  historicalHint: {
    en: 'This is the plan as it was. It is not the plan the bot is trading.',
    zh: '这是当时的计划，并非机器人正在交易的计划。',
    id: 'Ini rencana sebagaimana adanya dulu, bukan yang sedang dijalankan bot.',
  },
  backToActive: {
    en: 'Back to current',
    zh: '返回当前',
    id: 'Kembali ke saat ini',
  },
  whyItDied: {
    en: 'Why this version ended',
    zh: '此版本结束的原因',
    id: 'Mengapa versi ini berakhir',
  },
  whatChanged: {
    en: 'What changed next',
    zh: '之后的变化',
    id: 'Apa yang berubah',
  },
  writtenAt: { en: 'Written', zh: '写入时间', id: 'Ditulis' },
  supersededBy: {
    en: 'Replaced by v{n}',
    zh: '被 v{n} 取代',
    id: 'Digantikan v{n}',
  },
  replansLeftLabel: {
    en: 'Re-plans left this session',
    zh: '本时段剩余重规划',
    id: 'Sisa rencana ulang sesi ini',
  },
  uncarriedTitle: {
    en: '{n} edit(s) could not carry into this version — review',
    zh: '{n} 处修改无法沿用到此版本 — 请复核',
    id: '{n} suntingan tidak bisa dibawa ke versi ini — tinjau',
  },
  alertDismiss: { en: 'Remove', zh: '移除', id: 'Hapus' },
  alertClearRead: { en: 'Clear read', zh: '清除已读', id: 'Bersihkan terbaca' },
  alertP0NeedsAck: {
    en: 'Acknowledge this P0 first — a halt cannot be dismissed unseen.',
    zh: '请先确认此 P0 — 停止事件不能未读即移除。',
    id: 'Akui P0 ini dulu — penghentian tidak bisa dibuang tanpa dilihat.',
  },
  alertCleared: {
    en: '{n} cleared',
    zh: '已清除 {n} 条',
    id: '{n} dibersihkan',
  },
  rereadTitle: { en: 'Re-read', zh: '重新解读', id: 'Baca ulang' },
  rereadConfirm: {
    en: 'Spend one of {left} re-reads?',
    zh: '消耗 {left} 次重读中的一次？',
    id: 'Pakai satu dari {left} baca ulang?',
  },
  rereadConfirmHint: {
    en: 'This calls the planner again and writes a new version. It costs an API call and one re-read from this session budget.',
    zh: '这将再次调用规划师并写入新版本，消耗一次 API 调用和本时段的一次重读额度。',
    id: 'Ini memanggil perencana lagi dan menulis versi baru — biaya satu panggilan API dan satu jatah baca ulang sesi ini.',
  },
  rereadGo: { en: 'Yes, re-read', zh: '确认重读', id: 'Ya, baca ulang' },
  rereadCancel: { en: 'Cancel', zh: '取消', id: 'Batal' },
  rereadRunning: { en: 'Re-reading…', zh: '正在重读…', id: 'Membaca ulang…' },
  rereadDone: {
    en: 'Re-read requested',
    zh: '已请求重读',
    id: 'Baca ulang diminta',
  },
  // P6 — the owner reset (distinct from re-read: abandons the chain + restores
  // the full budget).
  resetTitle: { en: 'Reset planner', zh: '重置规划器', id: 'Reset perencana' },
  resetConfirm: {
    en: 'Abandon this plan chain and start fresh?',
    zh: '放弃当前计划链并重新开始？',
    id: 'Buang rantai rencana ini dan mulai ulang?',
  },
  resetConfirmHint: {
    en: 'The current chain is marked ABANDONED (history and death reasons are preserved). The full re-plan budget is restored and a fresh plan is read now. Open positions and their brackets are never touched.',
    zh: '当前计划链将被标记为已放弃（历史与死亡原因保留）。重读额度完全恢复并立即重新读取计划。持仓及其止盈止损不受影响。',
    id: 'Rantai saat ini ditandai DIBUANG (riwayat dan alasan kematian dipertahankan). Jatah baca ulang dipulihkan penuh dan rencana baru dibaca sekarang. Posisi terbuka dan bracketnya tidak pernah disentuh.',
  },
  resetGo: { en: 'Yes, reset', zh: '确认重置', id: 'Ya, reset' },
  resetRunning: { en: 'Resetting…', zh: '正在重置…', id: 'Mereset…' },
  resetDone: { en: 'Reset requested', zh: '已请求重置', id: 'Reset diminta' },
  readingBanner: {
    en: 'The planner is writing a fresh plan — this card updates in a minute or two.',
    zh: '规划器正在生成新计划 —— 卡片将在一两分钟内更新。',
    id: 'Planner sedang menulis rencana baru — kartu akan diperbarui dalam satu-dua menit.',
  },
  replanChip: {
    en: 'Planner re-reading… this plan stays live.',
    zh: '规划器正在重读…… 当前计划保持有效。',
    id: 'Planner membaca ulang… rencana ini tetap aktif.',
  },
  resetCaption: {
    en: 'Abandons the chain · restores the full budget · new plan',
    zh: '放弃计划链 · 恢复全部额度 · 新计划',
    id: 'Buang rantai · pulihkan jatah penuh · rencana baru',
  },
  rereadCaption: {
    en: 'One more plan on the same chain · spends budget',
    zh: '同一计划链再读一次 · 消耗额度',
    id: 'Satu rencana lagi di rantai yang sama · pakai jatah',
  },
  approveCaption: {
    en: 'Grant entries for this session-day',
    zh: '授权本交易日的开仓',
    id: 'Izinkan entri untuk hari sesi ini',
  },
  approveDone: {
    en: 'Plan approved — entries flow for this CME session-day',
    zh: '计划已批准 — 本 CME 交易日允许开仓',
    id: 'Rencana disetujui — entri diizinkan untuk hari sesi CME ini',
  },
  approveFailed: {
    en: 'Approve failed — see server logs',
    zh: '批准失败 — 请查看服务端日志',
    id: 'Persetujuan gagal — lihat log server',
  },
  askAnyway: { en: 'Ask the planner', zh: '询问规划师', id: 'Tanya perencana' },
  askContextHistorical: {
    en: 'HISTORICAL CONTEXT — answering about the last stored plan, not a live one',
    zh: '历史上下文 — 回答的是最近存储的计划，而非实时计划',
    id: 'KONTEKS HISTORIS — menjawab tentang rencana tersimpan terakhir, bukan yang aktif',
  },
  askContextNoPlan: {
    en: 'NO PLAN — answering from live market facts only',
    zh: '无计划 — 仅根据实时市场数据回答',
    id: 'TANPA RENCANA — menjawab dari fakta pasar saja',
  },
  noTradeChip: { en: 'NO-TRADE', zh: '禁止交易', id: 'TANPA TRADE' },
  noTradeBanner: {
    en: 'NO-TRADE — re-read budget exhausted',
    zh: '禁止交易 — 重读次数已用尽',
    id: 'TANPA TRADE — jatah baca ulang habis',
  },
  noTradeBannerHint: {
    en: 'This is not a plan. It is the marker written after {used} of {cap} re-plans were spent and the last one died — the session sits out.',
    zh: '这不是计划，而是在用尽 {cap} 次重读中的 {used} 次且最后一版失效后写入的标记 — 本时段观望。',
    id: 'Ini bukan rencana, melainkan penanda setelah {used} dari {cap} baca ulang terpakai dan yang terakhir mati — sesi ini libur.',
  },
  deathHistory: {
    en: 'This session already lost {n} plan(s)',
    zh: '本时段已有 {n} 个计划失效',
    id: 'Sesi ini sudah kehilangan {n} rencana',
  },
  deathHistoryHint: {
    en: 'Tap a version to read it as it was.',
    zh: '点击版本可查看当时的计划。',
    id: 'Ketuk versi untuk membacanya seperti dulu.',
  },
  chartNoLevels: {
    en: 'Bars only — this plan has no levels to overlay',
    zh: '仅K线 — 此计划无价位可叠加',
    id: 'Hanya bar — rencana ini tanpa level untuk ditumpuk',
  },

  // ── scenarios ──
  scenarios: { en: 'Scenarios', zh: '情景', id: 'Skenario' },
  scenarioActivation: {
    en: 'scenario activation',
    zh: '情景激活状态',
    id: 'aktivasi skenario',
  },
  scenarioOrderSeparation: {
    en: 'Scenario activation and confirmation do not mean an order is authorized or working. The order chip shows the separate ledger state.',
    zh: '情景激活与确认不代表订单已授权或已挂单。订单标签单独显示账本状态。',
    id: 'Aktivasi dan konfirmasi skenario tidak berarti order diotorisasi atau aktif. Label order menunjukkan status ledger terpisah.',
  },
  statusArmed: {
    en: 'in activation window',
    zh: '处于激活区间',
    id: 'dalam jendela aktivasi',
  },
  statusWaiting: { en: 'waiting', zh: '等待中', id: 'menunggu' },
  statusTriggered: { en: 'triggered', zh: '已触发', id: 'terpicu' },
  statusInvalidated: { en: 'invalidated', zh: '已失效', id: 'batal' },
  statusExpired: { en: 'expired', zh: '已过期', id: 'kedaluwarsa' },
  targets: { en: 'targets', zh: '目标', id: 'target' },
  invalidates: { en: 'invalid', zh: '失效', id: 'batal' },

  // ── rules ──
  noTrade: { en: 'No-trade', zh: '禁止交易', id: 'Larangan trade' },
  planDies: { en: 'Plan dies if', zh: '计划失效条件', id: 'Rencana mati jika' },
  // invalidation-wired F2 (2026-09-03): a position's provenance vs the plan on
  // screen — both stated, armed-under first.
  armedUnder: {
    en: 'Position armed under',
    zh: '持仓挂单版本',
    id: 'Posisi dipasang di',
  },
  planNow: { en: 'Plan now', zh: '当前计划', id: 'Rencana kini' },
  // no-trade band (2026-09-02): the machine's windows vs the model's prose
  noTradeSpent: {
    en: 'spent / other session',
    zh: '已过 / 其他时段',
    id: 'lewat / sesi lain',
  },
  noTradeNoneLive: {
    en: 'none live now',
    zh: '当前无限制',
    id: 'tidak ada saat ini',
  },
  modelNotes: { en: 'Model notes', zh: '模型备注', id: 'Catatan model' },
  bandElapsed: { en: 'spent', zh: '已过', id: 'lewat' },
  bandOtherSession: {
    en: 'other session',
    zh: '其他时段',
    id: 'sesi lain',
  },

  // ── footer ──
  dayType: { en: 'Day type', zh: '日型', id: 'Tipe hari' },
  model: { en: 'Model', zh: '模型', id: 'Model' },

  // ── sessions / timeline ──
  sessionAsia: { en: 'Asia', zh: '亚洲', id: 'Asia' },
  sessionLondon: { en: 'London', zh: '伦敦', id: 'London' },
  sessionNY: { en: 'New York', zh: '纽约', id: 'New York' },
  now: { en: 'now', zh: '当前', id: 'sekarang' },
  killzone: { en: 'killzone', zh: '狙击时段', id: 'killzone' },
  dstWarning: {
    en: 'DST shift — London window may be off by 1h',
    zh: '夏令时切换 — 伦敦时段可能偏差 1 小时',
    id: 'Pergeseran DST — jendela London bisa meleset 1 jam',
  },
  tabReading: { en: 'reading', zh: '解读中', id: 'membaca' },
  tabNight: { en: 'night', zh: '夜间', id: 'malam' },

  // ── handover banner ──
  handoverExpired: {
    en: '{session} plan expired',
    zh: '{session} 计划已过期',
    id: 'Rencana {session} kedaluwarsa',
  },
  handoverReading: {
    en: 'Reading {session}…',
    zh: '正在解读 {session}…',
    id: 'Membaca {session}…',
  },
  handoverBorn: {
    en: '{session} plan is live',
    zh: '{session} 计划已生效',
    id: 'Rencana {session} aktif',
  },
  handoverReadFailed: {
    en: '{session} read failed — sitting out',
    zh: '{session} 解读失败 — 观望',
    id: 'Baca {session} gagal — libur',
  },

  // ── alert center ──
  alerts: { en: 'Alerts', zh: '提醒', id: 'Peringatan' },
  noAlerts: { en: 'No alerts', zh: '暂无提醒', id: 'Tidak ada peringatan' },
  markRead: { en: 'Mark read', zh: '标记已读', id: 'Tandai dibaca' },
  markAllRead: {
    en: 'Mark all read',
    zh: '全部标记已读',
    id: 'Tandai semua dibaca',
  },
  unread: { en: '{n} unread', zh: '{n} 条未读', id: '{n} belum dibaca' },
  bellLabel: {
    en: 'Alerts, {n} unread',
    zh: '提醒，{n} 条未读',
    id: 'Peringatan, {n} belum dibaca',
  },
  dismiss: { en: 'Dismiss', zh: '关闭', id: 'Tutup' },

  // ── Studio Day Plan block ──
  dayPlanBlock: { en: 'Day Plan', zh: '日计划', id: 'Rencana Harian' },
  enableDayPlan: {
    en: 'Enable Day Plan',
    zh: '启用日计划',
    id: 'Aktifkan Rencana Harian',
  },
  plannerModel: { en: 'Planner model', zh: '规划模型', id: 'Model perencana' },
  planMode: { en: 'Plan mode', zh: '计划模式', id: 'Mode rencana' },
  modeAdvisory: { en: 'ADVISORY', zh: '顾问', id: 'SARAN' },
  modeDirection: { en: 'DIRECTION', zh: '定向', id: 'ARAH' },
  modeStrict: { en: 'STRICT', zh: '严格', id: 'KETAT' },
  plannerReads: {
    en: 'Planner reads',
    zh: '规划师读取',
    id: 'Perencana membaca',
  },
  autoLabel: { en: 'AUTO', zh: '自动', id: 'OTOMATIS' },
  autoTooltip: {
    en: 'auto-computed — not a setting',
    zh: '自动计算 — 非可配置项',
    id: 'dihitung otomatis — bukan pengaturan',
  },
  regime: { en: 'Regime', zh: '市况', id: 'Rezim' },
  filters: { en: 'Filters', zh: '筛选', id: 'Filter' },
  proximity: { en: 'Proximity', zh: '邻近度', id: 'Kedekatan' },
  maxLevels: { en: 'Max levels', zh: '最大价位数', id: 'Maks level' },
  htfSeats: {
    en: 'HTF seats (structure-first, S3)',
    zh: 'HTF 席位 (结构优先, S3)',
    id: 'Kursi HTF',
  },
  htfScoreMultiplier: {
    en: 'HTF score multiplier (S3)',
    zh: 'HTF 分数乘数 (S3)',
    id: 'Pengali skor HTF',
  },
  structureMap: {
    en: 'Structure map — D/4h/1h trend table + zones on the card; context only, never entries',
    zh: '结构地图 — D/4h/1h 趋势表 + 区域；仅上下文，永不作为入场',
    id: 'Peta struktur — tabel tren D/4h/1h + zona; konteks saja, bukan entri',
  },
  freshByTf: {
    en: 'Freshness by timeframe — HTF levels graded on their own bars (re-entries), research says no edge either way',
    zh: '按周期新鲜度 — HTF 价位按自身周期 K 线评分（重入）；研究显示两种方式均无优势',
    id: 'Kesegaran per TF — level HTF dinilai pada barnya sendiri (re-entries); riset: tanpa edge',
  },
  flipReread: {
    en: 'Flip re-read — when the flip condition fires, the plan still goes dormant, then ONE free re-read authors the flipped direction',
    zh: '翻转重读 — 触发翻转条件时计划仍转入休眠，然后一次免费重读按翻转方向重新制定',
    id: 'Flip re-read — saat kondisi flip terpicu, plan tetap dormant, lalu SATU re-read gratis menyusun arah yang baru',
  },
  maxScenarios: { en: 'Max scenarios', zh: '最大情景数', id: 'Maks skenario' },
  maxReplans: {
    en: 'Max re-plans',
    zh: '最大重规划数',
    id: 'Maks rencana ulang',
  },
  minGrade: { en: 'Min grade', zh: '最低评级', id: 'Nilai min' },
  maxTrades: { en: 'Max trades', zh: '最大交易数', id: 'Maks trade' },
  acceptance: { en: 'Acceptance', zh: '接受确认', id: 'Penerimaan' },
  approval: {
    en: 'Approval required',
    zh: '需要批准',
    id: 'Perlu persetujuan',
  },
  digest: { en: 'Digest', zh: '摘要', id: 'Ringkasan' },
  realignCap: {
    en: 'Max re-alignments',
    zh: '最大重新校准次数',
    id: 'Maks penyelarasan ulang',
  },
  // W6 (2026-08-25) — planner wake-up knobs (level events).
  wakeHeader: {
    en: 'Planner wake-ups',
    zh: '规划师唤醒',
    id: 'Bangun perencana',
  },
  wakeOn15mZone: {
    en: 'Wake on 15m zones',
    zh: '15m 供需区唤醒',
    id: 'Bangun di zona 15m',
  },
  wakeOnHTFZone: {
    en: 'Wake on HTF zones (1h/4h)',
    zh: 'HTF 供需区唤醒 (1h/4h)',
    id: 'Bangun di zona HTF (1j/4j)',
  },
  wakeOnHTFOB: {
    en: 'Wake on HTF order blocks',
    zh: 'HTF 订单块唤醒',
    id: 'Bangun di order block HTF',
  },
  wakeOnSeatedInvalidation: {
    en: 'Wake on seated-level invalidation',
    zh: '坐稳位失效唤醒',
    id: 'Bangun saat invalidasi level',
  },
  wakeOnIFVG: {
    en: 'Wake on iFVG inversion',
    zh: 'iFVG 反转唤醒',
    id: 'Bangun saat iFVG',
  },
  wakeMinInterval: {
    en: 'Min wake interval (min)',
    zh: '唤醒最小间隔（分钟）',
    id: 'Interval bangun min (mnt)',
  },
  seat1HZone: {
    en: 'Guarantee a 1h S/D seat',
    zh: '保证 1h 供需区座位',
    id: 'Jamin kursi zona 1j',
  },
  minScenarioQuality: {
    en: 'Min scenario quality',
    zh: '最低场景质量',
    id: 'Kualitas skenario min',
  },
  oneSetup: {
    en: 'One setup — arm only the single best level',
    zh: '单一设置 — 仅对最佳价位挂单',
    id: 'One setup — pasang hanya level terbaik',
  },
  oneSetupMinGrade: {
    en: 'One setup min grade',
    zh: '单一设置最低等级',
    id: 'Grade min one setup',
  },

  // W15.C follow-up — the owner door only opens on the LIVE session, because every
  // mutating endpoint resolves the active session server-side.
  siblingReadOnly: {
    en: 'Viewing another session — read-only. Switch to the live session to edit.',
    zh: '正在查看其他时段——只读。切换到当前时段才能编辑。',
    id: 'Melihat sesi lain — hanya-baca. Beralih ke sesi aktif untuk mengubah.',
  },
  // W16/R2 — declining is its own recorded outcome, never "applied"
  askDeclined: {
    en: 'Proposal declined — plan unchanged',
    zh: '已拒绝提议——计划未改动',
    id: 'Usulan ditolak — rencana tidak berubah',
  },
  askDeclinedNote: {
    en: 'You kept the plan as it was. The decision was recorded; nothing was changed.',
    zh: '你保留了原计划。该决定已记录，未做任何改动。',
    id: 'Anda mempertahankan rencana. Keputusan dicatat; tidak ada yang diubah.',
  },
  // W16/R3 — refusals, made visible
  gateBlocksHeader: {
    en: 'Refused this session',
    zh: '本时段被拒绝',
    id: 'Ditolak sesi ini',
  },
  gateBlocksNone: {
    en: 'No entries refused so far this session.',
    zh: '本时段暂无入场被拒绝。',
    id: 'Belum ada entri yang ditolak sesi ini.',
  },
  gateBlocksNote: {
    en: 'Counts reset at the 17:00 CT session roll and on restart. Per-decision reasons appear on each decision row.',
    zh: '计数在 17:00 CT 时段切换及重启时重置。每条决策的原因显示在对应决策行。',
    id: 'Hitungan direset pada pergantian sesi 17:00 CT dan saat restart. Alasan tiap keputusan ada di barisnya.',
  },
  sessionsHeader: { en: 'Sessions', zh: '交易时段', id: 'Sesi' },
  inherit: { en: 'inherit', zh: '继承', id: 'warisi' },
  override: { en: 'override', zh: '覆盖', id: 'ganti' },
  // S (2026-08-27) — plan-mode layering honesty: the global row shows the live
  // effect, and the session tri-state knobs default to inherit.
  planModeOverriddenIn: {
    en: '{mode} — overridden in: {sessions} ⚠',
    zh: '{mode} — 已在以下时段被覆盖: {sessions} ⚠',
    id: '{mode} — ditimpa di: {sessions} ⚠',
  },
  customValue: { en: 'custom', zh: '自定义', id: 'kustom' },
  windows: { en: 'Windows', zh: '时间窗口', id: 'Jendela' },

  // ── P5.2 edit sheet + bulk add (LABELS translate; stored tokens stay English) ──
  editLevel: { en: 'Edit level', zh: '编辑价位', id: 'Ubah level' },
  addLevel: { en: 'Add level', zh: '添加价位', id: 'Tambah level' },
  priceRange: {
    en: 'Price / zone range',
    zh: '价格 / 区间',
    id: 'Harga / rentang zona',
  },
  levelType: { en: 'Type', zh: '类型', id: 'Tipe' },
  instructionLabel: {
    en: 'Instruction (what the AI does here)',
    zh: '指令（AI 在此如何操作）',
    id: 'Instruksi (aksi AI di sini)',
  },
  gradeLabel: { en: 'Grade', zh: '评级', id: 'Nilai' },
  noteLabel: {
    en: 'Note (goes to the AI)',
    zh: '备注（发送给 AI）',
    id: 'Catatan (untuk AI)',
  },
  scenarioTag: { en: 'Scenario tag', zh: '情景标签', id: 'Tag skenario' },
  newPlay: { en: '＋ new play', zh: '＋ 新剧本', id: '＋ play baru' },
  save: { en: 'Save', zh: '保存', id: 'Simpan' },
  cancel: { en: 'Cancel', zh: '取消', id: 'Batal' },
  deleteLevel: { en: 'Delete', zh: '删除', id: 'Hapus' },
  armorWarn: {
    en: '⛔ armor check runs on save — a price ~8×ATR away gets rejected, same as the AI’s would',
    zh: '⛔ 保存时进行护栏检查 — 距价约 8×ATR 的价位会被拒绝，与 AI 的一样',
    id: '⛔ cek armor saat simpan — harga ~8×ATR jauh ditolak, sama seperti AI',
  },
  bulkAdd: { en: 'Bulk add', zh: '批量添加', id: 'Tambah massal' },
  bulkAddHint: {
    en: 'One level per line: price [type] [note]',
    zh: '每行一个价位：价格 [类型] [备注]',
    id: 'Satu level per baris: harga [tipe] [catatan]',
  },
  preview: { en: 'Preview', zh: '预览', id: 'Pratinjau' },
  overlayApplied: {
    en: 'Plan updated',
    zh: '计划已更新',
    id: 'Rencana diperbarui',
  },
  saveFailed: { en: 'Save rejected', zh: '保存被拒', id: 'Simpan ditolak' },

  // ── level type labels (values stay English tokens) ──
  typeDzone: { en: 'D-zone', zh: '需求区', id: 'Zona-D' },
  typeSzone: { en: 'S-zone', zh: '供给区', id: 'Zona-S' },
  typeLevel: { en: 'Level', zh: '价位', id: 'Level' },
  typeLiquidity: { en: 'Liquidity', zh: '流动性', id: 'Likuiditas' },
  typeMyLevel: { en: 'My level', zh: '我的价位', id: 'Level saya' },

  // ── instruction verb labels ──
  instrSweepReclaim: {
    en: 'sweep+reclaim = entry',
    zh: '扫单+夺回 = 进场',
    id: 'sweep+reclaim = masuk',
  },
  instrWatchReclaim: {
    en: 'watch reclaim',
    zh: '观察夺回',
    id: 'pantau reclaim',
  },
  instrFadeFirst: {
    en: 'fade first touch',
    zh: '首次触及反向',
    id: 'fade sentuhan pertama',
  },
  instrTargetOnly: { en: 'target only', zh: '仅作目标', id: 'target saja' },
  instrMagnet: {
    en: 'magnet — no entries',
    zh: '磁吸 — 不进场',
    id: 'magnet — tanpa entri',
  },
  instrNoTouch: { en: 'no touch', zh: '勿触及', id: 'jangan sentuh' },

  // ── P5.3 conflict chip ──
  conflict: { en: 'conflict', zh: '冲突', id: 'konflik' },
  conflictHint: {
    en: 'Owner wins on execution; the AI element is ghosted (both logged).',
    zh: '执行以所有者为准；AI 元素被虚化（均记录）。',
    id: 'Pemilik menang saat eksekusi; elemen AI diredupkan (keduanya dicatat).',
  },

  // ── P5.4 Ask-Planner ──
  askPlannerTitle: {
    en: 'Ask Planner',
    zh: '询问规划师',
    id: 'Tanya Perencana',
  },
  askEvidence: { en: 'Evidence', zh: '依据', id: 'Bukti' },
  askYourPoint: { en: 'Your point', zh: '你的观点', id: 'Poin Anda' },
  askVerdict: { en: 'Verdict', zh: '裁决', id: 'Putusan' },
  clsNewInfo: { en: 'NEW INFO', zh: '新信息', id: 'INFO BARU' },
  clsBare: { en: 'BARE DISAGREEMENT', zh: '无据反对', id: 'BEDA TANPA BUKTI' },
  vDefend: { en: 'DEFEND', zh: '坚持', id: 'PERTAHANKAN' },
  vConcede: { en: 'CONCEDE', zh: '让步', id: 'MENGALAH' },
  vMerge: { en: 'PROPOSE-MERGE', zh: '提议合并', id: 'USUL-GABUNG' },
  // W13 — plan re-alignment on owner edit
  realignReviewing: {
    en: 'planner reviewing your change…',
    zh: '规划器正在复核你的修改…',
    id: 'planner meninjau perubahan Anda…',
  },
  realignNoChange: {
    en: 'planner: no plan change needed',
    zh: '规划器：计划无需改动',
    id: 'planner: rencana tidak perlu diubah',
  },
  realignProposal: {
    en: 'Planner proposes a plan change',
    zh: '规划器建议修改计划',
    id: 'Planner mengusulkan perubahan rencana',
  },
  realignWouldBecome: { en: 'would become', zh: '将变为', id: 'akan menjadi' },
  realignButton: {
    en: 'Re-align plan',
    zh: '重新对齐计划',
    id: 'Selaraskan ulang',
  },
  realignCapped: {
    en: 'auto re-aligns used up — tap Re-align plan',
    zh: '自动对齐次数已用完 — 点击重新对齐计划',
    id: 'auto re-align habis — ketuk Selaraskan ulang',
  },
  realignFailed: {
    en: 'planner unavailable — plan unchanged',
    zh: '规划器不可用 — 计划未改动',
    id: 'planner tidak tersedia — rencana tidak berubah',
  },
  applyMerge: { en: 'Apply merge', zh: '应用合并', id: 'Terapkan gabungan' },
  keepAsIs: { en: 'Keep as-is', zh: '保持不变', id: 'Biarkan' },
  patchNote: {
    en: 'nothing changes until you Apply',
    zh: '在你应用前不会改变任何内容',
    id: 'tidak ada perubahan sampai Anda Terapkan',
  },
  askApplied: {
    en: 'Applied — card updated',
    zh: '已应用 — 计划已更新',
    id: 'Diterapkan — kartu diperbarui',
  },
  askPlaceholder: {
    en: "ask about today's plan… (any language)",
    zh: '询问今日计划…（任何语言）',
    id: 'tanya rencana hari ini… (bahasa apa saja)',
  },
  send: { en: 'Send', zh: '发送', id: 'Kirim' },
  qcWhyLevel: {
    en: 'why this level?',
    zh: '为何是这个价位？',
    id: 'kenapa level ini?',
  },
  qcWhatKills: {
    en: 'what kills the plan?',
    zh: '什么会让计划失效？',
    id: 'apa yang mematikan rencana?',
  },
  sycophancyKPI: {
    en: 'defends-on-bare',
    zh: '无据坚持',
    id: 'pertahankan-tanpa-bukti',
  },
  thinking: { en: 'thinking…', zh: '思考中…', id: 'berpikir…' },

  // ── P5.5 / P5.6 badges ──
  adherence: { en: 'Adherence', zh: '计划遵守', id: 'Kepatuhan' },
  gpa: { en: 'GPA', zh: '平均分', id: 'IPK' },
  tradeReview: { en: 'Trade review', zh: '交易复盘', id: 'Tinjauan trade' },
  beatsRandom: { en: 'beats random', zh: '优于随机', id: 'kalahkan acak' },
  noEdge: { en: 'no edge', zh: '无优势', id: 'tanpa edge' },
  statsWarming: {
    en: 'WARMING {n}/{target}',
    zh: '预热中 {n}/{target}',
    id: 'PEMANASAN {n}/{target}',
  },
} satisfies Record<string, TranslationEntry>

// tp(key, lang, params?) — resolve a plan string, fall back to English, then to
// the key itself; supports {param} interpolation (e.g. warming "{n}" → "3/10").
export function tp(
  key: PlanKey,
  lang: Language,
  params?: Record<string, string | number>
): string {
  const entry = planStrings[key]
  let out = (entry && (entry[lang] ?? entry.en)) || key
  if (params) {
    for (const [k, v] of Object.entries(params)) {
      out = out.replace(new RegExp(`\\{${k}\\}`, 'g'), String(v))
    }
  }
  return out
}

export type { PlanKey }
