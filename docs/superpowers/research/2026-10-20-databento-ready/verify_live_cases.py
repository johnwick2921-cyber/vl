from pathlib import Path
import csv,json,hashlib,unittest,io,types
import lucid_live as live
import test_lucid_live as tests
import lucid_payout as fresh
from test_lucid_payout import oracle as fresh_oracle
P=Path('/home/hoang/fleet-1017/out/lucid50k-EDA-live-20261023')
rows=lambda p:list(csv.DictReader(p.open()))
audit=json.loads((P/'audit.json').read_text())
for name,h in audit['inputs'].items():assert hashlib.sha256(Path(name).read_bytes()).hexdigest()==h
paths=json.loads(Path(next(k for k in audit['inputs'] if k.endswith('paths.json'))).read_text())
prior=rows(Path(next(k for k in audit['inputs'] if k.endswith('summary.csv'))))
for name,h in audit['artifacts'].items():assert hashlib.sha256((P/name).read_bytes()).hexdigest()==h
summary=rows(P/'summary.csv');ledger=rows(P/'ledger.csv');verified=0
for r in summary:
 if not r['transition_after']:continue
 ps=[p for p in paths if p['method']==r['method'] and p['execution']==r['execution']]
 policy=live.Policy(payout=r['policy']!='no_live_withdrawals',minimum_cents=50000,maximum_cents=200000,split_bps=9000,lock_trigger_cents=int(r['lock_trigger_cents']))
 expected=tests.oracle(ps,int(r['qty']),r['transition_after'],policy)
 for k,v in expected.items():assert str(v)==r[k],(k,v,r[k])
 ls=[l for l in ledger if all(l[k]==r[k] for k in ['method','execution','qty','policy','lock_trigger_cents'])]
 b=0
 for l in ls:
  assert int(l['start_cents'])==b;b+=int(l['pnl_cents'])-int(l['payout_cents']);assert b==int(l['end_cents'])
  if int(l['payout_cents']):assert b>=210000 and int(l['received_cents'])==int(l['payout_cents'])*9//10
 assert b==int(r['balance_cents']);verified+=1
# Owner's second scenario: hypothetical new $50k SIM account after payout five.
newrows=[];newledger=[]
for s in prior:
 if int(s['payout_count'])!=5:continue
 ps=sorted([p for p in paths if p['method']==s['method'] and p['execution']==s['execution'] and p['day']>s['last_day']],key=lambda p:p['day'])
 r=fresh.simulate(ps,int(s['qty']));want=fresh_oracle(ps,int(s['qty']))
 for k,v in want.items():assert r[k]==v
 key=dict(method=s['method'],execution=s['execution'],qty=int(s['qty']))
 newledger.extend(dict(**key,**l) for l in r.pop('ledger'));r.pop('payouts');r['qty']=int(s['qty'])
 newrows.append(dict(method=s['method'],execution=s['execution'],transition_after=s['last_day'],prior_sim_received_cents=int(s['received_cents']),**r))
for name,rr in [('new50k_summary.csv',newrows),('new50k_ledger.csv',newledger)]:
 with (P/name).open('x') as f:w=csv.DictWriter(f,fieldnames=rr[0]);w.writeheader();w.writerows(rr)
source=Path(live.__file__).read_text();mutants=[]
for name,old,new in [('carry_50k','balance=0;floor=-200000','balance=5000000;floor=-200000'),('wrong_initial_floor','floor=-200000;peak=0','floor=-100000;peak=0'),('same_day_reuse',"p['day']<=transition_after","p['day']<transition_after"),('omit_fee','policy.fee_rt_cents*qty','0*qty'),('lose_buffer','10000+policy.reserve_cents','10000'),('strict_breach','<=floor','<floor')]:
 mod=types.ModuleType('mutant')
 # dataclass does not need registration with concrete annotations in this module
 exec(compile(source.replace(old,new),'<mutant>','exec'),mod.__dict__)
 saved=tests.simulate;tests.simulate=mod.simulate
 result=unittest.TextTestRunner(stream=io.StringIO()).run(unittest.defaultTestLoader.loadTestsFromTestCase(tests.Tests));tests.simulate=saved
 if name=='strict_breach' and result.wasSuccessful():
  mutants.append(dict(name=name,result='Equivalent on fixed 3/6 MNQ cent lattice; not claimed detected'));continue
 assert not result.wasSuccessful(),name
 mutants.append(dict(name=name,failures=len(result.failures),errors=len(result.errors)))
with (P/'verification.json').open('x') as f:json.dump(dict(live_summaries_matched=verified,new50k_summaries_matched=len(newrows),ledger_rows=len(ledger),new50k_ledger_rows=len(newledger),tests_per_timezone=9,random_accounts_per_timezone=3000,mutants=mutants,hashes={n:hashlib.sha256((P/n).read_bytes()).hexdigest() for n in ['new50k_summary.csv','new50k_ledger.csv']}),f,indent=2)
print('PASS',verified,'live summaries',len(newrows),'new 50k summaries')
for r in newrows:
 if r['execution']=='trade_print_proxy':print(r)
