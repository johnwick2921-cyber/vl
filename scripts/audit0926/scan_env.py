#!/usr/bin/env python3
"""Scan the repo for env-knob read sites (file:line, type, code default).

READ-ONLY. Prints JSON to stdout:
[{name, defined_at, type, code_default, readers:[file:line ...]}]
"""
import json
import os
import re
import subprocess
import sys

ROOT = "/home/hoang/nofx-ds-102-audit0926"
NAMES_FILE = "/tmp/env-knobs.txt"
SKIP_DIRS = {"web", "vendor", "node_modules", ".git", "scripts", "data", "deploy"}

names = [l.strip() for l in open(NAMES_FILE) if l.strip()]

def go_files():
    out = subprocess.run(
        ["git", "-C", ROOT, "ls-files", "*.go"],
        capture_output=True, text=True, check=True).stdout.splitlines()
    return [f for f in out if f.endswith(".go") and not f.endswith("_test.go")]

files = go_files()

# 1. collect call sites with a tolerant regex
call_re = re.compile(
    r'(?:os\.)?(?:Getenv|LookupEnv|getEnv\w*)\s*\(\s*"([A-Z0-9_]+)"'
    r'(?:\s*,\s*([^)]*?))?\)')

sites = {n: [] for n in names}
extra = set()

for f in files:
    path = os.path.join(ROOT, f)
    for i, line in enumerate(open(path, encoding="utf-8", errors="replace"), 1):
        for m in call_re.finditer(line):
            name = m.group(1)
            default = (m.group(2) or "").strip()
            if name not in sites:
                sites[name] = []
                extra.add(name)
            sites[name].append((f, i, default, line.strip()))

# 2a. live call sites the 188 list missed join the census via the same loop.
# 2b. names with no regex match: plain string grep fallback (any non-vendor
# file, tests included — a test-only reader is itself a finding)
def grep_plain(name):
    out = subprocess.run(
        ["grep", "-rn", "-F", '"%s"' % name, "--include=*.go", "--include=*.sh",
         "--include=*.py", ROOT],
        capture_output=True, text=True)
    res = []
    for ln in out.stdout.splitlines():
        if "/vendor/" in ln or "/node_modules/" in ln:
            continue
        path = ln.split(":", 2)
        if len(path) == 3:
            res.append((path[0].replace(ROOT + "/", ""), int(path[1]), "", ""))
    return res

def infer_type(default, line):
    if "Atoi" in line or "ParseInt" in line or default.strip().lstrip("-").isdigit():
        return "int"
    if "ParseFloat" in line or re.search(r"\d+\.\d*", default):
        return "float"
    if "ParseBool" in line or default in ("true", "false"):
        return "bool"
    return "string"

out = []
for n in sorted(sites):
    ss = sites[n]
    if not ss:
        for p, l, d, ln in grep_plain(n):
            ss.append((p, l, d, ln))
    if not ss:
        out.append({"name": n, "defined_at": "NOT-FOUND", "type": "string",
                    "code_default": "", "readers": []})
        continue
    # first site = defined_at (prefer config/config.go, then non-test)
    first = min(ss, key=lambda s: (0 if "config/config.go" in s[0] else 1, s[0], s[1]))
    fname, lineno, default, line = first
    out.append({
        "name": n,
        "defined_at": "%s:%d" % (fname, lineno),
        "type": infer_type(default, line),
        "code_default": default,
        "readers": ["%s:%d" % (s[0], s[1]) for s in ss],
    })

print(json.dumps({"knobs": out, "extra_seen_but_not_listed": sorted(extra)}, indent=1))
