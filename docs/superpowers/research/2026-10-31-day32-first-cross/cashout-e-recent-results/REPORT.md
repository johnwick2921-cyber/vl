# E trước09:00 — cash-out sáu tháng gần nhất

01/05–30/10/2026. 101 tín hiệu đã kiểm tra, tổng392,25 điểm gross. E = EMA9 đóng cắt biên ORB2m, SL nửa box, TP cả box; entry trước09:00 Houston, một lệnh/ngày. Không đổi SL/TP. Ngày cuối có entry được giữ là29/10.

## Kịch bản chính: funded → live sau payout5 → mất live chờ14 ngày

| MNQ | Acc funded sử dụng (gồm đầu tiên) | Acc live | Mất funded | Mất live | Lệnh thực hiện | Bỏ do chờ | Số payout | Tiền yêu cầu gross | Tiền nhận sau90/10 | Số dư funded cuối |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 3 | 2 | 0 | 1 | 0 | 101 | 0 | 3 | $3.591,00 | **$3.231,90** | $48.489,50 |
| 6 | 3 | 1 | 1 | 1 | 94 | 7 | 5 | $8.952,00 | **$8.056,80** | $49.097,00 |

Số dư cuối không phải tiền rút được; vốn50K là danh nghĩa. Tiền nhận chưa trừ phí mua/evaluation. Funded thay mới phiên có tín hiệu kế tiếp sau breach; đây là giả định bỏ qua evaluation và xét duyệt, không phải quyền được cấp funded ngay. Live bắt đầu$0, MLL ban đầu-$2.000; mô hình cho chuyển phiên kế tiếp payout5 nhưng Lucid thực tế xét duyệt. Sau mất live chờ14 ngày lịch rồi giả định có funded mới; quy định thực là được quay lại mua evaluation, không phải được funded tự động.

3MNQ: funded đầu mất16/10 sau94 lệnh; funded mới bắt đầu20/10,7 lệnh, chưa có payout thêm.

6MNQ: payout5 ngày23/09; live giả định24/09, mất05/10 sau5 lệnh; chờ đến19/10, tín hiệu tiếp theo20/10; funded tiếp mất27/10; funded cuối bắt đầu28/10. Không có payout live. Rút được tiền trước chuỗi thua không có nghĩa hệ thống giữ được tài khoản về sau.

## Ngày và tiền thực nhận trong mô hình

| MNQ | Ngày | Nhận sau90/10 |
|---|---|---:|
| 3 | 02/07 | $946,35 |
| 3 | 17/07 | $1.610,55 |
| 3 | 06/08 | $675,00 |
| 6 | 23/06 | $1.687,50 |
| 6 | 07/07 | $1.800,00 |
| 6 | 24/07 | $1.800,00 |
| 6 | 13/08 | $1.800,00 |
| 6 | 23/09 | $969,30 |

## Giả định và quy tắc

Bắt đầu với funded50K, không có DLL/consistency trong mô hình. 5 ngày lời net>=150USD và cycle net dương; rút tối đa min(50% lợi nhuận còn trong tài khoản,2000USD), tối thiểu500USD; chia90/10. Áp cùng bộ luật lên toàn mẫu để so sánh, không xác nhận lịch sử mọi thay đổi điều khoản theo từng ngày.

Giữ2.000USD trên MLL đã khóa50.100 là lựa chọn chủ tài khoản: sau payout giữ ít nhất52.100. LucidFlex không bắt buộc buffer này. Phí giả định1USD round-trip/MNQ, trừ bảo thủ ngay entry. Theo dõi MLL bằng từng record low trong lệnh, không chỉ P&L đóng ngày. TP/market là trade-print proxy, chưa chứng minh fill thật.

Nguồn chính thức kiểm tra31/10/2026:
- https://support.lucidtrading.com/en/articles/12945796-lucidflex-payouts
- https://support.lucidtrading.com/en/articles/12945815-lucidflex-drawdown
- https://support.lucidtrading.com/en/articles/16558826-new-live-structure

Live policy giả định rút phần dư trên2.100USD mỗi cuối ngày,90/10; không bonus. Các thông số không được công bố rõ không được coi là cam kết của provider. Trong mẫu này không có live payout nên giả định minimum/cap không tác động tiền nhận.

## Kịch bản cũ thứ hai: thay bằng funded sau payout5, không lên live

Đây chỉ là giả định phản thực để so sánh, không khẳng định Lucid cho phép né chuyển live. 3MNQ dùng2funded, mất1; 6MNQ dùng5funded, mất3, thay1acc sau payout5. Tiền nhận vẫn là3.231,90 và8.056,80USD tương ứng; số dư cuối6MNQ là49.493USD. Không cộng tiền hai kịch bản với nhau.

## Stress một tick

Stress thay entry market bất lợi1tick, target đòi xuyên thêm1tick, stop/exit thêm1tick bất lợi. Tiền nhận3MNQ còn **$3.191,40**,6MNQ **$7.929,90**; số tài khoản và payout không đổi. Không phải mô hình đầy đủ spread/latency/queue.

## Kiểm chứng

101 entry/outcome/exit row/P&L proxy khớp ledger gốc.202 tick paths gồm stress; mỗi giai đoạn tài khoản so với oracle tiền Decimal độc lập.28 unit tests PASS. Đối chiếu tổng payout,90/10,buffer,ngày giao dịch duy nhất và bảo toàn tiền PASS. Không có lệnh gốc còn mở trong101 tín hiệu này. Không tải data mới, không giao dịch, không sửa bot hoặc DB.

Phí mua/evaluation và thời gian vượt evaluation chưa biết nên lợi nhuận ròng sau mua lại không xác định. Bộ lọc trước09:00 được tìm sau khi đã xem mẫu này; mô phỏng cash-out kế thừa rủi ro chọn lọc đó. Đây là tiền mô phỏng nhận, không phải đảm bảo Lucid sẽ duyệt hoặc trả.
