"""Recompute downstream exit policies on all changed economic input days."""
import sys,json,importlib
from pathlib import Path
sys.path.append('/home/hoang/vl-1017-databento-ready/docs/superpowers/research/2026-10-20-databento-ready')
import tick_orb as e
ROOT=Path('/home/hoang/fleet-1017/out');OUT=ROOT/'orb-clock-recheck-20261031'
variants=[('ema_rr3',()),('ema_box_be',(2,)),('ema_box_be',(3,)),('ema_halfbox_lock',(2,))]
variants += [('ema_box_lock',(r,l)) for r in (2,3) for l in (5,10,15)]
variants += [('ema_box_lock2025',(2,l)) for l in (20,25)]
variants += [('ema_box_lock_rr5',(5,5))]

def main():
    rows=json.loads((OUT/'rows.json').read_text())
    base={(r['day'],r['orb'],r['clock']):r for r in rows if r['variant']=='SL20_TP20'}
    keys=['outcome','orb_high','orb_low','entry_row','entry_points','side']
    changed=[]
    for day,m,clock in base:
        if clock=='event' and any(base[(day,m,'event')].get(k)!=base[(day,m,'receipt')].get(k) for k in keys):changed.append((day,m))
    result=[]
    for day in sorted({d for d,m in changed}):
        c,ticks=e.read_day(ROOT/'databento-mnq-20261020-data'/day,day)
        assert not c['reason'];_,o,z=e.boundaries(day)
        for d,m in changed:
            if d!=day:continue
            for name,args in variants:
                mod=importlib.import_module(name);pair={}
                for clock in ['event','receipt']:
                    b={k:str(v) for k,v in base[(day,m,clock)].items()}
                    pair[clock]=mod.simulate(ticks,b,z,*args)
                result.append(dict(day=day,orb=m,engine=name,args=args,**pair))
        print(day,'downstream checked',flush=True)
    with (OUT/'downstream.json').open('x') as f:json.dump(dict(changed_inputs=changed,rows=result),f,indent=2)
    print('Changed economic input day/timeframes',len(changed),'variant pairs',len(result))
if __name__=='__main__':main()
