"""Offline tests + three in-memory mutants. Output path must be new."""
import contextlib
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import types
import unittest
from importlib.metadata import version
import pull_mnq as p
import test_pull_mnq as tests


def run(out):
    results = {}
    for tz in ('America/Chicago', 'UTC'):
        env = dict(os.environ, TZ=tz, PYTHONDONTWRITEBYTECODE='1')
        r = subprocess.run([sys.executable, '-B', '-m', 'unittest', '-v',
                            'test_pull_mnq.py'], capture_output=True, text=True, env=env)
        p.require(r.returncode == 0, r.stderr)
        results[tz] = {'exit': r.returncode, 'output': r.stdout + r.stderr}
    source = Path('pull_mnq.py').read_text()
    mutations = [
        ('disable_total_cost_gate', 'spent + sum(quotes, Decimal(0)) <= cap', 'True'),
        ('disable_saved_hash_check', "digest(folder / 'ticks.dbn.zst') == done['sha256']", 'True'),
        ('ignore_Houston_DST', "CT = ZoneInfo('America/Chicago')", 'CT = timezone(timedelta(hours=-5))')]
    rows = []
    for name, old, new in mutations:
        p.require(old in source, 'Mutation target missing')
        mutant = types.ModuleType('mutant')
        exec(compile(source.replace(old, new, 1), 'in-memory-' + name, 'exec'), mutant.__dict__)
        stream = io.StringIO()
        tests.p = mutant
        try:
            with contextlib.redirect_stdout(stream):
                result = unittest.TextTestRunner(stream=stream).run(
                    unittest.defaultTestLoader.loadTestsFromTestCase(tests.Tests))
        finally:
            tests.p = p
        # Require an actual failed assertion, not exhausted mock responses.
        p.require(len(result.failures) > 0 and len(result.errors) == 0,
                  'Mutant did not fail cleanly: ' + name)
        rows.append({'name': name, 'caught': True, 'failures': len(result.failures),
                     'errors': len(result.errors)})
    report = {'status': 'OFFLINE_READY_AUTHENTICATED_VALIDATION_PENDING',
              'test_count': 15, 'timezone_runs': results, 'mutants': rows,
              'versions': {x: version(x) for x in
                           ['databento', 'databento-dbn', 'pandas', 'zstandard']},
              'network_calls': 0, 'paid_data_downloads': 0}
    p.write_json(out, report)
    print(json.dumps({k: v for k, v in report.items() if k != 'timezone_runs'}, indent=2))


if __name__ == '__main__':
    run(sys.argv[1])
