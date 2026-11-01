"""Exploratory A/E losing-day diagnostics. Never uses future data in filters."""
import csv,json,math
from pathlib import Path
from datetime import datetime
from zoneinfo import ZoneInfo
from statistics import mean,median
from collections import Counter
ROOT=Path('/home/hoang/fleet-1017/out/orb-AE-one-year-20261031')
CT=ZoneInfo('America/Chicago')
def write(p,data):
 with p.open('x',newline='') as f:
  w=csv.DictWriter(f,fieldnames=list(data[0]));w.writeheader();w.writerows(data)
def stats(rs):
 c=[r for r in rs if r['points']!=''];ps=[float(r['points']) for r in c];eq=peak=dd=0;streak=mx=0
 for p in ps:
  eq+=p;peak=max(peak,eq);dd=max(dd,peak-eq);streak=streak+1 if p<0 else 0;mx=max(mx,streak)
 return dict(entries=len(rs),closed=len(c),open=len(rs)-len(c),wins=sum(p>0 for p in ps),losses=sum(p<0 for p in ps),win_pct=100*sum(p>0 for p in ps)/len(ps) if ps else '',gross=sum(ps),cost2=sum(ps)-2*len(ps),closed_DD=dd,loss_streak=mx)
def main():
 rows=list(csv.DictReader((ROOT/'trades.csv').open()));out=ROOT/'loss-patterns';out.mkdir(exist_ok=False)
 entered=[]
 for r in rows:
  if not r['entry_row']:continue
  d=datetime.fromtimestamp(int(r['entry_event_ns'])/1e9,CT);r['minute']=d.hour*60+d.minute
  r['weekday']=d.strftime('%A');r['width']=float(r['width_points']);r['entry_houston']=d.isoformat()
  r['width_bin']='<=50' if r['width']<=50 else '50-75' if r['width']<=75 else '75-100' if r['width']<=100 else '>100'
  r['time_bin']='<09:00' if r['minute']<540 else '09:00-09:30' if r['minute']<570 else '09:30-10:00' if r['minute']<600 else '>=10:00'
  r['extension']='';r['extension_bin']='n/a'
  if r['group']=='E':
   sign=1 if r['side']=='LONG' else -1;boundary=float(r['orb_high'] if sign==1 else r['orb_low'])
   r['extension']=sign*(float(r['entry_points'])-boundary)/r['width']
   x=r['extension'];r['extension_bin']='<=0.25box' if x<=.25 else '0.25-0.5box' if x<=.5 else '0.5-1box' if x<=1 else '>1box'
  r['duration_min']=(int(r['exit_event_ns'])-int(r['entry_event_ns']))/60e9 if r['exit_event_ns'] else ''
  entered.append(r)
 feature=[];profiles=[];filters=[];selected=[]
 periods=[('year',lambda r:True),('early',lambda r:r['day']<'2026-05-01'),('recent',lambda r:r['day']>='2026-05-01')]
 for g in ['A','E']:
  rr=[r for r in entered if r['group']==g]
  for period,pred in periods:
   sample=[r for r in rr if pred(r)]
   for feat in ['width_bin','time_bin','weekday','side','extension_bin']:
    if feat=='extension_bin' and g=='A':continue
    for value in sorted({r[feat] for r in sample}):feature.append(dict(strategy=g,period=period,feature=feat,value=value,**stats([r for r in sample if r[feat]==value])))
   for outcome in ['TP','SL']:
    a=[r for r in sample if r['outcome']==outcome]
    profiles.append(dict(strategy=g,period=period,outcome=outcome,n=len(a),mean_width=mean(r['width'] for r in a) if a else '',median_width=median(r['width'] for r in a) if a else '',median_minutes_after_open=median(r['minute']-510 for r in a) if a else '',median_duration=median(float(r['duration_min']) for r in a) if a else '',loss_or_win_points=sum(float(r['points']) for r in a),extension_mean=mean(r['extension'] for r in a) if a and g=='E' else ''))
  rules=[('baseline',lambda r:True),('width<=50',lambda r:r['width']<=50),('width<=75',lambda r:r['width']<=75),('width<=100',lambda r:r['width']<=100),('entry<09:00',lambda r:r['minute']<540),('entry<09:30',lambda r:r['minute']<570),('entry<10:00',lambda r:r['minute']<600)]
  if g=='E':rules += [('extension<=0.25box',lambda r:r['extension']<=.25),('extension<=0.5box',lambda r:r['extension']<=.5),('extension<=1box',lambda r:r['extension']<=1)]
  for name,rule in rules:
   for period,pred in periods:filters.append(dict(strategy=g,rule=name,period=period,**stats([r for r in rr if pred(r) and rule(r)])))
  elig=[r for r in filters if r['strategy']==g and r['period']=='early' and r['closed']>=50]
  best=max(elig,key=lambda r:r['cost2'])
  selected.append(dict(strategy=g,selected_on_early_cost2=best['rule'],early=best,recent=next(r for r in filters if r['strategy']==g and r['period']=='recent' and r['rule']==best['rule'])))
 by={(r['group'],r['day']):r for r in entered if r['points']!=''}
 joint=[]
 for day in sorted({r['day'] for r in entered}):
  a=by.get(('A',day));e=by.get(('E',day))
  if a and e:joint.append(dict(day=day,A=float(a['points']),E=float(e['points']),both_loss=float(a['points'])<0 and float(e['points'])<0,same_side=a['side']==e['side']))
 write(out/'feature-buckets.csv',feature);write(out/'win-loss-profiles.csv',profiles);write(out/'filter-sensitivity.csv',filters);write(out/'joint-days.csv',joint)
 # Export all known-at-entry features for audit.
 keys=['day','group','outcome','points','entry_houston','weekday','side','width','time_bin','extension','duration_min']
 write(out/'entry-features.csv',[{k:r[k] for k in keys} for r in entered])
 with (out/'selection.json').open('x') as f:json.dump(dict(note='Exploratory only. Recent sample already inspected; not pristine holdout. Cost2 is sensitivity, not measured commission. One filter at a time, no threshold optimization. Minimum50 early closed trades. No live changes.',selected=selected,joint_closed_days=len(joint),both_loss=sum(r['both_loss'] for r in joint)),f,indent=2)
 print(json.dumps(selected,indent=2));print('joint',len(joint),'both loss',sum(r['both_loss'] for r in joint))
 print('RECENT PROFILES',json.dumps([r for r in profiles if r['period']=='recent'],indent=2))
 print('RECENT FILTERS',json.dumps([r for r in filters if r['period']=='recent'],indent=2))
if __name__=='__main__':main()
