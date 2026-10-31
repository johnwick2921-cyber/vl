"""Contract-stamped overnight adapter; offline, exclusive-create outputs."""
import json,csv,hashlib,bisect,multiprocessing
from dataclasses import replace
from pathlib import Path
from collections import Counter,defaultdict
from concurrent.futures import ProcessPoolExecutor
from datetime import datetime,timezone
import databento as db
from first_fill_receipt import Tick,simulate,SPAN
ROOT=Path('/home/hoang/fleet-1017/out/day32-first-cross-20261031')
WPATH=Path('/home/hoang/fleet-1017/out/orb-ema9-20261022/eth-1m.dbn.zst')
UNIT=250000000
WARM=None
NAMES=None
BAD=None

def digest(path):
    h=hashlib.sha256()
    with path.open('rb') as f:
        for b in iter(lambda:f.read(1048576),b''):h.update(b)
    return h.hexdigest()

def init():
    global WARM,NAMES,BAD
    WARM=defaultdict(list);NAMES={}
    s=db.DBNStore.from_file(WPATH)
    assert str(s.metadata.schema)=='ohlcv-1m'
    for name,rows in s.metadata.mappings.items():
        for r in rows:NAMES[int(r['symbol'])]=name
    for r in s:
        assert r.close>0 and r.close%UNIT==0
        WARM[r.instrument_id].append((r.ts_event,r.close//UNIT,r.volume))
    s.reader.close()
    BAD={r['date'] for r in json.loads((WPATH.parent/'dataset-condition.json').read_text()) if r['condition']!='available'}

def job(plan):
    day=plan['day'];opened=plan['open_ns'];cutoff=plan['end_ns']
    d=ROOT/'included-download'/day
    census={'day':day,'reason':'','contract':'','instrument_id':None}
    if not (d/'receipt.json').exists():return dict(census,reason='NOT_DOWNLOADED_NONZERO_QUOTE'),[]
    receipt=json.loads((d/'receipt.json').read_text())
    if receipt['status']!='DOWNLOADED_NOT_YET_AUDITED':return dict(census,reason=receipt['status']),[]
    assert digest(d/'ticks.dbn')==receipt['sha256']
    # Only completed data before session open. Three calendar days spans weekend.
    scores={iid:sum(v for ts,c,v in rows if opened-3*86400*10**9<=ts and ts+60*10**9<=opened) for iid,rows in WARM.items()}
    iid=max(scores,key=lambda k:(scores[k],-k))
    census.update(contract=NAMES[iid],instrument_id=iid,prior_volume=scores[iid])
    if scores[iid]<=0:return dict(census,reason='NO_PRIOR_VOLUME'),[]
    bars={}
    for ts,cl,vol in WARM[iid]:
        end=(ts//SPAN+1)*SPAN
        if end>opened:break
        bars[end]=cl
    chosen=sorted(bars.items())[-1000:]
    census['warmup_bars']=len(chosen)
    if len(chosen)<1000:return dict(census,reason='INSUFFICIENT_WARMUP'),[]
    dates={datetime.fromtimestamp((end-1)/1e9,timezone.utc).date().isoformat() for end,c in chosen}
    dates.update(datetime.fromtimestamp(t/1e9,timezone.utc).date().isoformat() for t in (opened,cutoff-1))
    if dates&BAD:return dict(census,reason='DEGRADED_PROVIDER_DATE:'+','.join(sorted(dates&BAD))),[]
    s=db.DBNStore.from_file(d/'ticks.dbn')
    assert str(s.metadata.schema)=='tbbo'
    available={int(r['symbol']) for r in s.metadata.mappings.get(NAMES[iid],[])}
    assert iid in available
    ticks=[];sequence=[];prev=None;invalid=Counter();maxgap=0;duplicates=0
    for row,r in enumerate(s):
        if r.instrument_id!=iid:continue
        if not opened<=r.ts_event<cutoff:continue
        if r.price<=0 or r.price%UNIT or str(r.action)!='T':invalid['BAD_PRICE_OR_ACTION']+=1;continue
        t=Tick(row,r.ts_event,r.ts_recv,r.price//UNIT,(r.ts_event,r.sequence),True)
        if prev:
            if t.event_ns<prev.event_ns:invalid['EVENT_REGRESSION']+=1
            if t.recv_ns<prev.recv_ns:invalid['RECEIVE_REGRESSION']+=1
            maxgap=max(maxgap,t.event_ns-prev.event_ns)
            if (r.ts_event,r.sequence)==sequence[-1] and t.price!=prev.price:duplicates+=1
        if t.recv_ns<t.event_ns:invalid['RECEIVE_BEFORE_EVENT']+=1
        ticks.append(t);sequence.append((r.ts_event,r.sequence));prev=t
    s.reader.close()
    census.update(ticks=len(ticks),max_gap_seconds=maxgap/1e9,ambiguous_sequence_prices=duplicates,anomalies=dict(invalid))
    if not ticks:return dict(census,reason='NO_OVERNIGHT_TICKS'),[]
    census.update(first_delay_seconds=(ticks[0].event_ns-opened)/1e9,last_gap_seconds=(cutoff-ticks[-1].event_ns)/1e9)
    if invalid:return dict(census,reason='TIMESTAMP_OR_RECORD_ANOMALY'),[]
    # Same event/sequence can carry multiple trade records. Preserve them,
    # but amend orders and sample EMA only after the event's final record.
    ticks=[replace(t,last_in_event=(i+1==len(ticks) or t.event_key!=ticks[i+1].event_key)) for i,t in enumerate(ticks)]
    if census['first_delay_seconds']>60 or census['last_gap_seconds']>60 or maxgap>600*10**9:
        return dict(census,reason='COVERAGE_REVIEW_REQUIRED'),[]
    out=[]
    for stop in (15,16):
        r=simulate(ticks,opened,cutoff,[c for end,c in chosen],stop)
        r.update(day=day,contract=NAMES[iid],instrument_id=iid)
        out.append(r)
    assert (out[0]['entry_row'],out[0]['signal_row'])==(out[1]['entry_row'],out[1]['signal_row'])
    return census,out

def writecsv(path,rows):
    keys=list(dict.fromkeys(k for r in rows for k in r))
    with path.open('x',newline='') as f:
        w=csv.DictWriter(f,fieldnames=keys);w.writeheader();w.writerows(rows)

def main():
    out=ROOT/'run-first-fill-recv-v1';out.mkdir(exist_ok=False)
    expected=json.loads((WPATH.parent/'warmup-receipt.json').read_text())['sha256']
    assert digest(WPATH)==expected
    plan=json.loads((ROOT/'plan.json').read_text());census=[];ledger=[]
    with ProcessPoolExecutor(max_workers=3,initializer=init,mp_context=multiprocessing.get_context("spawn")) as pool:
        for c,rows in pool.map(job,plan):
            census.append(c);ledger.extend(rows)
            print(c['day'],c['reason'] or ','.join(r['outcome'] for r in rows),flush=True)
    writecsv(out/'sessions.csv',census);writecsv(out/'trades.csv',ledger)
    with (out/'ledger.json').open('x') as f:json.dump(ledger,f,indent=2)
    with (out/'census.json').open('x') as f:json.dump(census,f,indent=2)
    summaries=[]
    for stop in (15,16):
        rows=[r for r in ledger if r['stop_points']==stop];ct=Counter(r['outcome'] for r in rows)
        closed=[r for r in rows if r['pnl_points'] is not None]
        tp=ct['TP'];sl=ct['SL'];n=tp+sl+ct['OPEN_AT_CUTOFF']
        won=sum(max(0,r['pnl_points']) for r in closed);lost=sum(min(0,r['pnl_points']) for r in closed)
        summaries.append(dict(stop=stop,target=15,included_sessions=len(rows),entries=n,TP=tp,SL=sl,open=ct['OPEN_AT_CUTOFF'],win_percent_closed=100*tp/(tp+sl) if tp+sl else None,won_points=won,lost_points=lost,net_points=won+lost,max_loss_points=min((r['pnl_points'] for r in closed),default=None),outcomes=dict(ct)))
    with (out/'summary.json').open('x') as f:json.dump(summaries,f,indent=2)
    with (out/'provenance.json').open('x') as f:json.dump({'warmup_sha256':expected,'code':{str(p.name):digest(p) for p in Path(__file__).parent.glob('*.py')},'artifacts':{p.name:digest(p) for p in out.iterdir() if p.is_file()}},f,indent=2)
    print(json.dumps(summaries),flush=True)
if __name__=='__main__':main()
