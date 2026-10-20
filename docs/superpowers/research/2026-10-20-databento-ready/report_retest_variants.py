"""Reconcile and publish retest-limit results only after engine verification."""
import csv,json,zipfile
from pathlib import Path
from xml.etree import ElementTree as ET
from xml.sax.saxutils import escape
import sys
import tick_orb as e
from xlsx_tick_tables import export

def report(p,baseline):
    audit=json.loads((p/'audit.json').read_text())
    assert audit['status']=='PASS_WITH_CAVEATS'
    for n,h in audit['artifacts'].items():assert e.p.digest(p/n)==h
    verify=Path(__file__).with_name('verification-retest-variants.json')
    evidence=json.loads(verify.read_text())
    assert evidence['engine_sha256']==audit['engine_sha256']
    assert all(x['exit']==0 for x in evidence['timezone_runs'].values())
    rows=list(csv.DictReader((p/'trades.csv').open()));summary=list(csv.DictReader((p/'summary.csv').open()))
    census=list(csv.DictReader((p/'sessions.csv').open()))
    old_census=list(csv.DictReader((baseline/'sessions.csv').open()))
    assert census==old_census,'Data selection changed'
    previous=list(csv.DictReader((baseline/'trades.csv').open()))
    prev={(r['day'],r['orb_minutes']):r for r in previous if r['ordering']=='provider_record_order' and r['target_rule']=='touch'}
    keys=[(r['day'],r['orb_minutes'],r['mode'],r['fill_rule']) for r in rows]
    assert len(keys)==len(set(keys))
    for r in rows:
        b=prev[(r['day'],r['orb_minutes'])]
        assert r['contract']==b['contract']
        assert r['break_row']==b['entry_row']
        if r['break_row']:
            side=1 if r['side']=='LONG' else -1
            level=float(b['orb_high'] if side==1 else b['orb_low'])
            assert float(r['limit_points'])==level
        if r['entry_row']:
            assert int(r['entry_row'])>int(r['break_row'])
            assert float(r['entry_points'])==level
            width=float(b['width_points']);risk=width if r['mode']=='box' else width/2 if r['mode']=='half_box' else 20
            assert float(r['risk_points'])==risk and float(r['width_points'])==width
            assert float(r['target_points'])==level+side*width
            assert float(r['stop_nominal_points'])==level-side*risk
            if r['be_row']:assert int(r['be_row'])>int(r['entry_row'])
        if r['exit_row']:
            assert int(r['exit_row'])>=int(r['entry_row'])
            gain=(float(r['exit_observed_points'])-level)*side
            expected=width if r['outcome']=='TP' else 0 if r['outcome']=='BE' else -risk
            assert float(r['pnl_planned_points'])==expected
            assert float(r['pnl_observed_points'])==(width if r['outcome']=='TP' else gain)
    for s in summary:
        a=[r for r in rows if all(r[k]==s[k] for k in ('mode','orb_minutes','fill_rule'))]
        assert len(a)==audit['included_days']
        for outcome,field in [('TP','TP'),('SL','SL'),('BE','BE'),('OPEN_AT_CUTOFF','open'),('EXCLUDED','excluded'),('NO_RETEST','no_retest'),('NO_BREAKOUT','no_breakout')]:
            assert sum(r['outcome']==outcome for r in a)==int(s[field])
        assert int(s['entries'])==sum(int(s[k]) for k in ('TP','SL','BE','open'))
        closed=[r for r in a if r['outcome'] in ('TP','SL','BE')]
        for field in ('planned','observed','tick_rounded'):
            assert sum(float(r['pnl_'+field+'_points']) for r in closed)==float(s['net_'+field+'_points'])
    labels={'box':'SL cả box','half_box':'SL nửa box','20_to_BE':'SL20, +20 kéo BE'}
    headers=['Stop','ORB','Lệnh','TP','SL','BE','Chưa đóng','Không retest','Điểm thắng','Điểm thua','Tổng điểm']
    def table(rule):
        out=[headers]
        for s in summary:
            if s['fill_rule']!=rule:continue
            out.append([labels[s['mode']],s['orb_minutes']+'m']+[s[k] for k in ('entries','TP','SL','BE','open','no_retest','won_points','lost_points','net_planned_points')])
        return out
    lines=['# MNQ ORB retest-limit — 6 tháng','',
       '**Đã kiểm tra tính toán; kết quả mô phỏng với các giới hạn bên dưới.**','',
       '20/10/2025–19/04/2026. Open 08:30 Houston. ORB 1m/2m/5m; '
       'giá phá biên trước, đặt limit tại biên vừa phá, chỉ khớp khi giá quay lại. '
       'Một lệnh/ngày cho mỗi bài; không tái vào lệnh. TP = cả box tính từ limit entry. '
       'SL cả box / nửa box / SL20 và lời20 kéo stop về entry.','']
    for rule,title in [('touch','Kết quả chính: giả định chạm limit là khớp'),('through_1_tick','Kiểm tra độ nhạy: phải xuyên limit 1 tick mới tính khớp')]:
        tab=table(rule);lines+=['## '+title,'','| '+' | '.join(tab[0])+' |','|'+'|'.join(['---']*len(tab[0]))+'|']
        lines+=['| '+' | '.join(row)+' |' for row in tab[1:]];lines+=['']
    notes=[
      'Điểm tính theo mức stop/target đặt sẵn, chưa trừ phí, spread hoặc độ trễ. BE được tính 0 điểm theo mô hình; khớp thật có thể trượt giá.',
      'Khớp limit khi chạm là giả định lạc quan. Xuyên 1 tick cũng không chứng minh vị trí hàng đợi hoặc khả năng khớp thật. Hai bảng không phải kết quả giao dịch broker.',
      'Giữ limit tại biên phá đầu tiên tới 15:00 Houston. Không quay lại = NO_RETEST; không phá = NO_BREAKOUT. Đã vào nhưng chưa chạm TP/SL/BE tới 15:00 = OPEN_AT_CUTOFF, không tự tạo P&L.',
      'Giá gap qua limit và stop cùng một print: tính SL ngay tại print đó theo giả định bảo thủ; không cho phép giá hồi sau đó cứu lệnh. Giá fill giả định ở limit, không hưởng cải thiện giá.',
      'TP chạm trước hoặc cùng tick kích hoạt BE thì TP kết thúc lệnh. BE chỉ có hiệu lực từ record tiếp theo; không mô phỏng độ trễ sửa lệnh.',
      'Nửa box có thể nằm ở nửa tick 0,125 điểm. Bảng chính tính đúng nửa box lý thuyết; cột net_tick_rounded_points làm tròn stop ra ngoài theo tick 0,25. Giá nửa tick không thể gửi thành lệnh MNQ thật.',
      '130 ngày trong tuần; '+str(audit['included_days'])+' đạt kiểm tra toàn phiên. Cùng hash, contract, loại trừ và breakout row IDs như bản baseline. 8 ngày bị loại toàn phiên; ORB 1m/2m/5m loại thêm 2/2/1 do ORB nhận trễ.',
      '41 test PASS ở CT và UTC, gồm 10.800 phép so sánh giả lập retest mỗi múi giờ; 8 lỗi cố ý bị phát hiện bằng assertion. '+str(len(rows))+' dòng được đối chiếu bằng phép tính first-passage riêng.',
      'Hàm đọc DBN và kiểm tra dữ liệu được dùng chung với baseline: đây không phải xác minh độc lập độ đầy đủ của dữ liệu sàn. Không tuyên bố xác minh toàn bộ bot live.',
      'Đã kiểm tra riêng: giá entry/stop/target, thứ tự row, tổng điểm, mẫu số, census, hash và mọi ô Excel. Tham chiếu AUDIT-CHECKLIST.md @ ba50edf22.',
      'Các bài test riêng trùng ngày; không cộng thành một danh mục một lệnh/ngày. Không thay đổi bot, DB, NT8 hoặc giao dịch live.'
    ]
    lines+=['## Quy tắc và kiểm tra','']+['- '+x for x in notes]+['','Engine SHA256: '+audit['engine_sha256']]
    export(p,p/'TRADES.xlsx')
    ns='{http://schemas.openxmlformats.org/spreadsheetml/2006/main}'
    with zipfile.ZipFile(p/'TRADES.xlsx') as z:
        for i,n in enumerate(('trades.csv','sessions.csv'),1):
            expected=list(csv.reader((p/n).open()));xml=ET.fromstring(z.read(f'xl/worksheets/sheet{i}.xml'))
            actual=[[''.join(c.itertext()) for c in row] for row in xml.findall(ns+'sheetData/'+ns+'row')]
            assert actual==expected
    with (p/'REPORT.md').open('x') as f:f.write('\n'.join(lines)+'\n')
    with (p/'verification-retest-variants.json').open('xb') as f:f.write(verify.read_bytes())
    # Editable Word report, landscape, with both tables and all caveats.
    w='http://schemas.openxmlformats.org/wordprocessingml/2006/main'
    def para(t):return '<w:p><w:r><w:rPr><w:sz w:val="18"/></w:rPr><w:t>'+escape(t)+'</w:t></w:r></w:p>'
    body=para('MNQ ORB RETEST LIMIT — 20/10/2025–19/04/2026')
    for rule in ('touch','through_1_tick'):
        body+=para('Fill assumption: '+rule)+'<w:tbl>'
        for row in table(rule):body+='<w:tr>'+''.join('<w:tc>'+para(x)+'</w:tc>' for x in row)+'</w:tr>'
        body+='</w:tbl>'
    body+=''.join(para(x) for x in notes)+'<w:sectPr><w:pgSz w:w="16838" w:h="11906" w:orient="landscape"/><w:pgMar w:top="600" w:right="600" w:bottom="600" w:left="600"/></w:sectPr>'
    with zipfile.ZipFile(p/'REPORT.docx','x',zipfile.ZIP_DEFLATED) as z:
        z.writestr('[Content_Types].xml','<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>')
        z.writestr('_rels/.rels','<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>')
        z.writestr('word/document.xml',f'<w:document xmlns:w="{w}"><w:body>{body}</w:body></w:document>')
    with zipfile.ZipFile(p/'REPORT.docx') as z:
        assert z.testzip() is None
        xml=ET.fromstring(z.read('word/document.xml'))
        actual=[[''.join(c.itertext()) for c in row] for row in xml.findall('.//{'+w+'}tr')]
        assert actual==table('touch')+table('through_1_tick')
    with zipfile.ZipFile(p/'FULL-REPORT.zip','x',zipfile.ZIP_DEFLATED) as z:
        for n in ('REPORT.md','REPORT.docx','summary.csv','trades.csv','sessions.csv','TRADES.xlsx','audit.json','verification-retest-variants.json'):z.write(p/n,n)
    print((p/'REPORT.md').read_text())
if __name__=='__main__':report(Path(sys.argv[1]),Path(sys.argv[2]))
