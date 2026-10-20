"""Fresh CT/UTC runs and assertion-killed mutations, no network calls."""
import contextlib
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import types
import unittest

import tick_orb as e
import test_tick_orb as tests


def run(out):
    logs={}
    for tz in ('America/Chicago','UTC'):
        p=subprocess.run([sys.executable,'-B','-m','unittest','-v','test_tick_orb.py'],
                         env=dict(os.environ,TZ=tz),capture_output=True,text=True)
        e.p.require(p.returncode==0,p.stdout+p.stderr)
        logs[tz]=dict(exit=p.returncode,log=p.stdout+p.stderr)
    mutations=[('stop_not_20','STOP = 80','STOP = 79'),
               ('break_on_touch','price >= high+1','price >= high'),
               ('entry_boundary_instead_of_observed','entry = price  #','entry = high+1 if side == 1 else low-1  #'),
               ('range_includes_next_tick','opened <= t[1] < end','opened <= t[1] <= end'),
               ('time_cutoff_includes_next_session','end <= t[1] < closed','end <= t[1] <= closed'),
               ('ignore_hash',"p.digest(raw) == receipt['sha256']",'True'),
               ('wrong_cost','pnl-2','pnl-1')]
    source=Path(e.__file__).read_text();results=[]
    for name,old,new in mutations:
        e.p.require(old in source,'Mutation target missing')
        module=types.ModuleType('tick_mutant');module.__file__=e.__file__
        exec(compile(source.replace(old,new,1),name,'exec'),module.__dict__)
        stream=io.StringIO();tests.e=module
        try:
            with contextlib.redirect_stdout(stream):
                result=unittest.TextTestRunner(stream=stream).run(
                    unittest.defaultTestLoader.loadTestsFromTestCase(tests.Tests))
        finally: tests.e=e
        e.p.require(len(result.failures)>0 and not result.errors,'Mutant not cleanly killed: '+name+' '+stream.getvalue())
        results.append(dict(name=name,failures=len(result.failures),errors=len(result.errors)))
    report=dict(status='PASS_OFFLINE_ENGINE_TESTS',timezone_runs=logs,mutants=results,
                test_count=unittest.defaultTestLoader.loadTestsFromTestCase(tests.Tests).countTestCases(),
                synthetic_first_passage_comparisons=6000,engine_sha256=e.p.digest(Path(e.__file__)))
    e.p.write_json(out,report)
    print(json.dumps({k:v for k,v in report.items() if k!='timezone_runs'},indent=2))


if __name__=='__main__': run(Path(sys.argv[1]))
