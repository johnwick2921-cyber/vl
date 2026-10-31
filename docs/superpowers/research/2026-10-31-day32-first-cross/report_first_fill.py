from pathlib import Path
from datetime import datetime
from zoneinfo import ZoneInfo
from collections import Counter
import json,subprocess,hashlib,zipfile
ROOT=Path('/home/hoang/fleet-1017/out/day32-first-cross-20261031')
RUN=ROOT/'run-first-fill-v1'
CODE=Path(__file__).parent

def main():
    audit=json.loads((RUN/'independent-audit.json').read_text());assert not audit['mismatches']
    summaries=json.loads((RUN/'summary.json').read_text());rows=json.loads((RUN/'ledger.json').read_text());census=json.loads((RUN/'census.json').read_text())
    test=subprocess.run(['python3','-B',str(CODE/'check_first_fill.py')],capture_output=True,text=True);assert test.returncode==0
    with (RUN/'tests.txt').open('x') as f:f.write(test.stdout+test.stderr)
    out=ROOT/'report-first-fill';out.mkdir(exist_ok=False)
    sections=[]
    sections.append(('title','Day 3.2 — LỆNH KHỚP ĐẦU TIÊN MỖI ĐÊM'))
    sections.append(('p','Bản sửa theo xác nhận của chủ tài khoản ngày 31/10/2026. Thay thế bảng 29 lệnh trước đó đã rút lại. Trước khi khớp, tiếp tục tìm giao cắt mới và first retest EMA9; khi có giao cắt ngược thì đổi hướng. Sau lệnh đã khớp đầu tiên, giữ TP/SL và nghỉ đêm đó, dù thắng hay thua.'))
    sections.append(('p','Kế hoạch 20/10/2025–19/04/2026: 130 ngày phiên. Đã tải 122 phiên, 109 phiên qua các tiêu chuẩn dữ liệu; chưa đủ trọn sáu tháng. 8 phiên chưa tải do báo phí khác $0; 13 phiên còn lại bị loại vì tình trạng nguồn/warmup, timestamp hoặc coverage. Những phiên đó không được coi là ngày không có tín hiệu.'))
    table=[['SL','TP','Lệnh','TP thắng','SL thua','Win rate','Tổng thắng','Tổng thua','Tổng điểm']]
    for s in summaries:
        table.append([str(s['stop']),'15',str(s['entries']),str(s['TP']),str(s['SL']),f"{s['win_percent_closed']:.2f}%",f"{s['won_points']:+.2f}",f"{s['lost_points']:.2f}",f"{s['net_points']:+.2f}"])
    sections.append(('table',table))
    for s in summaries:
        sections.append(('p',f"SL{s['stop']}: {s['TP']} TP × 15 = {s['won_points']:.2f} điểm thắng; cộng từng lệnh SL = {s['lost_points']:.2f}; tổng = {s['net_points']:.2f}. Win rate = TP / (TP + SL). Chưa đóng: {s['open']}. Không có entry trong phiên hợp lệ: {s['included_sessions']-s['entries']}. Lỗ một lệnh lớn nhất: {s['max_loss_points']:.2f} điểm. Không cộng hai bài độc lập thành một tài khoản."))
    sections.append(('heading','Giả định và giới hạn'))
    sections.append(('p','MNQ khung 2 phút, EMA9/34 xuyên phiên. Giờ nghiên cứu 17:00 Houston ngày trước đến trước 08:30 ngày phiên, dùng America/Chicago và DST. Seed 1.000 nến 2 phút đã đóng cùng hợp đồng. Chọn hợp đồng từ volume có sẵn trong ba ngày lịch trước open, không nhìn volume tương lai.'))
    sections.append(('p','Crossover và limit cập nhật trong nến, ở cuối nhóm sự kiện (ts_event, sequence). Video không xác định chính xác cadence của indicator hoặc độ trễ sửa lệnh; chưa chứng minh parity NT8. Limit mua làm tròn xuống, bán làm tròn lên theo tick 0,25. Limit đã tồn tại được xét trước khi sửa từ giá mới. Không dùng cùng sự kiện để vừa tạo vừa khớp lệnh; bảo vệ bắt đầu từ sự kiện sau entry.'))
    sections.append(('p','Fill giả định chạm giá (touch), chưa mô phỏng hàng chờ/độ trễ broker. Điểm là gross, chưa trừ commission và slippage thực tế. Stop gap lấy giá bất lợi đầu tiên quan sát được, không ép mọi lỗ bằng SL; TP giới hạn tại giá target. Quy tắc một entry không bảo đảm ngày nào cũng có fill: kết quả đếm từ dữ liệu, không ép lệnh.'))
    sections.append(('heading','Kiểm chứng'))
    sections.append(('p',f"23 kiểm thử PASS trên module first_fill.py, gồm đổi hướng khi chưa khớp, không đánh tiếp sau SL, không ép entry nếu thiếu tín hiệu, EMA trong nến và thứ tự sự kiện. Bộ replay độc lập không import engine, dùng weighted seed/float đối chiếu Decimal40: {audit['independent_replay_rows']} kết quả, {audit['raw_entry_exit_checks']} entry/exit gốc, không sai khác. Tổng bảng được cộng lại từ từng lệnh."))
    sections.append(('p','Hash SHA256 từng file tải và warmup đã kiểm tra. Giữ thứ tự record nguồn. Dataset-condition được kiểm tra lại qua API, không khác snapshot dùng trong chạy. Những ngày nguồn degraded hoặc warmup bị ảnh hưởng được loại bảo thủ. Đây là kiểm chứng phần mềm và phép tính theo giả định, không phải cam kết khớp thực tế 100%.'))
    sections.append(('heading','Từng ngày có lệnh'))
    daytable=[['Ngày phiên','Entry Houston','Hướng','Hợp đồng','Entry','SL15 outcome','SL15 điểm','SL16 outcome','SL16 điểm']]
    by={}
    for r in rows:by.setdefault(r['day'],{})[r['stop_points']]=r
    for day,rr in by.items():
        a,b=rr[15],rr[16]
        if a['entry_row'] is None:continue
        assert a['entry_row']==b['entry_row'] and a['entry_ticks']==b['entry_ticks']
        ts=datetime.fromtimestamp(a['entry_event_ns']/1e9,ZoneInfo('America/Chicago')).isoformat(timespec='seconds')
        daytable.append([day,ts,'BUY' if a['side']==1 else 'SELL',a['contract'],f"{a['entry_ticks']/4:.2f}",a['outcome'],str(a['pnl_points']),b['outcome'],str(b['pnl_points'])])
    sections.append(('table',daytable))
    sections.append(('heading','Ngày không đưa vào kết quả'))
    sections.append(('table',[['Ngày','Lý do']]+[[r['day'],r['reason']] for r in census if r['reason']]))
    sections.append(('p','Mỗi lệnh có signal_row, entry_row, exit_row và danh sách crosses trước entry trong ledger.json. Chỉ số row là 0-based trong file TBBO của ngày, kết hợp instrument_id để truy vết. Code: research/day32-first-cross-20261031; first_fill.py, run_first_fill.py, verify_first_fill.py. Dữ liệu: run-first-fill-v1. Raw tick có giấy phép không đưa vào repo hoặc gói báo cáo. Không thay đổi bot live/NT8.'))
    md=[]
    for kind,value in sections:
        if kind in ('title','heading'):md+=['# '+value if kind=='title' else '## '+value,'']
        elif kind=='p':md += [value,'']
        else:
            for i,row in enumerate(value):
                md.append('| '+' | '.join(row)+' |')
                if i==0:md.append('| '+' | '.join(['---']*len(row))+' |')
            md.append('')
    with (out/'REPORT.md').open('x') as f:f.write('\n'.join(md))
    from reportlab.pdfbase import pdfmetrics
    from reportlab.pdfbase.ttfonts import TTFont
    from reportlab.platypus import SimpleDocTemplate,Paragraph,Table,TableStyle,Spacer
    from reportlab.lib.styles import ParagraphStyle
    from reportlab.lib.pagesizes import A4,landscape
    from reportlab.lib import colors
    from xml.sax.saxutils import escape
    pdfmetrics.registerFont(TTFont('DV','/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf'))
    normal=ParagraphStyle('n',fontName='DV',fontSize=9,leading=13,spaceAfter=8)
    heading=ParagraphStyle('h',parent=normal,fontSize=14,leading=18,spaceBefore=10,keepWithNext=True)
    story=[]
    from docx import Document
    doc=Document()
    for kind,value in sections:
        if kind=='table':
            data=[[Paragraph(escape(s),normal) for s in row] for row in value]
            t=Table(data,colWidths=[760/len(value[0])]*len(value[0]),repeatRows=1)
            t.setStyle(TableStyle([('BACKGROUND',(0,0),(-1,0),colors.HexColor('#e2e8f0')),('GRID',(0,0),(-1,-1),.3,colors.grey),('VALIGN',(0,0),(-1,-1),'TOP')]))
            story.extend([t,Spacer(1,10)])
            tab=doc.add_table(rows=0,cols=len(value[0]));tab.style='Table Grid'
            for row in value:
                for c,v in zip(tab.add_row().cells,row):c.text=v
        else:
            story.append(Paragraph(escape(value),normal if kind=='p' else heading))
            if kind=='p':doc.add_paragraph(value)
            else:doc.add_heading(value,0 if kind=='title' else 1)
    SimpleDocTemplate(str(out/'Day32-FIRST-FILLED-TRADE.pdf'),pagesize=landscape(A4),leftMargin=35,rightMargin=35,topMargin=30,bottomMargin=30).build(story)
    doc.save(out/'Day32-FIRST-FILLED-TRADE.docx')
    with zipfile.ZipFile(out/'Day32-FIRST-FILLED-evidence.zip','x',zipfile.ZIP_DEFLATED) as z:
        for n in ['trades.csv','sessions.csv','ledger.json','summary.json','independent-audit.json','tests.txt','provenance.json']:z.write(RUN/n,n)
    from pypdf import PdfReader
    pdf=PdfReader(out/'Day32-FIRST-FILLED-TRADE.pdf');text=''.join(p.extract_text() for p in pdf.pages)
    assert 'LỆNH KHỚP ĐẦU TIÊN' in text
    for s in summaries:assert f"{s['net_points']:+.2f}" in text
    with (out/'verification.json').open('x') as f:json.dump({'pages':len(pdf.pages),'artifacts':{p.name:{'bytes':p.stat().st_size,'sha256':hashlib.sha256(p.read_bytes()).hexdigest()} for p in out.iterdir() if p.is_file()}},f,indent=2)
    print(out,flush=True)
if __name__=='__main__':main()
