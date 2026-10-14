import { armDisabledReasonText, type PlanScenario } from '../../lib/api/plan'

// Legacy missing economics stays UNKNOWN. Existing arm prices may still display,
// but neither an obstacle nor a newly introduced R field is inferred for legacy.
export function ScenarioEconomics({ scenario: s }: { scenario: PlanScenario }) {
  const e = s.economics
  const g = s.arm ?? e?.geometry
  const o = e?.first_obstacle
  const price = (n: number | null | undefined) =>
    typeof n === 'number' && Number.isFinite(n) && n > 0
      ? n.toFixed(2)
      : 'UNKNOWN'
  const risk = g ? Math.abs(g.entry - g.stop) : 0
  const ratio = (n: number | null | undefined) =>
    e &&
    g &&
    risk > 0 &&
    price(g.entry) !== 'UNKNOWN' &&
    price(g.stop) !== 'UNKNOWN' &&
    price(n) !== 'UNKNOWN'
      ? Math.abs(n! - g.entry) / risk
      : null
  const obstacleR = ratio(o?.price)
  const armR = ratio(g?.target)
  return (
    <div
      data-testid={`economics-${s.id}`}
      className="text-[11px] py-1"
      style={{ color: 'var(--vl-muted)' }}
    >
      <div>
        Entry {price(g?.entry)} · Stop {price(g?.stop)} · Entry zone{' '}
        {e?.entry_zone?.map(price).join('–') || 'UNKNOWN'}
      </div>
      <div>
        First obstacle {price(o?.price)} · {o?.level || 'UNKNOWN'} ·{' '}
        {o?.family || 'UNKNOWN'} · Response{' '}
        {o?.response?.replace(/_/g, ' ') || 'UNKNOWN'}
      </div>
      <div>
        Arm target (order objective) {price(g?.target)} · Obstacle R{' '}
        {obstacleR?.toFixed(6) ?? 'UNKNOWN'} · Arm R{' '}
        {armR?.toFixed(6) ?? 'UNKNOWN'}
      </div>
      <div>
        Target path: {s.target_chain?.map(price).join(' → ') || 'UNKNOWN'}
      </div>
      {obstacleR !== null && obstacleR < 1 && (
        <div style={{ color: 'var(--vl-gold)' }}>
          Sub-1R first obstacle · fact only, not a refusal
        </div>
      )}
      {e?.target_path_exception && (
        <div>Path exception: {e.target_path_exception}</div>
      )}
      {e?.role_exceptions?.map((x, i) => (
        <div key={i}>
          Role exception: {x.level} as {x.use} — {x.reason}
        </div>
      ))}
      {!e && (
        <div>
          Legacy economics UNKNOWN — no inferred obstacle or R declarations
        </div>
      )}
      {e && !s.arm && <div>Hypothetical geometry · no arm authorization</div>}
      {s.arm?.arm_disabled_reason && (
        <div style={{ color: 'var(--vl-gold)' }}>
          ⚔️ disabled at write:{' '}
          {armDisabledReasonText(s.arm.arm_disabled_reason)}
        </div>
      )}
      <div>
        Authored geometry before costs and stop composition; broker-accepted
        prices are separate.
      </div>
    </div>
  )
}
