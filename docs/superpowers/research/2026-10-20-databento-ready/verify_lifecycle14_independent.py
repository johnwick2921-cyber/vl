"""Independent lifecycle scheduler, Decimal account oracles, saved-artifact audit."""
from pathlib import Path
from datetime import date,timedelta
import csv,json,hashlib,random
from test_lucid_payout import oracle as sim_oracle,path
from test_lucid_live import oracle as live_oracle
from lucid_live import Policy
from lucid_lifecycle14 import run

def independent(pp,q):
 pp=sorted((p for p in pp if p['traded']),key=lambda p:p['day'])
 mode='SIM';eligible=date.min;cursor=0;st=[];skipped=0
 pol=Policy(payout=True,minimum_cents=1,maximum_cents=sum(max(0,p['gain_ticks'])*q*50 for p in pp)+1,split_bps=9000)
 while cursor<len(pp):
  if date.fromisoformat(pp[cursor]['day'])<eligible:skipped+=1;cursor+=1;continue
  r=sim_oracle(pp[cursor:],q) if mode=='SIM' else live_oracle(pp[cursor:],q,'0000-00-00',pol)
  st.append((mode,r));last=date.fromisoformat(r['last_day'])
  # Advance by date rather than trusting engine trade-count slicing.
  while cursor<len(pp) and date.fromisoformat(pp[cursor]['day'])<=last:cursor+=1
  if r['status']=='LIVE_TRANSITION_AFTER_5_PAYOUTS':mode='LIVE';eligible=last+timedelta(days=1)
  elif r['status']=='LIVE_MLL_BREACH':mode='SIM';eligible=last+timedelta(days=14)
  else:mode='SIM';eligible=last+timedelta(days=1)
 return dict(sim_accounts=sum(m=='SIM' for m,r in st),live_accounts=sum(m=='LIVE' for m,r in st),sim_breaches=sum(r['status']=='MLL_BREACH' for m,r in st),live_breaches=sum(r['status']=='LIVE_MLL_BREACH' for m,r in st),trades=sum(r['trades'] for m,r in st),skipped_signals=skipped,sim_received_cents=sum(r['received_cents'] for m,r in st if m=='SIM'),live_received_cents=sum(r['received_cents'] for m,r in st if m=='LIVE'),total_received_cents=sum(r['received_cents'] for m,r in st))

def main():
 out=Path('/home/hoang/fleet-1017/out/lucid50k-AE-lifecycle14-20261023')
 src=Path('/home/hoang/fleet-1017/out/lucid50k-EDA-3v6-20261022')
 audit=json.loads((src/'audit.json').read_text());checks=0
 for name,h in audit['source_hashes'].items():assert hashlib.sha256(Path(name).read_bytes()).hexdigest()==h;checks+=1
 for name,h in audit['artifacts'].items():assert hashlib.sha256((src/name).read_bytes()).hexdigest()==h;checks+=1
 verify=json.loads((out/'verification.json').read_text())
 for name,h in verify['files'].items():assert hashlib.sha256((out/name).read_bytes()).hexdigest()==h;checks+=1
 pp=json.loads((src/'paths.json').read_text())
 for row in csv.DictReader((out/'summary.csv').open()):
  paths=[p for p in pp if p['method']==row['method'] and p['execution']==row['execution']];q=int(row['qty'])
  for k,v in independent(paths,q).items():assert int(row[k])==v,(row,k,v)
  actual=run(paths,q)[0]
  for k,v in actual.items():assert row[k]==('' if v is None else str(v)),(k,row[k],v)
 rng=random.Random(2026102302)
 for _ in range(1000):
  paths=[]
  for i in range(180):
   g=rng.randint(-1800,2000);low=min(g,rng.randint(-1000,0))
   paths.append(path((date(2025,10,20)+timedelta(days=i)).isoformat(),g,low))
  q=rng.choice([3,6]);got=run(paths,q)[0]
  for k,v in independent(paths,q).items():assert got[k]==v,(k,got[k],v)
 result=dict(status='PASS',saved_scenarios=8,source_and_artifact_hash_checks=checks,independent_scheduler_random_sequences=1000,days_per_sequence=180,uses_decimal_account_oracles=True,raw_tick_replay_rerun=False)
 with (out/'independent-reverification.json').open('x') as f:json.dump(result,f,indent=2)
 print(json.dumps(result))
if __name__=='__main__':main()
