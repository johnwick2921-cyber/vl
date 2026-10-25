// Section-3 mock card — composed ONLY from the REAL plan-card components with
// realistic mock props (LIVE-COMPONENT RULE). Styled like the card's visual
// grammar (tokens only). Every chip shown here is the exact one the app ships.
import { BiasBlock } from '../../components/plan/BiasBlock'
import { useResolvedPlanMode } from './useResolvedPlanMode'
import {
  GradeChip,
  ProvenanceChip,
  FreshDot,
  StatusDot,
  VersionChips,
  LifecycleChip,
} from '../../components/plan/chips'
import {
  QualityChip,
  ConfirmChip,
  FvgStateChip,
} from '../../components/plan/ScenarioList'
import { TouchChip } from '../../components/plan/ZoneTable'

const muted = {
  color: 'var(--vl-muted)',
  fontFamily: 'var(--vl-font-ui)',
} as const
const faint = {
  color: 'var(--vl-faint)',
  fontFamily: 'var(--vl-font-ui)',
} as const

export function MockPlanCard() {
  const resolvedMode = useResolvedPlanMode()
  return (
    <div
      data-testid="mock-plan-card"
      className="rounded-xl p-4 flex flex-col gap-2 min-w-0 w-full break-words"
      style={{
        background: 'var(--vl-card)',
        border: '1px solid var(--vl-hair)',
        maxWidth: 420,
      }}
    >
      {/* header: title + version chips + lifecycle */}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span
          className="text-sm font-bold"
          style={{ color: 'var(--vl-ivory)' }}
        >
          Today's Plan
        </span>
        <div className="flex items-center gap-2">
          <VersionChips
            version={3}
            latest={3}
            count={3}
            onSelect={() => {}}
            titleFor={(v) => (v === 3 ? 'owner reset' : undefined)}
            noTradeLabel="NO-TRADE"
          />
          <LifecycleChip lifecycle="active" language="en" />
        </div>
      </div>

      {/* bias block (real) */}
      <BiasBlock
        bias={{
          direction: 'short',
          conviction: 'MEDIUM',
          flip_condition: 'flips long on 2x5m close above 29629',
        }}
        language="en"
      />

      {/* bias-tree line as the card's reasoning opener would show it */}
      <div className="text-[10px]" style={muted}>
        bias-tree: branch 5 premium (price at 72% of the dealing range; longs
        disallowed) · facts match branch 3 (inside day; close short PDC → short
        LOW)
      </div>

      {/* levels (mock rows with REAL chips) */}
      <div className="text-[10px] uppercase tracking-widest" style={faint}>
        Key Levels
      </div>
      {[
        {
          price: 29628.75,
          label: 'ONH',
          grade: 'A',
          role: 'LIQUIDITY/BREAKOUT',
          fresh: 'fresh' as const,
          touch: 'touching',
          dist: '+14.5',
          owner: false,
          consumedDim: false,
        },
        {
          price: 29585.99,
          label: 'VWAP',
          grade: 'A',
          role: 'MAGNET/MEAN-REVERT',
          fresh: 'fresh' as const,
          touch: 'approaching',
          dist: '−28.0',
          owner: false,
          consumedDim: false,
        },
        {
          price: 29638.0,
          label: 'Demand·4h',
          grade: 'A',
          role: 'HTF target only',
          fresh: 'fresh' as const,
          touch: undefined,
          dist: '+23.8',
          owner: false,
          consumedDim: false,
        },
        {
          price: 29541.12,
          label: 'Demand·1h',
          grade: 'C',
          role: 'react zone',
          fresh: 'consumed' as const,
          touch: undefined,
          dist: '−72.9',
          owner: false,
          consumedDim: true,
        },
      ].map((l) => (
        <div
          key={l.price + l.label}
          className="flex flex-wrap items-center gap-2 text-[11px]"
          style={
            l.consumedDim
              ? { opacity: 0.5, fontFamily: 'var(--vl-font-ui)' }
              : { fontFamily: 'var(--vl-font-ui)' }
          }
        >
          <span
            className="vl-num font-bold"
            style={{ color: 'var(--vl-ivory)' }}
          >
            {l.price.toFixed(2)}
          </span>
          <ProvenanceChip label={l.label} />
          <GradeChip grade={l.grade} />
          <span
            className="text-[9px] px-1 rounded"
            style={{
              border: '1px solid var(--vl-hair)',
              color: 'var(--vl-muted)',
              fontFamily: 'var(--vl-font-ui)',
            }}
          >
            {l.role}
          </span>
          <FreshDot fresh={l.fresh} language="en" />
          {l.touch && <TouchChip state={l.touch} />}
          <span className="vl-num ml-auto" style={faint}>
            {l.dist}
          </span>
        </div>
      ))}

      {/* scenarios (real chips) */}
      {/* GUIDE CONTENT LAW: this header used to type "advisory" while the
            bound strategy resolved strict. A mock that names a mode is making
            a claim, so it reads the resolved mode or says it is not reading
            one — it never asserts a mode it did not resolve. */}
      <div
        data-testid="mock-scenarios-header"
        className="text-[10px] uppercase tracking-widest"
        style={faint}
      >
        Scenarios ({resolvedMode})
      </div>
      <div className="flex flex-col gap-1">
        <div className="flex flex-wrap items-center gap-2 text-[11px]">
          <StatusDot status="armed" language="en" />
          <span className="font-bold" style={{ color: 'var(--vl-gold)' }}>
            S1
          </span>
          <QualityChip quality="A+" />
          <span className="uppercase" style={{ color: 'var(--vl-short)' }}>
            short
          </span>
          <span style={muted}>sweep 29628.75 ONH and reclaim inside</span>
        </div>
        <div className="flex flex-wrap items-center gap-2 text-[11px]">
          <StatusDot status="waiting" language="en" />
          <span className="font-bold" style={{ color: 'var(--vl-gold)' }}>
            S2
          </span>
          <QualityChip quality="B" />
          <span className="uppercase" style={{ color: 'var(--vl-short)' }}>
            short
          </span>
          <span style={muted}>
            fvg_entry at the 29620 gap (chain_after: S1)
          </span>
          <FvgStateChip
            f={{
              id: 'S2',
              fvg_lo: 29614,
              fvg_hi: 29626,
              ce: 29620,
              entry_mode: 'edge',
              state: 'IN_ZONE',
              touch_number: 1,
              met: false,
            }}
          />
          <ConfirmChip
            id="S2"
            c={{
              rule: 'touch',
              ref_price: 29620,
              side: 'above',
              met: true,
              detail:
                'touched 1× — MET (stale: written 2h ago, price now −30pt; treat as expired)',
            }}
          />
        </div>
        <div className="flex flex-wrap items-center gap-2 text-[11px]">
          <StatusDot status="armed" language="en" />
          <span className="font-bold" style={{ color: 'var(--vl-gold)' }}>
            S3
          </span>
          <QualityChip quality="C" />
          <span className="uppercase" style={{ color: 'var(--vl-muted)' }}>
            neutral
          </span>
          <span style={muted}>
            reject 29638 Demand·4h — only on a confirmed sweep
          </span>
        </div>
      </div>

      {/* death + flip */}
      <div className="text-[10px]" style={muted}>
        Plan dies if · acceptance above 29628.75 (2x5m) · Flips · 2x5m close
        above 29629 → bias long
      </div>

      <div className="text-[9px]" style={faint}>
        v3 · owner_reset · Model: deepseek-v4-pro · 3 re-reads left
      </div>
    </div>
  )
}
