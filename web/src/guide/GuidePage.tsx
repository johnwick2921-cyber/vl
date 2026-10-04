import { PRODUCT_NAME } from '../constants/branding'
// GuidePage — the in-app system guide (5th nav page). FE-only; renders typed
// content modules + the LIVE-COMPONENT examples. Rev-drift banner compares
// GUIDE_BUILT_REV against the running bot's /api/health revision.
import { useEffect, useMemo, useRef, useState } from 'react'
import {
  GUIDE_BUILT_REV,
  type GuideBlock,
  type GuideSection,
  type SearchHit,
} from './types'
import { Example } from './components/Example'
import { KnobCard } from './components/KnobCard'
import { MockPlanCard } from './components/MockPlanCard'
import { welcome } from './content/welcome'
import { tradingDay } from './content/tradingDay'
import { planCard } from './content/planCard'
import { levels } from './content/levels'
import { plays } from './content/plays'
import { guards } from './content/guards'
import { settings } from './content/settings'
import { buttons } from './content/buttons'
import { routines } from './content/routines'
import { status } from './content/status'
import { glossary } from './content/glossary'
import { faq } from './content/faq'
import { weeklyBias } from './content/weeklyBias'
import { expectancy } from './content/expectancy'
import { candidates } from './content/candidates'
import { updates } from './content/updates'
import { mentor } from './content/mentor'

export const GUIDE_SECTIONS: GuideSection[] = [
  welcome,
  tradingDay,
  planCard,
  levels,
  plays,
  guards,
  settings,
  buttons,
  routines,
  status,
  glossary,
  faq,
  weeklyBias,
  expectancy,
  candidates,
  updates,
  mentor,
]

const ivory = { color: 'var(--vl-ivory)' }
const muted = {
  color: 'var(--vl-muted)',
  fontFamily: 'var(--vl-font-ui)',
} as const
const faint = {
  color: 'var(--vl-faint)',
  fontFamily: 'var(--vl-font-ui)',
} as const
const hair = '1px solid var(--vl-hair)'

function collectHits(): SearchHit[] {
  const hits: SearchHit[] = []
  for (const s of GUIDE_SECTIONS) {
    hits.push({
      sectionId: s.id,
      sectionNum: s.num,
      sectionTitle: s.title,
      text: s.title,
    })
    hits.push({
      sectionId: s.id,
      sectionNum: s.num,
      sectionTitle: s.title,
      text: s.tagline,
    })
    for (const b of s.blocks) {
      if (b.kind === 'p')
        hits.push({
          sectionId: s.id,
          sectionNum: s.num,
          sectionTitle: s.title,
          text: b.text,
        })
      if (b.kind === 'h')
        hits.push({
          sectionId: s.id,
          sectionNum: s.num,
          sectionTitle: s.title,
          text: b.text,
        })
      if (b.kind === 'cards')
        for (const c of b.cards)
          hits.push({
            sectionId: s.id,
            sectionNum: s.num,
            sectionTitle: s.title,
            text: `${c.title} ${c.body}`,
          })
      if (b.kind === 'callout')
        for (const i of b.items)
          hits.push({
            sectionId: s.id,
            sectionNum: s.num,
            sectionTitle: s.title,
            text: `${i.title} ${i.body}`,
          })
      if (b.kind === 'timeline')
        for (const i of b.items)
          hits.push({
            sectionId: s.id,
            sectionNum: s.num,
            sectionTitle: s.title,
            text: `${i.label} ${i.detail}`,
          })
      if (b.kind === 'knobs')
        for (const k of b.knobs)
          hits.push({
            sectionId: s.id,
            sectionNum: s.num,
            sectionTitle: s.title,
            text: `${k.label} ${k.what} ${k.where}`,
          })
      if (b.kind === 'glossary')
        for (const t of b.terms)
          hits.push({
            sectionId: s.id,
            sectionNum: s.num,
            sectionTitle: s.title,
            text: `${t.term} ${t.def}`,
          })
      if (b.kind === 'faq')
        for (const f of b.items)
          hits.push({
            sectionId: s.id,
            sectionNum: s.num,
            sectionTitle: s.title,
            text: `${f.q} ${f.a}`,
          })
      if (b.kind === 'buttons')
        for (const bt of b.items)
          hits.push({
            sectionId: s.id,
            sectionNum: s.num,
            sectionTitle: s.title,
            text: bt.label,
          })
    }
  }
  return hits
}

function BlockView({ block }: { block: GuideBlock }) {
  switch (block.kind) {
    case 'p':
      return (
        <p className="text-[13px] leading-relaxed" style={ivory}>
          {block.text}
        </p>
      )
    case 'h':
      return (
        <h3
          className="mt-4 mb-2 text-[12px] font-bold uppercase tracking-widest"
          style={{ color: 'var(--vl-gold)', fontFamily: 'var(--vl-font-ui)' }}
        >
          {block.text}
        </h3>
      )
    case 'cards':
      return (
        <div className="grid gap-2 sm:grid-cols-2">
          {block.cards.map((c, i) => (
            <article
              key={i}
              className="rounded-xl p-3"
              style={{ background: 'var(--vl-card)', border: hair }}
            >
              <div className="mb-1 flex items-center justify-between gap-2">
                <span className="text-[13px] font-bold" style={ivory}>
                  {c.title}
                </span>
                {c.tag && (
                  <span
                    className="text-[9px] uppercase tracking-wider"
                    style={muted}
                  >
                    {c.tag}
                  </span>
                )}
              </div>
              <p className="text-[12px] leading-snug" style={ivory}>
                {c.body}
              </p>
            </article>
          ))}
        </div>
      )
    case 'timeline':
      return (
        <ol className="flex flex-col gap-1.5">
          {block.items.map((i, idx) => (
            <li key={idx} className="flex gap-3 text-[12px]">
              <span
                className="w-14 shrink-0 text-right font-bold"
                style={{ color: 'var(--vl-gold)' }}
              >
                {i.time}
              </span>
              <span className="flex flex-col">
                <span style={ivory}>{i.label}</span>
                <span style={muted}>{i.detail}</span>
              </span>
            </li>
          ))}
        </ol>
      )
    case 'callout':
      return (
        <div
          className="rounded-xl p-3"
          style={{ background: 'var(--vl-card)', border: hair }}
        >
          <p
            className="mb-2 text-[11px] font-bold uppercase tracking-widest"
            style={muted}
          >
            {block.title}
          </p>
          <div className="flex flex-col gap-2">
            {block.items.map((i, idx) => (
              <details key={idx} className="text-[12px]">
                <summary
                  className="cursor-pointer font-bold"
                  style={{ color: 'var(--vl-gold)' }}
                >
                  {i.title}
                </summary>
                <p className="mt-1 leading-snug" style={ivory}>
                  {i.body}
                </p>
                {i.cite && (
                  <p className="mt-1 text-[10px]" style={faint}>
                    cite: {i.cite}
                  </p>
                )}
              </details>
            ))}
          </div>
        </div>
      )
    case 'table':
      return (
        <div
          className="overflow-x-auto rounded-xl"
          style={{ background: 'var(--vl-card)', border: hair }}
        >
          <table className="w-full text-left text-[11px]">
            <thead>
              <tr>
                {block.head.map((h, i) => (
                  <th
                    key={i}
                    className="px-2 py-1.5 font-bold uppercase tracking-wider"
                    style={{ ...muted, borderBottom: hair }}
                  >
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {block.rows.map((r, i) => (
                <tr key={i}>
                  {r.map((c, j) => (
                    <td
                      key={j}
                      className="px-2 py-1.5 align-top"
                      style={{ ...ivory, borderBottom: hair }}
                    >
                      {c}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )
    case 'code':
      return (
        <pre
          className="overflow-x-auto rounded-xl p-3 text-[11px] leading-snug"
          style={{
            background: 'var(--vl-card2)',
            color: 'var(--vl-ivory)',
            border: hair,
          }}
        >
          {block.title && (
            <span
              className="mb-1 block font-bold uppercase tracking-wider text-[10px]"
              style={faint}
            >
              {block.title}
            </span>
          )}
          {block.lines.join('\n')}
        </pre>
      )
    case 'checklists':
      return (
        <div className="grid gap-2">
          {block.items.map((c, i) => (
            <article
              key={i}
              className="rounded-xl p-3"
              style={{ background: 'var(--vl-card)', border: hair }}
            >
              <p className="mb-1.5 text-[12px] font-bold" style={ivory}>
                {c.title}
              </p>
              <ol className="flex flex-col gap-1">
                {c.steps.map((s, j) => (
                  <li key={j} className="flex gap-2 text-[12px]">
                    <span
                      className="shrink-0 font-bold"
                      style={{ color: 'var(--vl-gold)' }}
                    >
                      {j + 1}.
                    </span>
                    <span style={ivory}>{s}</span>
                  </li>
                ))}
              </ol>
            </article>
          ))}
        </div>
      )
    case 'faq':
      return (
        <div className="flex flex-col gap-2">
          {block.items.map((f, i) => (
            <details
              key={i}
              className="rounded-xl p-3 text-[12px]"
              style={{ background: 'var(--vl-card)', border: hair }}
            >
              <summary className="cursor-pointer font-bold" style={ivory}>
                {f.q}
              </summary>
              <p className="mt-1.5 leading-snug" style={ivory}>
                {f.a}
              </p>
              {f.mechanism && (
                <p className="mt-1 text-[11px]" style={muted}>
                  mechanism: {f.mechanism}
                </p>
              )}
              {f.link && (
                <a
                  className="mt-1 inline-block text-[11px]"
                  style={{ color: 'var(--vl-gold)' }}
                  href={`#${f.link.slice(1)}`}
                >
                  {f.link}
                </a>
              )}
            </details>
          ))}
        </div>
      )
    case 'glossary':
      return (
        <div className="grid gap-2 sm:grid-cols-2">
          {block.terms.map((t, i) => (
            <article
              key={i}
              className="rounded-xl p-3"
              style={{ background: 'var(--vl-card)', border: hair }}
            >
              <span
                className="text-[12px] font-bold"
                style={{ color: 'var(--vl-gold)' }}
              >
                {t.term}
              </span>
              <p className="mt-0.5 text-[12px]" style={ivory}>
                {t.def}
              </p>
            </article>
          ))}
        </div>
      )
    case 'live':
      return <Example label={block.label}>{block.node}</Example>
    case 'mockCard':
      return (
        <Example label="mock — real components">{<MockPlanCard />}</Example>
      )
    case 'knobs':
      return (
        <div className="grid gap-2 sm:grid-cols-2">
          {block.knobs.map((k, i) => (
            <KnobCard key={i} knob={k} />
          ))}
        </div>
      )
    case 'buttons':
      return (
        <div className="grid gap-2">
          {block.items.map((b, i) => (
            <article
              key={i}
              className="rounded-xl p-3"
              style={{ background: 'var(--vl-card)', border: hair }}
            >
              <p className="text-[13px] font-bold" style={ivory}>
                {b.label}
              </p>
              <p className="mt-1 text-[11px]" style={faint}>
                api: {b.api}
              </p>
              <p className="mt-1 text-[12px]" style={ivory}>
                {b.sideEffects}
              </p>
              <p className="mt-1 text-[11px]" style={muted}>
                <b style={muted}>budget:</b> {b.budget} ·{' '}
                <b style={muted}>undo:</b> {b.undo}
              </p>
              <p className="mt-1 text-[11px]" style={ivory}>
                <b style={{ color: 'var(--vl-gold)' }}>use when:</b> {b.useWhen}
              </p>
            </article>
          ))}
        </div>
      )
  }
}

// Both sides truncate to this before comparing: the bot can only ever send a
// short sha, so a full-length equality test can never succeed.
const REV_COMPARE_LEN = 12

export function GuidePage() {
  const [query, setQuery] = useState('')
  const [revision, setRevision] = useState<string | undefined>(undefined)
  const indexRef = useRef<HTMLDivElement>(null)
  const allHits = useMemo(() => collectHits(), [])
  const hits = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return []
    return allHits.filter((h) => h.text.toLowerCase().includes(q)).slice(0, 40)
  }, [query, allHits])

  useEffect(() => {
    let live = true
    fetch('/api/health')
      .then((r) => r.json())
      .then((j) => live && setRevision(j?.revision))
      .catch(() => live && setRevision(undefined))
    return () => {
      live = false
    }
  }, [])

  // The bot reports kernel.RunningRevision() — shortRev(), 12 chars — while
  // GUIDE_BUILT_REV is a full sha. `revision.startsWith(GUIDE_BUILT_REV)` is
  // therefore false even for the SAME commit, which left the banner on
  // permanently and made it worth nothing. Compare on the short prefix both
  // sides can actually produce.
  //
  // An empty revision means the boot assertion has not run yet: the bot does
  // not know its own rev, which is not the same as disagreeing with it. Claim
  // drift only when there is a revision to disagree with.
  const drift =
    revision !== undefined &&
    revision !== '' &&
    revision.slice(0, REV_COMPARE_LEN) !==
      GUIDE_BUILT_REV.slice(0, REV_COMPARE_LEN)

  return (
    <div
      data-testid="guide-page"
      className="mx-auto flex w-full max-w-3xl flex-col gap-4 px-3 py-4 sm:px-4"
    >
      <header>
        <h1
          className="text-xl font-black uppercase tracking-widest"
          style={{ color: 'var(--vl-gold)' }}
        >
          {PRODUCT_NAME} System Guide
        </h1>
        <p className="text-[12px]" style={muted}>
          built against rev {GUIDE_BUILT_REV} · {GUIDE_SECTIONS.length} sections
          · live-component examples
        </p>
      </header>

      {drift && (
        <div
          data-testid="guide-rev-drift"
          className="rounded-xl px-3 py-2 text-[12px]"
          style={{
            background: 'rgba(224,168,54,0.08)',
            border: '1px solid var(--vl-gold-line)',
            color: 'var(--vl-gold)',
          }}
        >
          ⚠ REVISION DRIFT — this guide was built against {GUIDE_BUILT_REV}, the
          bot reports {revision}. Some cites may be stale; verify before
          trusting them.
        </div>
      )}

      <input
        data-testid="guide-search"
        type="search"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder="search sections, glossary, FAQ, knobs…"
        className="rounded-xl px-3 py-2 text-[13px]"
        style={{
          background: 'var(--vl-card)',
          border: hair,
          color: 'var(--vl-ivory)',
        }}
      />
      {query.trim() && (
        <div
          className="flex flex-col gap-1 rounded-xl p-2"
          style={{ background: 'var(--vl-card)', border: hair }}
        >
          {hits.length === 0 && (
            <p className="px-1 text-[12px]" style={muted}>
              no matches
            </p>
          )}
          {hits.map((h, i) => (
            <a
              key={i}
              href={`#${h.sectionId}`}
              onClick={() => setQuery('')}
              className="flex items-baseline gap-2 rounded px-2 py-1 text-[12px] hover:bg-[var(--vl-card2)]"
              style={ivory}
            >
              <span
                className="shrink-0 font-bold"
                style={{ color: 'var(--vl-gold)' }}
              >
                §{h.sectionNum}
              </span>
              <span className="truncate">{h.text}</span>
            </a>
          ))}
        </div>
      )}

      <div className="flex gap-4">
        {/* sticky index — horizontal chips on mobile, side list on desktop */}
        <nav ref={indexRef} className="hidden shrink-0 sm:block sm:w-44">
          <div
            className="sticky top-20 flex flex-col gap-1 rounded-xl p-2"
            style={{ background: 'var(--vl-card)', border: hair }}
          >
            {GUIDE_SECTIONS.map((s) => (
              <a
                key={s.id}
                href={`#${s.id}`}
                className="flex items-baseline gap-1.5 rounded px-2 py-1 text-[11px]"
                style={muted}
              >
                <span style={{ color: 'var(--vl-gold)' }}>{s.num}</span>
                <span className="truncate" style={ivory}>
                  {s.title}
                </span>
              </a>
            ))}
          </div>
        </nav>
        <div className="flex min-w-0 flex-1 flex-col gap-4">
          <div className="sm:hidden flex flex-wrap gap-1">
            {GUIDE_SECTIONS.map((s) => (
              <a
                key={s.id}
                href={`#${s.id}`}
                className="rounded px-2 py-1 text-[10px] uppercase tracking-wider"
                style={{
                  background: 'var(--vl-card)',
                  border: hair,
                  color: 'var(--vl-gold)',
                }}
              >
                {s.num} {s.title}
              </a>
            ))}
          </div>

          {GUIDE_SECTIONS.map((s) => (
            <section
              key={s.id}
              id={s.id}
              className="scroll-mt-20 rounded-2xl p-3 sm:p-4"
              style={{ background: 'var(--vl-panel)', border: hair }}
            >
              <header className="mb-3 flex flex-wrap items-baseline justify-between gap-2">
                <h2
                  className="text-base font-black uppercase tracking-wider"
                  style={{ color: 'var(--vl-ivory)' }}
                >
                  <span style={{ color: 'var(--vl-gold)' }}>{s.num} · </span>
                  {s.title}
                </h2>
                <span
                  className="text-[10px] uppercase tracking-wider"
                  style={faint}
                >
                  rev {s.asBuiltRev}
                </span>
              </header>
              <p className="mb-3 text-[12px]" style={muted}>
                {s.tagline}
              </p>
              <div className="flex flex-col gap-3">
                {s.blocks.map((b, i) => (
                  <BlockView key={i} block={b} />
                ))}
              </div>
            </section>
          ))}
        </div>
      </div>
    </div>
  )
}
