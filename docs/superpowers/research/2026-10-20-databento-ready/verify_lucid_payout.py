import csv,json,hashlib,unittest,io,types
from pathlib import Path
import test_lucid_payout as t
import lucid_payout as engine
P=Path('/home/hoang/fleet-1017/out/lucid50k-EDA-payout-20261023')
audit=json.loads((P/'audit.json').read_text())
paths=json.loads(Path(audit['source_path']).read_text())
for name,h in audit['artifacts'].items():assert hashlib.sha256((P/name).read_bytes()).hexdigest()==h
rows=lambda name:list(csv.DictReader((P/name).open()))
summaries=rows('summary.csv');ledger=rows('ledger.csv');payouts=rows('payouts.csv')
for s in summaries:
 key=lambda x:all(str(x[k])==s[k] for k in ['method','execution','qty'])
 ps=sorted([p for p in paths if p['method']==s['method'] and p['execution']==s['execution']],key=lambda p:p['day'])
 expected=t.oracle(ps,int(s['qty']))
 for k,v in expected.items():assert str(v)==s[k],(k,v,s[k])
 ls=[l for l in ledger if key(l)];pay=[p for p in payouts if key(p)]
 balance=5000000;cycle=0;days=0
 for l in ls:
  assert int(l['start_cents'])==balance
  net=int(l['pnl_cents']);cycle+=net;days+=net>=15000
  assert cycle==int(l['cycle_net_before_payout_cents']) and days==int(l['qualifying_days_before_payout'])
  before=balance+net;assert before==int(l['before_payout_cents'])
  gross=int(l['payout_cents']);balance=before-gross;assert balance==int(l['end_cents'])
  if gross:
   assert days>=5 and cycle>0 and 50000<=gross<=200000
   assert gross<=(before-5000000)//2 and balance>=5210000
   assert gross==min((before-5000000)//2,200000,before-5210000)
   r=next(p for p in pay if p['day']==l['day'])
   assert int(r['gross_cents'])==gross and int(r['received_cents'])==gross*9//10 and int(r['after_cents'])==balance
   cycle=0;days=0
 assert sum(int(p['gross_cents']) for p in pay)==int(s['gross_withdrawn_cents'])
 assert sum(int(p['received_cents']) for p in pay)==int(s['received_cents'])
 assert balance==int(s['ending_balance_cents'])
# Run source mutations in memory; tests must reject every altered engine.
source=Path(engine.__file__).read_text();mutations=[]
for name,old,new in [('buffer_missing','balance-5210000','balance-5010000'),('half_profit_removed','(balance-5000000)//2','balance-5000000'),('four_days','days>=5','days>=4'),('no_cycle_reset','days=0;cycle=0','days=days;cycle=cycle'),('wrong_split','gross*9//10','gross'),('six_payouts','len(payouts)==5','len(payouts)==6'),('no_balance_deduction','balance-=gross','balance-=0')]:
 assert old in source
 mod=types.ModuleType('mutant');exec(compile(source.replace(old,new),'<mutant>','exec'),mod.__dict__)
 saved=(t.simulate,t.allowance);t.simulate,t.allowance=mod.simulate,mod.allowance
 result=unittest.TextTestRunner(stream=io.StringIO()).run(unittest.defaultTestLoader.loadTestsFromTestCase(t.Tests));t.simulate,t.allowance=saved
 # Removing 50% alone is redundant under 52.1k buffer + 2k cap: test direct function with altered constants separately instead.
 if name=='half_profit_removed':
  assert result.wasSuccessful();mutations.append(dict(name=name,result='EQUIVALENT under owner buffer and cap; not claimed caught'));continue
 assert not result.wasSuccessful(),name
 mutations.append(dict(name=name,failures=len(result.failures),errors=len(result.errors)))
verification=dict(status='PASS',tests_per_timezone=9,random_accounts_per_timezone=3000,timezones=['America/Chicago','UTC'],summary_rows=len(summaries),ledger_rows=len(ledger),payout_rows=len(payouts),all_rows_reconciled=True,mutations=mutations,engine_sha256=hashlib.sha256(source.encode()).hexdigest())
with (P/'verification.json').open('x') as f:json.dump(verification,f,indent=2)
print(json.dumps(verification,indent=2))
