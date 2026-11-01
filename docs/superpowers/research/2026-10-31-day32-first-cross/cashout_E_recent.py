"""Recent filtered E, old owner cashout assumptions, intratrade MLL paths."""
import sys,json,csv,multiprocessing,hashlib
from pathlib import Path
from datetime import datetime
from zoneinfo import ZoneInfo
from concurrent.futures import ProcessPoolExecutor
sys.path.append('/home/hoang/vl-1017-databento-ready/docs/superpowers/research/2026-10-20-databento-ready')
import tick_orb as e
import lucid_eda_funded as pathengine
import lucid_lifecycle14 as life
import lucid_rebuy as rebuy
import lucid_payout as funded
from test_lucid_payout import oracle
ROOT=Path('/home/hoang/fleet-1017/out');SRC=ROOT/'orb-AE-one-year-20261031';OUT=ROOT/'cashout-E-before09-recent-20261031'
def job(b):
 day=b['day'];folder=ROOT/'orb3-recent-20261031/data'/day
 c,t=e.read_day(folder,day);assert not c['reason'];assert c['contract']==b['contract'];_,o,z=e.boundaries(day)
 out=[]
 for stress in [False,True]:
  p=pathengine.trade_path(t,b,z,'E',stress)
  if not stress:assert p['outcome']==b['outcome'] and p['exit_row']==int(b['exit_row']) and p['gain_ticks']/4==float(b['points'])
  out.append(dict(day=day,execution='stress1tick' if stress else 'trade_print_proxy',**p))
 return out

def main():
 OUT.mkdir(exist_ok=False)
 audit=json.loads((SRC/'audit.json').read_text());assert e.p.digest(SRC/'trades.csv')==audit['artifacts']['trades.csv']
 bs=[r for r in csv.DictReader((SRC/'trades.csv').open()) if r['group']=='E' and r['day']>='2026-05-01' and r['entry_row'] and datetime.fromtimestamp(int(r['entry_event_ns'])/1e9,ZoneInfo('America/Chicago')).strftime('%H:%M')<'09:00']
 assert len(bs)==101 and sum(float(b['points']) for b in bs)==392.25
 paths=[]
 with ProcessPoolExecutor(3,mp_context=multiprocessing.get_context('spawn')) as pool:
  for n,pp in enumerate(pool.map(job,bs),1):
   paths.extend(pp)
   if n%25==0:print('Path checked',n,flush=True)
 e.p.write_json(OUT/'paths.json',paths)
 summary=[];stages=[];ledger=[];payouts=[];skipped=[];single=[]
 for ex in ['trade_print_proxy','stress1tick']:
  ps=sorted([p for p in paths if p['execution']==ex],key=lambda p:p['day'])
  for qty in [3,6]:
   first=funded.simulate(ps,qty);expected=oracle(ps,qty)
   for k,v in expected.items():assert first[k]==v
   single.append(dict(execution=ex,**{k:v for k,v in first.items() if k not in ['ledger','payouts']}))
   for scenario in ['repeat_funded_hypothetical','live_after_five_cooldown14']:
    if scenario.startswith('repeat'):
     s,st,le,pa=rebuy.repeat(ps,qty);sk=[]
     # Independently recheck each replacement account from its dated source subset.
     for acct in st:
      subset=[p for p in ps if acct['first_day']<=p['day']<=acct['last_day']]
      check=oracle(subset,qty)
      for k in ['trades','payout_count','received_cents','ending_balance_cents']:assert acct[k]==check[k]
    else:s,st,le,pa,sk=life.run(ps,qty)
    key=dict(execution=ex,scenario=scenario,qty=qty)
    summary.append(dict(**key,**{k:v for k,v in s.items() if k!='qty'}))
    for dst,data in [(stages,st),(ledger,le),(payouts,pa),(skipped,sk)]:dst.extend(dict(**key,**{k:v for k,v in r.items() if k!='qty'}) for r in data)
 for name,rs in [('summary.csv',summary),('stages.csv',stages),('ledger.csv',ledger),('payouts.csv',payouts),('single-account.csv',single)]:e.write_csv(OUT/name,rs)
 if skipped:e.write_csv(OUT/'skipped.csv',skipped)
 e.p.write_json(OUT/'audit.json',dict(status='PASS',period=['2026-05-01','2026-10-30'],source_sha256=e.p.digest(SRC/'trades.csv'),filtered_trades=101,baseline_points=392.25,path_replays=202,independent_account_oracles=True,assumptions=dict(fee_roundtrip_usd_per_MNQ=1,owner_reserve_above_locked_MLL=2000,live_transition_next_signal_hypothetical=True,evaluation_time_omitted=True,purchase_fees_unknown=True,live_bonus_not_credited=True),sources={m.__name__:e.p.digest(Path(m.__file__)) for m in [e,pathengine,life,rebuy,funded]},artifacts={p.name:e.p.digest(p) for p in OUT.iterdir() if p.is_file()}))
 print(json.dumps(summary,indent=2),flush=True);print('FIRST ACCOUNTS',json.dumps(single,indent=2),flush=True)
if __name__=='__main__':main()
