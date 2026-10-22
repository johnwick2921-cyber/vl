import unittest,random
import ema_box_be as x

def base(side='LONG',width=80):
 return dict(day='2026-01-02',contract='MNQH6',orb_minutes='1',outcome='SL',entry_row='1',entry_event_ns='10',entry_recv_ns='10',entry_points='100',side=side,orb_high=str(100+width/4),orb_low='100',eligible_ns='9',signal_end_ns='9')
def ticks(prices):return [(i+1,10+i,10+i,i+1,p,1) for i,p in enumerate(prices)]
class Tests(unittest.TestCase):
 def test_one_R_then_BE(self):
  for rr in (2,3):
   r=x.simulate(ticks([400,440,400,600]),base(),100,rr)
   self.assertEqual((r['outcome'],r['be_row'],r['exit_row'],r['pnl_points']),('BE',2,3,0))
 def test_before_one_R_stays_original_stop(self):
  r=x.simulate(ticks([400,439,400,359]),base(),100,2)
  self.assertEqual((r['outcome'],r['be_row'],r['pnl_points']),('SL','',-10))
 def test_target_jump_takes_precedence(self):
  r=x.simulate(ticks([400,520,400]),base(),100,3)
  self.assertEqual((r['outcome'],r['be_row'],r['pnl_points']),('TP','',30))
 def test_after_BE_go_target(self):
  for rr in (2,3):
   r=x.simulate(ticks([400,440,401,400+rr*40]),base(),100,rr)
   self.assertEqual((r['outcome'],r['pnl_points']),('TP',rr*10))
 def test_short_BE_gap(self):
  r=x.simulate(ticks([400,360,405]),base('SHORT'),100,3)
  self.assertEqual((r['outcome'],r['pnl_points'],r['observed_pnl_points']),('BE',0,-1.25))
 def test_half_tick_BE_threshold(self):
  r=x.simulate(ticks([400,440,400,359]),base(width=81),100,3)
  self.assertEqual(r['outcome'],'SL')
  r=x.simulate(ticks([400,441,400]),base(width=81),100,3)
  self.assertEqual(r['outcome'],'BE')
 def test_same_timestamp_next_record(self):
  t=ticks([400,440,400]);t=[(v[0],10,10,v[3],v[4],1) for v in t]
  r=x.simulate(t,base(),100,2);self.assertEqual((r['outcome'],r['be_row'],r['exit_row']),('BE',2,3))
 def test_cutoff_open(self):
  r=x.simulate(ticks([400,440,400]),base(),12,2)
  self.assertEqual((r['outcome'],r['be_row']),('OPEN_AT_CUTOFF',2))
 def test_no_trailing_beyond_entry(self):
  r=x.simulate(ticks([400,450,410,480]),base(),100,2)
  self.assertEqual(r['outcome'],'TP')
 def test_not_reenter_after_BE(self):
  r=x.simulate(ticks([400,440,400,520]),base(),100,3)
  self.assertEqual(r['outcome'],'BE')
 def test_wrong_entry_rejected(self):
  with self.assertRaises(AssertionError):x.simulate(ticks([401,440]),base(),100,2)
 def test_exclusion_preserved(self):
  b=base();b.update(entry_row='',outcome='EXCLUDED')
  self.assertEqual(x.simulate([],b,100,3)['outcome'],'EXCLUDED')
 def test_random_oracle(self):
  rng=random.Random(220026)
  for i in range(4000):
   price=400;t=[price]
   for _ in range(100):price+=rng.randint(-30,30);t.append(price)
   x.simulate(ticks(t),base(rng.choice(['LONG','SHORT']),rng.randint(1,500)),1000,rng.choice([2,3]))
if __name__=='__main__':unittest.main()
