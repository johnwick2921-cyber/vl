import unittest,random
from decimal import Decimal as D
from lucid_live import Policy,simulate,live_floor
from test_lucid_payout import path

def oracle(paths,q,after,policy):
    b=D(0);high=D(0);floor=D(-2000);paid=D(0);cashout=D(0);net=D(0);status='END_OF_VALID_SAMPLE';count=0;first=last=''
    for p in sorted(paths,key=lambda p:p['day']):
        if p['day']<=after or not p['traded']:continue
        if not first:first=p['day']
        last=p['day'];count+=1;start=b;cash=b-D(policy.fee_rt_cents)*q/100
        hits=[cash+D(g)*q/2 for _,_,g in p['record_lows'] if cash+D(g)*q/2<=floor]
        breached=bool(hits) or cash<=floor
        b=cash if cash<=floor else hits[0] if hits else cash+D(p['gain_ticks'])*q/2
        net+=b-start
        if breached:status='LIVE_MLL_BREACH';break
        high=max(high,b);floor=max(floor,D(100) if high>=D(policy.lock_trigger_cents)/100 else high-D(2000))
        if policy.payout:
            possible=b-D(100)-D(policy.reserve_cents)/100
            if possible>=D(policy.minimum_cents)/100:
                amount=min(possible,D(policy.maximum_cents)/100);b-=amount;paid+=amount;cashout+=D(int(amount*policy.split_bps/100))/100;floor=D(100)
    return dict(status=status,first_day=first,last_day=last,trades=count,balance_cents=int(b*100),floor_cents=int(floor*100),trading_net_cents=int(net*100),gross_withdrawn_cents=int(paid*100),received_cents=int(cashout*100))

class Tests(unittest.TestCase):
 def test_no_approval(self):self.assertEqual(simulate([],3,None,Policy())['status'],'AWAITING_EXPLICIT_TRANSITION')
 def test_unknown_payouts(self):
  with self.assertRaises(ValueError):simulate([],3,'0',Policy(payout=True))
 def test_zero_balance(self):self.assertEqual(simulate([],3,'0',Policy())['balance_cents'],0)
 def test_after_not_same_day(self):self.assertEqual(simulate([path('01',100),path('02',100)],3,'01',Policy())['trades'],1)
 def test_initial_equality_breach(self):self.assertEqual(simulate([path('01',0,-998)],4 if False else 6,'00',Policy(fee_rt_cents=100))['status'],'LIVE_MLL_BREACH')
 def test_floor_trigger(self):
  self.assertEqual(live_floor(-200000,199999,200000),-1);self.assertEqual(live_floor(-200000,200000,200000),10000)
  self.assertEqual(live_floor(10000,0,200000),10000)
 def test_buffer(self):
  r=simulate([path('01',2002)],3,'00',Policy(payout=True,minimum_cents=50000,maximum_cents=200000,split_bps=9000));self.assertEqual(r['balance_cents'],210000);self.assertEqual(r['gross_withdrawn_cents'],90000)
 def test_bonus_not_cash(self):
  r=simulate([path(f'{i:02}',402) for i in range(30)],3,'00',Policy());self.assertIsNotNone(r['first_bonus_objectives_day']);self.assertEqual(r['bonus_credited_cents'],0)
 def test_random(self):
  rng=random.Random(971)
  for _ in range(3000):
   pp=[path(f'{i:03}',g:=rng.randint(-600,1500),min(g,rng.randint(-700,0))) for i in range(60)]
   q=rng.choice([3,6]);policy=Policy(payout=rng.choice([True,False]),minimum_cents=50000,maximum_cents=200000,split_bps=9000,lock_trigger_cents=rng.choice([200000,210000]))
   got=simulate(pp,q,'005',policy)
   for k,v in oracle(pp,q,'005',policy).items():self.assertEqual(got[k],v,k)
if __name__=='__main__':unittest.main()
