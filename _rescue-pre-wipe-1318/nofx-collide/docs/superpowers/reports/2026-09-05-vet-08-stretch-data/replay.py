"""Scratch replay of 2026-09-02..09-04 under CURRENT rules (dev tip 2a66d91c == running 36648655 for every seam used here).
READ-ONLY: reads CSV exports of plans/levels/bars produced by q15/q27/q28/q29 and the bars table. Never touches the engine.
Rules encoded (path:line cited in the report):
 - arm only when plan authored arm.enabled=1 (armed_executor.go:290)            [Run A]
 - Run B additionally arms every `reclaim` scenario as a stop-entry (arm_kind.go:60, owner ruling 2026-09-04) with
   trigger = confirm.ref ± 2 ticks (entry_law.go:113), stop composed, target = target_chain[0]
 - ArmableCondition: fvg_entry|reject|breakdown_continue|breakup_continue|reclaim (armed.go:23); shadow: fvg_entry, breakout_retest (condition_status.go:27)
 - stop = max(authored, 1.5xATR5m floor, nearest seated level on risk side within 3xATR + 2 ticks) (arm_stop_anchor.go:71-131)
 - R:R at arm >= 2.0 (arm_rr=2.0 boot line), re-evaluated each 2-min cycle; a working arm whose gate flips is cancelled 'gate changed: rr' and may re-arm (armed_executor.go:430-460)
 - invalidation leg (arm path): a 5m close beyond the cited level by > noise (0.2xATR) since plan birth refuses the arm (entry_gate.go:226-250) [B approximation]
 - placement band 100t=25pts for limits; marketable wrong-side -> cancelled, TERMINAL for the version (class 25 law); stop-entry placed immediately when on the CORRECT side
   (this replay uses correct stop semantics; the live guard at armed_executor.go:940 is inverted — reported separately)
 - one_open_position: no fill while a position is open; other working arms cancelled at fill (class 27)
 - version end / dormant: working arms cancelled; positions ride to stop/target/flat
 - session flats ASIA 02:00 / LONDON 08:30 / NY 14:45 CT; SIM fills at the authorized price; same-bar stop+target -> stop first
 - reaper reads the broker book (class 79 fix) -> no silence reaping
 - daily-limit leg: inert (strategy a5b7662e guardrails_enabled=false) ; bias-arm warning: warn-only, counted
"""
import csv, json, sys, bisect, math, pathlib
repo=next(p for p in pathlib.Path(__file__).resolve().parents if (p/'go.mod').exists())
sys.path.insert(0,str(repo/'scripts'))
from arm_state import is_terminal_arm_state
# Replay-only synthetic states are not ledger lifecycle values.
def replay_arm_active(state):
    return not is_terminal_arm_state(state) and state != 'none' and state != 'refused'

sys.path.insert(0,'/home/hoang/nofx-analysis/vet-08-0905')
from common import *
TICK=0.25; BAND=25.0; RR_MIN=2.0; MULT=1.5; ANCHOR_MAX=3.0; CLR=2*TICK; NOISE_ATR=0.2; OFFSET=2*TICK
SESS={'ASIA':('17:00','02:00'),'LONDON':('02:00','08:30'),'NY':('08:30','14:45')}
def sess_window(trade_date, sess):
    s,e=SESS[sess]
    start=ct_ms(f'{trade_date} {s}')
    if sess=='ASIA':
        d=datetime.datetime.strptime(trade_date,'%Y-%m-%d')+datetime.timedelta(days=1); end=ct_ms(f'{d:%Y-%m-%d} {e}')
    else: end=ct_ms(f'{trade_date} {e}')
    return start,end
A=ATR(); B1=A.b1; B1T=[b[0] for b in B1]; B5=A.b5; B5T=[b[0] for b in B5]
def last_close(ms):
    i=bisect.bisect_right(B1T, ms-60000)-1
    return B1[i][4] if i>=0 else None
def closes5_between(a,b):
    i=bisect.bisect_left(B5T,a); j=bisect.bisect_right(B5T,b-300000)
    return [(B5[k][0],B5[k][4]) for k in range(i,j)]
# ---- load
scen=list(csv.DictReader(open('q15_scenarios.csv')))
levels={}
for r in csv.DictReader(open('q27_plan_levels.csv')):
    levels.setdefault((r['trade_date'],r['session'],int(r['version'])),[]).append((float(r['price']),r['label']))
vers={}
for r in csv.DictReader(open('q28_plan_versions.csv')):
    vers[(r['trade_date'],r['session'],int(r['version']))]=r
chain={}
for r in csv.DictReader(open('q31_arm_chain.csv')):
    chain[(r['trade_date'],r['session'],int(r['version']),r['sid'])]=dict(wc=r['wait_confirm']=='1', legs=json.loads(r['legs']) if r['legs'] else [], c2=(r['c2_rule'],float(r['c2_ref']) if r['c2_ref'] else None,r['c2_side']))
live_stop={}
for r in csv.DictReader(open('q32_live_stops.csv')):
    live_stop[(r['td'],r['session'],int(r['version']),r['scenario'],int(r['leg_index']))]=float(r['stop_px'])
def confirm_met(rule, ref, side, since, now):
    if not rule or not ref: return False
    rule=rule.lower(); side=(side or '').lower()
    if rule=='touch':
        i=bisect.bisect_left(B1T,since); j=bisect.bisect_right(B1T,now-60000)
        for k in range(i,j):
            ms,o,h,l,c,v=B1[k]
            if l<=ref<=h: return True
        return False
    need=2 if rule.startswith('2x') else 1
    if rule in('1x5m_close','2x5m_close','5m_close'):
        run=0
        for t,c in closes5_between(since, now):
            ok=(c>ref) if side=='above' else (c<ref)
            run=run+1 if ok else 0
            if run>=need: return True
        return False
    return False  # 1m_mss / time_hold not modelled
# dormant/rearm events
events=[]
import re
for line in open('q29_dormant_events.txt'):
    m=re.search(r'^(\d\d-\d\d \d\d:\d\d:\d\d).*plan (\d{4}-\d\d-\d\d) (ASIA|LONDON|NY) v(\d+) (DORMANT|REARMED)', line)
    if m:
        ts=ct_ms('2026-'+m.group(1)); events.append((ts,m.group(2),m.group(3),int(m.group(4)),m.group(5)))
events.sort()
def version_life(td,sess,v):
    r=vers[(td,sess,v)]; born=ct_ms(r['born_ct']); s0,s1=sess_window(td,sess)
    born=max(born,s0)  # arms are authored only inside the session window (auto_trader_session.go: active session resolves the plan)
    nxt=[ct_ms(x['born_ct']) for k,x in vers.items() if k[0]==td and k[1]==sess and k[2]>v]
    end=min([s1]+nxt)
    # dormant gaps
    gaps=[]; dorm=None
    for ts,d,s,vv,ev in events:
        if (d,s,vv)!=(td,sess,v) or ts<born or ts>end: continue
        if ev=='DORMANT': dorm=ts
        elif ev=='REARMED' and dorm: gaps.append((dorm,ts)); dorm=None
    if dorm: gaps.append((dorm,end))
    return born,end,gaps,s1
def in_gap(ms,gaps): return any(a<=ms<b for a,b in gaps)
def compose_stop(side, entry, authored, atr, lv):
    floor = entry-MULT*atr if side=='long' else entry+MULT*atr
    best=None
    for p,lab in lv:
        if side=='long' and p<entry and entry-p<=ANCHOR_MAX*atr:
            if best is None or entry-p<entry-best[0]: best=(p,lab)
        if side=='short' and p>entry and p-entry<=ANCHOR_MAX*atr:
            if best is None or p-entry<best[0]-entry: best=(p,lab)
    cands=[floor]; bound='atr_floor'
    if authored: cands.append(authored)
    if best: cands.append(best[0]-CLR if side=='long' else best[0]+CLR)
    stop=min(cands) if side=='long' else max(cands)
    if best and stop==(best[0]-CLR if side=='long' else best[0]+CLR): bound='anchor:'+best[1]
    elif authored and stop==authored: bound='authored'
    return stop,bound,(best is None)
def invalidated(side, level, born, now, atr):
    noise=NOISE_ATR*atr
    i=bisect.bisect_left(B1T,born); j=bisect.bisect_right(B1T,now-60000); touched_at=None
    for k in range(i,j):
        ms,o,h,l,c,v=B1[k]
        if l<=level<=h: touched_at=ms; break
    if touched_at is None: return False
    for t,c in closes5_between(touched_at, now):
        if side=='long' and c < level-noise: return True
        if side=='short' and c > level+noise: return True
    return False
def run(mode):
    rows=[]; positions=[]; open_pos=None
    # build arm candidates per version
    cands=[]; cands_extra=[]
    for s in scen:
        td,sess,v=s['trade_date'],s['session'],int(s['version'])
        if td<'2026-09-02' or (td=='2026-09-04' and sess=='ASIA'): continue
        if vers[(td,sess,v)]['lifecycle']=='no_trade': continue
        cond=s['cond']; side=s['dir']; en=(s['arm_en']=='1')
        tgt=None
        try: tgt=json.loads(s['targets'])[0]
        except: pass
        c=dict(td=td,sess=sess,v=v,sid=s['sid'],side=side,cond=cond,q=s['q'],bias=s['bias'],authored_arm=en,leg=0,
               c_rule=s['c_rule'],c_ref=float(s['c_ref']) if s['c_ref'] else None,c_side=s['c_side'],wc=False,chain=None)
        ch=chain.get((td,sess,v,s['sid']))
        if en:
            c.update(entry=float(s['arm_entry'] or 0), stop_a=float(s['arm_stop'] or 0) or None, target=float(s['arm_target'] or 0) or tgt, src='authored')
            if ch:
                c['wc']=ch['wc']; c['chain']=(s['c_rule'],c['c_ref'],s['c_side'])
                if len(ch['legs'])==2:
                    l0,l1=ch['legs']
                    c.update(entry=float(l0.get('entry') or 0), stop_a=float(l0.get('stop') or 0) or None, target=float(l0.get('target') or 0) or tgt, legs=2)
                    c2=dict(c); c2.update(leg=1, entry=float(l1.get('entry') or 0), stop_a=float(l1.get('stop') or 0) or None, target=float(l1.get('target') or 0) or tgt,
                             chain=ch['c2'] if ch['c2'][0] else (s['c_rule'],c['c_ref'],s['c_side']), kind_override=l1.get('kind') or 'limit')
                    if c2['entry'] and c2['target']: cands_extra.append(c2)
        elif mode=='B' and cond=='reclaim' and s['c_ref']:
            c.update(entry=float(s['c_ref']), stop_a=None, target=tgt, src='counterfactual-reclaim')
        else: continue
        if not c['entry'] or not c['target']: c['refuse']='no geometry'; rows.append(c); continue
        c['kind']='stop_entry' if cond=='reclaim' else 'limit'
        if cond in ('fvg_entry','breakout_retest'): c['refuse']='shadow (0C)'; rows.append(c); continue
        if cond not in ('fvg_entry','reject','breakdown_continue','breakup_continue','reclaim','sweep_reclaim'): c['refuse']='non-armable'; rows.append(c); continue
        cands.append(c)
    for c2 in cands_extra:
        c2['kind']=c2.pop('kind_override','limit'); cands.append(c2)
    # simulate per version in time order; positions global (one open at a time)
    cands.sort(key=lambda c:(ct_ms(vers[(c['td'],c['sess'],c['v'])]['born_ct']), c['sid']))
    # event-driven: iterate cycles over the union of version lives
    for c in cands:
        c['state']='none'; c['born'],c['end'],c['gaps'],c['flat']=version_life(c['td'],c['sess'],c['v'])
        c['placed_at']=None; c['filled_at']=None; c['replaces']=0; c['warn_bias']=(c['bias'] and c['side']!=c['bias'] and c['bias'] in('long','short'))
    t0=min(c['born'] for c in cands); t1=max(c['flat'] for c in cands)
    t=t0-(t0%120000)
    while t<=t1:
        # manage open position on the bar that just closed [t-60s, t)
        if open_pos:
            i=bisect.bisect_right(B1T, t-60000)-1
            if i>=0 and B1[i][0]>=open_pos['filled_at']:
                ms,o,h,l,cl,vv=B1[i]; p=open_pos
                ex=None
                if p['side']=='long':
                    if l<=p['stop']: ex=('stop',p['stop'])
                    elif h>=p['target']: ex=('target',p['target'])
                else:
                    if h>=p['stop']: ex=('stop',p['stop'])
                    elif l<=p['target']: ex=('target',p['target'])
                if not ex and ms+60000>=p['flat']: ex=('flat',cl)
                if ex:
                    p['exit_at']=ms+60000; p['exit_reason'],p['exit_px']=ex
                    p['pts']=(p['exit_px']-p['fill_px']) if p['side']=='long' else (p['fill_px']-p['exit_px'])
                    positions.append(p); open_pos=None
        price=last_close(t); atr=A.at(t)
        for c in cands:
            if (is_terminal_arm_state(c['state']) or c['state']=='refused'): continue
            if t<c['born'] or t>=c['end']:
                if t>=c['end'] and replay_arm_active(c['state']): c['state']='cancelled'; c['reason']='superseded/session end'
                continue
            if in_gap(t,c['gaps']):
                if replay_arm_active(c['state']): c['state']='cancelled'; c['reason']='plan dormant'
                continue
            if price is None or atr is None: continue
            lv=levels.get((c['td'],c['sess'],c['v']),[])
            if c['wc'] and c['chain'] and c['state']=='none':
                if not confirm_met(c['chain'][0], c['chain'][1], c['chain'][2], c['born'], t):
                    c['waiting']=True; continue
                c['confirm_met_at']=c.get('confirm_met_at') or t
            stop,bound,unanch=compose_stop(c['side'],c['entry'],c['stop_a'],atr,lv)
            ls=live_stop.get((c['td'],c['sess'],c['v'],c['sid'],c['leg']))
            if ls: stop,bound=ls,'live-ledger'
            risk=abs(c['entry']-stop); rew=abs(c['target']-c['entry']); rr=rew/risk if risk>0 else 0
            c['stop']=stop; c['bound']=bound; c['rr']=rr; c['atr']=atr
            if open_pos and c['state']!='working':
                continue  # one_open_position: no new arming while a position is open
            if rr<RR_MIN:
                if c['state']=='working': c['state']='none'; c['replaces']+=1; c['gate_cancels']=c.get('gate_cancels',0)+1
                c['last_refuse']='rr %.2f<2.0'%rr; continue
            if c['kind']=='limit' and c['state']=='none' and invalidated(c['side'], c['entry'], c['born'], t, atr):
                c['state']='refused'; c['reason']='invalidated (5m close beyond level)'; c['refused_at']=t; continue
            if c['state']=='none':
                c['state']='armed'; c['armed_at']=c.get('armed_at') or t
            if c['state']=='armed':
                if c['kind']=='limit':
                    if (c['side']=='long' and price<c['entry']) or (c['side']=='short' and price>c['entry']):
                        c['state']='cancelled'; c['reason']='marketable (price through entry) — never placed'; c['cancel_at']=t; continue
                    if abs(price-c['entry'])<=BAND: c['state']='working'; c['placed_at']=t
                else:
                    trig=c['entry']+OFFSET if c['side']=='long' else c['entry']-OFFSET; c['trigger']=trig
                    ok=(price<trig) if c['side']=='long' else (price>trig)
                    live_guard_refuses = (price<trig) if c['side']=='long' else (price>trig)  # inverted live predicate (armed_executor.go:940)
                    c['live_guard_would_refuse']=live_guard_refuses
                    if not ok: c['state']='cancelled'; c['reason']='trigger already traded through — never placed'; c['cancel_at']=t; continue
                    c['state']='working'; c['placed_at']=t
            if c['state']=='working' and open_pos is None:
                # scan the bar closing at t (the one just closed) for a fill
                i=bisect.bisect_right(B1T, t-60000)-1
                if i>=0 and B1[i][0]>=c['placed_at']:
                    ms,o,h,l,cl,vv=B1[i]; fill=None
                    if c['kind']=='limit':
                        if c['side']=='long' and l<=c['entry']: fill=c['entry']
                        if c['side']=='short' and h>=c['entry']: fill=c['entry']
                    else:
                        if c['side']=='long' and h>=c['trigger']: fill=c['trigger']
                        if c['side']=='short' and l<=c['trigger']: fill=c['trigger']
                    if fill is not None:
                        c['state']='filled'; c['filled_at']=ms; c['fill_px']=fill
                        open_pos=dict(c); open_pos['filled_at']=ms
                        for o2 in cands:
                            if o2 is not c and replay_arm_active(o2['state']): o2['state']='cancelled'; o2['reason']='one_open_position: other arm filled'
        t+=120000
    if open_pos:
        open_pos['exit_at']=None; open_pos['exit_reason']='OPEN at end'; open_pos['pts']=0; positions.append(open_pos)
    return cands+rows, positions
def report(mode):
    cands,positions=run(mode)
    out=[]
    for c in cands:
        out.append({k:(ms_ct(v) if k in('placed_at','filled_at','armed_at','cancel_at','refused_at','exit_at') and v else v) for k,v in c.items() if k not in('gaps','born','end','flat')})
    with open(f'replay_{mode}_arms.csv','w') as f:
        keys=sorted({k for r in out for k in r}); w=csv.DictWriter(f,fieldnames=keys); w.writeheader(); [w.writerow(r) for r in out]
    print(f"=== RUN {mode}: candidates {len(cands)}")
    from collections import Counter
    for c in cands:
        if c.get('state')=='none' and not c.get('confirm_met_at') and c.get('wc'): c['state']='never-confirmed'
        elif c.get('state')=='none': c['state']='rr-refused'
    st=Counter((c.get('state') or 'refused:'+c.get('refuse','')) for c in cands); print(' states:',dict(st))
    rs=Counter(c.get('reason') or c.get('refuse') or c.get('last_refuse','') for c in cands if c.get('state') in('cancelled','refused',None)); print(' reasons:',dict(rs))
    print(' bias-arm warnings (arm side != plan bias):', sum(1 for c in cands if c.get('warn_bias')))
    print(' stop-entry live-guard inversions (would have been refused by the live predicate):', sum(1 for c in cands if c.get('live_guard_would_refuse')))
    tot=0
    for p in positions:
        print('  FILL %s %s v%d %s %s %s entry %.2f stop %.2f(%s) tgt %.2f fill %s exit %s %s %.2f pts $%.0f'%(p['td'],p['sess'],p['v'],p['sid'],p['side'],p['cond'],p['entry'],p['stop'],p['bound'],p['target'],ms_ct(p['filled_at']),ms_ct(p['exit_at']) if p['exit_at'] else '-',p['exit_reason'],p['pts'],p['pts']*2))
        tot+=p['pts']
    print(' fills %d, net %.2f pts = $%.2f'%(len(positions),tot,tot*2))
    byday=Counter(); 
    for p in positions: byday[(p['td'],p['side'])]+=p['pts']
    print(' by day/side:',{k:round(v,2) for k,v in byday.items()})
    return cands,positions
if __name__=='__main__':
    for m in ('A','B'): report(m)
