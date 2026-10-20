import csv,json,zipfile,sys
from pathlib import Path
from xml.etree import ElementTree as E
from xml.sax.saxutils import escape
import tick_orb as e
from xlsx_tick_tables import export

def run(p):
    audit=json.loads((p/'audit.json').read_text())
    assert audit['status']=='PASS_WITH_CAVEATS'
    for n,h in audit['artifacts'].items():assert e.p.digest(p/n)==h
    verify=Path(__file__).with_name('verification-delayed-retest.json')
    assert json.loads(verify.read_text())['engine_sha256']==audit['engine_sha256']
    rows=list(csv.DictReader((p/'trades.csv').open()));summ=list(csv.DictReader((p/'summary.csv').open()))
    baseline=Path('/home/hoang/fleet-1017/out/orb-tick-six-month-20261020-reviewed')
    assert list(csv.DictReader((p/'sessions.csv').open()))==list(csv.DictReader((baseline/'sessions.csv').open()))
    assert len(rows)==len({(r['day'],r['orb_minutes'],r['mode'],r['delay']) for r in rows})
    for r in rows:
        if not r['entry_row']:continue
        halves=1 if r['delay']=='half_candle' else 2
        due=int(r['break_event_ns'])+int(r['orb_minutes'])*e.MINUTE*halves//2
        assert int(r['eligible_event_ns'])==due and int(r['entry_event_ns'])>=due
        assert int(r['entry_row'])>int(r['break_row'])
        if halves==2:assert r['same_candle_as_break']=='False'
        assert float(r['entry_points'])==float(r['limit_points'])
        if r['exit_row']:assert int(r['exit_row'])>=int(r['entry_row'])
    for s in summ:
        a=[r for r in rows if all(r[k]==s[k] for k in ('mode','orb_minutes','delay'))]
        assert len(a)==audit['included_days']
        for outcome,k in [('TP','TP'),('SL','SL'),('BE','BE'),('OPEN_AT_CUTOFF','open'),('NO_RETEST','no_retest'),('EXCLUDED','excluded'),('NO_BREAKOUT','no_breakout')]:
            assert sum(r['outcome']==outcome for r in a)==int(s[k])
        assert int(s['entries'])==sum(int(s[k]) for k in ('TP','SL','BE','open'))
        closed=[r for r in a if r['outcome'] in ('TP','SL','BE')]
        assert sum(float(r['pnl_planned_points']) for r in closed)==float(s['net_planned_points'])
        assert sum(r.get('same_candle_as_break')=='True' for r in a)==int(s['same_candle_fills'])
        assert sum(r.get('activation_marketable')=='True' for r in a)==int(s['marketable_at_activation'])
    labels={'box':'SL cả box','half_box':'SL nửa box','20_to_BE':'SL20; lời20 kéo BE'}
    head=['Stop','ORB','Lệnh','TP','SL','BE','Chưa đóng','Không khớp','Điểm thắng','Điểm thua','Tổng điểm']
    def table(delay):
        return [head]+[[labels[s['mode']],s['orb_minutes']+'m']+[s[k] for k in ('entries','TP','SL','BE','open','no_retest','won_points','lost_points','net_planned_points')] for s in summ if s['delay']==delay]
    notes=[
     '6 tháng 20/10/2025–19/04/2026. ORB mở lúc 08:30 Houston. TP cả box, ba kiểu SL giữ nguyên. Một lệnh/ngày mỗi bài; không tái vào.',
     'Đợi từ tick phá biên đầu tiên. ½ nến: ORB1m đợi30s, ORB2m đợi60s, ORB5m đợi150s. Một nến: đợi60/120/300s. Đây là đợi thời gian, không yêu cầu thân nến đóng ngoài ORB.',
     'Limit chỉ được xét khớp từ thời điểm đủ thời gian. Các lần chạm trước đó không tính. Giữ cùng biên phá đầu tiên; không đổi chiều. Giả định chạm là khớp ở giá limit.',
     'Nếu tới giờ đặt lệnh mà giá đã ở phía trong box, mô hình có thể khớp ngay trên tick đầu tiên đủ giờ, tại giá limit giả định (không hưởng cải thiện giá). Đây không phải bằng chứng giá đã ra ngoài rồi quay lại thêm lần nữa. Marketable-at-activation và số khớp cùng cây nến được ghi riêng trong summary.csv.',
     '½ nến vẫn có thể khớp trong cùng cây nến phá. Đợi đủ một nến bảo đảm entry không cùng cây nến phá. Nếu muốn bắt buộc chờ nến đóng hoặc yêu cầu giá còn ngoài box thì đó là quy tắc khác, chưa áp dụng.',
     'Mốc đặt lệnh và khớp được xấp xỉ bằng tick giao dịch đủ giờ đầu tiên, không phải mô phỏng bid/ask, hàng đợi hoặc latency. Chạm limit không bảo đảm fill thật.',
     'Điểm theo stop/target đặt sẵn, chưa phí hoặc trượt giá. BE tính0; mức khớp quan sát riêng trong trades.csv. SL nửa box lý thuyết có thể nằm ở nửa tick; tổng khi làm tròn ra ngoài theo0,25 điểm có trong summary.csv.',
     'Chưa đóng lúc15:00 được ghi OPEN_AT_CUTOFF và loại khỏi P&L đã đóng. Không retest tính tới15:00; không tự tạo exit. Gap qua limit và stop tính SL ngay theo mô hình bảo thủ.',
     '130 ngày được kiểm tra,122 đạt kiểm tra toàn phiên; thêm2/2/1 trường hợp ORB1m/2m/5m bị loại do dữ liệu ORB nhận trễ. Cùng bộ dữ liệu và breakout baseline.',
     '50 test mỗi múi giờ CT/UTC;10.800 so sánh delay giả lập mỗi múi giờ;4 lỗi thời gian cố ý bị phát hiện. Tất cả '+str(len(rows))+' dòng dữ liệu thật được đối chiếu bằng thuật toán first-passage riêng; cùng decoder nên không phải kiểm chứng độc lập độ đầy đủ dữ liệu sàn.',
     'Mọi số tổng, thứ tự thời gian, cờ cùng nến, hash, và ô bảng Word/Excel đã được đối chiếu. AUDIT-CHECKLIST.md @ ba50edf22. Không thay đổi bot live/DB/NT8.'
    ]
    lines=['# MNQ ORB retest: đợi ½ nến và một nến','']+notes[:2]+['']
    for delay in ('half_candle','full_candle'):
        tab=table(delay);lines+=['## '+('Đợi ½ nến' if delay=='half_candle' else 'Đợi một nến'),'','| '+' | '.join(tab[0])+' |','|'+'|'.join(['---']*len(head))+'|']+['| '+' | '.join(row)+' |' for row in tab[1:]]+['']
    lines+=['## Kiểm tra thời điểm khớp','','| Đợi | ORB | Khớp cùng nến phá | Giá đã vào box lúc kích hoạt |','|---|---|---:|---:|']
    for s in summ:
        if s['mode']=='box':lines.append('| '+' | '.join(s[k] for k in ('delay','orb_minutes','same_candle_fills','marketable_at_activation'))+' |')
    lines+=['','## Giả định và xác minh','']+['- '+x for x in notes[2:]]+['','Engine SHA256: '+audit['engine_sha256']]
    export(p,p/'TRADES.xlsx')
    ns='{http://schemas.openxmlformats.org/spreadsheetml/2006/main}'
    with zipfile.ZipFile(p/'TRADES.xlsx') as z:
        for i,n in enumerate(('trades.csv','sessions.csv'),1):
            x=E.fromstring(z.read(f'xl/worksheets/sheet{i}.xml'))
            assert [[''.join(c.itertext()) for c in row] for row in x.findall(ns+'sheetData/'+ns+'row')]==list(csv.reader((p/n).open()))
    with (p/'REPORT.md').open('x') as f:f.write('\n'.join(lines)+'\n')
    with (p/'verification.json').open('xb') as f:f.write(verify.read_bytes())
    w='http://schemas.openxmlformats.org/wordprocessingml/2006/main'
    def para(t):return '<w:p><w:r><w:rPr><w:sz w:val="18"/></w:rPr><w:t>'+escape(t)+'</w:t></w:r></w:p>'
    body=para('MNQ ORB RETEST — ĐỢI ½ NẾN / MỘT NẾN')+''.join(para(x) for x in notes[:2])
    widths=[2100,500,650,500,500,500,850,850,1600,1700,1700]
    for delay in ('half_candle','full_candle'):
        body+=para('Đợi ½ nến' if delay=='half_candle' else 'Đợi một nến')+'<w:tbl><w:tblPr><w:tblLayout w:type="fixed"/><w:tblBorders>'+''.join(f'<w:{x} w:val="single" w:sz="4" w:color="CBD5E1"/>' for x in ('top','left','bottom','right','insideH','insideV'))+'</w:tblBorders></w:tblPr><w:tblGrid>'+''.join(f'<w:gridCol w:w="{x}"/>' for x in widths)+'</w:tblGrid>'
        for i,row in enumerate(table(delay)):
            body+='<w:tr><w:trPr><w:cantSplit/>'+('<w:tblHeader/>' if i==0 else '')+'</w:trPr>'
            for j,x in enumerate(row):body+=f'<w:tc><w:tcPr><w:tcW w:w="{widths[j]}" w:type="dxa"/></w:tcPr>'+para(x)+'</w:tc>'
            body+='</w:tr>'
        body+='</w:tbl>'
    body+=''.join(para(x) for x in notes[2:])+'<w:sectPr><w:pgSz w:w="16838" w:h="11906" w:orient="landscape"/><w:pgMar w:top="600" w:right="600" w:bottom="600" w:left="600"/></w:sectPr>'
    with zipfile.ZipFile(p/'REPORT.docx','x',zipfile.ZIP_DEFLATED) as z:
        z.writestr('[Content_Types].xml','<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>')
        z.writestr('_rels/.rels','<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>')
        z.writestr('word/document.xml',f'<w:document xmlns:w="{w}"><w:body>{body}</w:body></w:document>')
    with zipfile.ZipFile(p/'REPORT.docx') as z:
        assert z.testzip() is None
        x=E.fromstring(z.read('word/document.xml'))
        assert [[''.join(c.itertext()) for c in r.findall('{'+w+'}tc')] for r in x.findall('.//{'+w+'}tr')]==table('half_candle')+table('full_candle')
    with zipfile.ZipFile(p/'FULL-REPORT.zip','x',zipfile.ZIP_DEFLATED) as z:
        for n in ('REPORT.md','REPORT.docx','TRADES.xlsx','summary.csv','trades.csv','sessions.csv','audit.json','verification.json'):z.write(p/n,n)
    print((p/'REPORT.md').read_text())
if __name__=='__main__':run(Path(sys.argv[1]))
