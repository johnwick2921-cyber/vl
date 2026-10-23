from pathlib import Path
import csv,json,hashlib,html,zipfile
ROOT=Path('/home/hoang/fleet-1017/out')
OUT=ROOT/'AE-two-scenarios-complete-20261023';OUT.mkdir(exist_ok=False)
folders={'rebuy':ROOT/'lucid50k-AE-rebuy-20261023','cycle':ROOT/'lucid50k-AE-lifecycle14-20261023','live':ROOT/'lucid50k-EDA-live-20261023','daily':ROOT/'lucid50k-AE-live-daily-excess-20261023','sim':ROOT/'lucid50k-EDA-payout-20261023'}
verified={}
for name,folder in folders.items():
 audit=json.loads((folder/('verification.json' if name in ['cycle','daily'] else 'audit.json')).read_text())
 for file,h in audit.get('artifacts',audit.get('files',{})).items():
  assert hashlib.sha256((folder/file).read_bytes()).hexdigest()==h
  verified[str(folder/file)]=h

def read(group,file='summary.csv'):return list(csv.DictReader((folders[group]/file).open()))
def money(v):return '—' if v in ('',None) else f'${int(v)/100:,.2f}'
def table(headers,rows):return ['| '+' | '.join(headers)+' |','|'+'|'.join(['---']*len(headers))+'|']+['| '+' | '.join(str(x) for x in r)+' |' for r in rows]+['']
def select(rows,m,execution='trade_print_proxy'):return [r for r in rows if r['method']==m and r['execution']==execution]
allrows={k:read(k) for k in folders}
for m in ['A','E']:
 lines=[f'# Báo cáo đầy đủ — phương pháp {m}', '', 'Mẫu 20/10/2025–19/04/2026; phiên giao dịch cuối 17/04/2026. Báo cáo lập 23/10/2026. Mỗi dòng là một tài khoản/kịch bản độc lập, không cộng các dòng thành một danh mục.', '', ('A: ORB5m breakout vào ngay; SL cả box, TP cả box.' if m=='A' else 'E: EMA9 ORB2m xác nhận đóng nến; SL nửa box, TP cả box; không trailing.'),'', '## 1. Hai trường hợp tiếp tục trade đến hết mẫu', '', '**TH1 — chỉ funded, mua lại:** mất funded HOẶC đủ payout thứ 5 thì bắt đầu funded $50.000 mới ở phiên có tín hiệu kế tiếp. Không chuyển live. Đây là giả định nghiên cứu.', '', '**TH2 — funded → live → mua lại:** đủ payout thứ 5 thì giả định duyệt live phiên kế tiếp; live bắt đầu $0. Mất live thì chờ 14 ngày lịch, bắt đầu funded mới từ phiên có tín hiệu đầu tiên vào/sau ngày đủ 14 ngày. Mất funded thì mua lại phiên kế tiếp. Live rút hết phần dư trên $2.100 mỗi cuối ngày.', '', '**Cả hai bỏ qua evaluation và thời gian xét duyệt. Chưa trừ giá mua tài khoản/evaluation vì chưa có giá. Tiền nhận đã chia 90/10. Không cộng bonus. Không phải mô phỏng đầy đủ quyền mua tài khoản theo hợp đồng Lucid.**','']
 for execution,label in [('trade_print_proxy','Kết quả chính'),('stress1tick','Độ nhạy: stress 1 tick')]:
  rows=[]
  for q in ['3','6']:
   a=next(r for r in select(allrows['rebuy'],m,execution) if r['qty']==q);b=next(r for r in select(allrows['cycle'],m,execution) if r['qty']==q)
   rows.extend([['TH1',q,a['accounts_used'],0,a['breached_accounts'],0,a['trades'],0,a['payout_count'],money(a['received_cents']),money(0),money(a['received_cents']),'SIM',money(a['last_balance_cents'])],['TH2',q,b['sim_accounts'],b['live_accounts'],b['sim_breaches'],b['live_breaches'],b['trades'],b['skipped_signals'],b['payout_count'],money(b['sim_received_cents']),money(b['live_received_cents']),money(b['total_received_cents']),b['last_phase'],money(b['last_balance_cents'])]])
  lines+=['### '+label,'']+table(['TH','MNQ','Acc SIM','Acc live','Mất SIM','Mất live','Lệnh','Bỏ qua chờ','Lần rút','SIM nhận','Live nhận','Tổng nhận','Cuối mẫu','Số dư cuối'],rows)
 lines+=['Số dư SIM gồm vốn danh nghĩa $50.000; số dư live tính từ $0. Số dư cuối không phải tiền đã rút. Số acc SIM gồm tài khoản đầu tiên; acc live là chuyển đổi, không cộng như một lần mua funded.','', '## 2. Hai cách cash-out trên tài khoản live đầu tiên','', 'Cặp này giữ nguyên giai đoạn SIM đầu tiên: (C1) không rút live; (C2) mỗi cuối ngày rút phần dư trên $2.100, không tối thiểu $500 và không trần $2.000 trong giả định. Chỉ theo dõi live đầu tiên đến mất tài khoản hoặc hết mẫu; KHÔNG mua lại trong bảng này. Tổng nhận = tiền SIM đầu tiên đã nhận + tiền live đã nhận. Không cộng với mục 1.','']
 for execution,label in [('trade_print_proxy','Kết quả chính'),('stress1tick','Stress 1 tick')]:
  rows=[]
  for q in ['3','6']:
   sim=next(r for r in select(allrows['sim'],m,execution) if r['qty']==q)
   no=next(r for r in select(allrows['live'],m,execution) if r['qty']==q and r['policy']=='no_live_withdrawals' and r['lock_trigger_cents']=='200000')
   daily=next(r for r in select(allrows['daily'],m,execution) if r['qty']==q)
   for label2,r in [('C1 không rút',no),('C2 rút dư $2.100',daily)]:
    livecash=int(r['received_cents'] or 0)
    rows.append([label2,q,r['first_day'] or 'Chưa lên live',r['last_day'] or '—',r['trades'],money(sim['received_cents']),money(livecash),money(int(sim['received_cents'])+livecash),money(r['balance_cents']),r['status']])
  lines+=['### '+label,'']+table(['Cash-out','MNQ','Bắt đầu live','Đến','Lệnh live','SIM đã nhận','Live đã nhận','Tổng nhận','Số dư live cuối','Trạng thái'],rows)
 lines+=['## 3. Từng tài khoản/giai đoạn — kết quả chính','']
 for group,title,file in [('rebuy','TH1 — chỉ funded','accounts.csv'),('cycle','TH2 — có live và chờ 14 ngày','stages.csv')]:
  rows=[]
  for r in select(read(group,file),m):
   rows.append([r['qty'],r.get('account',r.get('stage')),r.get('phase','SIM'),r['first_day'],r['last_day'],r['trades'],r['payout_count'],money(r['received_cents']),money(r.get('balance_cents',r.get('ending_balance_cents'))),r['status']])
  lines+=['### '+title,'']+table(['MNQ','Số','Loại','Từ','Đến','Lệnh','Lần rút','Đã nhận','Số dư','Trạng thái'],rows)
 lines+=['## 4. Từng lần cash-out — kết quả chính','']
 for group,title in [('rebuy','TH1'),('cycle','TH2')]:
  rows=[]
  for r in select(read(group,'payouts.csv'),m):rows.append([r['qty'],r.get('account',r.get('stage')),r.get('phase','SIM'),r['day'],money(r['gross_cents']),money(r['received_cents']),money(r['after_cents'])])
  lines+=['### '+title,'']+table(['MNQ','Acc/giai đoạn','Loại','Ngày','Trừ khỏi acc','Thực nhận 90%','Số dư sau rút'],rows)
  for q in ['3','6']:
   total=sum(int(r['received_cents']) for r in select(read(group,'payouts.csv'),m) if r['qty']==q)
   s=next(r for r in select(allrows[group],m) if r['qty']==q)
   assert total==int(s['received_cents'] if group=='rebuy' else s['total_received_cents'])
 lines+=['## 5. Kiểm chứng và giới hạn','', '243 tests PASS ở America/Chicago và UTC; 8 kịch bản lifecycle khớp chạy lại và bộ tính Decimal độc lập; 1.000 chuỗi 180 ngày kiểm tra scheduler. Tệp nguồn đối chiếu SHA256; tổng payout chi tiết khớp bảng tổng. Báo cáo này tổng hợp dữ liệu đã kiểm chứng, không replay lại raw tick.', '', '122 phiên dữ liệu hợp lệ; ngày thiếu không được coi là không có cơ hội. Phí mô hình $1 round-trip/MNQ. Giá khớp là trade-print proxy; stress 1 tick không chứng minh fill thực. Ngưỡng lỗ xét cả đường đi trong lệnh; không chỉ P&L đóng lệnh. Fee đủ vòng được dự phòng tại entry. Live MLL bắt đầu −$2.000, khóa $100 theo trigger lợi nhuận $2.000 của mô hình; giữ lại $2.100 là lựa chọn của chủ tài khoản. Không có cam kết accuracy 100% thị trường hay quyền rút thực tế.', '', 'MLL_BREACH/LIVE_MLL_BREACH = vi phạm ngưỡng; END_OF_VALID_SAMPLE = còn tới hết dữ liệu hợp lệ; LIVE_TRANSITION_AFTER_5_PAYOUTS = giả định duyệt live, không phải xác nhận của Lucid. NO_FIFTH_PAYOUT_TRANSITION = chưa đạt mốc 5 lần rút.']
 with (OUT/f'{m}-complete.md').open('x') as f:f.write('\n'.join(lines)+'\n')
 parts=[];in_table=False
 for line in lines:
  if line.startswith('|'):
   if line.startswith('|---'):continue
   if not in_table:parts.append('<table>');in_table=True
   parts.append('<tr>'+''.join('<td>'+html.escape(c.strip())+'</td>' for c in line.strip('|').split('|'))+'</tr>')
  else:
   if in_table:parts.append('</table>');in_table=False
   level=len(line)-len(line.lstrip('#'))
   parts.append(f'<h{level}>'+html.escape(line[level:].strip())+f'</h{level}>' if level else '<p>'+html.escape(line)+'</p>')
 with (OUT/f'{m}-complete.html').open('x') as f:f.write('<meta charset="utf-8"><style>@page{size:A4 landscape;margin:10mm}body{font:10px Arial}table{border-collapse:collapse;width:100%}td{border:1px solid #aaa;padding:4px}tr{break-inside:avoid}h2,h3{break-after:avoid}p{line-height:1.4}</style>'+''.join(parts))
with (OUT/'source-verification.json').open('x') as f:json.dump(verified,f,indent=2)
print(OUT)
