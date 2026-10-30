from pathlib import Path
import csv,json,hashlib,zipfile
from decimal import Decimal
from reportlab.platypus import SimpleDocTemplate,Paragraph,Spacer,Table,TableStyle,PageBreak
from reportlab.lib import colors
from reportlab.lib.styles import getSampleStyleSheet
from reportlab.lib.pagesizes import A4,landscape
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
from pypdf import PdfReader
src=Path('/home/hoang/fleet-1017/out/lucid50k-AE-lifecycle14-20261023')
out=Path('/home/hoang/fleet-1017/out/AE-full-calculation-20261029');out.mkdir(exist_ok=False)
audit=json.loads((src/'verification.json').read_text())
for n,h in audit['files'].items():assert hashlib.sha256((src/n).read_bytes()).hexdigest()==h
rows={n:list(csv.DictReader((src/(n+'.csv')).open())) for n in ['summary','stages','ledger','payouts','skipped']}
def filt(name,s):return [r for r in rows[name] if all(r[k]==s[k] for k in ['method','qty','execution'])]
def val(r,k):return int(r[k] or 0)
def money(n):return f'{int(n)/100:,.2f}'
recon=[]
for s in rows['summary']:
 ls=filt('ledger',s);ps=filt('payouts',s);st=filt('stages',s)
 assert len(ls)==int(s['trades']) and len(ps)==int(s['payout_count'])
 assert sum(val(p,'received_cents') for p in ps)==int(s['total_received_cents'])
 for l in ls:
  assert val(l,'start_cents')+val(l,'pnl_cents')-val(l,'payout_cents')==val(l,'end_cents')
  assert val(l,'fee_cents')==int(s['qty'])*100
 for p in ps:assert val(p,'received_cents')==val(p,'gross_cents')*9//10
 for stage in st:
  ll=[l for l in ls if l['stage']==stage['stage']];pp=[p for p in ps if p['stage']==stage['stage']]
  initial=5000000 if stage['phase']=='SIM' else 0
  pnl=sum(val(l,'pnl_cents') for l in ll);fees=sum(val(l,'fee_cents') for l in ll);gross=sum(val(p,'gross_cents') for p in pp)
  assert initial+pnl-gross==val(stage,'balance_cents')
  recon.append(dict(**stage,initial_cents=initial,trade_gross_cents=pnl+fees,fees_cents=fees,trade_net_cents=pnl,payout_gross_cents=gross))
with (out/'stage_calculations.csv').open('x') as f:
 w=csv.DictWriter(f,fieldnames=recon[0]);w.writeheader();w.writerows(recon)
pdfmetrics.registerFont(TTFont('VN','/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf'))
styles=getSampleStyleSheet()
for style in styles.byName.values():style.fontName='VN'
styles['BodyText'].fontSize=10;styles['BodyText'].leading=15
story=[]
def para(t,style='BodyText'):story.append(Paragraph(t,styles[style]));story.append(Spacer(1,8))
def table(head,body):
 t=Table([head]+body,repeatRows=1,hAlign='LEFT')
 t.setStyle(TableStyle([('FONTNAME',(0,0),(-1,-1),'VN'),('FONTSIZE',(0,0),(-1,-1),8),('BACKGROUND',(0,0),(-1,0),colors.HexColor('#dce8f3')),('GRID',(0,0),(-1,-1),.3,colors.grey),('TOPPADDING',(0,0),(-1,-1),5),('BOTTOMPADDING',(0,0),(-1,-1),5)]));story.append(t);story.append(Spacer(1,12))
para('A + E — FULL CALCULATION / BÁO CÁO ĐẦY ĐỦ','Title')
para('Báo cáo lập 29/10/2026. Dữ liệu 20/10/2025–19/04/2026; phiên cuối 17/04/2026. Không phải kết quả giao dịch tháng 10/2026.')
para('A = ORB 5 PHÚT BREAKOUT VÀO NGAY','Heading1')
para('Range 5 phút đầu từ 08:30 Houston; phá biên nào trước vào hướng đó. SL cả box, TP cả box; không trailing, tối đa một lệnh/ngày.')
para('E = EMA9 ORB 2 PHÚT, NẾN ĐÓNG XÁC NHẬN','Heading1')
para('EMA9 khung 2 phút tính xuyên phiên; tín hiệu đóng nến cắt lên đỉnh ORB thì mua, cắt xuống đáy thì bán. SL nửa box (làm tròn ra tick hợp lệ), TP cả box; không trailing. Mỗi ngày tối đa một lệnh.')
para('Kịch bản tài khoản đang so sánh','Heading2')
para('Bắt đầu funded SIM $50.000. Mất funded → funded mới ở phiên có tín hiệu tiếp theo. Đủ 5 payout → giả định live được duyệt phiên tiếp theo. Live bắt đầu $0, không chuyển số dư SIM sang live. Mất live → chờ 14 ngày lịch rồi funded mới. Tiếp tục đến hết mẫu. Bỏ qua evaluation và thời gian duyệt thực tế; đây là giả định, không phải quyền cấp tài khoản đã xác nhận.')
para('1. Tổng kết — USD, tiền nhận sau chia 90/10','Heading2')
for ex in ['trade_print_proxy','stress1tick']:
 para('Primary: trade-print proxy' if ex=='trade_print_proxy' else 'Stress: trượt entry/stop 1 tick và target xuyên 1 tick')
 table(['Bài','MNQ','Lệnh','SIM dùng','Live','Mất SIM/live','SIM nhận','Live nhận','Tổng nhận'],[[s['method'],s['qty'],s['trades'],s['sim_accounts'],s['live_accounts'],s['sim_breaches']+'/'+s['live_breaches'],money(s['sim_received_cents']),money(s['live_received_cents']),money(s['total_received_cents'])] for s in rows['summary'] if s['execution']==ex])
para('Tập trung so sánh 6 MNQ: A $17.595,90; E $10.070,10; chênh lệch $7.525,80. A dùng 5 funded, E dùng 3 funded. Đây là tiền nhận trước phí mua/evaluation, không phải lợi nhuận ròng hoàn chỉnh.')
story.append(PageBreak())
para('2. Công thức tính — không cộng nhầm tiền rút với số dư','Heading1')
for t in [
'MNQ: 1 tick = 0,25 điểm = $0,50/contract. P&amp;L trước phí = signed gain_ticks × $0,50 × số MNQ. Phí giả định = $1 round-trip × số MNQ/lệnh. Net P&amp;L = gross P&amp;L − phí.',
'Breach: kiểm tra equity ở từng mức thấp mới của đường đi trong lệnh, có dự phòng toàn bộ phí vòng tại entry. Equity ≤ ngưỡng lỗ thì dừng tài khoản ngay ở tick đầu vi phạm; đó là dấu giá mô hình, không bảo đảm giá thanh lý thực.',
'Số dư cuối ngày = số dư đầu + net P&amp;L − gross payout. Tiền nhận = floor(gross payout cents × 90/100). Không trừ payout lần nữa khỏi tiền nhận.',
'SIM: floor ban đầu $48.000. Cập nhật EOD: min($50.100, max(floor cũ, peak số dư EOD − $2.000)). Payout yêu cầu ít nhất 5 ngày net ≥ $150 và tổng net của chu kỳ > 0.',
'SIM gross payout = max(0, min(50% × (balance − $50.000), $2.000, balance − $52.100)), làm tròn xuống cent; chỉ rút nếu ≥ $500. Sau rút, floor $50.100, reset bộ đếm chu kỳ. Sau payout 5, giả định chuyển live.',
'Live: bắt đầu balance $0, floor −$2.000. Cập nhật EOD theo peak −$2.000; khi peak ≥ $2.000, mô hình khóa floor $100. Mỗi cuối ngày rút toàn bộ phần dư trên $2.100, không tối thiểu $500/không trần $2.000 trong giả định. Không cộng bonus.',
'Live breach ngày D: bỏ các tín hiệu có ngày nhỏ hơn D + 14 ngày lịch. Tín hiệu đầu tiên vào/sau ngày đủ 14 ngày được trade bằng funded mới. Không mở thêm tài khoản nếu đã hết mẫu.',
'Chi phí mua chưa biết: nếu C là giá mỗi funded mới trong giả định, A6 = $17.595,90 − 5C; E6 = $10.070,10 − 3C. Nếu tính tài khoản đầu là chi phí đã trả, chỉ chi phí mua lại: A trừ 4C, E trừ 2C. Công thức này KHÔNG tính các lần eval thất bại hoặc thời gian thi eval.',
'Số dư còn trong SIM không phải tiền mặt nhận được. Số dư SIM ở thời điểm chuyển live cũng không được tự cộng vào tiền rút hoặc vốn live. Mỗi dòng A/E và 3/6 MNQ là kịch bản riêng, không cộng thành danh mục.'
]:para(t)
for m in ['A','E']:
 story.append(PageBreak());para(f'3. {m} — 6 MNQ: đối chiếu từng giai đoạn','Heading1')
 rr=[r for r in recon if r['method']==m and r['qty']=='6' and r['execution']=='trade_print_proxy']
 table(['GĐ','Loại','Ngày đầu','Ngày cuối','Lệnh','Gross trade','Phí','Net trade','Gross rút','Nhận 90%','Số dư cuối'],[[r['stage'],r['phase'],r['first_day'],r['last_day'],r['trades'],money(r['trade_gross_cents']),money(r['fees_cents']),money(r['trade_net_cents']),money(r['payout_gross_cents']),money(r['received_cents']),money(r['balance_cents'])] for r in rr])
 para('Mỗi dòng: số dư đầu giai đoạn ($50.000 SIM / $0 live) + Net trade − Gross rút = Số dư cuối. Số giai đoạn bao gồm live, không phải số lần mua funded.')
 para('Chi tiết từng payout','Heading2')
 ps=[r for r in rows['payouts'] if r['method']==m and r['qty']=='6' and r['execution']=='trade_print_proxy']
 table(['GĐ','Loại','Ngày','Gross rút','Giữ lại 10%','Nhận 90%','Số dư sau rút'],[[p['stage'],p['phase'],p['day'],money(p['gross_cents']),money(val(p,'gross_cents')-val(p,'received_cents')),money(p['received_cents']),money(p['after_cents'])] for p in ps])
 total=sum(val(p,'gross_cents') for p in ps);net=sum(val(p,'received_cents') for p in ps)
 para(f'Tổng: gross payout ${money(total)} − phần chia ${money(total-net)} = thực nhận ${money(net)}.')
 skip=[r['day'] for r in rows['skipped'] if r['method']==m and r['qty']=='6' and r['execution']=='trade_print_proxy']
 para('Các ngày có tín hiệu bị bỏ qua vì cooldown: '+', '.join(skip))
 story.append(PageBreak());para(f'4. {m} — 6 MNQ: từng ngày giao dịch (USD)','Heading1')
 ls=[r for r in rows['ledger'] if r['method']==m and r['qty']=='6' and r['execution']=='trade_print_proxy']
 table(['Ngày','GĐ','Loại','Đầu ngày','Gross P&L','Phí','Net P&L','Gross rút','Thực nhận','Cuối ngày','Kết quả'],[[r['day'],r['stage'],r['phase'],money(r['start_cents']),money(val(r,'pnl_cents')+val(r,'fee_cents')),money(r['fee_cents']),money(r['pnl_cents']),money(r['payout_cents']),money(r['received_cents']),money(r['end_cents']),r['outcome']] for r in ls])
story.append(PageBreak());para('5. Kiểm chứng, nguồn và giới hạn','Heading1')
para('Lượt xuất báo cáo này kiểm tra lại SHA256 của 5 CSV; từng dòng cash conservation; phí theo quantity; payout chia 90/10; số lệnh/lần rút và số dư từng giai đoạn; bảng tổng khớp payout. Không thay đổi kết quả hoặc chạy lại raw tick.')
para('Kiểm chứng engine trước đó: 243 tests ở America/Chicago và UTC; 1.000 chuỗi lifecycle 180 ngày đối chiếu scheduler độc lập; từng stage đối chiếu Decimal. Đây không phải bảo đảm fill thật hoặc kết quả tương lai. Bộ test có ResourceWarning event-loop cleanup đã được ghi nhận, không làm test fail.')
para('Mẫu có 122 phiên hợp lệ; A 121 tín hiệu, E 115 trước cooldown. Ngày thiếu dữ liệu không phải bằng chứng không có cơ hội. Giả định phí, execution, payout và chuyển live không được cập nhật theo quy định mới trong báo cáo này. Có thể có lệnh đóng ở giá cuối trước 15:00 Houston (CUTOFF_PROXY); không coi là chạm TP/SL.')
para('Tệp CSV đính kèm ZIP chứa đủ 3/6 MNQ, primary/stress: summary, stages, ledger, payouts, skipped và stage_calculations. Mỗi ledger có exit_row để truy vết nguồn. Không đánh đồng bảng điểm danh nghĩa A–F cũ với mô hình có phí, tick rounding, cutoff và breach này.')
pdf=out/'AE-full-calculation.pdf'
SimpleDocTemplate(str(pdf),pagesize=landscape(A4),leftMargin=22,rightMargin=22,topMargin=25,bottomMargin=25).build(story)
r=PdfReader(pdf);text=''.join(p.extract_text() for p in r.pages)
assert all(x in text for x in ['17,595.90','10,070.10','6,959.25','3,773.25'])
with (out/'verification.json').open('x') as f:json.dump(dict(pages=len(r.pages),source_files_verified=audit['files'],all_daily_cash_conservation=True,all_stage_reconciliation=True,all_payout_splits=True,pdf_totals_verified=True,pdf_sha256=hashlib.sha256(pdf.read_bytes()).hexdigest()),f,indent=2)
with zipfile.ZipFile(out/'AE-full-calculation-and-ledgers.zip','x',zipfile.ZIP_DEFLATED) as z:
 for n in audit['files']:z.write(src/n,n)
 for n in ['AE-full-calculation.pdf','stage_calculations.csv','verification.json']:z.write(out/n,n)
print(out,len(r.pages),'pages')
