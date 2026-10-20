# Databento MNQ historical download readiness — 2026-10-20

**Update:** authenticated one-day download subsequently passed. See
[AUTHENTICATED-PILOT.md](AUTHENTICATED-PILOT.md); the initial preparation record
below is retained. Full-year completion and tick backtest results are separate.

Prepared for the owner's ORB 1m/2m/5m study, not the live bot. Source base at
accept: `be172c7a8d401d437f1b70454f39833fcd52a89f`. Branch:
`research/databento-mnq-ready-20261020`.

**Ready offline; account access and real downloads NOT YET VERIFIED.** The API
key is pending. No paid request, live service change, DB write, or NT8 change was
made during preparation. Existing broker and Databento credentials were not read.

## What the official documentation establishes

- [Historical API](https://databento.com/docs/api-reference-historical):
  `metadata.get_dataset_range` provides overall and per-schema availability;
  `get_cost` respects flat-rate discounts but remains an estimate; `get_record_count`
  and `get_billable_size` support download sizing. `timeseries.get_range` streams
  DBN. Start is inclusive, end exclusive. TBBO queries filter on receive time.
- [TBBO schema](https://databento.com/docs/schemas-and-data-formats/tbbo): each trade
  includes the best bid and ask immediately before its effect, exchange event and
  receive timestamps, instrument ID, sequence and quality flags. It is not a full
  queue/order-book history or proof that our order would have filled.
- [Pricing](https://databento.com/pricing): CME Standard currently advertises
  $199/month and the latest 12 months of L1 history. Earlier data can incur usage
  charges. The exact first date of this requested year must be quoted against the
  actual account, not assumed free from the product name.

SDK already installed and inspected locally: `databento==0.74.0`. Its get_range
path validation refuses existing files. No SDK upgrade was made. Dependency
versions used for tests are recorded in `verification.json`.

## Frozen request specification

| Item | Prepared value |
|---|---|
| Dataset/schema | `GLBX.MDP3` / `tbbo` |
| Study dates | 2025-10-20 inclusive through 2026-10-20 exclusive |
| Candidate dates | All 261 weekdays, including holidays for explicit coverage accounting |
| Raw outright symbols | `MNQZ5`, `MNQH6`, `MNQM6`, `MNQU6`, `MNQZ6` |
| Contract handling | Download candidates separately by instrument ID; no continuous-price adjustment or stitching |
| Daily receive window | 07:50–15:10 America/Chicago, converted to UTC with DST |
| Event window for later research | 08:00–15:00 Houston; ORB starts 08:30 |
| Padding | Ten minutes around event window for receive/event differences; not a guarantee against arbitrary delays |
| Storage | Daily original compressed DBN plus SHA256, request and validation receipts |
| Default extra-usage ceiling | $0 estimated; positive/unknown/increasing quotes stop the downloader |
| Retries | Completed files are hash-checked and skipped. An interrupted day stops for inspection, never silently redownloaded |

The five raw symbols are conventional candidates, **not yet verified against this
account**. Preflight requires each to resolve for some part of the year and saves
the complete interval mapping. Partial lifetime coverage is expected for expired
contracts. No `ALL_SYMBOLS`, parent spread collection or continuous symbol is used.

## Owner: save the key locally

In your WSL terminal run:

```bash
python3 /home/hoang/vl-1017-databento-ready/docs/superpowers/research/2026-10-20-databento-ready/pull_mnq.py save-key
```

Paste the API key at the hidden prompt and press Enter. It creates
`/home/hoang/.config/vl-research/databento.key` with mode 0600, outside Git. The
script refuses an existing file and never prints the key. No need to paste the
key into chat or add it to the production bot's `.env`. Then tell the CTO
**“key saved”**. If that path already exists, use `--key-file /another/private/path`
and give only its path to the CTO; preserve the existing credential.

## Operator sequence after the key is saved

The full-year offline plan is already generated at
`/home/hoang/fleet-1017/out/databento-mnq-20261020-plan.json`.
The one-day overlap pilot is also ready at
`/home/hoang/fleet-1017/out/databento-mnq-20261020-pilot-plan.json` (2026-09-10).
Use NEW output directories for preflight; existing files are never overwritten.

```bash
cd /home/hoang/vl-1017-databento-ready/docs/superpowers/research/2026-10-20-databento-ready
python3 -B pull_mnq.py preflight \
  --plan /home/hoang/fleet-1017/out/databento-mnq-20261020-plan.json \
  --out /home/hoang/fleet-1017/out/databento-mnq-20261020-quote
```

1. Inspect availability, symbology mappings, per-day counts, expected bytes and
   extra usage total. Confirm disk space from the returned size. An unavailable
   boundary must be reported, not silently clipped or rolled forward.
2. Generate a one-day pilot plan and run preflight/download on it at the default
   $0 extra-usage ceiling. Check actual decoding, symbols, exchange timestamps,
   flags and native NT8 overlap. Do not claim authenticated integration from the
   offline fixtures below.
3. Run the full-year download only after pilot validation. Example:

```bash
python3 -B pull_mnq.py download \
  --plan /home/hoang/fleet-1017/out/databento-mnq-20261020-plan.json \
  --out /home/hoang/fleet-1017/out/databento-mnq-20261020-data
```

The downloader quotes monthly windows covering ALL pending days before downloading
(including intervening nights/weekends, a conservative superset), then checks the
exact daily quote immediately before its request. Transient server errors on
cost metadata retry at most three times; data downloads never auto-retry.
`--max-cost-usd` is an explicit operator
parameter, not a provider-enforced billing cap. Leave it at zero unless the owner
authorizes a concrete quoted extra charge. No automatic paid retries.

An interrupted directory contains `started.json` and possibly a partial DBN.
Preserve and inspect it; never delete it. If a retry is needed, move that day
directory to a dated archive and account for any prior charges first. No such
recovery has been needed/tested on a real request yet.

## Validation and next backtest stage

The downloader checks actual DBN dataset/schema, receive bounds/order, positive
trade size and 0.25-point price increments, records event timestamp regressions,
quote issues, quality flags and per-instrument/per-minute trade counts. It retains
original records and timestamps; it does not silently sort or deduplicate them.
Incomplete decoded records fail validation. Complete files have SHA256 receipts.

**Download complete does not mean research ready.** Every receipt deliberately
sets `research_ready=false`. After real data arrives, verify contract mappings,
holidays/early closes and feed gaps, flags, trade corrections and exchange sequence
ordering; then reconstruct 1m/2m/5m ORB bars from event times. Minute absence can
mean no trades and is not automatically a data gap. Treat off-grid/undefined
quotes separately from valid trades. Compare the existing NT8 overlap before
replacing prior results.

The subsequent tick replay must retain the owner's rules: 08:30 Houston opening
range, immediate first break by one tick, one trade/day/test, SL20, TP1x ORB width.
Contract selection must use only pre-open information. Keep the previous OHLC
report intact and publish a separate tick-based result. This preparation does
not implement or certify that future tick execution engine. Slippage, latency
and limit-order queue fills remain modeled even with complete trade history.

## Verification

Run `python3 -B -m unittest -v test_pull_mnq.py` in this directory; also run with
`TZ=UTC`. Tests exercise the actual preflight/download/validation call sites using
a fake network client and **real compressed DBN records** constructed with the
installed official decoder package. They test costs, quote changes, interruption,
resume hash corruption, schema availability, private key handling, no-overwrite,
DST, invalid prices/order and truncated DBN. No external API call occurs.

See `verification.json` for fresh results and in-memory mutation tests; reproduce
with `python3 -B verify.py /a/new/verification.json`. A Python
SDK import emits an unclosed asyncio event-loop ResourceWarning on interpreter
shutdown in this environment; file readers in this downloader are explicitly
closed. No claim of a live integration PASS is made.

Audit reference: `docs/superpowers/AUDIT-CHECKLIST.md`, especially timestamp class7,
call-site evidence and no-overwrite rules. Latest checklist commit at accept:
`ba50edf220dbc7fc639a8f9e449470134ebf8e33`, 2026-10-19 23:18:30 -0500.
