# MNQ EMA9 ORB — SL NỬA BOX / TP CẢ BOX

20/10/2025–19/04/2026; mở08:30 Houston; ORB1m/2m/5m. SL=½ độ rộng ORB, TP=1× độ rộng ORB từ entry; RR1:2. Một lệnh/ngày mỗi bài, không BE, không re-entry.

| ORB | Lệnh | TP | SL | Chưa đóng | TP % | Tổng thắng | Tổng thua | Lãi/lỗ điểm | Ngày thắng lớn nhất | Ngày thua lớn nhất |
|---|---|---|---|---|---|---|---|---|---|---|
| 1m | 118 | 44 | 74 | 0 | 37.29% | 2363.25 | -2032.875 | 330.375 | 118.0 | -52.5 |
| 2m | 115 | 47 | 67 | 1 | 40.87% | 3415.5 | -2131.875 | 1283.625 | 150.75 | -66.75 |
| 5m | 111 | 35 | 72 | 4 | 31.53% | 3258.75 | -3007.125 | 251.625 | 186.5 | -83.75 |

## Quy tắc và xác minh

- Entry GIỮ NGUYÊN từng row/timestamp/giá/chiều từ bài EMA9 SL20 TP20/30. Chờ EMA9 cùng khung đóng cắt lên đỉnh ORB hoặc xuống đáy; entry trade tick đầu tiên đủ điều kiện sau đóng nến. Không phải retest-limit.
- EMA xuyên phiên cùng raw contract, warmup300 nến từ Databento OHLCV1m; không reset08:30. Chưa chứng minh trùng tuyệt đối chart NT8 khác nguồn hoặc template. Entry logic và bộ lọc dữ liệu không thay đổi.
- Điểm chính theo TP/SL danh nghĩa, chưa phí/slippage. SL nửa box có thể ở0,125 điểm, không phải tick MNQ hợp lệ. stop_tick_rounded_points làm tròn stop xa entry đến tick0,25; rounded_net_points lưu riêng. Observed gap P&L lưu riêng, không bảo đảm market fill.
- Chỉ kiểm tra tới15:00 Houston. Lệnh chưa chạm TP/SL ghi OPEN_AT_CUTOFF, không đưa vào tổng điểm đã đóng. Tỷ lệ TP=TP/tất cả entry kể cả lệnh chưa đóng.
- 130 ngày ứng viên;8 loại toàn phiên. ORB1m/2m loại thêm4;5m loại thêm6 do receipt trễ/degraded. Không có tín hiệu:1m0,2m3,5m5. Cùng mẫu với bài EMA trước, các ngày loại có trong CSV.
- 8 test mới PASS tại CT/UTC, gồm2.000 chuỗi giả lập mỗi timezone đối chiếu first passage;4 mutation đều bị assertion phát hiện. Kế thừa15 test EMA cùng6 mutation ở bài nguồn. Tick file hash và count được kiểm tra lại;entry và ORB được đối chiếu raw ticks.
- Đã đối chiếu toàn bộ366 dòng,344 entry với bài nguồn; kiểm tra SL/TP/entry, exit thứ tự, tổng điểm, ngày thắng/thua lớn nhất và ô bảng Word. Decoder dùng chung; không cam kết dữ liệu/fill thực chính xác100%. Không sửa bot/DB/NT8.
- Mỗi dòng là một bài test riêng, không cộng thành danh mục. Audit reference docs/superpowers/AUDIT-CHECKLIST.md @ba50edf22.

## P&L bổ sung

| ORB | Nominal | Stop làm tròn tick | Stop theo print quan sát |
|---|---:|---:|---:|
| 1 | 330.375 | 324.75 | 324.75 |
| 2 | 1283.625 | 1279.75 | 1276.75 |
| 5 | 251.625 | 247.0 | 246.75 |
