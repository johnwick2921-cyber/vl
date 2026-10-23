from pathlib import Path
import csv,html
out=Path('/home/hoang/fleet-1017/out/lucid50k-AE-lifecycle14-20261023')
rows=list(csv.DictReader((out/'summary.csv').open()))
stages=list(csv.DictReader((out/'stages.csv').open()))
lines=['# A/E — mua lại sau mất tài khoản, chờ 14 ngày sau mất live','', 'Mẫu: 20/10/2025–19/04/2026; phiên cuối có giao dịch 17/04/2026. Mỗi dòng là một kịch bản riêng, không cộng chung.', '', '**Đây là mô hình giả định bỏ qua evaluation và thời gian xét duyệt live, không phải cam kết cấp tài khoản của Lucid.** SIM mất → funded mới phiên kế tiếp; payout thứ 5 → live phiên kế tiếp; live mất → chờ 14 ngày lịch, funded mới ở phiên có tín hiệu đầu tiên từ ngày đủ 14 ngày. Không mua tài khoản khi đã hết dữ liệu.', '', 'A: ORB5m phá biên vào ngay, SL cả box, TP cả box. E: EMA9 ORB2m xác nhận đóng nến, SL nửa box, TP cả box, không trailing.', '', 'Live bắt đầu $0, ngưỡng lỗ ban đầu −$2.000; rút phần dư trên $2.100 mỗi cuối ngày, chia 90/10. Không áp mức rút tối thiểu $500 hoặc trần $2.000 cho live trong giả định này. Không cộng bonus. SIM áp quy tắc payout của engine trước. Phí giả định $1 round-trip/MNQ. Chi phí mua tài khoản chưa biết nên chưa trừ.', '']
def money(x):return f'${int(x)/100:,.2f}'
for execution,title in [('trade_print_proxy','Kết quả chính — trade-print / touch proxy'),('stress1tick','Kiểm tra stress — 1 tick')]:
 lines+=['## '+title,'','| Bài | MNQ | Acc SIM mua | Acc live | Mất SIM | Mất live | Lệnh | Bỏ qua khi chờ | Tiền SIM nhận | Tiền live nhận | Tổng đã nhận |','|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|']
 for r in rows:
  if r['execution']==execution:lines.append('| '+' | '.join([r['method'],r['qty'],r['sim_accounts'],r['live_accounts'],r['sim_breaches'],r['live_breaches'],r['trades'],r['skipped_signals'],money(r['sim_received_cents']),money(r['live_received_cents']),money(r['total_received_cents'])])+' |')
 lines+=['']
lines+=['## Từng giai đoạn — kết quả chính','','| Bài | MNQ | Giai đoạn | Loại | Từ | Đến | Lệnh | Tiền nhận | Số dư cuối | Trạng thái |','|---|---:|---:|---|---|---|---:|---:|---:|---|']
for r in stages:
 if r['execution']=='trade_print_proxy':lines.append('| '+' | '.join([r['method'],r['qty'],r['stage'],r['phase'],r['first_day'],r['last_day'],r['trades'],money(r['received_cents']),money(r['balance_cents']),r['status']])+' |')
lines+=['','Số dư cuối chưa phải tiền được rút. Số dư SIM gồm vốn danh nghĩa $50.000; số dư live tính từ $0. Tiền nhận trong bảng đã chia 90/10, chưa trừ phí mua/evaluation. LIVE_TRANSITION_AFTER_5_PAYOUTS chỉ là giả định chuyển live, không phải xác nhận được duyệt.', '', '## Kiểm chứng và giới hạn','', '[A] Hash paths.json khớp audit nguồn. Mỗi giai đoạn đối chiếu oracle Decimal độc lập; bảo toàn tiền, không trùng ngày, và mọi tín hiệu hợp lệ thuộc đúng một nhóm: đã trade hoặc bị bỏ qua vì cooldown. 23 tests PASS ở America/Chicago và UTC, gồm 6.000 chuỗi ngẫu nhiên mỗi lần chạy. Test biên 14 ngày, không trade lại cùng ngày, không tạo tài khoản rỗng cuối mẫu đều PASS.', '', 'Dùng lại tick-path đã lưu, không tải/replay lại raw tick trong lượt này. 122 phiên dữ liệu hợp lệ trong 130 ngày thường; ngày thiếu dữ liệu không được xem là phiên không có cơ hội. A có 121 tín hiệu, E có 115 tín hiệu. Trade-print không chứng minh khớp lệnh thực tế hay thứ tự hàng đợi; stress chỉ là độ nhạy. Kết quả chưa xác định chi phí evaluation, thời gian vượt evaluation, quyền quay lại funded hoặc duyệt live. Không dùng kết quả này như lợi nhuận thực tế đảm bảo.', '', 'Tệp kiểm tra: verification.json; summary.csv; stages.csv; ledger.csv; payouts.csv; skipped.csv. Tham chiếu kiểm toán repo: docs/superpowers/AUDIT-CHECKLIST.md @ ba50edf22.']
text='\n'.join(lines)+'\n'
(out/'REPORT.md').write_text(text)
parts=[];table=False
for line in lines:
 if line.startswith('|'):
  if line.startswith('|---'):continue
  if not table:parts.append('<table>');table=True
  parts.append('<tr>'+''.join('<td>'+html.escape(c.strip())+'</td>' for c in line.strip('|').split('|'))+'</tr>')
 else:
  if table:parts.append('</table>');table=False
  parts.append('<p>'+html.escape(line)+'</p>')
if table:parts.append('</table>')
with (out/'report.html').open('x') as f:f.write('<meta charset="utf-8"><style>@page{size:A4 landscape;margin:12mm}body{font:11px Arial}table{border-collapse:collapse;width:100%}td{border:1px solid #aaa;padding:4px}tr{break-inside:avoid}p{line-height:1.35}</style>'+''.join(parts))
