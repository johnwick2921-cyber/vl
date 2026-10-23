import unittest,random
import lucid_eda_funded as x
from test_ema_halfbox import base,ticks

def b(side='LONG',width=81):
 r=base(side,width);r['width_points']=str(width/4);return r
class Tests(unittest.TestCase):
 def test_E(self):
  r=x.trade_path(ticks([400,360,359]),b(),100,'E',False)
  self.assertEqual((r['outcome'],r['gain_ticks']),('SL',-41))
 def test_A_full_box_stop(self):
  r=x.trade_path(ticks([400,359,319]),b(),100,'A',False)
  self.assertEqual((r['outcome'],r['exit_row'],r['gain_ticks']),('SL',3,-81))
 def test_D_entry_gap_stop_same_print(self):
  r=x.trade_path(ticks([350,500]),b(),100,'D',False)
  self.assertEqual((r['outcome'],r['exit_row'],r['gain_ticks']),('SL',1,-50))
 def test_D_through_not_touch(self):
  r=x.trade_path(ticks([400,401,410]),b(),100,'D',True)
  self.assertEqual(r['outcome'],'NO_THROUGH_FILL')
 def test_D_later_through(self):
  r=x.trade_path(ticks([400,399,481,482]),b(),100,'D',True)
  self.assertEqual((r['entry_row'],r['exit_row'],r['gain_ticks']),(2,4,81))
 def test_short(self):
  r=x.trade_path(ticks([400,319]),b('SHORT'),100,'E',False)
  self.assertEqual((r['outcome'],r['gain_ticks']),('TP',81))
 def test_cutoff(self):
  r=x.trade_path(ticks([400,420,481]),b(),12,'E',False)
  self.assertEqual((r['outcome'],r['gain_ticks']),('CUTOFF_PROXY',20))
 def test_entry_slippage_changes_bracket(self):
  r=x.trade_path(ticks([400,481,482,483]),b(),100,'E',True)
  self.assertEqual((r['entry_ticks'],r['exit_row']),(401,4))
 def test_no_entry(self):
  q=b();q.update(entry_row='',outcome='EXCLUDED')
  self.assertFalse(x.trade_path([],q,100,'E',False)['traded'])
 def test_random(self):
  rng=random.Random(3506)
  for i in range(5000):
   ps=[400]
   for j in range(100):ps.append(ps[-1]+rng.randint(-30,30))
   x.trade_path(ticks(ps),b(rng.choice(['LONG','SHORT']),rng.randint(1,500)),1000,rng.choice(['E','D','A']),bool(i%2))
if __name__=='__main__':unittest.main()
