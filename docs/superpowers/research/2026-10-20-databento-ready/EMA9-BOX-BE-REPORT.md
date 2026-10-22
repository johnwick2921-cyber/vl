# MNQ EMA9 ORB — SL NỬA BOX / TP2R–3R / +1R KÉO BE

20/10/2025–19/04/2026; mở08:30 Houston; ORB1m/2m/5m. SL=½ độ rộng ORB; TP2R hoặc3R từ entry. Lời+1R thì kéo stop về entry từ record tick tiếp theo, sau đó giữ nguyên. Một lệnh/ngày mỗi bài, không re-entry.

| RR | ORB | Lệnh | TP | SL | BE | Chưa đóng | TP % | Tổng thắng | Tổng thua | Lãi/lỗ điểm | Ngày thắng lớn nhất | Ngày thua lớn nhất |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 1:2 | 1m | 118 | 33 | 57 | 28 | 0 | 27.97% | 1747.75 | -1606.0 | 141.75 | 118.0 | -52.5 |
| 1:2 | 2m | 115 | 32 | 51 | 31 | 1 | 27.83% | 2314.5 | -1627.125 | 687.375 | 118.0 | -66.75 |
| 1:2 | 5m | 111 | 27 | 55 | 26 | 3 | 24.32% | 2587.25 | -2276.125 | 311.125 | 186.5 | -81.625 |
| 1:3 | 1m | 118 | 23 | 57 | 38 | 0 | 19.49% | 1926.75 | -1606.0 | 320.75 | 177.0 | -52.5 |
| 1:3 | 2m | 115 | 22 | 51 | 40 | 2 | 19.13% | 2304.0 | -1627.125 | 676.875 | 177.0 | -66.75 |
| 1:3 | 5m | 111 | 18 | 55 | 32 | 6 | 16.22% | 2424.0 | -2276.125 | 147.875 | 279.75 | -81.625 |

## Quy tắc và xác minh

- Entry GIỮ NGUYÊN từng row/timestamp/giá/chiều từ bài EMA9 SL20 TP20/30. Chờ EMA9 cùng khung đóng cắt lên đỉnh ORB hoặc xuống đáy; entry trade tick đầu tiên đủ điều kiện sau đóng nến. Không phải retest-limit.
- EMA xuyên phiên cùng raw contract, warmup300 nến từ Databento OHLCV1m; không reset08:30. Chưa chứng minh trùng tuyệt đối chart NT8 khác nguồn hoặc template. Entry logic và bộ lọc dữ liệu không thay đổi.
- Điểm chính theo TP/SL danh nghĩa, chưa phí/slippage. SL nửa box và TP3R có thể ở0,125 điểm, không phải tick MNQ hợp lệ. Stop/target làm tròn xa entry đến tick0,25 trong cột tick_rounded; RR sau làm tròn có thể khác danh nghĩa. BE nominal=0; gap qua entry vẫn có thể âm trong observed P&L. Lệnh chạm TP ngay tick kích hoạt1R kết thúc TP, không ghi thêm BE. rounded_net_points lưu riêng. Observed gap P&L lưu riêng, không bảo đảm market fill.
- Chỉ kiểm tra tới15:00 Houston. Lệnh chưa chạm TP/SL ghi OPEN_AT_CUTOFF, không đưa vào tổng điểm đã đóng. Tỷ lệ TP=TP/tất cả entry kể cả lệnh chưa đóng.
- 130 ngày ứng viên;8 loại toàn phiên. ORB1m/2m loại thêm4;5m loại thêm6 do receipt trễ/degraded. Không có tín hiệu:1m0,2m3,5m5. Cùng mẫu với bài EMA trước, các ngày loại có trong CSV.
- 13 test mới PASS tại CT/UTC, gồm4.000 chuỗi giả lập mỗi timezone đối chiếu first passage;5 mutation đều bị assertion phát hiện. Kế thừa15 test EMA cùng6 mutation ở bài nguồn. Tick file hash và count được kiểm tra lại;entry và ORB được đối chiếu raw ticks.
- Đã đối chiếu toàn bộ732 dòng,688 entry-variant (344 entry chung ×2 target) với bài nguồn; kiểm tra SL/TP/entry, exit thứ tự, tổng điểm, ngày thắng/thua lớn nhất và ô bảng Word. Decoder dùng chung; không cam kết dữ liệu/fill thực chính xác100%. Không sửa bot/DB/NT8.
- Mỗi dòng là một bài test riêng, không cộng thành danh mục. Audit reference docs/superpowers/AUDIT-CHECKLIST.md @ba50edf22.

## P&L bổ sung

| RR | ORB | Nominal | Stop/target làm tròn tick | Stop theo print quan sát |
|---|---|---:|---:|---:|
| 2 | 1 | 141.75 | 137.0 | 136.75 |
| 2 | 2 | 687.375 | 684.0 | 681.0 |
| 2 | 5 | 311.125 | 307.5 | 307.5 |
| 3 | 1 | 320.75 | 317.75 | 317.5 |
| 3 | 2 | 676.875 | 674.75 | 671.75 |
| 3 | 5 | 147.875 | 145.0 | 145.0 |
