"""Owner hypothetical: withdraw all excess over $2,100 daily. No verified provider minimum/cap."""
from pathlib import Path
import csv,json,hashlib
from lucid_live import simulate,Policy
from test_lucid_live import oracle

def main():
 src=Path('/home/hoang/fleet-1017/out/lucid50k-EDA-live-20261023');out=Path('/home/hoang/fleet-1017/out/lucid50k-AE-live-daily-excess-20261023');out.mkdir(exist_ok=False)
 audit=json.loads((src/'audit.json').read_text())
 for name,h in audit['inputs'].items():assert hashlib.sha256(Path(name).read_bytes()).hexdigest()==h
 source=next(n for n in audit['inputs'] if n.endswith('paths.json'));paths=json.loads(Path(source).read_text());summary=[];ledger=[];payouts=[]
 for r in csv.DictReader((src/'summary.csv').open()):
  if r['method'] not in ['A','E'] or r['policy']=='no_live_withdrawals' or r['lock_trigger_cents']!='200000':continue
  key=dict(method=r['method'],qty=int(r['qty']),execution=r['execution'])
  if not r['transition_after']:
   summary.append(dict(**key,status='NO_FIFTH_PAYOUT_TRANSITION',first_day='',last_day='',trades=0,balance_cents=None,floor_cents=None,trading_net_cents=None,gross_withdrawn_cents=None,received_cents=None,first_bonus_objectives_day=None,bonus_credited_cents=0,payout_count=0));continue
  pp=[p for p in paths if p['method']==r['method'] and p['execution']==r['execution']]
  # Bound above all positive gains is provably nonbinding, equivalent to no cap.
  upper=sum(max(0,p.get('gain_ticks',0))*key['qty']*50 for p in pp)+1
  pol=Policy(payout=True,minimum_cents=1,maximum_cents=upper,split_bps=9000,reserve_cents=200000)
  got=simulate(pp,key['qty'],r['transition_after'],pol)
  for k,v in oracle(pp,key['qty'],r['transition_after'],pol).items():assert got[k]==v
  for l in got['ledger']:
   if l['outcome']!='LIVE_MLL_BREACH':assert l['payout_cents']==max(0,l['start_cents']+l['pnl_cents']-210000)
  assert all(p['after_cents']==210000 for p in got['payouts'])
  ledger.extend(dict(**key,**l) for l in got.pop('ledger'));pay=got.pop('payouts');payouts.extend(dict(**key,**p) for p in pay)
  summary.append(dict(**key,**got,payout_count=len(pay)))
 for name,rr in [('summary.csv',summary),('ledger.csv',ledger),('payouts.csv',payouts)]:
  with (out/name).open('x') as f:w=csv.DictWriter(f,fieldnames=rr[0]);w.writeheader();w.writerows(rr)
 with (out/'verification.json').open('x') as f:json.dump(dict(status='PASS',source_hashes=audit['inputs'],engine_sha256=hashlib.sha256(Path('lucid_live.py').read_bytes()).hexdigest(),runner_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),independent_decimal_oracle=True,all_nonbreach_days_withdraw_exact_excess=True,payouts_leave_exactly_210000_cents=True,summary_rows=len(summary),ledger_rows=len(ledger),payout_rows=len(payouts),artifacts={n:hashlib.sha256((out/n).read_bytes()).hexdigest() for n in ['summary.csv','ledger.csv','payouts.csv']}),f,indent=2)
 for r in summary:print(r)
if __name__=='__main__':main()
