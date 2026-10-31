import unittest
from decimal import Decimal, localcontext
from engine import Tick, simulate, seed

WARM = [1000]*1000

def ticks(prices):
    return [Tick(i, i*1000, i*1000+1, p) for i,p in enumerate(prices)]

def run(prices, stop=15):
    return simulate(ticks(prices), 0, 10**12, WARM, stop)

class EngineTests(unittest.TestCase):
    def test_no_cross_constant(self):
        self.assertEqual(run([1000]*20)['outcome'], 'NO_CROSS')

    def test_cross_tick_cannot_fill(self):
        r=run([1100])
        self.assertEqual(r['outcome'], 'NO_RETEST')
        self.assertIsNone(r['entry_row'])

    def test_long_touch_then_target(self):
        r=run([1100,1020,1080])
        self.assertEqual((r['signal_row'],r['entry_row'],r['exit_row']), (0,1,2))
        self.assertEqual((r['entry_ticks'],r['outcome'],r['pnl_points']), (1020,'TP',15))

    def test_short_symmetry(self):
        r=run([900,980,920])
        self.assertEqual((r['entry_ticks'],r['outcome'],r['pnl_points']), (980,'TP',15))

    def test_stop15_vs16_same_entry(self):
        a,b=run([1100,1020,960,1080],15),run([1100,1020,960,1080],16)
        self.assertEqual(a['entry_row'],b['entry_row'])
        self.assertEqual((a['outcome'],b['outcome']),('SL','TP'))

    def test_gap_loss_not_falsely_capped(self):
        r=run([1100,1020,940])
        self.assertEqual((r['pnl_points'],r['nominal_pnl_points']),(-20,-15))

    def test_target_is_limit_not_favorable_gap(self):
        self.assertEqual(run([1100,1020,1200])['pnl_points'],15)

    def test_first_trade_only(self):
        r=run([1100,1020,1080,900,980,920])
        self.assertEqual((r['exit_row'],r['side']),(2,1))

    def test_forming_ema_not_tick_compounded(self):
        # Two prices in ONE 2m candle: EMA9 must be 1040, not 1056.
        r=run([1100,1200,1040,1100])
        self.assertEqual((r['entry_ticks'],r['entry_row']),(1040,2))

    def test_cutoff_keeps_open_separate(self):
        r=run([1100,1020,1030])
        self.assertEqual(r['outcome'],'OPEN_AT_CUTOFF')
        self.assertIsNone(r['pnl_points'])
        self.assertEqual(r['mark_pnl_points'],2.5)

    def test_no_same_print_exit_even_gap_at_entry(self):
        r=run([1100,900])
        self.assertEqual(r['outcome'],'OPEN_AT_CUTOFF')
        self.assertEqual(r['entry_row'],1)
        self.assertIsNone(r['exit_row'])

    def test_event_regression_rejected(self):
        with self.assertRaises(ValueError):
            simulate([Tick(0,100,101,1000),Tick(1,90,102,1001)],0,1000,WARM,15)

    def test_insufficient_seed_rejected(self):
        with self.assertRaises(ValueError):seed([1000]*999,34)

    def test_seed_independent_weighted_sum(self):
        closes=[1000+(i%17) for i in range(1000)]
        with localcontext() as c:
            c.prec=40
            for period in (9,34):
                a=Decimal(2)/Decimal(period+1);q=1-a;n=len(closes)
                expected=Decimal(closes[0])*q**(n-1)
                expected+=sum(a*Decimal(x)*q**(n-1-i) for i,x in enumerate(closes[1:],1))
                self.assertLess(abs(seed(closes,period)-expected),Decimal('1e-32'))

    def test_receipt_after_cutoff_not_used(self):
        r=simulate([Tick(0,0,1,1100),Tick(1,10,200,1020)],0,100,WARM,15)
        self.assertEqual(r['outcome'],'NO_RETEST')

    def test_two_minute_boundary_commits_once(self):
        r=simulate([Tick(0,0,1,1100),Tick(1,120000000000,120000000001,1200),
                    Tick(2,120000001000,120000001001,1056)],0,10**12,WARM,15)
        self.assertEqual(r['entry_ticks'],1056)

if __name__=='__main__':unittest.main()
