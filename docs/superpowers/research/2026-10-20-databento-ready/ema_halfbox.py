"""EMA ORB entries frozen from verified EMA9 run; half-box SL/full-box TP.
One trade/day; exclusive outputs; offline research only. Independent first-passage
exit oracle. Nominal half-ticks and executable outward-rounded stops separated.
"""
import csv,json,argparse,multiprocessing
from pathlib import Path
from collections import Counter
from concurrent.futures import ProcessPoolExecutor
import tick_orb as e

SOURCE=Path('/home/hoang/fleet-1017/out/orb-ema9-20261022')
KEEP=('day','contract','orb_minutes','reason','signal_row','entry_row','orb_high','orb_low','signal_end_ns','eligible_ns','ema_previous_points','ema_signal_points','side','entry_event_ns','entry_recv_ns','entry_points','warmup_bars','seed_error_bound_ticks')

def simulate(ticks,base,closed):
    r={k:base.get(k,'') for k in KEEP}
    r.update(outcome=base['outcome'],exit_row='',exit_event_ns='',exit_recv_ns='',pnl_points='',rounded_pnl_points='',observed_pnl_points='',stop_rule='half_box',target_rule='full_box')
    if not base['entry_row']:return r
    hi=int(float(base['orb_high'])*4);lo=int(float(base['orb_low'])*4);width=hi-lo
    assert width>0
    entry=int(float(base['entry_points'])*4);side=1 if base['side']=='LONG' else -1
    risk=width/2;rounded=(width+1)//2
    pos=next(i for i,t in enumerate(ticks) if t[0]==int(base['entry_row']))
    en=ticks[pos];assert en[4]==entry and en[1]==int(base['entry_event_ns']) and en[2]==int(base['entry_recv_ns'])
    assert en[1]>=int(base['eligible_ns'])>=int(base['signal_end_ns'])
    future=[t for t in ticks[pos+1:] if t[1]<closed]
    r.update(width_points=width/4,risk_points=risk/4,stop_points=(entry-side*risk)/4,
             stop_tick_rounded_points=(entry-side*rounded)/4,target_points=(entry+side*width)/4,outcome='OPEN_AT_CUTOFF')
    for t in future:
        gain=(t[4]-entry)*side
        if gain<=-risk or gain>=width:
            sl=gain<=-risk
            r.update(outcome='SL' if sl else 'TP',exit_row=t[0],exit_event_ns=t[1],exit_recv_ns=t[2],
                     pnl_points=-risk/4 if sl else width/4,rounded_pnl_points=-rounded/4 if sl else width/4,
                     observed_pnl_points=gain/4 if sl else width/4)
            break
    # Independent exit search in doubled tick units (no fractional thresholds).
    n=len(future)
    si=next((i for i,t in enumerate(future) if 2*side*(t[4]-entry)<=-width),n)
    ti=next((i for i,t in enumerate(future) if side*(t[4]-entry)>=width),n)
    j=min(si,ti)
    expected=('OPEN_AT_CUTOFF','','') if j==n else ('SL' if si<ti else 'TP',future[j][0],-width/8 if si<ti else width/4)
    assert (r['outcome'],r['exit_row'],r['pnl_points'])==expected
    if r['outcome']=='OPEN_AT_CUTOFF' and future:r['mark_pnl_points']=side*(future[-1][4]-entry)/4
    return r

def job(arg):
    root,day,base=arg;c,ticks=e.read_day(Path(root)/day,day)
    assert not c['reason']
    _,o,z=e.boundaries(day)
    out=[]
    for b in base:
        assert b['contract']==c['contract']
        if b['entry_row']:
            formation=[t[4] for t in ticks if o<=t[1]<o+int(b['orb_minutes'])*e.MINUTE]
            assert max(formation)/4==float(b['orb_high']) and min(formation)/4==float(b['orb_low'])
        out.append(simulate(ticks,b,z))
    return c,out

def run(root,out):
    audit=json.loads((SOURCE/'audit.json').read_text())
    for n,h in audit['artifacts'].items():assert e.p.digest(SOURCE/n)==h
    allrows=list(csv.DictReader((SOURCE/'trades.csv').open()))
    base=[r for r in allrows if r['tp_distance']=='20']
    assert len(base)==366
    days=sorted({r['day'] for r in base});out.mkdir(exist_ok=False);rows=[]
    with ProcessPoolExecutor(3,mp_context=multiprocessing.get_context('spawn')) as pool:
        for c,rr in pool.map(job,[(str(root),d,[r for r in base if r['day']==d]) for d in days]):
            rows.extend(rr);print(c['day'],'checked',flush=True)
    assert len(rows)==366
    e.write_csv(out/'trades.csv',rows)
    with (out/'sessions.csv').open('xb') as f:f.write((SOURCE/'sessions.csv').read_bytes())
    summary=[]
    for m in ('1','2','5'):
        rr=[r for r in rows if r['orb_minutes']==m];c=Counter(r['outcome'] for r in rr)
        closed=[r for r in rr if r['outcome'] in ('TP','SL')];n=c['TP']+c['SL']+c['OPEN_AT_CUTOFF']
        summary.append(dict(orb_minutes=m,entries=n,TP=c['TP'],SL=c['SL'],open=c['OPEN_AT_CUTOFF'],no_signal=c['NO_SIGNAL'],excluded=c['EXCLUDED'],win_percent=100*c['TP']/n if n else 0,
            won_points=sum(r['pnl_points'] for r in closed if r['outcome']=='TP'),lost_points=sum(r['pnl_points'] for r in closed if r['outcome']=='SL'),net_points=sum(r['pnl_points'] for r in closed),
            rounded_net_points=sum(r['rounded_pnl_points'] for r in closed),observed_net_points=sum(r['observed_pnl_points'] for r in closed),
            max_day_win=max([r['pnl_points'] for r in closed if r['outcome']=='TP'],default=0),max_day_loss=min([r['pnl_points'] for r in closed if r['outcome']=='SL'],default=0)))
    e.write_csv(out/'summary.csv',summary)
    e.p.write_json(out/'audit.json',dict(engine_sha256=e.p.digest(Path(__file__)),source_trade_sha256=e.p.digest(SOURCE/'trades.csv'),candidate_rows=len(rows),entries_checked=sum(bool(r['entry_row']) for r in rows),artifacts={n:e.p.digest(out/n) for n in ('trades.csv','summary.csv','sessions.csv')}))
    print(json.dumps(summary),flush=True)
if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--root',type=Path,required=True);p.add_argument('--out',type=Path,required=True)
    a=p.parse_args();run(a.root,a.out)
