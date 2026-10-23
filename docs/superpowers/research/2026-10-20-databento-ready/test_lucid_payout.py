import unittest,random
from decimal import Decimal as D,ROUND_DOWN
from lucid_payout import simulate,allowance

def oracle(paths,q):
 money=D(50000);high=money;limit=D(48000);profits=[];allprofit=D(0);withdrawals=[];received=D(0);last='';count=0;state='END_OF_VALID_SAMPLE'
 for p in paths:
  if not p['traded']:continue
  last=p['day'];count+=1;cash=money-D(q)
  values=[cash+D(g)*D('.5')*q for _,_,g in p['record_lows']]
  breach=next((v for v in values if v<=limit),None)
  if cash<=limit:breach=cash
  new=breach if breach is not None else cash+D(p['gain_ticks'])*D('.5')*q
  pnl=new-money;profits.append(pnl);allprofit+=pnl;money=new
  if breach is not None:state='MLL_BREACH';break
  high=max(high,money);limit=max(limit,min(D(50100),high-D(2000)))
  cap=min((money-D(50000))/2,D(2000),money-D(52100)).quantize(D('.01'),rounding=ROUND_DOWN)
  if sum(x>=150 for x in profits)>=5 and sum(profits)>0 and cap>=500:
   money-=cap;withdrawals.append(cap);received+=(cap*D('.9')).quantize(D('.01'),rounding=ROUND_DOWN);profits=[];limit=D(50100)
   if len(withdrawals)==5:state='LIVE_TRANSITION_AFTER_5_PAYOUTS';break
 return dict(qty=q,status=state,last_day=last,trades=count,payout_count=len(withdrawals),gross_withdrawn_cents=int(sum(withdrawals)*100),received_cents=int(received*100),ending_balance_cents=int(money*100),final_floor_cents=int(limit*100),trading_net_cents=int(allprofit*100))

def path(i,g,low=0):return dict(day=str(i),traded=True,gain_ticks=g,record_lows=[(i,i,low)],entry_row=i,entry_event_ns=i,exit_row=i,outcome='TP' if g>0 else 'SL')
class Tests(unittest.TestCase):
 def test_owner_examples(self):
  for balance,want in [(5250000,40000),(5260000,50000),(5400000,190000),(5500000,200000),(5100000,0)]:self.assertEqual(allowance(balance),want)
 def test_five_days_and_buffer(self):
  r=simulate([path(i,402) for i in range(5)],3);self.assertEqual(r['payout_count'],1);self.assertEqual(r['payouts'][0]['after_cents'],5210000);self.assertEqual(r['received_cents'],81000)
 def test_four_days_insufficient(self):self.assertEqual(simulate([path(i,802) for i in range(4)],3)['payout_count'],0)
 def test_below_minimum(self):self.assertEqual(simulate([path(i,335) for i in range(5)],3)['payout_count'],0)
 def test_cycle_resets(self):self.assertEqual(simulate([path(i,802) for i in range(9)],3)['payout_count'],1)
 def test_fifth_stops(self):
  r=simulate([path(i,802) for i in range(40)],3);self.assertEqual(r['payout_count'],5);self.assertEqual(r['trades'],25)
 def test_loss_after_payout(self):
  r=simulate([path(i,402) for i in range(5)]+[path(5,-1400,-1400),path(6,800)],3);self.assertEqual(r['status'],'MLL_BREACH');self.assertEqual(r['trades'],6);self.assertEqual(r['payout_count'],1)
 def test_nonpositive_cycle(self):
  ps=[path(i,2002) for i in range(5)]+[path(5,-1200,-1200)]+[path(i,102) for i in range(6,11)]
  self.assertEqual(simulate(ps,3)['payout_count'],1)
 def test_random_independent(self):
  rng=random.Random(20261023)
  for _ in range(3000):
   ps=[path(i,g:=rng.randint(-400,1500),min(g,rng.randint(-600,0))) for i in range(60)];q=rng.choice([3,6]);r=simulate(ps,q);r.pop('ledger');r.pop('payouts');self.assertEqual(r,oracle(ps,q))
if __name__=='__main__':unittest.main()
