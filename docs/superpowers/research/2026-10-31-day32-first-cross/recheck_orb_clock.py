"""Read-only source replay and event/receive bar-clock sensitivity. Exclusive outputs."""
import sys,csv,json,hashlib,multiprocessing
from pathlib import Path
from concurrent.futures import ProcessPoolExecutor
OLD=Path('/home/hoang/vl-1017-databento-ready/docs/superpowers/research/2026-10-20-databento-ready')
sys.path.append(str(OLD))
import ema_orb as old
import orb_ema_receipt as new
import ema_halfbox as half
import tick_orb as e
ROOT=Path('/home/hoang/fleet-1017/out')
SOURCE=ROOT/'orb-ema9-20261022'
OUT=ROOT/'orb-clock-recheck-20261031'

def init():
    old.init(SOURCE/'eth-1m.dbn.zst')

def job(arg):
    day,base=arg
    c,ticks=e.read_day(ROOT/'databento-mnq-20261020-data'/day,day)
    assert not c['reason'],(day,c)
    _,o,z=e.boundaries(day)
    # Read the whole selected contract at session boundaries: event-filtered
    # decoder alone would omit pre-open events received after the open.
    store=e.db.DBNStore.from_file(ROOT/'databento-mnq-20261020-data'/day/'ticks.dbn.zst')
    recv=[]
    for ix,t in enumerate(store):
        if t.instrument_id==c['instrument_id'] and o<=t.ts_recv<z:
            recv.append((ix,t.ts_event,t.ts_recv,t.sequence,t.price//e.UNIT,t.publisher_id))
    store.reader.close()
    changes=[];results=[];checked=0
    for b in base:
        m=int(b['orb_minutes']);tp=int(b['tp_distance'])
        warm,dates=old.warm_for(c['instrument_id'],o,m)
        if (dates|{day})&old.DEGRADED:
            assert b['outcome']=='EXCLUDED'
            continue
        before=old.checked(ticks,o,z,m,tp,warm)
        for k,v in before.items():
            assert str(v)==b.get(k,''),(day,m,tp,k,v,b.get(k))
        checked+=1
        after=new.checked(recv,o,z,m,tp,warm)
        keys=['outcome','orb_high','orb_low','signal_row','signal_end_ns','entry_row','entry_points','side','exit_row','pnl_points','observed_pnl_points']
        diff={k:[before.get(k),after.get(k)] for k in keys if before.get(k)!=after.get(k)}
        if diff:changes.append(dict(day=day,orb=m,tp=tp,differences=diff))
        for clock,r,tt in [('event',before,ticks),('receipt',after,recv)]:
            results.append(dict(day=day,clock=clock,variant='SL20_TP'+str(tp),orb=m,**r))
            if tp==20:
                bb={k:str(v) for k,v in dict(day=day,contract=c['contract'],**r).items()}
                h=half.simulate(tt,bb,z)
                results.append(dict(clock=clock,variant='halfbox_fullbox',orb=m,**h))
    return dict(day=day,checked=checked,changes=changes,results=results)

def main():
    OUT.mkdir(exist_ok=False)
    audit=json.loads((SOURCE/'audit.json').read_text())
    for name,h in audit['artifacts'].items():assert e.p.digest(SOURCE/name)==h
    rows=list(csv.DictReader((SOURCE/'trades.csv').open()))
    days=sorted({r['day'] for r in rows})
    allresults=[];changes=[];checked=0
    with ProcessPoolExecutor(3,initializer=init,mp_context=multiprocessing.get_context('spawn')) as pool:
        for r in pool.map(job,[(d,[x for x in rows if x['day']==d]) for d in days]):
            allresults.extend(r['results']);changes.extend(r['changes']);checked+=r['checked']
            print(r['day'],r['checked'],len(r['changes']),flush=True)
    summary=[]
    for clock in ['event','receipt']:
        for variant in ['SL20_TP20','SL20_TP30','halfbox_fullbox']:
            for m in [1,2,5]:
                rr=[r for r in allresults if r['clock']==clock and r['variant']==variant and r['orb']==m]
                closed=[r for r in rr if r['outcome'] in ['TP','SL']]
                summary.append(dict(clock=clock,variant=variant,orb=m,TP=sum(r['outcome']=='TP' for r in rr),SL=sum(r['outcome']=='SL' for r in rr),open=sum(r['outcome']=='OPEN_AT_CUTOFF' for r in rr),nominal_points=sum(float(r['pnl_points']) for r in closed),observed_points=sum(float(r['observed_pnl_points']) for r in closed)))
    result=dict(days=len(days),source_rows_reproduced=checked,changed_rows=len(changes),changes=changes,summary=summary,source_sha=e.p.digest(SOURCE/'trades.csv'))
    for name,data in [('audit.json',result),('rows.json',allresults)]:
        with (OUT/name).open('x') as f:json.dump(data,f,indent=2)
    print(json.dumps(result),flush=True)
if __name__=='__main__':main()
