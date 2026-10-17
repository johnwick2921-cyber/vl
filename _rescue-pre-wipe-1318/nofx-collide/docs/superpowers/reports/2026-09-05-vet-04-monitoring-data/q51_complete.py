"""Section 4 read-only audit. Run from worktree; outputs only to owned scratch."""
import sqlite3,json,math,datetime,pathlib,zoneinfo,hashlib,subprocess
ROOT=pathlib.Path('/home/hoang/nofx-analysis/vet-04-complete-0905')
ct=zoneinfo.ZoneInfo('America/Chicago')
# Resolve the current classifier; do not retype the ledger lifecycle here.
repo=next(p for p in pathlib.Path(__file__).resolve().parents if (p/'go.mod').exists())
nonterminal_arm_sql=subprocess.check_output(['go','run','./cmd/arm-state-sql'],cwd=repo,text=True).strip()
def ms(s): return int(datetime.datetime.fromisoformat(s).replace(tzinfo=ct).timestamp()*1000)
def wilson(k,n):
 if not n:return None
 z=1.95996398454;p=k/n;d=1+z*z/n;c=(p+z*z/(2*n))/d;w=z*math.sqrt(p*(1-p)/n+z*z/(4*n*n))/d
 return [c-w,c+w]
c=sqlite3.connect('file:/home/hoang/nofx/data/data.db?mode=ro',uri=True);c.row_factory=sqlite3.Row;c.execute('PRAGMA query_only=ON');c.execute('BEGIN')
o={'captured_ct':datetime.datetime.now(ct).isoformat(),'source_base':'b4376246','era_ms':ms('2026-08-15'),'strict_boot_ms':ms('2026-09-03T11:10:00'),'query_only':c.execute('PRAGMA query_only').fetchone()[0],'queries':{}}
def q(name,sql,p=()):
 rows=[dict(r) for r in c.execute(sql,p)];o['queries'][name]={'sql':sql,'parameters':p,'rows':rows};return rows
rows=q('population',"""SELECT id,status,source,plan_id,entry_time,exit_time,side,entry_price,exit_price,quantity,entry_quantity,pnl_corrected,close_reason,mae,mfe,plan_session FROM trader_positions WHERE entry_time>=? ORDER BY id""",(o['era_ms'],))
def category(r):
 if r['source']=='e7_farside_test':return 'test_seam'
 if r['plan_id']=='UNRESOLVABLE':return 'UNRESOLVABLE'
 if r['pnl_corrected'] is None:return 'null_pnl'
 if r['status']!='CLOSED':return 'not_closed'
 if r['close_reason'] in ['reconcile_flat','unresolved','test_seam']:return 'excluded_close_reason'
 return 'eligible'
for r in rows:r['category']=category(r)
e=[r for r in rows if category(r)=='eligible']
def summary(rs):
 n=len(rs);w=sum(r['pnl_corrected']>0 for r in rs)
 return {'n':n,'ids':[r['id'] for r in rs],'sum_pnl_corrected':sum(r['pnl_corrected'] for r in rs),'mean_pnl_corrected':sum(r['pnl_corrected'] for r in rs)/n if n else None,'wins':w,'losses':sum(r['pnl_corrected']<0 for r in rs),'flats':sum(r['pnl_corrected']==0 for r in rs),'win_wilson95':wilson(w,n)}
o['eligible']=summary(e);o['exclusions']={cat:[r['id'] for r in rows if category(r)==cat] for cat in sorted(set(category(r) for r in rows)) if cat!='eligible'}
for basis in ['entry_time','exit_time']:
 days={}
 for r in e:
  t=datetime.datetime.fromtimestamp(r[basis]/1000,ct);d=(t+datetime.timedelta(days=t.hour>=17)).date().isoformat();days.setdefault(d,[]).append(r)
 o['cme_days_'+basis]={d:summary(rs) for d,rs in sorted(days.items())}
o['post_strict_entry']=summary([r for r in e if r['entry_time']>=o['strict_boot_ms']]);o['post_strict_exit']=summary([r for r in e if r['exit_time']>=o['strict_boot_ms']])
o['excursions']=q('excursions','SELECT COUNT(*) n FROM trade_excursions')
o['eligible_mae_mfe_coverage']={'n':len(e),'covered':sum(r['mae'] is not None and r['mfe'] is not None for r in e)}
q('arm35','SELECT id,signal_id,side,entry_px,stop_px,target_px,fill_price,state,created_at,updated_at FROM armed_orders WHERE id=35')
q('open_positions','SELECT id,symbol,side,quantity,entry_price,entry_time FROM trader_positions WHERE status=\'OPEN\'')
q('current_pending_arms',"SELECT id,signal_id,scenario,side,entry_px,stop_px,target_px,state,created_at,updated_at FROM armed_orders WHERE "+nonterminal_arm_sql+" ORDER BY id")
q('digest_receipts','SELECT id,trade_date,session,kind,created_at,text FROM day_plan_digests WHERE id IN (55,56,59,60,63,64)')
q('gap_log_ids','SELECT id,ts_utc,message FROM log_events WHERE id IN (25754,25755)')
q('outage_alerts','SELECT id,level,kind,created_at,acked,dismissed FROM day_plan_alerts WHERE id IN (629,654)')
q('decision_gap_ids','SELECT id,timestamp FROM decision_records WHERE id IN (37169,37170)')
q('touch_keys',"SELECT COUNT(*) raw_n,COUNT(DISTINCT printf('%.8f|%d',level_price,opened_at_ms)) price_time_keys,GROUP_CONCAT(id) ids FROM touch_outcomes")
q('rthl_keys',"SELECT COUNT(*) raw_n,COUNT(DISTINCT printf('%.8f|%d',level_price,opened_at_ms)) price_time_keys,GROUP_CONCAT(id) ids FROM touch_outcomes WHERE level_kind='RTH-L'")
# Exact accepted + fill receipts, never infer accepted stop from mutable ledger.
f=pathlib.Path('/mnt/c/Users/hoang/Documents/NinjaTrader 8/log/log.20260903.00000.txt');signal=o['queries']['arm35']['rows'][0]['signal_id'];receipts=[]
for line,text in enumerate(f.open(errors='replace'),1):
 if signal in text and any(term in text for term in ['submitted entry','protective bracket',"New state='Accepted'","New state='Filled'"]):receipts.append({'path':str(f),'line':line,'text':text.rstrip()})
o['nt8_receipts']=receipts
assert any("Stop price=29355" in r['text'] and "New state='Accepted'" in r['text'] for r in receipts)
assert any("Stop price=29355" in r['text'] and "Fill price=29355" in r['text'] and "New state='Filled'" in r['text'] for r in receipts)
a=o['queries']['arm35']['rows'][0];o['pos591_stop']={'position_id':591,'arm_id':35,'accepted_stop':29355.0,'fill':29355.0,'broker_stop_slippage_pts':0,'broker_stop_slippage_ticks':0,'ledger_stop':a['stop_px'],'accepted_minus_ledger_drift_pts':29355-a['stop_px'],'ledger_drift_tick_equivalent':(29355-a['stop_px'])/.25,'measurement':'SIM receipt only, not live fill-quality inference'}
# Exact source receipt excerpts at the assigned base; inventories are not screenshots.
spans={'market/futures_symbol.go':[(83,116)],'web/src/guide/components/useResolvedPlanMode.ts':[(14,46)],'web/src/guide/components/MockPlanCard.tsx':[(165,176)],'trader/auto_trader_planner.go':[(2470,2514)],'api/server.go':[(626,632)],'api/handler_risk.go':[(178,233)],'trader/auto_trader_feedwatch.go':[(20,35),(63,102)],'web/src/components/plan/PlanCard.tsx':[(83,176)],'web/src/components/plan/SessionPlanCard.tsx':[(364,433),(488,561),(740,912)],'web/src/components/plan/ScenarioList.tsx':[(223,290)],'web/src/components/plan/ExpectancyPanel.tsx':[(183,302)],'web/src/components/plan/InstrumentsDrawer.tsx':[(104,258)],'web/src/components/plan/AlertCenter.tsx':[(161,169),(196,197)],'web/src/pages/TraderDashboardPage.tsx':[(159,177),(650,666),(880,920)],'web/src/guide/content/status.ts':[(100,116)],'web/src/guide/content/routines.ts':[(10,42)],'telegram/bot.go':[(109,114),(254,263)],'trader/armed_executor.go':[(1058,1104)],'store/armed_orders.go':[(174,206)],'store/pnl_surface_guard.go':[(60,84)]}
source=[]
for p,ranges in spans.items():
 content=pathlib.Path(p).read_text(); lines=content.splitlines();old=subprocess.run(['git','show','2a66d91c:'+p],capture_output=True).stdout
 source.append({'path':p,'sha256':hashlib.sha256(content.encode()).hexdigest(),'identical_to_original_source_base':old==content.encode()})
 for lo,hi in ranges:source.extend(f'{p}:{i}: {lines[i-1]}' for i in range(lo,min(hi,len(lines))+1))
o['source_inventory_receipts']=source
ROOT.joinpath('q51_complete.json').write_text(json.dumps(o,indent=2))
assert len(e)==58 and abs(sum(r['pnl_corrected'] for r in e)+466.428572)<.00001
assert (o['eligible']['wins'],o['eligible']['losses'],o['eligible']['flats'])==(18,38,2)
assert len(o['cme_days_entry_time'])==12 and len(o['cme_days_exit_time'])==12
assert not o['post_strict_entry']['n'] and not o['post_strict_exit']['n']
print(json.dumps({k:o[k] for k in ['captured_ct','eligible','exclusions','post_strict_exit','eligible_mae_mfe_coverage','pos591_stop']},indent=2))
