"""Generate a traceable, explicitly partial-coverage research report."""
from pathlib import Path
from datetime import datetime
from zoneinfo import ZoneInfo
from collections import Counter
import json,hashlib,subprocess,csv,zipfile
ROOT=Path('/home/hoang/fleet-1017/out/day32-first-cross-20261031')
RUN=ROOT/'run-v3'
CODE=Path(__file__).parent

def main():
    audit=json.loads((RUN/'independent-audit.json').read_text())
    assert not audit['mismatches'] and audit['summary_arithmetic']=='PASS'
    a=json.loads((RUN/'summary.json').read_text());ledger=json.loads((RUN/'ledger.json').read_text());census=json.loads((RUN/'census.json').read_text())
    old=json.loads((ROOT/'run-v2/ledger.json').read_text())
    assert len(old)==len(ledger)
    assert all(all(r[k]==n[k] for k in r) for r,n in zip(old,ledger))
    run=subprocess.run(['python3','-B','-m','unittest','discover','-s',str(CODE),'-v'],capture_output=True,text=True)
    assert run.returncode==0
    with (RUN/'tests-final-report.txt').open('x') as f:f.write(run.stdout+run.stderr)
    out=ROOT/'report-final';out.mkdir(exist_ok=False)
    lines=['# Day 3.2 — EMA9/EMA34, first overnight crossover only',
    '', '## Kết quả trên phần dữ liệu đã kiểm tra', '',
    '**Mẫu chưa đủ trọn 6 tháng.** Kế hoạch: 20/10/2025–19/04/2026; 130 ngày phiên trong tuần. Đã tải 122 phiên, 109 phiên được nhận vào phép tính. 8 phiên chưa tải có báo phí và 13 phiên khác bị loại; không coi ngày thiếu dữ liệu là không có tín hiệu.',
    '', '| SL (điểm) | TP | Lệnh | Thắng TP | Thua SL | Win rate | Tổng thắng | Tổng thua | Tổng điểm gross |',
    '|---:|---:|---:|---:|---:|---:|---:|---:|---:|']
    for s in a:
        lines.append(f"| {s['stop']} | 15 | {s['entries']} | {s['TP']} | {s['SL']} | {s['win_percent_closed']:.2f}% | +{s['won_points']:.2f} | {s['lost_points']:.2f} | +{s['net_points']:.2f} |")
    lines += ['', 'Win rate = 17 / 29 = 58,620689655%. TP thắng = 17 × 15 = 255 điểm. SL15: 12 × 15 = 180 điểm, net 75. SL16: 12 × 16 = 192 điểm, net 63. Mỗi bài là thử nghiệm độc lập; không cộng hai bài thành một tài khoản.',
    '', '80/109 phiên không có entry: trong mô hình intrabar này, EMA giao cắt ngược trở lại trước khi limit retest khớp; cơ hội đầu tiên bị hủy và không sử dụng giao cắt sau. 29 phiên còn lại đều đóng TP hoặc SL, không có vị thế chưa đóng ở cutoff. Quy tắc hủy này là quy ước mô phỏng đã ghi trước khi chạy, chưa được chủ tài khoản xác nhận riêng từ video.',
    '', 'Các điểm trên chưa trừ commission, spread/queue/slippage thực tế hay độ trễ sửa lệnh. Không thể gọi đây là lợi nhuận giao dịch thực nhận. SL15 có tổng gross cao hơn SL16 trong mẫu này, nhưng chỉ 29 lệnh không đủ chứng minh phương pháp tốt hơn trong tương lai.',
    '', '## Quy tắc được chạy', '',
    'MNQ 2 phút, EMA9/EMA34 xuyên phiên, seed từ 1.000 nến 2 phút đã đóng của cùng hợp đồng. Chỉ giao cắt đầu tiên sau 17:00 Houston, tối đa một entry mỗi phiên; TP15, chạy SL15 và SL16 riêng; không BE, trailing hoặc vào lại. Giao cắt lên đặt BUY LIMIT tại EMA9; xuống đặt SELL LIMIT. Limit cập nhật theo EMA9 và làm tròn tick 0,25: buy xuống, sell lên.',
    '', 'Giờ nghiên cứu: 17:00 ngày trước → trước 08:30 ngày ghi trong bảng, America/Chicago có DST. Đây là cách định nghĩa “nguyên đêm” cho nghiên cứu; video không chốt chính xác giờ kết thúc. Hợp đồng được chọn theo volume trong 3 ngày lịch trước open, chỉ dùng các phút đã đóng; không dùng volume tương lai. Không dùng chuỗi continuous back-adjusted cho giá khớp.',
    '', 'Crossover intrabar được đo ở cuối nhóm (ts_event, sequence); không chờ nến 2 phút đóng. Video cho thấy nến đang hình thành và limit di chuyển, nhưng không chứng minh chính xác cadence của indicator/NT8. Vì vậy đây là một cách mô hình hóa rõ ràng, KHÔNG tuyên bố parity 100% với NT8/video.',
    '', 'Limit đã tồn tại được xử lý trước giá mới dùng để sửa limit. Các record cùng sự kiện không được dùng để vừa tạo lệnh vừa khớp lệnh; bracket chỉ được đánh giá ở sự kiện sau entry. Fill giả định chạm giá (touch), chưa mô phỏng hàng chờ. Stop gap dùng giá bất lợi đầu tiên quan sát được, không ép lỗ về đúng SL; trong 29 lệnh của mỗi bài này không có vượt SL. TP khớp tại giá target, không nhận thêm lợi do gap.',
    '', '## Kiểm chứng', '',
    f"[A] {audit['independent_replay_rows']} kết quả được replay bằng bộ tính độc lập float/weighted EMA seed, đối chiếu engine Decimal40; 0 sai khác. {audit['raw_entry_exit_checks']} entry/exit được so với row tick gốc. Tổng điểm được cộng lại từ ledger; TP + SL + chưa đóng khớp số entry.",
    '', '[A] 22 kiểm thử engine PASS. Ba lỗi cố ý cài vào bản sao (EMA tính lặp từng tick, phản ứng cùng sự kiện, ép stop gap) đều bị test phát hiện. Mỗi phiên đã tải có SHA256; warmup SHA256 được xác minh. Đã đọc lại metadata tình trạng dataset từ Databento và đối chiếu snapshot cũ: không có khác biệt condition.',
    '', '[A] Giữ nguyên thứ tự record từ nguồn, không tự sort để che lỗi. Phiên có receive timestamp trước event bị loại. Phiên không đủ tick đầu/cuối hoặc gap lớn cần review bị loại. Ngày degraded trong phần warmup cũng loại để không dùng seed bị ảnh hưởng. Đây là lựa chọn bảo thủ; loại dữ liệu có thể làm lệch mẫu, không suy rộng kết quả thành toàn bộ sáu tháng.',
    '', '## Từng phiên có lệnh', '',
    '| Ngày phiên | Giờ entry Houston | Hợp đồng | Hướng | Entry | TP/SL bài 15 | Điểm SL15 | TP/SL bài 16 | Điểm SL16 |',
    '|---|---|---|---|---:|---|---:|---|---:|']
    grouped={}
    for r in ledger:grouped.setdefault(r['day'],{})[r['stop_points']]=r
    for day,rs in grouped.items():
        r=rs[15];s=rs[16]
        if r['entry_row'] is None:continue
        stamp=datetime.fromtimestamp(r['entry_event_ns']/1e9,ZoneInfo('America/Chicago')).isoformat(timespec='seconds')
        lines.append(f"| {day} | {stamp} | {r['contract']} | {'BUY' if r['side']==1 else 'SELL'} | {r['entry_ticks']/4:.2f} | {r['outcome']} | {r['pnl_points']:+.2f} | {s['outcome']} | {s['pnl_points']:+.2f} |")
    lines += ['', '## Phiên không được đưa vào kết quả', '', '| Ngày | Lý do |', '|---|---|']
    for r in census:
        if r['reason']:lines.append(f"| {r['day']} | {r['reason']} |")
    lines += ['', 'NOT_DOWNLOADED_NONZERO_QUOTE: chưa tải, chưa có quyền tính phí. NO_OVERNIGHT_TICKS: file không có tick hợp đồng được chọn trong giờ nghiên cứu. DEGRADED_PROVIDER_DATE: metadata nguồn báo degraded trong giờ nghiên cứu hoặc warmup. COVERAGE_REVIEW_REQUIRED: không qua ngưỡng kiểm tra đầu/cuối phiên/gap; chưa khẳng định là mất dữ liệu hay lịch đóng cửa. TIMESTAMP_OR_RECORD_ANOMALY: phát hiện timestamp không hợp lệ theo tiêu chuẩn nghiên cứu.',
    '', '## Tái lập và bằng chứng', '',
    'Code: branch research/day32-first-cross-20261031. Input root: /home/hoang/fleet-1017/out/day32-first-cross-20261031/. Run cuối: run-v3. run-v1 là lượt kiểm tra bị loại toàn bộ do kiểm tra sequence quá rộng; KHÔNG phải kết quả chiến lược. run-v2/v3 có cùng kết quả, v3 thêm row/time lý do hủy.',
    '', 'Xem ledger.json/trades.csv cho signal_row, entry_row, exit_row, cancel_row và timestamp; row là vị trí record 0-based trong file TBBO của ngày. instrument_id + file ngày + row xác định dữ liệu gốc. census.json chứa kiểm tra coverage. Raw dữ liệu cấp phép không được đẩy lên GitHub.',
    '', 'Nguồn schema: https://databento.com/docs/schemas/tbbo ; xử lý nhiều record/sự kiện: https://databento.com/docs/examples/order-book/order-tracking ; quyền lợi Standard: https://databento.com/pricing/ . Không có chỉnh sửa bot live, NT8 hay tài khoản giao dịch.']
    md='\n'.join(lines)+'\n'
    with (out/'Day32-first-cross-report.md').open('x') as f:f.write(md)
    from reportlab.pdfbase import pdfmetrics
    from reportlab.pdfbase.ttfonts import TTFont
    from reportlab.lib.styles import getSampleStyleSheet,ParagraphStyle
    from reportlab.platypus import SimpleDocTemplate,Paragraph,Spacer,Table,TableStyle
    from reportlab.lib import colors
    from reportlab.lib.pagesizes import A4,landscape
    from xml.sax.saxutils import escape
    pdfmetrics.registerFont(TTFont('DV','/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf'))
    styles=getSampleStyleSheet();normal=ParagraphStyle('Vietnamese',fontName='DV',fontSize=9,leading=13,spaceAfter=7)
    head=ParagraphStyle('VH',parent=normal,fontSize=14,leading=18,spaceBefore=10)
    story=[];table=[]
    def flush():
        if not table:return
        width=760/len(table[0]);data=[[Paragraph(escape(c),normal) for c in row] for row in table]
        t=Table(data,colWidths=[width]*len(table[0]),repeatRows=1)
        t.setStyle(TableStyle([('BACKGROUND',(0,0),(-1,0),colors.HexColor('#e2e8f0')),('GRID',(0,0),(-1,-1),.3,colors.grey),('VALIGN',(0,0),(-1,-1),'TOP')]))
        story.extend([t,Spacer(1,10)]);table.clear()
    for line in lines:
        if line.startswith('|'):
            cells=[x.strip() for x in line.strip('|').split('|')]
            if all(set(c)<=set('-: ') for c in cells):continue
            table.append(cells);continue
        flush()
        if line:story.append(Paragraph(escape(line.lstrip('# ').replace('**','')),head if line.startswith('#') else normal))
    flush()
    SimpleDocTemplate(str(out/'Day32-first-cross-report.pdf'),pagesize=landscape(A4),leftMargin=35,rightMargin=35,topMargin=30,bottomMargin=30).build(story)
    from docx import Document
    doc=Document();i=0
    while i<len(lines):
        line=lines[i]
        if line.startswith('|'):
            rows=[]
            while i<len(lines) and lines[i].startswith('|'):
                cells=[x.strip() for x in lines[i].strip('|').split('|')]
                if not all(set(c)<=set('-: ') for c in cells):rows.append(cells)
                i+=1
            t=doc.add_table(rows=0,cols=len(rows[0]));t.style='Table Grid'
            for row in rows:
                for cell,val in zip(t.add_row().cells,row):cell.text=val
            continue
        if line.startswith('#'):doc.add_heading(line.lstrip('# '),level=min(2,len(line)-len(line.lstrip('#'))))
        elif line:doc.add_paragraph(line.replace('**',''))
        i+=1
    doc.save(out/'Day32-first-cross-report.docx')
    with zipfile.ZipFile(out/'Day32-ledger-and-audit.zip','x',zipfile.ZIP_DEFLATED) as z:
        for name in ['trades.csv','sessions.csv','ledger.json','census.json','summary.json','independent-audit.json','tests-final-report.txt','provenance.json']:z.write(RUN/name,name)
        z.write(ROOT/'mutation-tests.json','mutation-tests.json')
        z.write(ROOT/'provider-condition-fresh.json','provider-condition-fresh.json')
    from pypdf import PdfReader
    reader=PdfReader(out/'Day32-first-cross-report.pdf');text=''.join(page.extract_text() for page in reader.pages)
    assert '58.62%' in text and '75.00' in text and '63.00' in text and '2026-04-15' in text
    with (out/'artifact-verification.json').open('x') as f:json.dump({'pdf_pages':len(reader.pages),'pdf_text_checks':'PASS','artifacts':{p.name:{'bytes':p.stat().st_size,'sha256':hashlib.sha256(p.read_bytes()).hexdigest()} for p in out.iterdir() if p.is_file()}},f,indent=2)
    print('REPORT',out,'PDF pages',len(reader.pages),flush=True)
if __name__=='__main__':main()
