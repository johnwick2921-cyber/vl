"""Second first-passage implementation + artifact reconciliation. Offline only."""
import argparse
from collections import Counter
import csv
import json
from pathlib import Path

import tick_orb as e


def first_passage(ticks, opened, closed, minutes, through):
    end = opened + minutes*e.MINUTE
    formation = [x for x in ticks if opened <= x[1] < end]
    if not formation or any(x[2] >= end for x in formation):
        return {'outcome':'EXCLUDED'}
    prices = [x[4] for x in formation]
    top, bottom = max(prices), min(prices)
    width = top-bottom
    if width == 0: return {'outcome':'EXCLUDED'}
    future = [x for x in ticks if end <= x[1] < closed]
    entries = [(i,x) for i,x in enumerate(future) if x[4] > top or x[4] < bottom]
    if not entries: return {'outcome':'NO_ENTRY'}
    index, trade = entries[0]
    sign = 1 if trade[4] > top else -1
    after = future[index+1:]
    gains = [(x[4]-trade[4])*sign for x in after]
    stops = [i for i,g in enumerate(gains) if g <= -80]
    targets = [i for i,g in enumerate(gains) if g >= width+int(through)]
    si, ti = min(stops, default=len(after)), min(targets, default=len(after))
    result = dict(entry_row=trade[0], entry_points=trade[4]/4,
                  side='LONG' if sign==1 else 'SHORT',
                  stop_points=(trade[4]-sign*80)/4,
                  target_points=(trade[4]+sign*width)/4,
                  orb_high=top/4,orb_low=bottom/4,width_points=width/4)
    if si == ti: return dict(result,outcome='OPEN_AT_CUTOFF')
    ix = min(si,ti)
    outcome = 'SL' if si < ti else 'TP'
    pnl = gains[ix]/4 if outcome=='SL' else width/4
    mfe = max([0,*gains[:ix+1]])/4
    return dict(result,outcome=outcome,exit_row=after[ix][0],pnl_points=pnl,
                net_2pt_scenario=pnl-2,mfe_points=mfe,
                comeback_stop=(outcome=='SL' and mfe>0))


def run(root, results):
    with (results/'sessions.csv').open() as f: census=list(csv.DictReader(f))
    with (results/'trades.csv').open() as f: rows=list(csv.DictReader(f))
    summary=json.loads((results/'summary.json').read_text())
    e.p.require(summary['engine_sha256']==e.p.digest(Path(e.__file__)),
                'Engine changed after replay; regenerate in a new output directory')
    expected=[x['day'] for x in e.p.make_plan(summary['start'],summary['end_exclusive'])['days']]
    e.p.require([x['day'] for x in census]==expected,'Session census/date mismatch')
    keys=[(x['day'],x['orb_minutes'],x['target_rule'],x['ordering']) for x in rows]
    e.p.require(len(keys)==len(set(keys)),'Duplicate trade key')
    checked=0
    for c in census:
        day=c['day']
        actual,ticks=e.read_day(root/day,day)
        e.p.require(actual['file_sha256']==c['file_sha256'],'Input changed')
        e.p.require(actual['contract']==c['contract'] and actual['reason']==c['reason'],
                    'Census replay differs')
        daily=[x for x in rows if x['day']==day]
        if c['reason']:
            e.p.require(not daily,'Excluded day has trades')
            continue
        e.p.require(len(daily)==12,'Missing variant')
        _,opened,closed=e.boundaries(day)
        for r in daily:
            if r['ordering']!='provider_record_order': continue
            wanted=first_passage(ticks,opened,closed,int(r['orb_minutes']),
                                 r['target_rule']=='through_1_tick')
            for k,v in wanted.items():
                observed=r[k]
                if isinstance(v,bool): same=observed==str(v)
                elif isinstance(v,(int,float)): same=float(observed)==v
                else: same=observed==v
                e.p.require(same,f'{day} {r["orb_minutes"]} {r["target_rule"]}: {k} differs')
            checked+=1
        print('independent replay',day,flush=True)
    for s in summary['summaries']:
        group=[r for r in rows if int(r['orb_minutes'])==s['orb_minutes']
               and r['target_rule']==s['target_rule'] and r['ordering']==s['ordering']]
        counts=dict(Counter(r['outcome'] for r in group))
        e.p.require(counts==s['counts'] and len(group)==s['sessions'],'Summary count mismatch')
        n=sum(counts.get(k,0) for k in ('TP','SL','OPEN_AT_CUTOFF','UNRESOLVED'))
        e.p.require(n==s['entry_or_unresolved'],'Denominator mismatch')
        for kind,field in [('TP','tp_pct_of_entries'),('SL','sl_pct_of_entries'),
                           ('UNRESOLVED','unresolved_pct')]:
            value=100*counts.get(kind,0)/n if n else None
            e.p.require(value==s[field],'Percentage mismatch')
        closed=[r for r in group if r['outcome'] in ('TP','SL')]
        gross=sum(float(r['pnl_points']) for r in closed)
        e.p.require(gross==s['closed_only_gross_points'],'Gross mismatch')
        e.p.require(gross-2*len(closed)==s['closed_only_net_2pt_scenario'],'Cost mismatch')
    audit=dict(status='PASS_COMPUTATIONAL_CHECKS_WITH_CAVEATS',
               independent_first_passage_rows=checked,source_files=len(census),
               engine_sha256=e.p.digest(Path(e.__file__)),
               auditor_sha256=e.p.digest(Path(__file__)),
               artifacts={n:e.p.digest(results/n) for n in ('sessions.csv','trades.csv','summary.json')},
               limitation='Independent first-passage algorithm shares decoder and coverage routine; '
                          'not independent vendor completeness verification or broker execution proof.')
    e.p.write_json(results/'audit.json',audit)
    return audit


if __name__=='__main__':
    p=argparse.ArgumentParser()
    p.add_argument('--root',type=Path,required=True)
    p.add_argument('--results',type=Path,required=True)
    a=p.parse_args();run(a.root,a.results)
