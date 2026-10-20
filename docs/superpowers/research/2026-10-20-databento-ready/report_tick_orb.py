"""Render audited results; does not claim exchange fills or a complete trade tape."""
import csv
import json
from pathlib import Path
import sys
import zipfile

import tick_orb as e


def render(root):
    summary=json.loads((root/'summary.json').read_text())
    audit=json.loads((root/'audit.json').read_text())
    e.p.require(audit['status']=='PASS_COMPUTATIONAL_CHECKS_WITH_CAVEATS','Audit missing')
    for name,digest in audit['artifacts'].items():
        e.p.require(e.p.digest(root/name)==digest,'Artifact changed since audit')
    with (root/'trades.csv').open() as f: rows=list(csv.DictReader(f))
    lines=[f"# MNQ ORB tick-path backtest: {summary['start']} to {summary['end_exclusive']} exclusive",
           '', '**COMPUTATION VERIFIED WITH CAVEATS — pending final CTO review.**', '',
           'This is a historical trade-path simulation, not a guarantee of real broker fills.', '',
           f"Candidate weekdays: {summary['candidate_weekdays']}; sessions passing the whole-day gate: "
           f"{summary['included_sessions']}; excluded: {summary['excluded_sessions']}. "
           'Every exclusion and file SHA256 is in sessions.csv. Missing minutes include early-close '
           'sessions and possible data gaps; they are not all labeled holidays. Results describe '
           'the included sample, not every possible trading day.', '',
           '## Rules', '',
           '- 08:30 Houston (America/Chicago, DST-aware). Separate 1/2/5-minute opening ranges.',
           '- First observed trade at least one tick outside the finished range enters immediately. '
           'Entry uses that observed trade price, including a gap; one trade per day per variant.',
           '- Stop 20 points from entry. Target one full opening-range width from entry.',
           '- Stop crossing exits at the observed crossing trade (loss can exceed 20 points). '
           'Target-touch model exits at target. Trade-through sensitivity requires another 0.25 point.',
           '- No forced fill at 15:00: still-open positions are reported OPEN_AT_CUTOFF. '
           'Their mark is separate and not included in realized P&L.',
           '- Contract chosen by 08:00–08:29:59.999999999 pre-open volume received before 08:30. '
           'No future volume, P&L selection, adjusted continuous prices or cross-contract stitching.',
           '- Provider file order is preserved. Conservative timestamp/sequence-tie treatment '
           'is shown separately; it is a stress assumption, not proof the provider order is missing.', '',
           '## Primary results: provider record order', '',
           '| ORB | Target assumption | Entries | TP | SL | Open | Unresolved | TP / all entries | Comeback SL | Closed-only gross pts | Closed-only net, 2pt scenario |',
           '|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|']
    for s in summary['summaries']:
        if s['ordering']!='provider_record_order': continue
        c=s['counts'];rate=s['tp_pct_of_entries']
        rate='n/a' if rate is None else f'{rate:.2f}%'
        lines.append(f"| {s['orb_minutes']}m | {s['target_rule']} | {s['entry_or_unresolved']} | "
                     f"{c.get('TP',0)} | {c.get('SL',0)} | {c.get('OPEN_AT_CUTOFF',0)} | "
                     f"{c.get('UNRESOLVED',0)} | {rate} | {s['comeback_stop']} | "
                     f"{s['closed_only_gross_points']:.2f} | {s['closed_only_net_2pt_scenario']:.2f} |")
    lines+=['', 'TP percentage uses all entries, including open/unresolved outcomes. '
            'NO_ENTRY and EXCLUDED are separate in summary.json. Comeback SL means price first '
            'moved strictly in favor of the entry, then hit the stop before its target under that '
            'variant; it does not mean a completed TP trade was reopened.', '',
            '## Conservative tie sensitivity', '',
            '| ORB | Target assumption | TP | SL | Open | Unresolved |',
            '|---|---|---:|---:|---:|---:|']
    for s in summary['summaries']:
        if s['ordering']!='conservative_ties': continue
        c=s['counts']
        lines.append(f"| {s['orb_minutes']}m | {s['target_rule']} | {c.get('TP',0)} | "
                     f"{c.get('SL',0)} | {c.get('OPEN_AT_CUTOFF',0)} | {c.get('UNRESOLVED',0)} |")
    lines+=['', '## Verification and limits', '',
            f"- {audit['independent_first_passage_rows']} primary variant rows checked against a "
            'separate first-passage implementation; summary counts and denominators reconciled to CSV.',
            '- SHA256 checked for each input; real compressed DBN decoding, symbol mappings, '
            'row count, flags, minute coverage, event/receive/sequence regressions reviewed by code.',
            '- A minute with trades is not proof that every trade within that minute was captured. '
            'This verifies the downloaded files as delivered, not an independent exchange tape.',
            '- A trade print is not an executable bid/ask quote or proof of queue fill. '
            'Spread, latency and actual slippage are not established by the primary model. '
            'Two points per round trip is a separate cost scenario, not measured broker cost.',
            '- The independent algorithm shares the decoder/coverage routine. It is not an '
            'independent data vendor audit or verification of the live trading bot.',
            f"- Engine SHA256: `{audit['engine_sha256']}`.",
            f"- Auditor SHA256: `{audit['auditor_sha256']}`.",
            '- Prior OHLC results remain intact and must not be substituted for tick evidence.', '',
            'Official schema/order reference: https://databento.com/docs/venues-and-datasets/glbx-mdp3',
            'Official TBBO reference: https://databento.com/docs/schemas-and-data-formats/tbbo', '',
            '## Files', '',
            '- trades.csv: every variant and trade, raw zero-based file row IDs and nanosecond timestamps.',
            '- sessions.csv: every candidate weekday, chosen contract, file hash and exclusion reason.',
            '- summary.json: all denominators, outcome counts and assumptions.',
            '- audit.json: computational cross-check and artifact hashes.']
    with (root/'REPORT.md').open('x') as f: f.write('\n'.join(lines)+'\n')
    # Standard-library exporter retains >15 digit values as text, including ns.
    from xlsx_tick_tables import export
    export(root, root/'TABLES.xlsx')
    names=['REPORT.md','TABLES.xlsx','trades.csv','sessions.csv','summary.json','audit.json']
    if (root/'engine-verification.json').exists(): names.append('engine-verification.json')
    with zipfile.ZipFile(root/'REPORT-BUNDLE.zip','x',compression=zipfile.ZIP_DEFLATED) as z:
        for name in names:z.write(root/name,name)


if __name__=='__main__':render(Path(sys.argv[1]))
