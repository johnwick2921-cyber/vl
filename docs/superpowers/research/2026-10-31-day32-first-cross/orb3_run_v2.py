"""51 declared ORB configurations, preserved legacy rules, no live operations."""
import sys,json,csv,time,multiprocessing,hashlib
from pathlib import Path
from concurrent.futures import ProcessPoolExecutor
from collections import Counter
OLD=Path('/home/hoang/vl-1017-databento-ready/docs/superpowers/research/2026-10-20-databento-ready')
sys.path.append(str(OLD))
import tick_orb as e
import stop_variants as breakout
import retest_variants as retest
import body_retest as body
import ema_orb as warm_engine
import orb_ema_receipt as ema
import ema_halfbox as half
import ema_box_lock as lock
ROOT=Path('/home/hoang/fleet-1017/out/orb3-recent-20261031')
WARM=Path('/home/hoang/fleet-1017/out/day32-recent-six-months-20261031/warmup/eth-1m.dbn.zst')
CONFIGS=[(m,g,v) for m in (1,2,5) for g,vs in [('breakout',['SL20_box','box','half_box','20_to_BE']),('retest',['touch_'+s for s in breakout.MODES]+[b+'_'+s for b in ('half_body','full_body') for s in breakout.MODES]),('EMA9',['SL20_TP20','SL20_TP30','halfbox_fullbox','halfbox_lock5'])] for v in vs]
assert len(CONFIGS)==51
def init():warm_engine.init(WARM)
def normalize(day,contract,m,g,v,r):
    out=dict(r);out.update(day=day,contract=contract,orb=m,group=g,variant=v)
    if r.get('entry_row','') not in ('',None):out['entered']=True
    else:out['entered']=False
    out['points']=r.get('pnl_observed_points',r.get('observed_pnl_points',r.get('pnl_points','')))
    if r.get('outcome') not in ('TP','SL','BE','LOCK'):out['points']=''
    return out
def fixed20(ticks,o,z,m):
    r=e.replay(ticks,o,z,m)
    if r['outcome']=='EXCLUDED':return r
    formation=[t for t in ticks if o<=t[1]<o+m*e.MINUTE];hi=max(t[4] for t in formation);lo=min(t[4] for t in formation)
    future=[t for t in ticks if o+m*e.MINUTE<=t[1]<z]
    ix=next((i for i,t in enumerate(future) if t[4]>hi or t[4]<lo),None)
    if ix is None:assert r['outcome']=='NO_ENTRY';return r
    en=future[ix];side=1 if en[4]>hi else -1
    assert r['entry_row']==en[0]
    exit=next((t for t in future[ix+1:] if side*(t[4]-en[4])<=-80 or side*(t[4]-en[4])>=hi-lo),None)
    if exit is None:assert r['outcome']=='OPEN_AT_CUTOFF'
    else:
        gain=side*(exit[4]-en[4]);assert r['exit_row']==exit[0] and r['pnl_points']==(gain/4 if gain<=-80 else (hi-lo)/4)
    return r
def job(day):
    receipt=ROOT/'data'/day/'complete.json';deadline=time.monotonic()+1800
    while not receipt.exists():
        if time.monotonic()>deadline:raise TimeoutError(day)
        time.sleep(1)
    c,ticks=e.read_day(ROOT/'data'/day,day);_,o,z=e.boundaries(day)
    if c['reason']:
        return c,[normalize(day,c['contract'],m,g,v,dict(outcome='EXCLUDED',reason=c['reason'])) for m,g,v in CONFIGS]
    # EMA warmup uses receipt-clock OHLCV. Read the full raw file so events
    # just before the open but received after it remain available.
    s=e.db.DBNStore.from_file(ROOT/'data'/day/'ticks.dbn.zst');recv=[]
    for ix,t in enumerate(s):
        if t.instrument_id==c['instrument_id'] and o<=t.ts_recv<z:recv.append((ix,t.ts_event,t.ts_recv,t.sequence,t.price//e.UNIT,t.publisher_id))
    s.reader.close();out=[]
    for m in (1,2,5):
        out.append(normalize(day,c['contract'],m,'breakout','SL20_box',fixed20(ticks,o,z,m)))
        for mode in breakout.MODES:
            out.append(normalize(day,c['contract'],m,'breakout',mode,breakout.checked(ticks,o,z,m,mode)))
            out.append(normalize(day,c['contract'],m,'retest','touch_'+mode,retest.checked(ticks,o,z,m,mode,False)))
            for b in ('half_body','full_body'):
                out.append(normalize(day,c['contract'],m,'retest',b+'_'+mode,body.checked(ticks,o,z,m,mode,b)))
        warm,dates=warm_engine.warm_for(c['instrument_id'],o,m)
        bad=(dates|{day})&warm_engine.DEGRADED
        if bad:
            for v in ['SL20_TP20','SL20_TP30','halfbox_fullbox','halfbox_lock5']:out.append(normalize(day,c['contract'],m,'EMA9',v,dict(outcome='EXCLUDED',reason='provider_degraded:'+','.join(sorted(bad)))))
            continue
        for tp in (20,30):
            r=ema.checked(recv,o,z,m,tp,warm)
            out.append(normalize(day,c['contract'],m,'EMA9','SL20_TP'+str(tp),r))
            if tp==20:base={k:str(v) for k,v in dict(day=day,contract=c['contract'],**r).items()}
        out.append(normalize(day,c['contract'],m,'EMA9','halfbox_fullbox',half.simulate(recv,base,z)))
        out.append(normalize(day,c['contract'],m,'EMA9','halfbox_lock5',lock.simulate(recv,base,z,2,5)))
    assert len(out)==51
    # Independent raw row accounting for all closed trades.
    raw={t[0]:t for t in ticks}
    for r in out:
        if r['points']=='':continue
        en=raw[int(r['entry_row'])];ex=raw[int(r['exit_row'])]
        side=1 if r['side']=='LONG' else -1
        if r['outcome']!='TP':assert r['points']==side*(ex[4]/4-float(r['entry_points'])),(day,r)
        assert ex[0]>=en[0]
    return c,out
def summary(rows,m,g,v):
    rr=[r for r in rows if (r['orb'],r['group'],r['variant'])==(m,g,v)];counts=Counter(r['outcome'] for r in rr)
    closed=[r for r in rr if r['points']!=''];wins=[r for r in closed if r['points']>0];losses=[r for r in closed if r['points']<0]
    runw=runl=maxw=maxl=0;eq=peak=dd=0
    for r in closed:
        p=r['points'];runw=runw+1 if p>0 else 0;runl=runl+1 if p<0 else 0;maxw=max(maxw,runw);maxl=max(maxl,runl)
        eq+=p;peak=max(peak,eq);dd=max(dd,peak-eq)
    return dict(group=g,variant=v,orb=m,entries=sum(r['entered'] for r in rr),TP=counts['TP'],SL=counts['SL'],BE=counts['BE'],LOCK=counts['LOCK'],open=counts['OPEN_AT_CUTOFF'],excluded=counts['EXCLUDED'],no_entry=sum(not r['entered'] and r['outcome']!='EXCLUDED' for r in rr),positive=len(wins),negative=len(losses),zero=sum(r['points']==0 for r in closed),win_percent=100*len(wins)/len(closed) if closed else None,won_points=sum(r['points'] for r in wins),lost_points=sum(r['points'] for r in losses),gross_points=eq,win_streak=maxw,loss_streak=maxl,closed_drawdown=dd,max_day_win=max([r['points'] for r in closed],default=0),max_day_loss=min([r['points'] for r in closed],default=0))
def main():
    out=ROOT/'run-v2';out.mkdir(exist_ok=False)
    assert e.p.digest(WARM)==json.loads((WARM.parent/'warmup-receipt.json').read_text())['sha256']
    plan=json.loads((ROOT/'plan.json').read_text());rows=[];census=[]
    with ProcessPoolExecutor(3,initializer=init,mp_context=multiprocessing.get_context('spawn')) as pool:
        for c,rr in pool.map(job,plan['days']):
            census.append(c);rows.extend(rr);print(c['day'],c['reason'] or '51 configurations checked',flush=True)
    summaries=[summary(rows,*c) for c in CONFIGS]
    assert len(rows)==len(plan['days'])*51
    for name,data in [('summary.csv',summaries),('trades.csv',rows),('sessions.csv',census)]:e.write_csv(out/name,data)
    for name,data in [('summary.json',summaries),('ledger.json',rows)]:
        with (out/name).open('x') as f:json.dump(data,f,indent=2)
    modules=[e,breakout,retest,body,warm_engine,ema,half,lock]
    with (out/'audit.json').open('x') as f:json.dump(dict(days=len(census),rows=len(rows),configurations=51,checked_days=sum(not c['reason'] for c in census),engine_hashes={m.__name__:e.p.digest(Path(m.__file__)) for m in modules},warmup_sha=e.p.digest(WARM),artifacts={n:e.p.digest(out/n) for n in ['summary.csv','trades.csv','sessions.csv','ledger.json']}),f,indent=2)
    print('COMPLETE',len(rows),'rows',flush=True)
if __name__=='__main__':main()
