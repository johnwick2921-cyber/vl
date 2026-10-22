import unittest,random
from decimal import Decimal
import ema_orb as x
M=x.e.MINUTE

def tick(i,t,p,delay=0):return (i,t,t+delay,i,p,1)
def fixture(m=1):
 s=m*M
 return [tick(0,0,90),tick(1,s//2,110),tick(2,s-1,100),tick(3,s,200),tick(4,2*s-1,200),tick(5,2*s,205),tick(6,2*s+1,285),tick(7,2*s+2,325)]
class Tests(unittest.TestCase):
 def test_formula(self):
  v,b=x.ema_seed([100]*299+[200]);self.assertEqual(v,Decimal(120));self.assertLess(b,Decimal('1e-20'))
 def test_warmup_required(self):
  with self.assertRaises(AssertionError):x.ema_seed([100]*299)
 def test_long_all_timeframes_targets(self):
  for m in (1,2,5):
   for tp,exitrow in ((20,6),(30,7)):
    r=x.checked(fixture(m),0,10*m*M,m,tp,[100]*300)
    self.assertEqual((r['outcome'],r['signal_row'],r['entry_row'],r['exit_row']),('TP',4,5,exitrow))
    self.assertEqual(r['entry_points'],205/4);self.assertEqual(r['pnl_points'],tp)
 def test_short(self):
  t=[(*a[:4],400-a[4],a[5]) for a in fixture()]
  r=x.checked(t,0,10*M,1,20,[300]*300)
  self.assertEqual((r['side'],r['outcome']),('SHORT','TP'))
 def test_first_stop_and_gap(self):
  t=fixture();t[6]=tick(6,2*M+1,100)
  r=x.checked(t,0,10*M,1,30,[100]*300)
  self.assertEqual((r['outcome'],r['pnl_points'],r['observed_pnl_points']),('SL',-20,-26.25))
 def test_no_intrabar_wick_signal(self):
  t=fixture();t[4]=tick(4,2*M-1,100)
  r=x.checked(t[:5],0,3*M,1,20,[100]*300)
  self.assertEqual(r['outcome'],'NO_SIGNAL')
 def test_orb_not_trade_signal(self):
  t=fixture()[:3];self.assertEqual(x.checked(t,0,3*M,1,20,[0]*300)['outcome'],'NO_SIGNAL')
 def test_late_confirmation_wait(self):
  t=fixture();t[4]=tick(4,2*M-1,200,2)
  r=x.checked(t,0,10*M,1,20,[100]*300);self.assertEqual(r['entry_row'],6)
 def test_late_orb_exclusion(self):
  t=fixture();t[2]=tick(2,M-1,100,2)
  self.assertEqual(x.checked(t,0,10*M,1,20,[100]*300)['outcome'],'EXCLUDED')
 def test_no_new_cross_when_already_above(self):
  self.assertEqual(x.checked(fixture(),0,3*M,1,20,[500]*300)['outcome'],'NO_SIGNAL')
 def test_equality_is_not_cross(self):
  t=fixture();t[3]=tick(3,M,150);t[4]=tick(4,2*M-1,150)
  self.assertEqual(x.checked(t[:5],0,3*M,1,20,[100]*300)['outcome'],'NO_SIGNAL')
 def test_cutoff_no_fabricated_exit(self):
  self.assertEqual(x.checked(fixture()[:6],0,3*M,1,30,[100]*300)['outcome'],'OPEN_AT_CUTOFF')
 def test_first_signal_only(self):
  r=x.checked(fixture(),0,10*M,1,20,[100]*300);self.assertEqual(r['entry_row'],5)
 def test_random_independent_oracle(self):
  rng=random.Random(221026)
  for case in range(600):
   m=(1,2,5)[case%3];s=m*M;price=1000;t=[]
   for i in range(60):
    price+=rng.randint(-45,45);t.append(tick(i,i*s//3,price))
   x.checked(t,0,21*s,m,(20,30)[case%2],[1000+rng.randint(-30,30) for _ in range(300)])
if __name__=='__main__':unittest.main()

class DayCallsiteTests(unittest.TestCase):
 def test_six_variants_and_quality_gate(self):
  from unittest.mock import patch
  census=dict(reason='',contract='MNQZ5',instrument_id=1)
  with patch.object(x.e,'read_day',return_value=(census,fixture())),patch.object(x.e,'boundaries',return_value=(-M,0,10*M)),patch.object(x,'warm_for',return_value=([100]*300,{'2025-10-19'})),patch.object(x,'DEGRADED',set()):
   c,rows=x.day_job(('unused','2025-10-20'))
   self.assertEqual(len(rows),6)
   self.assertEqual({(r['orb_minutes'],r['tp_distance']) for r in rows},{(m,t) for m in (1,2,5) for t in (20,30)})
  with patch.object(x.e,'read_day',return_value=(census,fixture())),patch.object(x.e,'boundaries',return_value=(-M,0,10*M)),patch.object(x,'warm_for',return_value=([100]*300,{'2025-10-19'})),patch.object(x,'DEGRADED',{'2025-10-19'}):
   c,rows=x.day_job(('unused','2025-10-20'))
   self.assertTrue(all(r['outcome']=='EXCLUDED' for r in rows))
