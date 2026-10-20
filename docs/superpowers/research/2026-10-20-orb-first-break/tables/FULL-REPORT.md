**FULL REPORT — MNQ ORB 1p / 2p / 5p, tái kiểm tra ngày 20/10/2026.**

| Câu hỏi | Kết luận |
|---|---|
| Engine đúng không? | PASS trong mô hình OHLC được mô tả; chưa phát hiện sai logic qua kiểm thử và đối chiếu độc lập. Không phải chứng minh fill/tick thực. |
| Có chờ nến đóng để entry không? | KHÔNG. ORB hoàn tất rồi vừa vượt biên 1 tick (0.25 điểm) là kích hoạt entry ngay. |
| Có thay đổi kết quả sau kiểm lại không? | KHÔNG. Engine SHA256 không đổi; TP/SL/chưa rõ vẫn 35/158/33; 37/163/26; 27/176/23. |
| Thành công nghĩa là gì? | Chạm TP trước SL. Giá về SL sau khi đã đóng TP không làm mất một lệnh thắng. |
| Có một tỷ lệ thắng chính xác cho cả năm không? | Không với snapshot OHLC hiện có. Báo đầy đủ kịch bản bảo thủ và thuận lợi, không giấu các lệnh chưa rõ. |
| Số lệnh | 226 lệnh mỗi bài test. 678 hàng = ba chiến lược trên cùng 226 phiên, không phải 678 lệnh của một tài khoản. |
| Thay đổi bot thật? | Không. Không DB write, không thay cấu hình, không gửi lệnh hoặc restart. |

| Quy tắc | Giá trị |
|---|---|
| Cửa sổ | 2025-10-20 inclusive đến 2026-10-20 exclusive; cuối phiên được dùng là 2026-10-19. |
| Múi giờ | Houston = America/Chicago; tự xử lý CDT/CST, không cố định UTC-5. |
| ORB | 1p = 08:30–08:31; 2p = 08:30–08:32; 5p = 08:30–08:35. |
| Entry | Phá đỉnh trước: LONG; phá đáy trước: SHORT; ngưỡng biên +/- 0.25 điểm. |
| Số lần | Tối đa 1 lệnh/ngày/bài test; thua nghỉ; không re-entry, không đảo chiều. |
| Stop | 20 điểm từ entry; gap qua stop có thể fill xấu hơn. |
| Target | 1 × (đỉnh ORB − đáy ORB), tính từ entry thực của mô hình. |
| Thoát theo giờ | 15:00 Houston nếu còn mở; giả định được công khai, 0 time-exit trong mẫu. |
| Filter | Không Mentor/day-off/EMA/AI gate; ORB thuần. |
| Chi phí | 0 điểm cho bảng gross; thêm kịch bản 2 điểm khứ hồi/lệnh. Không phải phí broker đã xác minh. |

**Bảng kết quả thành công/thất bại đã kiểm lại.**

| ORB | Lệnh | TP chắc chắn | SL chắc chắn | Chưa rõ | TP % bảo thủ | TP % thuận lợi |
|---|---|---|---|---|---|---|
| 1p | 226 | 35 | 158 | 33 | 15.49 | 30.09 |
| 2p | 226 | 37 | 163 | 26 | 16.37 | 27.88 |
| 5p | 226 | 27 | 176 | 23 | 11.95 | 22.12 |

| ORB | Bảo thủ: TP | Bảo thủ: SL | Thuận lợi: TP | Thuận lợi: SL |
|---|---|---|---|---|
| 1p | 35 | 191 | 68 | 158 |
| 2p | 37 | 189 | 63 | 163 |
| 5p | 27 | 199 | 50 | 176 |

Hai cột % là giới hạn phụ thuộc thứ tự intrabar, **không phải khoảng tin cậy thống kê**. Bảo thủ gán toàn bộ lệnh chưa rõ là SL; thuận lợi gán là TP. Đó là giả định, không phải dữ liệu tick đã thấy.

| ORB | SL chắc chắn | Ít nhất đã đi thuận ≥0.25 rồi SL | Tối đa có thể đã đi thuận rồi SL | Range TB | Range median |
|---|---|---|---|---|---|
| 1p | 158 | 47 | 158 | 56.32 | 53.75 |
| 2p | 163 | 46 | 163 | 70.96 | 65.875 |
| 5p | 176 | 65 | 175 | 96.77 | 92.625 |

| ORB | Gross thấp | Gross cao | Net 2pt thấp | Net 2pt cao | Net/lệnh thấp | Net/lệnh cao |
|---|---|---|---|---|---|---|
| 1p | -2053.5 | 423.25 | -2505.5 | -28.75 | -11.09 | -0.13 |
| 2p | -1483.5 | 751.5 | -1935.5 | 299.5 | -8.56 | 1.33 |
| 5p | -1903.75 | 848.5 | -2355.75 | 396.5 | -10.42 | 1.75 |

Tất cả P&L trên tính bằng **điểm**, không phải USD. ORB 1p vẫn âm ở cận thuận lợi sau chi phí giả định; khoảng 2p và 5p đi qua zero. Không kết luận có edge hoặc chọn khung tốt nhất từ cận thuận lợi.

**Theo tháng — mỗi ô là TP chắc chắn / SL chắc chắn / chưa rõ.** Hai tháng 10 chỉ chứa phần ngày nằm trong cửa sổ một năm.

| Tháng | Phiên | ORB 1p | ORB 2p | ORB 5p |
|---|---|---|---|---|
| 2025-10 | 10 | 3 / 7 / 0 | 2 / 5 / 3 | 3 / 7 / 0 |
| 2025-11 | 18 | 3 / 11 / 4 | 3 / 12 / 3 | 4 / 11 / 3 |
| 2025-12 | 13 | 2 / 10 / 1 | 4 / 7 / 2 | 5 / 8 / 0 |
| 2026-01 | 20 | 4 / 15 / 1 | 5 / 15 / 0 | 4 / 15 / 1 |
| 2026-02 | 19 | 2 / 14 / 3 | 4 / 14 / 1 | 0 / 14 / 5 |
| 2026-03 | 13 | 2 / 9 / 2 | 2 / 10 / 1 | 1 / 12 / 0 |
| 2026-04 | 21 | 3 / 16 / 2 | 2 / 16 / 3 | 0 / 19 / 2 |
| 2026-05 | 20 | 3 / 15 / 2 | 5 / 13 / 2 | 3 / 16 / 1 |
| 2026-06 | 17 | 3 / 10 / 4 | 0 / 13 / 4 | 0 / 15 / 2 |
| 2026-07 | 22 | 0 / 16 / 6 | 1 / 17 / 4 | 1 / 19 / 2 |
| 2026-08 | 20 | 5 / 11 / 4 | 4 / 16 / 0 | 2 / 17 / 1 |
| 2026-09 | 20 | 3 / 13 / 4 | 4 / 14 / 2 | 4 / 14 / 2 |
| 2026-10 | 13 | 2 / 11 / 0 | 1 / 11 / 1 | 0 / 9 / 4 |

| ORB | Chiều | Lệnh | TP chắc chắn | SL chắc chắn | Chưa rõ |
|---|---|---|---|---|---|
| 1 | LONG | 97 | 17 | 63 | 17 |
| 1 | SHORT | 119 | 18 | 85 | 16 |
| 1 | UNKNOWN | 10 | 0 | 10 | 0 |
| 2 | LONG | 102 | 22 | 69 | 11 |
| 2 | SHORT | 123 | 15 | 94 | 14 |
| 2 | UNKNOWN | 1 | 0 | 0 | 1 |
| 5 | LONG | 109 | 14 | 85 | 10 |
| 5 | SHORT | 117 | 13 | 91 | 13 |
| 5 | UNKNOWN | 0 | 0 | 0 | 0 |

UNKNOWN ở cột chiều không phải lúc nào cũng là kết quả chưa rõ: hai chiều đầu tiên có thể đều dẫn tới SL. Không gán những hàng đó tùy ý thành LONG hoặc SHORT.

**Ma trận tái kiểm tra engine và dữ liệu.**

| Kiểm tra | Số lượng / chứng cứ | Kết quả |
|---|---|---|
| Unit test trực tiếp simulate | 12/12; long/short, gap, ambiguity, ORB boundary, one trade, time exit | PASS |
| Giá có thứ tự độc lập | 2,000 đường giá giả lập × 3 ORB = 6,000 đối chiếu outcome/P&L/MFE | PASS — synthetic, không phải tick lịch sử |
| Mutation | 6 lỗi cố ý: stop21, bỏ tick, lookahead, stop-gap, target20, bỏ nhánh sống | 6/6 bị test bắt |
| Raw snapshot | 329,692 hàng; source split và canonical SHA256 tính lại độc lập | PASS |
| Chọn contract | 226 lựa chọn theo volume 08:00–08:29, không dùng volume cả ngày | PASS |
| Đủ nến | 226 phiên × 390 phút; không nối contract hoặc điền nến trống | PASS |
| ORB và first-break | 678 range high/low, phút phá đầu tiên, side và giá entry đối chiếu lại raw | PASS |
| Một lệnh/ngày | 678 khóa (ngày, ORB) khác nhau = 226 ngày × 3 khung | PASS |
| DST Houston | 08:30 ngày2025-10-20 =13:30UTC; ngày2025-11-07 =14:30UTC | PASS |
| Source timestamp | historical_import trừ60s; live/historical không trừ lại | Source chain đã được DS106 kiểm độc lập |
| Frozen replay | summary.json/trades.csv/sessions.csv của DB run và frozen replay giống từng byte | PASS |
| Review đối kháng DS106 | 13 probes; không tìm được phản ví dụ logic | PASS trong scope offline |
| Review dữ liệu DS102 | Counts, unique dates, source split, canonical digest | PASS; nhận định giờ entry ban đầu bị sửa theo DST |
| Historical tick execution | Không có đầy đủ tick một năm, spread/queue và fill thực | CHƯA ĐƯỢC CHỨNG MINH |

| ORB | Entry sớm nhất | Entry muộn nhất | Phút thường gặp nhất | Số lần |
|---|---|---|---|---|
| 1p | 08:31 | 08:40 | 08:31 | 180 |
| 2p | 08:32 | 08:45 | 08:32 | 135 |
| 5p | 08:35 | 09:29 | 08:35 | 126 |

Giờ trên là phút chứa entry ở Houston, không phải giây fill chính xác. Review ngoài từng dùng giờ muộn sai; bảng này được CTO tính lại bằng America/Chicago. Tất cả opening bars trong mẫu đều có mặt; không gán entry muộn là mất nến nếu chưa có chứng cứ.

**Ngày không đưa vào mẫu — công khai toàn bộ 35 ngày.** 261 ngày thứ Hai–thứ Sáu =226 included +35 excluded. Không gọi mọi ngày thiếu dữ liệu là holiday. Khoảng trống roll gồm8 ngày thường 03–12/12/2025 và9 ngày thường 03–13/03/2026.

| Ngày | Contract được chọn | Nến RTH | Lý do |
|---|---|---|---|
| 2025-11-27 | MNQ 12-25 | 210 | incomplete_390_minute_session_including_early_closes |
| 2025-11-28 | MNQ 12-25 | 225 | incomplete_390_minute_session_including_early_closes |
| 2025-12-03 |  | 0 | no_session_rows_holiday_or_missing |
| 2025-12-04 |  | 0 | no_session_rows_holiday_or_missing |
| 2025-12-05 |  | 0 | no_session_rows_holiday_or_missing |
| 2025-12-08 |  | 0 | no_session_rows_holiday_or_missing |
| 2025-12-09 |  | 0 | no_session_rows_holiday_or_missing |
| 2025-12-10 |  | 0 | no_session_rows_holiday_or_missing |
| 2025-12-11 |  | 0 | no_session_rows_holiday_or_missing |
| 2025-12-12 |  | 0 | no_session_rows_holiday_or_missing |
| 2025-12-24 | MNQ 03-26 | 225 | incomplete_390_minute_session_including_early_closes |
| 2025-12-25 |  | 0 | no_session_rows_holiday_or_missing |
| 2026-01-01 |  | 0 | no_session_rows_holiday_or_missing |
| 2026-01-19 | MNQ 03-26 | 210 | incomplete_390_minute_session_including_early_closes |
| 2026-02-16 | MNQ 03-26 | 210 | incomplete_390_minute_session_including_early_closes |
| 2026-03-03 |  | 0 | no_session_rows_holiday_or_missing |
| 2026-03-04 |  | 0 | no_session_rows_holiday_or_missing |
| 2026-03-05 |  | 0 | no_session_rows_holiday_or_missing |
| 2026-03-06 |  | 0 | no_session_rows_holiday_or_missing |
| 2026-03-09 |  | 0 | no_session_rows_holiday_or_missing |
| 2026-03-10 |  | 0 | no_session_rows_holiday_or_missing |
| 2026-03-11 |  | 0 | no_session_rows_holiday_or_missing |
| 2026-03-12 |  | 0 | no_session_rows_holiday_or_missing |
| 2026-03-13 |  | 0 | no_session_rows_holiday_or_missing |
| 2026-04-03 | MNQ 06-26 | 0 | incomplete_390_minute_session_including_early_closes |
| 2026-05-25 | MNQ 06-26 | 210 | incomplete_390_minute_session_including_early_closes |
| 2026-06-03 | MNQ 09-26 | 383 | incomplete_390_minute_session_including_early_closes |
| 2026-06-04 | MNQ 09-26 | 369 | incomplete_390_minute_session_including_early_closes |
| 2026-06-08 | MNQ 09-26 | 388 | incomplete_390_minute_session_including_early_closes |
| 2026-06-10 | MNQ 09-26 | 389 | incomplete_390_minute_session_including_early_closes |
| 2026-06-19 | MNQ 09-26 | 210 | incomplete_390_minute_session_including_early_closes |
| 2026-07-03 | MNQ 09-26 | 210 | incomplete_390_minute_session_including_early_closes |
| 2026-08-19 | MNQ 09-26 | 389 | incomplete_390_minute_session_including_early_closes |
| 2026-09-07 | MNQ 09-26 | 210 | incomplete_390_minute_session_including_early_closes |
| 2026-09-14 | MNQ 09-26 | 102 | incomplete_390_minute_session_including_early_closes |

| File | Nội dung |
|---|---|
| all-trades.csv | 678 hàng đầy đủ: ORB, entry Houston, SL, TP, kết quả, hai kịch bản, P&L, MFE. |
| all-trades.md | Toàn bộ678 lệnh dạng bảng Markdown dễ đọc. |
| successful-trades.csv | 99 hàng TP chắc chắn =35+37+27, thuộc3 test độc lập. |
| stopped-trades.csv | 497 hàng SL chắc chắn =158+163+176. |
| unresolved-trades.csv | 82 hàng chưa rõ =33+26+23. |
| monthly.csv / long-short.csv | Các bảng phân nhóm có thể lọc trong Excel. |
| excluded-dates.csv | 35 ngày không đủ điều kiện dữ liệu. |
| verification.json | Kết quả kiểm engine, mutation, raw-data/first-entry checks. |
| ../README.md | Chi tiết provenance, assumptions, source lines và cách chạy lại. |
| ../reverify.py / ../build_tables.py | Mã tái kiểm tra và mã tạo bảng; không sửa engine. |

| Giới hạn còn lại | Ý nghĩa |
|---|---|
| OHLC không có thứ tự tick | 33/26/23 kết quả cần tick để chốt; không có một win rate thực duy nhất. |
| Thiếu35 ngày trong tuần / loại ngày đóng sớm | Kết quả chỉ nói về226 phiên đầy đủ, không phải mọi ngày một năm. |
| Mô hình fill | Giá ngưỡng hoặc open khi gap; fill thật và slippage có thể khác. |
| Không tối ưu/test forward | Đây là kiểm tra bộ quy tắc đã yêu cầu trên mẫu quá khứ, không phải cam kết lợi nhuận. |
| Phần engine | Không phát hiện bug qua các kiểm tra nêu trên; không tuyên bố chứng minh đúng cho mọi dữ liệu có thể tồn tại. |

