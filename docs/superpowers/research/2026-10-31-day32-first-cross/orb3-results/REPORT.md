# ORB — kiểm tra lại ba nhóm, 01/05–30/10/2026

131 ngày trong lịch ngày thường; 51 cấu hình; 6.681 dòng ngày/cấu hình. Mỗi cấu hình chạy độc lập trên MNQ, tối đa một lệnh/ngày. Mở ORB 08:30 America/Chicago (Houston, tự điều chỉnh DST).

## Quy tắc và giới hạn

- Breakout: phá biên bằng ít nhất một tick, vào theo giá tick đầu tiên sau khi ORB hoàn thành.
- Retest touch: phá trước, tick sau quay lại chạm biên mới vào limit. Half/full-body: nến cùng khung ORB phải đóng với ít nhất nửa/toàn bộ thân ngoài biên; sau đó mới cho phép retest. Không dùng wick và không vào hồi tố trong nến xác nhận.
- EMA9: 300 nến hoàn thành cùng hợp đồng trước open, xuyên phiên; EMA9 đóng cắt biên ORB, vào tick đủ điều kiện tiếp theo. Không reset EMA tại open.
- halfbox_lock5: SL nửa box, TP cả box; khóa +5 một lần khi lời đạt max(1R, 5,25 điểm), để stop nằm phía sau giá. Đây không phải trailing liên tục.
- box = SL cả box; half_box = SL nửa box; 20_to_BE = SL20, lời20 kéo về entry. Trừ TP20/TP30 ghi riêng, target luôn bằng độ rộng cả box tính từ entry.
- Cửa sổ theo dõi kết thúc 15:00 Houston. Lệnh còn mở được ghi riêng, không tự gán thắng/thua và không tự tạo giá thoát cuối phiên.
- Điểm SL dùng giá giao dịch đầu tiên vượt stop: có thể lỗ hơn khoảng stop vì gap. TP giả định fill tại target khi chạm; retest giả định limit fill khi chạm. Không mô phỏng queue, bid/ask execution hay latency; đây là mô hình backtest, không phải bảo đảm fill thật.
- Breakout/retest dùng timestamp sự kiện; EMA dùng nến timestamp nhận để khớp OHLCV warmup Databento. Không khẳng định giống chart NT8 tuyệt đối. Giữ thứ tự bản ghi provider; chưa chạy độ nhạy cho các bản ghi trùng event/sequence.
- Điểm là gross cho một hợp đồng, chưa phí. Cột net_2pt_sensitivity trong CSV chỉ là phép trừ thêm 2 điểm/lệnh đóng, không phải phí thực đã xác nhận.
- Tỷ lệ thắng = số lệnh P&L dương / số lệnh đã đóng. BE/LOCK là cơ chế thoát, không đảm bảo P&L bằng0/dương nếu có gap.
- Chuỗi là các lệnh đã đóng liên tiếp trong mẫu, bỏ ngày không giao dịch/bị loại/còn mở; không suy ra kết quả của ngày thiếu. Drawdown chỉ tính equity lệnh đã đóng, không phải intratrade hay quy tắc tài khoản funded.
- So sánh 51 biến thể trên cùng mẫu là lựa chọn trong mẫu; chưa có kiểm định out-of-sample cho lựa chọn tốt nhất.

## Kiểm tra

77 unit tests PASS (74 engine, 3 clock). Replay chạy các oracle entry/exit của module tương ứng và đối chiếu row giá raw. Tổng hợp độc lập bằng Decimal kiểm tra 6.681 khóa duy nhất, 51 tổng điểm, số lệnh, phân loại, chuỗi và drawdown. Các kiểm tra này không biến giả định fill thành giao dịch thực.

## breakout

| ORB | Quy tắc | Lệnh | TP | SL | BE | Lock | Mở | Loại | Không vào | Thắng % | Điểm thắng | Điểm thua | Tổng | Chuỗi thắng | Chuỗi thua | Max ngày thắng | Max ngày thua | DD đóng |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 1 | SL20_box | 124 | 30 | 94 | 0 | 0 | 0 | 7 | 0 | 24.19 | 1736.75 | -1880.75 | -144.0 | 3 | 13 | 100.25 | -20.25 | 358.25 |
| 1 | box | 124 | 62 | 62 | 0 | 0 | 0 | 7 | 0 | 50.0 | 3885.0 | -3531.75 | 353.25 | 5 | 7 | 125.25 | -125.5 | 855.25 |
| 1 | half_box | 124 | 43 | 81 | 0 | 0 | 0 | 7 | 0 | 34.68 | 2744.75 | -2342.5 | 402.25 | 4 | 7 | 117.25 | -62.75 | 424.0 |
| 1 | 20_to_BE | 124 | 20 | 65 | 39 | 0 | 0 | 7 | 0 | 16.13 | 1100.75 | -1300.75 | -200.0 | 2 | 8 | 96.5 | -20.25 | 453.0 |
| 2 | SL20_box | 124 | 27 | 97 | 0 | 0 | 0 | 7 | 0 | 21.77 | 1906.25 | -1940.75 | -34.5 | 2 | 14 | 108.25 | -20.75 | 323.25 |
| 2 | box | 124 | 58 | 66 | 0 | 0 | 0 | 7 | 0 | 46.77 | 4499.75 | -4893.5 | -393.75 | 6 | 7 | 134.75 | -131.25 | 1054.0 |
| 2 | half_box | 124 | 39 | 85 | 0 | 0 | 0 | 7 | 0 | 31.45 | 3004.0 | -3201.0 | -197.0 | 5 | 9 | 134.75 | -65.75 | 623.25 |
| 2 | 20_to_BE | 124 | 23 | 58 | 43 | 0 | 0 | 7 | 0 | 18.55 | 1629.75 | -1161.0 | 468.75 | 2 | 6 | 108.25 | -20.75 | 220.25 |
| 5 | SL20_box | 124 | 21 | 103 | 0 | 0 | 0 | 7 | 0 | 16.94 | 1948.0 | -2061.25 | -113.25 | 2 | 19 | 190.75 | -20.5 | 476.75 |
| 5 | box | 124 | 53 | 67 | 0 | 0 | 4 | 7 | 0 | 44.17 | 5331.0 | -7285.25 | -1954.25 | 4 | 9 | 190.75 | -204.25 | 2252.75 |
| 5 | half_box | 124 | 39 | 84 | 0 | 0 | 1 | 7 | 0 | 31.71 | 3840.5 | -4597.5 | -757.0 | 4 | 10 | 190.75 | -102.25 | 950.0 |
| 5 | 20_to_BE | 124 | 15 | 62 | 47 | 0 | 0 | 7 | 0 | 12.1 | 1335.5 | -1241.25 | 94.25 | 2 | 5 | 190.75 | -20.5 | 416.0 |

## retest

| ORB | Quy tắc | Lệnh | TP | SL | BE | Lock | Mở | Loại | Không vào | Thắng % | Điểm thắng | Điểm thua | Tổng | Chuỗi thắng | Chuỗi thua | Max ngày thắng | Max ngày thua | DD đóng |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 1 | touch_box | 123 | 61 | 62 | 0 | 0 | 0 | 7 | 1 | 49.59 | 3845.75 | -3530.75 | 315.0 | 5 | 7 | 125.25 | -125.5 | 855.0 |
| 1 | touch_half_box | 123 | 42 | 81 | 0 | 0 | 0 | 7 | 1 | 34.15 | 2705.5 | -2342.5 | 363.0 | 4 | 7 | 117.25 | -62.75 | 424.5 |
| 1 | touch_20_to_BE | 123 | 20 | 64 | 39 | 0 | 0 | 7 | 1 | 16.26 | 1133.25 | -1281.25 | -148.0 | 2 | 9 | 96.5 | -20.25 | 361.5 |
| 1 | half_body_box | 112 | 59 | 53 | 0 | 0 | 0 | 7 | 12 | 52.68 | 3651.0 | -3144.75 | 506.25 | 5 | 6 | 125.25 | -125.5 | 580.75 |
| 1 | half_body_half_box | 112 | 45 | 67 | 0 | 0 | 0 | 7 | 12 | 40.18 | 2717.5 | -2043.25 | 674.25 | 3 | 10 | 117.25 | -62.75 | 304.5 |
| 1 | half_body_20_to_BE | 112 | 25 | 56 | 31 | 0 | 0 | 7 | 12 | 22.32 | 1255.5 | -1120.0 | 135.5 | 2 | 7 | 85.25 | -20.0 | 332.0 |
| 1 | full_body_box | 108 | 61 | 47 | 0 | 0 | 0 | 7 | 16 | 56.48 | 3786.25 | -2794.0 | 992.25 | 6 | 6 | 125.5 | -94.25 | 408.25 |
| 1 | full_body_half_box | 108 | 45 | 63 | 0 | 0 | 0 | 7 | 16 | 41.67 | 2676.5 | -1955.75 | 720.75 | 4 | 9 | 117.25 | -62.75 | 314.5 |
| 1 | full_body_20_to_BE | 108 | 27 | 52 | 29 | 0 | 0 | 7 | 16 | 25.0 | 1347.5 | -1040.0 | 307.5 | 2 | 11 | 85.25 | -20.0 | 286.25 |
| 2 | touch_box | 124 | 58 | 66 | 0 | 0 | 0 | 7 | 0 | 46.77 | 4499.75 | -4893.25 | -393.5 | 6 | 7 | 134.75 | -131.25 | 1053.75 |
| 2 | touch_half_box | 124 | 39 | 85 | 0 | 0 | 0 | 7 | 0 | 31.45 | 3004.0 | -3201.5 | -197.5 | 5 | 9 | 134.75 | -65.75 | 623.25 |
| 2 | touch_20_to_BE | 124 | 24 | 58 | 42 | 0 | 0 | 7 | 0 | 19.35 | 1701.5 | -1160.75 | 540.75 | 2 | 6 | 108.25 | -20.5 | 220.0 |
| 2 | half_body_box | 107 | 53 | 53 | 0 | 0 | 1 | 7 | 17 | 50.0 | 4093.75 | -4016.0 | 77.75 | 5 | 3 | 134.75 | -120.25 | 621.75 |
| 2 | half_body_half_box | 107 | 29 | 78 | 0 | 0 | 0 | 7 | 17 | 27.1 | 2182.5 | -3035.75 | -853.25 | 3 | 12 | 134.75 | -66.0 | 940.5 |
| 2 | half_body_20_to_BE | 107 | 12 | 58 | 37 | 0 | 0 | 7 | 17 | 11.21 | 755.75 | -1160.5 | -404.75 | 3 | 9 | 88.0 | -20.0 | 510.75 |
| 2 | full_body_box | 103 | 49 | 52 | 0 | 0 | 2 | 7 | 21 | 48.51 | 3663.75 | -4045.25 | -381.5 | 5 | 4 | 134.75 | -125.25 | 784.5 |
| 2 | full_body_half_box | 103 | 26 | 77 | 0 | 0 | 0 | 7 | 21 | 25.24 | 1921.0 | -2992.5 | -1071.5 | 3 | 12 | 134.75 | -66.0 | 1217.25 |
| 2 | full_body_20_to_BE | 103 | 9 | 53 | 41 | 0 | 0 | 7 | 21 | 8.74 | 547.75 | -1060.75 | -513.0 | 2 | 7 | 88.0 | -20.0 | 549.5 |
| 5 | touch_box | 124 | 52 | 67 | 0 | 0 | 5 | 7 | 0 | 43.7 | 5140.25 | -7285.25 | -2145.0 | 4 | 9 | 178.25 | -204.25 | 2443.5 |
| 5 | touch_half_box | 124 | 38 | 84 | 0 | 0 | 2 | 7 | 0 | 31.15 | 3649.75 | -4597.5 | -947.75 | 4 | 10 | 178.25 | -102.25 | 1140.75 |
| 5 | touch_20_to_BE | 124 | 14 | 63 | 47 | 0 | 0 | 7 | 0 | 11.29 | 1144.75 | -1261.0 | -116.25 | 2 | 5 | 158.5 | -20.25 | 490.0 |
| 5 | half_body_box | 105 | 34 | 61 | 0 | 0 | 10 | 7 | 19 | 35.79 | 3366.25 | -6662.5 | -3296.25 | 3 | 8 | 178.25 | -204.25 | 3518.25 |
| 5 | half_body_half_box | 105 | 26 | 73 | 0 | 0 | 6 | 7 | 19 | 26.26 | 2660.0 | -3957.0 | -1297.0 | 3 | 8 | 178.25 | -102.25 | 1618.25 |
| 5 | half_body_20_to_BE | 105 | 9 | 56 | 40 | 0 | 0 | 7 | 19 | 8.57 | 838.25 | -1122.0 | -283.75 | 1 | 8 | 178.25 | -20.0 | 440.25 |
| 5 | full_body_box | 101 | 34 | 56 | 0 | 0 | 11 | 7 | 23 | 37.78 | 3316.0 | -6158.25 | -2842.25 | 3 | 8 | 170.5 | -204.25 | 3227.75 |
| 5 | full_body_half_box | 101 | 24 | 70 | 0 | 0 | 7 | 7 | 23 | 25.53 | 2427.25 | -3796.0 | -1368.75 | 3 | 12 | 170.5 | -102.25 | 1555.0 |
| 5 | full_body_20_to_BE | 101 | 9 | 56 | 36 | 0 | 0 | 7 | 23 | 8.91 | 839.75 | -1121.75 | -282.0 | 1 | 8 | 140.75 | -20.0 | 438.0 |

## EMA9

| ORB | Quy tắc | Lệnh | TP | SL | BE | Lock | Mở | Loại | Không vào | Thắng % | Điểm thắng | Điểm thua | Tổng | Chuỗi thắng | Chuỗi thua | Max ngày thắng | Max ngày thua | DD đóng |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 1 | SL20_TP20 | 127 | 71 | 56 | 0 | 0 | 0 | 4 | 0 | 55.91 | 1420 | -1120.25 | 299.75 | 12 | 4 | 20 | -20.25 | 240.25 |
| 1 | SL20_TP30 | 127 | 50 | 77 | 0 | 0 | 0 | 4 | 0 | 39.37 | 1500 | -1540.25 | -40.25 | 5 | 13 | 30 | -20.25 | 340.25 |
| 1 | halfbox_fullbox | 127 | 41 | 86 | 0 | 0 | 0 | 4 | 0 | 32.28 | 2266.75 | -2658.25 | -391.5 | 4 | 10 | 117.25 | -62.75 | 699.75 |
| 1 | halfbox_lock5 | 127 | 25 | 64 | 0 | 38 | 0 | 4 | 0 | 49.61 | 1660.0 | -1989.0 | -329.0 | 5 | 5 | 115.75 | -62.75 | 610.75 |
| 2 | SL20_TP20 | 126 | 68 | 58 | 0 | 0 | 0 | 4 | 1 | 53.97 | 1360 | -1160.0 | 200.0 | 6 | 5 | 20 | -20.0 | 140.0 |
| 2 | SL20_TP30 | 126 | 53 | 73 | 0 | 0 | 0 | 4 | 1 | 42.06 | 1590 | -1460.0 | 130.0 | 5 | 5 | 30 | -20.0 | 140.0 |
| 2 | halfbox_fullbox | 126 | 42 | 84 | 0 | 0 | 0 | 4 | 1 | 33.33 | 3094.25 | -3249.75 | -155.5 | 4 | 13 | 131.0 | -67.5 | 866.0 |
| 2 | halfbox_lock5 | 126 | 27 | 63 | 0 | 36 | 0 | 4 | 1 | 50.0 | 2091.25 | -2493.5 | -402.25 | 5 | 5 | 131.0 | -67.5 | 771.0 |
| 5 | SL20_TP20 | 125 | 48 | 77 | 0 | 0 | 0 | 4 | 2 | 38.4 | 960 | -1540.25 | -580.25 | 4 | 11 | 20 | -20.25 | 580.25 |
| 5 | SL20_TP30 | 125 | 40 | 85 | 0 | 0 | 0 | 4 | 2 | 32.0 | 1200 | -1700.25 | -500.25 | 4 | 11 | 30 | -20.25 | 580.25 |
| 5 | halfbox_fullbox | 125 | 37 | 84 | 0 | 0 | 4 | 4 | 2 | 30.58 | 3830.5 | -4514.5 | -684.0 | 6 | 12 | 204.25 | -105.5 | 1458.0 |
| 5 | halfbox_lock5 | 125 | 27 | 67 | 0 | 31 | 0 | 4 | 2 | 46.4 | 2925.75 | -3699.5 | -773.75 | 6 | 9 | 178.25 | -105.5 | 1445.0 |

## Các lý do loại theo dòng ngày/cấu hình

{
  "opening_range_record_received_after_decision": 117,
  "incomplete_390_minute_session_including_early_closes": 204
}

## Provenance

Raw audit: /home/hoang/fleet-1017/out/orb3-recent-20261031/run-v2/audit.json
Ledger: /home/hoang/fleet-1017/out/orb3-recent-20261031/run-v2/trades.csv
Harness SHA256: cd84cd5f6b694fb8e5bd6c7182884fce2241bf76dc8881199639f1fa23272714

## Bổ sung định nghĩa

- Retest touch chọn hướng phá đầu tiên; retest thân nến chọn nến đầu tiên đạt điều kiện xác nhận, có thể khác hướng phá đầu tiên.
- `details/outcome-rates.csv` tách TP / lệnh đóng, TP / lệnh vào, và lệnh P&L dương / lệnh đóng.
- `details/streaks.csv` ghi ngày bắt đầu/kết thúc từng chuỗi; chuỗi ở biên mẫu có thể còn tiếp diễn ngoài mẫu.
- `details/common-days.csv` so sánh lại tổng điểm trên giao các ngày không bị loại ở bất kỳ cấu hình nào. Lệnh còn mở vẫn chưa được tính P&L.
- Không phải tất cả phép kiểm tra dùng thuật toán hoàn toàn độc lập: decoder và một số quy tắc được dùng chung. Chưa chứng minh fill thật hay chart NT8 parity.
