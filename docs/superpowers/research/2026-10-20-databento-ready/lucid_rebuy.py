"""Hypothetical immediate-funded replacement after breach OR payout five.
No evaluation, purchase price, onboarding delay, live transition, or real orders.
"""
import csv,json,hashlib
from pathlib import Path
from lucid_payout import simulate

def repeat(paths,qty):
    remaining=sorted([p for p in paths if p['traded']],key=lambda p:p['day'])
    assert len(remaining)==len({p['day'] for p in remaining})
    accounts=[];ledger=[];payouts=[]
    while remaining:
        r=simulate(remaining,qty);assert r['trades']>0
        number=len(accounts)+1;first=remaining[0]['day']
        ledger.extend(dict(account=number,**l) for l in r.pop('ledger'))
        payouts.extend(dict(account=number,**l) for l in r.pop('payouts'))
        r['first_day']=first;r['account']=number
        if r['status']=='LIVE_TRANSITION_AFTER_5_PAYOUTS':r['status']='REPLACE_AFTER_PAYOUT_FIVE_HYPOTHETICAL'
        accounts.append(r)
        remaining=remaining[r['trades']:]
        if r['status']=='END_OF_VALID_SAMPLE':assert not remaining
    summary=dict(qty=qty,accounts_used=len(accounts),replacements=max(0,len(accounts)-1),breached_accounts=sum(a['status']=='MLL_BREACH' for a in accounts),five_payout_accounts=sum(a['payout_count']==5 for a in accounts),trades=len(ledger),payout_count=len(payouts),gross_withdrawn_cents=sum(a['gross_withdrawn_cents'] for a in accounts),received_cents=sum(a['received_cents'] for a in accounts),last_balance_cents=accounts[-1]['ending_balance_cents'] if accounts else None,last_status=accounts[-1]['status'] if accounts else 'NO_TRADES',purchase_cost_cents=None,net_after_purchase_cents=None)
    assert len(ledger)==len({l['day'] for l in ledger})
    return summary,accounts,ledger,payouts

def main():
    src=Path('/home/hoang/fleet-1017/out/lucid50k-EDA-3v6-20261022');out=Path('/home/hoang/fleet-1017/out/lucid50k-AE-rebuy-20261023');out.mkdir(exist_ok=False)
    raw=(src/'paths.json').read_bytes();assert hashlib.sha256(raw).hexdigest()==json.loads((src/'audit.json').read_text())['artifacts']['paths.json'];paths=json.loads(raw)
    allrows=[[],[],[],[]]
    for method in ['A','E']:
        for execution in ['trade_print_proxy','stress1tick']:
            for qty in [3,6]:
                result=repeat([p for p in paths if p['method']==method and p['execution']==execution],qty)
                for dest,rr in zip(allrows,[[result[0]],*result[1:]]):
                    dest.extend(dict(r,method=method,execution=execution,qty=qty) for r in rr)
    for name,rr in zip(['summary.csv','accounts.csv','ledger.csv','payouts.csv'],allrows):
        with (out/name).open('x') as f:w=csv.DictWriter(f,fieldnames=rr[0]);w.writeheader();w.writerows(rr)
    with (out/'audit.json').open('x') as f:json.dump(dict(source=str(src/'paths.json'),source_sha256=hashlib.sha256(raw).hexdigest(),engine_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),account_engine_sha256=hashlib.sha256(Path(__file__).with_name('lucid_payout.py').read_bytes()).hexdigest(),artifacts={n:hashlib.sha256((out/n).read_bytes()).hexdigest() for n in ['summary.csv','accounts.csv','ledger.csv','payouts.csv']}),f,indent=2)
    for r in allrows[0]:print(r)
if __name__=='__main__':main()
