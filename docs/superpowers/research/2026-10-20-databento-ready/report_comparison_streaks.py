from pathlib import Path
from decimal import Decimal as D
import csv,datetime as dt,hashlib,json,zipfile
from xml.sax.saxutils import escape
from xml.etree import ElementTree as ET
root=Path('/home/hoang/fleet-1017/out');out=root/'orb-comparison-streaks-20261022';out.mkdir(exist_ok=False)
specs=[('A','Breakout ngay — ORB5m; SL cả box, TP cả box','orb-six-month-stop-variants-20261020',{'orb_minutes':'5','mode':'box'}),('B','Full-body → limit retest — ORB5m; SL cả box, TP cả box','orb-body-confirmed-retest-20261020',{'orb_minutes':'5','mode':'box','body_rule':'full_body'}),('C','Breakout → limit retest touch — ORB5m; SL cả box, TP cả box','orb-six-month-retest-20261020',{'orb_minutes':'5','mode':'box','fill_rule':'touch'}),('D','Full-body → limit retest — ORB5m; SL nửa box, TP cả box','orb-body-confirmed-retest-20261020',{'orb_minutes':'5','mode':'half_box','body_rule':'full_body'}),('E','EMA9 — ORB2m; SL nửa box, TP cả box; không dời stop','orb-ema9-halfbox-20261022',{'orb_minutes':'2'}),('F','EMA9 — ORB2m; SL nửa box, TP cả box; +1R khóa +5','orb-ema9-box-lock-20261022',{'orb_minutes':'2','rr':'2','lock_points':'5'})]
allstats=[];details=[];hashes={}
def streaks(rows,key,sgn):
 runs=[];run=[];prev=None
 for r in rows:
  d=dt.date.fromisoformat(r['day']);n=prev+dt.timedelta(days=1) if prev else None
  if n:
   while n.weekday()>4:n+=dt.timedelta(days=1)
  if r[key] and D(r[key])*sgn>0:
   if run and d!=n:runs.append(run);run=[]
   run.append(r)
  else:
   if run:runs.append(run);run=[]
  prev=d
 if run:runs.append(run)
 longest=max(map(len,runs),default=0)
 return [a for a in runs if len(a)==longest]
for code,name,folder,filters in specs:
 p=root/folder/'trades.csv';hashes[str(p)]=hashlib.sha256(p.read_bytes()).hexdigest()
 rows=sorted([r for r in csv.DictReader(p.open()) if all(r[k]==v for k,v in filters.items())],key=lambda r:r['day'])
 assert len(rows)==len({r['day'] for r in rows})
 key='pnl_points' if 'pnl_points' in rows[0] else 'pnl_planned_points'
 entries=[r for r in rows if r['entry_row']];closed=[r for r in entries if r[key]]
 wins=[r for r in closed if D(r[key])>0];loss=[r for r in closed if D(r[key])<0]
 sums=[r for r in csv.DictReader((root/folder/'summary.csv').open()) if all(r[k]==v for k,v in filters.items())];assert len(sums)==1
 summary=sums[0];net=sum(D(r[key]) for r in closed)
 assert net==D(summary.get('net_points',summary.get('net_planned_points')))
 assert len(entries)==int(summary['entries'])
 stat={'id':code,'method':name,'entries':len(entries),'TP':sum(r['outcome']=='TP' for r in entries),'SL':sum(r['outcome']=='SL' for r in entries),'LOCK':sum(r['outcome']=='LOCK' for r in entries),'open':len(entries)-len(closed),'win_pct_entries':f'{100*len(wins)/len(entries):.2f}%','win_pct_closed':f'{100*len(wins)/len(closed):.2f}%','won_points':str(sum(D(r[key]) for r in wins)),'lost_points':str(sum(D(r[key]) for r in loss)),'net_points':str(net),'max_win_day':str(max(D(r[key]) for r in closed)),'max_loss_day':str(min(D(r[key]) for r in closed))}
 for label,sgn in [('win',1),('loss',-1)]:
  rr=streaks(rows,key,sgn);stat[label+'_streak_days']=len(rr[0]) if rr else 0
  for a in rr:
   details.append({'id':code,'type':label,'days':len(a),'start':a[0]['day'],'end':a[-1]['day'],'points':str(sum(D(r[key]) for r in a)),'daily':[(r['day'],r[key]) for r in a]})
 allstats.append(stat)
with (out/'comparison.csv').open('x') as f:
 w=csv.DictWriter(f,fieldnames=allstats[0]);w.writeheader();w.writerows(allstats)
with (out/'streaks.json').open('x') as f:json.dump(details,f,indent=2)
paras=['MNQ ORB — So sánh kết quả và chuỗi ngày thắng/thua','Giai đoạn 20/10/2025–19/04/2026. Tổng hợp lại từ từng dòng giao dịch ngày 22/10/2026; không chạy lại engine trong báo cáo này.']
paras += [s['id']+': '+s['method'] for s in allstats]
notes=['Tất cả điểm là danh nghĩa, trước phí/slippage. Lệnh chưa đóng lúc15:00 Houston không tính vào tổng điểm hoặc win/loss. SL nửa box có thể nằm ở nửa tick; số liệu làm tròn tick nằm trong báo cáo gốc.', 'Win rate toàn bộ = (TP + LOCK có lãi)/tổng entry. Win rate đã đóng = số lệnh có lãi/số lệnh đã đóng. LOCK +5 được tính là thắng danh nghĩa, không phải chạm full target.', 'Chuỗi ngày: ngày có P&L dương/âm liên tiếp, bỏ qua thứ Bảy/Chủ nhật. Ngày không có lệnh, chưa đóng, bị loại hoặc thiếu dữ liệu ngắt chuỗi. Không tự nối qua ngày lễ thiếu dữ liệu. Đây là chuỗi xác nhận được, có thể ngắn hơn chuỗi chỉ tính các lệnh.', 'Nếu nhiều chuỗi cùng số ngày dài nhất, liệt kê tất cả; tổng điểm trong mỗi chuỗi được ghi riêng. Ngày thắng/thua lớn nhất là P&L của một ngày có một lệnh, không phải drawdown trong lệnh.', 'B/C/D dùng giả định limit chạm là khớp; không mô phỏng hàng chờ. Các nhóm khác bộ lọc dữ liệu và kiểu entry, nên bảng này không phải so sánh đối chứng hoàn toàn đồng nhất. Không cộng các phương pháp thành danh mục.', 'Các ngày và số điểm từng ngày trong chuỗi nằm ở streaks.json. comparison.csv giữ bảng số liệu. Đây là kết quả mẫu lịch sử, không chứng minh phương pháp tốt nhất tương lai.']
tables=[]
tables.append([['Mã','Lệnh','TP','SL','LOCK','Mở','Win% tất cả','Win% đã đóng','Thắng điểm','Thua điểm','Tổng điểm']]+[[str(s[k]) for k in ['id','entries','TP','SL','LOCK','open','win_pct_entries','win_pct_closed','won_points','lost_points','net_points']] for s in allstats])
tables.append([['Mã','Chuỗi thắng (ngày)','Chuỗi thua (ngày)','Ngày thắng max (điểm)','Ngày lỗ max (điểm)']]+[[str(s[k]) for k in ['id','win_streak_days','loss_streak_days','max_win_day','max_loss_day']] for s in allstats])
tables.append([['Mã','Loại chuỗi','Ngày','Bắt đầu','Kết thúc','Tổng điểm chuỗi']]+[[str(d[k]) for k in ['id','type','days','start','end','points']] for d in details])
def para(s):return '<w:p><w:r><w:t xml:space="preserve">'+escape(s)+'</w:t></w:r></w:p>'
def table(t):return '<w:tbl><w:tblPr><w:tblBorders>'+''.join('<w:'+x+' w:val="single" w:sz="4"/>' for x in ['top','left','bottom','right','insideH','insideV'])+'</w:tblBorders></w:tblPr>'+''.join('<w:tr>'+''.join('<w:tc>'+para(v)+'</w:tc>' for v in row)+'</w:tr>' for row in t)+'</w:tbl>'
body=''.join(para(p) for p in paras)+''.join(table(t)+para('') for t in tables)+''.join(para(n) for n in notes)
xml='<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>'+body+'<w:sectPr><w:pgSz w:w="16838" w:h="11906" w:orient="landscape"/><w:pgMar w:top="600" w:right="600" w:bottom="600" w:left="600"/></w:sectPr></w:body></w:document>'
with zipfile.ZipFile(out/'MNQ-ORB-comparison.docx','x',zipfile.ZIP_DEFLATED) as z:
 z.writestr('[Content_Types].xml','<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>')
 z.writestr('_rels/.rels','<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>')
 z.writestr('word/document.xml',xml)
with zipfile.ZipFile(out/'MNQ-ORB-comparison.docx') as z:
 assert z.testzip() is None
 tree=ET.fromstring(z.read('word/document.xml'));ns={'w':'http://schemas.openxmlformats.org/wordprocessingml/2006/main'}
 extracted=[[[ ''.join(c.itertext()) for c in row.findall('w:tc',ns)] for row in t.findall('w:tr',ns)] for t in tree.findall('.//w:tbl',ns)]
 assert extracted==tables
with (out/'verification.json').open('x') as f:json.dump({'source_hashes':hashes,'summary_decimal_reconciled':True,'docx_tables_match_calculations':True,'streak_definition':'Consecutive weekdays; all unavailable/non-win/non-loss days break corresponding run; weekends skipped'},f,indent=2)
print(json.dumps(allstats,ensure_ascii=False,indent=2));print(out/'MNQ-ORB-comparison.docx')
