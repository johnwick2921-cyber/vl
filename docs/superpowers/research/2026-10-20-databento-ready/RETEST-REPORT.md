# MNQ ORB retest-limit — 6 tháng

**Đã kiểm tra tính toán; kết quả mô phỏng với các giới hạn bên dưới.**

20/10/2025–19/04/2026. Open 08:30 Houston. ORB 1m/2m/5m; giá phá biên trước, đặt limit tại biên vừa phá, chỉ khớp khi giá quay lại. Một lệnh/ngày cho mỗi bài; không tái vào lệnh. TP = cả box tính từ limit entry. SL cả box / nửa box / SL20 và lời20 kéo stop về entry.

## Kết quả chính: giả định chạm limit là khớp

| Stop | ORB | Lệnh | TP | SL | BE | Chưa đóng | Không retest | Điểm thắng | Điểm thua | Tổng điểm |
|---|---|---|---|---|---|---|---|---|---|---|
| SL cả box | 1m | 119 | 47 | 72 | 0 | 0 | 1 | 2552.25 | -3919.25 | -1367.0 |
| SL cả box | 2m | 119 | 59 | 60 | 0 | 0 | 1 | 3951.0 | -3988.0 | -37.0 |
| SL cả box | 5m | 119 | 63 | 54 | 0 | 2 | 2 | 5801.5 | -4324.0 | 1477.5 |
| SL nửa box | 1m | 119 | 35 | 84 | 0 | 0 | 1 | 1876.5 | -2297.5 | -421.0 |
| SL nửa box | 2m | 119 | 42 | 77 | 0 | 0 | 1 | 2799.25 | -2569.875 | 229.375 |
| SL nửa box | 5m | 119 | 44 | 75 | 0 | 0 | 2 | 3988.5 | -3195.5 | 793.0 |
| SL20, +20 kéo BE | 1m | 119 | 21 | 68 | 30 | 0 | 1 | 1017.0 | -1360.0 | -343.0 |
| SL20, +20 kéo BE | 2m | 119 | 26 | 62 | 31 | 0 | 1 | 1478.75 | -1240.0 | 238.75 |
| SL20, +20 kéo BE | 5m | 119 | 18 | 60 | 41 | 0 | 2 | 1284.75 | -1200.0 | 84.75 |

## Kiểm tra độ nhạy: phải xuyên limit 1 tick mới tính khớp

| Stop | ORB | Lệnh | TP | SL | BE | Chưa đóng | Không retest | Điểm thắng | Điểm thua | Tổng điểm |
|---|---|---|---|---|---|---|---|---|---|---|
| SL cả box | 1m | 118 | 46 | 72 | 0 | 0 | 2 | 2512.5 | -3919.25 | -1406.75 |
| SL cả box | 2m | 119 | 58 | 61 | 0 | 0 | 1 | 3888.75 | -4050.25 | -161.5 |
| SL cả box | 5m | 119 | 61 | 56 | 0 | 2 | 2 | 5661.25 | -4464.25 | 1197.0 |
| SL nửa box | 1m | 118 | 34 | 84 | 0 | 0 | 2 | 1836.75 | -2297.5 | -460.75 |
| SL nửa box | 2m | 119 | 41 | 78 | 0 | 0 | 1 | 2737.0 | -2601.0 | 136.0 |
| SL nửa box | 5m | 119 | 42 | 77 | 0 | 0 | 2 | 3848.25 | -3265.625 | 582.625 |
| SL20, +20 kéo BE | 1m | 118 | 20 | 69 | 29 | 0 | 2 | 977.25 | -1380.0 | -402.75 |
| SL20, +20 kéo BE | 2m | 119 | 25 | 63 | 31 | 0 | 1 | 1416.5 | -1260.0 | 156.5 |
| SL20, +20 kéo BE | 5m | 119 | 16 | 62 | 41 | 0 | 2 | 1144.5 | -1240.0 | -95.5 |

## Quy tắc và kiểm tra

- Điểm tính theo mức stop/target đặt sẵn, chưa trừ phí, spread hoặc độ trễ. BE được tính 0 điểm theo mô hình; khớp thật có thể trượt giá.
- Khớp limit khi chạm là giả định lạc quan. Xuyên 1 tick cũng không chứng minh vị trí hàng đợi hoặc khả năng khớp thật. Hai bảng không phải kết quả giao dịch broker.
- Giữ limit tại biên phá đầu tiên tới 15:00 Houston. Không quay lại = NO_RETEST; không phá = NO_BREAKOUT. Đã vào nhưng chưa chạm TP/SL/BE tới 15:00 = OPEN_AT_CUTOFF, không tự tạo P&L.
- Giá gap qua limit và stop cùng một print: tính SL ngay tại print đó theo giả định bảo thủ; không cho phép giá hồi sau đó cứu lệnh. Giá fill giả định ở limit, không hưởng cải thiện giá.
- TP chạm trước hoặc cùng tick kích hoạt BE thì TP kết thúc lệnh. BE chỉ có hiệu lực từ record tiếp theo; không mô phỏng độ trễ sửa lệnh.
- Nửa box có thể nằm ở nửa tick 0,125 điểm. Bảng chính tính đúng nửa box lý thuyết; cột net_tick_rounded_points làm tròn stop ra ngoài theo tick 0,25. Giá nửa tick không thể gửi thành lệnh MNQ thật.
- 130 ngày trong tuần; 122 đạt kiểm tra toàn phiên. Cùng hash, contract, loại trừ và breakout row IDs như bản baseline. 8 ngày bị loại toàn phiên; ORB 1m/2m/5m loại thêm 2/2/1 do ORB nhận trễ.
- 41 test PASS ở CT và UTC, gồm 10.800 phép so sánh giả lập retest mỗi múi giờ; 8 lỗi cố ý bị phát hiện bằng assertion. 2196 dòng được đối chiếu bằng phép tính first-passage riêng.
- Hàm đọc DBN và kiểm tra dữ liệu được dùng chung với baseline: đây không phải xác minh độc lập độ đầy đủ của dữ liệu sàn. Không tuyên bố xác minh toàn bộ bot live.
- Đã kiểm tra riêng: giá entry/stop/target, thứ tự row, tổng điểm, mẫu số, census, hash và mọi ô Excel. Tham chiếu AUDIT-CHECKLIST.md @ ba50edf22.
- Các bài test riêng trùng ngày; không cộng thành một danh mục một lệnh/ngày. Không thay đổi bot, DB, NT8 hoặc giao dịch live.

Engine SHA256: ba98dac1cb36a91803d623ffb03eaccf19e38752cfadecfc8a9c9bc6bead6aae
