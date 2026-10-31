"""Freeze the checked outputs, clarify denominators, bundle without raw data."""
import csv,json,hashlib,zipfile
from pathlib import Path
ROOT=Path('/home/hoang/fleet-1017/out/orb3-recent-20261031');R=ROOT/'run-v2'
def main():
 manifest=json.loads((ROOT/'engine-snapshot/manifest.json').read_text());audit=json.loads((R/'audit.json').read_text())
 for name,h in audit['engine_hashes'].items():assert manifest[name]['sha256']==h
 for name,record in manifest.items():assert hashlib.sha256(Path(record['path']).read_bytes()).hexdigest()==record['sha256']
 assert json.loads((R/'verified-report/verification.json').read_text())['status']=='PASS'
 assert json.loads((R/'details/audit.json').read_text())['status']=='PASS'
 ss=json.loads((R/'summary.json').read_text());rates=[]
 for s in ss:
  closed=s['positive']+s['negative']+s['zero']
  rates.append(dict(group=s['group'],variant=s['variant'],orb=s['orb'],entries=s['entries'],closed=closed,open=s['open'],tp=s['TP'],tp_percent_of_closed=100*s['TP']/closed if closed else '',tp_percent_of_entries=100*s['TP']/s['entries'] if s['entries'] else '',positive_pnl_percent_of_closed=s['win_percent']))
 with (R/'details/outcome-rates.csv').open('x',newline='') as f:
  w=csv.DictWriter(f,fieldnames=list(rates[0]));w.writeheader();w.writerows(rates)
 out=ROOT/'final';out.mkdir(exist_ok=False)
 report=(R/'verified-report/REPORT.md').read_text()+'\n## Bổ sung định nghĩa\n\n- Retest touch chọn hướng phá đầu tiên; retest thân nến chọn nến đầu tiên đạt điều kiện xác nhận, có thể khác hướng phá đầu tiên.\n- `details/outcome-rates.csv` tách TP / lệnh đóng, TP / lệnh vào, và lệnh P&L dương / lệnh đóng.\n- `details/streaks.csv` ghi ngày bắt đầu/kết thúc từng chuỗi; chuỗi ở biên mẫu có thể còn tiếp diễn ngoài mẫu.\n- `details/common-days.csv` so sánh lại tổng điểm trên giao các ngày không bị loại ở bất kỳ cấu hình nào. Lệnh còn mở vẫn chưa được tính P&L.\n- Không phải tất cả phép kiểm tra dùng thuật toán hoàn toàn độc lập: decoder và một số quy tắc được dùng chung. Chưa chứng minh fill thật hay chart NT8 parity.\n'
 with (out/'ORB-three-groups-report.md').open('x') as f:f.write(report)
 paths=[R/'summary.csv',R/'trades.csv',R/'sessions.csv',R/'audit.json',R/'verified-report/verification.json',R/'verified-report/comparison.csv',*sorted((R/'details').glob('*')), *sorted((ROOT/'engine-snapshot').glob('*'))]
 with zipfile.ZipFile(out/'ORB-three-groups-verified.zip','x',compression=zipfile.ZIP_DEFLATED) as z:
  z.write(out/'ORB-three-groups-report.md','REPORT.md')
  for p in paths:z.write(p,str(p.relative_to(ROOT)))
 with (out/'manifest.json').open('x') as f:json.dump({p.name:dict(bytes=p.stat().st_size,sha256=hashlib.sha256(p.read_bytes()).hexdigest()) for p in sorted(out.iterdir()) if p.name!='manifest.json'},f,indent=2)
 print(out)
if __name__=='__main__':main()
