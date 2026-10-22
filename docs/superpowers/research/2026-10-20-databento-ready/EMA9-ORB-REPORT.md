# MNQ ORB — EMA9 CROSS ON CLOSED CANDLE

20/10/2025–19/04/2026; Houston America/Chicago, DST aware. ORB là 1/2/5 phút đầu từ08:30, EMA9 cùng khung. Một lệnh/ngày mỗi bài; không BE, không re-entry.

| ORB | SL | TP | Lệnh | Thắng | Thua | TP % | Tổng thắng | Tổng thua | Lãi/lỗ | Ngày thắng lớn nhất | Ngày thua lớn nhất |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 1m | 20 | 20 | 118 | 53 | 65 | 44.92% | 1060 | -1300 | -240 | 20 | -20 |
| 1m | 20 | 30 | 118 | 46 | 72 | 38.98% | 1380 | -1440 | -60 | 30 | -20 |
| 2m | 20 | 20 | 115 | 64 | 51 | 55.65% | 1280 | -1020 | 260 | 20 | -20 |
| 2m | 20 | 30 | 115 | 49 | 66 | 42.61% | 1470 | -1320 | 150 | 30 | -20 |
| 5m | 20 | 20 | 111 | 58 | 53 | 52.25% | 1160 | -1060 | 100 | 20 | -20 |
| 5m | 20 | 30 | 111 | 44 | 67 | 39.64% | 1320 | -1340 | -20 | 30 | -20 |

## Quy tắc và xác minh

- EMA9 alpha=2/(9+1)=0,2. Chỉ nến đã đóng: EMA trước ≤ đỉnh ORB và EMA mới > đỉnh → LONG; EMA trước ≥ đáy và EMA mới < đáy → SHORT. Chạm bằng biên chưa phải cắt. Không yêu cầu màu nến. Không dùng râu hoặc tín hiệu trong nến.
- Không lấy nến tạo ORB làm tín hiệu. Tín hiệu đầu tiên sau đó; entry là trade tick đầu tiên sau khi nến hoàn tất và tất cả dữ liệu xác nhận đã nhận. Không vào ở giá đóng nến xác nhận hoặc giá biên ORB giả định.
- EMA xuyên phiên, cùng raw contract được chọn trước open. Warmup300 nến đã đóng từ OHLCV1m Databento, gộp2m/5m theo mốc thời gian; không reset08:30. Sai số seed được chặn <1e-20 tick theo dải warmup. Thiếu nến do không có giao dịch không tự thêm giá. Không cam kết trùng từng giá EMA trên NT8 khác nguồn/template phiên.
- TP20 hoặc30, SL20 tính từ entry. Báo cáo chính dùng fill danh nghĩa; stop-gap theo tick quan sát lưu riêng observed_net_points. Entry trade-price là proxy market fill, không có bid/ask, queue, độ trễ broker, phí/slippage. Không phải cam kết kết quả khớp thực.
- Chỉ đánh giá đến15:00 Houston. OPEN_AT_CUTOFF không tự chốt lệnh hoặc tính P&L; lần chạy này không còn lệnh mở. Tỷ lệ TP = TP/tổng entry.
- 130 ngày trong tuần;8 loại ở kiểm tra toàn phiên. Loại thêm theo từng ORB vì receipt ORB trễ hoặc ngày Databento degraded nằm trong phiên/warmup. Bộ lọc degraded áp dụng bảo thủ cấp dataset, không khẳng định MNQ riêng bị lỗi. Ngày không tín hiệu không phải lệnh thua.
- 15 test PASS ở CT và UTC;600 ca giả lập đối chiếu mỗi timezone. 6 mutation đều bị phát hiện bằng assertion, không phải crash. 714 dòng qua hai bộ tính;18 dòng bị chặn trước mô phỏng vì chất lượng. audit.json oracle_checked_rows là tổng candidate732, không phải số gọi oracle thực; số chính xác nằm ở final-verification.json.
- Đối chiếu hash, mẫu ngày, contract, crossing, bracket distances, receipt-time, entry sau close, TP20/TP30 cùng entry, số lệnh và tổng điểm. Shared tick decoder; không chứng minh dữ liệu sàn hoàn hảo hoặc toàn bộ bot live. Không sửa DB/bot/NT8.
- Giả định nghiên cứu: ETH không reset, raw contract không back-adjust;300 nến warmup. Report reference docs/superpowers/AUDIT-CHECKLIST.md @ba50edf22. Không cộng các dòng thành một danh mục.

## Các trường hợp bị loại

| Ngày | ORB | Lý do |
|---|---|---|
| 2025-10-31 | 1 | late_orb_receipt |
| 2025-11-04 | 2 | late_orb_receipt |
| 2025-12-01 | 5 | provider_degraded_dates:2025-11-28 |
| 2025-12-04 | 2 | late_orb_receipt |
| 2026-02-10 | 5 | late_orb_receipt |
| 2026-03-12 | 1 | late_orb_receipt |
| 2026-03-16 | 1 | provider_degraded_dates:2026-03-16 |
| 2026-03-16 | 2 | provider_degraded_dates:2026-03-16 |
| 2026-03-16 | 5 | provider_degraded_dates:2026-03-15,2026-03-16 |
| 2026-03-17 | 5 | provider_degraded_dates:2026-03-16 |
| 2026-04-10 | 1 | provider_degraded_dates:2026-04-10 |
| 2026-04-10 | 2 | provider_degraded_dates:2026-04-10 |
| 2026-04-10 | 5 | provider_degraded_dates:2026-04-10 |
| 2026-04-13 | 5 | provider_degraded_dates:2026-04-10 |

## Ngày không có tín hiệu

| Ngày | ORB |
|---|---|
| 2025-10-24 | 2 |
| 2025-11-03 | 2 |
| 2025-11-03 | 5 |
| 2025-12-04 | 5 |
| 2025-12-26 | 5 |
| 2026-02-06 | 2 |
| 2026-02-23 | 5 |
| 2026-04-06 | 5 |
