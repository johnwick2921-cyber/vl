// C12 (release #50) — mobile 390x844: the dashboard header (Emergency Flat +
// ticker) ran off the right edge to ~424px and the chart canvas was ~234px
// wide inside a heavily padded card. jsdom has no layout engine, so this pins
// the responsive classes/CSS that make the row wrap and the card shrink.
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const read = (rel: string) => readFileSync(resolve(__dirname, rel), 'utf8')

describe('C12 mobile layout pins', () => {
  const page = read('./TraderDashboardPage.tsx')
  it('header button row wraps (Emergency Flat can drop below the title)', () => {
    expect(page).toContain(
      'className="flex flex-wrap items-start justify-between gap-3 mb-4"'
    )
    expect(page).toContain('flex flex-wrap items-center gap-2 sm:gap-4')
  })
  it('LAST_UPDATE/EQ ticker wraps instead of overflowing', () => {
    expect(page).toMatch(
      /flex flex-wrap gap-x-4 gap-y-1">\s*<span>LAST_UPDATE::/
    )
    expect(page).toContain('flex flex-wrap gap-x-4 gap-y-1 justify-between')
  })
  it('page + header card gutters shrink below sm; desktop (md) unchanged', () => {
    expect(page).toContain('px-2 sm:px-4 md:px-8')
    expect(page).toContain('p-3 sm:p-6 animate-scale-in')
  })
  it('chart card padding is reduced in the <=768px media block only', () => {
    const css = read('../index.css')
    expect(css).toMatch(
      /\.chart-container \{\s*background:[^}]*padding: 1\.25rem/
    )
    const media = css.slice(css.indexOf('/* Responsive utilities */'))
    expect(media).toMatch(
      /@media \(max-width: 768px\) \{[\s\S]*?\.chart-container \{\s*padding: 0\.25rem;/
    )
  })
  it('chart toolbar wraps so the B/S button is never clipped', () => {
    const chart = read('../components/charts/AdvancedChart.tsx')
    expect(chart).toContain(
      'flex flex-wrap items-center justify-between gap-y-1 px-2 sm:px-4 py-2'
    )
  })
})
