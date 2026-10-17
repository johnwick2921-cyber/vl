// W-ONE-BUTTON M5 (U2) — Updates page i18n. Self-contained feature module,
// mirrors plan-translations.ts: every entry carries en / zh / id so a missing
// language is a compile error.

import type { Language } from './translations'

type UpdatesKey = keyof typeof updatesStrings

export const updatesStrings = {
  // page + panels
  pageTitle: { en: 'Updates', zh: '更新', id: 'Pembaruan' },
  runningNow: { en: 'Running now', zh: '当前运行', id: 'Sedang berjalan' },
  revision: { en: 'Revision', zh: '版本号', id: 'Revisi' },
  guideRev: { en: 'Guide rev', zh: '指南版本', id: 'Revisi panduan' },
  addonBuild: {
    en: 'AddOn build (maintenance ack)',
    zh: 'AddOn 构建（维护回执）',
    id: 'Build AddOn (ack pemeliharaan)',
  },
  mvidMatch: {
    en: 'MVID / hello match',
    zh: 'MVID / hello 匹配',
    id: 'MVID / kecocokan hello',
  },
  updatePanel: { en: 'Update', zh: '更新', id: 'Perbarui' },
  maintenancePanel: {
    en: 'Maintenance hold + gate',
    zh: '维护挂起 + 闸门',
    id: 'Tahan pemeliharaan + gerbang',
  },
  jobPanel: { en: 'Job', zh: '任务', id: 'Pekerjaan' },
  historyPanel: {
    en: 'Native history readiness',
    zh: '原生历史就绪状态',
    id: 'Kesiapan riwayat native',
  },

  // button states (labels)
  updateNow: { en: 'Update now', zh: '立即更新', id: 'Perbarui sekarang' },
  checking: { en: 'Checking…', zh: '检查中…', id: 'Memeriksa…' },
  installing: { en: 'Installing…', zh: '安装中…', id: 'Memasang…' },
  retry: { en: 'Retry', zh: '重试', id: 'Coba lagi' },
  upToDate: { en: 'Up to date', zh: '已是最新', id: 'Sudah terbaru' },
  blocked: { en: 'Blocked', zh: '已阻止', id: 'Diblokir' },
  installUnderReview: {
    en: 'install authorization under review',
    zh: '安装授权审核中',
    id: 'otorisasi pemasangan sedang ditinjau',
  },

  // hold / gate
  holdState: { en: 'Hold state', zh: '挂起状态', id: 'Status tahan' },
  drained: { en: 'Drained', zh: '已排空', id: 'Terkuras' },
  inFlightSends: { en: 'In-flight sends', zh: '在途发送', id: 'Kiriman aktif' },
  addonAck: { en: 'AddOn ack', zh: 'AddOn 回执', id: 'Ack AddOn' },
  noAckYet: { en: 'no ack yet', zh: '暂无回执', id: 'belum ada ack' },
  gateReady: { en: 'Gate ready', zh: '闸门就绪', id: 'Gerbang siap' },
  gateLegs: { en: 'Legs', zh: '检查项', id: 'Pemeriksaan' },

  // job
  noUpdateJob: {
    en: 'no update job',
    zh: '没有更新任务',
    id: 'tidak ada pekerjaan pembaruan',
  },
  jobId: { en: 'Job id', zh: '任务 ID', id: 'ID pekerjaan' },
  jobState: { en: 'State', zh: '状态', id: 'Status' },
  jobStep: { en: 'Step', zh: '步骤', id: 'Langkah' },
  jobBlocker: { en: 'Blocker', zh: '阻塞原因', id: 'Pemblokir' },
  receipt: { en: 'Receipt', zh: '回执', id: 'Tanda terima' },
  downloadReceipt: {
    en: 'Download receipt',
    zh: '下载回执',
    id: 'Unduh tanda terima',
  },

  // history
  tradingPaused: {
    en: 'Trading paused while native history loads',
    zh: '原生历史加载期间暂停交易',
    id: 'Trading dijeda saat riwayat native dimuat',
  },
  completedBars: {
    en: '4H completed bars',
    zh: '4H 已完成K线',
    id: 'Bar 4H selesai',
  },
  lastProgress: {
    en: 'last progress',
    zh: '最近进度',
    id: 'progres terakhir',
  },
  pivotWindow: { en: 'PivotWindow', zh: 'PivotWindow', id: 'PivotWindow' },
  reloadHistory: {
    en: 'Reload history',
    zh: '重新加载历史',
    id: 'Muat ulang riwayat',
  },
  reloading: { en: 'Reloading…', zh: '加载中…', id: 'Memuat…' },
  notApplicable: { en: 'n/a', zh: 'n/a', id: 'n/a' },
  checkReason: { en: 'Server says', zh: '服务器返回', id: 'Kata server' },
  pivotWindowUnknown: {
    en: 'PivotWindow unknown',
    zh: 'PivotWindow 未知',
    id: 'PivotWindow tidak diketahui',
  },
  futuresSymbolUnknown: {
    en: 'Futures symbol unknown',
    zh: '期货合约未知',
    id: 'Simbol futures tidak diketahui',
  },
  confirmBackfill: {
    en: 'Send a 4H backfill of {n} bars to NT8?',
    zh: '向 NT8 发送 {n} 根 4H K线的回填？',
    id: 'Kirim backfill 4H sebanyak {n} bar ke NT8?',
  },
  confirm: { en: 'Confirm', zh: '确认', id: 'Konfirmasi' },
  cancel: { en: 'Cancel', zh: '取消', id: 'Batal' },
} as const

export function up(
  key: UpdatesKey,
  lang: Language,
  params?: Record<string, string | number>
): string {
  let text: string = updatesStrings[key]?.[lang] ?? updatesStrings[key].en
  if (params) {
    for (const [param, value] of Object.entries(params)) {
      text = text.replace(`{${param}}`, String(value))
    }
  }
  return text
}
