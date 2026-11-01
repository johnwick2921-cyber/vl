# Fixed $500 payout — E trước09:00,01/05–30/10/2026

Mỗi lần yêu cầu500USD gross, nhận450USD sau90/10. Không phải nhận500USD ròng. Cùng101 tín hiệu E, SL½box TP cảbox, một lệnh/ngày, phí giả định1USD/MNQ round-trip. Các điều kiện5ngày>=150USD,cycle dương,50%profit vẫn giữ.

| MNQ | Buffer tự chọn sau rút | Payout funded | Nhận | Funded đã dùng | Live | Mất funded/live | Payout đầu |
|---|---:|---:|---:|---:|---:|---|---|
| 3 | Không thêm | 6 | $2.700 | 2 | 1 | 1/1 | 29/05 |
| 3 | $2.000 trên MLL | 4 | $1.800 | 1 | 0 | 1/0 | 02/07 |
| 6 | Không thêm | 5 | $2.250 | 2 | 0 | 1/0 | 27/05 |
| 6 | $2.000 trên MLL | 5 | $2.250 | 3 | 1 | 1/1 | 23/06 |

3MNQ khôngbuffer:6payout là1 từ acc đầu+5 từ acc thứhai, không phải6 trên một acc. 6MNQ khôngbuffer:5payout là1+4 trên hai acc, chưa chạm5 trên cùngacc nên không cólive. Khôngbuffer không nghĩa là bỏ MLL: saupayout vẫn phải trên50.100, 50%profit tối thiểu500 buộc profit ít nhất1000.

Giữbuffer2.000 nghĩa số dư sau rút>=52.100. Đây là yêu cầu chủ tài khoản, không phải LucidFlex bắtbuộc. Mức rút cố định500 giới hạn mỗiacc đến5payout là2.500gross/2.250 nhận trước giả định chuyểnlive. Phần dư SIM còn lại không tự ghi thànhcashout.

Ngày payout3MNQ khôngbuffer:29/05,23/06,07/07,24/07,13/08,22/09. 3MNQ giữbuffer:02/07,17/07,04/08,23/09.

Ngày payout6MNQ khôngbuffer:27/05,25/06,09/07,31/07,21/08. 6MNQ giữbuffer:23/06,07/07,24/07,13/08,22/09.

Đã tính mua lại funded sau breach phiên cótínhiệu kếtiếp; saupayout5 giả định live phiên kếtiếp; mấtlive chờ14ngàylịch rồi fundedmới. Đây bỏqua evaluation và xétduyệt, không phải quyền chắc chắn củaLucid. Live giữpolicy cũ rút dư trên2100mỗiEOD; trong cả4trườnghợp live nhận0 nên fixed500chỉSIM không ảnhhưởng tiềnlive.

Stress1tick cho cùngsốpayout vàtiềnnhận; sốdưcuối cóthayđổi. Kết quảchưa trừ phí mua/evaluation, không phải lãi ròng. Không tạoaccmới khi hếtmẫu. 3MNQ giữbuffer mất ởlệnh cuối29/10; khôngtạoaccmới rỗng.

Kiểmtra:2000 chuỗingẫu nhiên sooracle Decimal;8assert biên4/5/9/25ngày; từnggiaiđoạn thật sooracle; bảo toàn tiền/ngày/buffer/fixed500/450 đềuPASS. Hash202tickpaths nguồnkhớp. Không giao dịch, không sửa bot. Bộlọc trước09:00 được chọn saukhi xem mẫu nên môphỏng kếthừa selectionbias.

Quytắc chínhthức đãkiểmtra31/10/2026: https://support.lucidtrading.com/en/articles/12945796-lucidflex-payouts ; https://support.lucidtrading.com/en/articles/12945815-lucidflex-drawdown ; https://support.lucidtrading.com/en/articles/16558826-new-live-structure . Xétduyệt live và mua evaluation saucooldown không bằng giảđịnh funded ngay trong môhình.
