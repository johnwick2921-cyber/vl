"""Reconcile and publish half-box with positive locks at legal 1R activation, 2R/3R targets owner report without overwriting."""
import csv,json,zipfile
from decimal import Decimal as D
from pathlib import Path
from collections import Counter
from xml.sax.saxutils import escape
from xml.etree import ElementTree as ET
import tick_orb as e
from xlsx_tick_tables import export
p=Path('/home/hoang/fleet-1017/out/orb-ema9-box-lock-20261022');source=Path('/home/hoang/fleet-1017/out/orb-ema9-20261022')
a=json.loads((p/'audit.json').read_text());v=json.loads((p/'engine-verification.json').read_text())
assert a['engine_sha256']==v['engine_sha256']==e.p.digest(Path(__file__).with_name('ema_box_lock.py'))
assert a['source_trade_sha256']==e.p.digest(source/'trades.csv')
for n,h in a['artifacts'].items():assert e.p.digest(p/n)==h
base={(r['day'],r['orb_minutes']):r for r in csv.DictReader((source/'trades.csv').open()) if r['tp_distance']=='20'}
rows=list(csv.DictReader((p/'trades.csv').open()));summ=list(csv.DictReader((p/'summary.csv').open()))
assert len(rows)==len({(r['day'],r['orb_minutes'],r['rr'],r['lock_points']) for r in rows})==2196
assert (p/'sessions.csv').read_bytes()==(source/'sessions.csv').read_bytes()
for r in rows:
 b=base[r['day'],r['orb_minutes']]
 assert all(r.get(k,'')==b.get(k,'') for k in ('entry_row','entry_event_ns','entry_recv_ns','entry_points','signal_row','side','contract','eligible_ns'))
 if not r['entry_row']:
  assert r['outcome']==b['outcome'];continue
 w=D(r['orb_high'])-D(r['orb_low']);side=1 if r['side']=='LONG' else -1
 assert D(r['risk_points'])==w/2
 assert (D(r['entry_points'])-D(r['stop_points']))*side==w/2
 assert (D(r['target_points'])-D(r['entry_points']))*side==D(r['rr'])*w/2
 assert (D(r['lock_trigger_points'])-D(r['entry_points']))*side==max(w/2,D(r['lock_points'])+D('0.25'))
 assert (D(r['lock_stop_points'])-D(r['entry_points']))*side==D(r['lock_points'])
 if r['lock_row']:
  assert int(r['lock_row'])>int(r['entry_row'])
  if r['exit_row']:assert int(r['exit_row'])>int(r['lock_row'])
 assert int(r['entry_event_ns'])>=int(r['eligible_ns'])>=int(r['signal_end_ns'])
 if r['exit_row']:
  assert int(r['exit_row'])>int(r['entry_row'])
  assert D(r['pnl_points'])==(D(r['rr'])*w/2 if r['outcome']=='TP' else D(r['lock_points']) if r['outcome']=='LOCK' else -w/2)
for s in summ:
 rr=[r for r in rows if r['orb_minutes']==s['orb_minutes'] and r['rr']==s['rr'] and r['lock_points']==s['lock_points']];c=Counter(r['outcome'] for r in rr)
 assert len(rr)==122
 assert int(s['small_R_days'])==sum(D(r['risk_points'])<=D(s['lock_points']) for r in rr if r['entry_row'])
 for k,o in [('TP','TP'),('SL','SL'),('LOCK','LOCK'),('open','OPEN_AT_CUTOFF'),('no_signal','NO_SIGNAL'),('excluded','EXCLUDED')]:assert int(s[k])==c[o]
 assert int(s['entries'])==c['TP']+c['SL']+c['LOCK']+c['OPEN_AT_CUTOFF']
 closed=[r for r in rr if r['outcome'] in ('TP','SL','LOCK')]
 assert D(s['won_points'])==sum(D(r['pnl_points']) for r in closed if r['outcome'] in ('TP','LOCK'))
 assert D(s['lost_points'])==sum(D(r['pnl_points']) for r in closed if r['outcome']=='SL')
 assert D(s['net_points'])==D(s['won_points'])+D(s['lost_points'])
 assert D(s['rounded_net_points'])==sum(D(r['rounded_pnl_points']) for r in closed)
 assert D(s['observed_net_points'])==sum(D(r['observed_pnl_points']) for r in closed)
 assert D(s['max_day_win'])==max((D(r['pnl_points']) for r in closed if r['outcome'] in ('TP','LOCK')),default=D(0))
 assert D(s['max_day_loss'])==min((D(r['pnl_points']) for r in closed if r['outcome']=='SL'),default=D(0))
 assert abs(float(s['win_percent'])-100*(c['TP']+c['LOCK'])/int(s['entries']))<1e-10
head=['Khóa lời','RR','ORB','Lệnh','TP','SL','LOCK','Chưa đóng','Win %','Tổng thắng','Tổng thua','Lãi/lỗ điểm','Ngày thắng lớn nhất','Ngày thua lớn nhất']
table=[head]+[['+'+s['lock_points'],'1:'+s['rr'],s['orb_minutes']+'m',s['entries'],s['TP'],s['SL'],s['LOCK'],s['open'],f"{float(s['win_percent']):.2f}%",s['won_points'],s['lost_points'],s['net_points'],s['max_day_win'],s['max_day_loss']] for s in summ]
notes=[
 '20/10/2025–19/04/2026; mở08:30 Houston; ORB1m/2m/5m. SL=½ độ rộng ORB; TP2R hoặc3R từ entry. Lời+1R thì dời stop khóa+5/+10/+15 điểm (3 bài riêng) từ record tick tiếp theo, sau đó giữ nguyên. LONG stop=entry+lock; SHORT stop=entry−lock. Nếu1R chưa vượt mức khóa, chờ gain≥max(1R,lock+0,25) để stop hợp lệ; không loại ngày. Nếu TP đến trước thì chốt TP. Một lệnh/ngày mỗi bài, không re-entry.',
 'Entry GIỮ NGUYÊN từng row/timestamp/giá/chiều từ bài EMA9 SL20 TP20/30. Chờ EMA9 cùng khung đóng cắt lên đỉnh ORB hoặc xuống đáy; entry trade tick đầu tiên đủ điều kiện sau đóng nến. Không phải retest-limit.',
 'EMA xuyên phiên cùng raw contract, warmup300 nến từ Databento OHLCV1m; không reset08:30. Chưa chứng minh trùng tuyệt đối chart NT8 khác nguồn hoặc template. Entry logic và bộ lọc dữ liệu không thay đổi.',
 'Điểm chính theo TP/SL danh nghĩa, chưa phí/slippage. SL nửa box và TP3R có thể ở0,125 điểm, không phải tick MNQ hợp lệ. Stop/target làm tròn xa entry đến tick0,25 trong cột tick_rounded; RR sau làm tròn có thể khác danh nghĩa. LOCK nominal=+5/+10/+15; gap qua stop khóa vẫn có thể âm trong observed P&L. Lệnh chạm TP ngay tick kích hoạt kết thúc TP, không ghi thêm lock. rounded_net_points lưu riêng. Observed gap P&L lưu riêng, không bảo đảm market fill.',
 'Chỉ kiểm tra tới15:00 Houston. Lệnh chưa chạm TP/SL ghi OPEN_AT_CUTOFF, không đưa vào tổng điểm đã đóng. Win%=(TP+LOCK)/tất cả entry kể cả lệnh chưa đóng; đây là win danh nghĩa, không gán LOCK thành full TP. Tổng điểm thắng gồm cả TP và LOCK.',
 '130 ngày ứng viên;8 loại toàn phiên. ORB1m/2m loại thêm4;5m loại thêm6 do receipt trễ/degraded. Không có tín hiệu:1m0,2m3,5m5. Cùng mẫu với bài EMA trước, các ngày loại có trong CSV.',
 '15 test mới PASS tại CT/UTC, gồm6.000 chuỗi giả lập mỗi timezone đối chiếu first passage;6 mutation đều bị assertion phát hiện. Kế thừa15 test EMA cùng6 mutation ở bài nguồn. Tick file hash và count được kiểm tra lại;entry và ORB được đối chiếu raw ticks.',
 'Đã đối chiếu toàn bộ2196 dòng,2064 entry-variant (344 entry chung ×2 target ×3 lock) với bài nguồn; kiểm tra SL/TP/entry, exit thứ tự, tổng điểm, ngày thắng/thua lớn nhất và ô bảng Word. Decoder dùng chung; không cam kết dữ liệu/fill thực chính xác100%. Không sửa bot/DB/NT8.',
 'Mỗi dòng là một bài test riêng, không cộng thành danh mục. Audit reference docs/superpowers/AUDIT-CHECKLIST.md @ba50edf22.'
]
lines=['# MNQ EMA9 ORB — SL NỬA BOX / TP2R–3R / KHÓA LỜI +5,+10,+15','',notes[0],'','| '+' | '.join(head)+' |','|'+'|'.join(['---']*len(head))+'|']+['| '+' | '.join(r)+' |' for r in table[1:]]+['','## Quy tắc và xác minh','']+['- '+n for n in notes[1:]]
lines+=['','## P&L bổ sung','','| Khóa | RR | ORB | Nominal | Stop/target làm tròn tick | Stop theo print quan sát |','|---|---|---|---:|---:|---:|']+['| '+' | '.join(s[k] for k in ('lock_points','rr','orb_minutes','net_points','rounded_net_points','observed_net_points'))+' |' for s in summ]
lines+=['','## Số entry có 1R không lớn hơn mức khóa (phải chờ giá vượt thêm ít nhất 1 tick)','','| Khóa | ORB | Ngày |','|---|---|---:|']+['| '+s['lock_points']+' | '+s['orb_minutes']+' | '+s['small_R_days']+' |' for s in summ if s['rr']=='2']
with (p/'REPORT.md').open('x') as f:f.write('\n'.join(lines)+'\n')
def para(t):return '<w:p><w:r><w:rPr><w:sz w:val="18"/></w:rPr><w:t>'+escape(t)+'</w:t></w:r></w:p>'
w='http://schemas.openxmlformats.org/wordprocessingml/2006/main';widths=[700,500,500,550,500,500,650,750,700,1000,1000,1000,1200,1200]
body=para('MNQ EMA9 ORB — SL NỬA BOX / TP2R–3R / KHÓA LỜI +5,+10,+15')+para(notes[0])+para('Đơn vị: điểm MNQ, chưa phí/slippage.')
body+='<w:tbl><w:tblPr><w:tblLayout w:type="fixed"/><w:tblBorders>'+''.join(f'<w:{q} w:val="single" w:sz="4"/>' for q in ('top','left','bottom','right','insideH','insideV'))+'</w:tblBorders></w:tblPr><w:tblGrid>'+''.join(f'<w:gridCol w:w="{q}"/>' for q in widths)+'</w:tblGrid>'
for i,row in enumerate(table):
 body+='<w:tr><w:trPr><w:cantSplit/>'+('<w:tblHeader/>' if i==0 else '')+'</w:trPr>'
 for j,t in enumerate(row):body+=f'<w:tc><w:tcPr><w:tcW w:w="{widths[j]}" w:type="dxa"/></w:tcPr>'+para(t)+'</w:tc>'
 body+='</w:tr>'
body+='</w:tbl>'+''.join(para(n) for n in notes[1:])+''.join(para('1R ≤ mức khóa: khóa+'+s['lock_points']+' ORB'+s['orb_minutes']+'m: '+s['small_R_days']+' ngày.') for s in summ if s['rr']=='2')+''.join(para('Khóa+'+s['lock_points']+' RR1:'+s['rr']+' ORB'+s['orb_minutes']+'m — P&L stop/target làm tròn tick: '+s['rounded_net_points']+'; stop theo print quan sát: '+s['observed_net_points']) for s in summ)+'<w:sectPr><w:pgSz w:w="16838" w:h="11906" w:orient="landscape"/><w:pgMar w:top="650" w:right="650" w:bottom="650" w:left="650"/></w:sectPr>'
with zipfile.ZipFile(p/'REPORT.docx','x',zipfile.ZIP_DEFLATED) as z:
 z.writestr('[Content_Types].xml','<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>')
 z.writestr('_rels/.rels','<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>')
 z.writestr('word/document.xml',f'<w:document xmlns:w="{w}"><w:body>'+body+'</w:body></w:document>')
with zipfile.ZipFile(p/'REPORT.docx') as z:
 assert z.testzip() is None
 root=ET.fromstring(z.read('word/document.xml'));ns={'w':w}
 assert [[''.join(c.itertext()) for c in r.findall('w:tc',ns)] for r in root.findall('.//w:tr',ns)]==table
export(p,p/'TRADES.xlsx')
e.p.write_json(p/'final-verification.json',dict(status='PASS_WITH_ASSUMPTIONS',all_rows=len(rows),entry_rows=sum(bool(r['entry_row']) for r in rows),source_entry_parity=True,all_arithmetic_reconciled=True,word_cells_match=True))
with zipfile.ZipFile(p/'FULL-REPORT.zip','x',zipfile.ZIP_DEFLATED) as z:
 for n in ('REPORT.docx','REPORT.md','TRADES.xlsx','trades.csv','summary.csv','sessions.csv','audit.json','engine-verification.json','final-verification.json'):z.write(p/n,n)
print('PASS',table)
