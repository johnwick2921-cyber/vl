// In-app guide content types (FE-only). Every content module exports a
// GuideSection stamped with asBuiltRev — the guide top banner compares that
// against GET /api/health revision and warns on drift.
import type { ReactNode } from 'react'

export const GUIDE_BUILT_REV = '38a63a9bb2892beb91041bf7e551a8701df8cf9b'

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
