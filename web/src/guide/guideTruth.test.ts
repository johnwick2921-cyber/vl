// UI-GUIDE-TRUTH (release #22): the guide's numbers are pinned to the code
// that decides them. Each expectation below is READ from the Go / TSX source
// the bot and the Studio run — never typed twice — so moving the code without
// the guide (or the guide without the code) fails here.
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { GUIDE_SECTIONS } from './GuidePage'
import { MENTOR_NO_STUDIO_CONTROL } from './content/mentor'
import type { KnobSpec } from './types'

const repo = (p: string) =>
  readFileSync(resolve(process.cwd(), '..', p), 'utf8')
const web = (p: string) => readFileSync(resolve(process.cwd(), p), 'utf8')

function goConst(src: string, name: string): string {
  const m = src.match(new RegExp(`\\b${name}\\s*=\\s*([0-9.]+)`))
  if (!m) throw new Error(`Go constant ${name} not found — re-anchor this pin`)
  return m[1]
}

const knobs = (sectionId: string): KnobSpec[] =>
  GUIDE_SECTIONS.find((s) => s.id === sectionId)?.blocks.flatMap((b) =>
    b.kind === 'knobs' ? b.knobs : []
  ) ?? []
const card = (sectionId: string, label: string | RegExp) => {
  const k = knobs(sectionId).find((x) =>
    typeof label === 'string' ? x.label === label : label.test(x.label)
  )
  if (!k) throw new Error(`no ${sectionId} card ${label}`)
  return k
}
const sectionText = (sectionId: string) =>
  JSON.stringify(GUIDE_SECTIONS.find((s) => s.id === sectionId)?.blocks)

const mentorGo = repo('trader/mentor_mode.go')
const SIZE = {
  base: goConst(mentorGo, 'mentorBaseContractsDefault'),
  confluence: goConst(mentorGo, 'mentorConfluenceContractsDefault'),
  big: goConst(mentorGo, 'mentorBigContractsDefault'),
  reduced: goConst(mentorGo, 'mentorReducedContractsDefault'),
  swing: goConst(mentorGo, 'mentorSwing4HContractsDefault'),
  max: goConst(mentorGo, 'mentorMaxContractsDefault'),
}

describe('T13/T14 — mentor size cards equal trader/mentor_mode.go', () => {
  it('every size card states the code default', () => {
    expect(card('mentor', 'Base contracts').systemDefault).toBe(SIZE.base)
    expect(card('mentor', 'Confluence contracts').systemDefault).toBe(
      SIZE.confluence
    )
    expect(card('mentor', 'Big contracts').systemDefault).toBe(SIZE.big)
    expect(card('mentor', 'Reduced contracts').systemDefault).toBe(SIZE.reduced)
    expect(card('mentor', 'SWING4H contracts').systemDefault).toBe(SIZE.swing)
    expect(card('mentor', 'Max contracts').systemDefault).toBe(SIZE.max)
  })
  it('the spent-day knob is described as having no effect while the code discards it', () => {
    expect(mentorGo).toMatch(/_ = spentCap/)
    const k = card('mentor', 'Spent-day contracts')
    expect(k.what).toMatch(/NOT READ FOR SIZING/)
    expect(k.systemDefault).toMatch(/no effect/)
    expect(sectionText('mentor')).not.toMatch(/A spent day \(2\) still wins/)
    expect(sectionText('mentor')).not.toMatch(
      /stop is in the twenties or the day is spent/
    )
  })
})

describe('T3 — max contracts card tells the mentor truth', () => {
  it('names the bypass and the mentor sizes from the code', () => {
    const orders = repo('trader/auto_trader_orders.go')
    expect(orders).toMatch(
      /if mx, ok := at\.mentorMaxContracts\(\); ok \{\s*return mx/
    )
    const k = card('settings', /^Max contracts \(always-on\)/)
    expect(k.what).toContain('MENTOR MODE IS NOT CLAMPED')
    expect(k.what).toContain(`${SIZE.base} by default`)
    expect(k.what).toContain(`${SIZE.reduced} when the stop is 20–25 pts`)
    expect(k.what).toContain(`${SIZE.swing} on the SWING4H`)
    expect(k.what).toContain(`mentor_max_contracts (default ${SIZE.max})`)
    expect(k.what).not.toMatch(
      /^Contract cap per order — on with or without the master\. 0B \(2026-09-02\): every resolution is clamped/
    )
  })
})

describe('T15 — swing first target', () => {
  it('says max(1R, EMA34) and never the removed 50-pt fallback', () => {
    expect(repo('kernel/mentor/swing4h.go')).toMatch(
      /The old 50-pt fallback is gone/
    )
    expect(sectionText('mentor')).not.toMatch(/50-pt fallback\)/)
    expect(sectionText('mentor')).toMatch(/at least 1R/)
  })
})

describe('T16 — a mentor card names a Studio control only when one exists', () => {
  // The controls the Studio really renders, read from the components.
  const editor = web('src/components/strategy/RiskControlEditor.tsx')
  const tuning = web('src/components/strategy/MentorTuningPanel.tsx')
  const controls = new Set<string>([
    ...[...editor.matchAll(/updateField\('(mentor_[a-z0-9_]+)'/g)].map(
      (m) => m[1]
    ),
    ...[...tuning.matchAll(/(?:numField|boolField)\('([a-z0-9_]+)'/g)].map(
      (m) => m[1]
    ),
    ...(tuning.includes('trigger_school') ? ['trigger_school'] : []),
  ])
  it('found the Studio controls (non-vacuous)', () => {
    expect(controls.has('mentor_mode')).toBe(true)
    expect(controls.has('ping_pong_min_gap_pts')).toBe(true)
  })
  it('every live mentor card agrees with the Studio', () => {
    const live = knobs('mentor').filter((k) => k.live === true && k.settingId)
    expect(live.length).toBeGreaterThan(20)
    for (const k of live) {
      const id = k.settingId!.replace(/^risk_control\.mentor_tuning\./, '')
      if (controls.has(id)) {
        expect(k.where, `${k.label} has a control`).not.toMatch(
          /No Studio control/
        )
      } else {
        expect(k.where, `${k.label} (${id}) has NO Studio control`).toMatch(
          /No Studio control/
        )
      }
    }
    for (const id of MENTOR_NO_STUDIO_CONTROL) {
      expect(
        controls.has(id),
        `${id} gained a Studio control — update the guide`
      ).toBe(false)
    }
  })
})

describe('T20/T21/T22 — settings and gate numbers equal the code', () => {
  it('MIN-SL default is kernel.MinSLATRMultDefault', () => {
    const v = goConst(repo('kernel/min_sl.go'), 'MinSLATRMultDefault')
    expect(sectionText('guards')).toContain(
      `MIN-SL (env MIN_SL_ATR_MULT, default ${v})`
    )
    expect(sectionText('guards')).not.toMatch(/ARM_MIN_RR 2\.0/)
    expect(repo('trader/armed_executor.go')).toMatch(/ARM_MIN_RR is DELETED/)
    expect(sectionText('settings')).not.toMatch(
      /'ARM_MIN_RR = 2\.0'|"ARM_MIN_RR = 2\.0"/
    )
  })
  it('INGEST_QUEUE_CAP default is the provider return value', () => {
    const src = repo('provider/ninjatrader/tcp_server.go')
    const m = src.match(
      /func ingestQueueCap\(\) int \{[\s\S]*?\n\treturn (\d+)\n\}/
    )
    expect(m).not.toBeNull()
    expect(sectionText('settings')).toContain(`INGEST_QUEUE_CAP = ${m![1]}`)
  })
  it('guardrail toggles resolve to their Go default', () => {
    const src = repo('kernel/risk_limits.go')
    for (const [label, field] of [
      ['Daily profit cap', 'DailyProfitEnabled'],
      ['Max daily trades', 'MaxDailyTradesEnabled'],
      ['Consistency', 'ConsistencyEnabled'],
    ]) {
      const m = src.match(
        new RegExp(
          `${field}:\\s*boolOrDefault\\(rc\\.${field}, (true|false)\\)`
        )
      )
      expect(m, field).not.toBeNull()
      expect(
        card('settings', label).systemDefault.startsWith(
          m![1] === 'true' ? 'ON' : 'OFF'
        ),
        label
      ).toBe(true)
    }
  })
  it('min R:R and proximity no longer claim a saved value the strategies do not hold', () => {
    expect(card('settings', 'Min risk:reward').recommended).not.toMatch(
      /3 — current config/
    )
    expect(card('settings', 'Proximity filter').systemDefault).not.toMatch(
      /the saved config reads 1\.5/
    )
  })
})

describe('T17/T18/T19/T24/T8 — operations text equals the code', () => {
  it('T17: the update job age limit is internal/updaterjob MaxJobAge and the NT8 restart comes after the park', () => {
    const m = repo('internal/updaterjob/job.go').match(
      /const MaxJobAge = (\d+) \* time\.Minute/
    )
    expect(m).not.toBeNull()
    const u = sectionText('updates')
    expect(u).toContain(`within ${m![1]} minutes of pressing Update`)
    expect(repo('internal/updaterworker/runner.go')).toMatch(
      /a\.AcceptSeq == j\.NT8\.AckAcceptSeq/
    )
    expect(u).toContain('restart NT8 AFTER the job has parked')
  })
  it('T18: pause never claims to stop resting orders (the pause has no cancel path)', () => {
    const pause = repo('trader/auto_trader_pause.go')
    expect(pause).not.toMatch(/cancelArmedOrders/)
    expect(sectionText('buttons')).toContain(
      'ALREADY RESTING at the broker are NOT cancelled'
    )
  })
  it('T19: Save reloads the bound running traders — the guide no longer says a restart is needed', () => {
    const src = repo('api/strategy.go')
    expect(src).toMatch(/s\.traderManager\.RemoveTrader\(t\.ID\)/)
    expect(src).toMatch(/LoadUserTradersFromStore\(s\.store, userID\)/)
    for (const id of ['buttons', 'faq']) {
      expect(sectionText(id)).not.toMatch(
        /needs the trader reload(\/| or )restart/
      )
    }
  })
  it('T24: the chart boot line READS the stitch mode and the guide does not call the roll un-adjusted by default', () => {
    expect(repo('api/handler_klines_roll_derive.go')).toMatch(
      /os\.Getenv\("CHART_ROLL_STITCH"\)\)\) != "legacy"/
    )
    const main = repo('main.go')
    expect(main).not.toMatch(/step never adjusted/)
    expect(main).toMatch(
      /prior-segment stitch=%s[\s\S]{0,200}api\.ChartRollStitchResolved\(\)/
    )
    expect(sectionText('status')).not.toMatch(/that is never back-adjusted/)
  })
  it('T8: the banner is described as a build-identity check, not a content check', () => {
    expect(sectionText('faq')).not.toMatch(
      /amber means the guide is older than the bot/
    )
    expect(sectionText('status')).toMatch(
      /build-identity check, not a content check/
    )
  })
})

// r24 ADDON-CLOSE-ROUTING (rescan N-1 / N-2): Emergency Flat and Positions →
// Close are ONE market close naming the trader's bound account. From AddOn
// build 2026-10-14-a the AddOn flattens that account only (fail-closed), and
// NT8's Account.Flatten cancels every working order for the contract on it —
// resting entries included. The guide, the modal and the 60-second checklist
// are pinned to the AddOn source and the build floor, never to a sentence.
describe('ADDON-CLOSE-ROUTING — Emergency Flat says what the AddOn does', () => {
  const addon = repo('ninjascript/VLTraderTCPClient.cs')
  const expected = repo('provider/ninjatrader/order_snapshot.go').match(
    /const ExpectedAddonBuild = "([^"]+)"/
  )
  const modal = web('src/components/trader/EmergencyFlatButton.tsx')
  it('the close is routed by the NAMED account and is a full Flatten (the code the text describes)', () => {
    expect(addon).toContain('closeTarget = FindAccountByName(namedAccount);')
    expect(addon).toContain('closeTarget.Flatten(new[] { instrument });')
    expect(expected).not.toBeNull()
    // The guide names the routing floor; the build Go expects is at or past it
    // (build ids compare bytewise — the ISO-date prefix is kept for that).
    expect(sectionText('buttons')).toContain('build 2026-10-14-a or newer')
    expect(expected![1] >= '2026-10-14-a').toBe(true)
  })
  it('no surface claims Emergency Flat leaves resting entries working', () => {
    for (const text of [
      sectionText('buttons'),
      sectionText('routines'),
      modal,
    ]) {
      expect(text).not.toMatch(
        /(does NOT|not|never) (pause the trader or )?cancel resting entry orders/i
      )
    }
    expect(sectionText('buttons')).toContain('cancels EVERY working MNQ order')
    expect(sectionText('routines')).toContain(
      'cancels every working MNQ order, resting entries included'
    )
    expect(modal).toContain('cancels every working MNQ order')
  })
  it('the buttons section says another trader is never touched, and Positions → Close is the same routing', () => {
    const b = sectionText('buttons')
    expect(b).toContain(
      "Another trader's account, position and orders are never touched"
    )
    expect(b).toContain('routed by the AddOn exactly like Emergency Flat')
    expect(b).not.toContain('Closes one position now.')
  })
})
