# MNQ EMA9 ORB — SL NỬA BOX / TP 3R (1,5 BOX)

20/10/2025–19/04/2026; mở08:30 Houston; ORB1m/2m/5m. SL=½ độ rộng ORB, TP=1,5× độ rộng ORB từ entry; RR1:3. Một lệnh/ngày mỗi bài, không BE, không re-entry.

| ORB | Lệnh | TP | SL | Chưa đóng | TP % | Tổng thắng | Tổng thua | Lãi/lỗ điểm | Ngày thắng lớn nhất | Ngày thua lớn nhất |
|---|---|---|---|---|---|---|---|---|---|---|
| 1m | 118 | 34 | 84 | 0 | 28.81% | 2784.75 | -2286.25 | 498.5 | 177.0 | -52.5 |
| 2m | 115 | 35 | 76 | 4 | 30.43% | 3930.75 | -2429.875 | 1500.875 | 226.125 | -66.75 |
| 5m | 111 | 27 | 77 | 7 | 24.32% | 3681.375 | -3240.875 | 440.5 | 279.75 | -83.75 |

## Quy tắc và xác minh

- Entry GIỮ NGUYÊN từng row/timestamp/giá/chiều từ bài EMA9 SL20 TP20/30. Chờ EMA9 cùng khung đóng cắt lên đỉnh ORB hoặc xuống đáy; entry trade tick đầu tiên đủ điều kiện sau đóng nến. Không phải retest-limit.
- EMA xuyên phiên cùng raw contract, warmup300 nến từ Databento OHLCV1m; không reset08:30. Chưa chứng minh trùng tuyệt đối chart NT8 khác nguồn hoặc template. Entry logic và bộ lọc dữ liệu không thay đổi.
- Điểm chính theo TP/SL danh nghĩa, chưa phí/slippage. SL nửa box và TP1,5box có thể ở0,125 điểm, không phải tick MNQ hợp lệ. Stop và target làm tròn xa entry đến tick0,25 trong các cột tick_rounded; RR sau làm tròn có thể hơi khác1:3. rounded_net_points lưu riêng. Observed gap P&L lưu riêng, không bảo đảm market fill.
- Chỉ kiểm tra tới15:00 Houston. Lệnh chưa chạm TP/SL ghi OPEN_AT_CUTOFF, không đưa vào tổng điểm đã đóng. Tỷ lệ TP=TP/tất cả entry kể cả lệnh chưa đóng.
- 130 ngày ứng viên;8 loại toàn phiên. ORB1m/2m loại thêm4;5m loại thêm6 do receipt trễ/degraded. Không có tín hiệu:1m0,2m3,5m5. Cùng mẫu với bài EMA trước, các ngày loại có trong CSV.
- 9 test mới PASS tại CT/UTC, gồm2.000 chuỗi giả lập mỗi timezone đối chiếu first passage;4 mutation đều bị assertion phát hiện. Kế thừa15 test EMA cùng6 mutation ở bài nguồn. Tick file hash và count được kiểm tra lại;entry và ORB được đối chiếu raw ticks.
- Đã đối chiếu toàn bộ366 dòng,344 entry với bài nguồn; kiểm tra SL/TP/entry, exit thứ tự, tổng điểm, ngày thắng/thua lớn nhất và ô bảng Word. Decoder dùng chung; không cam kết dữ liệu/fill thực chính xác100%. Không sửa bot/DB/NT8.
- Mỗi dòng là một bài test riêng, không cộng thành danh mục. Audit reference docs/superpowers/AUDIT-CHECKLIST.md @ba50edf22.

## P&L bổ sung

| ORB | Nominal | Stop/target làm tròn tick | Stop theo print quan sát |
|---|---:|---:|---:|
| 1 | 498.5 | 494.75 | 494.75 |
| 2 | 1500.875 | 1498.75 | 1495.75 |
| 5 | 440.5 | 437.25 | 437.0 |
