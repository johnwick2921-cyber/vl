# MNQ ORB 1 / 2 / 5 phút — một năm, một lệnh mỗi ngày

Ngày chạy: 20/10/2026, giờ Houston (`America/Chicago`).

**[A] Đã chạy, kiểm thử và chạy lại từ dữ liệu đóng băng.** Khoảng yêu cầu:
20/10/2025 đến hết 19/10/2026. Có **226 phiên đủ dữ liệu**, mỗi khung 226
lệnh. Ba khung là ba bài test độc lập, không phải ba lệnh trong cùng một tài khoản.

## Kết quả chính

| ORB | Tổng lệnh | TP trước — chắc chắn | SL trước — chắc chắn | Chưa xác định TP/SL trước | Khoảng tỷ lệ TP trên tất cả lệnh |
|---|---:|---:|---:|---:|---:|
| 1 phút | 226 | 35 | 158 | 33 | **15,49%–30,09%** |
| 2 phút | 226 | 37 | 163 | 26 | **16,37%–27,88%** |
| 5 phút | 226 | 27 | 176 | 23 | **11,95%–22,12%** |

Không có lệnh phải đóng theo giờ trong mẫu này. Không có phiên đủ dữ liệu mà
không xuất hiện breakout. Khoảng % trên là **giới hạn do thiếu thứ tự tick**,
không phải khoảng tin cậy thống kê: cận dưới tính các lệnh chưa rõ là SL,
cận trên tính chúng là TP. Không lấy riêng các lệnh rõ kết quả để quảng bá
một tỷ lệ thắng duy nhất.

**Quay lại hit stop:** trong các lệnh chắc chắn SL, có ít nhất **47 / 46 / 65**
lệnh tương ứng ORB 1p / 2p / 5p đã đi thuận ít nhất một tick (0,25 điểm)
rồi quay lại SL. Đây là số tối thiểu xác định được. Số có thể thỏa điều kiện
này là tối đa **158 / 163 / 175**; phần còn lại phụ thuộc thứ tự trong nến.
Không được hiểu rằng mọi lệnh SL đều từng có lãi. Các mức đi thuận tối thiểu
và tối đa nằm trong CSV.

Ví dụ có thể truy lại: ORB 1p ngày **22/10/2025**, contract `MNQ 12-25`,
short **25.213,25**, TP cách **58 điểm**, SL **25.233,25**.
Giá đã đi thuận **40,5 điểm** nhưng chưa đủ TP, sau đó chạm SL: **−20 điểm**.
Ngược lại, một lệnh đã chạm TP trước được xem là đóng; giá quay lại SL sau
đó không biến lệnh ấy thành lệnh thua.

## Quy tắc đúng theo yêu cầu và giả định kỹ thuật

- ORB 1p: đỉnh/đáy các giao dịch trong 08:30–08:31 Houston.
- ORB 2p: 08:30–08:32; ORB 5p: 08:30–08:35.
- Chỉ sau khi range hoàn tất mới bắt đầu tìm entry. Không chờ nến xác nhận.
- Phá đỉnh trước: long; phá đáy trước: short. Triển khai “phá” là vượt biên
  **một tick = 0,25 điểm**. Đây là giả định tick tối thiểu được công khai,
  không phải tham số đã tối ưu.
- Tối đa **một lệnh mỗi ngày cho mỗi bài test**, không re-entry, không đảo
  chiều sau SL, không partial, không trailing, không dời stop.
- SL **20 điểm từ giá fill entry**; TP **1× (đỉnh ORB − đáy ORB)** từ entry.
- Tìm entry đến 15:00 Houston và đóng vị thế còn mở bằng close nến
  14:59–15:00. Đây là giả định kết thúc NY cash session; chủ tài khoản
  không chỉ định riêng cutoff. Không có time exit trong kết quả nên quy tắc
  đóng còn mở này không thay đổi kết quả hiện tại.
- Đơn vị P&L là điểm cho một hợp đồng MNQ. Không áp dụng bộ lọc Mentor,
  day-off, stop ceiling, AI planner, EMA, confluence hoặc risk gate của bot.
  Đây là test ORB độc lập, không phải replay chiến lược production.

## Fill và thứ tự intrabar

Tất cả ba ORB được dựng từ cùng một bộ nến **1 phút**. Không dùng nến 5m
để đoán thứ tự entry/SL/TP bên trong 5 phút.

Lệnh stop entry gap qua mức kích hoạt được fill tại open quan sát được của
nến, không fill ngược lại tại giá tốt hơn. Stop loss gap qua mức stop cũng
fill tại open, nên một số lệnh có thể lỗ hơn 20 điểm. Target limit gap qua
được ghi ở giá target, không giả định price improvement.

Nến entry có low/high vượt SL có thể đã chạm mức đó trước entry. Harness giữ
cả khả năng “SL trong nến entry” và “còn sống sau nến entry”. Nếu nhánh còn
sống sau đó cũng SL, kết quả được xác định là SL; nếu sau đó TP, kết quả
vẫn chưa xác định. Nếu cùng một nến phá cả hai biên ORB, harness xét cả hai
chiều đầu tiên có thể xảy ra. Một số ngày xác định được cùng kết quả SL dù
không xác định được chiều đầu tiên.

Nến có thể chứa cả TP và SL không được mặc định là TP. Chưa có tick/bid/ask,
queue hoặc mô hình xác suất intrabar; kết quả có điều kiện theo mô hình
OHLC stop/target này, không phải bằng chứng fill thực ngoài thị trường.
Không có lookahead trong dựng ORB hoặc chọn contract.

## Lời/lỗ theo hai giới hạn intrabar

Chi phí **2 điểm khứ hồi/lệnh** là kịch bản so sánh dùng trong các vòng
nghiên cứu trước, **không phải phí/slippage broker đã đo**. Bảng “sau chi phí”
trừ 2 điểm đúng một lần cho mỗi lệnh. Giá gap đã được xử lý riêng.

| ORB | P&L gross thấp nhất | Gross cao nhất | Sau chi phí 2 điểm: thấp nhất | Cao nhất |
|---|---:|---:|---:|---:|
| 1p | −2.053,50 | +423,25 | −2.505,50 | −28,75 |
| 2p | −1.483,50 | +751,50 | −1.935,50 | +299,50 |
| 5p | −1.903,75 | +848,50 | −2.355,75 | +396,50 |

Độ rộng ORB trung bình: **56,32 / 70,96 / 96,77 điểm**. Median:
**53,75 / 65,875 / 92,625 điểm**. TP thay đổi theo ngày; SL vẫn 20 điểm.
Không thể chọn “khung tốt nhất” bằng cách chỉ so số TP hoặc chọn toàn bộ
nhánh intrabar có lợi. ORB 1p có cận trên sau chi phí vẫn âm trong mẫu này;
2p/5p có khoảng P&L đi qua zero, chưa chứng minh có lợi nhuận.

## Dữ liệu và giới hạn bao phủ

- [A] SQLite chỉ đọc: `/home/hoang/vl/data/data.db`, bảng `bars`,
  `symbol='MNQ' AND tf='1m'`, cửa sổ epoch tương ứng ngày Houston.
- [A] Snapshot chứa **329.692 hàng**: `historical_import` 270.247,
  `live` 55.188, `historical` 4.253, `mixed` 4. Bỏ nguồn `mixed`.
- [A] Có 261 ngày thứ Hai–thứ Sáu trong cửa sổ. **226 ngày được đưa vào**.
  **19 ngày không có hàng RTH phù hợp**; **16 ngày có phiên không đủ 390
  phút**, gồm ngày đóng sớm và ngày thiếu một phần dữ liệu.
- Các khoảng ngày thường không có dữ liệu quanh roll gồm **03–12/12/2025**
  (8 ngày thường) và **03–13/03/2026** (9 ngày thường). Không gọi toàn bộ 19
  ngày thiếu là ngày nghỉ lễ. Các ngày và số nến được ghi trong `sessions.csv`.
- Gate đủ 390 phút loại cả phiên đóng sớm và ngày thiếu 1–2 phút. Điều này
  làm mẫu thiên về phiên đầy đủ; kết quả **không đại diện cho mọi ngày trong
  cả năm**, cũng không chứng minh không có cơ hội ở ngày bị loại.
- Contract được chọn bằng volume quan sát 08:00–08:29 Houston, trước
  entry. Không nối nến hai contract trong một phiên. Không chọn contract
  bằng P&L hoặc volume của cả ngày.
- Chỉ giữ OHLC hợp lệ, theo tick; kiểm tra phút liên tiếp, không forward-fill
  khoảng trống. Các hàng khác giá sau chuẩn hóa cùng khóa làm phiên bị loại.
- Không thay đổi hoặc backfill DB thật; không gửi lệnh, không deploy/restart.

## Chuẩn hóa timestamp — phát hiện quan trọng

Không tin tên cột `open_time_ms` là bằng chứng bar thật sự open-stamped.
Nguồn `historical_import` đi qua chuỗi sau:

1. `ninjascript/VLHistoryPull.cs:205`: lấy `Bars.GetTime(i)` của NT8 (close
   stamp) và chỉ đổi sang UTC.
2. `provider/ninjatrader/tcp_server.go:2355`: nhánh
   `FrameBarsHistoryData` chuyển nguyên payload tới importer.
3. `trader/historical_import.go:166`: `OpenTimeMs: b.T`.
4. `store/bar_history.go:392`: `ImportBars` ghi giá trị nguyên trạng.

Vì vậy harness **trừ 60.000 ms chỉ cho `historical_import` 1m**. Nguồn
`live`/`historical` đi qua chuyển đổi open-stamp của bridge, nên không trừ lần
nữa. Xem `provider/ninjatrader/bar_cache.go:233` và
`provider/ninjatrader/research_wire.go:59`.
DS-106 đã độc lập xác nhận source chain. DB và code import production không
bị sửa trong bài test này. Các nguồn lịch sử/hiện tại có thể khác provenance;
đây không phải xác minh lại tick feed gốc cho từng năm.

## Verification / reproducibility

Base at acceptance: `be172c7a8d401d437f1b70454f39833fcd52a89f`.
Runtime release source: `0a008962a2a6dd885f2e071fc793870ae176d60a`; base chỉ
thêm release marker. Branch: `research/orb-1-2-5-20261020`.
Audit reference: `docs/superpowers/AUDIT-CHECKLIST.md`, class 7 và R1–R4/R9/R10.

Source freshness (`git log -1`):

- `trader/historical_import.go`: `f8813d80fb8aa3636ef600d47a3089bfea41be12`
  — module rename nofx→vl.
- `ninjascript/VLHistoryPull.cs`: `28ac9192154e20acceda1b3404fa585d4f138489`
  — TimeZoneInfo compilation fix.
- `provider/ninjatrader/research_wire.go`:
  `f8813d80fb8aa3636ef600d47a3089bfea41be12` — module rename.

[A] **12 tests PASS**, including mirrored long/short, gap fills, first-direction
ambiguity, stop-now/survive branch collapse, same-bar TP/SL, no re-entry,
2m ORB boundary and time exit. Tests exercise the actual offline simulation
function; they do not claim to exercise the production trader.

[A] DB run2 and replay from the frozen raw CSV produce **byte-identical**
`summary.json`, `trades.csv`, and `sessions.csv`.

- Canonical input row SHA256:
  `34a84b59ff2c0de05a93968fa43218e0db1f66af0b846fbab3725edbd48570a5`
- `summary.json`: 3.034 bytes;
  `11143d0b4184e8f43a99bb54d9b35ac7e6f1c919e8caef6bdab385c3c97cc5f5`
- `trades.csv`: 93.445 bytes;
  `0b3291bd2343993cca8336e9cdea2dbebae701dbc68180bbae617d9166ee049c`
- `sessions.csv`: 10.153 bytes;
  `b97b266931510fe6042c971f00253a1bd237daf43890b293b13bce65e969ab83`
- Raw snapshot:
  `/home/hoang/fleet-1017/out/orb-1year-20261020-run2/input-1m.csv.gz`
  (4.660.313 bytes). Kept locally; not published as redistributable exchange
  data. Run1 remains preserved as a superseded preliminary analysis.

[A] DS-106 final independent review (message
`1792517232503-2174984-000001`, 12:27:12 Houston) found no new logic defect
and confirmed final counts. Its coverage concerns are retained above;
“16 incomplete sessions” must not be mislabeled as 16 confirmed half-days.

Run from this folder's parent repository worktree:

```bash
python3 -B -m unittest discover \
  -s docs/superpowers/research/2026-10-20-orb-first-break -p test_backtest.py -v

python3 -B docs/superpowers/research/2026-10-20-orb-first-break/backtest.py \
  --input-csv /home/hoang/fleet-1017/out/orb-1year-20261020-run2/input-1m.csv.gz \
  --out /tmp/orb-owner-replay-UNIQUE \
  --start 2025-10-20 --end 2026-10-20
```

Output folder must **not already exist**: exclusive creation deliberately
protects earlier evidence. `--db` can replace `--input-csv` for a fresh read-only
query. A later DB query may change if the source database gains historical bars.
CSV `entry_ms`/`exit_ms` identify the **minute containing** the event, not an
invented exact fill time. Blank values mean unknown/not applicable, not zero.
