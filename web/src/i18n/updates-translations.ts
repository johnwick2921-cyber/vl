// W-ONE-BUTTON M5 (U2) — Updates page i18n. Self-contained feature module,
// mirrors plan-translations.ts: every entry carries en / zh / id so a missing
// language is a compile error.

import type { Language } from './translations'

type UpdatesKey = keyof typeof updatesStrings

export const updatesStrings = {
  // page + panels
  pageTitle: { en: 'Updates', zh: '更新', id: 'Pembaruan' },
  pollingStopped: {
    en: 'Update surface not enrolled — polling stopped',
    zh: '更新功能未登记 — 轮询已停止',
    id: 'Permukaan pembaruan tidak terdaftar — polling dihentikan',
  },
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
  checkAgain: { en: 'Check again', zh: '再次检查', id: 'Periksa lagi' },
  checkRefused: {
    en: 'Check refused{status} — {reason}',
    zh: '检查被拒绝{status} — {reason}',
    id: 'Pemeriksaan ditolak{status} — {reason}',
  },
  blocked: { en: 'Blocked', zh: '已阻止', id: 'Diblokir' },
  check: { en: 'Check', zh: '检查', id: 'Periksa' },
  installPasswordLabel: {
    en: 'Your VL login password',
    zh: '你的 VL 登录密码',
    id: 'Kata sandi login VL Anda',
  },
  installPasswordPlaceholder: {
    en: 'password (never stored)',
    zh: '密码（不会保存）',
    id: 'kata sandi (tidak disimpan)',
  },
  advancedInstallCode: {
    en: 'Advanced: paste an install code',
    zh: '高级：粘贴安装授权码',
    id: 'Lanjutan: tempel kode pemasangan',
  },
  checkFirstForUpdate: {
    en: 'run Check first — the release id comes from the server',
    zh: '请先点击检查 — 版本号由服务器返回',
    id: 'jalankan Periksa dulu — id rilis dari server',
  },
  updateAvailableVerified: {
    en: 'Update available {tag} — verified',
    zh: '发现新版本 {tag} — 已验证',
    id: 'Pembaruan tersedia {tag} — terverifikasi',
  },
  updateNotReady: {
    en: 'Update available {tag} — not yet verified',
    zh: '发现新版本 {tag} — 尚未验证',
    id: 'Pembaruan tersedia {tag} — belum terverifikasi',
  },
  rateLimited: {
    en: 'rate limited, try later',
    zh: '请求受限，请稍后再试',
    id: 'dibatasi, coba lagi nanti',
  },
  downloading: { en: 'Downloading…', zh: '下载中…', id: 'Mengunduh…' },
  verifying: { en: 'Verifying…', zh: '验证中…', id: 'Memverifikasi…' },
  installUnderReview: {
    en: 'install authorization under review',
    zh: '安装授权审核中',
    id: 'otorisasi pemasangan sedang ditinjau',
  },
  workerNotRunning: {
    en: 'updater worker not running',
    zh: '更新器工作进程未运行',
    id: 'pekerja updater tidak berjalan',
  },
  // authorization paste box (UPDATER-USABLE-V1)
  authzLabel: {
    en: 'Authorization — paste the one line "vl-updater-bootstrap authorize <release_id>" prints (valid 5 minutes, single use)',
    zh: '授权 — 粘贴 "vl-updater-bootstrap authorize <release_id>" 打印的那一行（有效期 5 分钟，仅限一次）',
    id: 'Otorisasi — tempel satu baris yang dicetak "vl-updater-bootstrap authorize <release_id>" (berlaku 5 menit, sekali pakai)',
  },
  installAccepted: {
    en: 'Install accepted',
    zh: '安装已接受',
    id: 'Pemasangan diterima',
  },
  openOn8080: {
    en: 'open http://localhost:8080 to install — this page is served from port {port}, which the bot refuses for Updates (open the bot directly)',
    zh: '请打开 http://localhost:8080 进行安装 — 本页面来自端口 {port}，机器人会拒绝该来源的更新请求（请直接打开机器人）',
    id: 'buka http://localhost:8080 untuk memasang — halaman ini dilayani dari port {port}, yang ditolak bot untuk Pembaruan (buka bot langsung)',
  },

  // hold / gate
  holdState: { en: 'Hold state', zh: '挂起状态', id: 'Status tahan' },
  drained: { en: 'Drained', zh: '已排空', id: 'Terkuras' },
  inFlightSends: { en: 'In-flight sends', zh: '在途发送', id: 'Kiriman aktif' },
  addonAck: { en: 'AddOn ack', zh: 'AddOn 回执', id: 'Ack AddOn' },
  noAckYet: { en: 'no ack yet', zh: '暂无回执', id: 'belum ada ack' },
  gateReady: { en: 'Gate ready', zh: '闸门就绪', id: 'Gerbang siap' },
  gateLegs: { en: 'Legs', zh: '检查项', id: 'Pemeriksaan' },

  // W-ADDON-ONE-CLICK — plain-words job statuses (the spec's exact wording).
  // The server sends a raw step name; the page renders the owner-facing
  // sentence and falls back to the raw step for an unknown one.
  addonStatusBuilding: {
    en: 'Building the NinjaTrader AddOn…',
    zh: '正在构建 NinjaTrader AddOn…',
    id: 'Membangun AddOn NinjaTrader…',
  },
  addonStatusInstalling: {
    en: 'Installing the NinjaTrader AddOn…',
    zh: '正在安装 NinjaTrader AddOn…',
    id: 'Memasang AddOn NinjaTrader…',
  },
  addonStatusRestarting: {
    en: 'Restarting NinjaTrader…',
    zh: '正在重启 NinjaTrader…',
    id: 'Memulai ulang NinjaTrader…',
  },
  addonStatusReconnecting: {
    en: 'Waiting for NinjaTrader to reconnect…',
    zh: '等待 NinjaTrader 重新连接…',
    id: 'Menunggu NinjaTrader terhubung kembali…',
  },
  addonStatusDone: {
    en: 'Done — NinjaTrader is running the new AddOn',
    zh: '完成 — NinjaTrader 正在运行新的 AddOn',
    id: 'Selesai — NinjaTrader menjalankan AddOn baru',
  },
  retryInstall: { en: 'Retry', zh: '重试', id: 'Coba lagi' },
  retrying: { en: 'Retrying…', zh: '重试中…', id: 'Mencoba lagi…' },
  retryAccepted: {
    en: 'Retry accepted',
    zh: '重试已接受',
    id: 'Percobaan ulang diterima',
  },
  retryBlockerHelp: {
    en: 'This update is held. Type your VL login password and Retry — nothing to type in a terminal.',
    zh: '此更新已挂起。输入你的 VL 登录密码并点击重试 — 无需在终端输入任何命令。',
    id: 'Pembaruan ini tertahan. Ketik kata sandi login VL Anda dan Coba lagi — tidak ada yang perlu diketik di terminal.',
  },

  // W-ADDON-ONE-CLICK "walk away" result banner — the plan's exact plain words.
  resultDone: {
    en: 'Update done — running {tag}',
    zh: '更新完成 — 正在运行 {tag}',
    id: 'Pembaruan selesai — menjalankan {tag}',
  },
  resultFailed: {
    en: 'Update could not finish — nothing changed, still running {tag}, trading normal. Reason: {reason}',
    zh: '更新未完成 — 没有更改，仍在运行 {tag}，交易正常。原因：{reason}',
    id: 'Pembaruan tidak selesai — tidak ada perubahan, masih menjalankan {tag}, trading normal. Alasan: {reason}',
  },
  // fix/update-says-failed: a REFUSED or reverted_clean job is a FAILURE, not
  // a "still trading normal" rollback — the red banner, the plain words.
  resultFailedRed: {
    en: 'Update failed — nothing changed: {reason}',
    zh: '更新失败 — 没有更改：{reason}',
    id: 'Pembaruan gagal — tidak ada perubahan: {reason}',
  },
  resultRecovery: {
    en: 'Update needs your attention — the automatic recovery failed. Reason: {reason}',
    zh: '更新需要你的注意 — 自动恢复失败。原因：{reason}',
    id: 'Pembaruan perlu perhatian Anda — pemulihan otomatis gagal. Alasan: {reason}',
  },

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
  // fix/update-says-failed: a gate-blocked (non-terminal) job is WAITING, not
  // installing — the plain reason + the elapsed time since the blocker landed.
  waitingFor: {
    en: 'Waiting: {reason}',
    zh: '等待：{reason}',
    id: 'Menunggu: {reason}',
  },
  waitingElapsed: {
    en: '({time} elapsed)',
    zh: '（已等待 {time}）',
    id: '({time} berlalu)',
  },
  // The sim_accounts gate leg, plain words (the server sends
  // "sim_accounts: <detail>" — the detail carries the last-seen time once the
  // account list is persisted, and the old "… not SIM-tradeable" before then).
  blockerSimAccounts: {
    en: 'NinjaTrader accounts not confirmed SIM ({detail})',
    zh: 'NinjaTrader 账户未确认 SIM（{detail}）',
    id: 'Akun NinjaTrader belum dikonfirmasi SIM ({detail})',
  },
  jobError: { en: 'Reason', zh: '原因', id: 'Alasan' },
  jobRecovery: { en: 'Recovery', zh: '恢复原因', id: 'Pemulihan' },
  installPending: {
    en: 'Install pending',
    zh: '待安装',
    id: 'Instalasi tertunda',
  },
  installPendingFrozen: {
    en: 'new entries frozen (exits and protection continue)',
    zh: '新开仓已冻结（平仓与保护继续）',
    id: 'entri baru dibekukan (keluar dan proteksi tetap jalan)',
  },
  installPendingClearsBy: {
    en: 'clears when the job ends, at the latest',
    zh: '任务结束时解除，最迟',
    id: 'lepas saat pekerjaan selesai, paling lambat',
  },
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
