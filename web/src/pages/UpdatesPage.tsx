// W-ONE-BUTTON M5 (U2) — the Updates page: the owner's face of M2/M3/M4.
//
// Truth rules (CLAUDE-canon "Updates page truth rules"): this page renders
// ONLY what the API returns and nothing it does not. A value the binary
// cannot know prints n/a. An absent job is "no update job", never an empty
// timeline. Every blocker is shown with the server's exact text.

import { useCallback, useEffect, useMemo, useState } from 'react'
import { Download, Loader2, RefreshCw, ShieldAlert } from 'lucide-react'
import { useLanguage } from '../contexts/LanguageContext'
import { up } from '../i18n/updates-translations'
import { storageMigrationLine } from '../lib/storageMigration'
import { GUIDE_BUILT_REV } from '../guide/types'
import {
  INSTALL_AUTHZ_UNDER_REVIEW,
  parseInstallAuthorization,
  updatesApi,
  type HealthStatus,
  type InstallationGate,
  type MaintenanceStatusView,
  type UpdateJobView,
  type UpdatesCheck,
  type UpdatesStatus,
} from '../lib/api/updates'
import { strategyEffectiveApi } from '../lib/api/strategyEffective'
import { traderApi } from '../lib/api/traders'
import { configApi } from '../lib/api/config'
import { httpClient } from '../lib/httpClient'
import { loadStoredTraderId } from '../router/selectedTrader'

const POLL_MS = 10_000

function na(lang: 'en' | 'zh' | 'id') {
  return up('notApplicable', lang)
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-start justify-between gap-4 py-1.5">
      <span className="text-xs text-zinc-500 shrink-0">{label}</span>
      <span className="text-sm text-zinc-200 text-right break-all font-mono">
        {value}
      </span>
    </div>
  )
}

function Panel({
  title,
  children,
}: {
  title: string
  children: React.ReactNode
}) {
  return (
    <div className="bg-zinc-900/60 backdrop-blur-xl border border-zinc-800/80 rounded-2xl p-5">
      <h3 className="text-sm font-semibold text-white mb-3">{title}</h3>
      {children}
    </div>
  )
}

// The last polled update-job id, per tab (UI-GUIDE N-5). Every access is
// guarded: storage can be absent or throw (private window, blocked site
// data, previews) and the page must still render.
const LAST_JOB_KEY = 'vl.updates.lastJobID'

function readLastJobID(): string | null {
  try {
    return window.sessionStorage.getItem(LAST_JOB_KEY)
  } catch {
    return null
  }
}

function writeLastJobID(id: string) {
  try {
    window.sessionStorage.setItem(LAST_JOB_KEY, id)
  } catch {
    // memory only — the panel still shows the job until the tab reloads
  }
}

export default function UpdatesPage() {
  const { language } = useLanguage()

  const [health, setHealth] = useState<HealthStatus | null>(null)
  const [maintenance, setMaintenance] = useState<MaintenanceStatusView | null>(
    null
  )
  const [gate, setGate] = useState<InstallationGate | null>(null)
  const [status, setStatus] = useState<UpdatesStatus | null>(null)
  const [check, setCheck] = useState<UpdatesCheck | null>(null)
  const [checking, setChecking] = useState(false)
  const [checkError, setCheckError] = useState<{
    status?: number
    reason: string
  } | null>(null)
  const [installError, setInstallError] = useState<string | null>(null)
  const [notEnrolled, setNotEnrolled] = useState(false)
  const [job, setJob] = useState<UpdateJobView | null>(null)

  // UPDATER-USABLE-V1: the paste box + the install action.
  const [authzText, setAuthzText] = useState('')
  const [installing, setInstalling] = useState(false)
  const [installJobId, setInstallJobId] = useState<string | null>(null)
  // install-with-password (owner order 10-02 07:3x CT): the PRIMARY flow.
  const [password, setPassword] = useState('')
  const [advancedOpen, setAdvancedOpen] = useState(false)

  // Panel E — the resolved PivotWindow (W1 effective settings), or null.
  const [pivotWindow, setPivotWindow] = useState<number | null>(null)
  const [backfillTraderId, setBackfillTraderId] = useState<string | null>(null)
  const [futuresSymbol, setFuturesSymbol] = useState<string | null>(null)
  const [confirmBackfill, setConfirmBackfill] = useState(false)
  const [historyReloading, setHistoryReloading] = useState(false)
  const [historyReply, setHistoryReply] = useState<string | null>(null)

  const poll = useCallback(async () => {
    const [h, m, g, sr] = await Promise.all([
      updatesApi.health(),
      updatesApi.maintenance(),
      updatesApi.installationGate(),
      updatesApi.updatesStatus(),
    ])
    if (h) setHealth(h)
    if (m) setMaintenance(m)
    if (g) setGate(g)
    if (sr.status) setStatus(sr.status)
    return sr.statusCode
  }, [])

  useEffect(() => {
    let alive = true
    let timer: number | null = null
    const tick = async () => {
      const code = await poll()
      if (!alive) return
      if (code === 403) {
        // not-enrolled: the server refuses with its own text and logs a WARN
        // per request — stop the page poll instead of 6 more per minute ([5]).
        setNotEnrolled(true)
        return
      }
      timer = window.setTimeout(tick, POLL_MS)
    }
    tick()
    return () => {
      alive = false
      if (timer !== null) window.clearTimeout(timer)
    }
  }, [poll])

  // Job fetch: while the API names a job, AND after the hold clears — the
  // LAST id the API named keeps being polled, so the terminal state
  // (complete / rolled_back) is the last thing shown, never a frozen
  // snapshot of the moment the hold appeared (#206 review fold). Absent job
  // = "no update job".
  //
  // UI-GUIDE N-5: a job refused IN PREFLIGHT never writes the hold, so the
  // hold never names it — the install-pending view does (job_id), and it is
  // a polled id too. The last id is kept in sessionStorage (this tab only)
  // so a reload does not drop a refused job from the panel; storage that
  // throws (private window, blocked site data) falls back to memory.
  const [lastJobID, setLastJobIDState] = useState<string | null>(readLastJobID)
  const setLastJobID = useCallback((id: string) => {
    setLastJobIDState(id)
    writeLastJobID(id)
  }, [])
  const polledJobID =
    maintenance?.job_id ?? maintenance?.install_pending?.job_id ?? lastJobID
  useEffect(() => {
    if (!polledJobID) {
      setJob(null)
      return
    }
    let alive = true
    const fetch = () => {
      updatesApi.job(polledJobID).then((j) => {
        if (alive) setJob(j)
      })
    }
    fetch()
    const id = window.setInterval(fetch, POLL_MS)
    return () => {
      alive = false
      window.clearInterval(id)
    }
  }, [polledJobID])

  // Remember the id the API named: it stays the polled id once the hold
  // clears (the job route reads the worker's own job file, which outlives
  // the hold).
  useEffect(() => {
    if (maintenance?.job_id) setLastJobID(maintenance.job_id)
    else if (maintenance?.install_pending?.job_id)
      setLastJobID(maintenance.install_pending.job_id)
  }, [maintenance?.job_id, maintenance?.install_pending?.job_id, setLastJobID])

  // The receipt download (OQ-7): the route sits behind the M3 gate, so a bare
  // navigation 403s — the receipt is fetched through the API client (which
  // sends X-VL-Update) and saved as a file. A refusal shows the server's
  // own text, never a fabricated one.
  const [receiptBusy, setReceiptBusy] = useState(false)
  const [receiptError, setReceiptError] = useState<string | null>(null)
  const downloadReceipt = useCallback(async () => {
    const id = polledJobID
    if (!id || receiptBusy) return
    setReceiptBusy(true)
    setReceiptError(null)
    try {
      const res = await updatesApi.receipt(id)
      if (!res?.data) {
        // The server's own text when it said one; nothing fabricated when it
        // did not (the button simply stops spinning).
        setReceiptError(res?.error ?? null)
        return
      }
      const blob = new Blob([JSON.stringify(res.data, null, 2)], {
        type: 'application/json',
      })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `receipt-${id}.json`
      document.body.appendChild(a)
      a.click()
      a.remove()
      URL.revokeObjectURL(url)
    } finally {
      setReceiptBusy(false)
    }
  }, [polledJobID, receiptBusy, language])

  // Panel E — resolve PivotWindow, the trader id and the trader's futures
  // symbol (READ from the trader row via its exchange config — never a
  // literal) from the selected trader.
  useEffect(() => {
    let alive = true
    Promise.all([traderApi.getTraders(true), configApi.getExchangeConfigs()])
      .then(([traders, exchanges]) => {
        const selected = loadStoredTraderId()
        const trader =
          traders.find((t) => t.trader_id === selected) ??
          (traders.length ? traders[0] : undefined)
        const strategyId = trader?.strategy_id
        if (!alive) return
        if (trader) setBackfillTraderId(trader.trader_id)
        const match = trader?.exchange_id
          ? exchanges.find((e) => e.id === trader.exchange_id)
          : undefined
        // The list endpoint returns nt_instrument_name (api/handler_exchange.go:96);
        // the shared Exchange type predates it — read it here, page-local.
        const symbol =
          (match as { nt_instrument_name?: string } | undefined)
            ?.nt_instrument_name ?? null
        setFuturesSymbol(symbol ? (symbol.length > 0 ? symbol : null) : null)
        if (!strategyId) {
          setPivotWindow(null)
          return
        }
        strategyEffectiveApi
          .getStrategyEffective(strategyId)
          .then((resp) => {
            if (!alive) return
            const knob = resp?.settings.find(
              (k) => k.path === 'picture_htf.pivot_window'
            )
            const v = knob?.effective
            setPivotWindow(
              typeof v === 'number' && Number.isFinite(v) ? v : null
            )
          })
          .catch(() => alive && setPivotWindow(null))
      })
      .catch(() => {
        if (alive) {
          setPivotWindow(null)
          setFuturesSymbol(null)
          setBackfillTraderId(null)
        }
      })
    return () => {
      alive = false
    }
  }, [])

  // ── Panel B: two buttons, driven ONLY by the API + the pasted authz ───────
  // The paste box parses the ONE line `vl-updater-bootstrap authorize` prints.
  // Shape is checked in the browser (expires_at must be a JSON number) but
  // the MAC is the SERVER's to verify — the parsed body goes verbatim, never
  // retyped, so no other encoding can alias the MAC's decimal text.
  const authz = useMemo(
    () => (authzText.trim() ? parseInstallAuthorization(authzText) : null),
    [authzText]
  )

  // Check asks the bot whether an update exists (it never installs).
  // The label reflects the actual answer (owner-facing bug 15:1x CT): a
  // verified_ready answer is NOT "Up to date". available (ready or not) ->
  // "Check again" (the green banner names the version); checked without an
  // available release -> "Up to date"; never checked -> "Check".
  const checkLabel = checking
    ? up('checking', language)
    : check?.available === true
      ? up('checkAgain', language)
      : check?.checked
        ? up('upToDate', language)
        : up('check', language)

  const doCheck = useCallback(async () => {
    setChecking(true)
    setCheckError(null)
    try {
      const c = await updatesApi.check()
      setCheck(c)
    } catch (e) {
      // A refusal (e.g. the 403 cross-origin answer on a non-8080 origin)
      // must not leave the button on "Checking…" forever: render the
      // status + the server's own reason under the button.
      const err = e as
        | {
            statusCode?: number
            response?: {
              status?: number
              data?: { error?: string; message?: string }
            }
            message?: string
          }
        | undefined
      setCheckError({
        status: err?.statusCode ?? err?.response?.status,
        reason:
          err?.response?.data?.error ||
          err?.response?.data?.message ||
          err?.message ||
          'check refused',
      })
    } finally {
      setChecking(false)
    }
  }, [])

  // The install control's enabled state comes from the SERVER
  // (install_enabled AND worker_listening, both measured) AND the review
  // constant — no other source (#206's ruling).
  const workerDown =
    status?.install_enabled === true && status?.worker_listening === false
  const installDisabled =
    INSTALL_AUTHZ_UNDER_REVIEW ||
    status?.install_enabled !== true ||
    status?.worker_listening !== true

  const installLabel = installing
    ? up('installing', language)
    : installDisabled
      ? up('blocked', language)
      : up('updateNow', language)

  const doInstall = useCallback(async () => {
    // The belt: the disabled button cannot be clicked, and the constant
    // gates the call itself too, so no install POST can ever fire while
    // the review is open (pinned by UpdatesPage.authzReview.test.tsx).
    if (INSTALL_AUTHZ_UNDER_REVIEW || installing) return
    if (!authz || !authz.ok) return
    setInstalling(true)
    setInstallError(null)
    const res = await updatesApi.install(authz.body)
    // The authorization is single use and is spent on ANY attempt — success
    // or refusal — so the box empties either way (a retried code answers
    // 409; a fresh one must be pasted).
    setAuthzText('')
    if (res.ok && res.job_id) {
      // 202 {job_id}: the Job panel now polls this id (the same remembered-id
      // poll as a hold-named one) and the receipt downloads beside it.
      setLastJobID(res.job_id)
      setInstallJobId(res.job_id)
    } else {
      // 400/403/409/422/503: the SERVER's own text, shown verbatim, never a
      // fabricated reason.
      setInstallError(res.error || 'install refused')
    }
    setInstalling(false)
  }, [authz, installing])

  // install-with-password (owner order 10-02 07:3x CT): the PRIMARY
  // flow — the release id comes from the last Check the SERVER affirmed; the
  // password is sent only in the request body and never rendered anywhere.
  const releaseId = check?.tag || null
  const availableReady = check?.available === true && check?.ready === true
  const passwordInstallDisabled =
    installDisabled ||
    installing ||
    !password.trim() ||
    !releaseId ||
    !availableReady

  const doInstallWithPassword = useCallback(async () => {
    if (INSTALL_AUTHZ_UNDER_REVIEW || installing) return
    if (!releaseId || !password.trim()) return
    setInstalling(true)
    setInstallError(null)
    const res = await updatesApi.installWithPassword(releaseId, password)
    if (res.ok && res.job_id) {
      setLastJobID(res.job_id)
      setInstallJobId(res.job_id)
      setPassword('')
    } else {
      // the SERVER's own text verbatim (wrong password 403 counted, lockout
      // 429 with the unlock time, not enrolled 403, release not verified 422)
      setInstallError(res.error || 'install refused')
    }
    setInstalling(false)
  }, [releaseId, password, installing])

  const askReloadHistory = useCallback(() => {
    setHistoryReply(null)
    setConfirmBackfill(true)
  }, [])

  const cancelReloadHistory = useCallback(() => {
    setConfirmBackfill(false)
    setHistoryReply(null)
  }, [])

  const confirmReloadHistory = useCallback(async () => {
    if (pivotWindow === null || !futuresSymbol) return
    setHistoryReloading(true)
    setHistoryReply(null)
    try {
      const res = await httpClient.request<{
        ok?: boolean
        note?: string
        error?: string
      }>('/api/nt/bar-arbiter', {
        method: 'POST',
        data: {
          trader_id: backfillTraderId ?? '',
          action: 'backfill',
          symbol: futuresSymbol,
          timeframe: '4h',
          bars_back: pivotWindow + 4,
        },
      })
      setHistoryReply(
        res.data?.error ||
          res.data?.note ||
          (res.success ? 'backfill request accepted' : res.message) ||
          'no server text'
      )
    } catch (e) {
      setHistoryReply(e instanceof Error ? e.message : 'request failed')
    } finally {
      setHistoryReloading(false)
      setConfirmBackfill(false)
    }
  }, [pivotWindow, futuresSymbol, backfillTraderId])

  const state = maintenance?.state ?? null
  const ack = maintenance?.addon_ack ?? null
  const completedBarsN = pivotWindow !== null ? pivotWindow + 4 : null

  // UPDATER-NT8-CLOSED item 5: the install surface is loopback :8080 only.
  // A page opened from a non-8080 origin (the :3000 dev server) can never
  // pass the origin gate — the page says so plainly instead of a bare
  // cross-origin 403. The origin check itself is UNCHANGED: this is a hint,
  // never a bypass.
  const non8080Origin =
    typeof window !== 'undefined' &&
    window.location.port !== '' &&
    window.location.port !== '8080'

  return (
    <div className="space-y-5" data-testid="updates-page">
      {notEnrolled && (
        <div className="rounded-lg border border-amber-600/40 bg-amber-500/10 px-4 py-2 text-xs text-amber-300">
          {up('pollingStopped', language)}
        </div>
      )}
      <p className="text-xs text-zinc-500">
        {storageMigrationLine(window.localStorage, window.location.origin)}
      </p>
      {/* Panel A — Running now */}
      <Panel title={up('runningNow', language)}>
        <Row
          label={up('revision', language)}
          value={health?.revision || na(language)}
        />
        <Row
          label={up('guideRev', language)}
          value={GUIDE_BUILT_REV || na(language)}
        />
        <Row
          label={up('addonBuild', language)}
          value={ack?.build_id || na(language)}
        />
        <Row label={up('mvidMatch', language)} value={na(language)} />
      </Panel>

      {/* Panel B — Update */}
      <Panel title={up('updatePanel', language)}>
        {non8080Origin && (
          <p
            className="mb-2 text-xs text-amber-400"
            data-testid="non-8080-origin"
          >
            {up('openOn8080', language, { port: window.location.port })}
          </p>
        )}
        <div className="flex items-center gap-3">
          <button
            type="button"
            disabled={checking}
            onClick={doCheck}
            className="inline-flex items-center gap-2 rounded-lg px-4 py-2 text-sm font-medium bg-zinc-700 text-zinc-100 disabled:opacity-50"
            data-testid="check-button"
          >
            {checking && <Loader2 size={15} className="animate-spin" />}
            {checkLabel}
          </button>
        </div>
        {checkError && (
          <p className="mt-2 text-xs text-red-400" data-testid="check-error">
            {up('checkRefused', language, {
              status: checkError.status ? ` (${checkError.status})` : '',
              reason: checkError.reason,
            })}
          </p>
        )}
        <label
          htmlFor="updates-password"
          className="mt-3 block text-xs text-zinc-400"
        >
          {up('installPasswordLabel', language)}
        </label>
        <input
          id="updates-password"
          data-testid="install-password"
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="current-password"
          spellCheck={false}
          className="mt-1 w-full rounded-lg border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-100 placeholder-zinc-600 focus:outline-none focus:border-zinc-500"
          placeholder={up('installPasswordPlaceholder', language)}
        />
        <button
          type="button"
          disabled={passwordInstallDisabled}
          onClick={doInstallWithPassword}
          className="mt-3 inline-flex items-center gap-2 rounded-lg px-4 py-2 text-sm font-medium bg-vl-neo-gold text-black disabled:opacity-50"
          data-testid="update-button"
        >
          {installing && <Loader2 size={15} className="animate-spin" />}
          {installLabel}
        </button>
        {!releaseId && (
          <p
            className="mt-2 text-xs text-zinc-500"
            data-testid="check-first-hint"
          >
            {up('checkFirstForUpdate', language)}
          </p>
        )}
        <div className="mt-4 border-t border-zinc-800 pt-3">
          <button
            type="button"
            onClick={() => setAdvancedOpen((v) => !v)}
            className="text-xs text-zinc-500 hover:text-zinc-300"
            data-testid="advanced-toggle"
          >
            {up('advancedInstallCode', language)}
          </button>
          {advancedOpen && (
            <>
              <label
                htmlFor="updates-authz"
                className="mt-3 block text-xs text-zinc-400"
              >
                {up('authzLabel', language)}
              </label>
              <textarea
                id="updates-authz"
                data-testid="authz-paste"
                value={authzText}
                onChange={(e) => setAuthzText(e.target.value)}
                rows={2}
                spellCheck={false}
                className="mt-1 w-full rounded-lg border border-zinc-700 bg-zinc-950 px-3 py-2 text-xs font-mono text-zinc-200 placeholder-zinc-600 focus:outline-none focus:border-zinc-500"
                placeholder='{"release_id":"…","job_id":"…","expires_at":…,"hmac":"…"}'
              />
              <button
                type="button"
                disabled={installDisabled || !authz?.ok || installing}
                onClick={doInstall}
                className="mt-2 inline-flex items-center gap-2 rounded-lg px-4 py-2 text-sm font-medium bg-zinc-700 text-zinc-100 disabled:opacity-50"
                data-testid="update-button-advanced"
              >
                {up('updateNow', language)}
              </button>
            </>
          )}
        </div>
        {authz?.ok === false && advancedOpen && (
          <p
            className="mt-1 text-xs text-amber-400"
            data-testid="authz-parse-error"
          >
            {authz.error}
          </p>
        )}
        {installJobId && (
          <p
            className="mt-2 text-xs text-emerald-400"
            data-testid="install-accepted"
          >
            {up('installAccepted', language)}: {installJobId}
          </p>
        )}
        {INSTALL_AUTHZ_UNDER_REVIEW && (
          <p className="mt-2 text-xs text-amber-400 flex items-center gap-1.5">
            <ShieldAlert size={13} />
            {up('installUnderReview', language)}
          </p>
        )}
        {workerDown && (
          <p
            className="mt-2 text-xs text-amber-400 flex items-center gap-1.5"
            data-testid="worker-not-running"
          >
            <ShieldAlert size={13} />
            {up('workerNotRunning', language)}
          </p>
        )}
        {installError && (
          <p className="mt-2 text-xs text-red-400" data-testid="install-error">
            {installError}
          </p>
        )}
        {check && (
          <p className="mt-2 text-xs text-zinc-400">
            {up('checkReason', language)}: {check.reason || na(language)}
          </p>
        )}
        {check?.available === true && check?.ready === true && (
          <p
            className="mt-2 text-xs text-emerald-400 font-semibold"
            data-testid="update-available"
          >
            {up('updateAvailableVerified', language).replace(
              '{tag}',
              check.tag || ''
            )}
          </p>
        )}
        {check?.available === true && check?.ready !== true && (
          <p
            className="mt-2 text-xs text-amber-400"
            data-testid="update-not-ready"
          >
            {up('updateNotReady', language).replace('{tag}', check.tag || '')}
          </p>
        )}
        {check?.checked === false &&
          /rate limited/i.test(check?.reason || '') && (
            <p
              className="mt-2 text-xs text-amber-400"
              data-testid="check-rate-limited"
            >
              {up('rateLimited', language)}
            </p>
          )}
        {(status?.install_state === 'downloading' ||
          status?.install_state === 'verifying') && (
          <p
            className="mt-2 text-xs text-sky-400 flex items-center gap-1.5"
            data-testid="install-state"
          >
            <Loader2 size={13} className="animate-spin" />
            {status.install_state === 'downloading'
              ? up('downloading', language)
              : up('verifying', language)}
          </p>
        )}
      </Panel>

      {/* Panel C — Maintenance hold + gate */}
      <Panel title={up('maintenancePanel', language)}>
        <Row label={up('holdState', language)} value={state || na(language)} />
        {maintenance?.job_id !== undefined && maintenance?.job_id !== null && (
          <Row label={up('jobId', language)} value={maintenance.job_id} />
        )}
        {maintenance?.since !== undefined && maintenance?.since !== null && (
          <Row label="Since" value={maintenance.since} />
        )}
        {/* release #22: an update job in preflight freezes NEW entries
            (exits and protection continue) until it ends or times out. */}
        {maintenance?.install_pending?.pending && (
          <div data-testid="install-pending">
            <Row
              label={up('installPending', language)}
              value={`${up('installPendingFrozen', language)} · job=${maintenance.install_pending.job_id ?? na(language)} · ${maintenance.install_pending.state ?? na(language)}/${maintenance.install_pending.phase ?? na(language)} · ${up('installPendingClearsBy', language)} ${maintenance.install_pending.clears_by ?? na(language)}`}
            />
          </div>
        )}
        <Row
          label={up('inFlightSends', language)}
          value={String(maintenance?.in_flight_sends ?? 0)}
        />
        <Row
          label={up('drained', language)}
          value={maintenance ? String(maintenance.drained) : na(language)}
        />
        <Row
          label={up('addonAck', language)}
          value={
            ack
              ? `${ack.held ? 'held' : 'released'} · job=${ack.job_id || na(language)} · build=${ack.build_id || na(language)}`
              : up('noAckYet', language)
          }
        />

        <div className="border-t border-zinc-800 mt-4 pt-4">
          <Row
            label={up('gateReady', language)}
            value={gate ? String(gate.ready) : na(language)}
          />
          <div className="mt-2 space-y-1.5">
            {(gate?.legs ?? []).map((leg) => (
              <div
                key={leg.name}
                className="flex items-start justify-between gap-3 text-xs"
              >
                <span
                  className={`shrink-0 px-1.5 py-0.5 rounded ${
                    leg.pass
                      ? 'bg-emerald-500/15 text-emerald-400'
                      : 'bg-red-500/15 text-red-400'
                  }`}
                >
                  {leg.pass ? 'PASS' : 'FAIL'}
                </span>
                <span className="text-zinc-300 font-mono">{leg.name}</span>
                <span className="text-zinc-400 text-right flex-1">
                  {leg.detail}
                </span>
              </div>
            ))}
            {!gate && <p className="text-xs text-zinc-500">{na(language)}</p>}
          </div>
        </div>
      </Panel>

      {/* Panel D — Job */}
      <Panel title={up('jobPanel', language)}>
        {!maintenance?.job_id && !job?.job_id ? (
          <p className="text-sm text-zinc-400" data-testid="no-job">
            {up('noUpdateJob', language)}
          </p>
        ) : (
          <>
            <Row
              label={up('jobId', language)}
              value={job?.job_id || maintenance?.job_id || na(language)}
            />
            <Row
              label={up('jobState', language)}
              value={
                job?.state ||
                (job?.error ? `${job.status ?? ''} ${job.error}` : na(language))
              }
            />
            {job?.step && (
              <Row label={up('jobStep', language)} value={job.step} />
            )}
            {job?.blocker && (
              <div data-testid="job-blocker">
                <Row label={up('jobBlocker', language)} value={job.blocker} />
              </div>
            )}
            {/* release #22: the failure reason was rendered only when the job
                had no state, and a refused job always has one — the owner saw
                "refused" and never why. Shown verbatim whenever present. */}
            {job?.state && job?.error && (
              <div data-testid="job-error">
                <Row label={up('jobError', language)} value={job.error} />
              </div>
            )}
            {/* UI-GUIDE N-5: stale at start / stale at resume / the attempts
                cap put a job in recovery_needed with ONLY a recovery reason
                (no error) — shown verbatim, as the server wrote it. */}
            {job?.recovery_reason && (
              <div data-testid="job-recovery">
                <Row
                  label={up('jobRecovery', language)}
                  value={job.recovery_reason}
                />
              </div>
            )}
            {job?.timestamps && Object.keys(job.timestamps).length > 0 && (
              <div
                className="mt-3 border-t border-zinc-800 pt-3 space-y-1.5"
                data-testid="job-timestamps"
              >
                {Object.entries(job.timestamps).map(([state, at]) => (
                  <Row key={state} label={state} value={at} />
                ))}
              </div>
            )}
            {job?.receipt_url && (
              <div className="mt-2">
                <button
                  type="button"
                  onClick={downloadReceipt}
                  disabled={receiptBusy}
                  className="inline-flex items-center gap-1.5 text-xs text-vl-neo-gold hover:underline disabled:opacity-60"
                  data-testid="receipt-link"
                >
                  {receiptBusy ? (
                    <Loader2 size={13} className="animate-spin" />
                  ) : (
                    <Download size={13} />
                  )}
                  {up('downloadReceipt', language)}
                </button>
                {receiptError && (
                  <p className="mt-1 text-xs text-red-400">{receiptError}</p>
                )}
              </div>
            )}
          </>
        )}
      </Panel>

      {/* Panel E — Native history readiness */}
      <Panel title={up('historyPanel', language)}>
        <Row label={up('tradingPaused', language)} value={na(language)} />
        <Row
          label={up('completedBars', language)}
          value={
            completedBarsN !== null
              ? `${na(language)} of ${completedBarsN} (${up('pivotWindow', language)}+4)`
              : `${na(language)} of ${na(language)} (${up('pivotWindow', language)}+4)`
          }
        />
        <Row label={up('lastProgress', language)} value={na(language)} />
        <div className="mt-3">
          <button
            type="button"
            onClick={askReloadHistory}
            disabled={pivotWindow === null || futuresSymbol === null}
            className="inline-flex items-center gap-2 rounded-lg px-3 py-1.5 text-xs font-medium bg-zinc-800 hover:bg-zinc-700 text-zinc-200 disabled:opacity-50"
            data-testid="reload-history"
          >
            <RefreshCw size={13} />
            {up('reloadHistory', language)}
          </button>
          {pivotWindow === null && (
            <p
              className="mt-2 text-xs text-amber-400"
              data-testid="pivot-unknown"
            >
              {up('pivotWindowUnknown', language)}
            </p>
          )}
          {futuresSymbol === null && (
            <p
              className="mt-2 text-xs text-amber-400"
              data-testid="symbol-unknown"
            >
              {up('futuresSymbolUnknown', language)}
            </p>
          )}
          {confirmBackfill &&
            pivotWindow !== null &&
            futuresSymbol !== null && (
              <div className="mt-2 rounded-lg border border-zinc-700 p-3">
                <p
                  className="text-xs text-zinc-300"
                  data-testid="backfill-confirm"
                >
                  {up('confirmBackfill', language, { n: pivotWindow + 4 })}
                </p>
                <div className="mt-2 flex gap-2">
                  <button
                    type="button"
                    onClick={confirmReloadHistory}
                    disabled={historyReloading}
                    className="rounded-lg px-3 py-1.5 text-xs font-medium bg-vl-neo-gold text-black disabled:opacity-50"
                    data-testid="confirm-backfill"
                  >
                    {historyReloading
                      ? up('reloading', language)
                      : up('confirm', language)}
                  </button>
                  <button
                    type="button"
                    onClick={cancelReloadHistory}
                    className="rounded-lg px-3 py-1.5 text-xs font-medium bg-zinc-700 text-zinc-200"
                    data-testid="cancel-backfill"
                  >
                    {up('cancel', language)}
                  </button>
                </div>
              </div>
            )}
          {historyReply && (
            <p
              className="mt-2 text-xs text-zinc-400"
              data-testid="backfill-reply"
            >
              {up('checkReason', language)}: {historyReply}
            </p>
          )}
        </div>
      </Panel>
    </div>
  )
}
