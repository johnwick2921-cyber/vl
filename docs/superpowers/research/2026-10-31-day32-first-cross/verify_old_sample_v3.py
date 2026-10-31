"""Independent float/weighted-seed event replay, no production engine imports."""
import json,math,csv
from pathlib import Path
from collections import defaultdict,Counter
from itertools import groupby
import databento as db
ROOT=Path('/home/hoang/fleet-1017/out/day32-first-cross-20261031')
SPAN=120000000000;UNIT=250000000

def replay(groups,warm,opened,stop):
    # Independent explicit geometric weights rather than recursive seed.
    seeds=[]
    for period in (9,34):
        a=2/(period+1);q=1-a;n=len(warm)
        seeds.append(warm[0]*q**(n-1)+math.fsum(a*x*q**(n-1-i) for i,x in enumerate(warm[1:],1)))
    closed=seeds[:];forming=seeds[:];previous=seeds[0]-seeds[1];bar=None
    direction=0;limit=None;entry=None;signal=None;entryrow=None
    outcome='NO_CROSS';last=None
    for key,g in groups:
        if entry is not None:
            for row,price,recv in g:
                gain=direction*(price-entry)
                if gain>=60:return ('TP',signal,entryrow,row,entry,15)
                if gain<=-stop*4:return ('SL',signal,entryrow,row,entry,gain/4)
            continue
        if limit is not None:
            hits=[(row,price) for row,price,recv in g if direction*(price-limit)<=0]
            if hits:
                entry=limit;entryrow=hits[0][0];outcome='OPEN_AT_CUTOFF';continue
        timestamp=g[-1][2];idx=(timestamp-opened)//SPAN
        if bar is not None and idx!=bar:closed=forming[:]
        bar=idx;price=g[-1][1]
        forming=[closed[0]*.8+price*.2,closed[1]*(33/35)+price*(2/35)]
        diff=forming[0]-forming[1]
        cross=1 if previous<=0<diff else -1 if previous>=0>diff else 0
        previous=diff
        if cross:
            direction=cross;signal=g[-1][0];outcome='NO_RETEST';limit=None
        if direction:
            v=forming[0]
            if abs(v-round(v))<1e-8:v=round(v)
            proposed=math.floor(v) if direction==1 else math.ceil(v)
            limit=proposed if direction*(price-proposed)>0 else None
    return (outcome,signal,entryrow,None,entry,None)

def main():
    warm=defaultdict(list)
    s=db.DBNStore.from_file('/home/hoang/fleet-1017/out/orb-ema9-20261022/eth-1m.dbn.zst')
    for r in s:warm[r.instrument_id].append((r.ts_event,r.close//UNIT))
    s.reader.close()
    plan={r['day']:r for r in json.loads((ROOT/'plan.json').read_text())}
    ledger=json.loads((ROOT/'run-ema9-34-owner-rerun-v3/ledger.json').read_text())
    byday=defaultdict(list)
    for r in ledger:byday[r['day']].append(r)
    mismatches=[];checked=0;rawchecks=0
    for day,rows in byday.items():
        p=plan[day];iid=rows[0]['instrument_id'];bars={}
        for ts,cl in warm[iid]:
            end=(ts//SPAN+1)*SPAN
            if end<=p['open_ns']:bars[end]=cl
        seed=[v for k,v in sorted(bars.items())[-1000:]]
        raw=[];s=db.DBNStore.from_file(ROOT/'included-download'/day/'ticks.dbn')
        for i,r in enumerate(s):
            if r.instrument_id==iid and p['open_ns']<=r.ts_event<p['end_ns'] and r.ts_recv<p['end_ns']:
                raw.append(((r.ts_event,r.sequence),(i,r.price//UNIT,r.ts_recv)))
        s.reader.close()
        groups=[(key,[v for k,v in g]) for key,g in groupby(raw,key=lambda t:t[0])]
        rawmap={value[0]:(key,value[1]) for key,value in raw}
        for r in rows:
            expected=(r['outcome'],r['signal_row'],r['entry_row'],r['exit_row'],r.get('entry_ticks'),r['pnl_points'])
            actual=replay(groups,seed,p['open_ns'],r['stop_points'])
            if expected!=actual:mismatches.append(dict(day=day,stop=r['stop_points'],engine=expected,oracle=actual))
            checked+=1
            if r['entry_row'] is not None:
                en=rawmap[r['entry_row']];sg=rawmap[r['signal_row']]
                assert r['entry_row']>r['signal_row'] and en[0]!=sg[0]
                assert r['side']*(en[1]-r['entry_ticks'])<=0
                if r['exit_row'] is not None:
                    ex=rawmap[r['exit_row']];gain=r['side']*(ex[1]-r['entry_ticks'])/4
                    assert r['exit_row']>r['entry_row'] and ex[0]!=en[0]
                    assert gain>=15 if r['outcome']=='TP' else gain<=-r['stop_points']
                    assert r['pnl_points']==(15 if r['outcome']=='TP' else gain)
                rawchecks+=1
        print(day,'checked',flush=True)
    summaries=json.loads((ROOT/'run-ema9-34-owner-rerun-v3/summary.json').read_text())
    for summary in summaries:
        rr=[r for r in ledger if r['stop_points']==summary['stop']]
        assert summary['net_points']==sum(r['pnl_points'] or 0 for r in rr)
        assert summary['TP']+summary['SL']+summary['open']==summary['entries']
        assert summary['won_points']==summary['TP']*15
        assert summary['included_sessions']==len(rr)
    result=dict(independent_replay_rows=checked,raw_entry_exit_checks=rawchecks,mismatches=mismatches,summary_arithmetic='PASS',precision='engine Decimal40 versus independent float weighted seed')
    with (ROOT/'run-ema9-34-owner-rerun-v3/independent-audit.json').open('x') as f:json.dump(result,f,indent=2)
    print(json.dumps(result),flush=True)
    if mismatches:raise SystemExit(1)
if __name__=='__main__':main()
