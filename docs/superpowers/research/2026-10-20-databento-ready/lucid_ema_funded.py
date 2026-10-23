"""Offline LucidFlex50K sensitivity replay. No brokerage/DB writes; cents/ticks integers.
Current Lucid rules applied counterfactually to old data, no payouts or resets.
"""
from pathlib import Path
import csv,json,multiprocessing,argparse
from concurrent.futures import ProcessPoolExecutor
import tick_orb as e
import ema_halfbox as plain
import ema_box_lock as locked
ROOT=Path('/home/hoang/fleet-1017/out')
SRC=ROOT/'orb-ema9-halfbox-20261022'
LOCKSRC=ROOT/'orb-ema9-box-lock-20261022'

def path(ticks,base,cutoff,lock=False,stress=False):
    if not base['entry_row']:return dict(outcome=base['outcome'],reason=base.get('reason',''),traded=False)
    idx=next(i for i,t in enumerate(ticks) if t[0]==int(base['entry_row']))
    assert ticks[idx][4]==int(float(base['entry_points'])*4)
    assert ticks[idx][1]==int(base['entry_event_ns']) and ticks[idx][2]==int(base['entry_recv_ns'])
    side=1 if base['side']=='LONG' else -1
    width=int((float(base['orb_high'])-float(base['orb_low']))*4)
    risk=(width+1)//2;slip=int(stress);entry=ticks[idx][4]+side*slip
    lows=[(ticks[idx][0],ticks[idx][1],-slip)];minimum=-slip
    arm=False;exit_t=None;outcome=None;gain=None
    for t in ticks[idx+1:]:
        if t[1]>=cutoff:break
        g=(t[4]-entry)*side
        if g<minimum:minimum=g;lows.append((t[0],t[1],g))
        stop=20 if arm else -risk
        if g<=stop:
            outcome='LOCK' if arm else 'SL';exit_t=t;gain=g-slip;break
        if g>=width+slip:
            outcome='TP';exit_t=t;gain=width;break
        if lock and not arm and g>=max(risk,21):arm=True
    if exit_t is None:
        # Mark-to-market forced-close proxy: last observable trade BEFORE15:00.
        eligible=[t for t in ticks[idx:] if t[1]<cutoff]
        exit_t=eligible[-1];outcome='CUTOFF_PROXY';gain=(exit_t[4]-entry)*side-slip
    if gain<minimum:minimum=gain;lows.append((exit_t[0],exit_t[1],gain))
    # Independent index-search oracle for exits with integer doubled-thresholds.
    future=[t for t in ticks[idx+1:] if t[1]<cutoff];n=len(future)
    gs=[(t[4]-entry)*side for t in future]
    si=next((i for i,g in enumerate(gs) if g<=-risk),n)
    ti=next((i for i,g in enumerate(gs) if g>=width+slip),n)
    ai=next((i for i,g in enumerate(gs) if g>=max(risk,21)),n) if lock else n
    li=next((i for i in range(ai+1,n) if gs[i]<=20),n) if ai<min(si,ti) else n
    first=min(si,ti,li)
    expected='CUTOFF_PROXY' if first==n else 'TP' if first==ti else 'LOCK' if first==li else 'SL'
    assert outcome==expected
    if first<n:assert exit_t[0]==future[first][0]
    return dict(traded=True,outcome=outcome,entry_row=ticks[idx][0],entry_event_ns=ticks[idx][1],
                exit_row=exit_t[0],exit_event_ns=exit_t[1],entry_ticks=entry,risk_ticks=risk,
                gain_ticks=gain,min_gain_ticks=minimum,record_lows=lows)

def mll_hit(equity,floor):
    return equity<=floor

def account(paths,qty,fee_rt_cents=100):
    assert qty in (3,6) and fee_rt_cents>=0
    balance=5000000;floor=4800000;peak=balance;dd=0;ledger=[];breach=None;qualified=0;lockday=None
    for p in paths:
        if not p['traded']:continue
        start=balance;fee=qty*fee_rt_cents;cash=start-fee;breach_event=None
        # Reserve complete round-trip fee at entry; conservative intraday accounting.
        for row,ts,g in p['record_lows']:
            if mll_hit(cash+g*50*qty,floor):breach_event=(row,ts,g);break
        if mll_hit(cash,floor):breach_event=(p['entry_row'],p['entry_event_ns'],0)
        if breach_event:
            row,ts,g=breach_event;balance=cash+g*50*qty;outcome='MLL_BREACH';low=balance
        else:
            balance=cash+p['gain_ticks']*50*qty;outcome=p['outcome'];low=cash+p['min_gain_ticks']*50*qty
        dd=max(dd,peak-low)
        pnl=balance-start;qualified+=pnl>=15000
        prior_floor=floor
        if not breach_event:
            peak=max(peak,balance);floor=min(5010000,max(floor,peak-200000))
            if floor==5010000 and lockday is None:lockday=p['day']
        ledger.append(dict(day=p['day'],qty=qty,outcome=outcome,start_cents=start,fee_cents=fee,pnl_cents=pnl,
            end_cents=balance,low_equity_cents=low,floor_before_cents=prior_floor,floor_after_cents=floor,
            exit_row=breach_event[0] if breach_event else p['exit_row'],exit_event_ns=breach_event[1] if breach_event else p['exit_event_ns']))
        if breach_event:breach=p['day'];break
    assert balance==5000000+sum(r['pnl_cents'] for r in ledger)
    return dict(qty=qty,status='BREACHED_ON_VALID_SAMPLE' if breach else 'NO_BREACH_ON_VALID_SAMPLE',breach_day=breach,
        trades=len(ledger),ending_balance_cents=balance,net_cents=balance-5000000,qualified_150_days=qualified,
        max_EOD_peak_to_intraday_trough_cents=dd,final_floor_cents=floor,lock_day=lockday,ledger=ledger)

def day_job(args):
    root,base,oldlock=args;day=base['day'];c,ticks=e.read_day(Path(root)/day,day);assert not c['reason']
    _,o,z=e.boundaries(day)
    # Exercise existing production research call sites and compare stored outputs.
    v=plain.simulate(ticks,base,z)
    for k in ['outcome','entry_row','exit_row']:
        assert str(v[k])==str(base[k]),(day,k)
    v2=locked.simulate(ticks,base,z,2,5)
    for k in ['outcome','entry_row','exit_row']:
        assert str(v2[k])==str(oldlock[k]),(day,k)
    if base['entry_row']:
        f=[t[4] for t in ticks if o<=t[1]<o+2*e.MINUTE]
        assert max(f)/4==float(base['orb_high']) and min(f)/4==float(base['orb_low'])
    out=[]
    for lock in (False,True):
        for stress in (False,True):
            r=path(ticks,base,z,lock,stress)
            if not stress and r['traded']:
                old=oldlock if lock else base
                assert r['outcome']==('CUTOFF_PROXY' if old['outcome']=='OPEN_AT_CUTOFF' else old['outcome'])
                if old['exit_row']:assert str(r['exit_row'])==old['exit_row']
            out.append(dict(day=day,method='lock5' if lock else 'fixed',execution='stress1tick' if stress else 'trade_print_proxy',**r))
    return out

def run(root,out):
    out.mkdir(exist_ok=False)
    for folder in [SRC,LOCKSRC]:
        audit=json.loads((folder/'audit.json').read_text())
        for name,h in audit['artifacts'].items():assert e.p.digest(folder/name)==h
    base=[r for r in csv.DictReader((SRC/'trades.csv').open()) if r['orb_minutes']=='2']
    locks={r['day']:r for r in csv.DictReader((LOCKSRC/'trades.csv').open()) if r['orb_minutes']=='2' and r['rr']=='2' and r['lock_points']=='5'}
    paths=[]
    with ProcessPoolExecutor(3,mp_context=multiprocessing.get_context('spawn')) as pool:
        for rr in pool.map(day_job,[(str(root),b,locks[b['day']]) for b in base]):
            paths.extend(rr);print(rr[0]['day'],'rechecked',flush=True)
    assert len(paths)==488
    e.p.write_json(out/'paths.json',paths)
    results=[];ledgers=[];rolling=[]
    for method in ['fixed','lock5']:
        for execution in ['trade_print_proxy','stress1tick']:
            pp=sorted([r for r in paths if r['method']==method and r['execution']==execution],key=lambda r:r['day'])
            trades=[r for r in pp if r['traded']];assert len(trades)==115
            for qty in (3,6):
                r=account(pp,qty);ledger=r.pop('ledger');ledgers.extend([dict(method=method,execution=execution,**x) for x in ledger])
                r.update(method=method,execution=execution,
                    hypothetical_all_trades_net_cents=sum(p['gain_ticks']*50*qty-100*qty for p in trades),
                    cutoff_proxy_trades=sum(p['outcome']=='CUTOFF_PROXY' for p in trades))
                results.append(r)
                for i in range(len(trades)-19):
                    a=account(trades[i:i+20],qty);a.pop('ledger')
                    rolling.append(dict(method=method,execution=execution,start=trades[i]['day'],end=trades[i+19]['day'],**a))
    e.write_csv(out/'accounts.csv',results);e.write_csv(out/'ledger.csv',ledgers);e.write_csv(out/'rolling20.csv',rolling)
    e.p.write_json(out/'audit.json',dict(engine_sha256=e.p.digest(Path(__file__)),source_hashes={str(f/'trades.csv'):e.p.digest(f/'trades.csv') for f in (SRC,LOCKSRC)},paths=len(paths),unique_entries=115,artifacts={n:e.p.digest(out/n) for n in ['paths.json','accounts.csv','ledger.csv','rolling20.csv']}))
    print(json.dumps(results,indent=2))
if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--root',type=Path,required=True);p.add_argument('--out',type=Path,required=True)
    a=p.parse_args();run(a.root,a.out)
