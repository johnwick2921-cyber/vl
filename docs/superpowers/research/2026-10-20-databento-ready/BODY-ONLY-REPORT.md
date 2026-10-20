# MNQ ORB RETEST — XÁC NHẬN THÂN NẾN ĐÃ ĐÓNG

ĐÚNG YÊU CẦU BODY ONLY. Báo cáo này thay thế bài chờ thời gian, vốn hiểu sai ý chủ sở hữu. Không dùng chờ30/60/150/300 giây để xác nhận.

Giai đoạn20/10/2025–19/04/2026. Open08:30 Houston. ORB1m dùng nến1m, ORB2m dùng nến2m, ORB5m dùng nến5m. Nến được căn từ08:30; ORB là nến đầu tiên.

## Ít nhất ½ thân ngoài ORB

| Stop | ORB | Lệnh | TP | SL | BE | Chưa đóng | Điểm thắng | Điểm thua | Tổng điểm |
|---|---|---|---|---|---|---|---|---|---|
| SL cả box | 1m | 105 | 50 | 55 | 0 | 0 | 2831.5 | -2947.75 | -116.25 |
| SL cả box | 2m | 102 | 54 | 48 | 0 | 0 | 3719.5 | -3004.0 | 715.5 |
| SL cả box | 5m | 97 | 52 | 42 | 0 | 3 | 4804.5 | -3392.25 | 1412.25 |
| SL nửa box | 1m | 105 | 34 | 71 | 0 | 0 | 1951.75 | -1913.75 | 38.0 |
| SL nửa box | 2m | 102 | 40 | 62 | 0 | 0 | 2807.75 | -1957.875 | 849.875 |
| SL nửa box | 5m | 97 | 36 | 59 | 0 | 2 | 3522.25 | -2413.875 | 1108.375 |
| SL20; lời20 kéo BE | 1m | 105 | 23 | 49 | 33 | 0 | 1121.0 | -980.0 | 141.0 |
| SL20; lời20 kéo BE | 2m | 102 | 17 | 53 | 32 | 0 | 1039.0 | -1060.0 | -21.0 |
| SL20; lời20 kéo BE | 5m | 97 | 18 | 46 | 33 | 0 | 1497.25 | -920.0 | 577.25 |

## Toàn bộ thân ngoài ORB

| Stop | ORB | Lệnh | TP | SL | BE | Chưa đóng | Điểm thắng | Điểm thua | Tổng điểm |
|---|---|---|---|---|---|---|---|---|---|
| SL cả box | 1m | 103 | 49 | 54 | 0 | 0 | 2842.0 | -2812.25 | 29.75 |
| SL cả box | 2m | 99 | 54 | 45 | 0 | 0 | 3619.5 | -2860.75 | 758.75 |
| SL cả box | 5m | 89 | 48 | 37 | 0 | 4 | 4507.5 | -3003.75 | 1503.75 |
| SL nửa box | 1m | 103 | 33 | 70 | 0 | 0 | 1866.75 | -1893.75 | -27.0 |
| SL nửa box | 2m | 99 | 38 | 61 | 0 | 0 | 2700.25 | -1890.0 | 810.25 |
| SL nửa box | 5m | 89 | 35 | 51 | 0 | 3 | 3520.75 | -2071.875 | 1448.875 |
| SL20; lời20 kéo BE | 1m | 103 | 23 | 49 | 31 | 0 | 1160.75 | -980.0 | 180.75 |
| SL20; lời20 kéo BE | 2m | 99 | 16 | 51 | 32 | 0 | 1011.0 | -1020.0 | -9.0 |
| SL20; lời20 kéo BE | 5m | 89 | 16 | 39 | 34 | 0 | 1342.5 | -780.0 | 562.5 |

## Các ngày không vào lệnh

| Điều kiện | ORB | Không xác nhận | Không retest |
|---|---|---:|---:|
| half_body | 1 | 0 | 15 |
| half_body | 2 | 0 | 18 |
| half_body | 5 | 0 | 24 |
| full_body | 1 | 0 | 17 |
| full_body | 2 | 0 | 21 |
| full_body | 5 | 0 | 32 |

## Quy tắc và xác minh

- ½ thân: nến đã đóng ngoài ORB, ít nhất50% chiều dài thân open-close nằm ngoài biên. Full thân: cả open và close nằm nghiêm ngặt ngoài cùng biên. Không tính râu. Doji bỏ qua vì thân có chiều dài0.
- Chọn nến đóng đầu tiên đạt điều kiện của từng bài. Close phía trên ORB → buy limit tại đỉnh; close phía dưới → sell limit tại đáy. Không thêm điều kiện màu nến: thân đỏ hoàn toàn phía trên vẫn đạt điều kiện full; thân xanh hoàn toàn phía dưới cũng vậy.
- Chỉ cho phép fill sau khi nến xác nhận hoàn thành và dữ liệu nến đã nhận; không dùng lần chạm bên trong nến xác nhận. 0 entry trong cùng nến xác nhận. Chạm limit sau đó được giả định khớp ở giá biên.
- Một lệnh/ngày mỗi bài; không tái vào, không chuyển limit sang hướng khác. TP luôn bằng cả box từ entry. SL cả box / nửa box / SL20, khi lời20 kéo stop về entry. TP chạm trước hoặc cùng lúc kích hoạt BE thì TP kết thúc lệnh.
- Tới15:00 Houston: không có nến đủ điều kiện = NO_CONFIRMATION; có xác nhận nhưng chưa khớp = NO_RETEST; đã khớp nhưng chưa chạm exit = OPEN_AT_CUTOFF, không gán P&L đã đóng.
- Điểm chính theo stop/target đặt sẵn, chưa phí hoặc trượt giá. BE tính0 theo mô hình. Half-box có thể ở nửa tick; kết quả làm tròn ra ngoài theo tick0,25 và observed crossing PnL nằm riêng trong summary.csv.
- Giả định chạm limit là khớp không chứng minh fill thật; không mô phỏng hàng đợi, bid/ask hay độ trễ broker. Gap qua cả limit và stop trên tick fill tính SL ngay theo giả định bảo thủ.
- 130 ngày trong tuần;122 đạt kiểm tra toàn phiên. Cùng dữ liệu, contract và loại trừ với baseline; ORB1m/2m/5m loại thêm2/2/1 trường hợp do ORB nhận trễ.
- 62 test PASS mỗi múi giờ CT/UTC;5.400 so sánh body giả lập mỗi múi giờ;6 lỗi cố ý về body/close/receipt bị phát hiện. 2196 dòng dữ liệu thật đối chiếu bằng first-passage riêng, bao gồm xác nhận nến độc lập.
- Kiểm tra lại body fraction, biên limit, entry/SL/TP, thứ tự thời gian, tổng điểm, mẫu số, hash, census và mọi ô Word/Excel. Decoder dùng chung: không phải kiểm chứng độc lập tính đầy đủ dữ liệu sàn hoặc toàn bộ bot live.
- Audit reference: docs/superpowers/AUDIT-CHECKLIST.md @ba50edf22. Mỗi bài riêng, có thể trùng ngày; không gộp thành danh mục một lệnh/ngày. Không sửa DB/NT8/bot live.

Engine SHA256: 11b8e24701404208b05d86264d7de1a49547ed47cf28f7525140c1da01bcce3b
