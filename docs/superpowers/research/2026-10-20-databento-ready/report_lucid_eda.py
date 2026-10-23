from pathlib import Path
import csv,json,zipfile,html
from xml.sax.saxutils import escape
from xml.etree import ElementTree as ET
p=Path('/home/hoang/fleet-1017/out/lucid50k-EDA-3v6-20261022')
a=list(csv.DictReader((p/'accounts.csv').open()));v=json.loads((p/'independent-account-verification.json').read_text())
paras=['E / D / A — LucidFlex funded 50K: offline verification, 22/10/2026', 'Sample: 20/10/2025–19/04/2026. Separate 3 and 6 MNQ accounts. No DLL, no withdrawals, no resets. Current published rules applied counterfactually.', 'E: EMA9 ORB2m, candle-close signal, SL half box, TP whole box, no trailing. D: ORB5m full-body confirmation then retest limit, SL half box, TP whole box. A: immediate ORB5m breakout, SL whole box, TP whole box.', 'Verified: 26 tests in Chicago and UTC; 5,000 randomized E/D/A exit paths, 4,000 other exit paths and 2,000 account paths per timezone. Nine deliberately injected faults were detected. Full tick replay recomputed source entries and exits. Independent Decimal-dollar audit matched 12 account summaries, 1,140 ledger rows and 1,072 rolling windows; file hashes matched.', 'This is research-engine verification. It does not certify the live bot, NT8 order hooks, broker fills, or future profitability. No deployment or live execution was performed.']
tables=[]
for mode in ['trade_print_proxy','stress1tick']:
 t=[['Method','MNQ','Trades','Net USD','End USD','Breach date','Max loss/day USD','Max win/day USD','20-trade breaches']]
 for r in a:
  if r['execution']!=mode:continue
  s=next(s for s in v['stats'] if s['method']==r['method'] and s['execution']==mode and s['qty']==int(r['qty']))
  t.append([r['method'],r['qty'],r['trades'],f"{int(r['net_cents'])/100:,.2f}",f"{int(r['ending_balance_cents'])/100:,.2f}",r['breach_day'] or 'None in valid sample',f"{s['min_day_cents']/100:,.2f}",f"{s['max_day_cents']/100:,.2f}",f"{s['rolling20_breaches']}/{s['rolling20_windows']}"])
 tables.append(t)
notes=[
'Pipeline: receipt/hash and contract/timestamp checks → complete ORB → E continuous-session EMA warmup / D closed full-body confirmation / A first breakout → entry → integer-tick stop/target → chronological exit print → fees → integer-cent daily ledger → intraday loss-limit check → EOD floor update → report. Independent account checker imports neither engine.',
'Math: MNQ 0.25 point/tick, $0.50/tick/contract. Gross USD = signed gain ticks × $0.50 × quantity. Fee assumption = $1 round trip per contract (Lucid published commission schedule), reserved at entry. Net = gross minus fee. This fee has not been checked against an owner statement.',
'SL half box is rounded UP to a valid whole tick, so an odd-tick box has slightly less than 2:1 planned reward/risk. Targets use the full box. Stops use first crossing trade price, not a guaranteed stop price. No fractional-tick orders.',
'Account: initial $50,000; floor $48,000. Equity at or below floor breaches immediately. Next EOD floor = min($50,100, max(previous floor, highest EOD balance − $2,000)). Stop the account on breach; never include later profits. Breach balance is a trade-print mark, not a guaranteed liquidation fill.',
'Primary uses trade-print entry/stop proxies and touched target limits. Stress: 1 tick adverse on market entries and stop/cutoff exits; target must trade 1 tick through. D entry must trade 1 tick through the limit, filled at the limit. Queue position, quotes, latency and actual broker fills are not available.',
'At 15:00 Houston, unresolved trades use the last observed trade BEFORE cutoff as a forced-close proxy. E includes 1 such trade, D 3, A 2. Prior reports excluded these open trades; totals therefore differ, also because of tick rounding, stop overshoots and fees.',
'130 weekdays in the requested span; 122 pass whole-session coverage. Eight excluded days are gaps, not confirmed no-trade days. E has another four excluded days; A/D another one. E has 115 entries, D 89, A 121. Different eligible cohorts mean this is not a perfectly controlled ranking.',
'Rolling tests start a fresh $50,000 account at each eligible entry and follow up to 20 trades, stopping on breach. Windows overlap and omit unavailable days; fractions are descriptive counts, NOT independent estimates of failure probability. They are not 20 calendar days.',
'After floor locks at $50,100, peak-to-trough drawdown can exceed $2,000 without a breach if accumulated equity remains above the fixed floor. No payout withdrawals were modeled; withdrawals could materially change survival.',
'Findings: D with 6 MNQ breaches on 04/11/2025 in both fill models. E with 3 MNQ had 0/96 rolling-window breaches in both models; E with 6 had 33/96. A had higher October-start profits but many more rolling-window breaches. These are historical conditional results, not proof of a safe or best future strategy.',
'Sources (checked for this study): https://support.lucidtrading.com/en/articles/12945795-lucidflex-funded-account ; https://support.lucidtrading.com/en/articles/12945815-lucidflex-drawdown ; https://support.lucidtrading.com/en/articles/12945808-lucidflex-scaling-plan ; https://support.lucidtrading.com/en/articles/12945796-lucidflex-payouts ; https://support.lucidtrading.com/en/articles/11508978-approved-products-and-commissions ; https://info.tradovate.com/pricing/commissions ; https://www.cmegroup.com/markets/equities/nasdaq/micro-e-mini-nasdaq-100.contractSpecs.html',
'Evidence: audit.json, engine-verification.json, independent-account-verification.json, paths.json, accounts.csv, ledger.csv, rolling20.csv. Audit reference: docs/superpowers/AUDIT-CHECKLIST.md @ ba50edf22.'
]
md='\n\n'.join(paras)+'\n\n'
for title,t in zip(['Primary — trade-print proxy','Stress — one tick'],tables):
 md+=title+'\n\n'+'\n'.join('| '+' | '.join(row)+' |' for row in [t[0],['---']*len(t[0])]+t[1:])+'\n\n'
md+='\n\n'.join(notes)+'\n'
with (p/'REPORT.md').open('x') as f:f.write(md)
def para(x):return '<w:p><w:r><w:t>'+escape(x)+'</w:t></w:r></w:p>'
def table(t):return '<w:tbl>'+''.join('<w:tr>'+''.join('<w:tc>'+para(c)+'</w:tc>' for c in row)+'</w:tr>' for row in t)+'</w:tbl>'
body=''.join(map(para,paras))+para('Primary — trade-print proxy')+table(tables[0])+para('Stress — one tick')+table(tables[1])+''.join(map(para,notes))
xml='<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>'+body+'<w:sectPr><w:pgSz w:w="16838" w:h="11906" w:orient="landscape"/></w:sectPr></w:body></w:document>'
with zipfile.ZipFile(p/'EDA-verified.docx','x',zipfile.ZIP_DEFLATED) as z:
 z.writestr('[Content_Types].xml','<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>')
 z.writestr('_rels/.rels','<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>')
 z.writestr('word/document.xml',xml)
with zipfile.ZipFile(p/'EDA-verified.docx') as z:
 assert z.testzip() is None
 ns={'w':'http://schemas.openxmlformats.org/wordprocessingml/2006/main'};tree=ET.fromstring(z.read('word/document.xml'))
 assert [[[''.join(c.itertext()) for c in r.findall('w:tc',ns)] for r in t.findall('w:tr',ns)] for t in tree.findall('.//w:tbl',ns)]==tables
h='<meta charset="utf-8"><style>@page{size:A4 landscape;margin:14mm}body{font:12px Arial}table{border-collapse:collapse;width:100%}td,th{border:1px solid #aaa;padding:6px}tr{break-inside:avoid}p{line-height:1.4}thead{display:table-header-group}</style>'
h+=''.join('<p>'+html.escape(x)+'</p>' for x in paras)
for title,t in zip(['Primary — trade-print proxy','Stress — one tick'],tables):h+='<h3>'+title+'</h3><table><thead><tr>'+''.join('<th>'+html.escape(c)+'</th>' for c in t[0])+'</tr></thead><tbody>'+''.join('<tr>'+''.join('<td>'+html.escape(c)+'</td>' for c in row)+'</tr>' for row in t[1:])+'</tbody></table>'
h+=''.join('<p>'+html.escape(x)+'</p>' for x in notes)
with (p/'REPORT.html').open('x') as f:f.write(h)
print('Report and DOCX tables verified')
