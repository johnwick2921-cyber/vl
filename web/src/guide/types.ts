// In-app guide content types (FE-only). Every content module exports a
// GuideSection stamped with asBuiltRev — the guide top banner compares that
// against GET /api/health revision and warns on drift.
import type { ReactNode } from 'react'

// The sha this guide was BUILT for. The top banner compares it against GET
// /api/health revision, so a guide that disagrees with the running binary says
// so instead of quietly lying about which behaviour it describes.
//
// It is now supplied AT BUILD TIME (VITE_GUIDE_BUILT_REV), not edited into
// this file by hand. Hand-editing is how it went stale: the bump was a step in
// a deploy procedure, and a step in a procedure is a step someone skips under
// pressure — which is exactly when the guide matters most.
//
// A PRODUCTION build with the variable missing, or not a 40-hex sha, is
// refused IN THE BUILD by the `guide-built-rev-is-a-build-input` plugin in
// vite.config.ts. It cannot be refused here: this file's guard runs at module
// scope, and Vite does not execute the module while building — an earlier
// version threw here, the build exited 0, and the throw shipped into the
// bundle to fire on page load. A guide revision must never take the trading
// UI down, so at RUNTIME an unusable value degrades to 'unknown' and the
// banner says it cannot verify the build. A dev build shows 'dev', which the
// banner renders as "not a release build" and never as a matching revision.
function resolveGuideBuiltRev(): string {
  const raw = import.meta.env?.VITE_GUIDE_BUILT_REV
  if (import.meta.env?.PROD) {
    // Never throw: the build gate is the enforcement point, and a crash here
    // would blank the whole UI over a documentation revision. 'unknown' is an
    // honest unknowable value (A24) — it is never a real-looking sha.
    return typeof raw === 'string' && /^[0-9a-f]{40}$/.test(raw)
      ? raw
      : 'unknown'
  }
  return typeof raw === 'string' && /^[0-9a-f]{40}$/.test(raw) ? raw : 'dev'
}

export const GUIDE_BUILT_REV = resolveGuideBuiltRev()

export interface Card {
  title: string
  body: string
  tag?: string
}

export interface TimelineItem {
  time: string
  label: string
  detail: string
  shade?: boolean
}

export interface CalloutItem {
  title: string
  body: string
  cite?: string
}

export interface GlossaryTerm {
  term: string
  def: string
}

export interface FaqItem {
  q: string
  a: string
  mechanism?: string
  link?: string
}

export interface ChecklistItem {
  title: string
  steps: string[]
}

export interface ButtonSpec {
  label: string
  api: string
  sideEffects: string
  budget: string
  undo: string
  useWhen: string
}

/** Per-knob card — ALL fields mandatory (linted by a content test). */
export interface KnobSpec {
  label: string // exact on-screen text
  settingId?: string
  live?: boolean
  where: string // page / section / accordion
  what: string // one plain sentence
  trader: string // one trader sentence
  consumer: string // engine consumer file:line
  range: string // range/clamp + honest unit
  systemDefault: string
  recommended: string // ⭐ recommended + WHY (one line, sourced)
  whenToTouch: string
  perSession: string // yes/no + precedence note
}

export type GuideBlock =
  | { kind: 'p'; text: string }
  | { kind: 'h'; text: string }
  | { kind: 'cards'; cards: Card[] }
  | { kind: 'timeline'; items: TimelineItem[] }
  | { kind: 'callout'; title: string; items: CalloutItem[] }
  | { kind: 'table'; title?: string; head: string[]; rows: string[][] }
  | { kind: 'code'; title?: string; lines: string[] }
  | { kind: 'checklists'; items: ChecklistItem[] }
  | { kind: 'faq'; items: FaqItem[] }
  | { kind: 'glossary'; terms: GlossaryTerm[] }
  | { kind: 'live'; label: string; node: ReactNode }
  | { kind: 'mockCard' }
  | { kind: 'knobs'; knobs: KnobSpec[] }
  | { kind: 'buttons'; items: ButtonSpec[] }

export interface GuideSection {
  id: string
  num: number
  title: string
  tagline: string
  asBuiltRev: string
  blocks: GuideBlock[]
}

export type SearchHit = {
  sectionId: string
  sectionNum: number
  sectionTitle: string
  text: string
}
