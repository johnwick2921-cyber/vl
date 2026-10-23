"""Offline ONLY: conditional Lucid standard-live continuation. Not broker routing.
Transition date is supplied, NEVER implied approval. No SIM equity carry-over.
Unverified payout constraints are explicitly scenario inputs, not provider rules.
"""
from dataclasses import dataclass
import csv,json,hashlib
from pathlib import Path

@dataclass(frozen=True)
class Policy:
    payout: bool=False
    minimum_cents: int|None=None
    maximum_cents: int|None=None
    split_bps: int|None=None
    fee_rt_cents: int=100  # sensitivity assumption, not verified live statement
    reserve_cents: int=200000
    lock_trigger_cents: int=200000
    def validate(self):
        if self.fee_rt_cents<0 or self.reserve_cents<0:raise ValueError('negative cost/reserve')
        if self.lock_trigger_cents not in (200000,210000):raise ValueError('explicit lock sensitivity required')
        if self.payout and (self.minimum_cents is None or self.maximum_cents is None or self.split_bps is None):raise ValueError('payout terms missing')
        if self.payout and not (0<self.minimum_cents<=self.maximum_cents and 0<=self.split_bps<=10000):raise ValueError('invalid payout terms')

def live_floor(previous,peak,trigger):
    return max(previous,10000 if peak>=trigger else peak-200000)

def simulate(paths,qty,transition_after,policy):
    policy.validate()
    if qty not in (3,6):raise ValueError('study restricted to 3 or 6 MNQ')
    if transition_after is None:return dict(status='AWAITING_EXPLICIT_TRANSITION',ledger=[],payouts=[],balance_cents=None)
    balance=0;floor=-200000;peak=0;ledger=[];payouts=[];status='END_OF_VALID_SAMPLE';bonus_day=None;bonus_days=0;trade_net=0
    for p in sorted(paths,key=lambda p:p['day']):
        if p['day']<=transition_after or not p['traded']:continue
        # Lowest published CME tier is 20 micros; negative-balance tier assumed same.
        cap=40 if balance>=400000 else 30 if balance>=200000 else 20
        assert qty<=cap
        start=balance;cash=start-policy.fee_rt_cents*qty;hit=None
        for row,ts,g in p['record_lows']:
            if cash+g*qty*50<=floor:hit=(row,ts,g);break
        if cash<=floor:hit=(p['entry_row'],p['entry_event_ns'],0)
        balance=cash+(hit[2] if hit else p['gain_ticks'])*qty*50
        pnl=balance-start;trade_net+=pnl;gross=received=0;oldfloor=floor
        if hit:status='LIVE_MLL_BREACH'
        else:
            peak=max(peak,balance);floor=live_floor(floor,peak,policy.lock_trigger_cents)
            bonus_days+=abs(pnl)>=100
            if bonus_day is None and bonus_days>=20 and trade_net>=400000:bonus_day=p['day']
            if policy.payout:
                available=balance-(10000+policy.reserve_cents)
                if available>=policy.minimum_cents:
                    gross=min(available,policy.maximum_cents);received=gross*policy.split_bps//10000
                    balance-=gross;floor=10000
                    payouts.append(dict(day=p['day'],gross_cents=gross,received_cents=received,after_cents=balance))
        ledger.append(dict(day=p['day'],start_cents=start,pnl_cents=pnl,fee_cents=qty*policy.fee_rt_cents,payout_cents=gross,received_cents=received,end_cents=balance,floor_before_cents=oldfloor,floor_after_cents=floor,exit_row=hit[0] if hit else p['exit_row'],outcome='LIVE_MLL_BREACH' if hit else p['outcome']))
        if hit:break
    withdrawn=sum(p['gross_cents'] for p in payouts)
    assert balance==trade_net-withdrawn
    return dict(status=status,first_day=ledger[0]['day'] if ledger else '',last_day=ledger[-1]['day'] if ledger else '',trades=len(ledger),balance_cents=balance,floor_cents=floor,trading_net_cents=trade_net,gross_withdrawn_cents=withdrawn,received_cents=sum(p['received_cents'] for p in payouts),first_bonus_objectives_day=bonus_day,bonus_credited_cents=0,ledger=ledger,payouts=payouts)

def main():
    src=Path('/home/hoang/fleet-1017/out/lucid50k-EDA-3v6-20261022');prior=Path('/home/hoang/fleet-1017/out/lucid50k-EDA-payout-20261023');out=Path('/home/hoang/fleet-1017/out/lucid50k-EDA-live-20261023');out.mkdir(exist_ok=False)
    def digest(p):return hashlib.sha256(p.read_bytes()).hexdigest()
    for folder,names in [(src,['paths.json']),(prior,['summary.csv'])]:
        audit=json.loads((folder/'audit.json').read_text())
        for n in names:assert digest(folder/n)==audit['artifacts'][n]
    paths=json.loads((src/'paths.json').read_text());rows=[];ledger=[]
    for s in csv.DictReader((prior/'summary.csv').open()):
        for policy_name,payout in [('no_live_withdrawals',False),('illustrative_500_to_2000_keep_2100',True)]:
            for trigger in [200000,210000]:
                key=dict(method=s['method'],qty=int(s['qty']),execution=s['execution'],policy=policy_name,lock_trigger_cents=trigger)
                if int(s['payout_count'])!=5:
                    rows.append(dict(**key,status='NO_FIFTH_PAYOUT_TRANSITION',sim_received_cents=int(s['received_cents']),transition_after='',first_day='',last_day='',trades=0,balance_cents=None,floor_cents=None,trading_net_cents=None,gross_withdrawn_cents=None,received_cents=None,first_bonus_objectives_day=None,bonus_credited_cents=0));continue
                pp=[p for p in paths if p['method']==s['method'] and p['execution']==s['execution']]
                policy=Policy(payout=payout,minimum_cents=50000,maximum_cents=200000,split_bps=9000,lock_trigger_cents=trigger)
                r=simulate(pp,int(s['qty']),s['last_day'],policy)
                ledger.extend(dict(**key,**l) for l in r.pop('ledger'));r.pop('payouts')
                rows.append(dict(**key,sim_received_cents=int(s['received_cents']),transition_after=s['last_day'],**r))
    for name,data in [('summary.csv',rows),('ledger.csv',ledger)]:
        with (out/name).open('x') as f:w=csv.DictWriter(f,fieldnames=data[0]);w.writeheader();w.writerows(data)
    with (out/'audit.json').open('x') as f:json.dump(dict(engine_sha256=digest(Path(__file__)),inputs={str(src/'paths.json'):digest(src/'paths.json'),str(prior/'summary.csv'):digest(prior/'summary.csv')},artifacts={n:digest(out/n) for n in ['summary.csv','ledger.csv']}),f,indent=2)
    for r in rows:
        if r['execution']=='trade_print_proxy' and r['lock_trigger_cents']==200000:print(r)
if __name__=='__main__':main()
