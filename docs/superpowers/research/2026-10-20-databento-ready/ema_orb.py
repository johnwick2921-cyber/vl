"""Offline EMA9 confirmed-close cross of opening range. No broker operations.
ETH warmup: last 300 available closed bars of SAME raw contract, no session reset.
EMA alpha=0.2, first-close seed; 300-bar seed error bound recorded.
RTH uses audited TBBO decoder; confirmed bars never fill their own close.
"""
import argparse,csv,json,multiprocessing
from datetime import datetime,timezone
from collections import Counter
from concurrent.futures import ProcessPoolExecutor
from decimal import Decimal,localcontext
from itertools import groupby
from pathlib import Path
import tick_orb as e

D=Decimal

def ema_seed(closes):
    assert len(closes)>=300,'Insufficient ETH warmup'
    with localcontext() as c:
        c.prec=40
        v=D(closes[0])
        for x in closes[1:]:v=(4*v+D(x))/5
        bound=D(max(closes)-min(closes))*D('0.8')**(len(closes)-1)
        assert bound<D('1e-20')
        return v,bound

def bars(ticks,opened,closed,m):
    span=m*e.MINUTE
    return [(opened+(k+1)*span,list(g)) for k,g in groupby(
        (t for t in ticks if opened<=t[1]<closed),lambda t:(t[1]-opened)//span)]

def simulate(ticks,opened,closed,m,tp,warm):
    r=dict(orb_minutes=m,tp_distance=tp,sl_distance=20,outcome='NO_SIGNAL',reason='',
           signal_row='',entry_row='',exit_row='',pnl_points='',observed_pnl_points='')
    bb=bars(ticks,opened,closed,m)
    if not bb or bb[0][0]!=opened+m*e.MINUTE:return dict(r,outcome='EXCLUDED',reason='missing_orb')
    opening=bb[0][1]
    if any(t[2]>=bb[0][0] for t in opening):return dict(r,outcome='EXCLUDED',reason='late_orb_receipt')
    hi=max(t[4] for t in opening);lo=min(t[4] for t in opening)
    if hi==lo:return dict(r,outcome='EXCLUDED',reason='zero_orb')
    v,bound=ema_seed(warm);r.update(orb_high=hi/4,orb_low=lo/4,warmup_bars=len(warm),seed_error_bound_ticks=str(bound))
    signal=None
    with localcontext() as c:
        c.prec=40
        for i,(end,b) in enumerate(bb):
            old=v;v=(4*v+D(b[-1][4]))/5
            if i==0 or end>=closed:continue
            side=1 if old<=hi and v>hi else -1 if old>=lo and v<lo else 0
            if side:
                # All candle records must be available before entry is eligible.
                ready=max(end,max(t[2] for _,z in bb[:i+1] for t in z))
                r.update(signal_row=b[-1][0],signal_end_ns=end,eligible_ns=ready,
                         ema_previous_points=str(old/4),ema_signal_points=str(v/4),side='LONG' if side==1 else 'SHORT')
                signal=(side,ready,b[-1][0]);break
    if signal is None:return r
    side,ready,row=signal
    future=[t for t in ticks if ready<=t[1]<closed and t[0]>row]
    if not future:return dict(r,outcome='SIGNAL_NO_ENTRY')
    ent=future[0];entry=ent[4]
    r.update(entry_row=ent[0],entry_event_ns=ent[1],entry_recv_ns=ent[2],entry_points=entry/4,
             stop_points=(entry-side*80)/4,target_points=(entry+side*tp*4)/4)
    for t in future[1:]:
        gain=side*(t[4]-entry)
        if gain<=-80 or gain>=tp*4:
            sl=gain<=-80
            return dict(r,outcome='SL' if sl else 'TP',exit_row=t[0],exit_event_ns=t[1],
                        exit_recv_ns=t[2],pnl_points=-20 if sl else tp,
                        observed_pnl_points=gain/4 if sl else tp)
    return dict(r,outcome='OPEN_AT_CUTOFF',mark_pnl_points=side*(future[-1][4]-entry)/4)

def oracle(ticks,opened,closed,m,tp,warm):
    """Independent dictionary bar aggregation and weighted-sum EMA / first passage."""
    b={}
    for t in ticks:
        if opened<=t[1]<closed:b.setdefault((t[1]-opened)//(m*e.MINUTE),[]).append(t)
    if 0 not in b:return ('EXCLUDED','','','','')
    if any(t[2]>=opened+m*e.MINUTE for t in b[0]):return ('EXCLUDED','','','','')
    high=max(t[4] for t in b[0]);low=min(t[4] for t in b[0])
    if high==low:return ('EXCLUDED','','','','')
    closes=list(warm);previous=None;signal=None;recv=0
    for k,z in sorted(b.items()):
        closes.append(z[-1][4]);n=len(closes)
        value=round(closes[0]*(.8**(n-1))+sum(.2*x*(.8**(n-1-j)) for j,x in enumerate(closes[1:],1)),8)
        recv=max(recv,max(t[2] for t in z));end=opened+(k+1)*m*e.MINUTE
        if k>0 and end<closed:
            side=1 if previous<=high and value>high else -1 if previous>=low and value<low else 0
            if side:signal=(side,z[-1][0],max(recv,end));break
        previous=value
    if signal is None:return ('NO_SIGNAL','','','','')
    side,srow,ready=signal
    ix=next((i for i,t in enumerate(ticks) if t[0]>srow and ready<=t[1]<closed),None)
    if ix is None:return ('SIGNAL_NO_ENTRY',srow,'','','')
    en=ticks[ix];after=[t for t in ticks[ix+1:] if t[1]<closed]
    target=next((j for j,t in enumerate(after) if (t[4]-en[4])*side>=4*tp),len(after))
    stop=next((j for j,t in enumerate(after) if (t[4]-en[4])*side<=-80),len(after))
    j=min(target,stop)
    if j==len(after):return ('OPEN_AT_CUTOFF',srow,en[0],'','')
    return ('TP' if target<stop else 'SL',srow,en[0],after[j][0],tp if target<stop else -20)

def checked(*args):
    r=simulate(*args)
    key=tuple(r[k] for k in ('outcome','signal_row','entry_row','exit_row','pnl_points'))
    assert key==oracle(*args),(key,oracle(*args))
    return r

def load_warm(path):
    store=e.db.DBNStore.from_file(path);by={}
    try:
        assert str(store.metadata.schema)=='ohlcv-1m' and store.metadata.dataset==e.p.DATASET
        mappings=store.metadata.mappings
        for t in store:
            assert t.close>0 and t.close%e.UNIT==0
            by.setdefault(t.instrument_id,[]).append((t.ts_event,t.close//e.UNIT))
    finally:store.reader.close()
    # iid mapping is resolved per trading date by the audited tick decoder.
    return by

WARM=None
DEGRADED=set()

def init(path):
    global WARM,DEGRADED
    WARM=load_warm(path)
    DEGRADED={r["date"] for r in json.loads((Path(path).parent/"dataset-condition.json").read_text()) if r["condition"]!="available"}

def warm_for(iid,opened,m):
    span=m*e.MINUTE;b={}
    for ts,cl in WARM[iid]:
        end=(ts//span+1)*span
        if end>opened:break
        b[end]=cl
    chosen=sorted(b.items())[-300:]
    dates={datetime.fromtimestamp((end-1)/1e9,timezone.utc).date().isoformat() for end,_ in chosen}
    return [v for _,v in chosen],dates

def day_job(arg):
    root,day=arg;c,t=e.read_day(Path(root)/day,day);out=[]
    if not c['reason']:
        _,o,z=e.boundaries(day)
        for m in (1,2,5):
            warm,dates=warm_for(c['instrument_id'],o,m)
            bad=sorted((dates|{day})&DEGRADED)
            for tp in (20,30):
                if bad:
                    r=dict(orb_minutes=m,tp_distance=tp,sl_distance=20,outcome="EXCLUDED",reason="provider_degraded_dates:"+",".join(bad),signal_row="",entry_row="",exit_row="",pnl_points="",observed_pnl_points="")
                else:r=checked(t,o,z,m,tp,warm)
                out.append(dict(day=day,contract=c['contract'],**r))
    return c,out

def run(root,out):
    receipt=json.loads((out/'warmup-receipt.json').read_text())
    assert receipt['sha256']==e.p.digest(out/'eth-1m.dbn.zst')
    days=[d['day'] for d in e.p.make_plan('2025-10-20','2026-04-20')['days']]
    census=[];rows=[]
    with ProcessPoolExecutor(3,mp_context=multiprocessing.get_context('spawn'),initializer=init,initargs=(str(out/'eth-1m.dbn.zst'),)) as pool:
        for c,rr in pool.map(day_job,[(str(root),d) for d in days]):
            census.append(c);rows.extend(rr);print(c['day'],len(rr),c['reason'],flush=True)
    e.write_csv(out/'trades.csv',rows);e.write_csv(out/'sessions.csv',census)
    summary=[]
    for m in (1,2,5):
        for tp in (20,30):
            a=[r for r in rows if r['orb_minutes']==m and r['tp_distance']==tp];c=Counter(r['outcome'] for r in a)
            n=c['TP']+c['SL']+c['OPEN_AT_CUTOFF'];won=c['TP']*tp;lost=-20*c['SL']
            summary.append(dict(orb_minutes=m,target=tp,stop=20,entries=n,TP=c['TP'],SL=c['SL'],open=c['OPEN_AT_CUTOFF'],no_signal=c['NO_SIGNAL'],signal_no_entry=c['SIGNAL_NO_ENTRY'],excluded=c['EXCLUDED'],win_percent=100*c['TP']/n if n else 0,won_points=won,lost_points=lost,net_points=won+lost,observed_net_points=sum(r['observed_pnl_points'] for r in a if r['outcome'] in ('TP','SL')),max_day_win=tp if c['TP'] else 0,max_day_loss=-20 if c['SL'] else 0))
    e.write_csv(out/'summary.csv',summary)
    e.p.write_json(out/'audit.json',dict(engine_sha256=e.p.digest(Path(__file__)),decoder_sha256=e.p.digest(Path(e.__file__)),days=len(census),included_days=sum(not c['reason'] for c in census),oracle_checked_rows=len(rows),warmup_sha256=receipt['sha256'],artifacts={n:e.p.digest(out/n) for n in ('trades.csv','sessions.csv','summary.csv')}))
    print(json.dumps(summary),flush=True)

if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--root',type=Path,required=True);p.add_argument('--out',type=Path,required=True)
    a=p.parse_args();run(a.root,a.out)
