import unittest,random
import lucid_ema_funded as x
from test_ema_halfbox import base,ticks

def trade(day,gain,low=None):
 low=min(0,gain) if low is None else low
 return dict(day=day,traded=True,outcome='TP' if gain>0 else 'SL',gain_ticks=gain,min_gain_ticks=low,
             record_lows=[(1,10,0),(2,11,low)],entry_row=1,entry_event_ns=10,exit_row=3,exit_event_ns=12)
class Tests(unittest.TestCase):
 def test_target(self):
  p=x.path(ticks([400,481]),base(),100)
  self.assertEqual((p['outcome'],p['gain_ticks']),('TP',81))
 def test_tick_round_gap(self):
  p=x.path(ticks([400,360,350]),base(),100)
  self.assertEqual((p['outcome'],p['gain_ticks'],p['risk_ticks']),('SL',-50,41))
 def test_short(self):
  p=x.path(ticks([400,319]),base('SHORT'),100)
  self.assertEqual((p['outcome'],p['gain_ticks']),('TP',81))
 def test_stress_bracket_entry_shift(self):
  p=x.path(ticks([400,481,482,483]),base(),100,stress=True)
  self.assertEqual((p['entry_ticks'],p['exit_row'],p['gain_ticks']),(401,4,81))
 def test_lock_next_record(self):
  p=x.path(ticks([400,441,420]),base(),100,lock=True)
  self.assertEqual((p['outcome'],p['gain_ticks']),('LOCK',20))
 def test_cutoff_forced_mark(self):
  p=x.path(ticks([400,420,481]),base(),12)
  self.assertEqual((p['outcome'],p['gain_ticks']),('CUTOFF_PROXY',20))
 def test_excluded(self):
  b=base();b.update(entry_row='',outcome='EXCLUDED')
  self.assertFalse(x.path([],b,100)['traded'])
 def test_equality_breach(self):
  self.assertTrue(x.mll_hit(4800000,4800000))
  self.assertFalse(x.mll_hit(4800001,4800000))
  r=x.account([trade('a',0,-2000)],3,0)
  self.assertEqual(r['status'],'BREACHED_ON_VALID_SAMPLE')
 def test_fee_can_breach(self):
  # Six contracts: -666 ticks => -1998 dollars; six-dollar fee breaches.
  r=x.account([trade('a',-666)],6)
  self.assertEqual(r['status'],'BREACHED_ON_VALID_SAMPLE')
  self.assertEqual(x.account([trade('a',-666)],6,0)['status'],'NO_BREACH_ON_VALID_SAMPLE')
 def test_no_intraday_trail(self):
  r=x.account([trade('a',0,0)],3)
  self.assertEqual(r['final_floor_cents'],4800000)
 def test_eod_trail_and_never_down(self):
  r=x.account([trade('a',500),trade('b',-100)],3)
  self.assertEqual(r['final_floor_cents'],4874700)
 def test_lock_cap(self):
  r=x.account([trade('a',2000),trade('b',2000)],3)
  self.assertEqual(r['final_floor_cents'],5010000)
 def test_breach_before_recovery_stops_account(self):
  r=x.account([trade('a',100,-1500),trade('b',1000)],3)
  self.assertEqual((r['trades'],r['breach_day']),(1,'a'))
 def test_payout_day_threshold_after_costs(self):
  self.assertEqual(x.account([trade('a',100)],3)['qualified_150_days'],0)
  self.assertEqual(x.account([trade('a',102)],3)['qualified_150_days'],1)
 def test_arithmetic_and_reference_random(self):
  rng=random.Random(502236)
  for i in range(2000):
   qty=rng.choice([3,6]);ps=[]
   for j in range(20):
    g=rng.randint(-800,1200);ps.append(trade(str(j),g,min(g,0)-rng.randint(0,200)))
   actual=x.account(ps,qty)
   # Separate dollar arithmetic and minimum-only daily oracle.
   bal=50000.;peak=bal;floor=48000.;n=0;breach=None
   for p in ps:
    n+=1;cash=bal-qty
    if cash+min(0,p['min_gain_ticks'])*.5*qty<=floor:
     breach=p['day'];bal=cash+p['min_gain_ticks']*.5*qty;break
    bal=cash+p['gain_ticks']*.5*qty;peak=max(peak,bal);floor=min(50100.,peak-2000.)
   self.assertEqual((actual['ending_balance_cents'],actual['final_floor_cents'],actual['trades'],actual['breach_day']),(round(bal*100),round(floor*100),n,breach))
 def test_random_exit_oracle(self):
  rng=random.Random(202650)
  for i in range(4000):
   ps=[400]
   for j in range(50):ps.append(ps[-1]+rng.randint(-30,30))
   x.path(ticks(ps),base(rng.choice(['LONG','SHORT']),rng.randint(1,500)),1000,lock=bool(i%2),stress=bool(i%3))
if __name__=='__main__':unittest.main()
