"""Fixed $500 SIM payout; 3/6 MNQ, optional owner reserve. Offline only."""
import sys,json,hashlib,csv,random
from pathlib import Path
from decimal import Decimal as D
from datetime import date,timedelta
sys.path.append('/home/hoang/vl-1017-databento-ready/docs/superpowers/research/2026-10-20-databento-ready')
from lucid_live import simulate as live,Policy
from test_lucid_live import oracle as live_oracle
from test_lucid_payout import path
R=Path('/home/hoang/fleet-1017/out/cashout-E-before09-recent-20261031');O=Path('/home/hoang/fleet-1017/out/cashout-E-54k-one-day-20261101')
def sim(paths,q,reserve,initial=False):
 balance=5400000 if initial else 5000000;start_balance=balance;floor=5010000 if initial else 4800000;peak=balance;cycle=400000 if initial else 0;days=1 if initial else 0;received=0;le=[];pa=[];state='END_OF_VALID_SAMPLE'
 for p in paths:
  if not p['traded']:continue
  start=balance;cash=start-q*100;hit=next(((row,ts,g) for row,ts,g in p['record_lows'] if cash+g*q*50<=floor),None)
  if cash<=floor:hit=(p['entry_row'],p['entry_event_ns'],0)
  balance=cash+(hit[2] if hit else p['gain_ticks'])*q*50;pnl=balance-start;cycle+=pnl;days+=pnl>=15000;before=balance;gross=net=0;oldfloor=floor
  if hit:state='MLL_BREACH'
  else:
   peak=max(peak,balance);floor=min(5010000,max(floor,peak-200000))
   if days>=5 and cycle>0 and (balance-5000000)//2>=50000 and balance-50000>=5010000+reserve and balance-50000>5010000:
    gross=50000;net=45000;balance-=gross;received+=net;floor=5010000
    pa.append(dict(day=p['day'],gross_cents=gross,received_cents=net,before_cents=before,after_cents=balance,qualifying_days=days,cycle_net_cents=cycle));cycle=days=0
    if len(pa)==5:state='LIVE_TRANSITION_AFTER_5_PAYOUTS'
  le.append(dict(day=p['day'],pnl_cents=pnl,start_cents=start,end_cents=balance,payout_cents=gross,received_cents=net,floor_before_cents=oldfloor,floor_after_cents=floor,outcome='MLL_BREACH' if hit else p['outcome'],exit_row=hit[0] if hit else p['exit_row']))
  if state!='END_OF_VALID_SAMPLE':break
 assert balance==start_balance+sum(r['pnl_cents'] for r in le)-sum(r['gross_cents'] for r in pa)
 return dict(status=state,trades=len(le),last_day=le[-1]['day'] if le else '',balance_cents=balance,floor_cents=floor,received_cents=received,payout_count=len(pa),ledger=le,payouts=pa)
def oracle(paths,q,reserve,initial=False):
 money=D(54000 if initial else 50000);high=money;limit=D(50100 if initial else 48000);prior_profit=D(4000 if initial else 0);prior_days=1 if initial else 0;profits=[];paid=0;count=0;state='END_OF_VALID_SAMPLE';last=''
 for p in paths:
  if not p['traded']:continue
  count+=1;last=p['day'];cash=money-q
  bad=next((cash+D(g)*D('.5')*q for _,_,g in p['record_lows'] if cash+D(g)*D('.5')*q<=limit),None)
  if cash<=limit:bad=cash
  new=bad if bad is not None else cash+D(p['gain_ticks'])*D('.5')*q
  profits.append(new-money);money=new
  if bad is not None:state='MLL_BREACH';break
  high=max(high,money);limit=max(limit,min(D(50100),high-2000))
  available=min((money-50000)/2,D(2000));after=money-500
  if prior_days+sum(p>=150 for p in profits)>=5 and prior_profit+sum(profits)>0 and available>=500 and after>=D(50100)+D(reserve)/100 and after>50100:
   money=after;paid+=1;profits=[];prior_days=0;prior_profit=D(0);limit=D(50100)
   if paid==5:state='LIVE_TRANSITION_AFTER_5_PAYOUTS';break
 return dict(status=state,trades=count,last_day=last,balance_cents=int(money*100),floor_cents=int(limit*100),received_cents=paid*45000,payout_count=paid)
def check(ps,q,res,initial=False):
 r=sim(ps,q,res,initial)
 for k,v in oracle(ps,q,res,initial).items():assert r[k]==v,(k,r[k],v)
 return r
def run(ps,q,res):
 remain=list(ps);phase='SIM';st=[];le=[];pa=[];skip=[]
 while remain:
  if phase=='SIM':r=check(remain,q,res,initial=not st)
  else:
   policy=Policy(payout=True,minimum_cents=1,maximum_cents=10**12,split_bps=9000,reserve_cents=200000)
   r=live(remain,q,'0000-00-00',policy)
   for k,v in live_oracle(remain,q,'0000-00-00',policy).items():assert r[k]==v
  n=r['trades'];assert n>0;tag=dict(stage=len(st)+1,phase=phase)
  st.append(dict(**tag,first_day=remain[0]['day'],last_day=r['last_day'],status=r['status'],trades=n,received_cents=r['received_cents'],balance_cents=r['balance_cents'],payout_count=r.get('payout_count',len(r['payouts']))))
  le.extend(dict(**tag,**x) for x in r['ledger']);pa.extend(dict(**tag,**x) for x in r['payouts']);remain=remain[n:]
  if r['status']=='END_OF_VALID_SAMPLE':assert not remain
  elif phase=='SIM':phase='LIVE' if r['status']=='LIVE_TRANSITION_AFTER_5_PAYOUTS' else 'SIM'
  else:
   threshold=(date.fromisoformat(r['last_day'])+timedelta(days=14)).isoformat()
   skip.extend(dict(day=p['day'],eligible_on=threshold) for p in remain if p['day']<threshold);remain=[p for p in remain if p['day']>=threshold];phase='SIM'
 assert sorted(x['day'] for x in le+skip)==sorted(x['day'] for x in ps)
 assert len({x['day'] for x in le})==len(le)
 s=dict(qty=q,funded_reserve_usd=res/100,sim_accounts=sum(x['phase']=='SIM' for x in st),live_accounts=sum(x['phase']=='LIVE' for x in st),sim_breaches=sum(x['status']=='MLL_BREACH' for x in st),live_breaches=sum(x['status']=='LIVE_MLL_BREACH' for x in st),trades=len(le),skipped=len(skip),funded_payouts=sum(x['phase']=='SIM' for x in pa),live_payouts=sum(x['phase']=='LIVE' for x in pa),funded_received_cents=sum(x['received_cents'] for x in pa if x['phase']=='SIM'),live_received_cents=sum(x['received_cents'] for x in pa if x['phase']=='LIVE'),total_received_cents=sum(x['received_cents'] for x in pa),last_phase=st[-1]['phase'],last_balance_cents=st[-1]['balance_cents'])
 return s,st,le,pa,skip

def main():
 O.mkdir(exist_ok=False);rng=random.Random(10312026)
 for i in range(2000):
  ps=[path(j,g:=rng.randint(-600,1400),min(g,rng.randint(-650,0))) for j in range(45)]
  check(ps,rng.choice([3,6]),rng.choice([0,200000]),initial=bool(i%2))
 # Payout must wait for five qualifying days; exact $500 and reset.
 for reserve in [0,200000]:
  assert check([path(i,802) for i in range(4)],3,reserve)['payout_count']==0
  x=check([path(i,802) for i in range(5)],3,reserve);assert x['payout_count']==1 and x['received_cents']==45000
  assert check([path(i,802) for i in range(9)],3,reserve)['payout_count']==1
  assert check([path(i,802) for i in range(25)],3,reserve)['payout_count']==5
 # Existing qualifying day counts once, then resets; replacements use fresh state.
 for reserve in [0,200000]:
  assert check([path(i,802) for i in range(3)],3,reserve,True)['payout_count']==0
  assert check([path(i,802) for i in range(4)],3,reserve,True)['payout_count']==1
  assert check([path(i,802) for i in range(8)],3,reserve,True)['payout_count']==1
  assert check([path(i,802) for i in range(9)],3,reserve,True)['payout_count']==2
 assert check([],3,0,True)['balance_cents']==5400000
 assert check([],3,0)['balance_cents']==5000000
 raw=(R/'paths.json').read_bytes();assert hashlib.sha256(raw).hexdigest()==json.loads((R/'audit.json').read_text())['artifacts']['paths.json'];paths=json.loads(raw)
 allrows=[[],[],[],[],[]]
 for ex in ['trade_print_proxy','stress1tick']:
  ps=sorted([p for p in paths if p['execution']==ex],key=lambda p:p['day']);assert len(ps)==101
  for q in [3,6]:
   for reserve in [0,200000]:
    result=run(ps,q,reserve);key=dict(execution=ex,qty=q,funded_reserve_usd=reserve/100)
    for dst,rs in zip(allrows,[[result[0]],*result[1:]]):dst.extend(dict(**key,**{k:v for k,v in x.items() if k not in key}) for x in rs)
 for name,rs in zip(['summary','stages','ledger','payouts','skipped'],allrows):
  if rs:
   with (O/(name+'.csv')).open('x',newline='') as f:w=csv.DictWriter(f,fieldnames=list(dict.fromkeys(k for r in rs for k in r)));w.writeheader();w.writerows(rs)
 with (O/'audit.json').open('x') as f:json.dump(dict(status='PASS',random_oracle_cases=2000,boundary_assertions=18,source_paths_sha256=hashlib.sha256(raw).hexdigest(),script_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),artifacts={p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in O.glob('*.csv')},initial_balance_usd=54000,initial_qualified_days=1,initial_cycle_profit_usd=4000,note='Fixed500 applies SIM only. Live retains old daily excess over2100 policy. Immediate funded replacements and live approvals remain hypothetical; no evaluation costs/time.'),f,indent=2)
 print(json.dumps(allrows[0],indent=2))
if __name__=='__main__':main()
