"""Run shared execution tests against the corrected production module."""
import sys,unittest
import first_fill
sys.modules['engine']=first_fill
suite=unittest.TestLoader().loadTestsFromNames(['test_engine','test_events','test_first_fill'])
result=unittest.TextTestRunner(verbosity=2).run(suite)
raise SystemExit(not result.wasSuccessful())
