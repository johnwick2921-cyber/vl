import unittest,sys
from decimal import Decimal
import first_fill_receipt as e
class ClockTests(unittest.TestCase):
    def test_late_record_belongs_to_receive_bucket(self):
        ticks=[e.Tick(0,0,1,1100),e.Tick(1,e.SPAN-1,e.SPAN+1,1200)]
        values=[]
        def trace(fr,event,arg):
            if fr.f_code is e._simulate.__code__ and event=='return':
                values.append(fr.f_locals['cur9'])
            return trace
        sys.settrace(trace)
        try:e.simulate(ticks,0,e.SPAN*3,[1000]*1000,15)
        finally:sys.settrace(None)
        self.assertEqual(values[-1],Decimal(1056))
    def test_no_reset_at_midnight(self):
        # A session timestamp offset cannot alter arithmetic or fill rows.
        ticks=[e.Tick(0,0,1,1100),e.Tick(1,1000,1001,1020),e.Tick(2,2000,2001,1080)]
        a=e.simulate(ticks,0,10000,[1000]*1000,15)
        offset=7*3600*10**9
        b=e.simulate([e.Tick(t.row,t.event_ns+offset,t.recv_ns+offset,t.price) for t in ticks],offset,offset+10000,[1000]*1000,15)
        self.assertEqual((a['entry_row'],a['pnl_points']),(b['entry_row'],b['pnl_points']))
if __name__=='__main__':unittest.main()
