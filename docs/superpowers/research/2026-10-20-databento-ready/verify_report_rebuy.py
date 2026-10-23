from pathlib import Path
import csv,json,hashlib,html
from test_lucid_payout import oracle
P=Path('/home/hoang/fleet-1017/out/lucid50k-AE-rebuy-20261023')
read=lambda n:list(csv.DictReader((P/n).open()))
summaries=read('summary.csv');accounts=read('accounts.csv');ledger=read('ledger.csv');payouts=read('payouts.csv');audit=json.loads((P/'audit.json').read_text())
for n,h in audit['artifacts'].items():assert hashlib.sha256((P/n).read_bytes()).hexdigest()==h
paths=json.loads(Path(audit['source']).read_text())
for s in summaries:
 key=lambda x:all(x[k]==s[k] for k in ['qty','method','execution'])
 aa=[a for a in accounts if key(a)];ll=[l for l in ledger if key(l)];pp=[p for p in payouts if key(p)]
 assert len(aa)==int(s['accounts_used']) and len(ll)==int(s['trades']) and len(pp)==int(s['payout_count'])
 ticks=sorted([p for p in paths if p['traded'] and p['method']==s['method'] and p['execution']==s['execution']],key=lambda p:p['day']);index=0
 assert [l['day'] for l in ll]==[p['day'] for p in ticks]
 for a in aa:
  expected=oracle(ticks[index:],int(s['qty']))
  for k,v in expected.items():
   if k=='status' and v=='LIVE_TRANSITION_AFTER_5_PAYOUTS':v='REPLACE_AFTER_PAYOUT_FIVE_HYPOTHETICAL'
   assert str(v)==a[k],(k,v,a[k])
  al=[l for l in ll if l['account']==a['account']];ap=[p for p in pp if p['account']==a['account']];b=5000000
  for l in al:
   assert int(l['start_cents'])==b;b+=int(l['pnl_cents'])-int(l['payout_cents']);assert b==int(l['end_cents'])
   if int(l['payout_cents']):assert b>=5210000
  assert b==int(a['ending_balance_cents'])
  assert sum(int(p['received_cents']) for p in ap)==int(a['received_cents'])
  index+=int(a['trades'])
 assert index==len(ticks)
 assert sum(int(a['received_cents']) for a in aa)==int(s['received_cents'])
with (P/'verification.json').open('x') as f:json.dump(dict(status='PASS',summary_rows=len(summaries),accounts=len(accounts),ledger_rows=len(ledger),payout_rows=len(payouts),all_entry_days_consumed_once=True,independent_Decimal_accounts=True,tests_per_timezone=14,random_rebuy_sequences_per_timezone=1000),f,indent=2)
paras=['A / E — repeat account purchase hypothetical | 2026-10-23',
'Sample 2025-10-20–2026-04-19. Start with one funded $50,000 account. After a breach OR fifth payout, replace with a fresh funded account before the next eligible trading session. Continue to end of data. Never reuse the breach-day trade. No extra purchase after the final trade.',
'A: immediate ORB5m breakout, SL whole box, TP whole box. E: EMA9 ORB2m confirmed-close entry, SL half box, TP whole box, fixed stop. One trade/day per strategy; 3 and 6 MNQ studied separately.',
'Payout policy unchanged: five days net >= $150, positive cycle net P&L, request min(50% retained profit, $2,000, balance minus $52,100), minimum request $500. Trader gets 90%; full request deducted. Keep $2,000 above locked $50,100 at withdrawal. That reserve may decline on later losses.',
'CRITICAL: immediate funded access is a research assumption, not a verified purchasable Lucid product. Evaluation phase, failures, purchase prices, onboarding/cooldown, real-live transition and provider permission are NOT modeled. Repeated SIM purchases after five payouts are hypothetical. These results must not be described as realizable net income.',
'All received amounts are after modeled trading fees and 90/10 split, BEFORE purchase/evaluation costs and tax. Account prices are unknown, not zero. If each account costs C and you include the initial account, net = reported received cash minus accounts_used × C. If initial cost is sunk, incremental cost uses replacements × C. Residual account equity is not added to payouts.',
'Primary uses trade-print fill proxies; stress adds one adverse tick to market entries/stop exits and requires one tick through limits. Round-trip fee assumption $1/MNQ; no queue/latency model. Original incomplete-data exclusions retained, so results are conditional on available sessions. No new raw tick download or replay; verified prior tick paths reused.',
'Verification: each actual account reconciled against an independent Decimal oracle; daily balance + P&L − withdrawal checked; every eligible trade consumed exactly once. 14 tests in Chicago/UTC, including 1,000 randomized multi-account sequences and earlier 3,000 account sequences per timezone. Initial export lacked the quantity key in the daily ledger: fixed and regenerated before delivery, with first attempt preserved. Final CSVs identify method, execution, quantity, account and date.',
'Audit reference docs/superpowers/AUDIT-CHECKLIST.md @ ba50edf22. Engine lucid_rebuy.py wraps verified lucid_payout.py. Evidence files: accounts.csv, ledger.csv, payouts.csv, summary.csv, audit.json, verification.json.']
md='\n\n'.join(paras[:4])+'\n\n';h='<meta charset="utf-8"><style>@page{size:A4 landscape;margin:12mm}body{font:12px Arial;line-height:1.4}table{width:100%;border-collapse:collapse}td,th{border:1px solid #aaa;padding:5px}tr{break-inside:avoid}</style>'+''.join('<p>'+html.escape(p)+'</p>' for p in paras[:4])
for mode in ['trade_print_proxy','stress1tick']:
 table=[['Method','MNQ','Accounts used','Breached','Payout-5 accounts','Payouts','Trader receives USD','Last balance USD']]
 for s in summaries:
  if s['execution']==mode:table.append([s[k] for k in ['method','qty','accounts_used','breached_accounts','five_payout_accounts','payout_count']]+[f"{int(s[k])/100:,.2f}" for k in ['received_cents','last_balance_cents']])
 md+='## '+mode+'\n\n'+'\n'.join('| '+' | '.join(r)+' |' for r in [table[0],['---']*len(table[0])]+table[1:])+'\n\n'
 h+='<h3>'+mode+'</h3><table>'+''.join('<tr>'+''.join('<td>'+html.escape(c)+'</td>' for c in r)+'</tr>' for r in table)+'</table>'
md+='\n\n'.join(paras[4:]);h+=''.join('<p>'+html.escape(p)+'</p>' for p in paras[4:])
for name,content in [('REPORT.md',md),('REPORT.html',h)]:
 with (P/name).open('x') as f:f.write(content)
dest=Path('lucid-rebuy-evidence-20261023');dest.mkdir()
for n in ['REPORT.md','summary.csv','accounts.csv','ledger.csv','payouts.csv','audit.json','verification.json']:
 with (dest/n).open('xb') as f:f.write((P/n).read_bytes())
print('PASS',len(accounts),'accounts',len(ledger),'daily rows',len(payouts),'payout rows')
