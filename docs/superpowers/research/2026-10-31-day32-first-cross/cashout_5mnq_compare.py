"""Offline 5-MNQ comparison, same cached paths and policies as prior runs."""
import csv,json,hashlib,random
from pathlib import Path
import cashout_50k_max as fresh
import cashout_54k_one_day_max as funded
import lucid_live_5mnq as live5
fresh.live=funded.live=live5.simulate
O=Path('/home/hoang/fleet-1017/out/cashout-5mnq-50k-54k-v2-20261101')
def digest(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def main():
 O.mkdir(exist_ok=False)
 raw=(funded.R/'paths.json').read_bytes()
 assert hashlib.sha256(raw).hexdigest()==json.loads((funded.R/'audit.json').read_text())['artifacts']['paths.json']
 paths=json.loads(raw);rng=random.Random(11012026)
 for i in range(2000):
  ps=[funded.path(j,g:=rng.randint(-600,1400),min(g,rng.randint(-650,0))) for j in range(45)]
  funded.check(ps,5,0,initial=bool(i%2))
 for i in range(2000):
  ps=[funded.path(f'{j:03}',g:=rng.randint(-600,1500),min(g,rng.randint(-700,0))) for j in range(60)]
  policy=live5.Policy(payout=True,minimum_cents=1,maximum_cents=10**12,split_bps=9000,reserve_cents=200000)
  r=live5.simulate(ps,5,'0000-00-00',policy)
  for k,v in funded.live_oracle(ps,5,'0000-00-00',policy).items():assert r[k]==v,(k,r[k],v)
 for initial,first in [(False,5),(True,4)]:
  assert funded.check([funded.path(i,202) for i in range(first-1)],5,0,initial)['payout_count']==0
  assert funded.check([funded.path(i,202) for i in range(first)],5,0,initial)['payout_count']==1
  assert funded.check([funded.path(i,202) for i in range(first+4)],5,0,initial)['payout_count']==1
  assert funded.check([funded.path(i,202) for i in range(first+5)],5,0,initial)['payout_count']==2
 buckets=[[],[],[],[],[]]
 for ex in ['trade_print_proxy','stress1tick']:
  ps=sorted([p for p in paths if p['execution']==ex],key=lambda p:p['day']);assert len(ps)==101
  for start,days,engine in [(50000,0,fresh),(54000,1,funded)]:
   rs=engine.run(ps,5,0);key=dict(execution=ex,initial_usd=start,initial_qualified_days=days)
   assert rs[2][0]['start_cents']==start*100
   for dst,rows in zip(buckets,[[rs[0]],*rs[1:]]):dst.extend(dict(**key,**r) for r in rows)
 for name,rows in zip(['summary','stages','ledger','payouts','skipped'],buckets):
  if rows:
   with (O/(name+'.csv')).open('x',newline='') as f:
    w=csv.DictWriter(f,fieldnames=list(dict.fromkeys(k for r in rows for k in r)));w.writeheader();w.writerows(rows)
 with (O/'audit.json').open('x') as f:json.dump(dict(status='PASS',funded_random_oracle_cases=2000,live_random_oracle_cases=2000,boundary_assertions=8,initial_balance_assertions=4,source_paths_sha256=hashlib.sha256(raw).hexdigest(),sources={Path(m.__file__).name:digest(Path(m.__file__)) for m in [fresh,funded,live5]},script_sha256=digest(Path(__file__)),artifacts={p.name:digest(p) for p in O.glob('*.csv')}),f,indent=2)
 print(json.dumps(buckets[0],indent=2))
if __name__=='__main__':main()
