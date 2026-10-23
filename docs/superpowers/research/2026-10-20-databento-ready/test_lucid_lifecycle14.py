import unittest
from datetime import date,timedelta
from lucid_lifecycle14 import run,cooldown_end
from test_lucid_payout import path

class LifecycleTests(unittest.TestCase):
 def test_calendar_boundary(self):
  self.assertEqual(cooldown_end('2026-01-14'),'2026-01-28')
  self.assertEqual(cooldown_end('2026-02-17'),'2026-03-03')
 def test_live_failure_skips_exactly_thirteen_following_days(self):
  pp=[path((date(2026,1,1)+timedelta(days=i)).isoformat(),802) for i in range(25)]
  pp.append(path('2026-01-26',-2000,-2000))
  pp.extend(path((date(2026,1,27)+timedelta(days=i)).isoformat(),100) for i in range(14))
  s,st,ls,ps,skip=run(pp,3)
  self.assertEqual([r['phase'] for r in st],['SIM','LIVE','SIM'])
  self.assertEqual(st[-1]['first_day'],'2026-02-09')
  self.assertEqual(len(skip),13)
  self.assertEqual(s['live_breaches'],1)
 def test_sim_breach_next_day_no_cooldown(self):
  s,st,*_=run([path('2026-01-01',-2000,-2000),path('2026-01-02',100)],3)
  self.assertEqual(s['sim_accounts'],2);self.assertEqual(s['skipped_signals'],0)
 def test_last_day_breach_no_phantom_purchase(self):
  s,*_=run([path('2026-01-01',-2000,-2000)],3)
  self.assertEqual(s['sim_accounts'],1)
 def test_fifth_payout_no_phantom_live(self):
  s,*_=run([path((date(2026,1,1)+timedelta(days=i)).isoformat(),802) for i in range(25)],3)
  self.assertEqual(s['live_accounts'],0)
if __name__=='__main__':unittest.main()
