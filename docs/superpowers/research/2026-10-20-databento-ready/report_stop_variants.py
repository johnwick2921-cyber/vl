"""Publish only after raw-tape oracle and frozen-baseline checks passed."""
import csv,json,zipfile
from pathlib import Path
import sys
from xml.etree import ElementTree as ET
import tick_orb as e
from xlsx_tick_tables import export

def report(p):
    audit=json.loads((p/'audit.json').read_text())
    assert audit['status']=='PASS_WITH_CAVEATS'
    for name,h in audit['artifacts'].items():assert e.p.digest(p/name)==h
    rows=list(csv.DictReader((p/'trades.csv').open()))
    summary=list(csv.DictReader((p/'summary.csv').open()))
    for s in summary:
        group=[r for r in rows if r['mode']==s['mode'] and r['orb_minutes']==s['orb_minutes']]
        for k in ('TP','SL','BE'):assert sum(r['outcome']==k for r in group)==int(s[k])
        closed=[r for r in group if r['outcome'] in ('TP','SL','BE')]
        assert sum(float(r['pnl_planned_points']) for r in closed)==float(s['net_planned_points'])
        assert float(s['won_points'])+float(s['lost_points'])==float(s['net_planned_points'])
    lines=['# MNQ ORB: three stop rules, six months','',
       '**Computational checks PASS, with simulation limitations below.**','',
       'Period: 2025-10-20 through 2026-04-19. 08:30 Houston open (DST aware). '
       'Separate 1m, 2m, 5m ranges. First trade one tick outside completed ORB enters '
       'at that trade price. One trade per day per test; no re-entry. TP always one '
       'full box width from entry. Box means ORB high minus low.','',
       'Rules: box = SL one full box (1:1); half_box = SL half box; '
       '20_to_BE = initial SL20 points, move stop to entry after a trade reaches +20 points. '
       'A target reached before or on that activation print exits at TP, not BE. '
       'Moved BE stop becomes active for subsequent records; zero modeled amendment latency.','',
       '| Stop rule | ORB | Entries | TP | SL | BE | Open | TP % of entries | Points won | Points lost | Net planned points |',
       '|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|']
    for s in summary:
        vals=[s[k] for k in ('mode','orb_minutes','entries','TP','SL','BE','open')]
        vals+=[f"{float(s[k]):.3f}" for k in ('target_pct','won_points','lost_points','net_planned_points')]
        lines.append('| '+' | '.join(vals)+' |')
    lines+=['','No commission, spread, or latency costs deducted. Planned stop accounting assumes '
        'an exact stop fill. Observed crossing-trade PnL is separately preserved; a BE crossing '
        'can produce a negative observed result. These are trade-path outcomes, not broker-fill proof.','',
        'Half-box odd tick widths produce nominal 0.125-point increments, which are not valid '
        'MNQ order prices. Primary table is the exact half-box research calculation requested; '
        'outward rounding to 0.25-point ticks uses the same crossing tick and its net PnL '
        'is in net_tick_rounded_points. Do not submit half-tick stop prices to a broker.','',
        'Coverage: 130 candidate weekdays, '+str(audit['included_days'])+' pass full-session '
        'checks. Same exclusions and entry row IDs as frozen baseline. Additional ORB exclusions '
        'are in summary.csv and trades.csv. Full 390-minute coverage gate excludes early '
        'closes and some incomplete sessions; exclusions are not all labeled holidays. '
        'Any still-open trade at 15:00 is OPEN_AT_CUTOFF, not assigned an invented exit.','',
        '## Verification','',
        '- '+str(audit['independently_checked_rows'])+' variant rows checked with separate first-crossing math.',
        '- Every included entry compared to the frozen six-month baseline.',
        '- Raw file SHA256, record counts, contract selection, flags and timestamp checks use the prior audited decoder.',
        '- 10 tests in CT and UTC; 6000 synthetic comparisons per timezone; 5 assertion-killed mutants.',
        '- All summary arithmetic reconciled to per-trade rows. Excel cells round-trip checked against CSV.',
        '- Independent algorithm shares raw decoding and data gates; no independent exchange-tape completeness claim.',
        '- Audit checklist reference: docs/superpowers/AUDIT-CHECKLIST.md @ ba50edf22.',
        '- Engine SHA256: '+audit['engine_sha256'], '',
        'All nine tests overlap in dates. Their sum is not a one-trade-per-day portfolio. '
        'This is historical research only; no production config, DB, NT8 or trading changes.']
    export(p,p/'TRADES.xlsx')
    ns='{http://schemas.openxmlformats.org/spreadsheetml/2006/main}'
    with zipfile.ZipFile(p/'TRADES.xlsx') as z:
        for i,n in enumerate(('trades.csv','sessions.csv'),1):
            expected=list(csv.reader((p/n).open()))
            xml=ET.fromstring(z.read(f'xl/worksheets/sheet{i}.xml'))
            actual=[[''.join(c.itertext()) for c in row] for row in xml.findall(ns+'sheetData/'+ns+'row')]
            assert actual==expected
    with (p/'REPORT.md').open('x') as f:f.write('\n'.join(lines)+'\n')
    verification=Path(__file__).with_name('verification-stop-variants.json')
    with (p/'verification-stop-variants.json').open('xb') as f:f.write(verification.read_bytes())
    with zipfile.ZipFile(p/'FULL-REPORT.zip','x',zipfile.ZIP_DEFLATED) as z:
        for n in ('REPORT.md','summary.csv','trades.csv','sessions.csv','TRADES.xlsx','audit.json','verification-stop-variants.json'):
            z.write(p/n,n)
    print((p/'REPORT.md').read_text())
if __name__=='__main__':report(Path(sys.argv[1]))
