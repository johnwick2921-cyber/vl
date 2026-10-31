"""Report only after independent verification; preserve all earlier studies."""
from pathlib import Path
from collections import Counter
import json,csv,hashlib
ROOT=Path('/home/hoang/fleet-1017/out/day32-recent-six-months-20261031')
OUT=ROOT/'run-recent-v1'
def main():
    a=json.loads((OUT/'independent-audit.json').read_text());assert not a['mismatches']
    rows=json.loads((OUT/'ledger.json').read_text());census=json.loads((OUT/'census.json').read_text())
    summary=json.loads((OUT/'summary.json').read_text())
    weekly=json.loads(Path('/home/hoang/fleet-1017/out/day32-week-20261026/run-week-v2/ledger.json').read_text())
    # File row offsets may change with additional requested contracts; compare
    # economic records and timestamps, not record indices from different files.
    fields=['outcome','side','entry_event_ns','exit_event_ns','entry_ticks','pnl_points']
    for w in weekly:
        r=next(r for r in rows if r['day']==w['day'] and r['stop_points']==w['stop_points'])
        assert all(r.get(k)==w.get(k) for k in fields),(w['day'],'weekly mismatch')
    lines=['# EMA9 / EMA34 2m — 1 May to 30 October 2026','',
      'Same engine as the separately verified 5/5 winning week. One first FILLED trade per overnight session, TP15, SL15 and SL16 separately. Before first fill, subsequent crosses may switch direction. EMA9 retest limit updates at event boundaries; touch-fill assumption. No trailing.', '',
      'Session date labels the morning: 17:00 Houston on the prior date to before 08:30 Houston on the listed date. Both EMAs use 1,000 completed 2m bars of the same raw contract before each session; no EMA reset. Historical OHLCV and intraday bar grouping use a consistent receive-time clock. Intrabar crossover is evaluated after the final record of an exchange event, not only at candle close.', '',
      '| SL | TP | Entries | TP hits | SL hits | Open | Closed win % | Won points | Lost points | Gross points |',
      '|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|']
    for s in summary:
        assert s['entries']==s['TP']+s['SL']+s['open']
        assert s['net_points']==sum(r['pnl_points'] or 0 for r in rows if r['stop_points']==s['stop'])
        rate='n/a' if s['win_percent_closed'] is None else f"{s['win_percent_closed']:.2f}"
        lines.append(f"| {s['stop']} | 15 | {s['entries']} | {s['TP']} | {s['SL']} | {s['open']} | {rate} | {s['won_points']:+.2f} | {s['lost_points']:.2f} | {s['net_points']:+.2f} |")
    counts=Counter(c['reason'].split(':')[0] or 'INCLUDED' for c in census)
    lines+=['','## Coverage','',f"Planned session dates: {len(census)}. Coverage counts: `{dict(counts)}`.",
      'Excluded sessions are not counted as no-signal days. Open positions are excluded from closed P&L; their counts remain visible. No missing trades are invented.', '',
      '## Monthly results','', '| Month | SL | Trades | TP | SL hits | Open | Gross points |','|---|---:|---:|---:|---:|---:|---:|']
    monthly=[]
    for month in sorted({r['day'][:7] for r in rows}):
        for stop in (15,16):
            rr=[r for r in rows if r['day'].startswith(month) and r['stop_points']==stop]
            c=Counter(r['outcome'] for r in rr);n=c['TP']+c['SL']+c['OPEN_AT_CUTOFF'];p=sum(r['pnl_points'] or 0 for r in rr)
            monthly.append(dict(month=month,stop=stop,entries=n,TP=c['TP'],SL=c['SL'],open=c['OPEN_AT_CUTOFF'],points=p))
            lines.append(f"| {month} | {stop} | {n} | {c['TP']} | {c['SL']} | {c['OPEN_AT_CUTOFF']} | {p:+.2f} |")
    lines+=['','## Verification and limitations','',
      f"[A] Independent weighted-seed EMA/execution replay: {a['independent_replay_rows']} rows; {a['raw_entry_exit_checks']} raw entry/exit checks; zero mismatches; count/P&L arithmetic PASS. All ten economic results of the separately checked final week agree.",
      '[A] Raw data SHA256 and historical warmup SHA256 checked before replay. Contract selection uses only pre-session information. Provider quality gates remain in force. Engine and inputs are recorded in provenance.json.',
      'Gross results are before commissions and real queue/latency costs. Stops use observed adverse trade prints, so realized loss can exceed 15/16 points. Touch limit fills are an assumption; exact parity with the closed-source TradingView indicator is not proven. These checks verify the declared model, not live profitability. No live service, trading account or production DB was changed.']
    with (OUT/'REPORT.md').open('x') as f:f.write('\n'.join(lines)+'\n')
    with (OUT/'monthly.csv').open('x',newline='') as f:
        w=csv.DictWriter(f,fieldnames=list(monthly[0]));w.writeheader();w.writerows(monthly)
    dest=Path(__file__).parent/'results-recent-six-months';dest.mkdir(exist_ok=False)
    for name in ['REPORT.md','summary.json','monthly.csv','sessions.csv','trades.csv','independent-audit.json','provenance.json']:
        with (dest/name).open('xb') as f:f.write((OUT/name).read_bytes())
    print((OUT/'REPORT.md').read_text())
if __name__=='__main__':main()
