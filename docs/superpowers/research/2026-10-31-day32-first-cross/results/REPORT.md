# Day 3.2 — EMA9/EMA34, first overnight crossover only

## Kết quả trên phần dữ liệu đã kiểm tra

**Mẫu chưa đủ trọn 6 tháng.** Kế hoạch: 20/10/2025–19/04/2026; 130 ngày phiên trong tuần. Đã tải 122 phiên, 109 phiên được nhận vào phép tính. 8 phiên chưa tải có báo phí và 13 phiên khác bị loại; không coi ngày thiếu dữ liệu là không có tín hiệu.

| SL (điểm) | TP | Lệnh | Thắng TP | Thua SL | Win rate | Tổng thắng | Tổng thua | Tổng điểm gross |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 15 | 15 | 29 | 17 | 12 | 58.62% | +255.00 | -180.00 | +75.00 |
| 16 | 15 | 29 | 17 | 12 | 58.62% | +255.00 | -192.00 | +63.00 |

Win rate = 17 / 29 = 58,620689655%. TP thắng = 17 × 15 = 255 điểm. SL15: 12 × 15 = 180 điểm, net 75. SL16: 12 × 16 = 192 điểm, net 63. Mỗi bài là thử nghiệm độc lập; không cộng hai bài thành một tài khoản.

80/109 phiên không có entry: trong mô hình intrabar này, EMA giao cắt ngược trở lại trước khi limit retest khớp; cơ hội đầu tiên bị hủy và không sử dụng giao cắt sau. 29 phiên còn lại đều đóng TP hoặc SL, không có vị thế chưa đóng ở cutoff. Quy tắc hủy này là quy ước mô phỏng đã ghi trước khi chạy, chưa được chủ tài khoản xác nhận riêng từ video.

Các điểm trên chưa trừ commission, spread/queue/slippage thực tế hay độ trễ sửa lệnh. Không thể gọi đây là lợi nhuận giao dịch thực nhận. SL15 có tổng gross cao hơn SL16 trong mẫu này, nhưng chỉ 29 lệnh không đủ chứng minh phương pháp tốt hơn trong tương lai.

## Quy tắc được chạy

MNQ 2 phút, EMA9/EMA34 xuyên phiên, seed từ 1.000 nến 2 phút đã đóng của cùng hợp đồng. Chỉ giao cắt đầu tiên sau 17:00 Houston, tối đa một entry mỗi phiên; TP15, chạy SL15 và SL16 riêng; không BE, trailing hoặc vào lại. Giao cắt lên đặt BUY LIMIT tại EMA9; xuống đặt SELL LIMIT. Limit cập nhật theo EMA9 và làm tròn tick 0,25: buy xuống, sell lên.

Giờ nghiên cứu: 17:00 ngày trước → trước 08:30 ngày ghi trong bảng, America/Chicago có DST. Đây là cách định nghĩa “nguyên đêm” cho nghiên cứu; video không chốt chính xác giờ kết thúc. Hợp đồng được chọn theo volume trong 3 ngày lịch trước open, chỉ dùng các phút đã đóng; không dùng volume tương lai. Không dùng chuỗi continuous back-adjusted cho giá khớp.

Crossover intrabar được đo ở cuối nhóm (ts_event, sequence); không chờ nến 2 phút đóng. Video cho thấy nến đang hình thành và limit di chuyển, nhưng không chứng minh chính xác cadence của indicator/NT8. Vì vậy đây là một cách mô hình hóa rõ ràng, KHÔNG tuyên bố parity 100% với NT8/video.

Limit đã tồn tại được xử lý trước giá mới dùng để sửa limit. Các record cùng sự kiện không được dùng để vừa tạo lệnh vừa khớp lệnh; bracket chỉ được đánh giá ở sự kiện sau entry. Fill giả định chạm giá (touch), chưa mô phỏng hàng chờ. Stop gap dùng giá bất lợi đầu tiên quan sát được, không ép lỗ về đúng SL; trong 29 lệnh của mỗi bài này không có vượt SL. TP khớp tại giá target, không nhận thêm lợi do gap.

## Kiểm chứng

[A] 218 kết quả được replay bằng bộ tính độc lập float/weighted EMA seed, đối chiếu engine Decimal40; 0 sai khác. 58 entry/exit được so với row tick gốc. Tổng điểm được cộng lại từ ledger; TP + SL + chưa đóng khớp số entry.

[A] 22 kiểm thử engine PASS. Ba lỗi cố ý cài vào bản sao (EMA tính lặp từng tick, phản ứng cùng sự kiện, ép stop gap) đều bị test phát hiện. Mỗi phiên đã tải có SHA256; warmup SHA256 được xác minh. Đã đọc lại metadata tình trạng dataset từ Databento và đối chiếu snapshot cũ: không có khác biệt condition.

[A] Giữ nguyên thứ tự record từ nguồn, không tự sort để che lỗi. Phiên có receive timestamp trước event bị loại. Phiên không đủ tick đầu/cuối hoặc gap lớn cần review bị loại. Ngày degraded trong phần warmup cũng loại để không dùng seed bị ảnh hưởng. Đây là lựa chọn bảo thủ; loại dữ liệu có thể làm lệch mẫu, không suy rộng kết quả thành toàn bộ sáu tháng.

## Từng phiên có lệnh

| Ngày phiên | Giờ entry Houston | Hợp đồng | Hướng | Entry | TP/SL bài 15 | Điểm SL15 | TP/SL bài 16 | Điểm SL16 |
|---|---|---|---|---:|---|---:|---|---:|
| 2025-11-04 | 2025-11-03T17:27:23-06:00 | MNQZ5 | SELL | 26083.50 | TP | +15.00 | TP | +15.00 |
| 2025-11-12 | 2025-11-11T17:00:00-06:00 | MNQZ5 | BUY | 25665.50 | SL | -15.00 | SL | -16.00 |
| 2025-11-18 | 2025-11-17T18:46:31-06:00 | MNQZ5 | SELL | 24851.50 | SL | -15.00 | SL | -16.00 |
| 2025-11-24 | 2025-11-23T17:17:08-06:00 | MNQZ5 | BUY | 24473.25 | TP | +15.00 | TP | +15.00 |
| 2025-12-03 | 2025-12-02T17:12:56-06:00 | MNQZ5 | BUY | 25611.75 | TP | +15.00 | TP | +15.00 |
| 2025-12-05 | 2025-12-04T17:42:48-06:00 | MNQZ5 | SELL | 25623.75 | TP | +15.00 | TP | +15.00 |
| 2025-12-09 | 2025-12-08T19:08:16-06:00 | MNQZ5 | SELL | 25715.50 | TP | +15.00 | TP | +15.00 |
| 2025-12-10 | 2025-12-09T18:15:11-06:00 | MNQZ5 | BUY | 25681.75 | SL | -15.00 | SL | -16.00 |
| 2025-12-11 | 2025-12-10T17:34:51-06:00 | MNQZ5 | SELL | 25734.25 | TP | +15.00 | TP | +15.00 |
| 2025-12-12 | 2025-12-11T17:31:12-06:00 | MNQZ5 | SELL | 25679.25 | TP | +15.00 | TP | +15.00 |
| 2025-12-22 | 2025-12-21T21:42:55-06:00 | MNQH6 | SELL | 25679.75 | TP | +15.00 | TP | +15.00 |
| 2025-12-24 | 2025-12-23T17:00:00-06:00 | MNQH6 | SELL | 25809.50 | SL | -15.00 | SL | -16.00 |
| 2026-01-02 | 2026-01-01T17:27:27-06:00 | MNQH6 | BUY | 25483.00 | TP | +15.00 | TP | +15.00 |
| 2026-01-19 | 2026-01-18T19:18:50-06:00 | MNQH6 | BUY | 25434.25 | SL | -15.00 | SL | -16.00 |
| 2026-01-27 | 2026-01-26T17:26:54-06:00 | MNQH6 | BUY | 25879.00 | TP | +15.00 | TP | +15.00 |
| 2026-01-30 | 2026-01-29T17:23:38-06:00 | MNQH6 | SELL | 25937.00 | SL | -15.00 | SL | -16.00 |
| 2026-02-06 | 2026-02-05T18:40:36-06:00 | MNQH6 | BUY | 24366.00 | TP | +15.00 | TP | +15.00 |
| 2026-02-12 | 2026-02-11T19:02:27-06:00 | MNQH6 | BUY | 25263.75 | SL | -15.00 | SL | -16.00 |
| 2026-02-13 | 2026-02-12T17:48:05-06:00 | MNQH6 | SELL | 24793.25 | SL | -15.00 | SL | -16.00 |
| 2026-02-20 | 2026-02-19T17:00:00-06:00 | MNQH6 | SELL | 24837.25 | SL | -15.00 | SL | -16.00 |
| 2026-02-25 | 2026-02-24T17:32:42-06:00 | MNQH6 | BUY | 25018.00 | TP | +15.00 | TP | +15.00 |
| 2026-03-02 | 2026-03-01T19:03:09-06:00 | MNQH6 | BUY | 24772.75 | TP | +15.00 | TP | +15.00 |
| 2026-03-09 | 2026-03-08T21:52:57-05:00 | MNQH6 | BUY | 24071.50 | TP | +15.00 | TP | +15.00 |
| 2026-03-19 | 2026-03-18T18:18:54-05:00 | MNQM6 | BUY | 24607.50 | TP | +15.00 | TP | +15.00 |
| 2026-03-25 | 2026-03-24T18:54:01-05:00 | MNQM6 | SELL | 24419.00 | TP | +15.00 | TP | +15.00 |
| 2026-04-01 | 2026-03-31T17:08:20-05:00 | MNQM6 | BUY | 23918.75 | TP | +15.00 | TP | +15.00 |
| 2026-04-06 | 2026-04-05T18:00:47-05:00 | MNQM6 | BUY | 24074.00 | SL | -15.00 | SL | -16.00 |
| 2026-04-08 | 2026-04-07T22:31:05-05:00 | MNQM6 | SELL | 25099.50 | SL | -15.00 | SL | -16.00 |
| 2026-04-15 | 2026-04-14T17:58:51-05:00 | MNQM6 | SELL | 25997.75 | SL | -15.00 | SL | -16.00 |

## Phiên không được đưa vào kết quả

| Ngày | Lý do |
|---|---|
| 2025-10-20 | NOT_DOWNLOADED_NONZERO_QUOTE |
| 2025-10-21 | NOT_DOWNLOADED_NONZERO_QUOTE |
| 2025-10-22 | NOT_DOWNLOADED_NONZERO_QUOTE |
| 2025-10-23 | NOT_DOWNLOADED_NONZERO_QUOTE |
| 2025-10-24 | NOT_DOWNLOADED_NONZERO_QUOTE |
| 2025-10-27 | NOT_DOWNLOADED_NONZERO_QUOTE |
| 2025-10-28 | NOT_DOWNLOADED_NONZERO_QUOTE |
| 2025-10-29 | NOT_DOWNLOADED_NONZERO_QUOTE |
| 2025-10-31 | TIMESTAMP_OR_RECORD_ANOMALY |
| 2025-11-28 | DEGRADED_PROVIDER_DATE:2025-11-28 |
| 2025-12-01 | DEGRADED_PROVIDER_DATE:2025-11-28 |
| 2025-12-02 | DEGRADED_PROVIDER_DATE:2025-11-28 |
| 2025-12-25 | NO_OVERNIGHT_TICKS |
| 2026-01-01 | NO_OVERNIGHT_TICKS |
| 2026-03-16 | DEGRADED_PROVIDER_DATE:2026-03-15,2026-03-16 |
| 2026-03-17 | DEGRADED_PROVIDER_DATE:2026-03-15,2026-03-16 |
| 2026-03-18 | DEGRADED_PROVIDER_DATE:2026-03-16 |
| 2026-04-03 | COVERAGE_REVIEW_REQUIRED |
| 2026-04-10 | DEGRADED_PROVIDER_DATE:2026-04-10 |
| 2026-04-13 | DEGRADED_PROVIDER_DATE:2026-04-10 |
| 2026-04-14 | DEGRADED_PROVIDER_DATE:2026-04-10 |

NOT_DOWNLOADED_NONZERO_QUOTE: chưa tải, chưa có quyền tính phí. NO_OVERNIGHT_TICKS: file không có tick hợp đồng được chọn trong giờ nghiên cứu. DEGRADED_PROVIDER_DATE: metadata nguồn báo degraded trong giờ nghiên cứu hoặc warmup. COVERAGE_REVIEW_REQUIRED: không qua ngưỡng kiểm tra đầu/cuối phiên/gap; chưa khẳng định là mất dữ liệu hay lịch đóng cửa. TIMESTAMP_OR_RECORD_ANOMALY: phát hiện timestamp không hợp lệ theo tiêu chuẩn nghiên cứu.

## Tái lập và bằng chứng

Code: branch research/day32-first-cross-20261031. Input root: /home/hoang/fleet-1017/out/day32-first-cross-20261031/. Run cuối: run-v3. run-v1 là lượt kiểm tra bị loại toàn bộ do kiểm tra sequence quá rộng; KHÔNG phải kết quả chiến lược. run-v2/v3 có cùng kết quả, v3 thêm row/time lý do hủy.

Xem ledger.json/trades.csv cho signal_row, entry_row, exit_row, cancel_row và timestamp; row là vị trí record 0-based trong file TBBO của ngày. instrument_id + file ngày + row xác định dữ liệu gốc. census.json chứa kiểm tra coverage. Raw dữ liệu cấp phép không được đẩy lên GitHub.

Nguồn schema: https://databento.com/docs/schemas/tbbo ; xử lý nhiều record/sự kiện: https://databento.com/docs/examples/order-book/order-tracking ; quyền lợi Standard: https://databento.com/pricing/ . Không có chỉnh sửa bot live, NT8 hay tài khoản giao dịch.
