# post-install-check fixtures

Every line in these logs is a REAL line from the live bot's log, copied
byte-for-byte and kept in its original order. Nothing was written by hand. Each
fixture is a selection of lines, not a whole log, so the source line numbers are
listed here. The file names keep the `vl_YYYY-MM-DD.log` shape because the script
reads the year from the name.

| Fixture | Source (`~/vl/data/`) | Source line numbers |
|---|---|---|
| `healthy/vl_2026-11-06.log` | `vl_2026-11-06.log` (release #62 boot, rev 5e50da0cea8a) | 1, 2, 3, 269, 405, 411, 3676, 3679, 3690, 3691, 3788 |
| `outage/vl_2026-11-04.log` | `vl_2026-11-04.log` (boot rev ab356a3a46e7) | 1, 2, 3, 244, 354, 360, 368, 372, 573, 586, 1761, 1815, 1817, 1821, 1822, 1910 |
| `push-down/vl_2026-11-01.log` | `vl_2026-11-01.log` (boot rev ab356a3a46e7) | 1, 2, 3, 199, 73552, 102060, 102093, 102106, 102198 |
| `closed-boot/vl_2026-11-01.log` | `vl_2026-11-01.log` (boot rev ab356a3a46e7, Sunday 15:55 CT, CME closed) | 1, 2, 3, 83, 191, 199, 434, 435, 436, 596, 5017-5023 |

What each one is for:

- **healthy**: the 11-06 boot after the release #62 install. Boot OK, goldens
  PASS, no `[ERRO]`, AI calls OK, pushes sent. The script must say PASS.
- **outage**: the 11-04 boot. No single moment of that day had a feed outage,
  an open pause and a 402 at once, so this is a selection. Trader `hoang`'s
  recovery lines (1746 "bars are flowing again", 1889 "TRADING RESUMED") were
  left out. Its pause (368) and FEED DOWN (573) therefore stay open. Trader
  `12345` keeps both its recovery lines (1761, 1910), so its pause and feed
  outage are closed. Its newest AI cycle is the 402 at 1817, and line 1815 is
  the `ai_call ... http_status=402` before it. The script must FAIL `hoang` on
  pause and feed, FAIL the 402, and must NOT fail `12345` on pause or feed.
- **push-down**: the 11-03 02:00 CT Telegram outage inside the 11-01 process.
  Line 73552 is a SENT push. After it come four `owner push: SEND FAILED` lines,
  with no SENT after them. The script must FAIL push.
- **closed-boot**: the 11-01 install, booted on a Sunday at 15:55 CT while the
  CME was closed. Lines 83 and 191 are two `mentor seed REFUSING ALL entries`
  `[ERRO]` lines written BEFORE the boot line (199): the traders start first,
  so the judged window starts at the banner (3). Lines 434-436 pause every
  trader on `data_integrity_unknown` a minute after boot; they resume at
  5017, 5018 and 5021 (17:26 CT), after the 17:00 reopen. Line 596 is the
  first AI call (16:31 CT). Cut before 17:00, the script must say TOO EARLY
  (not FAIL, not PASS); whole, it must PASS; without the RESUMED lines and
  with the log still written past 17:45, it must FAIL.

The script judges liveness and "too early" against the clock, so every test
pins "now" with `POST_INSTALL_NOW` (60 s after the fixture's newest line unless
the case says otherwise). Some tests also drop real lines from a fixture, or
re-stamp a real line from another fixture or log to place it inside the
process; each such line says so where the test builds it.
