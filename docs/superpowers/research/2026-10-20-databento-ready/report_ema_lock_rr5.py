import csv,json,hashlib
from pathlib import Path
from decimal import Decimal as D
from collections import Counter
p=Path('/home/hoang/fleet-1017/out/orb-ema9-lock5-rr5-20261022')
a=json.loads((p/'audit.json').read_text())
for name,h in a['artifacts'].items():assert hashlib.sha256((p/name).read_bytes()).hexdigest()==h
rows=list(csv.DictReader((p/'trades.csv').open()));sums=list(csv.DictReader((p/'summary.csv').open()))
base={(r['day'],r['orb_minutes']):r for r in csv.DictReader(open('/home/hoang/fleet-1017/out/orb-ema9-20261022/trades.csv')) if r['tp_distance']=='20'}
assert len(rows)==366==len({(r['day'],r['orb_minutes']) for r in rows})
for r in rows:
 b=base[r['day'],r['orb_minutes']]
 for k in ['entry_row','entry_points','entry_event_ns','side','contract']:assert r[k]==b[k]
 if not r['entry_row']:continue
 w=D(r['orb_high'])-D(r['orb_low']);side=1 if r['side']=='LONG' else -1
 assert (D(r['target_points'])-D(r['entry_points']))*side==5*w/2
 assert (D(r['entry_points'])-D(r['stop_points']))*side==w/2
 if r['pnl_points']:assert D(r['pnl_points'])==({'TP':5*w/2,'SL':-w/2,'LOCK':D(5)}[r['outcome']])
for s in sums:
 rr=[r for r in rows if r['orb_minutes']==s['orb_minutes']];c=Counter(r['outcome'] for r in rr)
 for k,o in [('TP','TP'),('SL','SL'),('LOCK','LOCK'),('open','OPEN_AT_CUTOFF')]:assert int(s[k])==c[o]
 for key,col in [('net_points','pnl_points'),('rounded_net_points','rounded_pnl_points'),('observed_net_points','observed_pnl_points')]:assert D(s[key])==sum(D(r[col]) for r in rr if r[col])
 assert D(s['net_points'])==D(s['won_points'])+D(s['lost_points'])
head=['orb_minutes','entries','TP','SL','LOCK','open','won_points','lost_points','net_points','rounded_net_points']
lines=['# EMA9 ORB: TP5R, SL half box, lock +5 at +1R','', 'Period: 2025-10-20 through 2026-04-19. 08:30 America/Chicago open. Same frozen continuous-EMA9 closed-candle entry. One entry/day per independent timeframe. Primary requested comparison: ORB2m.','', '| '+' | '.join(head)+' |','|'+'|'.join(['---']*len(head))+'|']
lines+=['| '+' | '.join(s[k] for k in head)+' |' for s in sums]
lines+=['','Gross nominal realized points only; fees/slippage excluded. OPEN_AT_CUTOFF at15:00 omitted from realized totals; marks retained in trades.csv. Half-tick nominal levels are not executable: rounded totals are separate. Lock activates at max(1R,5.25 points), becomes effective next record, and stays fixed. No continuous trailing.','', 'Validation: 16 tests in CT and UTC, 6000 randomized paths each, 3 caught mutations; raw tick checksums and entry/ORB checked during replay; independent first-passage exit oracle; all366 rows reconciled with original entries and Decimal arithmetic. Shared source/decoder, not independent data or broker fills. Audit reference docs/superpowers/AUDIT-CHECKLIST.md @ba50edf22.']
with (p/'REPORT.md').open('x') as f:f.write('\n'.join(lines)+'\n')
with (p/'final-verification.json').open('x') as f:json.dump({'rows_checked':len(rows),'entries_checked':sum(bool(r['entry_row']) for r in rows),'source_entry_parity':True,'decimal_reconciliation':True},f,indent=2)
print(json.dumps(sums,indent=2))
