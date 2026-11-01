# A/E — một năm và mẫu ngày thua

Thời gian: **31/10/2025–30/10/2026**. 261 ngày ứng viên; 522 dòng ngày/chiến lược. Replay A/E đầy đủ, không cộng hai mẫu bị hở ngày. 262 dòng chồng lấn với báo cáo sáu tháng gần nhất khớp chính xác outcome/entry/exit/P&L/ORB/reason.

A: ORB5m phá biên vào ngay, SL cả box, TP cả box. E: EMA9 ORB2m xác nhận đóng nến, SL nửa box, TP cả box; không dời stop. Một lệnh/ngày, một hợp đồng; EMA xuyên phiên, 300 nến cùng hợp đồng, thống nhất timestamp nhận cho nến EMA.

## Tổng một năm — lệnh đã đóng, trước phí

| Bài | Lệnh vào | TP | SL | Còn mở tại cutoff | Điểm thắng | Điểm thua | Tổng | Win % trên lệnh đóng | Chuỗi thua |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| A | 245 | 116 | 123 | 6 | 11.350,75 | -11.893,25 | **-542,50** | 48,54% | 9 |
| E | 244 | 89 | 154 | 1 | 6.496,75 | -5.591,50 | **+905,25** | 36,63% | 13 |

A: 16 ngày bị loại. E: 14 bị loại, 3 không vào. Các lệnh còn mở là chưa chạm TP/SL tại 15:00 Houston; không tự tạo giá thoát, không cộng vào tổng đóng. Mỗi ngày là một episode độc lập, chưa mô phỏng carry-over các vị thế đó. Đây không phải NAV cuối cùng của một tài khoản giao dịch thực.

## Dấu hiệu E: entry muộn

Trong 01/05–30/10/2026: 25 lệnh E vào từ 09:00 Houston trở đi, 5 TP / 20 SL, tổng -547,75 điểm. Giữ trước 09:00: 101 lệnh, 37 TP / 64 SL, +392,25 thay vì -155,50.

| E | 31/10/2025–30/04/2026 | 01/05–30/10/2026 | Tổng năm |
|---|---:|---:|---:|
| Gốc, điểm đóng | +1.060,75 | -155,50 | +905,25 |
| Entry trước 09:00, điểm đóng | +1.289,50 | +392,25 | **+1.681,75** |
| Entry trước 09:00, trừ giả định 2 điểm/lệnh | +1.107,50 | +190,25 | **+1.297,75** |

Bộ lọc E giữ 192 lệnh, 78 TP / 114 SL, không còn lệnh mở; win 40,625%, chuỗi thua tối đa 10 lệnh, drawdown equity lệnh đóng 583,75 điểm. Bản gốc có chuỗi thua 13 và DD 866 điểm. Bỏ entry >=09:00, không tìm lệnh thay thế, không sửa SL/TP.

Đây là ứng viên thử tiếp, chưa phải giải pháp đã chứng minh. Đã xem mẫu gần đây trước khi xây phân tích này; chia thời gian không làm nó trở thành holdout độc lập. Có 6 bộ lọc A và 9 bộ lọc E ngoài baseline, thử riêng từng bộ, chưa tối ưu kết hợp. Để minh bạch, chọn bộ có tổng điểm sau giả định 2 điểm/lệnh cao nhất ở phần đầu, tối thiểu50 lệnh đóng: A chọn baseline, E chọn trước09:00. Không dùng kết quả phần sau trong hàm chọn nhưng việc phân tích vẫn mang tính khám phá sau khi xem dữ liệu.

## A: box lớn gây thiệt hại lớn, nhưng lọc chưa ổn định

Sáu tháng mới, A với box >100 điểm: 63 lệnh đóng, 26 TP / 37 SL, -1.631 điểm; thêm4 lệnh còn mở. Ngày thắng có box trung bình100,58, ngày thua108,74 điểm. Đây là tương quan mô tả, chưa xác định nguyên nhân thị trường.

Chỉ giữ box<=75 điểm: phần đầu46 lệnh **-330,25**; phần sau24 lệnh **+306,25**; cả năm70 lệnh **-24,00** trước phí. Không có cơ sở gọi đây là bộ lọc cứu A ổn định. Box<=50 trong phần sau chỉ3 lệnh, không đủ mẫu.

## Các đặc điểm khác

Sáu tháng mới: SHORT của A -1.593,75 điểm (59 lệnh đóng,3 còn mở), SHORT của E -362,75 (60 lệnh). Không đủ để tự chuyển sang long-only: chưa kiểm định bộ lọc đó theo thời gian. E ngày thắng có độ mở rộng ngoài ORB trung bình0,857box, ngày thua0,734box; không hỗ trợ kết luận đơn giản rằng đuổi xa luôn gây thua. Bảng theo thứ có trong feature-buckets.csv; không chọn bỏ một thứ chỉ vì đẹp trên mẫu này.

Có233 ngày cả A/E đều có lệnh đóng;96 ngày cả hai cùng âm. Chưa phân tích tin tức, overnight trend, volume, VWAP hay spread/queue. Chưa kết luận được nguyên nhân thị trường của từng ngày thua.

## Kiểm tra và giới hạn

Replay gọi oracle entry/exit, kiểm tra stop P&L bằng row giá gốc. Tổng Decimal và chuỗi được đối chiếu. Tổng từng bucket và early+recent khớp year cho gross/cost2/số lệnh/thắng/thua/mở. Mọi bộ lọc chỉ dùng độ rộng box, giờ entry hoặc khoảng cách entry với biên ORB đã biết tại entry. Thời gian tới exit chỉ là mô tả hậu nghiệm, không dùng lọc.

Stop theo tick quan sát vượt stop; TP giả định khớp khi chạm. Chưa chứng minh queue/bid-ask/latency và chart NT8 parity. 2 điểm/lệnh là độ nhạy chi phí, không phải phí thực đã xác nhận. Win rate trên lệnh đóng; chuỗi bỏ ngày loại/không trade/lệnh mở, không phải chuỗi tất cả ngày lịch. Chưa chạy lại vòng đời Lucid/payout cho bộ lọc. Không thay bot hoặc cấu hình giao dịch.

Bước kiểm tra tiếp: đóng băng quy tắc E trước09:00 và chi phí/fill, thử trên dữ liệu chưa dùng hoặc paper forward; không tiếp tục đổi ngưỡng theo kết quả tương lai. Chưa có kết luận bộ lọc sẽ giữ được lợi nhuận.
