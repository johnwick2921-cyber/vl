#!/usr/bin/env python3
"""Verify cited public basis blobs by immutable SHA and exact Git blob size."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import urllib.request

RUNNING = '770e2297d2188d09de0dcf76c3722e19e022e4c6'
RULEBOOK = 'daeb654978b0c739592e8589a393c5e79171560d'
sources = [
    (RUNNING, 'docs/superpowers/reports/2026-09-10-episode-contract.md'),
    (RUNNING, 'docs/superpowers/reports/2026-09-09-candidates-not-entitlements.md'),
    (RUNNING, 'docs/superpowers/reports/2026-09-10-every-detector-every-timeframe.md'),
    (RUNNING, 'docs/superpowers/SYSTEM-MAP.md'),
    (RULEBOOK, 'docs/superpowers/VL-TRADING-RULEBOOK-v1.md'),
]
out = []
for rev, path in sources:
    blob = subprocess.check_output(['git', 'show', rev + ':' + path])
    ls_tree = subprocess.check_output(['git', 'ls-tree', '-l', rev, '--', path], text=True).strip()
    last_change = subprocess.check_output(['git', 'log', rev, '-1', '--format=%H %cI %s', '--', path], text=True).strip()
    url = 'https://raw.githubusercontent.com/johnwick2921-cyber/nofx/' + rev + '/' + path
    with urllib.request.urlopen(url, timeout=30) as response:
        content = response.read()
        status = response.status
    assert status == 200 and content == blob, (path, status, len(content), len(blob))
    out.append(dict(revision=rev, path=path, url=url, http_status=status,
                    size_download=len(content), git_blob_bytes=len(blob),
                    git_ls_tree=ls_tree, last_change=last_change,
                    sha256=hashlib.sha256(blob).hexdigest()))
Path(sys.argv[1]).write_text(json.dumps(out, indent=2) + '\n')
print(json.dumps([{k: r[k] for k in ('path', 'http_status', 'size_download')} for r in out], indent=2))
