# MNQ EMA9 ORB — SL NỬA BOX / TP2R / KHÓA LỜI NỬA BOX

20/10/2025–19/04/2026; mở08:30 Houston; ORB1m/2m/5m. SL=½ độ rộng ORB; TP2R=cả box từ entry. Giá lời ≥½ box+0,25 điểm thì dời stop khóa lời+½ box từ record tick tiếp theo, sau đó giữ nguyên. LONG stop=entry+½ box; SHORT stop=entry−½ box. Không trailing liên tục. Một lệnh/ngày mỗi bài, không re-entry.

| RR | ORB | Lệnh | TP | SL | LOCK | Chưa đóng | Win % | Tổng thắng | Tổng thua | Lãi/lỗ điểm | Ngày thắng lớn nhất | Ngày thua lớn nhất |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 1:2 | 1m | 118 | 4 | 57 | 57 | 0 | 51.69% | 1705.75 | -1606.0 | 99.75 | 66.75 | -52.5 |
| 1:2 | 2m | 115 | 0 | 51 | 64 | 0 | 55.65% | 2262.25 | -1627.125 | 635.125 | 75.375 | -66.75 |
| 1:2 | 5m | 111 | 0 | 55 | 56 | 0 | 50.45% | 2656.5 | -2276.125 | 380.375 | 122.25 | -81.625 |

## Quy tắc và xác minh

- Entry GIỮ NGUYÊN từng row/timestamp/giá/chiều từ bài EMA9 SL20 TP20/30. Chờ EMA9 cùng khung đóng cắt lên đỉnh ORB hoặc xuống đáy; entry trade tick đầu tiên đủ điều kiện sau đóng nến. Không phải retest-limit.
- EMA xuyên phiên cùng raw contract, warmup300 nến từ Databento OHLCV1m; không reset08:30. Chưa chứng minh trùng tuyệt đối chart NT8 khác nguồn hoặc template. Entry logic và bộ lọc dữ liệu không thay đổi.
- Điểm chính theo TP/SL danh nghĩa, chưa phí/slippage. SL ban đầu và stop khóa nửa box có thể ở0,125 điểm, không phải tick MNQ hợp lệ. Bản tick-rounded làm tròn rủi ro ban đầu lên, lợi nhuận khóa xuống theo bước0,25 để giữ cùng các tick kích hoạt/thoát; TP cả box đã đúng tick. LOCK nominal=+½ box; gap qua stop vẫn có thể âm trong observed P&L. Chạm TP ngay tick kích hoạt thì kết thúc TP, không ghi thêm lock. rounded_net_points lưu riêng. Observed gap P&L lưu riêng, không bảo đảm market fill.
- Chỉ kiểm tra tới15:00 Houston. Lệnh chưa chạm TP/SL ghi OPEN_AT_CUTOFF, không đưa vào tổng điểm đã đóng. Win%=(TP+LOCK)/tất cả entry kể cả lệnh chưa đóng; tổng thắng gồm TP và LOCK. LOCK không phải full TP.
- 130 ngày ứng viên;8 loại toàn phiên. ORB1m/2m loại thêm4;5m loại thêm6 do receipt trễ/degraded. Không có tín hiệu:1m0,2m3,5m5. Cùng mẫu với bài EMA trước, các ngày loại có trong CSV.
- 14 test mới PASS tại CT/UTC, gồm4.000 chuỗi giả lập mỗi timezone đối chiếu first passage;5 mutation đều bị assertion phát hiện. Kế thừa15 test EMA cùng6 mutation ở bài nguồn. Tick file hash và count được kiểm tra lại;entry và ORB được đối chiếu raw ticks.
- Đã đối chiếu toàn bộ366 dòng,344 entry với bài nguồn; kiểm tra SL/TP/entry, exit thứ tự, tổng điểm, ngày thắng/thua lớn nhất và ô bảng Word. Decoder dùng chung; không cam kết dữ liệu/fill thực chính xác100%. Không sửa bot/DB/NT8.
- Mỗi dòng là một bài test riêng, không cộng thành danh mục. Audit reference docs/superpowers/AUDIT-CHECKLIST.md @ba50edf22.

## P&L bổ sung

| RR | ORB | Nominal | Stop/target làm tròn tick | Stop theo print quan sát |
|---|---|---:|---:|---:|
| 2 | 1 | 99.75 | 91.25 | 77.5 |
| 2 | 2 | 635.125 | 628.75 | 612.75 |
| 2 | 5 | 380.375 | 373.5 | 359.75 |
