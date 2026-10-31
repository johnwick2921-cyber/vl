"""Instrument the actual first_fill call and compare every evaluated EMA to pandas."""
from pathlib import Path
from collections import defaultdict
from concurrent.futures import ProcessPoolExecutor
from dataclasses import replace
import json,sys,inspect,multiprocessing,hashlib
import pandas as pd
import databento as db
import first_fill as e
ROOT=Path('/home/hoang/fleet-1017/out/day32-first-cross-20261031')
WP=Path('/home/hoang/fleet-1017/out/orb-ema9-20261022/eth-1m.dbn.zst')
W=None

def init():
    global W
    W=defaultdict(list);s=db.DBNStore.from_file(WP)
    for r in s:W[r.instrument_id].append((r.ts_event,r.close/250000000))
    s.reader.close()

def job(arg):
    plan,ledger=arg;iid=ledger['instrument_id'];opened=plan['open_ns'];end=plan['end_ns']
    data=W[iid]
    frame=pd.DataFrame(data,columns=['ts','close']).set_index(pd.to_datetime([t for t,p in data],utc=True))
    series=frame.loc[frame['ts']+60*10**9<=opened,'close'].resample('2min',origin='epoch',closed='left',label='right').last().dropna().iloc[-1000:]
    assert len(series)==1000 and series.index[-1].value<=opened
    warm=[int(v) for v in series.values]
    ss=[float(series.ewm(span=n,adjust=False).mean().iloc[-1]) for n in (9,34)]
    errors_seed=[abs(float(e.seed(warm,n))-ss[j]) for j,n in enumerate((9,34))]
    src=ROOT/'included-download'/plan['day']/'ticks.dbn';store=db.DBNStore.from_file(src)
    ticks=[];raw_closes={}
    for row,r in enumerate(store):
        if r.instrument_id!=iid or not opened<=r.ts_event<end:continue
        t=e.Tick(row,r.ts_event,r.ts_recv,r.price//250000000,(r.ts_event,r.sequence),True)
        raw_closes[(r.ts_event//e.SPAN+1)*e.SPAN]=t.price
        # Keep full source for independent bar close audit, only entry prefix for EMA.
        if row<=ledger['entry_row']:ticks.append(t)
    store.reader.close()
    ticks=[replace(t,last_in_event=(i+1==len(ticks) or t.event_key!=ticks[i+1].event_key)) for i,t in enumerate(ticks)]
    trace=[]
    source,start=inspect.getsourcelines(e._simulate)
    line=start+next(i for i,s in enumerate(source) if s.strip()=='diff = cur9-cur34')
    def hook(fr,event,arg):
        if fr.f_code is e._simulate.__code__ and event=='line' and fr.f_lineno==line:
            v=fr.f_locals;t=v['tick'];trace.append((t.row,t.event_ns,t.price,float(v['cur9']),float(v['cur34'])))
        return hook
    sys.settrace(hook)
    try:r=e.simulate(ticks,opened,end,warm,15)
    finally:sys.settrace(None)
    assert r['entry_row']==ledger['entry_row'] and r['entry_ticks']==ledger['entry_ticks']
    # Independent resampling of the decision-event price stream.
    tdf=pd.DataFrame(trace,columns=['row','ts','price','actual9','actual34'])
    tdf['bucket']=((tdf.ts-opened)//e.SPAN).astype('int64')
    closes=tdf.groupby('bucket',sort=True).price.last()
    baseline={}
    for n in (9,34):
        joined=pd.Series(warm+closes.tolist(),dtype='float64').ewm(span=n,adjust=False).mean()
        before=[joined.iloc[999]]+joined.iloc[1000:-1].tolist()
        baseline[n]=dict(zip(closes.index.tolist(),before))
    errors=[];crosses=[];prev=ss[0]-ss[1]
    for t in tdf.itertuples():
        expected=[baseline[n][t.bucket]*(1-2/(n+1))+t.price*2/(n+1) for n in (9,34)]
        errors.append(max(abs(t.actual9-expected[0]),abs(t.actual34-expected[1])))
        diff=expected[0]-expected[1]
        side=1 if prev<=0<diff else -1 if prev>=0>diff else 0
        if side:crosses.append({'row':t.row,'event_ns':t.ts,'side':side})
        prev=diff
    assert crosses==ledger['crosses'],(plan['day'],'crossover mismatch')
    maximum=max(errors,default=0);assert maximum<1e-7 and max(errors_seed)<1e-7
    full=frame.loc[(frame.ts>=opened)&(frame.ts<end),'close'].resample('2min',origin='epoch',closed='left',label='right').last().dropna()
    diffs=[{'end_ns':ts.value,'tick_close':raw_closes.get(ts.value),'ohlcv_close':v} for ts,v in full.items() if raw_closes.get(ts.value)!=v]
    result={'day':plan['day'],'ema_decisions_checked':len(trace),'max_error_ticks':maximum,'seed_error_ticks':max(errors_seed),'cross_count':len(crosses),'crosses_exact':True,'raw_2m_bars':len(raw_closes),'ohlcv_2m_bars':len(full),'bar_close_differences':len(diffs),'difference_samples':diffs[:5],'missing_2m_buckets':465-len(raw_closes),'seed_initial_error_factor_ema34':(33/35)**999}
    return result

def main():
    plans={r['day']:r for r in json.loads((ROOT/'plan.json').read_text())}
    rows=[r for r in json.loads((ROOT/'run-first-fill-v1/ledger.json').read_text()) if r['stop_points']==15]
    out=ROOT/'ema-specific-audit';out.mkdir(exist_ok=False)
    results=[]
    with ProcessPoolExecutor(max_workers=3,initializer=init,mp_context=multiprocessing.get_context('spawn')) as pool:
        for r in pool.map(job,[(plans[r['day']],r) for r in rows]):
            results.append(r);print(r['day'],r['ema_decisions_checked'],r['bar_close_differences'],flush=True)
    final={'sessions':len(results),'decision_ema_pairs_checked':sum(r['ema_decisions_checked'] for r in results),'max_error_ticks':max(r['max_error_ticks'] for r in results),'crosses_exact_all':all(r['crosses_exact'] for r in results),'bar_close_differences':sum(r['bar_close_differences'] for r in results),'raw_bar_count':sum(r['raw_2m_bars'] for r in results),'sessions_with_bar_differences':sum(r['bar_close_differences']>0 for r in results),'missing_buckets':sum(r['missing_2m_buckets'] for r in results),'pandas_version':pd.__version__,'engine_sha256':hashlib.sha256(Path(e.__file__).read_bytes()).hexdigest(),'rows':results}
    with (out/'audit.json').open('x') as f:json.dump(final,f,indent=2)
    print(json.dumps({k:v for k,v in final.items() if k!='rows'}),flush=True)
if __name__=='__main__':main()
