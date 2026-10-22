"""Reconcile source rows and publish offline EMA report, exclusive create."""
import csv,json,zipfile
from decimal import Decimal as D
from pathlib import Path
from collections import Counter
from xml.sax.saxutils import escape
from xml.etree import ElementTree as ET
import tick_orb as e
from xlsx_tick_tables import export
p=Path('/home/hoang/fleet-1017/out/orb-ema9-20261022')
a=json.loads((p/'audit.json').read_text());v=json.loads((p/'engine-verification-final.json').read_text())
assert a['engine_sha256']==v['engine_sha256']==e.p.digest(Path(__file__).with_name('ema_orb.py'))
for n,h in a['artifacts'].items():assert e.p.digest(p/n)==h
rows=list(csv.DictReader((p/'trades.csv').open()));summary=list(csv.DictReader((p/'summary.csv').open()));sessions=list(csv.DictReader((p/'sessions.csv').open()))
assert len(rows)==732 and len(sessions)==130
assert len({(r['day'],r['orb_minutes'],r['tp_distance']) for r in rows})==732
old=Path('/home/hoang/fleet-1017/out/orb-tick-six-month-20261020-reviewed')
assert sessions==list(csv.DictReader((old/'sessions.csv').open()))
by={(r['day'],r['orb_minutes'],r['tp_distance']):r for r in rows}
for r in rows:
 if r['signal_row']:
  prev,now=D(r['ema_previous_points']),D(r['ema_signal_points'])
  assert (prev<=D(r['orb_high'])<now) if r['side']=='LONG' else (prev>=D(r['orb_low'])>now)
 if not r['entry_row']:continue
 assert int(r['entry_row'])>int(r['signal_row'])
 assert int(r['entry_event_ns'])>=int(r['eligible_ns'])>=int(r['signal_end_ns'])
 _,opened,closed=e.boundaries(r['day']);assert opened<int(r['entry_event_ns'])<closed
 side=1 if r['side']=='LONG' else -1
 assert (D(r['entry_points'])-D(r['stop_points']))*side==20
 assert (D(r['target_points'])-D(r['entry_points']))*side==D(r['tp_distance'])
 if r['exit_row']:
  assert int(r['exit_row'])>int(r['entry_row']) and int(r['exit_event_ns'])>=int(r['entry_event_ns'])
  assert D(r['pnl_points'])==(D(r['tp_distance']) if r['outcome']=='TP' else -20)
 other=by[r['day'],r['orb_minutes'],'30' if r['tp_distance']=='20' else '20']
 assert all(r[k]==other[k] for k in ('entry_row','entry_points','signal_row','side'))
for s in summary:
 rr=[r for r in rows if r['orb_minutes']==s['orb_minutes'] and r['tp_distance']==s['target']];c=Counter(r['outcome'] for r in rr)
 assert len(rr)==122
 for k,o in [('TP','TP'),('SL','SL'),('open','OPEN_AT_CUTOFF'),('no_signal','NO_SIGNAL'),('excluded','EXCLUDED')]:assert int(s[k])==c[o]
 assert int(s['entries'])==c['TP']+c['SL']+c['OPEN_AT_CUTOFF']
 assert D(s['won_points'])==c['TP']*int(s['target'])
 assert D(s['lost_points'])==-20*c['SL']
 assert D(s['net_points'])==D(s['won_points'])+D(s['lost_points'])==sum(D(r['pnl_points']) for r in rr if r['pnl_points'])
 assert D(s['observed_net_points'])==sum(D(r['observed_pnl_points']) for r in rr if r['observed_pnl_points'])
 assert abs(float(s['win_percent'])-100*c['TP']/int(s['entries']))<1e-10
quality=sum(r['reason'].startswith('provider_degraded') for r in rows)
notes=[
 '20/10/2025–19/04/2026; Houston America/Chicago, DST aware. ORB là 1/2/5 phút đầu từ08:30, EMA9 cùng khung. Một lệnh/ngày mỗi bài; không BE, không re-entry.',
 'EMA9 alpha=2/(9+1)=0,2. Chỉ nến đã đóng: EMA trước ≤ đỉnh ORB và EMA mới > đỉnh → LONG; EMA trước ≥ đáy và EMA mới < đáy → SHORT. Chạm bằng biên chưa phải cắt. Không yêu cầu màu nến. Không dùng râu hoặc tín hiệu trong nến.',
 'Không lấy nến tạo ORB làm tín hiệu. Tín hiệu đầu tiên sau đó; entry là trade tick đầu tiên sau khi nến hoàn tất và tất cả dữ liệu xác nhận đã nhận. Không vào ở giá đóng nến xác nhận hoặc giá biên ORB giả định.',
 'EMA xuyên phiên, cùng raw contract được chọn trước open. Warmup300 nến đã đóng từ OHLCV1m Databento, gộp2m/5m theo mốc thời gian; không reset08:30. Sai số seed được chặn <1e-20 tick theo dải warmup. Thiếu nến do không có giao dịch không tự thêm giá. Không cam kết trùng từng giá EMA trên NT8 khác nguồn/template phiên.',
 'TP20 hoặc30, SL20 tính từ entry. Báo cáo chính dùng fill danh nghĩa; stop-gap theo tick quan sát lưu riêng observed_net_points. Entry trade-price là proxy market fill, không có bid/ask, queue, độ trễ broker, phí/slippage. Không phải cam kết kết quả khớp thực.',
 'Chỉ đánh giá đến15:00 Houston. OPEN_AT_CUTOFF không tự chốt lệnh hoặc tính P&L; lần chạy này không còn lệnh mở. Tỷ lệ TP = TP/tổng entry.',
 '130 ngày trong tuần;8 loại ở kiểm tra toàn phiên. Loại thêm theo từng ORB vì receipt ORB trễ hoặc ngày Databento degraded nằm trong phiên/warmup. Bộ lọc degraded áp dụng bảo thủ cấp dataset, không khẳng định MNQ riêng bị lỗi. Ngày không tín hiệu không phải lệnh thua.',
 '15 test PASS ở CT và UTC;600 ca giả lập đối chiếu mỗi timezone. 6 mutation đều bị phát hiện bằng assertion, không phải crash. '+str(len(rows)-quality)+' dòng qua hai bộ tính;'+str(quality)+' dòng bị chặn trước mô phỏng vì chất lượng. audit.json oracle_checked_rows là tổng candidate732, không phải số gọi oracle thực; số chính xác nằm ở final-verification.json.',
 'Đối chiếu hash, mẫu ngày, contract, crossing, bracket distances, receipt-time, entry sau close, TP20/TP30 cùng entry, số lệnh và tổng điểm. Shared tick decoder; không chứng minh dữ liệu sàn hoàn hảo hoặc toàn bộ bot live. Không sửa DB/bot/NT8.',
 'Giả định nghiên cứu: ETH không reset, raw contract không back-adjust;300 nến warmup. Report reference docs/superpowers/AUDIT-CHECKLIST.md @ba50edf22. Không cộng các dòng thành một danh mục.'
]
head=['ORB','SL','TP','Lệnh','Thắng','Thua','TP %','Tổng thắng','Tổng thua','Lãi/lỗ','Ngày thắng lớn nhất','Ngày thua lớn nhất']
table=[head]
for s in summary:table.append([s['orb_minutes']+'m',s['stop'],s['target'],s['entries'],s['TP'],s['SL'],f"{float(s['win_percent']):.2f}%",s['won_points'],s['lost_points'],s['net_points'],s['max_day_win'],s['max_day_loss']])
lines=['# MNQ ORB — EMA9 CROSS ON CLOSED CANDLE','',notes[0],'','| '+' | '.join(head)+' |','|'+'|'.join(['---']*len(head))+'|']+['| '+' | '.join(r)+' |' for r in table[1:]]+['','## Quy tắc và xác minh','']+['- '+n for n in notes[1:]]
lines+=['','## Các trường hợp bị loại','','| Ngày | ORB | Lý do |','|---|---|---|']
for r in rows:
 if r['tp_distance']=='20' and r['outcome']=='EXCLUDED':lines.append('| '+r['day']+' | '+r['orb_minutes']+' | '+r['reason']+' |')
lines+=['','## Ngày không có tín hiệu','','| Ngày | ORB |','|---|---|']
for r in rows:
 if r['tp_distance']=='20' and r['outcome']=='NO_SIGNAL':lines.append('| '+r['day']+' | '+r['orb_minutes']+' |')
with (p/'REPORT.md').open('x') as f:f.write('\n'.join(lines)+'\n')
def para(t):return '<w:p><w:r><w:rPr><w:sz w:val="18"/></w:rPr><w:t>'+escape(t)+'</w:t></w:r></w:p>'
w='http://schemas.openxmlformats.org/wordprocessingml/2006/main'
body=para('MNQ ORB — EMA9 CẮT BIÊN KHI NẾN ĐÓNG')+para(notes[0])+para('Đơn vị: điểm MNQ; TP/SL danh nghĩa, chưa phí/slippage.')
widths=[600,550,550,650,650,650,850,1100,1100,1100,1300,1300]
body+='<w:tbl><w:tblPr><w:tblLayout w:type="fixed"/><w:tblBorders>'+''.join(f'<w:{q} w:val="single" w:sz="4"/>' for q in ('top','left','bottom','right','insideH','insideV'))+'</w:tblBorders></w:tblPr><w:tblGrid>'+''.join(f'<w:gridCol w:w="{q}"/>' for q in widths)+'</w:tblGrid>'
for i,row in enumerate(table):
 body+='<w:tr><w:trPr><w:cantSplit/>'+('<w:tblHeader/>' if i==0 else '')+'</w:trPr>'
 for j,t in enumerate(row):body+=f'<w:tc><w:tcPr><w:tcW w:w="{widths[j]}" w:type="dxa"/></w:tcPr>'+para(t)+'</w:tc>'
 body+='</w:tr>'
body+='</w:tbl>'+''.join(para(n) for n in notes[1:])+ '<w:sectPr><w:pgSz w:w="16838" w:h="11906" w:orient="landscape"/><w:pgMar w:top="650" w:right="650" w:bottom="650" w:left="650"/></w:sectPr>'
with zipfile.ZipFile(p/'REPORT.docx','x',zipfile.ZIP_DEFLATED) as z:
 z.writestr('[Content_Types].xml','<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>')
 z.writestr('_rels/.rels','<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>')
 z.writestr('word/document.xml',f'<w:document xmlns:w="{w}"><w:body>'+body+'</w:body></w:document>')
with zipfile.ZipFile(p/'REPORT.docx') as z:
 assert z.testzip() is None
 root=ET.fromstring(z.read('word/document.xml'));ns={'w':w}
 assert [[''.join(c.itertext()) for c in r.findall('w:tc',ns)] for r in root.findall('.//w:tr',ns)]==table
export(p,p/'TRADES.xlsx')
e.p.write_json(p/'final-verification.json',dict(status='PASS_WITH_DISCLOSED_ASSUMPTIONS',candidate_rows=len(rows),oracle_checked_rows=len(rows)-quality,provider_quality_excluded_rows=quality,all_trade_arithmetic_reconciles=True,all_entry_pairs_match=True,no_same_candle_entries=True,word_table_cells_match_csv=True,engine_sha256=a['engine_sha256']))
with zipfile.ZipFile(p/'FULL-REPORT.zip','x',zipfile.ZIP_DEFLATED) as z:
 for n in ('REPORT.docx','REPORT.md','TRADES.xlsx','trades.csv','summary.csv','sessions.csv','audit.json','final-verification.json','engine-verification-final.json','warmup-request.json','warmup-receipt.json','warmup-validation.json','dataset-condition.json'):z.write(p/n,n)
print(json.dumps(summary,indent=2));print('VERIFICATION PASS',len(rows)-quality,quality)
