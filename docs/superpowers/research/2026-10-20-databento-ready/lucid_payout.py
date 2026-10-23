"""Offline payout overlay on hash-verified E/D/A tick paths. All money is cents.
Owner: keep $2k above locked MLL. Max eligible payout each EOD, immediate approval
before next session assumed. Stop at breach or fifth payout, never reset account.
"""
import csv,json,hashlib
from pathlib import Path

def allowance(balance):
    return max(0,min((balance-5000000)//2,200000,balance-5210000))

def simulate(paths,qty):
    balance=5000000;floor=4800000;peak=balance;cycle=0;days=0;paid=0;netpaid=0;ledger=[];payouts=[];status='END_OF_VALID_SAMPLE'
    for p in paths:
        if not p['traded']:continue
        start=balance;cash=balance-qty*100;hit=None
        for row,ts,g in p['record_lows']:
            if cash+g*qty*50<=floor:hit=(row,ts,g);break
        if cash<=floor:hit=(p['entry_row'],p['entry_event_ns'],0)
        gain=hit[2] if hit else p['gain_ticks'];before=cash+gain*qty*50;pnl=before-start
        cycle+=pnl;days+=pnl>=15000;balance=before;gross=received=0;priorfloor=floor;qualified=days;cycle_before=cycle
        if hit:status='MLL_BREACH'
        else:
            peak=max(peak,balance);floor=min(5010000,max(floor,peak-200000))
            available=allowance(balance)
            if days>=5 and cycle>0 and available>=50000:
                gross=available;received=gross*9//10;balance-=gross;floor=5010000
                paid+=gross;netpaid+=received
                payouts.append(dict(day=p['day'],number=len(payouts)+1,before_cents=before,gross_cents=gross,received_cents=received,after_cents=balance,buffer_cents=balance-floor,qualifying_days=days,cycle_net_cents=cycle))
                days=0;cycle=0
                if len(payouts)==5:status='LIVE_TRANSITION_AFTER_5_PAYOUTS'
        ledger.append(dict(day=p['day'],start_cents=start,pnl_cents=pnl,fee_cents=qty*100,before_payout_cents=before,payout_cents=gross,received_cents=received,end_cents=balance,floor_before_cents=priorfloor,floor_after_cents=floor,qualifying_days_before_payout=qualified,cycle_net_before_payout_cents=cycle_before,outcome='MLL_BREACH' if hit else p['outcome'],exit_row=hit[0] if hit else p['exit_row']))
        if status!='END_OF_VALID_SAMPLE':break
    assert balance==5000000+sum(x['pnl_cents'] for x in ledger)-paid
    return dict(qty=qty,status=status,last_day=ledger[-1]['day'] if ledger else '',trades=len(ledger),payout_count=len(payouts),gross_withdrawn_cents=paid,received_cents=netpaid,ending_balance_cents=balance,final_floor_cents=floor,trading_net_cents=sum(x['pnl_cents'] for x in ledger),ledger=ledger,payouts=payouts)

def main():
    src=Path('/home/hoang/fleet-1017/out/lucid50k-EDA-3v6-20261022');out=Path('/home/hoang/fleet-1017/out/lucid50k-EDA-payout-20261023');out.mkdir(exist_ok=False)
    audit=json.loads((src/'audit.json').read_text());raw=(src/'paths.json').read_bytes();assert hashlib.sha256(raw).hexdigest()==audit['artifacts']['paths.json']
    paths=json.loads(raw);results=[];ledger=[];payouts=[]
    for method in ['E','D','A']:
        for execution in ['trade_print_proxy','stress1tick']:
            ps=sorted([p for p in paths if p['method']==method and p['execution']==execution],key=lambda p:p['day']);assert len({p['day'] for p in ps})==len(ps)
            for qty in [3,6]:
                r=simulate(ps,qty);key=dict(method=method,execution=execution,qty=qty)
                ledger.extend(dict(**key,**l) for l in r.pop('ledger'));payouts.extend(dict(**key,**l) for l in r.pop('payouts'));results.append(dict(method=method,execution=execution,**r))
    for name,rows in [('summary.csv',results),('ledger.csv',ledger),('payouts.csv',payouts)]:
        with (out/name).open('x') as f:w=csv.DictWriter(f,fieldnames=rows[0]);w.writeheader();w.writerows(rows)
    with (out/'audit.json').open('x') as f:json.dump(dict(source_path=str(src/'paths.json'),source_sha256=hashlib.sha256(raw).hexdigest(),engine_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),artifacts={n:hashlib.sha256((out/n).read_bytes()).hexdigest() for n in ['summary.csv','ledger.csv','payouts.csv']}),f,indent=2)
    print(json.dumps(results,indent=2))
if __name__=='__main__':main()
