# MNQ EMA9 ORB — SL NỬA BOX / TP2R / KHÓA LỜI +20,+25

20/10/2025–19/04/2026; mở08:30 Houston; ORB1m/2m/5m. SL=½ độ rộng ORB; TP2R từ entry. Lời+1R thì dời stop khóa+20/+25 điểm (2 bài riêng) từ record tick tiếp theo, sau đó giữ nguyên. LONG stop=entry+lock; SHORT stop=entry−lock. Nếu1R chưa vượt mức khóa, chờ gain≥max(1R,lock+0,25) để stop hợp lệ; không loại ngày. Nếu TP đến trước thì chốt TP. Một lệnh/ngày mỗi bài, không re-entry.

| Khóa lời | RR | ORB | Lệnh | TP | SL | LOCK | Chưa đóng | Win % | Tổng thắng | Tổng thua | Lãi/lỗ điểm | Ngày thắng lớn nhất | Ngày thua lớn nhất |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| +20 | 1:2 | 1m | 118 | 11 | 62 | 45 | 0 | 47.46% | 1538.0 | -1670.75 | -132.75 | 118.0 | -52.5 |
| +20 | 1:2 | 2m | 115 | 12 | 53 | 49 | 1 | 53.04% | 1986.25 | -1652.25 | 334.0 | 118.0 | -66.75 |
| +20 | 1:2 | 5m | 111 | 18 | 55 | 36 | 2 | 48.65% | 2647.0 | -2276.125 | 370.875 | 186.5 | -81.625 |
| +25 | 1:2 | 1m | 118 | 8 | 63 | 47 | 0 | 46.61% | 1635.5 | -1692.625 | -57.125 | 118.0 | -52.5 |
| +25 | 1:2 | 2m | 115 | 9 | 53 | 52 | 1 | 53.04% | 2016.5 | -1652.25 | 364.25 | 118.0 | -66.75 |
| +25 | 1:2 | 5m | 111 | 17 | 55 | 37 | 2 | 48.65% | 2793.0 | -2276.125 | 516.875 | 186.5 | -81.625 |

## Quy tắc và xác minh

- Entry GIỮ NGUYÊN từng row/timestamp/giá/chiều từ bài EMA9 SL20 TP20/30. Chờ EMA9 cùng khung đóng cắt lên đỉnh ORB hoặc xuống đáy; entry trade tick đầu tiên đủ điều kiện sau đóng nến. Không phải retest-limit.
- EMA xuyên phiên cùng raw contract, warmup300 nến từ Databento OHLCV1m; không reset08:30. Chưa chứng minh trùng tuyệt đối chart NT8 khác nguồn hoặc template. Entry logic và bộ lọc dữ liệu không thay đổi.
- Điểm chính theo TP/SL danh nghĩa, chưa phí/slippage. SL nửa box có thể ở0,125 điểm, không phải tick MNQ hợp lệ. Stop/target làm tròn xa entry đến tick0,25 trong cột tick_rounded; RR sau làm tròn có thể khác danh nghĩa. LOCK nominal=+20/+25; gap qua stop khóa vẫn có thể âm trong observed P&L. Lệnh chạm TP ngay tick kích hoạt kết thúc TP, không ghi thêm lock. rounded_net_points lưu riêng. Observed gap P&L lưu riêng, không bảo đảm market fill.
- Chỉ kiểm tra tới15:00 Houston. Lệnh chưa chạm TP/SL ghi OPEN_AT_CUTOFF, không đưa vào tổng điểm đã đóng. Win%=(TP+LOCK)/tất cả entry kể cả lệnh chưa đóng; đây là win danh nghĩa, không gán LOCK thành full TP. Tổng điểm thắng gồm cả TP và LOCK.
- 130 ngày ứng viên;8 loại toàn phiên. ORB1m/2m loại thêm4;5m loại thêm6 do receipt trễ/degraded. Không có tín hiệu:1m0,2m3,5m5. Cùng mẫu với bài EMA trước, các ngày loại có trong CSV.
- 13 test mới PASS tại CT/UTC, gồm4.000 chuỗi giả lập mỗi timezone đối chiếu first passage;5 mutation đều bị assertion phát hiện. Kế thừa15 test EMA cùng6 mutation ở bài nguồn. Tick file hash và count được kiểm tra lại;entry và ORB được đối chiếu raw ticks.
- Đã đối chiếu toàn bộ732 dòng,688 entry-variant (344 entry chung ×2 lock) với bài nguồn; kiểm tra SL/TP/entry, exit thứ tự, tổng điểm, ngày thắng/thua lớn nhất và ô bảng Word. Decoder dùng chung; không cam kết dữ liệu/fill thực chính xác100%. Không sửa bot/DB/NT8.
- Mỗi dòng là một bài test riêng, không cộng thành danh mục. Audit reference docs/superpowers/AUDIT-CHECKLIST.md @ba50edf22.

## P&L bổ sung

| Khóa | RR | ORB | Nominal | Stop/target làm tròn tick | Stop theo print quan sát |
|---|---|---|---:|---:|---:|
| 20 | 2 | 1 | -132.75 | -137.75 | -143.5 |
| 20 | 2 | 2 | 334.0 | 330.5 | 327.5 |
| 20 | 2 | 5 | 370.875 | 367.25 | 366.75 |
| 25 | 2 | 1 | -57.125 | -62.25 | -67.25 |
| 25 | 2 | 2 | 364.25 | 360.75 | 356.0 |
| 25 | 2 | 5 | 516.875 | 513.25 | 511.25 |

## Số entry có 1R không lớn hơn mức khóa (phải chờ giá vượt thêm ít nhất 1 tick)

| Khóa | ORB | Ngày |
|---|---|---:|
| 20 | 1 | 28 |
| 20 | 2 | 8 |
| 20 | 5 | 2 |
| 25 | 1 | 52 |
| 25 | 2 | 24 |
| 25 | 5 | 10 |
