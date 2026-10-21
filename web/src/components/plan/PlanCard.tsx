// P4.3 — the dashboard panel. Self-fetches the plan (usePlanToday), and composes
// the SessionTimelineStrip + SessionTabs + SessionPlanCard. This is what mounts
// top-left in TraderDashboardPage. Additive + dormant: it only renders when a
// trader is present; every no-plan state degrades gracefully.
// (HandoverBanner deleted 2026-08-18: its only trigger was lifecycle
// expired|died|superseded, which no writer ever produces.)

import { useEffect, useState } from 'react'
import { useLanguage } from '../../contexts/LanguageContext'
import { tp } from '../../i18n/plan-translations'
import { usePlanToday, usePlanVersions } from './usePlan'
import { SessionTimelineStrip } from './SessionTimelineStrip'
import { SessionTabs, type SessionTab, type TabState } from './SessionTabs'
import { SessionPlanCard } from './SessionPlanCard'
import { RereadButton } from './RereadButton'
import { ResetButton } from './ResetButton'
import { ApproveButton } from './ApproveButton'
import { AlertCenter } from './AlertCenter'
import { DeskStrip } from './DeskStrip'
import { GateBlocksPanel } from './GateBlocksPanel'
import { ExpectancyPanel } from './ExpectancyPanel'
import { InstrumentsDrawer } from './InstrumentsDrawer'
import { MentorTruthCard } from '../mentor/MentorTruthCard'
import { useMentorTruth } from '../mentor/useMentorTruth'
import type { MentorTruth } from '../../lib/api/traders'
import { SESSION_BANDS, type SessionName } from './sessionConfig'

interface Props {
  traderId?: string
  symbol?: string
  exchange?: string
  /** P2-1: when the page renders its own live card, pass its truth down so
   * PlanCard does NOT poll or render a second card. undefined = fetch own. */
  mentorTruth?: MentorTruth | null
}

const ALL_SESSIONS: SessionName[] = ['ASIA', 'LONDON', 'NY']

export function PlanCard({
  traderId,
  symbol = 'MNQ',
  exchange = 'ninjatrader',
  mentorTruth,
}: Props) {
  const { language } = useLanguage()
  // W15.B — `selected` now drives the FETCH, not just the highlight. Before this
  // the tabs were a lie: clicking ASIA moved the chip while the card kept showing
  // the live session's plan, because the request had no session dimension.
  const [selected, setSelected] = useState<SessionName | null>(null)
  // ITEM 15 — which VERSION is being viewed. null = the live/latest one, which
  // is the only state that existed before: the chips were inert spans, so a past
  // version could not be opened at all.
  const [viewVersion, setViewVersion] = useState<number | null>(null)
  const { plan, isLoading, error, mutate } = usePlanToday(
    traderId,
    symbol,
    selected ?? undefined,
    viewVersion ?? undefined
  )
  const { versions, latestVersion } = usePlanVersions(
    traderId,
    selected ?? undefined
  )
  // MENTOR-TRUTH PANEL: the live evaluator truth ("what trades"), refreshed
  // every 30s + on focus (see useMentorTruth). When the page passes its own
  // truth down (mentorTruth !== undefined), PlanCard uses it and does NOT poll
  // or render a second card — the page-level card is the single source.
  // mentor mode OFF → enabled=false → the card hides and the AI bias card keeps
  // its unchanged label. A failed refresh keeps the last good payload (marked
  // stale), so the advice-only label never flips off on a blip.
  const externalMentor = mentorTruth !== undefined
  const { truth: ownMentor, stale: mentorStale } = useMentorTruth(
    traderId,
    !externalMentor
  )
  const mentor = externalMentor ? mentorTruth : ownMentor

  // Which session is LIVE right now (server-told), independent of what tab the
  // owner is looking at.
  const activeSession = (plan?.active_session as SessionName) || null

  // auto-advance: follow the backend active session until the owner picks a tab.
  useEffect(() => {
    if (activeSession && selected === null) setSelected(activeSession)
  }, [activeSession, selected])

  // Switching sessions must drop the version pin — v3 of NY is not v3 of ASIA.
  useEffect(() => {
    setViewVersion(null)
  }, [selected])

  const tabs = computeSessionTabs(activeSession, plan?.runnable_sessions)

  return (
    <div
      className="p-5 flex flex-col gap-3 animate-slide-in"
      style={{
        background: 'var(--vl-card)',
        border: '1px solid var(--vl-hair)',
        borderRadius: 'var(--vl-radius-card)',
        fontFamily: 'var(--vl-font-ui)',
      }}
    >
      {/* THE DESK STRIP — the first thing on screen, above the alert feed.
          One read, one row per fact, every number dated. It adds no unread
          queue: alerts are acknowledged 40.8% of the time (P0, 62 of 152) and
          a second backlog would make that worse, not better. */}
      <DeskStrip traderId={traderId} />
      <AlertCenter traderId={traderId} language={language} />
      <SessionTimelineStrip activeSession={activeSession} language={language} />
      <SessionTabs
        tabs={tabs}
        selected={selected ?? activeSession ?? 'NY'}
        onSelect={setSelected}
        language={language}
      />
      {/* W16/R3 — refusals, where the owner already looks for plan state. */}
      <GateBlocksPanel traderId={traderId} language={language} />
      {/* 1D — what each play has actually paid, on the corrected column. Read
          model only: it rules on nothing and no control here changes behaviour.
          Almost every row reads DESCRIPTIVE ONLY today, which is the floor
          working rather than the table failing. */}
      <ExpectancyPanel />
      {/* SIBLING of the panel above, deliberately. It used to render from
          inside it, and the panel returns null on a day the expectancy
          endpoint has nothing — which took the three instruments down with it
          for a reason no reader could have connected to the drawer. */}
      <InstrumentsDrawer traderId={traderId} />
      {/* ITEM 3 — the owner's manual re-read, next to the plan it acts on.
          P6 — the owner RESET sits beside it: both visible, one explanatory
          line each, so the two escape hatches can never be mistaken for one
          another (re-read = one more plan on the same chain, spends budget ·
          reset = abandons the chain, restores the full budget). The Approve
          action is the plan-header grant (approval_required flow). */}
      <div className="flex items-start justify-end gap-4">
        <div className="flex flex-col items-end gap-1">
          {plan?.approval_required === true && (
            <ApproveButton
              traderId={traderId}
              language={language}
              onDone={() => mutate()}
            />
          )}
          <span
            className="text-[9px]"
            style={{
              color: 'var(--vl-faint)',
              fontFamily: 'var(--vl-font-ui)',
            }}
          >
            {tp('approveCaption', language)}
          </span>
        </div>
        <div className="flex flex-col items-end gap-1">
          <RereadButton
            traderId={traderId}
            language={language}
            onDone={() => mutate()}
          />
          <span
            className="text-[9px]"
            style={{
              color: 'var(--vl-faint)',
              fontFamily: 'var(--vl-font-ui)',
            }}
          >
            {tp('rereadCaption', language)}
          </span>
        </div>
        <div className="flex flex-col items-end gap-1">
          <ResetButton
            traderId={traderId}
            language={language}
            onDone={() => mutate()}
          />
          <span
            className="text-[9px]"
            style={{
              color: 'var(--vl-faint)',
              fontFamily: 'var(--vl-font-ui)',
            }}
          >
            {tp('resetCaption', language)}
          </span>
        </div>
      </div>
      <SessionPlanCard
        plan={plan}
        traderId={traderId}
        symbol={symbol}
        exchange={exchange}
        language={language}
        isLoading={isLoading}
        errored={!!error && !plan}
        onChanged={() => mutate()}
        versions={versions}
        latestVersion={latestVersion}
        onSelectVersion={(v) => setViewVersion(v === latestVersion ? null : v)}
        mentorOn={mentor?.enabled ?? false}
      />
      {!externalMentor && (
        <MentorTruthCard truth={mentor} stale={mentorStale} />
      )}
    </div>
  )
}

// tab states: the live active session is 'active'; runnable-but-inactive is
// 'inactive'; everything else is 'disabled' (SessionTabs refuses the click).
//
// W15.B — enablement comes from the SERVER (`runnable_sessions`, resolved by the
// same gate the bot runs). It used to come from the hardcoded SESSION_BANDS
// constant, so a session the owner switched on stayed greyed out forever. No
// server list (older payload / still loading) → fall back to the constant, which
// keeps the pre-W15 rendering.
export function computeSessionTabs(
  activeSession: SessionName | null,
  runnable?: string[]
): SessionTab[] {
  return ALL_SESSIONS.map((name) => {
    const on = runnable
      ? runnable.includes(name)
      : !!SESSION_BANDS.find((b) => b.name === name)?.enabled
    let state: TabState = 'disabled'
    if (activeSession === name) state = 'active'
    else if (on) state = 'inactive'
    return { name, state }
  })
}

export default PlanCard
