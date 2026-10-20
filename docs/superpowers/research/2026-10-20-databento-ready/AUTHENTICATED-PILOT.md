# Authenticated pilot — 2026-10-20

[A] Owner supplied a research credential; stored privately outside Git (0600).
No credential value is included in this report or download manifests.

| Check | Observed result |
|---|---|
| Historical API authentication | PASS |
| TBBO available range reported | 2010-06-06T00:00:00Z to 2026-10-20T12:47:57.720672Z |
| Study raw-symbol resolution | All five candidates resolved over the year; intervals saved |
| Pilot date | 2026-09-10, 07:50–15:10 Houston receive-time window |
| Pilot cost quote | USD 0 additional usage |
| Expected / decoded records | 854,300 / 854,300 |
| Download bytes | 20,832,085 compressed DBN |
| SHA256 | `89f1a7965ac3cf4cb3d37c3ed39dde314d8f1d7f30946ac5043e08fb6e0bf346` |
| Instrument counts | 42004800 (MNQU6): 844,100; 42005282 (MNQZ6): 10,200 |
| Event timestamp regressions | 0 |
| Quote issues under validator | 0 |
| Quality flags | All 854,300 records have flags=0 |
| Pre-open traded volume, 08:00–08:29 | MNQU6=80,355; MNQZ6=965 |
| Selected instrument for pilot study | MNQU6, using pre-open volume only |
| Reconstructed RTH minutes | 390 |
| Existing NT8 OHLC exact matches | 345 of 390 |
| Existing NT8 OHLC differences | 45 of 390; 2 involve high/low, max component difference 2.25 points |
| Whole-year single cost query | Server HTTP504 timeout; no data downloaded by that metadata query |
| Smaller monthly estimates | All 13 partial/full calendar months returned USD0 |
| Full-year download | Started; not yet complete at this report |

The initial offline tests did not establish account access. This pilot now proves
authentication, a real zero-quoted download, decoding and raw-file preservation.
It does not establish real execution fills, complete year coverage or a profitable
strategy. Original NT8 results remain intact. Candle differences are recorded,
not forced to match. Boundary timestamp differences are a possible explanation
[B], not a proven cause for all 45 rows.

Source evidence outside Git:

- `/home/hoang/fleet-1017/out/databento-mnq-auth-20261020/`
  (availability, symbology-year, pilot-quote, month-quotes,
  pilot-event-bars, pilot-nt8-comparison)
- `/home/hoang/fleet-1017/out/databento-mnq-20261020-pilot-data/2026-09-10/`
  (started, original ticks.dbn.zst, validation, complete receipt)
- `/home/hoang/fleet-1017/out/databento-mnq-full-download-20261020.log`

Operational correction: preflight resolves the five-contract study universe over
the full year even when the download plan is a single-day pilot. Expired symbols
need not resolve on that one day. The actual pilot stream warned that MNQZ5 and
MNQM6 did not resolve on September 10; remaining symbols supplied the expected
records. The full-year mapping and daily instrument counts must be used together.
Regression test added; `verification-after-auth.json` records 16 tests passing in
both Houston and UTC plus 3/3 deliberate mutations caught by assertions.

The full-year query still enforces a zero extra-usage quote before billable calls.
Quoted cost is an estimate; these records are not a provider invoice.

## Full-run recovery

Run 2 validated 2025-10-20 and 2025-10-21: 977,344 records in 23,731,777 compressed bytes. The next request raised BentoServerError and left only started.json, with no tick file. Under the download lock that directory was moved to /home/hoang/trash/2026-10-20/databento-2025-10-22-server-error. No completed data was replaced. The new bounded zero-cost server-error retry path passes 20 tests under Chicago and UTC; all three mutation checks detect their introduced fault (verification-data-retry.json). Full-year completion and research coverage remain unverified until subsequent receipts.
