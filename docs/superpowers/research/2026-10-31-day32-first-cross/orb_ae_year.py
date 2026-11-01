"""One-year A/E replay on identical definitions and receipt-consistent EMA."""
import sys,json,csv,multiprocessing,hashlib
from pathlib import Path
from datetime import date,timedelta
from concurrent.futures import ProcessPoolExecutor
from decimal import Decimal as D
from itertools import groupby
sys.path.append('/home/hoang/vl-1017-databento-ready/docs/superpowers/research/2026-10-20-databento-ready')
import tick_orb as e
import stop_variants as a
import ema_orb as warm
import orb_ema_receipt as ema
import ema_halfbox as half
from orb3_run_v2 import normalize,summary
ROOT=Path('/home/hoang/fleet-1017/out')
OUT=ROOT/'orb-AE-one-year-20261031'
W=[ROOT/'orb-ema9-20261022/eth-1m.dbn.zst',ROOT/'day32-recent-six-months-20261031/warmup/eth-1m.dbn.zst']
def init():
 warm.WARM={};warm.DEGRADED=set()
 for p in W:
  assert e.p.digest(p)==json.loads((p.parent/'warmup-receipt.json').read_text())['sha256']
  b=warm.load_warm(p)
  for iid,rows in b.items():warm.WARM.setdefault(iid,[]).extend(rows)
  warm.DEGRADED|={r['date'] for r in json.loads((p.parent/'dataset-condition.json').read_text()) if r['condition']!='available'}
 for rows in warm.WARM.values():assert all(x[0]<y[0] for x,y in zip(rows,rows[1:]))
def job(day):
 folder=ROOT/'databento-mnq-20261020-data'/day
 if not (folder/'complete.json').exists():folder=ROOT/'orb3-recent-20261031/data'/day
 c,t=e.read_day(folder,day);_,o,z=e.boundaries(day)
 if c['reason']:return c,[normalize(day,c['contract'],m,g,v,dict(outcome='EXCLUDED',reason=c['reason'])) for m,g,v in [(5,'A','box'),(2,'E','halfbox_fullbox')]]
 ar=a.checked(t,o,z,5,'box');out=[normalize(day,c['contract'],5,'A','box',ar)]
 closes,dates=warm.warm_for(c['instrument_id'],o,2);bad=(dates|{day})&warm.DEGRADED
 if bad:er=dict(outcome='EXCLUDED',reason='provider_degraded:'+','.join(sorted(bad)))
 else:
  recv=[];s=e.db.DBNStore.from_file(folder/'ticks.dbn.zst')
  for ix,r in enumerate(s):
   if r.instrument_id==c['instrument_id'] and o<=r.ts_recv<z:recv.append((ix,r.ts_event,r.ts_recv,r.sequence,r.price//e.UNIT,r.publisher_id))
  s.reader.close()
  base=ema.checked(recv,o,z,2,20,closes)
  er=half.simulate(recv,{k:str(v) for k,v in dict(day=day,contract=c['contract'],**base).items()},z)
 out.append(normalize(day,c['contract'],2,'E','halfbox_fullbox',er))
 raw={x[0]:x for x in t}
 for r in out:
  if r['points']=='' or r['outcome']=='TP':continue
  assert r['points']==(1 if r['side']=='LONG' else -1)*(raw[int(r['exit_row'])][4]/4-float(r['entry_points']))
 return c,out
def main():
 OUT.mkdir(exist_ok=False);days=[];d=date(2025,10,31)
 while d<=date(2026,10,30):
  if d.weekday()<5:days.append(str(d))
  d+=timedelta(days=1)
 assert len(days)==261
 rows=[];sessions=[]
 with ProcessPoolExecutor(3,initializer=init,mp_context=multiprocessing.get_context('spawn')) as pool:
  for c,rr in pool.map(job,days):
   rows.extend(rr);sessions.append(c)
   if len(sessions)%20==0:print('Checked',len(sessions),'of',len(days),c['day'],flush=True)
 assert len(rows)==522 and len({(r['day'],r['group']) for r in rows})==522
 ss=[summary(rows,5,'A','box'),summary(rows,2,'E','halfbox_fullbox')]
 # Independent Decimal totals and maximal sign runs.
 for s in ss:
  rr=[r for r in rows if r['group']==s['group']];p=[D(str(r['points'])) for r in rr if r['points']!='']
  assert sum(p,D(0))==D(str(s['gross_points']))
  assert s['entries']+s['no_entry']+s['excluded']==261
  assert s['entries']==len(p)+s['open']
  runs=[(k,len(list(g))) for k,g in groupby(p,key=lambda v:1 if v>0 else -1 if v<0 else 0)]
  assert max([n for k,n in runs if k==1],default=0)==s['win_streak']
  assert max([n for k,n in runs if k==-1],default=0)==s['loss_streak']
 # Exact overlap check against already audited recent-half-year ledger.
 source=ROOT/'orb3-recent-20261031/run-v2'
 audit=json.loads((source/'audit.json').read_text());assert e.p.digest(source/'ledger.json')==audit['artifacts']['ledger.json']
 prior=json.loads((source/'ledger.json').read_text());matched=0
 lookup={(r['day'],'A' if r['group']=='breakout' else 'E'):r for r in prior if (r['group'],r['variant'],r['orb']) in [('breakout','box',5),('EMA9','halfbox_fullbox',2)]}
 for r in rows:
  old=lookup.get((r['day'],r['group']))
  if old is None:continue
  for k in ['outcome','entry_row','exit_row','entry_points','points','orb_high','orb_low','reason']:assert r.get(k)==old.get(k),(r['day'],r['group'],k,r.get(k),old.get(k))
  matched+=1
 assert matched==262
 for name,data in [('summary.csv',ss),('trades.csv',rows),('sessions.csv',sessions)]:e.write_csv(OUT/name,data)
 monthly=[]
 for g in ['A','E']:
  for month,rr in groupby([r for r in rows if r['group']==g],key=lambda r:r['day'][:7]):
   rs=list(rr);p=[D(str(r['points'])) for r in rs if r['points']!='']
   monthly.append(dict(group=g,month=month,TP=sum(r['outcome']=='TP' for r in rs),SL=sum(r['outcome']=='SL' for r in rs),open=sum(r['outcome']=='OPEN_AT_CUTOFF' for r in rs),points=str(sum(p,D(0)))))
 e.write_csv(OUT/'monthly.csv',monthly)
 with (OUT/'summary.json').open('x') as f:json.dump(ss,f,indent=2)
 with (OUT/'audit.json').open('x') as f:json.dump(dict(status='PASS',period=['2025-10-31','2026-10-30'],candidate_days=261,rows=522,recent_overlap_rows_identical=matched,warmup_hashes={str(p):e.p.digest(p) for p in W},source_hashes={m.__name__:e.p.digest(Path(m.__file__)) for m in [e,a,warm,ema,half]},harness_hash=e.p.digest(Path(__file__)),artifacts={n:e.p.digest(OUT/n) for n in ['summary.csv','trades.csv','sessions.csv','monthly.csv']}),f,indent=2)
 print(json.dumps(ss,indent=2),flush=True)
if __name__=='__main__':main()
