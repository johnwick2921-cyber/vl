# Day 3.2 — LỆNH KHỚP ĐẦU TIÊN MỖI ĐÊM

Bản sửa theo xác nhận của chủ tài khoản ngày 31/10/2026. Thay thế bảng 29 lệnh trước đó đã rút lại. Trước khi khớp, tiếp tục tìm giao cắt mới và first retest EMA9; khi có giao cắt ngược thì đổi hướng. Sau lệnh đã khớp đầu tiên, giữ TP/SL và nghỉ đêm đó, dù thắng hay thua.

Kế hoạch 20/10/2025–19/04/2026: 130 ngày phiên. Đã tải 122 phiên, 109 phiên qua các tiêu chuẩn dữ liệu; chưa đủ trọn sáu tháng. 8 phiên chưa tải do báo phí khác $0; 13 phiên còn lại bị loại vì tình trạng nguồn/warmup, timestamp hoặc coverage. Những phiên đó không được coi là ngày không có tín hiệu.

| SL | TP | Lệnh | TP thắng | SL thua | Win rate | Tổng thắng | Tổng thua | Tổng điểm |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 15 | 15 | 109 | 56 | 53 | 51.38% | +840.00 | -795.50 | +44.50 |
| 16 | 15 | 109 | 57 | 52 | 52.29% | +855.00 | -834.25 | +20.75 |

SL15: 56 TP × 15 = 840.00 điểm thắng; cộng từng lệnh SL = -795.50; tổng = 44.50. Win rate = TP / (TP + SL). Chưa đóng: 0. Không có entry trong phiên hợp lệ: 0. Lỗ một lệnh lớn nhất: -15.25 điểm. Không cộng hai bài độc lập thành một tài khoản.

SL16: 57 TP × 15 = 855.00 điểm thắng; cộng từng lệnh SL = -834.25; tổng = 20.75. Win rate = TP / (TP + SL). Chưa đóng: 0. Không có entry trong phiên hợp lệ: 0. Lỗ một lệnh lớn nhất: -17.00 điểm. Không cộng hai bài độc lập thành một tài khoản.

## Giả định và giới hạn

MNQ khung 2 phút, EMA9/34 xuyên phiên. Giờ nghiên cứu 17:00 Houston ngày trước đến trước 08:30 ngày phiên, dùng America/Chicago và DST. Seed 1.000 nến 2 phút đã đóng cùng hợp đồng. Chọn hợp đồng từ volume có sẵn trong ba ngày lịch trước open, không nhìn volume tương lai.

Crossover và limit cập nhật trong nến, ở cuối nhóm sự kiện (ts_event, sequence). Video không xác định chính xác cadence của indicator hoặc độ trễ sửa lệnh; chưa chứng minh parity NT8. Limit mua làm tròn xuống, bán làm tròn lên theo tick 0,25. Limit đã tồn tại được xét trước khi sửa từ giá mới. Không dùng cùng sự kiện để vừa tạo vừa khớp lệnh; bảo vệ bắt đầu từ sự kiện sau entry.

Fill giả định chạm giá (touch), chưa mô phỏng hàng chờ/độ trễ broker. Điểm là gross, chưa trừ commission và slippage thực tế. Stop gap lấy giá bất lợi đầu tiên quan sát được, không ép mọi lỗ bằng SL; TP giới hạn tại giá target. Quy tắc một entry không bảo đảm ngày nào cũng có fill: kết quả đếm từ dữ liệu, không ép lệnh.

## Kiểm chứng

23 kiểm thử PASS trên module first_fill.py, gồm đổi hướng khi chưa khớp, không đánh tiếp sau SL, không ép entry nếu thiếu tín hiệu, EMA trong nến và thứ tự sự kiện. Bộ replay độc lập không import engine, dùng weighted seed/float đối chiếu Decimal40: 218 kết quả, 218 entry/exit gốc, không sai khác. Tổng bảng được cộng lại từ từng lệnh.

Hash SHA256 từng file tải và warmup đã kiểm tra. Giữ thứ tự record nguồn. Dataset-condition được kiểm tra lại qua API, không khác snapshot dùng trong chạy. Những ngày nguồn degraded hoặc warmup bị ảnh hưởng được loại bảo thủ. Đây là kiểm chứng phần mềm và phép tính theo giả định, không phải cam kết khớp thực tế 100%.

## Từng ngày có lệnh

| Ngày phiên | Entry Houston | Hướng | Hợp đồng | Entry | SL15 outcome | SL15 điểm | SL16 outcome | SL16 điểm |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 2025-10-30 | 2025-10-29T17:07:02-05:00 | SELL | MNQZ5 | 26209.50 | SL | -15.0 | SL | -16.0 |
| 2025-11-03 | 2025-11-02T17:23:27-06:00 | BUY | MNQZ5 | 26060.25 | TP | 15 | TP | 15 |
| 2025-11-04 | 2025-11-03T17:27:23-06:00 | SELL | MNQZ5 | 26083.50 | TP | 15 | TP | 15 |
| 2025-11-05 | 2025-11-04T17:00:15-06:00 | BUY | MNQZ5 | 25568.50 | SL | -15.25 | SL | -16.0 |
| 2025-11-06 | 2025-11-05T17:50:34-06:00 | BUY | MNQZ5 | 25750.00 | TP | 15 | TP | 15 |
| 2025-11-07 | 2025-11-06T17:07:59-06:00 | SELL | MNQZ5 | 25295.25 | TP | 15 | TP | 15 |
| 2025-11-10 | 2025-11-09T22:34:08-06:00 | BUY | MNQZ5 | 25465.75 | TP | 15 | TP | 15 |
| 2025-11-11 | 2025-11-10T17:30:34-06:00 | SELL | MNQZ5 | 25720.25 | SL | -15.0 | SL | -16.0 |
| 2025-11-12 | 2025-11-11T17:00:00-06:00 | BUY | MNQZ5 | 25665.50 | SL | -15.0 | SL | -16.0 |
| 2025-11-13 | 2025-11-12T17:00:01-06:00 | BUY | MNQZ5 | 25632.50 | SL | -15.0 | SL | -16.0 |
| 2025-11-14 | 2025-11-13T17:11:14-06:00 | BUY | MNQZ5 | 25119.00 | SL | -15.0 | TP | 15 |
| 2025-11-17 | 2025-11-16T17:00:00-06:00 | SELL | MNQZ5 | 25139.00 | TP | 15 | TP | 15 |
| 2025-11-18 | 2025-11-17T18:46:31-06:00 | SELL | MNQZ5 | 24851.50 | SL | -15.0 | SL | -16.0 |
| 2025-11-19 | 2025-11-18T17:35:18-06:00 | BUY | MNQZ5 | 24612.00 | SL | -15.0 | SL | -16.0 |
| 2025-11-20 | 2025-11-19T20:31:29-06:00 | SELL | MNQZ5 | 25141.00 | SL | -15.0 | SL | -16.0 |
| 2025-11-21 | 2025-11-20T17:30:24-06:00 | BUY | MNQZ5 | 24109.50 | TP | 15 | TP | 15 |
| 2025-11-24 | 2025-11-23T17:17:08-06:00 | BUY | MNQZ5 | 24473.25 | TP | 15 | TP | 15 |
| 2025-11-25 | 2025-11-24T17:19:56-06:00 | BUY | MNQZ5 | 24962.50 | TP | 15 | TP | 15 |
| 2025-11-26 | 2025-11-25T17:00:19-06:00 | SELL | MNQZ5 | 25065.75 | SL | -15.0 | SL | -16.0 |
| 2025-11-27 | 2025-11-26T17:12:43-06:00 | BUY | MNQZ5 | 25324.25 | TP | 15 | TP | 15 |
| 2025-12-03 | 2025-12-02T17:12:56-06:00 | BUY | MNQZ5 | 25611.75 | TP | 15 | TP | 15 |
| 2025-12-04 | 2025-12-03T17:24:22-06:00 | BUY | MNQZ5 | 25650.00 | TP | 15 | TP | 15 |
| 2025-12-05 | 2025-12-04T17:42:48-06:00 | SELL | MNQZ5 | 25623.75 | TP | 15 | TP | 15 |
| 2025-12-08 | 2025-12-07T17:06:37-06:00 | BUY | MNQZ5 | 25734.50 | TP | 15 | TP | 15 |
| 2025-12-09 | 2025-12-08T19:08:16-06:00 | SELL | MNQZ5 | 25715.50 | TP | 15 | TP | 15 |
| 2025-12-10 | 2025-12-09T18:15:11-06:00 | BUY | MNQZ5 | 25681.75 | SL | -15.0 | SL | -16.0 |
| 2025-12-11 | 2025-12-10T17:34:51-06:00 | SELL | MNQZ5 | 25734.25 | TP | 15 | TP | 15 |
| 2025-12-12 | 2025-12-11T17:31:12-06:00 | SELL | MNQZ5 | 25679.25 | TP | 15 | TP | 15 |
| 2025-12-15 | 2025-12-14T17:02:26-06:00 | BUY | MNQZ5 | 25212.25 | SL | -15.0 | SL | -16.75 |
| 2025-12-16 | 2025-12-15T17:11:26-06:00 | BUY | MNQZ5 | 25110.50 | SL | -15.0 | SL | -16.25 |
| 2025-12-17 | 2025-12-16T17:39:52-06:00 | BUY | MNQH6 | 25339.50 | TP | 15 | TP | 15 |
| 2025-12-18 | 2025-12-17T17:29:36-06:00 | SELL | MNQH6 | 24954.00 | SL | -15.0 | SL | -16.0 |
| 2025-12-19 | 2025-12-18T17:20:49-06:00 | BUY | MNQH6 | 25230.75 | TP | 15 | TP | 15 |
| 2025-12-22 | 2025-12-21T21:42:55-06:00 | SELL | MNQH6 | 25679.75 | TP | 15 | TP | 15 |
| 2025-12-23 | 2025-12-22T19:15:09-06:00 | BUY | MNQH6 | 25718.25 | SL | -15.0 | SL | -16.0 |
| 2025-12-24 | 2025-12-23T17:00:00-06:00 | SELL | MNQH6 | 25809.50 | SL | -15.0 | SL | -16.0 |
| 2025-12-26 | 2025-12-25T17:00:07-06:00 | BUY | MNQH6 | 25872.00 | TP | 15 | TP | 15 |
| 2025-12-29 | 2025-12-28T17:06:44-06:00 | SELL | MNQH6 | 25864.00 | TP | 15 | TP | 15 |
| 2025-12-30 | 2025-12-29T17:34:33-06:00 | SELL | MNQH6 | 25718.50 | TP | 15 | TP | 15 |
| 2025-12-31 | 2025-12-30T20:05:08-06:00 | SELL | MNQH6 | 25619.00 | SL | -15.0 | SL | -16.0 |
| 2026-01-02 | 2026-01-01T17:27:27-06:00 | BUY | MNQH6 | 25483.00 | TP | 15 | TP | 15 |
| 2026-01-05 | 2026-01-04T17:07:30-06:00 | BUY | MNQH6 | 25415.75 | TP | 15 | TP | 15 |
| 2026-01-06 | 2026-01-05T17:13:31-06:00 | BUY | MNQH6 | 25588.50 | SL | -15.0 | SL | -16.0 |
| 2026-01-07 | 2026-01-06T17:29:45-06:00 | SELL | MNQH6 | 25826.50 | SL | -15.0 | SL | -16.0 |
| 2026-01-08 | 2026-01-07T17:04:19-06:00 | SELL | MNQH6 | 25836.50 | TP | 15 | TP | 15 |
| 2026-01-09 | 2026-01-08T17:48:24-06:00 | SELL | MNQH6 | 25690.00 | TP | 15 | TP | 15 |
| 2026-01-12 | 2026-01-11T23:12:00-06:00 | SELL | MNQH6 | 25729.00 | TP | 15 | TP | 15 |
| 2026-01-13 | 2026-01-12T20:10:10-06:00 | BUY | MNQH6 | 25885.00 | SL | -15.0 | SL | -16.0 |
| 2026-01-14 | 2026-01-13T17:21:18-06:00 | BUY | MNQH6 | 25900.00 | SL | -15.0 | SL | -16.0 |
| 2026-01-15 | 2026-01-14T17:02:27-06:00 | SELL | MNQH6 | 25610.25 | TP | 15 | TP | 15 |
| 2026-01-16 | 2026-01-15T17:05:25-06:00 | SELL | MNQH6 | 25710.25 | SL | -15.0 | SL | -16.0 |
| 2026-01-19 | 2026-01-18T19:18:50-06:00 | BUY | MNQH6 | 25434.25 | SL | -15.0 | SL | -16.0 |
| 2026-01-20 | 2026-01-19T17:17:17-06:00 | SELL | MNQH6 | 25383.50 | SL | -15.0 | SL | -16.0 |
| 2026-01-21 | 2026-01-20T17:00:21-06:00 | BUY | MNQH6 | 25140.25 | TP | 15 | TP | 15 |
| 2026-01-22 | 2026-01-21T19:07:22-06:00 | BUY | MNQH6 | 25559.00 | TP | 15 | TP | 15 |
| 2026-01-23 | 2026-01-22T17:47:00-06:00 | SELL | MNQH6 | 25620.50 | TP | 15 | TP | 15 |
| 2026-01-26 | 2026-01-25T18:17:48-06:00 | BUY | MNQH6 | 25574.25 | TP | 15 | TP | 15 |
| 2026-01-27 | 2026-01-26T17:26:54-06:00 | BUY | MNQH6 | 25879.00 | TP | 15 | TP | 15 |
| 2026-01-28 | 2026-01-27T20:48:30-06:00 | BUY | MNQH6 | 26213.00 | SL | -15.0 | SL | -16.0 |
| 2026-01-29 | 2026-01-28T17:21:50-06:00 | SELL | MNQH6 | 26223.75 | SL | -15.0 | SL | -16.0 |
| 2026-01-30 | 2026-01-29T17:23:38-06:00 | SELL | MNQH6 | 25937.00 | SL | -15.0 | SL | -16.0 |
| 2026-02-02 | 2026-02-01T17:59:28-06:00 | BUY | MNQH6 | 25532.00 | SL | -15.0 | SL | -16.0 |
| 2026-02-03 | 2026-02-02T17:01:47-06:00 | SELL | MNQH6 | 25875.50 | SL | -15.25 | SL | -16.0 |
| 2026-02-04 | 2026-02-03T17:10:19-06:00 | SELL | MNQH6 | 25395.00 | SL | -15.0 | SL | -16.0 |
| 2026-02-05 | 2026-02-04T17:21:58-06:00 | SELL | MNQH6 | 25090.00 | TP | 15 | TP | 15 |
| 2026-02-06 | 2026-02-05T18:40:36-06:00 | BUY | MNQH6 | 24366.00 | TP | 15 | TP | 15 |
| 2026-02-09 | 2026-02-08T17:26:55-06:00 | BUY | MNQH6 | 25242.50 | SL | -15.0 | SL | -16.0 |
| 2026-02-10 | 2026-02-09T17:08:25-06:00 | BUY | MNQH6 | 25379.00 | SL | -15.0 | SL | -16.0 |
| 2026-02-11 | 2026-02-10T20:47:43-06:00 | BUY | MNQH6 | 25314.50 | TP | 15 | TP | 15 |
| 2026-02-12 | 2026-02-11T19:02:27-06:00 | BUY | MNQH6 | 25263.75 | SL | -15.0 | SL | -16.0 |
| 2026-02-13 | 2026-02-12T17:48:05-06:00 | SELL | MNQH6 | 24793.25 | SL | -15.0 | SL | -16.0 |
| 2026-02-16 | 2026-02-15T17:02:49-06:00 | SELL | MNQH6 | 24784.75 | TP | 15 | TP | 15 |
| 2026-02-17 | 2026-02-16T19:00:04-06:00 | SELL | MNQH6 | 24795.25 | SL | -15.0 | SL | -16.0 |
| 2026-02-18 | 2026-02-17T18:18:45-06:00 | BUY | MNQH6 | 24737.25 | SL | -15.0 | SL | -16.0 |
| 2026-02-19 | 2026-02-18T17:17:36-06:00 | BUY | MNQH6 | 24977.50 | SL | -15.0 | SL | -16.0 |
| 2026-02-20 | 2026-02-19T17:00:00-06:00 | SELL | MNQH6 | 24837.25 | SL | -15.0 | SL | -16.0 |
| 2026-02-23 | 2026-02-22T17:13:01-06:00 | SELL | MNQH6 | 25048.50 | TP | 15 | TP | 15 |
| 2026-02-24 | 2026-02-23T18:00:58-06:00 | BUY | MNQH6 | 24800.25 | SL | -15.0 | SL | -16.0 |
| 2026-02-25 | 2026-02-24T17:32:42-06:00 | BUY | MNQH6 | 25018.00 | TP | 15 | TP | 15 |
| 2026-02-26 | 2026-02-25T17:28:57-06:00 | SELL | MNQH6 | 25379.25 | TP | 15 | TP | 15 |
| 2026-02-27 | 2026-02-26T18:23:06-06:00 | BUY | MNQH6 | 25012.00 | SL | -15.0 | SL | -16.0 |
| 2026-03-02 | 2026-03-01T19:03:09-06:00 | BUY | MNQH6 | 24772.75 | TP | 15 | TP | 15 |
| 2026-03-03 | 2026-03-02T18:09:23-06:00 | SELL | MNQH6 | 24968.75 | TP | 15 | TP | 15 |
| 2026-03-04 | 2026-03-03T17:23:53-06:00 | BUY | MNQH6 | 24733.75 | SL | -15.0 | SL | -16.0 |
| 2026-03-05 | 2026-03-04T17:18:07-06:00 | BUY | MNQH6 | 25163.50 | SL | -15.0 | SL | -16.0 |
| 2026-03-06 | 2026-03-05T19:30:40-06:00 | SELL | MNQH6 | 25063.75 | SL | -15.0 | SL | -16.25 |
| 2026-03-09 | 2026-03-08T21:52:57-05:00 | BUY | MNQH6 | 24071.50 | TP | 15 | TP | 15 |
| 2026-03-10 | 2026-03-09T17:06:57-05:00 | BUY | MNQH6 | 24941.00 | TP | 15 | TP | 15 |
| 2026-03-11 | 2026-03-10T18:26:55-05:00 | BUY | MNQH6 | 24983.75 | TP | 15 | TP | 15 |
| 2026-03-12 | 2026-03-11T19:50:08-05:00 | BUY | MNQH6 | 24793.25 | SL | -15.0 | SL | -16.0 |
| 2026-03-13 | 2026-03-12T17:06:44-05:00 | SELL | MNQH6 | 24557.50 | SL | -15.0 | SL | -16.0 |
| 2026-03-19 | 2026-03-18T18:18:54-05:00 | BUY | MNQM6 | 24607.50 | TP | 15 | TP | 15 |
| 2026-03-20 | 2026-03-19T18:30:47-05:00 | SELL | MNQM6 | 24622.75 | TP | 15 | TP | 15 |
| 2026-03-23 | 2026-03-22T17:21:53-05:00 | SELL | MNQM6 | 24008.50 | SL | -15.0 | SL | -16.0 |
| 2026-03-24 | 2026-03-23T17:28:54-05:00 | BUY | MNQM6 | 24424.50 | TP | 15 | TP | 15 |
| 2026-03-25 | 2026-03-24T18:54:01-05:00 | SELL | MNQM6 | 24419.00 | TP | 15 | TP | 15 |
| 2026-03-26 | 2026-03-25T19:05:08-05:00 | BUY | MNQM6 | 24319.25 | TP | 15 | TP | 15 |
| 2026-03-27 | 2026-03-26T17:24:01-05:00 | BUY | MNQM6 | 23866.50 | SL | -15.0 | SL | -16.0 |
| 2026-03-30 | 2026-03-29T19:35:24-05:00 | BUY | MNQM6 | 23208.50 | SL | -15.0 | SL | -16.0 |
| 2026-03-31 | 2026-03-30T17:50:18-05:00 | SELL | MNQM6 | 23113.25 | TP | 15 | TP | 15 |
| 2026-04-01 | 2026-03-31T17:08:20-05:00 | BUY | MNQM6 | 23918.75 | TP | 15 | TP | 15 |
| 2026-04-02 | 2026-04-01T17:45:49-05:00 | BUY | MNQM6 | 24192.25 | SL | -15.0 | SL | -16.0 |
| 2026-04-06 | 2026-04-05T18:00:47-05:00 | BUY | MNQM6 | 24074.00 | SL | -15.0 | SL | -16.0 |
| 2026-04-07 | 2026-04-06T17:00:13-05:00 | SELL | MNQM6 | 24345.00 | SL | -15.0 | SL | -17.0 |
| 2026-04-08 | 2026-04-07T22:31:05-05:00 | SELL | MNQM6 | 25099.50 | SL | -15.0 | SL | -16.0 |
| 2026-04-09 | 2026-04-08T17:03:29-05:00 | BUY | MNQM6 | 25043.25 | TP | 15 | TP | 15 |
| 2026-04-15 | 2026-04-14T17:58:51-05:00 | SELL | MNQM6 | 25997.75 | SL | -15.0 | SL | -16.0 |
| 2026-04-16 | 2026-04-15T22:00:13-05:00 | SELL | MNQM6 | 26446.00 | TP | 15 | TP | 15 |
| 2026-04-17 | 2026-04-16T18:16:55-05:00 | SELL | MNQM6 | 26474.25 | TP | 15 | TP | 15 |

## Ngày không đưa vào kết quả

| Ngày | Lý do |
| --- | --- |
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

Mỗi lệnh có signal_row, entry_row, exit_row và danh sách crosses trước entry trong ledger.json. Chỉ số row là 0-based trong file TBBO của ngày, kết hợp instrument_id để truy vết. Code: research/day32-first-cross-20261031; first_fill.py, run_first_fill.py, verify_first_fill.py. Dữ liệu: run-first-fill-v1. Raw tick có giấy phép không đưa vào repo hoặc gói báo cáo. Không thay đổi bot live/NT8.
