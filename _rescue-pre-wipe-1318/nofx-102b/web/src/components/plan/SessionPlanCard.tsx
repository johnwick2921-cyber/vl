import { StructuralGeometry } from './StructuralGeometry'
import { PlanLiveness } from './PlanLiveness'
// P4.3 / P5 — the SessionPlanCard: composes bias / mini-chart / levels /
// scenarios / rules / footer, and renders every lifecycle state. It is a pure
// VIEW of plan_final; the OWNER DOOR (P5) hangs off the header: ✎ opens the edit
// sheet, 💬 opens Ask-Planner. Edits/applies bump the version via onChanged.

import { useState, type ReactNode } from 'react'
import type { Language } from '../../i18n/translations'
import { tp } from '../../i18n/plan-translations'
import type {
  PlanToday,
  PlanLevelFact,
  PlanVersionItem,
} from '../../lib/api/plan'
import { LifecycleChip, VersionChips } from './chips'
import { WeeklyChip } from './WeeklyChip'
import { BiasBlock } from './BiasBlock'
import { ZoneTable } from './ZoneTable'
import { LevelZoneMap } from './LevelZoneMap'
import { StructurePanel, structureZonesToOverlay } from './StructurePanel'
import { ScenarioList } from './ScenarioList'
import type { FadeLabelView } from './FadePermissionChip'
import type { OneSetupView } from './OneSetupChip'
import { RulesBlock, type NoTradeBandWindow } from './RulesBlock'
import { ArmedUnderBlock, type OpenPositionProvenance } from './ArmedUnderBlock'
import { PlanFooter } from './PlanFooter'
import { PlanMiniChart } from './PlanMiniChart'
import { EditSheet } from './EditSheet'
import { BulkAddSheet } from './BulkAddSheet'
import { AskPlannerPanel } from './AskPlannerPanel'
import { RealignPanel, RealignButton, type RealignState } from './RealignPanel'
import { api } from '../../lib/api'
import { guardedCall } from '../../lib/api/guarded'
import type { RealignChange } from '../../lib/api/plan'

interface Props {
  plan: PlanToday | null
  traderId?: string
  symbol: string
  exchange: string
  language: Language
  isLoading?: boolean
  errored?: boolean
  onChanged?: () => void // re-fetch after an edit / apply so the card version-bumps
  /** ITEM 15 — every stored version of this session's plan (chips + history). */
  versions?: PlanVersionItem[]
  latestVersion?: number
  /** omit to keep the chips read-only */
  onSelectVersion?: (version: number) => void
}

// A centered state panel (loading / night / no-plan / error).
function StatePanel({
  icon,
  title,
  hint,
  tone,
}: {
  icon: string
  title: string
  hint?: string
  tone?: string
}) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 py-10 text-center">
      <span aria-hidden style={{ fontSize: 26, opacity: 0.7 }}>
        {icon}
      </span>
      <span
        className="text-sm font-semibold"
        style={{
          color: tone ?? 'var(--vl-ivory)',
          fontFamily: 'var(--vl-font-ui)',
        }}
      >
        {title}
      </span>
      {hint && (
        <span
          className="text-[11px] max-w-[260px]"
          style={{ color: 'var(--vl-faint)', fontFamily: 'var(--vl-font-ui)' }}
        >
          {hint}
        </span>
      )}
    </div>
  )
}

function Badge({ children, color }: { children: ReactNode; color: string }) {
  return (
    <span
      className="text-[9px] font-bold uppercase tracking-wider"
      style={{
        color,
        border: `1px solid ${color}`,
        borderRadius: 'var(--vl-radius-chip)',
        padding: '1px 5px',
        fontFamily: 'var(--vl-font-ui)',
      }}
    >
      {children}
    </span>
  )
}

export function SessionPlanCard({
  plan,
  traderId,
  symbol,
  exchange,
  language,
  isLoading,
  errored,
  onChanged,
  versions = [],
  latestVersion = 0,
  onSelectVersion,
}: Props) {
  // P5 owner-door state (hooks before the early state-returns, unconditional).
  const [edit, setEdit] = useState<{
    open: boolean
    level?: PlanLevelFact
    index?: number
  }>({ open: false })
  const [askOpen, setAskOpen] = useState(false)
  const [bulkOpen, setBulkOpen] = useState(false)
  // W13 — plan re-alignment after an owner edit.
  const [realign, setRealign] = useState<RealignState>({ phase: 'idle' })
  const [flash, setFlash] = useState(0) // bump → changed rows flash gold once

  const runRealign = async (change: RealignChange, manual = false) => {
    if (!traderId) return
    setRealign({ phase: 'reviewing' })
    // Stuck-dialog class (reset hotfix): the realign is planner-backed (long);
    // a thrown timeout used to strand the phase at 'reviewing' forever.
    const g = await guardedCall(() =>
      api.realignPlan(traderId, change, symbol, manual)
    )
    if (!g.ok) {
      setRealign({ phase: 'failed' })
      return
    }
    const res = g.value
    switch (res.status) {
      case 'proposal':
        setRealign({ phase: 'proposal', res })
        break
      case 'no-change':
        setRealign({ phase: 'no-change' })
        break
      case 'capped':
        setRealign({ phase: 'capped' })
        break
      case 'failed':
        setRealign({ phase: 'failed' })
        break
      default: // skipped / debounced → stay quiet, the batch is already covered
        setRealign({ phase: 'idle' })
    }
  }
  // The owner door (edit / add / bulk / delete / Ask-Planner / re-align) may only
  // be opened while we are looking at the LIVE session. Every mutating endpoint
  // resolves reg.ActiveSession(now) SERVER-SIDE and writes the active session's
  // plan — it takes no session argument. Once the tabs became real (W15.C) the
  // card could display a sibling session, so leaving the door enabled there would
  // let an edit made against ASIA's levels land silently on NY's plan.
  // is_active comes from /api/plan/today; an older payload omits it (undefined),
  // which keeps the pre-W15.C behavior rather than locking the door.
  // ITEM 15 adds the same hazard on a second axis: every mutating endpoint writes
  // the LATEST version of the active session, so an edit made while reading v2
  // would silently land on v6. A historical view is strictly read-only.
  // P7 (2026-08-17) adds the third axis: a NO-TRADE plan is the terminal marker,
  // not a tradeable plan — its levels are the MAP (market facts), read-only under
  // the ⛔ banner. Editing them would edit a plan the bot is not trading.
  const viewingLiveSession = plan?.is_active !== false
  const doorEnabled =
    !!traderId &&
    viewingLiveSession &&
    !plan?.historical &&
    plan?.lifecycle !== 'no_trade'

  // ITEM 2 (2026-08-17) — the 💬 must be reachable with NO PLAN.
  //
  // Every no-plan state returned a bare StatePanel, so there was no ask
  // affordance at all — and "why did the plan die?" / "why no levels tonight?"
  // are exactly the questions that only exist once the plan is gone. The backend
  // now answers in a labelled HISTORICAL / NO-PLAN context and strips any patch,
  // so opening the thread here cannot be mistaken for, or act on, a live plan.
  const noPlanAsk = traderId ? (
    <>
      <div className="flex justify-center pb-4">
        <button
          type="button"
          data-testid="ask-anyway"
          onClick={() => setAskOpen(true)}
          className="text-[12px] px-3 py-2"
          style={{
            color: 'var(--vl-gold)',
            border: '1px solid var(--vl-gold-line)',
            borderRadius: 'var(--vl-radius-chip)',
            background: 'transparent',
            cursor: 'pointer',
            minHeight: 44,
          }}
        >
          💬 {tp('askAnyway', language)}
        </button>
      </div>
      <AskPlannerPanel
        open={askOpen}
        traderId={traderId}
        symbol={symbol}
        language={language}
        contextLabel={tp('askContextNoPlan', language)}
        onClose={() => setAskOpen(false)}
        onApplied={() => onChanged?.()}
      />
    </>
  ) : null

  // ── non-plan states ──
  if (errored) {
    return (
      <StatePanel
        icon="⛔"
        title={tp('errorFailClosed', language)}
        hint={tp('errorHint', language)}
        tone="var(--vl-short)"
      />
    )
  }
  if (isLoading && !plan) {
    return <StatePanel icon="◌" title={tp('loading', language)} />
  }
  if (!plan || !plan.found) {
    // F7 (2026-08-30) — a read is in flight and NOTHING is committed yet: say
    // "writing" out loud instead of the bare no-plan card (which reads as
    // broken). Once ANY row commits, the plan renders and a running read
    // becomes the subtle replan chip below.
    if (plan?.reading) {
      return (
        <>
          <div
            data-testid="reading-banner"
            className="flex items-center gap-2 px-3 py-2 text-[12px]"
            style={{
              background: 'rgba(212,175,55,0.08)',
              border: '1px solid var(--vl-gold-line)',
              borderRadius: 'var(--vl-radius-chip)',
              color: 'var(--vl-gold)',
            }}
          >
            <span className="vl-spin" aria-hidden>
              ◌
            </span>
            {tp('readingBanner', language)}
          </div>
          {noPlanAsk}
        </>
      )
    }
    if (plan?.night || !plan?.session) {
      return (
        <>
          <StatePanel
            icon="🌙"
            title={tp('night', language)}
            hint={tp('nightHint', language)}
          />
          {noPlanAsk}
        </>
      )
    }
    // enabled session, plan not armed yet (pre-★2 graceful state)
    return (
      <>
        <StatePanel
          icon="🕗"
          title={tp('noPlanYet', language)}
          hint={tp('noPlanYetHint', language)}
        />
        {noPlanAsk}
      </>
    )
  }

  const doc = plan.doc
  if (!doc) {
    return (
      <>
        <StatePanel
          icon="🕗"
          title={tp('noPlanYet', language)}
          hint={tp('noPlanYetHint', language)}
        />
        {noPlanAsk}
      </>
    )
  }

  const failClosed =
    doc.day_type === 'no-trade' ||
    (doc.reasoning || '').startsWith('FAIL-CLOSED')
  const expired =
    plan.lifecycle === 'expired' ||
    plan.lifecycle === 'died' ||
    plan.lifecycle === 'superseded'
  const warming = plan.warming && plan.warming !== ''
  const facts = plan.level_facts ?? []
  // FAIL LOUD (P0 2026-08-17): a plan with no levels must say WHY on the card.
  // v6 of 2026-08-16:ASIA stored `levels: null` with the reason sitting unread in
  // doc.reasoning ("FAIL-CLOSED: re-plans exhausted after death condition …"),
  // so the card showed a bare "No levels in this plan" and the actual fault — the
  // planner killing every version on arrival — stayed invisible for a session.
  const emptyReason =
    facts.length > 0
      ? undefined
      : doc.reasoning?.trim() ||
        (doc.no_trade?.length ? doc.no_trade.join(' · ') : '')

  // ITEM 1 (2026-08-17) — ONE derivation, used by BOTH chip rows. The card
  // renders version chips twice (header + footer); only the header was wired when
  // click-to-open shipped, so the footer row stayed a set of disabled buttons.
  // Tapping those did nothing, which from the owner's chair is indistinguishable
  // from "the chips are still not clickable".
  const newestVersion =
    latestVersion || plan.latest_version || plan.version || 1
  const versionCount =
    versions.length || latestVersion || plan.latest_version || plan.version || 1
  // Every version that has been superseded — i.e. the deaths this session.
  const deadVersions = versions.filter((v) => !v.is_latest)
  // The NO-TRADE TERMINAL MARKER, if the session ended that way. It is NOT a
  // re-plan: cap=4 legitimately produces v1..v5 real + v6 marker, and rendering
  // that marker as a plain "v6" is what read as "the cap didn't work".
  const noTradeVersion =
    versions.find((v) => v.lifecycle === 'no_trade')?.version ??
    (plan.lifecycle === 'no_trade' ? plan.version : undefined)
  // CLASS 35 (2026-09-01) — the card READS the budget, it never computes one:
  // `replans_left` / `replan_cap` are the RECORDED counter the death gate and
  // the executor prompt share. (The old `noTradeVersion − 2 : version − 1` was
  // a third formula, and it counted wake reads and dormant flips as spends.)
  const replanCap = plan.replan_cap
  const replansSpent =
    typeof plan.replan_cap === 'number' && typeof plan.replans_left === 'number'
      ? Math.max(0, plan.replan_cap - plan.replans_left)
      : undefined
  const versionTitle = (v: number) => {
    const rec = versions.find((x) => x.version === v)
    if (!rec) return undefined
    return rec.death_reason
      ? `v${v} — ${rec.death_reason}`
      : `v${v} — ${rec.lifecycle}`
  }

  return (
    <div
      className="flex flex-col gap-3"
      style={{ opacity: expired ? 0.7 : 1 }}
      role="region"
      aria-label={`${tp('title', language)}, v${plan.version ?? 1}, ${plan.lifecycle ?? 'active'}`}
    >
      <PlanLiveness value={plan.scenario_liveness} />
      {/* UI-verification (2026-08-18): the owner tapped Reset while a death
          re-plan was writing and the card showed NOTHING for minutes — the reset
          worked but read as "does nothing". F7 (2026-08-30): once a plan row is
          committed this is the subtle re-reading chip — NEVER the "writing a
          fresh plan" banner, which used to stick for hours over a tradeable
          plan while wake re-reads held the claim back-to-back. */}
      {plan.replan_in_flight && (
        <div
          data-testid="replan-chip"
          className="flex items-center gap-2 px-3 py-2 text-[12px]"
          style={{
            background: 'rgba(212,175,55,0.06)',
            border: '1px solid var(--vl-gold-line)',
            borderRadius: 'var(--vl-radius-chip)',
            color: 'var(--vl-gold)',
          }}
        >
          <span className="vl-spin" aria-hidden>
            ◌
          </span>
          {tp('replanChip', language)}
        </div>
      )}
      {/* DORMANT (plan-lifecycle wave, 2026-08-27) — flip/death line breached;
          entries blocked until price closes back (auto re-arm, same version). */}
      {plan.lifecycle === 'dormant' && (
        <div
          data-testid="dormant-banner"
          className="flex items-center gap-2 px-3 py-2 text-[12px]"
          style={{
            background: 'rgba(212,175,55,0.08)',
            border: '1px solid var(--vl-gold-line)',
            borderRadius: 'var(--vl-radius-chip)',
            color: 'var(--vl-gold)',
          }}
        >
          <span aria-hidden>⏸</span>
          <span>
            dormant:{' '}
            {(plan.trigger_reason ?? '').replace(/^dormant:/, '') ||
              'flip/death line breached'}{' '}
            — entries blocked until price closes back (auto re-arm, same
            version)
          </span>
        </div>
      )}
      {/* header — wraps: at 390px a v6 chip row + SUPERSEDED + DEGRADED overflowed
          the 316px card interior by ~41px and pushed the ✎/💬 buttons off the card. */}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-2 min-w-0">
          <span
            style={{
              fontFamily: 'var(--vl-font-display)',
              fontSize: 20,
              fontWeight: 600,
              color: 'var(--vl-ivory)',
            }}
          >
            {tp('title', language)}
          </span>
          <VersionChips
            version={plan.version ?? 1}
            latest={newestVersion}
            count={versionCount}
            onSelect={onSelectVersion}
            titleFor={versionTitle}
            noTradeVersion={noTradeVersion}
            noTradeLabel={tp('noTradeChip', language)}
          />
          <LifecycleChip
            lifecycle={plan.lifecycle ?? 'active'}
            language={language}
          />
          {/* W7 (weekly-bias wave), corrected 2026-09-06: the chip is REFS-ONLY since
              class 50 (2026-09-02) — "WEEKLY refs — PWH x · PWL y", no directional
              call. It has not been an "advisory view" since then, and this comment
              saying so is what sent a later wave hunting a bug that was already
              fixed. The rendered label is the contract; a comment is not. */}
          <WeeklyChip weekly={plan.weekly} />
          {plan.degraded && (
            <span
              data-testid="degraded-badge"
              title={`${plan.dark_regime_count ?? 0}/7 regime fields were unavailable at the read`}
              style={{
                fontSize: 9,
                fontWeight: 700,
                letterSpacing: '.1em',
                color: 'var(--vl-short)',
                border: '1px solid rgba(224,108,108,.4)',
                background: 'rgba(224,108,108,.08)',
                borderRadius: 5,
                padding: '2px 6px',
              }}
            >
              ⚠ DEGRADED {plan.dark_regime_count ?? 0}/7
            </span>
          )}
        </div>
        <div className="flex items-center gap-1">
          {/* the OWNER DOOR (P5): 💬 Ask-Planner + ✎ add level. Enabled once a
              trader is present; the level rows themselves are tap-to-edit. */}
          <button
            onClick={() => doorEnabled && setAskOpen(true)}
            disabled={!doorEnabled}
            className="text-[13px] px-2 py-1"
            style={{
              color: doorEnabled ? 'var(--vl-gold)' : 'var(--vl-faint)',
              opacity: doorEnabled ? 1 : 0.4,
              fontFamily: 'var(--vl-font-ui)',
              cursor: doorEnabled ? 'pointer' : 'not-allowed',
            }}
            title={tp('askPlannerTitle', language)}
          >
            💬
          </button>
          <button
            onClick={() => doorEnabled && setEdit({ open: true })}
            disabled={!doorEnabled}
            className="text-[13px] px-2 py-1"
            style={{
              color: doorEnabled ? 'var(--vl-gold)' : 'var(--vl-faint)',
              opacity: doorEnabled ? 1 : 0.4,
              fontFamily: 'var(--vl-font-ui)',
              cursor: doorEnabled ? 'pointer' : 'not-allowed',
            }}
            title={tp('edit', language)}
          >
            ✎
          </button>
        </div>
      </div>

      {/* badges + banners */}
      <div className="flex flex-wrap items-center gap-2">
        {plan.mode === 'advisory' && (
          <Badge color="var(--vl-muted)">{tp('modeAdvisory', language)}</Badge>
        )}
        {warming && (
          <Badge color="var(--vl-gold)">
            {tp('warming', language, { n: plan.warming as string })}
          </Badge>
        )}
        {warming && (
          <Badge color="var(--vl-faint)">{tp('uncalibrated', language)}</Badge>
        )}
      </div>
      {plan.mode === 'advisory' && (
        <div
          className="text-[11px] px-2.5 py-1.5"
          style={{
            background: 'var(--vl-gold-dim)',
            border: '1px solid var(--vl-gold-line)',
            borderRadius: 'var(--vl-radius-inner)',
            color: 'var(--vl-muted)',
            fontFamily: 'var(--vl-font-ui)',
          }}
        >
          {tp('advisoryMode', language)}
        </div>
      )}
      {plan.lifecycle === 'no_trade' && (
        <div
          data-testid="no-trade-banner"
          className="flex flex-col gap-1 px-2.5 py-2"
          style={{
            background: 'rgba(224,108,108,0.08)',
            border: '1px solid rgba(224,108,108,0.4)',
            borderRadius: 'var(--vl-radius-inner)',
            fontFamily: 'var(--vl-font-ui)',
          }}
        >
          <span
            className="text-[11px] font-bold uppercase tracking-wider"
            style={{ color: 'var(--vl-short)' }}
          >
            ⛔ {tp('noTradeBanner', language)}
          </span>
          <span className="text-[11px]" style={{ color: 'var(--vl-muted)' }}>
            {tp('noTradeBannerHint', language, {
              used: replansSpent === undefined ? '?' : String(replansSpent),
              cap: replanCap === undefined ? '?' : String(replanCap),
            })}
          </span>
          {/* The planner's own reason must survive the reframing — it is the
              only place the killing condition is written down. */}
          <span
            data-testid="fail-closed-reason"
            className="text-[11px]"
            style={{ color: 'var(--vl-faint)' }}
          >
            {doc.reasoning?.trim() || tp('errorHint', language)}
          </span>
        </div>
      )}
      {failClosed && plan.lifecycle !== 'no_trade' && (
        <div
          className="text-[11px] px-2.5 py-1.5"
          style={{
            background: 'rgba(224,108,108,0.08)',
            border: '1px solid rgba(224,108,108,0.3)',
            borderRadius: 'var(--vl-radius-inner)',
            color: 'var(--vl-short)',
            fontFamily: 'var(--vl-font-ui)',
          }}
        >
          ⛔ {tp('errorFailClosed', language)} —{' '}
          <span data-testid="fail-closed-reason">
            {doc.reasoning?.trim() || tp('errorHint', language)}
          </span>
        </div>
      )}

      {/* ITEM 15 — viewing a superseded version. It must be unmistakable that this
          is NOT what the bot is trading, must say why the version ended, and must
          offer the way back. */}
      {plan.historical && (
        <div
          data-testid="historical-banner"
          className="flex flex-col gap-1.5 px-2.5 py-2"
          style={{
            background: 'var(--vl-card-2)',
            border: '1px solid var(--vl-gold-line)',
            borderRadius: 'var(--vl-radius-inner)',
            fontFamily: 'var(--vl-font-ui)',
          }}
        >
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span
              className="text-[10px] font-bold uppercase tracking-wider"
              style={{ color: 'var(--vl-gold)' }}
            >
              🕘 {tp('historicalTitle', language)} · v{plan.version}
            </span>
            {onSelectVersion && (
              <button
                type="button"
                data-testid="back-to-active"
                onClick={() => onSelectVersion(latestVersion || 0)}
                className="text-[11px] px-2 py-1"
                style={{
                  color: 'var(--vl-gold)',
                  border: '1px solid var(--vl-gold-line)',
                  borderRadius: 'var(--vl-radius-chip)',
                  background: 'transparent',
                  cursor: 'pointer',
                }}
              >
                ← {tp('backToActive', language)}
              </button>
            )}
          </div>
          <span className="text-[11px]" style={{ color: 'var(--vl-muted)' }}>
            {tp('historicalHint', language)}
          </span>
          {(() => {
            const rec = versions.find((v) => v.version === plan.version)
            if (!rec) return null
            return (
              <>
                {rec.death_reason && (
                  <span
                    data-testid="death-reason"
                    className="text-[11px]"
                    style={{ color: 'var(--vl-short)' }}
                  >
                    {tp('whyItDied', language)}: {rec.death_reason}
                    {rec.superseded_by
                      ? ` · ${tp('supersededBy', language, { n: String(rec.superseded_by) })}`
                      : ''}
                  </span>
                )}
                {rec.death_condition && (
                  <span
                    className="text-[11px]"
                    style={{ color: 'var(--vl-faint)' }}
                  >
                    {tp('planDies', language)}: {rec.death_condition}
                  </span>
                )}
                {!!rec.diff_vs_next?.length && (
                  <span
                    data-testid="version-diff"
                    className="text-[11px]"
                    style={{ color: 'var(--vl-muted)' }}
                  >
                    {tp('whatChanged', language)}:{' '}
                    {rec.diff_vs_next.join(' · ')}
                  </span>
                )}
                <span
                  className="text-[10px]"
                  style={{ color: 'var(--vl-faint)' }}
                >
                  {tp('writtenAt', language)} {rec.created_at} ·{' '}
                  {tp('replansLeftLabel', language)}: {plan.replans_left ?? 0}
                </span>
              </>
            )
          })()}
        </div>
      )}

      {/* ITEM 2d — DEATH HISTORY on the live card. Six plans died in 25 minutes
          on 2026-08-16 and the card showed nothing about it; the owner only saw
          the final levels-less v6. Every superseded version and the condition
          that killed it is now visible without leaving the active plan. */}
      {!plan.historical && deadVersions.length > 0 && (
        <div
          data-testid="death-history"
          className="flex flex-col gap-1 px-2.5 py-2"
          style={{
            background: 'var(--vl-card-2)',
            border: '1px solid rgba(224,108,108,0.28)',
            borderRadius: 'var(--vl-radius-inner)',
            fontFamily: 'var(--vl-font-ui)',
          }}
        >
          <span
            className="text-[10px] font-bold uppercase tracking-wider"
            style={{ color: 'var(--vl-short)' }}
          >
            ☠ {tp('deathHistory', language, { n: String(deadVersions.length) })}
          </span>
          {deadVersions.map((v) => (
            <button
              key={v.version}
              type="button"
              data-testid={`death-row-v${v.version}`}
              onClick={
                onSelectVersion ? () => onSelectVersion(v.version) : undefined
              }
              disabled={!onSelectVersion}
              className="text-[11px] text-left"
              style={{
                color: 'var(--vl-muted)',
                background: 'transparent',
                border: 0,
                padding: '2px 0',
                cursor: onSelectVersion ? 'pointer' : 'default',
              }}
            >
              <span style={{ color: 'var(--vl-gold)' }}>v{v.version}</span>{' '}
              {v.death_reason || v.lifecycle}
            </button>
          ))}
          <span className="text-[10px]" style={{ color: 'var(--vl-faint)' }}>
            {tp('deathHistoryHint', language)}
          </span>
        </div>
      )}

      {/* ITEM 4 — owner edits a re-plan could not re-anchor. NEVER dropped
          silently and never mis-applied: they wait here for the owner's eye. */}
      {!!plan.uncarried_edits?.length && (
        <div
          data-testid="uncarried-edits"
          className="flex flex-col gap-1 px-2.5 py-2"
          style={{
            background: 'var(--vl-gold-dim)',
            border: '1px solid var(--vl-gold-line)',
            borderRadius: 'var(--vl-radius-inner)',
            fontFamily: 'var(--vl-font-ui)',
          }}
        >
          <span
            className="text-[10px] font-bold uppercase tracking-wider"
            style={{ color: 'var(--vl-gold)' }}
          >
            ✎{' '}
            {tp('uncarriedTitle', language, {
              n: String(plan.uncarried_edits.length),
            })}
          </span>
          {plan.uncarried_edits.map((u, i) => (
            <span
              key={`${u.path}-${i}`}
              className="text-[11px]"
              style={{ color: 'var(--vl-muted)' }}
            >
              • {u.summary}
            </span>
          ))}
        </div>
      )}

      {/* bias */}
      <BiasBlock bias={doc.bias} language={language} />

      {/* Mini chart (levels shared with the table). It is NOT gated on having
          levels: the bars are real regardless, and hiding the whole chart when a
          plan came back empty removed the one view that would have shown the
          owner the market was fine and the PLAN was the problem. Zero levels →
          bars with no overlay, plus a caption saying so. */}
      {exchange === 'ninjatrader' && (
        <div className="flex flex-col gap-1">
          <PlanMiniChart
            symbol={symbol}
            exchange={exchange}
            facts={facts}
            language={language}
            structureZones={structureZonesToOverlay(doc?.structure)}
          />
          {facts.length === 0 && (
            <span
              data-testid="mini-chart-no-levels"
              className="text-[10px]"
              style={{
                color: 'var(--vl-faint)',
                fontFamily: 'var(--vl-font-ui)',
              }}
            >
              {tp('chartNoLevels', language)}
            </span>
          )}
        </div>
      )}

      <StructurePanel structure={doc?.structure} />
      <LevelZoneMap map={doc.zone_map} />
      {/* levels — tap a row to edit (P5), ＋ to add an owner level */}
      <ZoneTable
        facts={facts}
        emptyReason={emptyReason}
        language={language}
        onEdit={
          doorEnabled
            ? (level, index) => setEdit({ open: true, level, index })
            : undefined
        }
        onAdd={doorEnabled ? () => setEdit({ open: true }) : undefined}
        onBulkAdd={doorEnabled ? () => setBulkOpen(true) : undefined}
        flashKey={flash}
      />

      {/* W13 — re-align status / proposal, in place under the levels */}
      {doorEnabled && (
        <>
          <RealignPanel
            state={realign}
            traderId={traderId}
            symbol={symbol}
            language={language}
            onDismiss={() => setRealign({ phase: 'idle' })}
            onApplied={() => {
              setFlash((n) => n + 1)
              onChanged?.()
            }}
          />
          {realign.phase === 'idle' && (
            <div className="px-3 pb-1">
              <RealignButton
                language={language}
                onClick={() =>
                  runRealign(
                    { kind: 'edit-level', summary: 'manual re-align' },
                    true
                  )
                }
              />
            </div>
          )}
        </>
      )}

      {/* Why the door is shut — never leave a disabled surface unexplained. Sits
          OUTSIDE the doorEnabled block, which is exactly what is false here. */}
      {!!traderId && !viewingLiveSession && (
        <div
          className="px-3 pb-2 text-[10px]"
          style={{ color: 'var(--vl-faint)' }}
          data-testid="sibling-session-readonly"
        >
          {tp('siblingReadOnly', language)}
        </div>
      )}

      {/* scenarios */}
      <StructuralGeometry rows={plan.structural_geometry} language={language} />
      {doc.scenarios && doc.scenarios.length > 0 && (
        <ScenarioList
          scenarios={doc.scenarios}
          statusMap={plan.scenario_status}
          deaths={plan.scenario_deaths}
          fadeLabels={
            (plan as { fade_permission?: Record<string, FadeLabelView> })
              .fade_permission
          }
          oneSetup={(plan as { one_setup?: OneSetupView }).one_setup}
          identities={plan.scenario_identity}
          armedStates={
            (
              plan as {
                armed?: Record<
                  string,
                  { state: string; reason?: string; entry_px?: number }
                >
              }
            ).armed
          }
          fvgStates={
            (
              plan as {
                fvg_states?: Array<{
                  id: string
                  fvg_lo: number
                  fvg_hi: number
                  ce: number
                  entry_mode: string
                  state: string
                  touch_number: number
                  met: boolean
                }>
              }
            ).fvg_states
          }
          meta={
            (
              plan as {
                scenario_meta?: {
                  basis?: Record<string, string>
                  unevaluable?: string[]
                  confirm?: Record<
                    string,
                    {
                      rule: string
                      ref_price: number
                      side: string
                      met: boolean
                      detail: string
                    }
                  >
                }
              }
            ).scenario_meta
          }
          language={language}
        />
      )}

      {/* F2 (2026-09-03): a position entered under a different plan version
          states its own terms before the rows below are read as its own */}
      <ArmedUnderBlock
        position={
          (plan as { open_position?: OpenPositionProvenance | null })
            .open_position
        }
        language={language}
      />

      {/* rules */}
      <RulesBlock
        noTrade={doc.no_trade ?? []}
        band={(plan as { no_trade_band?: NoTradeBandWindow[] }).no_trade_band}
        deathCondition={doc.death_condition}
        language={language}
      />

      {/* footer */}
      <PlanFooter
        version={plan.version ?? 1}
        dayType={doc.day_type}
        modelId={plan.model_id}
        replansLeft={plan.replans_left}
        language={language}
        latestVersion={newestVersion}
        versionCount={versionCount}
        onSelectVersion={onSelectVersion}
        titleForVersion={versionTitle}
        noTradeVersion={noTradeVersion}
        noTradeLabel={tp('noTradeChip', language)}
      />

      {/* P5 owner door — portaled sheets */}
      {doorEnabled && (
        <>
          <EditSheet
            open={edit.open}
            traderId={traderId!}
            symbol={symbol}
            language={language}
            level={edit.level}
            levelIndex={edit.index}
            scenarioIds={(doc.scenarios ?? []).map((s) => s.id)}
            onClose={() => setEdit({ open: false })}
            onSaved={(change) => {
              onChanged?.()
              if (change) void runRealign(change)
            }}
          />
          <BulkAddSheet
            open={bulkOpen}
            traderId={traderId!}
            symbol={symbol}
            language={language}
            onClose={() => setBulkOpen(false)}
            onSaved={(change) => {
              onChanged?.()
              if (change) void runRealign(change)
            }}
          />
          <AskPlannerPanel
            open={askOpen}
            traderId={traderId!}
            symbol={symbol}
            planVersion={plan.version}
            language={language}
            contextLabel={
              plan.lifecycle && plan.lifecycle !== 'active'
                ? tp('askContextHistorical', language)
                : undefined
            }
            onClose={() => setAskOpen(false)}
            onApplied={() => onChanged?.()}
          />
        </>
      )}
    </div>
  )
}
