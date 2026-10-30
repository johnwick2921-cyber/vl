// PEP MODE i18n (owner order 2026-10-29). Self-contained feature module, the
// pattern of plan-translations.ts / updates-translations.ts: EVERY entry carries
// en / zh / id, so a missing language is a compile error (TranslationEntry) and
// pep-translations.test.ts pins non-empty text for all three.
//
// Why not the Studio's own Copy type: the strategy components carry zh/en/es,
// but the UI languages are en / zh / id — an Indonesian owner would read
// English in every Studio panel. Pep copy is written for the three real UI
// languages. (The older Studio panels keep their gap; it is not widened here.)
//
// SIM-ONLY wording is deliberate and repeated: every trade path is
// NinjaTrader SIM.

import type { Language } from './translations'

interface TranslationEntry {
  en: string
  zh: string
  id: string
}

export type PepKey = keyof typeof pepStrings

export const pepStrings = {
  // ── trading-mode selector ──
  modeTitle: { en: 'Trading mode', zh: '交易模式', id: 'Mode trading' },
  modeDesc: {
    en: 'A strategy is exactly ONE of AI planner, Mentor or Pep. Choosing one turns the others off. Takes effect when you save (the running trader reloads on save, no restart). SIM only.',
    zh: '一个策略只能是【AI 规划】、【导师】或【Pep】三者之一。选择其一会关闭其他模式。保存后生效（运行中的交易员自动重载，无需重启）。仅限 SIM。',
    id: 'Satu strategi hanya boleh SATU dari Perencana AI, Mentor, atau Pep. Memilih satu akan mematikan yang lain. Berlaku saat disimpan (trader yang berjalan dimuat ulang, tanpa restart). Hanya SIM.',
  },
  modeAI: { en: 'AI planner', zh: 'AI 规划', id: 'Perencana AI' },
  modeMentor: { en: 'Mentor', zh: '导师', id: 'Mentor' },
  modePep: { en: 'Pep', zh: 'Pep', id: 'Pep' },
  modeAIHelp: {
    en: 'The AI planner and the day plan place the entries (default).',
    zh: '由 AI 规划器和日计划下入场单（默认）。',
    id: 'Perencana AI dan rencana harian yang menempatkan entri (bawaan).',
  },
  modeMentorHelp: {
    en: 'The mentor evaluator places the entries; the AI places none.',
    zh: '入场单来自导师评估器；AI 不下入场单。',
    id: 'Evaluator mentor yang menempatkan entri; AI tidak menempatkan entri.',
  },
  modePepHelp: {
    en: 'Two fixed opening-range rules from the owner’s PDF (style A or E). No mentor rule applies. The AI makes no entries. Pep has no break-even and no trailing, and the entry R:R / minimum-stop checks of Risk Control do not apply (stop and target are the box); daily limits, owner pause and the SIM-only block still do. OFF by default.',
    zh: '来自 owner PDF 的两套固定开盘区间规则（A 或 E 风格）。不适用任何导师规则。AI 不下入场单。Pep 没有保本和移动止损，风险控制中的入场盈亏比/最小止损检查不适用（止损和止盈就是箱体）；每日限额、所有者暂停和仅 SIM 限制仍然生效。默认关闭。',
    id: 'Dua aturan rentang pembukaan tetap dari PDF pemilik (gaya A atau E). Tidak ada aturan mentor yang berlaku. AI tidak menempatkan entri. Pep tidak punya break-even dan trailing, dan pemeriksaan R:R entri / stop minimum di Risk Control tidak berlaku (stop dan target adalah box); batas harian, jeda pemilik, dan blokir hanya-SIM tetap berlaku. Bawaan MATI.',
  },
  modeDayPlanBlocks: {
    en: 'Pep cannot be chosen while the Day Plan is ON (the server refuses the combination). Turn the Day Plan off first.',
    zh: '日计划开启时不能选择 Pep（服务器会拒绝该组合）。请先关闭日计划。',
    id: 'Pep tidak bisa dipilih saat Rencana Harian AKTIF (server menolak kombinasi ini). Matikan Rencana Harian terlebih dahulu.',
  },
  dayPlanPepBlocks: {
    en: 'Pep mode is ON for this strategy. The Day Plan cannot be turned on together with Pep (the server refuses the save). Switch the Trading mode to AI planner first.',
    zh: '该策略已开启 Pep 模式。日计划不能与 Pep 同时开启（服务器会拒绝保存）。请先把交易模式切换为 AI 规划。',
    id: 'Mode Pep AKTIF untuk strategi ini. Rencana Harian tidak bisa diaktifkan bersama Pep (server menolak penyimpanan). Ubah Mode trading ke Perencana AI terlebih dahulu.',
  },
  dayPlanPepBoth: {
    en: 'Pep AND the Day Plan are both ON. The server refuses to save that and will not start the trader. Turn the Day Plan off here, or switch the Trading mode.',
    zh: 'Pep 和日计划同时开启。服务器会拒绝保存，且不会启动该交易员。请在此关闭日计划，或切换交易模式。',
    id: 'Pep DAN Rencana Harian sama-sama AKTIF. Server menolak menyimpannya dan tidak akan menjalankan trader. Matikan Rencana Harian di sini, atau ubah Mode trading.',
  },
  modeConflict: {
    en: 'This strategy has Mentor AND Pep both ON. The server refuses to save that. Pick one mode above.',
    zh: '该策略同时开启了导师和 Pep，服务器会拒绝保存。请在上方只选一种模式。',
    id: 'Strategi ini mengaktifkan Mentor DAN Pep sekaligus. Server menolak menyimpannya. Pilih satu mode di atas.',
  },
  confirmPepTitle: {
    en: 'Turn Pep mode ON for this strategy?',
    zh: '为该策略开启 Pep 模式？',
    id: 'Aktifkan mode Pep untuk strategi ini?',
  },
  confirmMentorTitle: {
    en: 'Turn Mentor mode ON for this strategy?',
    zh: '为该策略开启导师模式？',
    id: 'Aktifkan mode Mentor untuk strategi ini?',
  },
  confirmPepBody: {
    en: 'Pep will trade the opening range by fixed rules on NinjaTrader SIM only, {n} contract(s), style {style}. Mentor mode is switched off. Nothing is placed until you press Save.',
    zh: 'Pep 将按固定规则在 NinjaTrader SIM 上交易开盘区间，{n} 口，{style} 风格。导师模式将被关闭。点击保存之前不会下任何单。',
    id: 'Pep akan memperdagangkan rentang pembukaan dengan aturan tetap hanya di NinjaTrader SIM, {n} kontrak, gaya {style}. Mode Mentor dimatikan. Tidak ada order sebelum Anda menekan Simpan.',
  },
  confirmMentorBody: {
    en: 'The mentor evaluator will place the entries (SIM only) and the AI places none. Pep is switched off. Nothing is placed until you press Save.',
    zh: '入场单将来自导师评估器（仅 SIM），AI 不下入场单。Pep 将被关闭。点击保存之前不会下任何单。',
    id: 'Evaluator mentor akan menempatkan entri (hanya SIM) dan AI tidak menempatkan entri. Pep dimatikan. Tidak ada order sebelum Anda menekan Simpan.',
  },
  confirmStrategy: { en: 'Strategy', zh: '策略', id: 'Strategi' },
  confirmOk: { en: 'Turn ON', zh: '开启', id: 'Aktifkan' },
  confirmCancel: { en: 'Cancel', zh: '取消', id: 'Batal' },

  // ── panel ──
  panelTitle: {
    en: 'Pep mode — opening-range rules A / E',
    zh: 'Pep 模式 — 开盘区间规则 A / E',
    id: 'Mode Pep — aturan rentang pembukaan A / E',
  },
  panelOff: {
    en: 'Pep is OFF for this strategy. The numbers below are kept, but no order is placed until Pep is chosen under Trading mode and the strategy is saved.',
    zh: '该策略的 Pep 为关闭状态。下方数字会被保留，但在【交易模式】中选择 Pep 并保存之前不会下任何单。',
    id: 'Pep MATI untuk strategi ini. Angka di bawah tetap tersimpan, tetapi tidak ada order sebelum Pep dipilih di Mode trading dan strategi disimpan.',
  },
  panelOn: {
    en: 'Pep is ON: style {style}, SIM only. A saved change reaches the running trader when you save (it reloads). The “eff” line under each number is the value the bot RUNS.',
    zh: 'Pep 已开启：{style} 风格，仅 SIM。保存后修改才会到达运行中的交易员（自动重载）。每个数字下的 “eff” 行是机器人实际运行的值。',
    id: 'Pep AKTIF: gaya {style}, hanya SIM. Perubahan sampai ke trader yang berjalan saat disimpan (dimuat ulang). Baris “eff” di bawah tiap angka adalah nilai yang DIJALANKAN bot.',
  },
  sharedBlock: {
    en: 'Shared by both styles',
    zh: '两种风格共用',
    id: 'Dipakai bersama kedua gaya',
  },
  runsStyle: {
    en: 'Style that runs',
    zh: '运行的风格',
    id: 'Gaya yang dijalankan',
  },
  runsStyleHelp: {
    en: 'Switching style never loses the other style’s numbers: each style keeps its own block.',
    zh: '切换风格不会丢失另一种风格的数字：每种风格有各自的数据块。',
    id: 'Berganti gaya tidak menghilangkan angka gaya lain: setiap gaya punya blok sendiri.',
  },
  tabA: { en: 'Style A', zh: 'A 风格', id: 'Gaya A' },
  tabE: { en: 'Style E', zh: 'E 风格', id: 'Gaya E' },
  styleAName: {
    en: 'A — ORB 5m breakout, enter immediately',
    zh: 'A — 5 分钟 ORB 突破，立即入场',
    id: 'A — breakout ORB 5m, masuk langsung',
  },
  styleEName: {
    en: 'E — EMA9 ORB 2m, candle-close confirmation',
    zh: 'E — EMA9 ORB 2 分钟，收盘确认',
    id: 'E — EMA9 ORB 2m, konfirmasi penutupan candle',
  },
  styleAHelp: {
    en: 'The first side to trade THROUGH a box edge is entered. Stop and target are fractions of the box (PDF: full box each). No trailing, no break-even.',
    zh: '第一个穿过区间边缘的方向即入场。止损和止盈是区间的倍数（PDF：各为整个区间）。无移动止损，无保本。',
    id: 'Sisi pertama yang menembus tepi kotak langsung dimasuki. Stop dan target adalah pecahan kotak (PDF: masing-masing satu kotak penuh). Tanpa trailing, tanpa break-even.',
  },
  styleEHelp: {
    en: 'On a CLOSED candle the EMA (carried across sessions) crossing a box edge enters. PDF: stop half the box rounded OUTWARD to a tick, target the full box. No trailing.',
    zh: '在已收盘的 K 线上，EMA（跨交易日延续）穿过区间边缘即入场。PDF：止损为半个区间并向外取整到跳动点，止盈为整个区间。无移动止损。',
    id: 'Pada candle yang SUDAH TUTUP, EMA (berlanjut lintas sesi) yang melewati tepi kotak masuk. PDF: stop setengah kotak dibulatkan KE LUAR ke tick, target satu kotak penuh. Tanpa trailing.',
  },
  pdfSource: {
    en: 'Source: the owner’s PDF “A + E — Full calculation”.',
    zh: '来源：owner 的 PDF《A + E — 完整计算》。',
    id: 'Sumber: PDF pemilik “A + E — Perhitungan lengkap”.',
  },

  // field labels + help
  fContracts: { en: 'Contracts', zh: '口数', id: 'Kontrak' },
  fContractsHelp: {
    en: 'Shipped 6, hard ceiling 10. The 2-contract AI clamp does not apply to pep.',
    zh: '默认 6，硬上限 10。AI 的 2 口限制不适用于 pep。',
    id: 'Bawaan 6, batas keras 10. Batas 2 kontrak AI tidak berlaku untuk pep.',
  },
  fBoxStart: {
    en: 'Box start (CT)',
    zh: '区间开始 (CT)',
    id: 'Mulai kotak (CT)',
  },
  fBoxStartHelp: {
    en: 'HH:MM America/Chicago (DST-safe). Shipped 08:30.',
    zh: 'HH:MM，美国芝加哥时间（自动处理夏令时）。默认 08:30。',
    id: 'HH:MM America/Chicago (aman DST). Bawaan 08:30.',
  },
  fFlatTime: {
    en: 'Flat time (CT)',
    zh: '平仓时间 (CT)',
    id: 'Waktu flat (CT)',
  },
  fFlatTimeHelp: {
    en: 'The time exit at the last price. Shipped 15:00 CT (the session flat of 14:45 is not used).',
    zh: '按最新价平仓的时间出场。默认 15:00 CT（不使用 14:45 的交易时段平仓）。',
    id: 'Keluar waktu di harga terakhir. Bawaan 15:00 CT (flat sesi 14:45 tidak dipakai).',
  },
  fTick: { en: 'Tick size', zh: '最小跳动点', id: 'Ukuran tick' },
  fTickHelp: {
    en: 'MNQ tick, shipped 0.25 points.',
    zh: 'MNQ 最小跳动，默认 0.25 点。',
    id: 'Tick MNQ, bawaan 0,25 poin.',
  },
  fSlRounding: { en: 'Stop rounding', zh: '止损取整', id: 'Pembulatan stop' },
  fSlRoundingHelp: {
    en: 'outward (shipped, the PDF rule: away from entry) or nearest tick.',
    zh: 'outward（默认，PDF 规则：远离入场价）或 nearest（最近跳动点）。',
    id: 'outward (bawaan, aturan PDF: menjauhi entri) atau tick terdekat (nearest).',
  },
  fMaxTrades: {
    en: 'Max trades per day',
    zh: '每日最大交易次数',
    id: 'Maks trade per hari',
  },
  fMaxTradesHelp: {
    en: 'Shipped 1. Style A takes the first break only, whatever this says.',
    zh: '默认 1。A 风格只取第一次突破，无论此值为多少。',
    id: 'Bawaan 1. Gaya A hanya mengambil breakout pertama, berapa pun nilai ini.',
  },
  fFee: {
    en: 'Fee per contract round trip (USD)',
    zh: '每口来回手续费 (USD)',
    id: 'Biaya per kontrak pulang-pergi (USD)',
  },
  fFeeHelp: {
    en: 'Shipped $1. Backtest and reporting ONLY — no order or gate reads it.',
    zh: '默认 $1。仅用于回测和报表，任何订单或闸门都不读取。',
    id: 'Bawaan $1. HANYA untuk backtest dan laporan — tidak ada order atau gerbang yang membacanya.',
  },
  fMaxBox: {
    en: 'Max box size (points, 0 = OFF)',
    zh: '区间上限（点，0 = 关闭）',
    id: 'Ukuran kotak maks (poin, 0 = MATI)',
  },
  fMaxBoxHelp: {
    en: 'OFF by default (the PDF trades every box). When set, a wider box is refused with a named reason.',
    zh: '默认关闭（PDF 每个区间都交易）。设置后，更宽的区间会以明确理由被拒绝。',
    id: 'MATI secara bawaan (PDF memperdagangkan setiap kotak). Jika diisi, kotak yang lebih lebar ditolak dengan alasan bernama.',
  },
  fSignalAge: {
    en: 'Signal max age (min)',
    zh: '信号最大时效（分钟）',
    id: 'Usia maks sinyal (mnt)',
  },
  fSignalAgeHelp: {
    en: 'Shipped 2. A break/cross older than this, or a newest 1m bar older than this, is refused as stale — never entered late.',
    zh: '默认 2。比此更旧的突破/交叉，或比此更旧的最新 1 分钟 K 线，会因过期被拒绝，绝不迟入场。',
    id: 'Bawaan 2. Breakout/cross yang lebih tua dari ini, atau bar 1m terbaru yang lebih tua, ditolak sebagai basi — tidak pernah dimasuki terlambat.',
  },
  fBoxMinutes: {
    en: 'Box length (min)',
    zh: '区间长度（分钟）',
    id: 'Panjang kotak (mnt)',
  },
  fABoxMinutesHelp: {
    en: 'Style A: shipped 5 (the PDF’s 5-minute ORB).',
    zh: 'A 风格：默认 5（PDF 的 5 分钟 ORB）。',
    id: 'Gaya A: bawaan 5 (ORB 5 menit PDF).',
  },
  fEBoxMinutesHelp: {
    en: 'Style E: shipped 2 (the PDF’s “ORB 2 PHÚT”; its ledger equals the first two minutes’ range).',
    zh: 'E 风格：默认 2（PDF 的 “ORB 2 PHÚT”；其账本等于前两分钟的区间）。',
    id: 'Gaya E: bawaan 2 (“ORB 2 PHÚT” PDF; buku besarnya sama dengan rentang dua menit pertama).',
  },
  fSlFraction: {
    en: 'Stop (× box)',
    zh: '止损（× 区间）',
    id: 'Stop (× kotak)',
  },
  fASlHelp: {
    en: 'Style A stop as a fraction of the box. Shipped 1.0 (full box).',
    zh: 'A 风格止损占区间的比例。默认 1.0（整个区间）。',
    id: 'Stop gaya A sebagai pecahan kotak. Bawaan 1,0 (kotak penuh).',
  },
  fESlHelp: {
    en: 'Style E stop as a fraction of the box. Shipped 0.5 (half box), rounded per Stop rounding.',
    zh: 'E 风格止损占区间的比例。默认 0.5（半个区间），按“止损取整”取整。',
    id: 'Stop gaya E sebagai pecahan kotak. Bawaan 0,5 (setengah kotak), dibulatkan sesuai Pembulatan stop.',
  },
  fTpFraction: {
    en: 'Target (× box)',
    zh: '止盈（× 区间）',
    id: 'Target (× kotak)',
  },
  fATpHelp: {
    en: 'Style A target as a fraction of the box. Shipped 1.0 (full box).',
    zh: 'A 风格止盈占区间的比例。默认 1.0（整个区间）。',
    id: 'Target gaya A sebagai pecahan kotak. Bawaan 1,0 (kotak penuh).',
  },
  fETpHelp: {
    en: 'Style E target as a fraction of the box. Shipped 1.0 (full box).',
    zh: 'E 风格止盈占区间的比例。默认 1.0（整个区间）。',
    id: 'Target gaya E sebagai pecahan kotak. Bawaan 1,0 (kotak penuh).',
  },
  fBothEdges: {
    en: 'One bar through both edges',
    zh: '一根 K 线穿过两条边',
    id: 'Satu bar menembus kedua tepi',
  },
  fBothEdgesHelp: {
    en: 'no_trade (shipped, fail closed) or nearest_open (a bar-level guess: the edge nearer the bar’s open broke first).',
    zh: 'no_trade（默认，失败即关闭）或 nearest_open（按 K 线级别猜测：离开盘价更近的边先被突破）。',
    id: 'no_trade (bawaan, gagal tertutup) atau nearest_open (tebakan tingkat bar: tepi yang lebih dekat ke open bar lebih dulu tembus).',
  },
  fEmaPeriod: { en: 'EMA period', zh: 'EMA 周期', id: 'Periode EMA' },
  fEmaPeriodHelp: {
    en: 'Shipped 9 (range 2–200). Computed across sessions, never reset daily.',
    zh: '默认 9（范围 2–200）。跨交易日计算，从不按日重置。',
    id: 'Bawaan 9 (rentang 2–200). Dihitung lintas sesi, tidak pernah direset harian.',
  },
  fEmaTf: {
    en: 'EMA candle (min)',
    zh: 'EMA K 线（分钟）',
    id: 'Candle EMA (mnt)',
  },
  fEmaTfHelp: {
    en: 'Shipped 2. Built from 1m on even Chicago minutes; must divide an hour.',
    zh: '默认 2。由 1 分钟 K 线按芝加哥偶数分钟合成；必须能整除一小时。',
    id: 'Bawaan 2. Dibangun dari 1m pada menit genap Chicago; harus membagi satu jam.',
  },
  fSignalSource: { en: 'Signal source', zh: '信号来源', id: 'Sumber sinyal' },
  fSignalSourceHelp: {
    en: 'ema (owner ruling, shipped): the EMA crosses the box edge. close: the candle CLOSE crosses the edge.',
    zh: 'ema（owner 裁定，默认）：EMA 穿过区间边缘。close：K 线收盘价穿过边缘。',
    id: 'ema (keputusan pemilik, bawaan): EMA melewati tepi kotak. close: HARGA PENUTUPAN candle melewati tepi.',
  },
  fWarmup: {
    en: 'Warm-up candles (0 = auto)',
    zh: '预热 K 线数（0 = 自动）',
    id: 'Candle pemanasan (0 = otomatis)',
  },
  fWarmupHelp: {
    en: '0 = 3 × the EMA period complete candles before the EMA may signal.',
    zh: '0 = 在 EMA 可发信号前需要 3 × EMA 周期根完整 K 线。',
    id: '0 = 3 × periode EMA candle lengkap sebelum EMA boleh memberi sinyal.',
  },
  badTime: {
    en: 'Enter the time as HH:MM (00:00–23:59).',
    zh: '请按 HH:MM 输入时间（00:00–23:59）。',
    id: 'Masukkan waktu sebagai HH:MM (00:00–23:59).',
  },
  optOutward: { en: 'outward', zh: 'outward（向外）', id: 'outward (ke luar)' },
  optNearest: {
    en: 'nearest',
    zh: 'nearest（最近）',
    id: 'nearest (terdekat)',
  },
  optNoTrade: {
    en: 'no_trade',
    zh: 'no_trade（不交易）',
    id: 'no_trade (tidak trade)',
  },
  optNearestOpen: {
    en: 'nearest_open',
    zh: 'nearest_open（最近开盘）',
    id: 'nearest_open (open terdekat)',
  },
  optEma: { en: 'ema', zh: 'ema', id: 'ema' },
  optClose: { en: 'close', zh: 'close', id: 'close' },
  effEnabled: { en: 'Pep on/off', zh: 'Pep 开/关', id: 'Pep aktif/mati' },

  // ── dashboard ──
  chipAI: { en: 'AI', zh: 'AI', id: 'AI' },
  chipMentor: { en: 'Mentor', zh: '导师', id: 'Mentor' },
  chipPep: { en: 'Pep {style}', zh: 'Pep {style}', id: 'Pep {style}' },
  chipTitle: {
    en: 'Trading mode of this trader’s strategy',
    zh: '该交易员策略的交易模式',
    id: 'Mode trading dari strategi trader ini',
  },
  truthTitle: {
    en: 'Pep — what trades',
    zh: 'Pep — 交易内容',
    id: 'Pep — apa yang diperdagangkan',
  },
  truthSim: { en: 'SIM only', zh: '仅 SIM', id: 'Hanya SIM' },
  truthAsOf: { en: 'as of {t} CT', zh: '截至 {t} CT', id: 'per {t} CT' },
  truthStale: {
    en: 'refresh failed — showing the last good read',
    zh: '刷新失败 — 显示最后一次有效数据',
    id: 'penyegaran gagal — menampilkan pembacaan terakhir yang baik',
  },
  truthBox: { en: 'Box', zh: '区间', id: 'Kotak' },
  truthBoxHigh: { en: 'high', zh: '高', id: 'tinggi' },
  truthBoxLow: { en: 'low', zh: '低', id: 'rendah' },
  truthBoxSize: { en: 'size', zh: '大小', id: 'ukuran' },
  truthBoxNotDrawn: {
    en: 'Box not drawn yet (the box window has not closed, or a minute is missing)',
    zh: '区间尚未形成（区间窗口未结束，或缺少某一分钟）',
    id: 'Kotak belum terbentuk (jendela kotak belum tutup, atau ada menit yang hilang)',
  },
  truthSignal: { en: 'Signal', zh: '信号', id: 'Sinyal' },
  sigNoData: {
    en: 'no evaluation yet',
    zh: '尚未评估',
    id: 'belum ada evaluasi',
  },
  sigWaitingBox: {
    en: 'waiting for the box to close',
    zh: '等待区间形成',
    id: 'menunggu kotak selesai',
  },
  sigWaitingSignal: {
    en: 'waiting for a break',
    zh: '等待突破',
    id: 'menunggu breakout',
  },
  sigSignal: {
    en: 'signal — entry being sent',
    zh: '信号 — 正在下单',
    id: 'sinyal — entri sedang dikirim',
  },
  sigBlocked: {
    en: 'refused (see the reason)',
    zh: '被拒绝（见原因）',
    id: 'ditolak (lihat alasan)',
  },
  sigTraded: {
    en: 'today’s trade is done',
    zh: '今日交易已完成',
    id: 'trade hari ini selesai',
  },
  sigPositionOpen: {
    en: 'position open',
    zh: '持仓中',
    id: 'posisi terbuka',
  },
  sigAfterFlat: {
    en: 'past the flat time — no new entry',
    zh: '已过平仓时间 — 不再入场',
    id: 'lewat waktu flat — tanpa entri baru',
  },
  sigFlatPending: {
    en: 'flat time reached — closing',
    zh: '已到平仓时间 — 正在平仓',
    id: 'waktu flat tercapai — menutup',
  },
  truthNotEvaluated: {
    en: 'not evaluated yet',
    zh: '尚未评估',
    id: 'belum dievaluasi',
  },
  truthConflictMentor: {
    en: 'Mode conflict: Mentor and Pep are both ON. The trader was NOT started and will not trade until the strategy is fixed.',
    zh: '模式冲突：导师和 Pep 同时开启。该交易员未启动，策略修复前不会交易。',
    id: 'Konflik mode: Mentor dan Pep sama-sama AKTIF. Trader TIDAK dijalankan dan tidak akan trading sampai strategi diperbaiki.',
  },
  truthConflictDayPlan: {
    en: 'Mode conflict: Pep and the Day Plan are both ON. The trader was NOT started and will not trade until the strategy is fixed.',
    zh: '模式冲突：Pep 和日计划同时开启。该交易员未启动，策略修复前不会交易。',
    id: 'Konflik mode: Pep dan Rencana Harian sama-sama AKTIF. Trader TIDAK dijalankan dan tidak akan trading sampai strategi diperbaiki.',
  },
  truthConflictOther: {
    en: 'Mode conflict: two trading modes are both ON. The trader was NOT started and will not trade until the strategy is fixed.',
    zh: '模式冲突：两种交易模式同时开启。该交易员未启动，策略修复前不会交易。',
    id: 'Konflik mode: dua mode trading sama-sama AKTIF. Trader TIDAK dijalankan dan tidak akan trading sampai strategi diperbaiki.',
  },
  chipConflict: {
    en: 'Mode conflict',
    zh: '模式冲突',
    id: 'Konflik mode',
  },
  truthTrades: { en: 'Trades today', zh: '今日交易', id: 'Trade hari ini' },
  truthStopTarget: {
    en: 'Stop / target',
    zh: '止损 / 止盈',
    id: 'Stop / target',
  },
  truthFlat: { en: 'Flat at', zh: '平仓于', id: 'Flat pada' },
  truthFlatIn: { en: 'in {t}', zh: '还有 {t}', id: 'dalam {t}' },
  truthFlatDone: {
    en: 'flat time passed',
    zh: '已过平仓时间',
    id: 'waktu flat lewat',
  },
  truthPosition: { en: 'Position open', zh: '持仓中', id: 'Posisi terbuka' },
  truthRefusal: { en: 'Refusal', zh: '拒绝原因', id: 'Penolakan' },
  truthOverlayHidden: {
    en: 'pep box lines hidden — they belong to {symbol}, this chart shows {chart}',
    zh: 'pep 区间线已隐藏 — 它们属于 {symbol}，当前图表显示 {chart}',
    id: 'garis kotak pep disembunyikan — milik {symbol}, grafik ini menampilkan {chart}',
  },
  lineBoxHigh: {
    en: 'pep box high {price}',
    zh: 'pep 区间高点 {price}',
    id: 'pep kotak tinggi {price}',
  },
  lineBoxLow: {
    en: 'pep box low {price}',
    zh: 'pep 区间低点 {price}',
    id: 'pep kotak rendah {price}',
  },
  lineStop: {
    en: 'pep stop {price}',
    zh: 'pep 止损 {price}',
    id: 'pep batas rugi {price}',
  },
  lineTarget: {
    en: 'pep target {price}',
    zh: 'pep 止盈 {price}',
    id: 'pep sasaran {price}',
  },
  truthOverlayNote: {
    en: 'pep box lines: {symbol}{stale}',
    zh: 'pep 区间线：{symbol}{stale}',
    id: 'garis kotak pep: {symbol}{stale}',
  },
} satisfies Record<string, TranslationEntry>

/** pt(key, lang, params?) — resolve a pep string; falls back to English, then
 *  to the key itself; supports {param} interpolation. */
export function pt(
  key: PepKey,
  lang: Language,
  params?: Record<string, string | number>
): string {
  const entry = pepStrings[key] as TranslationEntry | undefined
  let out = (entry && (entry[lang] || entry.en)) || key
  if (params) {
    for (const [k, v] of Object.entries(params)) {
      out = out.split(`{${k}}`).join(String(v))
    }
  }
  return out
}
