"""Independent Decimal-dollar audit; does not import replay/account engines."""
import csv,json,hashlib
from decimal import Decimal as D
from pathlib import Path
from collections import Counter
P=Path('/home/hoang/fleet-1017/out/lucid50k-EDA-3v6-20261022')
def rows(name):return list(csv.DictReader((P/name).open()))
paths=json.loads((P/'paths.json').read_text())
audit=json.loads((P/'audit.json').read_text())
for name,h in audit['artifacts'].items():assert hashlib.sha256((P/name).read_bytes()).hexdigest()==h

def oracle(ps,q):
 balance=D(50000);peak=balance;floor=D(48000);dd=D(0);ledger=[];lock='';breach=''
 for p in ps:
  if not p['traded']:continue
  before=balance;cash=balance-D(q);event=None
  for row,ts,g in p['record_lows']:
   if cash+D(g)*D('.50')*q<=floor:event=(row,ts,g);break
  if cash<=floor:event=(p['entry_row'],p['entry_event_ns'],0)
  g=event[2] if event else p['gain_ticks']
  balance=cash+D(g)*D('.50')*q
  low=balance if event else cash+D(p['min_gain_ticks'])*D('.50')*q
  dd=max(dd,peak-low);prior=floor
  if event:breach=p['day']
  else:
   peak=max(peak,balance);floor=max(floor,min(D(50100),peak-D(2000)))
   if floor==D(50100) and not lock:lock=p['day']
  ledger.append(dict(day=p['day'],pnl_cents=int((balance-before)*100),end_cents=int(balance*100),start_cents=int(before*100),fee_cents=q*100,low_equity_cents=int(low*100),floor_before_cents=int(prior*100),floor_after_cents=int(floor*100),outcome='MLL_BREACH' if event else p['outcome'],exit_row=event[0] if event else p['exit_row'],exit_event_ns=event[1] if event else p['exit_event_ns']))
  if event:break
 return dict(trades=len(ledger),ending_balance_cents=int(balance*100),net_cents=int((balance-50000)*100),qualified_150_days=sum(l['pnl_cents']>=15000 for l in ledger),max_EOD_peak_to_intraday_trough_cents=int(dd*100),final_floor_cents=int(floor*100),lock_day=lock,breach_day=breach,status='BREACHED_ON_VALID_SAMPLE' if breach else 'NO_BREACH_ON_VALID_SAMPLE'),ledger

def check(got,expected):
 for k,v in expected.items():assert str(got[k])==str(v),(k,got[k],v)
ledgers=rows('ledger.csv');accts=rows('accounts.csv');windows=rows('rolling20.csv');stats=[]
for a in accts:
 ps=sorted([p for p in paths if p['method']==a['method'] and p['execution']==a['execution']],key=lambda p:p['day'])
 q=int(a['qty']);expected,ls=oracle(ps,q);check(a,expected)
 actual=[l for l in ledgers if all(l[k]==a[k] for k in ['method','execution','qty'])]
 assert len(actual)==len(ls)
 for got,want in zip(actual,ls):check(got,want)
 ws=[w for w in windows if all(w[k]==a[k] for k in ['method','execution','qty'])]
 trades=[p for p in ps if p['traded']]
 for w in ws:
  subset=[p for p in trades if w['start']<=p['day']<=w['end']];assert len(subset)==20
  expected,_=oracle(subset,q);check(w,expected)
 stats.append(dict(method=a['method'],execution=a['execution'],qty=q,rolling20_breaches=sum(bool(w['breach_day']) for w in ws),rolling20_windows=len(ws),min_day_cents=min(l['pnl_cents'] for l in ls),max_day_cents=max(l['pnl_cents'] for l in ls),outcomes=dict(Counter(l['outcome'] for l in ls))))
result=dict(status='PASS',account_rows=len(accts),ledger_rows=len(ledgers),rolling_windows=len(windows),independent_arithmetic='Decimal dollars; no replay imports',artifacts_sha256_verified=True,stats=stats)
with (P/'independent-account-verification.json').open('x') as f:json.dump(result,f,indent=2)
print(json.dumps(result,indent=2))
