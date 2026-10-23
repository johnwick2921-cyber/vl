"""E/D/A only: audited source entry recomputation plus Lucid account path replay."""
from pathlib import Path
import csv,json,argparse,multiprocessing
from concurrent.futures import ProcessPoolExecutor
import tick_orb as e
import ema_orb as ema
import ema_halfbox as half
import body_retest as body
import stop_variants as breakout
from lucid_ema_funded import account
ROOT=Path('/home/hoang/fleet-1017/out')
SOURCES={'E':('orb-ema9-halfbox-20261022',{'orb_minutes':'2'}),'D':('orb-body-confirmed-retest-20261020',{'orb_minutes':'5','mode':'half_box','body_rule':'full_body'}),'A':('orb-six-month-stop-variants-20261020',{'orb_minutes':'5','mode':'box'})}

def trade_path(ticks,b,cutoff,method,stress):
 if not b['entry_row']:return dict(traded=False,outcome=b['outcome'],reason=b.get('reason',''))
 side=1 if b['side']=='LONG' else -1;slip=int(stress)
 original=int(float(b['entry_points'])*4)
 idx=next(i for i,t in enumerate(ticks) if t[0]==int(b['entry_row']))
 assert ticks[idx][1]==int(b['entry_event_ns']) and ticks[idx][2]==int(b['entry_recv_ns'])
 if method=='D' and stress:
  # Conservative entry sensitivity: must trade one tick THROUGH resting limit.
  idx=next((i for i in range(idx,len(ticks)) if ticks[i][1]<cutoff and (ticks[i][4]-original)*side<=-1),None)
  if idx is None:return dict(traded=False,outcome='NO_THROUGH_FILL',reason='touch_only_limit')
 if method!='D':assert ticks[idx][4]==original
 entry=original+(side*slip if method!='D' else 0)
 width=int(float(b['width_points'])*4)
 risk=width if method=='A' else (width+1)//2
 start=idx if method=='D' else idx+1
 future=[t for t in ticks[start:] if t[1]<cutoff]
 initial=(ticks[idx][4]-entry)*side
 lows=[(ticks[idx][0],ticks[idx][1],initial)];minimum=initial
 outcome=None;end=None;gain=None
 for t in future:
  g=(t[4]-entry)*side
  if g<minimum:minimum=g;lows.append((t[0],t[1],g))
  if g<=-risk:outcome='SL';end=t;gain=g-slip;break
  if g>=width+slip:outcome='TP';end=t;gain=width;break
 if end is None:
  end=[t for t in ticks[idx:] if t[1]<cutoff][-1];outcome='CUTOFF_PROXY';gain=(end[4]-entry)*side-slip
 if gain<minimum:minimum=gain;lows.append((end[0],end[1],gain))
 # Independent first-passage search.
 gains=[side*(t[4]-entry) for t in future];n=len(gains)
 si=next((i for i,g in enumerate(gains) if g<=-risk),n);ti=next((i for i,g in enumerate(gains) if g>=width+slip),n);i=min(si,ti)
 assert outcome==('CUTOFF_PROXY' if i==n else 'SL' if si<ti else 'TP')
 if i<n:assert end[0]==future[i][0]
 return dict(traded=True,outcome=outcome,entry_row=ticks[idx][0],entry_event_ns=ticks[idx][1],exit_row=end[0],exit_event_ns=end[1],entry_ticks=entry,risk_ticks=risk,gain_ticks=gain,min_gain_ticks=minimum,record_lows=lows)

def job(args):
 root,day,bs=args;c,ticks=e.read_day(Path(root)/day,day);assert not c['reason'];_,o,z=e.boundaries(day);out=[]
 for method,b in bs.items():
  if method=='E':
   warm,dates=ema.warm_for(c['instrument_id'],o,2);bad=(dates|{day})&ema.DEGRADED
   if bad:assert b['outcome']=='EXCLUDED' and 'degraded' in b['reason']
   else:
    signal=ema.checked(ticks,o,z,2,20,warm)
    for k in ['signal_row','entry_row','side','entry_points','eligible_ns']:
     assert str(signal.get(k,''))==b.get(k,''),(day,method,k)
   recomputed=half.simulate(ticks,b,z)
  elif method=='D':recomputed=body.checked(ticks,o,z,5,'half_box','full_body')
  else:recomputed=breakout.checked(ticks,o,z,5,'box')
  for k in ['entry_row','exit_row','outcome']:
   assert str(recomputed[k])==b[k],(day,method,k)
  for stress in [False,True]:
   r=trade_path(ticks,b,z,method,stress)
   if r['traded'] and not stress:
    assert r['outcome']==('CUTOFF_PROXY' if b['outcome']=='OPEN_AT_CUTOFF' else b['outcome'])
    if b['exit_row']:assert str(r['exit_row'])==b['exit_row']
   out.append(dict(day=day,method=method,execution='stress1tick' if stress else 'trade_print_proxy',**r))
 return out

def run(root,out):
 out.mkdir(exist_ok=False);by={};hashes={}
 for method,(folder,filters) in SOURCES.items():
  p=ROOT/folder
  audit=json.loads((p/'audit.json').read_text())
  for name,h in audit['artifacts'].items():assert e.p.digest(p/name)==h
  hashes[str(p/'trades.csv')]=e.p.digest(p/'trades.csv')
  for b in csv.DictReader((p/'trades.csv').open()):
   if all(b[k]==v for k,v in filters.items()):by.setdefault(b['day'],{})[method]=b
 warm=ROOT/'orb-ema9-20261022'/'eth-1m.dbn.zst'
 receipt=json.loads((warm.parent/'warmup-receipt.json').read_text());assert e.p.digest(warm)==receipt['sha256']
 paths=[]
 with ProcessPoolExecutor(3,mp_context=multiprocessing.get_context('spawn'),initializer=ema.init,initargs=(str(warm),)) as pool:
  for rr in pool.map(job,[(str(root),day,b) for day,b in sorted(by.items())]):paths.extend(rr);print(rr[0]['day'],'E/D/A verified',flush=True)
 assert len(paths)==732
 e.p.write_json(out/'paths.json',paths)
 results=[];ledgers=[];rolling=[]
 for method in ['E','D','A']:
  for execution in ['trade_print_proxy','stress1tick']:
   pp=sorted([p for p in paths if p['method']==method and p['execution']==execution],key=lambda p:p['day']);trades=[p for p in pp if p['traded']]
   if execution=='trade_print_proxy':assert len(trades)=={'E':115,'D':89,'A':121}[method]
   for qty in (3,6):
    a=account(pp,qty);ledger=a.pop('ledger');ledgers.extend([dict(method=method,execution=execution,**l) for l in ledger])
    a.update(method=method,execution=execution,hypothetical_all_trades_net_cents=sum(p['gain_ticks']*50*qty-100*qty for p in trades),hypothetical_all_trades=len(trades),cutoff_proxy_trades=sum(p['outcome']=='CUTOFF_PROXY' for p in trades))
    results.append(a)
    for i in range(len(trades)-19):
     a=account(trades[i:i+20],qty);a.pop('ledger');rolling.append(dict(method=method,execution=execution,start=trades[i]['day'],end=trades[i+19]['day'],**a))
 e.write_csv(out/'accounts.csv',results);e.write_csv(out/'ledger.csv',ledgers);e.write_csv(out/'rolling20.csv',rolling)
 e.p.write_json(out/'audit.json',dict(engine_sha256=e.p.digest(Path(__file__)),account_engine_sha256=e.p.digest(Path(__file__).with_name('lucid_ema_funded.py')),source_hashes=hashes,warmup_sha256=receipt['sha256'],paths=len(paths),artifacts={n:e.p.digest(out/n) for n in ['paths.json','accounts.csv','ledger.csv','rolling20.csv']}))
 print(json.dumps(results,indent=2))
if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('--root',type=Path,required=True);p.add_argument('--out',type=Path,required=True);a=p.parse_args();run(a.root,a.out)
