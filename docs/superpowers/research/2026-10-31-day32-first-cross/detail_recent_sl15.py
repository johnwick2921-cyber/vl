from pathlib import Path
from datetime import datetime
from zoneinfo import ZoneInfo
import json,csv,hashlib
ROOT=Path('/home/hoang/fleet-1017/out/day32-recent-six-months-20261031/run-recent-v1')
CT=ZoneInfo('America/Chicago')
def stamp(ns):return datetime.fromtimestamp(ns/1e9,CT).isoformat(timespec='seconds')
def main():
    audit=json.loads((ROOT/'independent-audit.json').read_text());assert not audit['mismatches']
    rows=sorted([r for r in json.loads((ROOT/'ledger.json').read_text()) if r['stop_points']==15],key=lambda r:r['day'])
    assert len(rows)==129 and all(r['outcome'] in ('TP','SL') for r in rows)
    wins=[r for r in rows if r['outcome']=='TP'];losses=[r for r in rows if r['outcome']=='SL']
    won=sum(r['pnl_points'] for r in wins);lost=sum(r['pnl_points'] for r in losses)
    assert (len(wins),len(losses),won,lost)==(66,63,990,-946.5)
    equity=peak=drawdown=0;peakday='START';dd_span=None;details=[]
    for r in rows:
        equity+=r['pnl_points']
        if equity>peak:peak=equity;peakday=r['day']
        if peak-equity>drawdown:drawdown=peak-equity;dd_span=[peakday,r['day']]
        entry=r['entry_ticks']/4;side=r['side']
        details.append(dict(session_day=r['day'],contract=r['contract'],signal_houston=stamp(r['signal_event_ns']),entry_houston=stamp(r['entry_event_ns']),exit_houston=stamp(r['exit_event_ns']),side='BUY' if side==1 else 'SELL',entry=entry,stop_level=entry-side*15,target_level=entry+side*15,modeled_exit=entry+side*r['pnl_points'],outcome=r['outcome'],points=r['pnl_points'],cumulative_points=equity,closed_trade_drawdown=peak-equity,signal_row=r['signal_row'],entry_row=r['entry_row'],exit_row=r['exit_row']))
    streaks={}
    for outcome in ('TP','SL'):
        runs=[];seq=[]
        for r in rows:
            if r['outcome']==outcome:seq.append(r)
            elif seq:runs.append(seq);seq=[]
        if seq:runs.append(seq)
        length=max(map(len,runs));streaks[outcome]=[dict(trades=len(s),start=s[0]['day'],end=s[-1]['day'],points=sum(r['pnl_points'] for r in s)) for s in runs if len(s)==length]
    metrics=dict(trades=len(rows),TP=len(wins),SL=len(losses),win_percent=100*len(wins)/len(rows),won_points=won,lost_points=lost,gross_points=equity,profit_factor=won/-lost,mean_points=equity/len(rows),worst_trade=min(r['pnl_points'] for r in rows),closed_trade_max_drawdown=drawdown,drawdown_span=dd_span,streaks=streaks)
    out=ROOT/'sl15-detail';out.mkdir(exist_ok=False)
    for filename,data in [('all-trades.csv',details),('losses-only.csv',[r for r in details if r['outcome']=='SL'])]:
        with (out/filename).open('x',newline='') as f:
            w=csv.DictWriter(f,fieldnames=list(details[0]));w.writeheader();w.writerows(data)
    with (out/'metrics.json').open('x') as f:json.dump(metrics,f,indent=2)
    lines=['# SL15 / TP15 — EMA9/EMA34 2m, May–October 2026','',
      'One first filled trade per night. Same historical warmup and touch-limit model as the verified parent report. Each session runs 17:00 Houston on the prior date to before 08:30 on the labeled date. Entry/exit timestamps below use Houston time with UTC offset.', '',
      f"129 trades: 66 TP, 63 SL; win rate {metrics['win_percent']:.2f}%. Won +990.00 points, lost -946.50; gross net +43.50 points. Profit factor {metrics['profit_factor']:.4f}; mean +{metrics['mean_points']:.4f} points/trade.", '',
      'Worst losing-trade streak: 7 consecutive observed trades, 25 June–3 July, -105 points. Longest winning-trade streak: 5, 26–30 October, +75 points. These dates label the morning session. Weekends are not trades; excluded sessions are not invented.', '',
      'Maximum CLOSED-TRADE equity drawdown: 151.50 points, peak after 24 June to trough after 21 August. This is not intratrade/mark-to-market drawdown. Worst trade: 20 July, -16.50 points, because the observed stop-triggering print passed through the 15-point stop. Other 62 losses are -15 points.', '',
      'Coverage: 129 of 131 planned sessions. 25–26 May excluded due provider-degraded warmup data. All results before commissions and realistic queue/latency costs. Touch limit fill assumption; exact live chart parity unproven.', '',
      '## Monthly totals','', '| Month | Trades | TP | SL | Won points | Lost points | Gross net |','|---|---:|---:|---:|---:|---:|---:|']
    for month in sorted({r['day'][:7] for r in rows}):
        rr=[r for r in rows if r['day'].startswith(month)];w=sum(max(0,r['pnl_points']) for r in rr);l=sum(min(0,r['pnl_points']) for r in rr)
        lines.append(f"| {month} | {len(rr)} | {sum(r['outcome']=='TP' for r in rr)} | {sum(r['outcome']=='SL' for r in rr)} | +{w:.2f} | {l:.2f} | {w+l:+.2f} |")
    lines+=['','## All 129 trades','', 'Exit is modeled target fill or adverse observed stop print; it is not a broker fill receipt. Raw source row IDs are in the CSV.', '', '| Session | Contract | Entry Houston | Side | Entry | Stop | Target | Exit Houston | Modeled exit | Outcome | Points |','|---|---|---|---|---:|---:|---:|---|---:|---|---:|']
    for r in details:
        lines.append('| '+' | '.join(str(r[k]) for k in ['session_day','contract','entry_houston','side','entry','stop_level','target_level','exit_houston','modeled_exit','outcome','points'])+' |')
    with (out/'REPORT.md').open('x') as f:f.write('\n'.join(lines)+'\n')
    dest=Path(__file__).parent/'results-recent-sl15-detail';dest.mkdir(exist_ok=False)
    for p in out.iterdir():
        with (dest/p.name).open('xb') as f:f.write(p.read_bytes())
    print(json.dumps(metrics,indent=2));print(out/'REPORT.md')
if __name__=='__main__':main()
