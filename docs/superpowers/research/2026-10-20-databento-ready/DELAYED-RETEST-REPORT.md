# MNQ ORB retest: đợi ½ nến và một nến

6 tháng 20/10/2025–19/04/2026. ORB mở lúc 08:30 Houston. TP cả box, ba kiểu SL giữ nguyên. Một lệnh/ngày mỗi bài; không tái vào.
Đợi từ tick phá biên đầu tiên. ½ nến: ORB1m đợi30s, ORB2m đợi60s, ORB5m đợi150s. Một nến: đợi60/120/300s. Đây là đợi thời gian, không yêu cầu thân nến đóng ngoài ORB.

## Đợi ½ nến

| Stop | ORB | Lệnh | TP | SL | BE | Chưa đóng | Không khớp | Điểm thắng | Điểm thua | Tổng điểm |
|---|---|---|---|---|---|---|---|---|---|---|
| SL cả box | 1m | 114 | 39 | 75 | 0 | 0 | 6 | 2170.5 | -4034.25 | -1863.75 |
| SL cả box | 2m | 118 | 55 | 63 | 0 | 0 | 2 | 3652.25 | -4136.0 | -483.75 |
| SL cả box | 5m | 111 | 49 | 60 | 0 | 2 | 10 | 4715.75 | -4793.25 | -77.5 |
| SL nửa box | 1m | 114 | 26 | 88 | 0 | 0 | 6 | 1459.5 | -2372.625 | -913.125 |
| SL nửa box | 2m | 118 | 37 | 81 | 0 | 0 | 2 | 2480.5 | -2653.875 | -173.375 |
| SL nửa box | 5m | 111 | 29 | 82 | 0 | 0 | 10 | 2855.25 | -3453.875 | -598.625 |
| SL20; lời20 kéo BE | 1m | 114 | 14 | 77 | 23 | 0 | 6 | 714.5 | -1540.0 | -825.5 |
| SL20; lời20 kéo BE | 2m | 118 | 19 | 73 | 26 | 0 | 2 | 1036.75 | -1460.0 | -423.25 |
| SL20; lời20 kéo BE | 5m | 111 | 8 | 85 | 18 | 0 | 10 | 662.5 | -1700.0 | -1037.5 |

## Đợi một nến

| Stop | ORB | Lệnh | TP | SL | BE | Chưa đóng | Không khớp | Điểm thắng | Điểm thua | Tổng điểm |
|---|---|---|---|---|---|---|---|---|---|---|
| SL cả box | 1m | 113 | 37 | 76 | 0 | 0 | 7 | 2043.75 | -4101.0 | -2057.25 |
| SL cả box | 2m | 113 | 47 | 66 | 0 | 0 | 7 | 3152.75 | -4299.75 | -1147.0 |
| SL cả box | 5m | 108 | 45 | 60 | 0 | 3 | 13 | 4350.0 | -4769.75 | -419.75 |
| SL nửa box | 1m | 113 | 24 | 89 | 0 | 0 | 7 | 1332.75 | -2406.0 | -1073.25 |
| SL nửa box | 2m | 113 | 31 | 82 | 0 | 0 | 7 | 2096.0 | -2678.25 | -582.25 |
| SL nửa box | 5m | 108 | 25 | 82 | 0 | 1 | 13 | 2448.0 | -3462.875 | -1014.875 |
| SL20; lời20 kéo BE | 1m | 113 | 14 | 77 | 22 | 0 | 7 | 670.25 | -1540.0 | -869.75 |
| SL20; lời20 kéo BE | 2m | 113 | 16 | 70 | 27 | 0 | 7 | 864.25 | -1400.0 | -535.75 |
| SL20; lời20 kéo BE | 5m | 108 | 10 | 75 | 23 | 0 | 13 | 884.5 | -1500.0 | -615.5 |

## Kiểm tra thời điểm khớp

| Đợi | ORB | Khớp cùng nến phá | Giá đã vào box lúc kích hoạt |
|---|---|---:|---:|
| half_candle | 1 | 63 | 65 |
| half_candle | 2 | 56 | 66 |
| half_candle | 5 | 67 | 62 |
| full_candle | 1 | 0 | 66 |
| full_candle | 2 | 0 | 65 |
| full_candle | 5 | 0 | 60 |

## Giả định và xác minh

- Limit chỉ được xét khớp từ thời điểm đủ thời gian. Các lần chạm trước đó không tính. Giữ cùng biên phá đầu tiên; không đổi chiều. Giả định chạm là khớp ở giá limit.
- Nếu tới giờ đặt lệnh mà giá đã ở phía trong box, mô hình có thể khớp ngay trên tick đầu tiên đủ giờ, tại giá limit giả định (không hưởng cải thiện giá). Đây không phải bằng chứng giá đã ra ngoài rồi quay lại thêm lần nữa. Marketable-at-activation và số khớp cùng cây nến được ghi riêng trong summary.csv.
- ½ nến vẫn có thể khớp trong cùng cây nến phá. Đợi đủ một nến bảo đảm entry không cùng cây nến phá. Nếu muốn bắt buộc chờ nến đóng hoặc yêu cầu giá còn ngoài box thì đó là quy tắc khác, chưa áp dụng.
- Mốc đặt lệnh và khớp được xấp xỉ bằng tick giao dịch đủ giờ đầu tiên, không phải mô phỏng bid/ask, hàng đợi hoặc latency. Chạm limit không bảo đảm fill thật.
- Điểm theo stop/target đặt sẵn, chưa phí hoặc trượt giá. BE tính0; mức khớp quan sát riêng trong trades.csv. SL nửa box lý thuyết có thể nằm ở nửa tick; tổng khi làm tròn ra ngoài theo0,25 điểm có trong summary.csv.
- Chưa đóng lúc15:00 được ghi OPEN_AT_CUTOFF và loại khỏi P&L đã đóng. Không retest tính tới15:00; không tự tạo exit. Gap qua limit và stop tính SL ngay theo mô hình bảo thủ.
- 130 ngày được kiểm tra,122 đạt kiểm tra toàn phiên; thêm2/2/1 trường hợp ORB1m/2m/5m bị loại do dữ liệu ORB nhận trễ. Cùng bộ dữ liệu và breakout baseline.
- 50 test mỗi múi giờ CT/UTC;10.800 so sánh delay giả lập mỗi múi giờ;4 lỗi thời gian cố ý bị phát hiện. Tất cả 2196 dòng dữ liệu thật được đối chiếu bằng thuật toán first-passage riêng; cùng decoder nên không phải kiểm chứng độc lập độ đầy đủ dữ liệu sàn.
- Mọi số tổng, thứ tự thời gian, cờ cùng nến, hash, và ô bảng Word/Excel đã được đối chiếu. AUDIT-CHECKLIST.md @ ba50edf22. Không thay đổi bot live/DB/NT8.

Engine SHA256: 9c4ea681845beafa04733feea3bce433ba67b9d87ae9e4c41fe352628302f7d2
