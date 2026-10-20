"""Final body-confirmation review and owner artifacts; exclusive output only."""
import csv,json,zipfile,sys
from pathlib import Path
from xml.etree import ElementTree as E
from xml.sax.saxutils import escape
import tick_orb as e
from xlsx_tick_tables import export

def run(p):
    audit=json.loads((p/'audit.json').read_text());assert audit['status']=='PASS_WITH_CAVEATS'
    for n,h in audit['artifacts'].items():assert e.p.digest(p/n)==h
    verification=Path(__file__).with_name('verification-body-retest.json')
    evidence=json.loads(verification.read_text());assert evidence['engine_sha256']==audit['engine_sha256']
    assert all(x['exit']==0 for x in evidence['timezone_runs'].values())
    rows=list(csv.DictReader((p/'trades.csv').open()));summ=list(csv.DictReader((p/'summary.csv').open()))
    old=Path('/home/hoang/fleet-1017/out/orb-tick-six-month-20261020-reviewed')
    assert list(csv.DictReader((p/'sessions.csv').open()))==list(csv.DictReader((old/'sessions.csv').open()))
    baseline={(r['day'],r['orb_minutes']):r for r in csv.DictReader((old/'trades.csv').open()) if r['ordering']=='provider_record_order' and r['target_rule']=='touch'}
    assert len(rows)==len({(r['day'],r['orb_minutes'],r['mode'],r['body_rule']) for r in rows})
    same_candle=0
    for r in rows:
        b=baseline[(r['day'],r['orb_minutes'])];assert b['contract']==r['contract']
        if not r['confirm_row']:continue
        side=1 if r['side']=='LONG' else -1
        boundary=float(b['orb_high'] if side==1 else b['orb_low'])
        op,cl=float(r['body_open_points']),float(r['body_close_points'])
        assert op!=cl and (cl-boundary)*side>0
        a,c=(op-boundary)*side,(cl-boundary)*side
        fraction=(max(a,c)-max(0,min(a,c)))/abs(op-cl)
        assert fraction==float(r['outside_fraction'])
        assert fraction>=.5 if r['body_rule']=='half_body' else (a>0 and c>0)
        assert float(r['limit_points'])==boundary
        assert int(r['confirm_end_ns'])-int(r['confirm_start_ns'])==int(r['orb_minutes'])*e.MINUTE
        if r['entry_row']:
            assert int(r['entry_row'])>int(r['confirm_row'])
            assert int(r['entry_event_ns'])>=int(r['eligible_event_ns'])>=int(r['confirm_end_ns'])
            assert float(r['entry_points'])==boundary
            width=float(b['width_points']);risk=width if r['mode']=='box' else width/2 if r['mode']=='half_box' else 20
            assert float(r['target_points'])==boundary+side*width
            assert float(r['stop_nominal_points'])==boundary-side*risk
            if r['be_row']:assert int(r['be_row'])>int(r['entry_row'])
        if r['exit_row']:
            assert int(r['exit_row'])>=int(r['entry_row'])
            pnl=width if r['outcome']=='TP' else 0 if r['outcome']=='BE' else -risk
            assert float(r['pnl_planned_points'])==pnl
    for s in summ:
        group=[r for r in rows if all(r[k]==s[k] for k in ('mode','orb_minutes','body_rule'))]
        assert len(group)==audit['included_days']
        for outcome,k in [('TP','TP'),('SL','SL'),('BE','BE'),('OPEN_AT_CUTOFF','open'),('NO_RETEST','no_retest'),('NO_CONFIRMATION','no_confirmation'),('EXCLUDED','excluded')]:
            assert sum(r['outcome']==outcome for r in group)==int(s[k])
        assert int(s['entries'])==sum(int(s[k]) for k in ('TP','SL','BE','open'))
        closed=[r for r in group if r['outcome'] in ('TP','SL','BE')]
        for name in ('planned','observed','tick_rounded'):
            assert sum(float(r['pnl_'+name+'_points']) for r in closed)==float(s['net_'+name+'_points'])
    labels={'box':'SL cả box','half_box':'SL nửa box','20_to_BE':'SL20; lời20 kéo BE'}
    head=['Stop','ORB','Lệnh','TP','SL','BE','Chưa đóng','Điểm thắng','Điểm thua','Tổng điểm']
    def table(rule):return [head]+[[labels[s['mode']],s['orb_minutes']+'m']+[s[k] for k in ('entries','TP','SL','BE','open','won_points','lost_points','net_planned_points')] for s in summ if s['body_rule']==rule]
    notes=[
      'ĐÚNG YÊU CẦU BODY ONLY. Báo cáo này thay thế bài chờ thời gian, vốn hiểu sai ý chủ sở hữu. Không dùng chờ30/60/150/300 giây để xác nhận.',
      'Giai đoạn20/10/2025–19/04/2026. Open08:30 Houston. ORB1m dùng nến1m, ORB2m dùng nến2m, ORB5m dùng nến5m. Nến được căn từ08:30; ORB là nến đầu tiên.',
      '½ thân: nến đã đóng ngoài ORB, ít nhất50% chiều dài thân open-close nằm ngoài biên. Full thân: cả open và close nằm nghiêm ngặt ngoài cùng biên. Không tính râu. Doji bỏ qua vì thân có chiều dài0.',
      'Chọn nến đóng đầu tiên đạt điều kiện của từng bài. Close phía trên ORB → buy limit tại đỉnh; close phía dưới → sell limit tại đáy. Không thêm điều kiện màu nến: thân đỏ hoàn toàn phía trên vẫn đạt điều kiện full; thân xanh hoàn toàn phía dưới cũng vậy.',
      'Chỉ cho phép fill sau khi nến xác nhận hoàn thành và dữ liệu nến đã nhận; không dùng lần chạm bên trong nến xác nhận. 0 entry trong cùng nến xác nhận. Chạm limit sau đó được giả định khớp ở giá biên.',
      'Một lệnh/ngày mỗi bài; không tái vào, không chuyển limit sang hướng khác. TP luôn bằng cả box từ entry. SL cả box / nửa box / SL20, khi lời20 kéo stop về entry. TP chạm trước hoặc cùng lúc kích hoạt BE thì TP kết thúc lệnh.',
      'Tới15:00 Houston: không có nến đủ điều kiện = NO_CONFIRMATION; có xác nhận nhưng chưa khớp = NO_RETEST; đã khớp nhưng chưa chạm exit = OPEN_AT_CUTOFF, không gán P&L đã đóng.',
      'Điểm chính theo stop/target đặt sẵn, chưa phí hoặc trượt giá. BE tính0 theo mô hình. Half-box có thể ở nửa tick; kết quả làm tròn ra ngoài theo tick0,25 và observed crossing PnL nằm riêng trong summary.csv.',
      'Giả định chạm limit là khớp không chứng minh fill thật; không mô phỏng hàng đợi, bid/ask hay độ trễ broker. Gap qua cả limit và stop trên tick fill tính SL ngay theo giả định bảo thủ.',
      '130 ngày trong tuần;122 đạt kiểm tra toàn phiên. Cùng dữ liệu, contract và loại trừ với baseline; ORB1m/2m/5m loại thêm2/2/1 trường hợp do ORB nhận trễ.',
      '62 test PASS mỗi múi giờ CT/UTC;5.400 so sánh body giả lập mỗi múi giờ;6 lỗi cố ý về body/close/receipt bị phát hiện. '+str(len(rows))+' dòng dữ liệu thật đối chiếu bằng first-passage riêng, bao gồm xác nhận nến độc lập.',
      'Kiểm tra lại body fraction, biên limit, entry/SL/TP, thứ tự thời gian, tổng điểm, mẫu số, hash, census và mọi ô Word/Excel. Decoder dùng chung: không phải kiểm chứng độc lập tính đầy đủ dữ liệu sàn hoặc toàn bộ bot live.',
      'Audit reference: docs/superpowers/AUDIT-CHECKLIST.md @ba50edf22. Mỗi bài riêng, có thể trùng ngày; không gộp thành danh mục một lệnh/ngày. Không sửa DB/NT8/bot live.'
    ]
    lines=['# MNQ ORB RETEST — XÁC NHẬN THÂN NẾN ĐÃ ĐÓNG','',notes[0],'',notes[1],'']
    for rule in ('half_body','full_body'):
        tab=table(rule);lines+=['## '+('Ít nhất ½ thân ngoài ORB' if rule=='half_body' else 'Toàn bộ thân ngoài ORB'),'','| '+' | '.join(tab[0])+' |','|'+'|'.join(['---']*len(head))+'|']+['| '+' | '.join(row)+' |' for row in tab[1:]]+['']
    lines+=['## Các ngày không vào lệnh','','| Điều kiện | ORB | Không xác nhận | Không retest |','|---|---|---:|---:|']
    for s in summ:
        if s['mode']=='box':lines.append('| '+' | '.join(s[k] for k in ('body_rule','orb_minutes','no_confirmation','no_retest'))+' |')
    lines+=['','## Quy tắc và xác minh','']+['- '+x for x in notes[2:]]+['','Engine SHA256: '+audit['engine_sha256']]
    export(p,p/'TRADES.xlsx')
    ns='{http://schemas.openxmlformats.org/spreadsheetml/2006/main}'
    with zipfile.ZipFile(p/'TRADES.xlsx') as z:
        for i,n in enumerate(('trades.csv','sessions.csv'),1):
            x=E.fromstring(z.read(f'xl/worksheets/sheet{i}.xml'))
            assert [[''.join(c.itertext()) for c in r] for r in x.findall(ns+'sheetData/'+ns+'row')]==list(csv.reader((p/n).open()))
    with (p/'REPORT.md').open('x') as f:f.write('\n'.join(lines)+'\n')
    with (p/'verification.json').open('xb') as f:f.write(verification.read_bytes())
    w='http://schemas.openxmlformats.org/wordprocessingml/2006/main'
    def para(t):return '<w:p><w:r><w:rPr><w:sz w:val="18"/></w:rPr><w:t>'+escape(t)+'</w:t></w:r></w:p>'
    body=para('MNQ ORB RETEST — BODY ONLY, NẾN ĐÃ ĐÓNG')+''.join(para(n) for n in notes[:2])
    widths=[2200,550,700,550,550,550,950,1750,1750,1750]
    for rule in ('half_body','full_body'):
        body+=para('Ít nhất ½ thân ngoài ORB' if rule=='half_body' else 'Toàn bộ thân ngoài ORB')+'<w:tbl><w:tblPr><w:tblLayout w:type="fixed"/><w:tblBorders>'+''.join(f'<w:{x} w:val="single" w:sz="4" w:color="CBD5E1"/>' for x in ('top','left','bottom','right','insideH','insideV'))+'</w:tblBorders></w:tblPr><w:tblGrid>'+''.join(f'<w:gridCol w:w="{x}"/>' for x in widths)+'</w:tblGrid>'
        for i,row in enumerate(table(rule)):
            body+='<w:tr><w:trPr><w:cantSplit/>'+('<w:tblHeader/>' if i==0 else '')+'</w:trPr>'
            for j,x in enumerate(row):body+=f'<w:tc><w:tcPr><w:tcW w:w="{widths[j]}" w:type="dxa"/></w:tcPr>'+para(x)+'</w:tc>'
            body+='</w:tr>'
        body+='</w:tbl>'
    body+=''.join(para(n) for n in notes[2:])+'<w:sectPr><w:pgSz w:w="16838" w:h="11906" w:orient="landscape"/><w:pgMar w:top="600" w:right="600" w:bottom="600" w:left="600"/></w:sectPr>'
    with zipfile.ZipFile(p/'REPORT.docx','x',zipfile.ZIP_DEFLATED) as z:
        z.writestr('[Content_Types].xml','<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>')
        z.writestr('_rels/.rels','<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>')
        z.writestr('word/document.xml',f'<w:document xmlns:w="{w}"><w:body>{body}</w:body></w:document>')
    with zipfile.ZipFile(p/'REPORT.docx') as z:
        assert z.testzip() is None;x=E.fromstring(z.read('word/document.xml'))
        assert [[''.join(c.itertext()) for c in r.findall('{'+w+'}tc')] for r in x.findall('.//{'+w+'}tr')]==table('half_body')+table('full_body')
    with zipfile.ZipFile(p/'FULL-REPORT.zip','x',zipfile.ZIP_DEFLATED) as z:
        for n in ('REPORT.md','REPORT.docx','TRADES.xlsx','summary.csv','trades.csv','sessions.csv','audit.json','verification.json'):z.write(p/n,n)
    print((p/'REPORT.md').read_text())
if __name__=='__main__':run(Path(sys.argv[1]))
