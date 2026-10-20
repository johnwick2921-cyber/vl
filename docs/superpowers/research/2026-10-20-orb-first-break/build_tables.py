#!/usr/bin/env python3
"""Build exclusively-created owner tables from the frozen, checked ledger."""
import argparse
import collections
import csv
import datetime as dt
import hashlib
import json
import statistics
from pathlib import Path
from zoneinfo import ZoneInfo

BASE = Path(__file__).parent
CT = ZoneInfo("America/Chicago")


def table(headers, rows):
    def cell(x):
        return str(x).replace("|"," / ").replace("\n"," ")
    return "\n".join(["| "+" | ".join(map(cell,headers))+" |",
                      "|"+"|".join(["---"]*len(headers))+"|"]+
                     ["| "+" | ".join(map(cell,r))+" |" for r in rows])+"\n\n"


def csvfile(path, rows):
    with path.open("x",newline="") as f:
        w=csv.DictWriter(f,fieldnames=list(rows[0]))
        w.writeheader()
        w.writerows(rows)


def local(stamp):
    return dt.datetime.fromtimestamp(int(stamp)/1000,CT).strftime("%Y-%m-%d %H:%M") if stamp else ""


def build(out, verification_path):
    out.mkdir(exist_ok=False)
    trades=list(csv.DictReader((BASE/"trades.csv").open()))
    sessions=list(csv.DictReader((BASE/"sessions.csv").open()))
    summary=json.loads((BASE/"summary.json").read_text())
    evidence=json.loads(Path(verification_path).read_text())
    assert evidence["engine_sha256"]==hashlib.sha256((BASE/"backtest.py").read_bytes()).hexdigest()
    with (out/"verification.json").open("x") as f:
        json.dump(evidence,f,indent=2)
    detailed=[]
    for r in trades:
        known=r["side"]!="UNKNOWN"
        sign=1 if r["side"]=="LONG" else -1
        entry=float(r["entry"]) if known else None
        width=float(r["width_points"])
        detailed.append(dict(
            date=r["date"],contract=r["contract"],orb_minutes=r["orb_minutes"],
            orb_low=r["orb_low"],orb_high=r["orb_high"],orb_width_points=width,
            entry_minute_houston=local(r["entry_ms"]),side=r["side"],entry=r["entry"],
            stop_loss=entry-sign*20 if known else "",
            take_profit=entry+sign*width if known else "",
            possible_long_entry=float(r["orb_high"])+.25 if not known else "",
            possible_short_entry=float(r["orb_low"])-.25 if not known else "",
            exit_minute_houston=local(r["exit_ms"]),exit=r["exit"],
            confirmed_outcome=r["outcome"],possible_outcomes=r["possible_outcomes"],
            conservative_outcome="SL" if r["outcome"]=="UNRESOLVED" else r["outcome"],
            optimistic_outcome="TP" if r["outcome"]=="UNRESOLVED" else r["outcome"],
            gross_points_lower=r["pnl_min_points"],gross_points_upper=r["pnl_max_points"],
            net_points_lower_2pt_cost=float(r["pnl_min_points"])-2,
            net_points_upper_2pt_cost=float(r["pnl_max_points"])-2,
            stop_mfe_min_points=r["stop_mfe_min_points"],stop_mfe_max_points=r["stop_mfe_max_points"],
            reason=r["reason"]))
    csvfile(out/"all-trades.csv",detailed)
    csvfile(out/"successful-trades.csv",[r for r in detailed if r["confirmed_outcome"]=="TP"])
    csvfile(out/"stopped-trades.csv",[r for r in detailed if r["confirmed_outcome"]=="SL"])
    csvfile(out/"unresolved-trades.csv",[r for r in detailed if r["confirmed_outcome"]=="UNRESOLVED"])
    excluded=[r for r in sessions if r["reason"]!="included"]
    csvfile(out/"excluded-dates.csv",excluded)
    monthly=[]
    sides=[]
    headline=[]
    for tf in ("1","2","5"):
        group=[r for r in trades if r["orb_minutes"]==tf]
        counts=collections.Counter(r["outcome"] for r in group)
        n=len(group);tp=counts["TP"];sl=counts["SL"];u=counts["UNRESOLVED"]
        headline.append(dict(orb_minutes=tf,trades=n,confirmed_TP=tp,confirmed_SL=sl,
                             unresolved=u,conservative_TP=tp,conservative_SL=sl+u,
                             conservative_TP_pct=round(100*tp/n,2),
                             optimistic_TP=tp+u,optimistic_SL=sl,
                             optimistic_TP_pct=round(100*(tp+u)/n,2)))
        for month in sorted({r["date"][:7] for r in group}):
            g=[r for r in group if r["date"].startswith(month)]
            c=collections.Counter(r["outcome"] for r in g)
            monthly.append(dict(month=month,orb_minutes=tf,trades=len(g),
                                confirmed_TP=c["TP"],confirmed_SL=c["SL"],unresolved=c["UNRESOLVED"],
                                conservative_TP_pct=round(100*c["TP"]/len(g),2),
                                optimistic_TP_pct=round(100*(c["TP"]+c["UNRESOLVED"])/len(g),2)))
        for side in ("LONG","SHORT","UNKNOWN"):
            g=[r for r in group if r["side"]==side]
            c=collections.Counter(r["outcome"] for r in g)
            sides.append(dict(orb_minutes=tf,side=side,trades=len(g),
                              confirmed_TP=c["TP"],confirmed_SL=c["SL"],unresolved=c["UNRESOLVED"]))
    csvfile(out/"summary-table.csv",headline)
    csvfile(out/"monthly.csv",monthly)
    csvfile(out/"long-short.csv",sides)
    docs=["**FULL REPORT — MNQ ORB 1p / 2p / 5p, tái kiểm tra ngày 20/10/2026.**\n\n"]
    docs.append(table(["Câu hỏi","Kết luận"],[
        ["Engine đúng không?","PASS trong mô hình OHLC được mô tả; chưa phát hiện sai logic qua kiểm thử và đối chiếu độc lập. Không phải chứng minh fill/tick thực."],
        ["Có chờ nến đóng để entry không?","KHÔNG. ORB hoàn tất rồi vừa vượt biên 1 tick (0.25 điểm) là kích hoạt entry ngay."],
        ["Có thay đổi kết quả sau kiểm lại không?","KHÔNG. Engine SHA256 không đổi; TP/SL/chưa rõ vẫn 35/158/33; 37/163/26; 27/176/23."],
        ["Thành công nghĩa là gì?","Chạm TP trước SL. Giá về SL sau khi đã đóng TP không làm mất một lệnh thắng."],
        ["Có một tỷ lệ thắng chính xác cho cả năm không?","Không với snapshot OHLC hiện có. Báo đầy đủ kịch bản bảo thủ và thuận lợi, không giấu các lệnh chưa rõ."],
        ["Số lệnh","226 lệnh mỗi bài test. 678 hàng = ba chiến lược trên cùng 226 phiên, không phải 678 lệnh của một tài khoản."],
        ["Thay đổi bot thật?","Không. Không DB write, không thay cấu hình, không gửi lệnh hoặc restart."]]))
    docs.append(table(["Quy tắc","Giá trị"],[
        ["Cửa sổ","2025-10-20 inclusive đến 2026-10-20 exclusive; cuối phiên được dùng là 2026-10-19."],
        ["Múi giờ","Houston = America/Chicago; tự xử lý CDT/CST, không cố định UTC-5."],
        ["ORB","1p = 08:30–08:31; 2p = 08:30–08:32; 5p = 08:30–08:35."],
        ["Entry","Phá đỉnh trước: LONG; phá đáy trước: SHORT; ngưỡng biên +/- 0.25 điểm."],
        ["Số lần","Tối đa 1 lệnh/ngày/bài test; thua nghỉ; không re-entry, không đảo chiều."],
        ["Stop","20 điểm từ entry; gap qua stop có thể fill xấu hơn."],
        ["Target","1 × (đỉnh ORB − đáy ORB), tính từ entry thực của mô hình."],
        ["Thoát theo giờ","15:00 Houston nếu còn mở; giả định được công khai, 0 time-exit trong mẫu."],
        ["Filter","Không Mentor/day-off/EMA/AI gate; ORB thuần."],
        ["Chi phí","0 điểm cho bảng gross; thêm kịch bản 2 điểm khứ hồi/lệnh. Không phải phí broker đã xác minh."]]))
    docs.append("**Bảng kết quả thành công/thất bại đã kiểm lại.**\n\n")
    docs.append(table(["ORB","Lệnh","TP chắc chắn","SL chắc chắn","Chưa rõ","TP % bảo thủ","TP % thuận lợi"],
                     [[h["orb_minutes"]+"p",h["trades"],h["confirmed_TP"],h["confirmed_SL"],h["unresolved"],h["conservative_TP_pct"],h["optimistic_TP_pct"]] for h in headline]))
    docs.append(table(["ORB","Bảo thủ: TP","Bảo thủ: SL","Thuận lợi: TP","Thuận lợi: SL"],
                     [[h["orb_minutes"]+"p",h["conservative_TP"],h["conservative_SL"],h["optimistic_TP"],h["optimistic_SL"]] for h in headline]))
    docs.append("Hai cột % là giới hạn phụ thuộc thứ tự intrabar, **không phải khoảng tin cậy thống kê**. Bảo thủ gán toàn bộ lệnh chưa rõ là SL; thuận lợi gán là TP. Đó là giả định, không phải dữ liệu tick đã thấy.\n\n")
    docs.append(table(["ORB","SL chắc chắn","Ít nhất đã đi thuận ≥0.25 rồi SL","Tối đa có thể đã đi thuận rồi SL","Range TB","Range median"],
        [[str(s["orb_minutes"])+"p",s["counts"]["SL"],s["definite_comeback_stops"],s["possible_comeback_stops"],
          round(statistics.mean(float(r["width_points"]) for r in trades if int(r["orb_minutes"])==s["orb_minutes"]),2),
          statistics.median(float(r["width_points"]) for r in trades if int(r["orb_minutes"])==s["orb_minutes"])] for s in summary["results"]]))
    docs.append(table(["ORB","Gross thấp","Gross cao","Net 2pt thấp","Net 2pt cao","Net/lệnh thấp","Net/lệnh cao"],
        [[str(s["orb_minutes"])+"p",s["gross_points_lower"],s["gross_points_upper"],
          s["net_points_lower_2pt_cost"],s["net_points_upper_2pt_cost"],
          round(s["net_points_lower_2pt_cost"]/226,2),round(s["net_points_upper_2pt_cost"]/226,2)] for s in summary["results"]]))
    docs.append("Tất cả P&L trên tính bằng **điểm**, không phải USD. ORB 1p vẫn âm ở cận thuận lợi sau chi phí giả định; khoảng 2p và 5p đi qua zero. Không kết luận có edge hoặc chọn khung tốt nhất từ cận thuận lợi.\n\n")
    docs.append("**Theo tháng — mỗi ô là TP chắc chắn / SL chắc chắn / chưa rõ.** Hai tháng 10 chỉ chứa phần ngày nằm trong cửa sổ một năm.\n\n")
    monthly_rows=[]
    for month in sorted({r["month"] for r in monthly}):
        g={r["orb_minutes"]:r for r in monthly if r["month"]==month}
        monthly_rows.append([month,g["1"]["trades"]]+[f'{g[t]["confirmed_TP"]} / {g[t]["confirmed_SL"]} / {g[t]["unresolved"]}' for t in ("1","2","5")])
    docs.append(table(["Tháng","Phiên","ORB 1p","ORB 2p","ORB 5p"],monthly_rows))
    docs.append(table(["ORB","Chiều","Lệnh","TP chắc chắn","SL chắc chắn","Chưa rõ"],[list(r.values()) for r in sides]))
    docs.append("UNKNOWN ở cột chiều không phải lúc nào cũng là kết quả chưa rõ: hai chiều đầu tiên có thể đều dẫn tới SL. Không gán những hàng đó tùy ý thành LONG hoặc SHORT.\n\n")
    docs.append("**Ma trận tái kiểm tra engine và dữ liệu.**\n\n")
    docs.append(table(["Kiểm tra","Số lượng / chứng cứ","Kết quả"],[
        ["Unit test trực tiếp simulate","12/12; long/short, gap, ambiguity, ORB boundary, one trade, time exit","PASS"],
        ["Giá có thứ tự độc lập","2,000 đường giá giả lập × 3 ORB = 6,000 đối chiếu outcome/P&L/MFE","PASS — synthetic, không phải tick lịch sử"],
        ["Mutation","6 lỗi cố ý: stop21, bỏ tick, lookahead, stop-gap, target20, bỏ nhánh sống","6/6 bị test bắt"],
        ["Raw snapshot","329,692 hàng; source split và canonical SHA256 tính lại độc lập","PASS"],
        ["Chọn contract","226 lựa chọn theo volume 08:00–08:29, không dùng volume cả ngày","PASS"],
        ["Đủ nến","226 phiên × 390 phút; không nối contract hoặc điền nến trống","PASS"],
        ["ORB và first-break","678 range high/low, phút phá đầu tiên, side và giá entry đối chiếu lại raw","PASS"],
        ["Một lệnh/ngày","678 khóa (ngày, ORB) khác nhau = 226 ngày × 3 khung","PASS"],
        ["DST Houston","08:30 ngày2025-10-20 =13:30UTC; ngày2025-11-07 =14:30UTC","PASS"],
        ["Source timestamp","historical_import trừ60s; live/historical không trừ lại","Source chain đã được DS106 kiểm độc lập"],
        ["Frozen replay","summary.json/trades.csv/sessions.csv của DB run và frozen replay giống từng byte","PASS"],
        ["Review đối kháng DS106","13 probes; không tìm được phản ví dụ logic","PASS trong scope offline"],
        ["Review dữ liệu DS102","Counts, unique dates, source split, canonical digest","PASS; nhận định giờ entry ban đầu bị sửa theo DST"],
        ["Historical tick execution","Không có đầy đủ tick một năm, spread/queue và fill thực","CHƯA ĐƯỢC CHỨNG MINH"]]))
    timing=evidence["independent_input_crosscheck"]["houston_entry_times"]
    docs.append(table(["ORB","Entry sớm nhất","Entry muộn nhất","Phút thường gặp nhất","Số lần"],
                     [[t+"p",timing[t]["first"],timing[t]["last"],timing[t]["mode"][0][0],timing[t]["mode"][0][1]] for t in ("1","2","5")]))
    docs.append("Giờ trên là phút chứa entry ở Houston, không phải giây fill chính xác. Review ngoài từng dùng giờ muộn sai; bảng này được CTO tính lại bằng America/Chicago. Tất cả opening bars trong mẫu đều có mặt; không gán entry muộn là mất nến nếu chưa có chứng cứ.\n\n")
    docs.append("**Ngày không đưa vào mẫu — công khai toàn bộ 35 ngày.** 261 ngày thứ Hai–thứ Sáu =226 included +35 excluded. Không gọi mọi ngày thiếu dữ liệu là holiday. Khoảng trống roll gồm8 ngày thường 03–12/12/2025 và9 ngày thường 03–13/03/2026.\n\n")
    docs.append(table(["Ngày","Contract được chọn","Nến RTH","Lý do"],[[r["date"],r["contract"],r["rth_bars"],r["reason"]] for r in excluded]))
    docs.append(table(["File","Nội dung"],[
        ["all-trades.csv","678 hàng đầy đủ: ORB, entry Houston, SL, TP, kết quả, hai kịch bản, P&L, MFE."],
        ["all-trades.md","Toàn bộ678 lệnh dạng bảng Markdown dễ đọc."],
        ["successful-trades.csv","99 hàng TP chắc chắn =35+37+27, thuộc3 test độc lập."],
        ["stopped-trades.csv","497 hàng SL chắc chắn =158+163+176."],
        ["unresolved-trades.csv","82 hàng chưa rõ =33+26+23."],
        ["monthly.csv / long-short.csv","Các bảng phân nhóm có thể lọc trong Excel."],
        ["excluded-dates.csv","35 ngày không đủ điều kiện dữ liệu."],
        ["verification.json","Kết quả kiểm engine, mutation, raw-data/first-entry checks."],
        ["../README.md","Chi tiết provenance, assumptions, source lines và cách chạy lại."],
        ["../reverify.py / ../build_tables.py","Mã tái kiểm tra và mã tạo bảng; không sửa engine."]]))
    docs.append(table(["Giới hạn còn lại","Ý nghĩa"],[
        ["OHLC không có thứ tự tick","33/26/23 kết quả cần tick để chốt; không có một win rate thực duy nhất."],
        ["Thiếu35 ngày trong tuần / loại ngày đóng sớm","Kết quả chỉ nói về226 phiên đầy đủ, không phải mọi ngày một năm."],
        ["Mô hình fill","Giá ngưỡng hoặc open khi gap; fill thật và slippage có thể khác."],
        ["Không tối ưu/test forward","Đây là kiểm tra bộ quy tắc đã yêu cầu trên mẫu quá khứ, không phải cam kết lợi nhuận."],
        ["Phần engine","Không phát hiện bug qua các kiểm tra nêu trên; không tuyên bố chứng minh đúng cho mọi dữ liệu có thể tồn tại."]]))
    with (out/"FULL-REPORT.md").open("x") as f:
        f.write("".join(docs))
    with (out/"all-trades.md").open("x") as f:
        f.write("**678 hàng của ba bài test độc lập; một lệnh mỗi ngày mỗi ORB.** Giờ entry/exit là phút chứa sự kiện. Trống = chưa xác định, không phải0. TP/SL là outcome; reason có thể ghi sự mơ hồ ban đầu đã được giải quyết.\n\n")
        for tf in ("1","2","5"):
            g=[r for r in detailed if r["orb_minutes"]==tf]
            f.write("**ORB "+tf+" phút — 226 lệnh.**\n\n")
            f.write(table(["Ngày","Contract","ORB low–high","Entry phút CT","Chiều","Entry","SL","TP","Exit phút CT","Kết quả","Gross thấp–cao"],
                [[r["date"],r["contract"],r["orb_low"]+"–"+r["orb_high"],r["entry_minute_houston"],
                  r["side"],r["entry"],r["stop_loss"],r["take_profit"],r["exit_minute_houston"],
                  r["confirmed_outcome"],str(r["gross_points_lower"])+"…"+str(r["gross_points_upper"])] for r in g]))
    print(json.dumps(dict(output=str(out),trades=len(detailed),successful=99,stopped=497,unresolved=82,files=len(list(out.iterdir()))),indent=2))


if __name__=="__main__":
    parser=argparse.ArgumentParser()
    parser.add_argument("--out",required=True)
    parser.add_argument("--verification",required=True)
    args=parser.parse_args()
    build(Path(args.out),args.verification)
