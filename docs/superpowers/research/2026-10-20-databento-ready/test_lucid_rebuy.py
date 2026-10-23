import unittest,random
from lucid_rebuy import repeat
from test_lucid_payout import path,oracle
class Tests(unittest.TestCase):
 def test_breach_next_day(self):
  s,a,l,p=repeat([path('01',-1500,-1500),path('02',100)],3)
  self.assertEqual(s['accounts_used'],2);self.assertEqual(a[1]['first_day'],'02');self.assertEqual(l[1]['start_cents'],5000000)
 def test_last_day_no_extra_purchase(self):self.assertEqual(repeat([path('01',-1500,-1500)],3)[0]['accounts_used'],1)
 def test_five_payouts(self):
  s,a,l,p=repeat([path(f'{i:03}',802) for i in range(26)],3);self.assertEqual(a[0]['trades'],25);self.assertEqual(a[1]['trades'],1);self.assertEqual(l[25]['start_cents'],5000000)
 def test_duplicate_rejected(self):
  with self.assertRaises(AssertionError):repeat([path('01',100),path('01',100)],3)
 def test_random_oracle(self):
  rng=random.Random(2323)
  for _ in range(1000):
   ps=[path(f'{i:03}',g:=rng.randint(-1800,1800),min(g,rng.randint(-1400,0))) for i in range(70)];q=rng.choice([3,6]);s,aa,ll,pp=repeat(ps,q);i=0
   for a in aa:
    expected=oracle(ps[i:],q)
    for k,v in expected.items():
     if k=='status' and v=='LIVE_TRANSITION_AFTER_5_PAYOUTS':v='REPLACE_AFTER_PAYOUT_FIVE_HYPOTHETICAL'
     self.assertEqual(a[k],v)
    i+=expected['trades']
   self.assertEqual(i,len(ps));self.assertEqual(s['received_cents'],sum(p['received_cents'] for p in pp))
if __name__=='__main__':unittest.main()
