"""Additional audit: monthly reconciliation, streak dates and comparable-day set."""
from pathlib import Path
from decimal import Decimal as D
from collections import defaultdict
from itertools import groupby
import json,csv
R=Path('/home/hoang/fleet-1017/out/orb3-recent-20261031/run-v2')
def write(p,rows):
 with p.open('x',newline='') as f:
  w=csv.DictWriter(f,fieldnames=list(rows[0]));w.writeheader();w.writerows(rows)
def main():
 rows=list(csv.DictReader((R/'trades.csv').open()));ss=json.loads((R/'summary.json').read_text())
 groups=defaultdict(list)
 for r in rows:groups[(r['group'],r['variant'],r['orb'])].append(r)
 common=set.intersection(*[{r['day'] for r in rr if r['outcome']!='EXCLUDED'} for rr in groups.values()])
 months=[];streaks=[];comparables=[]
 for s in ss:
  key=(s['group'],s['variant'],str(s['orb']));rr=sorted(groups[key],key=lambda r:r['day']);closed=[r for r in rr if r['points']!='']
  pct=100*sum(D(r['points'])>0 for r in closed)/len(closed) if closed else None
  assert pct==s['win_percent']
  for month,g in groupby(rr,key=lambda r:r['day'][:7]):
   a=list(g);p=[D(r['points']) for r in a if r['points']!='']
   months.append(dict(group=key[0],variant=key[1],orb=key[2],month=month,closed=len(p),positive=sum(v>0 for v in p),negative=sum(v<0 for v in p),zero=sum(v==0 for v in p),won=sum((v for v in p if v>0),D(0)),lost=sum((v for v in p if v<0),D(0)),gross=sum(p,D(0)),open=sum(r['outcome']=='OPEN_AT_CUTOFF' for r in a),excluded=sum(r['outcome']=='EXCLUDED' for r in a)))
  for sign,g in groupby(closed,key=lambda r:1 if D(r['points'])>0 else -1 if D(r['points'])<0 else 0):
   a=list(g);streaks.append(dict(group=key[0],variant=key[1],orb=key[2],sign=sign,length=len(a),first_day=a[0]['day'],last_day=a[-1]['day'],points=sum((D(r['points']) for r in a),D(0))))
  shared=[r for r in rr if r['day'] in common];p=[D(r['points']) for r in shared if r['points']!='']
  comparables.append(dict(group=key[0],variant=key[1],orb=key[2],shared_days=len(common),closed=len(p),open=sum(r['outcome']=='OPEN_AT_CUTOFF' for r in shared),gross=sum(p,D(0))))
 out=R/'details';out.mkdir(exist_ok=False)
 write(out/'monthly.csv',months);write(out/'streaks.csv',streaks);write(out/'common-days.csv',comparables)
 with (out/'audit.json').open('x') as f:json.dump(dict(status='PASS',win_rates_checked=51,common_days=sorted(common),note='Streaks ignore excluded, unfilled and open days; runs at sample edge may continue outside sample.'),f,indent=2)
 print('PASS: 51 win rates; monthly totals, dated streaks; common days',len(common))
if __name__=='__main__':main()
