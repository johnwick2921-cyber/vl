"""Owner-authorized six-month ORB stop comparison. Offline research only.
Shares the audited DBN decoder, not the new state machine and first-passage oracle.
Primary accounting: exact planned stop/target. Observed crossing PnL separate.
Half-box nominal stop can be half-tick; outward tick-rounded accounting separate.
"""
import argparse
from collections import Counter
from concurrent.futures import ProcessPoolExecutor
import csv
import json
from pathlib import Path
import tick_orb as e

MODES = ('box', 'half_box', '20_to_BE')

def simulate(ticks, opened, closed, minutes, mode):
    assert mode in MODES
    end=opened+minutes*e.MINUTE
    opening=[t for t in ticks if opened<=t[1]<end]
    r=dict(orb_minutes=minutes,mode=mode,outcome='NO_ENTRY',reason='',be_row='',
           entry_row='',exit_row='',pnl_planned_points='',pnl_observed_points='',
           pnl_tick_rounded_points='')
    if not opening:return dict(r,outcome='EXCLUDED',reason='missing_opening_range')
    if any(t[2]>=end for t in opening):
        return dict(r,outcome='EXCLUDED',reason='opening_range_record_received_after_decision')
    high=max(t[4] for t in opening);low=min(t[4] for t in opening);width=high-low
    if width<=0:return dict(r,outcome='EXCLUDED',reason='zero_range')
    risk=width if mode=='box' else width/2 if mode=='half_box' else 80
    r.update(width_points=width/4,risk_points=risk/4)
    entry=None;side=0;armed=False
    for t in ticks:
        if not end<=t[1]<closed:continue
        price=t[4]
        if entry is None:
            if price>high:side=1
            elif price<low:side=-1
            else:continue
            entry=price
            r.update(entry_row=t[0],entry_event_ns=t[1],entry_recv_ns=t[2],
                     entry_points=price/4,side='LONG' if side==1 else 'SHORT',
                     target_points=(entry+side*width)/4,stop_nominal_points=(entry-side*risk)/4)
            continue
        gain=(price-entry)*side
        # Bracket active after entry; BE active only after the activation print.
        stop=0 if armed else -risk
        if gain<=stop or gain>=width:
            outcome=('BE' if armed else 'SL') if gain<=stop else 'TP'
            planned=(width if outcome=='TP' else stop)/4
            rounded=(width if outcome=='TP' else 0 if outcome=='BE' else -((width+1)//2 if mode=='half_box' else risk))/4
            return dict(r,outcome=outcome,exit_row=t[0],exit_event_ns=t[1],exit_recv_ns=t[2],
                        exit_observed_points=price/4,pnl_planned_points=planned,
                        pnl_observed_points=(width if outcome=='TP' else gain)/4,
                        pnl_tick_rounded_points=rounded)
        if mode=='20_to_BE' and not armed and gain>=80:
            armed=True;r['be_row']=t[0]
    return dict(r,outcome='OPEN_AT_CUTOFF') if entry is not None else r


def oracle(ticks,opened,closed,minutes,mode):
    """Independent vector first-crossing indices; no call to simulate."""
    end=opened+minutes*e.MINUTE
    formation=[x for x in ticks if opened<=x[1]<end]
    if not formation or any(x[2]>=end for x in formation):return ('EXCLUDED','','','', '')
    hi=max(x[4] for x in formation);lo=min(x[4] for x in formation);w=hi-lo
    if not w:return ('EXCLUDED','','','', '')
    future=[x for x in ticks if end<=x[1]<closed]
    i=next((i for i,x in enumerate(future) if x[4]>hi or x[4]<lo),None)
    if i is None:return ('NO_ENTRY','','','', '')
    ent=future[i];sgn=1 if ent[4]>hi else -1;after=future[i+1:]
    g=[sgn*(x[4]-ent[4]) for x in after];n=len(g)
    distance={'box':w,'half_box':w/2,'20_to_BE':80}[mode]
    tp=next((j for j,v in enumerate(g) if v>=w),n)
    sl=next((j for j,v in enumerate(g) if v<=-distance),n)
    activation=next((j for j,v in enumerate(g) if v>=80),n) if mode=='20_to_BE' else n
    be=n;be_row=''
    if activation<min(tp,sl):
        be=next((j for j in range(activation+1,n) if g[j]<=0),n)
        be_row=after[activation][0]
    ix=min(tp,sl,be)
    if ix==n:return ('OPEN_AT_CUTOFF',ent[0],'','',be_row)
    outcome='TP' if ix==tp else 'BE' if ix==be else 'SL'
    pnl=w/4 if outcome=='TP' else 0 if outcome=='BE' else -distance/4
    return (outcome,ent[0],after[ix][0],pnl,be_row)


def checked(ticks,opened,closed,minutes,mode):
    r=simulate(ticks,opened,closed,minutes,mode)
    actual=(r['outcome'],r['entry_row'],r['exit_row'],r['pnl_planned_points'],r['be_row'])
    assert actual==oracle(ticks,opened,closed,minutes,mode),(actual,mode)
    return r


def day_job(args):
    root,day=args;c,ticks=e.read_day(Path(root)/day,day);rows=[]
    if not c['reason']:
        _,o,z=e.boundaries(day)
        for m in (1,2,5):
            for mode in MODES:
                rows.append(dict(day=day,contract=c['contract'],**checked(ticks,o,z,m,mode)))
    return c,rows


def run(root,out,baseline):
    days=[x['day'] for x in e.p.make_plan('2025-10-20','2026-04-20')['days']]
    assert all((root/d/'complete.json').exists() for d in days)
    old=list(csv.DictReader((baseline/'trades.csv').open()))
    old={ (r['day'],int(r['orb_minutes'])):r for r in old
          if r['ordering']=='provider_record_order' and r['target_rule']=='touch'}
    census=[];rows=[]
    out.mkdir(exist_ok=False)
    with ProcessPoolExecutor(max_workers=3) as pool:
        for c,rr in pool.map(day_job,[(str(root),d) for d in days]):
            census.append(c)
            for r in rr:
                prev=old[(r['day'],r['orb_minutes'])]
                assert str(r['entry_row'])==prev['entry_row'],'Entry changed from frozen baseline'
                assert (r['outcome']=='EXCLUDED')==(prev['outcome']=='EXCLUDED')
            rows.extend(rr);print(c['day'],'verified',flush=True)
    e.write_csv(out/'trades.csv',rows);e.write_csv(out/'sessions.csv',census)
    summary=[]
    for mode in MODES:
        for m in (1,2,5):
            a=[r for r in rows if r['mode']==mode and r['orb_minutes']==m]
            c=Counter(r['outcome'] for r in a);n=sum(c[x] for x in ('TP','SL','BE','OPEN_AT_CUTOFF'))
            closed=[r for r in a if r['outcome'] in ('TP','SL','BE')]
            summary.append(dict(mode=mode,orb_minutes=m,entries=n,TP=c['TP'],SL=c['SL'],BE=c['BE'],
                open=c['OPEN_AT_CUTOFF'],excluded=c['EXCLUDED'],target_pct=100*c['TP']/n if n else None,
                won_points=sum(r['pnl_planned_points'] for r in closed if r['outcome']=='TP'),
                lost_points=sum(r['pnl_planned_points'] for r in closed if r['outcome']=='SL'),
                net_planned_points=sum(r['pnl_planned_points'] for r in closed),
                net_tick_rounded_points=sum(r['pnl_tick_rounded_points'] for r in closed),
                net_observed_points=sum(r['pnl_observed_points'] for r in closed)))
    e.write_csv(out/'summary.csv',summary)
    e.p.write_json(out/'audit.json',dict(status='PASS_WITH_CAVEATS',independently_checked_rows=len(rows),
        input_days=len(days),included_days=sum(not c['reason'] for c in census),
        entry_matches_frozen_baseline=True,engine_sha256=e.p.digest(Path(__file__)),
        shared_decoder_sha256=e.p.digest(Path(e.__file__)),
        artifacts={n:e.p.digest(out/n) for n in ('trades.csv','sessions.csv','summary.csv')},
        limits='Independent first-passage math shares decoder. Trade-path simulation, not broker fills. '
        'Exact nominal planned stops; half-box half-ticks are idealized, outward rounded PnL separate. '
        'BE means stop moved to entry, not guaranteed zero loss under gaps. No costs.'))
    print(json.dumps(summary),flush=True)

if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--root',type=Path,required=True)
    p.add_argument('--out',type=Path,required=True);p.add_argument('--baseline',type=Path,required=True)
    a=p.parse_args();run(a.root,a.out,a.baseline)
