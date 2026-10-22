"""Isolated in-memory mutation tests; never rewrites engine or existing output."""
import importlib.util,json,os,sys,types,unittest,io,hashlib
from pathlib import Path
import ema_orb as engine
import test_ema_orb as tests
p=Path(engine.__file__);src=p.read_text();results=[]
for tz in ('America/Chicago','UTC'):
 os.environ['TZ']=tz
 tests.x=engine
 result=unittest.TextTestRunner(stream=io.StringIO()).run(unittest.defaultTestLoader.loadTestsFromModule(tests))
 assert result.wasSuccessful()
 results.append(dict(timezone=tz,tests=result.testsRun,pass_all=True,synthetic_oracle_cases=600))
mutants=[('wrong_alpha','(4*v+D(x))/5','(3*v+D(x))/4'),('wick_instead_of_close','D(b[-1][4])','D(max(t[4] for t in b))'),('skip_receive_gate','ready=max(end,max(t[2] for _,z in bb[:i+1] for t in z))','ready=end'),('allow_equality','v>hi','v>=hi'),('enter_confirmation_close','ready<=t[1]<closed and t[0]>row','t[0]>=row and t[1]<closed'),('wrong_stop','gain<=-80','gain<=-84')]
mr=[]
for name,a,b in mutants:
 assert a in src
 mod=types.ModuleType('mutant_'+name);mod.__file__=str(p)
 exec(compile(src.replace(a,b),str(p),'exec'),mod.__dict__)
 tests.x=mod
 result=unittest.TextTestRunner(stream=io.StringIO()).run(unittest.defaultTestLoader.loadTestsFromModule(tests))
 assert result.failures and not result.errors,(name,result.failures,result.errors)
 mr.append(dict(name=name,assertion_failures=len(result.failures),errors=len(result.errors)))
tests.x=engine
out=Path('/home/hoang/fleet-1017/out/orb-ema9-20261022/engine-verification.json')
with out.open('x') as f:json.dump(dict(engine_sha256=hashlib.sha256(p.read_bytes()).hexdigest(),runs=results,mutants=mr),f,indent=2)
print(out);print(results);print(mr)
