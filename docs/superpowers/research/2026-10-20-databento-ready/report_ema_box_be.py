"""Reconcile and publish half-box with BE at 1R, 2R/3R targets owner report without overwriting."""
import csv,json,zipfile
from decimal import Decimal as D
from pathlib import Path
from collections import Counter
from xml.sax.saxutils import escape
from xml.etree import ElementTree as ET
import tick_orb as e
from xlsx_tick_tables import export
p=Path('/home/hoang/fleet-1017/out/orb-ema9-box-be-20261022');source=Path('/home/hoang/fleet-1017/out/orb-ema9-20261022')
a=json.loads((p/'audit.json').read_text());v=json.loads((p/'engine-verification.json').read_text())
assert a['engine_sha256']==v['engine_sha256']==e.p.digest(Path(__file__).with_name('ema_box_be.py'))
assert a['source_trade_sha256']==e.p.digest(source/'trades.csv')
for n,h in a['artifacts'].items():assert e.p.digest(p/n)==h
base={(r['day'],r['orb_minutes']):r for r in csv.DictReader((source/'trades.csv').open()) if r['tp_distance']=='20'}
rows=list(csv.DictReader((p/'trades.csv').open()));summ=list(csv.DictReader((p/'summary.csv').open()))
assert len(rows)==len({(r['day'],r['orb_minutes'],r['rr']) for r in rows})==732
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
 assert (D(r['be_trigger_points'])-D(r['entry_points']))*side==w/2
 if r['be_row']:
  assert int(r['be_row'])>int(r['entry_row'])
  if r['exit_row']:assert int(r['exit_row'])>int(r['be_row'])
 assert int(r['entry_event_ns'])>=int(r['eligible_ns'])>=int(r['signal_end_ns'])
 if r['exit_row']:
  assert int(r['exit_row'])>int(r['entry_row'])
  assert D(r['pnl_points'])==(D(r['rr'])*w/2 if r['outcome']=='TP' else 0 if r['outcome']=='BE' else -w/2)
for s in summ:
 rr=[r for r in rows if r['orb_minutes']==s['orb_minutes'] and r['rr']==s['rr']];c=Counter(r['outcome'] for r in rr)
 assert len(rr)==122
 for k,o in [('TP','TP'),('SL','SL'),('BE','BE'),('open','OPEN_AT_CUTOFF'),('no_signal','NO_SIGNAL'),('excluded','EXCLUDED')]:assert int(s[k])==c[o]
 assert int(s['entries'])==c['TP']+c['SL']+c['BE']+c['OPEN_AT_CUTOFF']
 closed=[r for r in rr if r['outcome'] in ('TP','SL','BE')]
 assert D(s['won_points'])==sum(D(r['pnl_points']) for r in closed if r['outcome']=='TP')
 assert D(s['lost_points'])==sum(D(r['pnl_points']) for r in closed if r['outcome']=='SL')
 assert D(s['net_points'])==D(s['won_points'])+D(s['lost_points'])
 assert D(s['rounded_net_points'])==sum(D(r['rounded_pnl_points']) for r in closed)
 assert D(s['observed_net_points'])==sum(D(r['observed_pnl_points']) for r in closed)
 assert D(s['max_day_win'])==max((D(r['pnl_points']) for r in closed if r['outcome']=='TP'),default=D(0))
 assert D(s['max_day_loss'])==min((D(r['pnl_points']) for r in closed if r['outcome']=='SL'),default=D(0))
 assert abs(float(s['win_percent'])-100*c['TP']/int(s['entries']))<1e-10
head=['RR','ORB','Lệnh','TP','SL','BE','Chưa đóng','TP %','Tổng thắng','Tổng thua','Lãi/lỗ điểm','Ngày thắng lớn nhất','Ngày thua lớn nhất']
table=[head]+[['1:'+s['rr'],s['orb_minutes']+'m',s['entries'],s['TP'],s['SL'],s['BE'],s['open'],f"{float(s['win_percent']):.2f}%",s['won_points'],s['lost_points'],s['net_points'],s['max_day_win'],s['max_day_loss']] for s in summ]
notes=[
 '20/10/2025–19/04/2026; mở08:30 Houston; ORB1m/2m/5m. SL=½ độ rộng ORB; TP2R hoặc3R từ entry. Lời+1R thì kéo stop về entry từ record tick tiếp theo, sau đó giữ nguyên. Một lệnh/ngày mỗi bài, không re-entry.',
 'Entry GIỮ NGUYÊN từng row/timestamp/giá/chiều từ bài EMA9 SL20 TP20/30. Chờ EMA9 cùng khung đóng cắt lên đỉnh ORB hoặc xuống đáy; entry trade tick đầu tiên đủ điều kiện sau đóng nến. Không phải retest-limit.',
 'EMA xuyên phiên cùng raw contract, warmup300 nến từ Databento OHLCV1m; không reset08:30. Chưa chứng minh trùng tuyệt đối chart NT8 khác nguồn hoặc template. Entry logic và bộ lọc dữ liệu không thay đổi.',
 'Điểm chính theo TP/SL danh nghĩa, chưa phí/slippage. SL nửa box và TP3R có thể ở0,125 điểm, không phải tick MNQ hợp lệ. Stop/target làm tròn xa entry đến tick0,25 trong cột tick_rounded; RR sau làm tròn có thể khác danh nghĩa. BE nominal=0; gap qua entry vẫn có thể âm trong observed P&L. Lệnh chạm TP ngay tick kích hoạt1R kết thúc TP, không ghi thêm BE. rounded_net_points lưu riêng. Observed gap P&L lưu riêng, không bảo đảm market fill.',
 'Chỉ kiểm tra tới15:00 Houston. Lệnh chưa chạm TP/SL ghi OPEN_AT_CUTOFF, không đưa vào tổng điểm đã đóng. Tỷ lệ TP=TP/tất cả entry kể cả lệnh chưa đóng.',
 '130 ngày ứng viên;8 loại toàn phiên. ORB1m/2m loại thêm4;5m loại thêm6 do receipt trễ/degraded. Không có tín hiệu:1m0,2m3,5m5. Cùng mẫu với bài EMA trước, các ngày loại có trong CSV.',
 '13 test mới PASS tại CT/UTC, gồm4.000 chuỗi giả lập mỗi timezone đối chiếu first passage;5 mutation đều bị assertion phát hiện. Kế thừa15 test EMA cùng6 mutation ở bài nguồn. Tick file hash và count được kiểm tra lại;entry và ORB được đối chiếu raw ticks.',
 'Đã đối chiếu toàn bộ732 dòng,688 entry-variant (344 entry chung ×2 target) với bài nguồn; kiểm tra SL/TP/entry, exit thứ tự, tổng điểm, ngày thắng/thua lớn nhất và ô bảng Word. Decoder dùng chung; không cam kết dữ liệu/fill thực chính xác100%. Không sửa bot/DB/NT8.',
 'Mỗi dòng là một bài test riêng, không cộng thành danh mục. Audit reference docs/superpowers/AUDIT-CHECKLIST.md @ba50edf22.'
]
lines=['# MNQ EMA9 ORB — SL NỬA BOX / TP2R–3R / +1R KÉO BE','',notes[0],'','| '+' | '.join(head)+' |','|'+'|'.join(['---']*len(head))+'|']+['| '+' | '.join(r)+' |' for r in table[1:]]+['','## Quy tắc và xác minh','']+['- '+n for n in notes[1:]]
lines+=['','## P&L bổ sung','','| RR | ORB | Nominal | Stop/target làm tròn tick | Stop theo print quan sát |','|---|---|---:|---:|---:|']+['| '+' | '.join(s[k] for k in ('rr','orb_minutes','net_points','rounded_net_points','observed_net_points'))+' |' for s in summ]
with (p/'REPORT.md').open('x') as f:f.write('\n'.join(lines)+'\n')
def para(t):return '<w:p><w:r><w:rPr><w:sz w:val="18"/></w:rPr><w:t>'+escape(t)+'</w:t></w:r></w:p>'
w='http://schemas.openxmlformats.org/wordprocessingml/2006/main';widths=[550,550,600,550,550,550,800,750,1050,1050,1050,1250,1250]
body=para('MNQ EMA9 ORB — SL NỬA BOX / TP2R–3R / +1R KÉO BE')+para(notes[0])+para('Đơn vị: điểm MNQ, chưa phí/slippage.')
body+='<w:tbl><w:tblPr><w:tblLayout w:type="fixed"/><w:tblBorders>'+''.join(f'<w:{q} w:val="single" w:sz="4"/>' for q in ('top','left','bottom','right','insideH','insideV'))+'</w:tblBorders></w:tblPr><w:tblGrid>'+''.join(f'<w:gridCol w:w="{q}"/>' for q in widths)+'</w:tblGrid>'
for i,row in enumerate(table):
 body+='<w:tr><w:trPr><w:cantSplit/>'+('<w:tblHeader/>' if i==0 else '')+'</w:trPr>'
 for j,t in enumerate(row):body+=f'<w:tc><w:tcPr><w:tcW w:w="{widths[j]}" w:type="dxa"/></w:tcPr>'+para(t)+'</w:tc>'
 body+='</w:tr>'
body+='</w:tbl>'+''.join(para(n) for n in notes[1:])+''.join(para('RR1:'+s['rr']+' ORB'+s['orb_minutes']+'m — P&L stop/target làm tròn tick: '+s['rounded_net_points']+'; stop theo print quan sát: '+s['observed_net_points']) for s in summ)+'<w:sectPr><w:pgSz w:w="16838" w:h="11906" w:orient="landscape"/><w:pgMar w:top="650" w:right="650" w:bottom="650" w:left="650"/></w:sectPr>'
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
