"""EMA ORB entries frozen from verified EMA9 run; half-box SL, lock +5/+10/+15 points at +1R when legally placeable, TP 2R/3R.
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

def simulate(ticks,base,closed,multiple,lock_points):
    assert lock_points in (5,10,15)
    assert multiple in (2,3)
    r={k:base.get(k,'') for k in KEEP}
    r.update(rr=multiple,lock_points=lock_points,outcome=base['outcome'],exit_row='',exit_event_ns='',exit_recv_ns='',
        pnl_points='',rounded_pnl_points='',observed_pnl_points='',lock_row='',lock_event_ns='',
        stop_rule='half_box_then_profit_lock_at_1R_or_legal_threshold',target_rule=str(multiple)+'R')
    if not base['entry_row']:return r
    hi=int(float(base['orb_high'])*4);lo=int(float(base['orb_low'])*4);width=hi-lo
    assert width>0
    entry=int(float(base['entry_points'])*4);side=1 if base['side']=='LONG' else -1
    lock=lock_points*4
    risk=width/2;trigger=max(risk,lock+1);rounded=(width+1)//2;reward=multiple*width/2;rounded_reward=(multiple*width+1)//2
    pos=next(i for i,t in enumerate(ticks) if t[0]==int(base['entry_row']))
    en=ticks[pos];assert en[4]==entry and en[1]==int(base['entry_event_ns']) and en[2]==int(base['entry_recv_ns'])
    assert en[1]>=int(base['eligible_ns'])>=int(base['signal_end_ns'])
    future=[t for t in ticks[pos+1:] if t[1]<closed]
    r.update(width_points=width/4,risk_points=risk/4,stop_points=(entry-side*risk)/4,
        lock_trigger_points=(entry+side*trigger)/4,lock_stop_points=(entry+side*lock)/4,one_R_not_above_lock=risk<=lock,stop_tick_rounded_points=(entry-side*rounded)/4,
        target_points=(entry+side*reward)/4,target_tick_rounded_points=(entry+side*rounded_reward)/4,outcome='OPEN_AT_CUTOFF')
    armed=False
    for t in future:
        gain=(t[4]-entry)*side
        stop=lock if armed else -risk
        if gain<=stop or gain>=reward:
            outcome=('LOCK' if armed else 'SL') if gain<=stop else 'TP'
            r.update(outcome=outcome,exit_row=t[0],exit_event_ns=t[1],exit_recv_ns=t[2],
                pnl_points=reward/4 if outcome=='TP' else lock_points if outcome=='LOCK' else -risk/4,
                rounded_pnl_points=rounded_reward/4 if outcome=='TP' else lock_points if outcome=='LOCK' else -rounded/4,
                observed_pnl_points=rounded_reward/4 if outcome=='TP' else gain/4)
            break
        if not armed and gain>=trigger:
            armed=True;r.update(lock_row=t[0],lock_event_ns=t[1])
    # Independent doubled-tick first-passage oracle, no state-machine reuse.
    n=len(future)
    gains=[2*side*(t[4]-entry) for t in future]
    si=next((i for i,g in enumerate(gains) if g<=-width),n)
    ti=next((i for i,g in enumerate(gains) if g>=multiple*width),n)
    activation=next((i for i,g in enumerate(gains) if g>=width and g>=2*(lock+1)),n)
    bi=n;arow=''
    if activation<min(si,ti):
        arow=future[activation][0]
        bi=next((i for i in range(activation+1,n) if gains[i]<=2*lock),n)
    j=min(si,ti,bi)
    if j==n:expected=('OPEN_AT_CUTOFF','','',arow)
    else:
        outcome='TP' if ti==j else 'LOCK' if bi==j else 'SL'
        expected=(outcome,future[j][0],multiple*width/8 if outcome=='TP' else lock_points if outcome=='LOCK' else -width/8,arow)
    assert (r['outcome'],r['exit_row'],r['pnl_points'],r['lock_row'])==expected
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
        for multiple in (2,3):
            for lock_points in (5,10,15):out.append(simulate(ticks,b,z,multiple,lock_points))
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
    assert len(rows)==2196
    e.write_csv(out/'trades.csv',rows)
    with (out/'sessions.csv').open('xb') as f:f.write((SOURCE/'sessions.csv').read_bytes())
    summary=[]
    for lock_points,multiple,m in ((l,r,m) for l in (5,10,15) for r in (2,3) for m in ('1','2','5')):
        rr=[r for r in rows if r['orb_minutes']==m and r['rr']==multiple and r['lock_points']==lock_points];c=Counter(r['outcome'] for r in rr)
        closed=[r for r in rr if r['outcome'] in ('TP','SL','LOCK')];n=c['TP']+c['SL']+c['LOCK']+c['OPEN_AT_CUTOFF']
        summary.append(dict(orb_minutes=m,rr=multiple,lock_points=lock_points,small_R_days=sum(bool(r.get('one_R_not_above_lock')) for r in rr if r['entry_row']),entries=n,TP=c['TP'],SL=c['SL'],LOCK=c['LOCK'],open=c['OPEN_AT_CUTOFF'],no_signal=c['NO_SIGNAL'],excluded=c['EXCLUDED'],tp_percent=100*c['TP']/n if n else 0,win_percent=100*(c['TP']+c['LOCK'])/n if n else 0,
            won_points=sum(r['pnl_points'] for r in closed if r['outcome'] in ('TP','LOCK')),lost_points=sum(r['pnl_points'] for r in closed if r['outcome']=='SL'),net_points=sum(r['pnl_points'] for r in closed),
            rounded_net_points=sum(r['rounded_pnl_points'] for r in closed),observed_net_points=sum(r['observed_pnl_points'] for r in closed),
            max_day_win=max([r['pnl_points'] for r in closed if r['outcome'] in ('TP','LOCK')],default=0),max_day_loss=min([r['pnl_points'] for r in closed if r['outcome']=='SL'],default=0)))
    e.write_csv(out/'summary.csv',summary)
    e.p.write_json(out/'audit.json',dict(engine_sha256=e.p.digest(Path(__file__)),source_trade_sha256=e.p.digest(SOURCE/'trades.csv'),candidate_rows=len(rows),entries_checked=sum(bool(r['entry_row']) for r in rows),artifacts={n:e.p.digest(out/n) for n in ('trades.csv','summary.csv','sessions.csv')}))
    print(json.dumps(summary),flush=True)
if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--root',type=Path,required=True);p.add_argument('--out',type=Path,required=True)
    a=p.parse_args();run(a.root,a.out)
