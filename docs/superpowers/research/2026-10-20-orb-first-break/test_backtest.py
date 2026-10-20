import unittest
from backtest import simulate


def bars(*rows):
    return [(i*60000,*row) for i,row in enumerate(rows)]


class ORBTests(unittest.TestCase):
    def test_first_side_long_and_target(self):
        # Range 100..110, enter110.25, target120.25, stop90.25.
        r=simulate(bars((420,440,400,420),(420,482,410,480)),1)
        self.assertEqual((r["side"],r["outcome"],r["pnl_points"]),("LONG","TP",10))

    def test_short_mirror(self):
        r=simulate(bars((420,440,400,420),(420,435,358,360)),1)
        self.assertEqual((r["side"],r["outcome"],r["pnl_points"]),("SHORT","TP",10))

    def test_entry_bar_ambiguity_can_resolve_to_stop(self):
        # Wide range (50pts); stop is inside range. Entry bar lower extreme
        # could occur before or after entry. A later stop makes outcome certain.
        r=simulate(bars((500,600,400,500),(590,610,510,580),(580,590,500,520)),1)
        self.assertEqual(r["outcome"],"SL")
        self.assertEqual(r["pnl_points"],-20)
        self.assertEqual(r["stop_mfe_min_points"],0)

    def test_entry_bar_stop_then_possible_later_target_is_unresolved(self):
        r=simulate(bars((500,600,400,500),(590,610,510,580),(580,810,570,800)),1)
        self.assertEqual(r["outcome"],"UNRESOLVED")
        self.assertEqual(r["possible_outcomes"],"SL|TP")

    def test_gap_entry_and_gap_stop(self):
        r=simulate(bars((420,440,400,420),(448,450,440,445),(350,355,340,350)),1)
        self.assertEqual((r["entry"],r["pnl_points"]),(112,-24.5))

    def test_existing_position_same_bar_both_exits(self):
        r=simulate(bars((420,440,400,420),(420,450,410,445),(445,490,350,440)),1)
        self.assertEqual(r["possible_outcomes"],"SL|TP")

    def test_both_breakout_sides_cannot_survive_opposite_extreme(self):
        r=simulate(bars((500,600,400,500),(500,610,390,550),(550,850,540,800)),1)
        self.assertEqual(r["outcome"],"SL")
        self.assertEqual(r["side"],"UNKNOWN")
        self.assertEqual(r["pnl_points"],-20)

    def test_stop_ends_trade_no_later_reentry(self):
        r=simulate(bars((420,440,400,420),(445,450,350,355),(355,600,300,590)),1)
        self.assertEqual(r["outcome"],"SL")
        self.assertEqual(r["exit_ms"],60000)

    def test_known_favorable_move_then_stop(self):
        r=simulate(bars((500,600,400,500),(590,640,580,630),(630,650,500,520)),1)
        self.assertEqual(r["outcome"],"SL")
        self.assertEqual(r["stop_mfe_min_points"],9.75)

    def test_orb_2m_does_not_enter_during_second_minute(self):
        r=simulate(bars((420,440,400,420),(420,480,410,470),(470,490,460,480)),2)
        self.assertEqual(r["entry_ms"],120000)
        self.assertEqual(r["orb_high"],120)

    def test_time_exit(self):
        r=simulate(bars((420,440,400,420),(420,450,410,448)),1)
        self.assertEqual(r["outcome"],"TIME")
        self.assertEqual(r["pnl_points"],1.75)

    def test_no_entry(self):
        r=simulate(bars((420,440,400,420),(420,440,400,430)),1)
        self.assertEqual(r["outcome"],"NO_ENTRY")


if __name__=="__main__":
    unittest.main()
