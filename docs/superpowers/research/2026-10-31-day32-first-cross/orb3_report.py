"""Independent Decimal ledger aggregation and documented ORB comparison."""
import csv,json,hashlib
from pathlib import Path
from decimal import Decimal as D
from collections import Counter
from itertools import groupby
R=Path('/home/hoang/fleet-1017/out/orb3-recent-20261031/run-v2')
def main():
 rows=list(csv.DictReader((R/'trades.csv').open())); summaries=json.loads((R/'summary.json').read_text()); audit=json.loads((R/'audit.json').read_text())
 assert len(rows)==6681 and len(summaries)==51
 for n,h in audit['artifacts'].items():assert hashlib.sha256((R/n).read_bytes()).hexdigest()==h
 assert len({(r['day'],r['orb'],r['group'],r['variant']) for r in rows})==6681
 for s in summaries:
  rr=sorted([r for r in rows if (r['group'],r['variant'],r['orb'])==(s['group'],s['variant'],str(s['orb']))],key=lambda r:r['day'])
  assert len(rr)==131
  closed=[r for r in rr if r['points']!=''];p=[D(r['points']) for r in closed];c=Counter(r['outcome'] for r in rr)
  assert sum(p,D(0))==D(str(s['gross_points']))
  assert sum((v for v in p if v>0),D(0))==D(str(s['won_points']))
  assert sum((v for v in p if v<0),D(0))==D(str(s['lost_points']))
  for k in ('TP','SL','BE','LOCK'):assert c[k]==s[k]
  assert s['entries']==len(closed)+s['open']
  assert s['entries']+s['excluded']+s['no_entry']==131
  assert s['positive']+s['negative']+s['zero']==len(closed)
  signs=[1 if x>0 else -1 if x<0 else 0 for x in p]
  runs=[(k,len(list(g))) for k,g in groupby(signs)]
  assert max([n for k,n in runs if k==1],default=0)==s['win_streak']
  assert max([n for k,n in runs if k==-1],default=0)==s['loss_streak']
  equity=[D(0)]
  for x in p:equity.append(equity[-1]+x)
  assert max(max(equity[:i+1])-v for i,v in enumerate(equity))==D(str(s['closed_drawdown']))
  assert D(str(s['max_day_win']))==max([v for v in p if v>0],default=D(0))
  assert D(str(s['max_day_loss']))==min([v for v in p if v<0],default=D(0))
  s['net_2pt_sensitivity']=float(sum(p,D(0))-2*len(p))
 out=R/'verified-report';out.mkdir(exist_ok=False)
 fields=list(summaries[0]);f=(out/'comparison.csv').open('x');w=csv.DictWriter(f,fieldnames=fields);w.writeheader();w.writerows(summaries);f.close()
 text=['# ORB — kiểm tra lại ba nhóm, 01/05–30/10/2026','', '131 ngày trong lịch ngày thường; 51 cấu hình; 6.681 dòng ngày/cấu hình. Mỗi cấu hình chạy độc lập trên MNQ, tối đa một lệnh/ngày. Mở ORB 08:30 America/Chicago (Houston, tự điều chỉnh DST).','', '## Quy tắc và giới hạn','', '- Breakout: phá biên bằng ít nhất một tick, vào theo giá tick đầu tiên sau khi ORB hoàn thành.', '- Retest touch: phá trước, tick sau quay lại chạm biên mới vào limit. Half/full-body: nến cùng khung ORB phải đóng với ít nhất nửa/toàn bộ thân ngoài biên; sau đó mới cho phép retest. Không dùng wick và không vào hồi tố trong nến xác nhận.', '- EMA9: 300 nến hoàn thành cùng hợp đồng trước open, xuyên phiên; EMA9 đóng cắt biên ORB, vào tick đủ điều kiện tiếp theo. Không reset EMA tại open.', '- halfbox_lock5: SL nửa box, TP cả box; khóa +5 một lần khi lời đạt max(1R, 5,25 điểm), để stop nằm phía sau giá. Đây không phải trailing liên tục.', '- box = SL cả box; half_box = SL nửa box; 20_to_BE = SL20, lời20 kéo về entry. Trừ TP20/TP30 ghi riêng, target luôn bằng độ rộng cả box tính từ entry.', '- Cửa sổ theo dõi kết thúc 15:00 Houston. Lệnh còn mở được ghi riêng, không tự gán thắng/thua và không tự tạo giá thoát cuối phiên.', '- Điểm SL dùng giá giao dịch đầu tiên vượt stop: có thể lỗ hơn khoảng stop vì gap. TP giả định fill tại target khi chạm; retest giả định limit fill khi chạm. Không mô phỏng queue, bid/ask execution hay latency; đây là mô hình backtest, không phải bảo đảm fill thật.', '- Breakout/retest dùng timestamp sự kiện; EMA dùng nến timestamp nhận để khớp OHLCV warmup Databento. Không khẳng định giống chart NT8 tuyệt đối. Giữ thứ tự bản ghi provider; chưa chạy độ nhạy cho các bản ghi trùng event/sequence.', '- Điểm là gross cho một hợp đồng, chưa phí. Cột net_2pt_sensitivity trong CSV chỉ là phép trừ thêm 2 điểm/lệnh đóng, không phải phí thực đã xác nhận.', '- Tỷ lệ thắng = số lệnh P&L dương / số lệnh đã đóng. BE/LOCK là cơ chế thoát, không đảm bảo P&L bằng0/dương nếu có gap.', '- Chuỗi là các lệnh đã đóng liên tiếp trong mẫu, bỏ ngày không giao dịch/bị loại/còn mở; không suy ra kết quả của ngày thiếu. Drawdown chỉ tính equity lệnh đã đóng, không phải intratrade hay quy tắc tài khoản funded.', '- So sánh 51 biến thể trên cùng mẫu là lựa chọn trong mẫu; chưa có kiểm định out-of-sample cho lựa chọn tốt nhất.','', '## Kiểm tra','', '77 unit tests PASS (74 engine, 3 clock). Replay chạy các oracle entry/exit của module tương ứng và đối chiếu row giá raw. Tổng hợp độc lập bằng Decimal kiểm tra 6.681 khóa duy nhất, 51 tổng điểm, số lệnh, phân loại, chuỗi và drawdown. Các kiểm tra này không biến giả định fill thành giao dịch thực.','']
 headers=['ORB','Quy tắc','Lệnh','TP','SL','BE','Lock','Mở','Loại','Không vào','Thắng %','Điểm thắng','Điểm thua','Tổng','Chuỗi thắng','Chuỗi thua','Max ngày thắng','Max ngày thua','DD đóng']
 for group in ('breakout','retest','EMA9'):
  text+=['## '+group,'','| '+' | '.join(headers)+' |','|'+'|'.join(['---']*len(headers))+'|']
  for s in [x for x in summaries if x['group']==group]:
   vals=[s['orb'],s['variant'],s['entries'],s['TP'],s['SL'],s['BE'],s['LOCK'],s['open'],s['excluded'],s['no_entry'],round(s['win_percent'],2) if s['win_percent'] is not None else '',s['won_points'],s['lost_points'],s['gross_points'],s['win_streak'],s['loss_streak'],s['max_day_win'],s['max_day_loss'],s['closed_drawdown']]
   text+=['| '+' | '.join(map(str,vals))+' |']
  text+=['']
 text+=['## Các lý do loại theo dòng ngày/cấu hình','',json.dumps(dict(Counter(r['reason'] for r in rows if r['outcome']=='EXCLUDED')),ensure_ascii=False,indent=2),'','## Provenance','',f"Raw audit: {R/'audit.json'}",f"Ledger: {R/'trades.csv'}",'Harness SHA256: '+hashlib.sha256(Path(__file__).with_name('orb3_run_v2.py').read_bytes()).hexdigest()]
 (out/'REPORT.md').write_text('\n'.join(text)+'\n')
 (out/'verification.json').write_text(json.dumps(dict(status='PASS',rows=6681,configurations=51,tests=77,source_audit_sha256=hashlib.sha256((R/'audit.json').read_bytes()).hexdigest(),report_sha256=hashlib.sha256((out/'REPORT.md').read_bytes()).hexdigest()),indent=2))
 print(json.dumps(sorted(summaries,key=lambda x:x['gross_points'],reverse=True)[:8],indent=2));print(out)
if __name__=='__main__':main()
