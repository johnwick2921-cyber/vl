"""One research milestone, using existing bridge delivery; no new mail transport."""
import json
import os
from pathlib import Path
import subprocess
import sys
import time

import pull_mnq as p

BASE=Path('/home/hoang/fleet-1017/out')
DATA=BASE/'databento-mnq-20261020-data'
OUTPUT=BASE/'orb-tick-six-month-20261020'
HERE=Path(__file__).parent


def notify(message, name):
    text='FROM: CTO (Codex)\nTO: CTO/ALL\n'+message
    with (BASE/name).open('x') as f:
        result=subprocess.run(['node','/home/hoang/.agent-bridge/server.mjs','--cli','send','claude',text],
                              env=dict(os.environ,BRIDGE_FROM='claude'),stdout=f,stderr=subprocess.STDOUT)
    p.require(result.returncode==0,'Bridge delivery failed; inspect receipt')


def main():
    days=p.make_plan('2025-10-20','2026-04-20')['days']
    last=-1;stopped_notified=False
    while True:
        done=sum((DATA/d['day']/'complete.json').exists() for d in days)
        if done!=last: print(f'Six-month receipts {done}/{len(days)}',flush=True);last=done
        if done==len(days):break
        completion=BASE/'databento-mnq-completion-20261020-run3.json'
        if completion.exists() and not stopped_notified:
            notify(f'Six-month ORB milestone still missing {len(days)-done} daily files. '
                   'Run 3 ended before the milestone. Continue download recovery; no report claimed.',
                   'orb-six-month-download-blocked-20261020.txt')
            stopped_notified=True
        time.sleep(30)
    # Run fresh tests before processing. Never deliver a final result on file count alone.
    subprocess.run([sys.executable,'-B','verify_tick_engine.py',
                    str(BASE/'orb-six-month-engine-verification-20261020.json')],cwd=HERE,check=True)
    subprocess.run([sys.executable,'-B','tick_orb.py','--root',str(DATA),'--out',str(OUTPUT)],
                   cwd=HERE,check=True)
    with (OUTPUT/'engine-verification.json').open('x') as f:
        f.write((BASE/'orb-six-month-engine-verification-20261020.json').read_text())
    subprocess.run([sys.executable,'-B','audit_tick_orb.py','--root',str(DATA),'--results',str(OUTPUT)],
                   cwd=HERE,check=True)
    subprocess.run([sys.executable,'-B','report_tick_orb.py',str(OUTPUT)],cwd=HERE,check=True)
    notify('Earliest six months 2025-10-20 inclusive to 2026-04-20 exclusive: all 130 weekday '
           'receipts present; engine tests, separate first-passage replay and table reconciliation '
           'passed. Draft report at '+str(OUTPUT/'REPORT.md')+'. CTO must inspect exclusions, '
           'assumptions and evidence before presenting it as final. No broker-fill proof or deployment.',
           'orb-six-month-complete-delivery-20261020.txt')


if __name__=='__main__':
    try: main()
    except Exception as exc:
        notify('Six-month tick report stopped at verification: '+type(exc).__name__+
               '. Read /home/hoang/fleet-1017/out/orb-six-month-milestone-20261020.log. '
               'Do not publish a final report until corrected.',
               'orb-six-month-error-delivery-20261020.txt')
        raise
