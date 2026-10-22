# MNQ EMA9 ORB — SL NỬA BOX / TP2R–3R / KHÓA LỜI +5,+10,+15

20/10/2025–19/04/2026; mở08:30 Houston; ORB1m/2m/5m. SL=½ độ rộng ORB; TP2R hoặc3R từ entry. Lời+1R thì dời stop khóa+5/+10/+15 điểm (3 bài riêng) từ record tick tiếp theo, sau đó giữ nguyên. LONG stop=entry+lock; SHORT stop=entry−lock. Nếu1R chưa vượt mức khóa, chờ gain≥max(1R,lock+0,25) để stop hợp lệ; không loại ngày. Nếu TP đến trước thì chốt TP. Một lệnh/ngày mỗi bài, không re-entry.

| Khóa lời | RR | ORB | Lệnh | TP | SL | LOCK | Chưa đóng | Win % | Tổng thắng | Tổng thua | Lãi/lỗ điểm | Ngày thắng lớn nhất | Ngày thua lớn nhất |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| +5 | 1:2 | 1m | 118 | 27 | 57 | 34 | 0 | 51.69% | 1624.5 | -1606.0 | 18.5 | 118.0 | -52.5 |
| +5 | 1:2 | 2m | 115 | 29 | 51 | 34 | 1 | 54.78% | 2314.75 | -1627.125 | 687.625 | 118.0 | -66.75 |
| +5 | 1:2 | 5m | 111 | 26 | 55 | 27 | 3 | 47.75% | 2677.25 | -2276.125 | 401.125 | 186.5 | -81.625 |
| +5 | 1:3 | 1m | 118 | 18 | 57 | 43 | 0 | 51.69% | 1734.875 | -1606.0 | 128.875 | 177.0 | -52.5 |
| +5 | 1:3 | 2m | 115 | 18 | 51 | 44 | 2 | 53.91% | 2165.875 | -1627.125 | 538.75 | 177.0 | -66.75 |
| +5 | 1:3 | 5m | 111 | 17 | 55 | 33 | 6 | 45.05% | 2521.5 | -2276.125 | 245.375 | 279.75 | -81.625 |
| +10 | 1:2 | 1m | 118 | 21 | 57 | 40 | 0 | 51.69% | 1577.5 | -1606.0 | -28.5 | 118.0 | -52.5 |
| +10 | 1:2 | 2m | 115 | 24 | 51 | 39 | 1 | 54.78% | 2208.25 | -1627.125 | 581.125 | 118.0 | -66.75 |
| +10 | 1:2 | 5m | 111 | 25 | 55 | 28 | 3 | 47.75% | 2750.5 | -2276.125 | 474.375 | 186.5 | -81.625 |
| +10 | 1:3 | 1m | 118 | 14 | 57 | 47 | 0 | 51.69% | 1694.375 | -1606.0 | 88.375 | 177.0 | -52.5 |
| +10 | 1:3 | 2m | 115 | 12 | 51 | 51 | 1 | 54.78% | 1907.25 | -1627.125 | 280.125 | 177.0 | -66.75 |
| +10 | 1:3 | 5m | 111 | 16 | 55 | 34 | 6 | 45.05% | 2588.875 | -2276.125 | 312.75 | 279.75 | -81.625 |
| +15 | 1:2 | 1m | 118 | 16 | 59 | 43 | 0 | 50.00% | 1602.75 | -1626.25 | -23.5 | 118.0 | -52.5 |
| +15 | 1:2 | 2m | 115 | 18 | 52 | 44 | 1 | 53.91% | 2071.25 | -1638.875 | 432.375 | 118.0 | -66.75 |
| +15 | 1:2 | 5m | 111 | 23 | 55 | 30 | 3 | 47.75% | 2776.0 | -2276.125 | 499.875 | 186.5 | -81.625 |
| +15 | 1:3 | 1m | 118 | 10 | 59 | 49 | 0 | 50.00% | 1651.875 | -1626.25 | 25.625 | 177.0 | -52.5 |
| +15 | 1:3 | 2m | 115 | 9 | 52 | 53 | 1 | 53.91% | 1839.375 | -1638.875 | 200.5 | 177.0 | -66.75 |
| +15 | 1:3 | 5m | 111 | 15 | 55 | 36 | 5 | 45.95% | 2690.625 | -2276.125 | 414.5 | 279.75 | -81.625 |

## Quy tắc và xác minh

- Entry GIỮ NGUYÊN từng row/timestamp/giá/chiều từ bài EMA9 SL20 TP20/30. Chờ EMA9 cùng khung đóng cắt lên đỉnh ORB hoặc xuống đáy; entry trade tick đầu tiên đủ điều kiện sau đóng nến. Không phải retest-limit.
- EMA xuyên phiên cùng raw contract, warmup300 nến từ Databento OHLCV1m; không reset08:30. Chưa chứng minh trùng tuyệt đối chart NT8 khác nguồn hoặc template. Entry logic và bộ lọc dữ liệu không thay đổi.
- Điểm chính theo TP/SL danh nghĩa, chưa phí/slippage. SL nửa box và TP3R có thể ở0,125 điểm, không phải tick MNQ hợp lệ. Stop/target làm tròn xa entry đến tick0,25 trong cột tick_rounded; RR sau làm tròn có thể khác danh nghĩa. LOCK nominal=+5/+10/+15; gap qua stop khóa vẫn có thể âm trong observed P&L. Lệnh chạm TP ngay tick kích hoạt kết thúc TP, không ghi thêm lock. rounded_net_points lưu riêng. Observed gap P&L lưu riêng, không bảo đảm market fill.
- Chỉ kiểm tra tới15:00 Houston. Lệnh chưa chạm TP/SL ghi OPEN_AT_CUTOFF, không đưa vào tổng điểm đã đóng. Win%=(TP+LOCK)/tất cả entry kể cả lệnh chưa đóng; đây là win danh nghĩa, không gán LOCK thành full TP. Tổng điểm thắng gồm cả TP và LOCK.
- 130 ngày ứng viên;8 loại toàn phiên. ORB1m/2m loại thêm4;5m loại thêm6 do receipt trễ/degraded. Không có tín hiệu:1m0,2m3,5m5. Cùng mẫu với bài EMA trước, các ngày loại có trong CSV.
- 15 test mới PASS tại CT/UTC, gồm6.000 chuỗi giả lập mỗi timezone đối chiếu first passage;6 mutation đều bị assertion phát hiện. Kế thừa15 test EMA cùng6 mutation ở bài nguồn. Tick file hash và count được kiểm tra lại;entry và ORB được đối chiếu raw ticks.
- Đã đối chiếu toàn bộ2196 dòng,2064 entry-variant (344 entry chung ×2 target ×3 lock) với bài nguồn; kiểm tra SL/TP/entry, exit thứ tự, tổng điểm, ngày thắng/thua lớn nhất và ô bảng Word. Decoder dùng chung; không cam kết dữ liệu/fill thực chính xác100%. Không sửa bot/DB/NT8.
- Mỗi dòng là một bài test riêng, không cộng thành danh mục. Audit reference docs/superpowers/AUDIT-CHECKLIST.md @ba50edf22.

## P&L bổ sung

| Khóa | RR | ORB | Nominal | Stop/target làm tròn tick | Stop theo print quan sát |
|---|---|---|---:|---:|---:|
| 5 | 2 | 1 | 18.5 | 13.75 | 13.75 |
| 5 | 2 | 2 | 687.625 | 684.25 | 681.0 |
| 5 | 2 | 5 | 401.125 | 397.5 | 397.5 |
| 5 | 3 | 1 | 128.875 | 125.5 | 125.5 |
| 5 | 3 | 2 | 538.75 | 536.5 | 533.25 |
| 5 | 3 | 5 | 245.375 | 242.5 | 242.5 |
| 10 | 2 | 1 | -28.5 | -33.25 | -33.75 |
| 10 | 2 | 2 | 581.125 | 577.75 | 574.25 |
| 10 | 2 | 5 | 474.375 | 470.75 | 470.75 |
| 10 | 3 | 1 | 88.375 | 84.5 | 84.0 |
| 10 | 3 | 2 | 280.125 | 277.5 | 274.0 |
| 10 | 3 | 5 | 312.75 | 309.75 | 309.5 |
| 15 | 2 | 1 | -23.5 | -28.25 | -30.25 |
| 15 | 2 | 2 | 432.375 | 429.0 | 426.0 |
| 15 | 2 | 5 | 499.875 | 496.25 | 496.25 |
| 15 | 3 | 1 | 25.625 | 21.5 | 19.5 |
| 15 | 3 | 2 | 200.5 | 197.5 | 194.5 |
| 15 | 3 | 5 | 414.5 | 411.5 | 411.5 |

## Số entry có 1R không lớn hơn mức khóa (phải chờ giá vượt thêm ít nhất 1 tick)

| Khóa | ORB | Ngày |
|---|---|---:|
| 5 | 1 | 0 |
| 5 | 2 | 0 |
| 5 | 5 | 0 |
| 10 | 1 | 2 |
| 10 | 2 | 0 |
| 10 | 5 | 0 |
| 15 | 1 | 8 |
| 15 | 2 | 5 |
| 15 | 5 | 1 |
