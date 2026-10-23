"""Owner hypothetical: repeated SIM/live, 14 calendar days after live breach.
Evaluation and approval delays omitted; not a provider entitlement model.
"""
import csv, json, hashlib
from pathlib import Path
from datetime import date, timedelta
from lucid_payout import simulate as sim
from lucid_live import simulate as live, Policy
from test_lucid_payout import oracle as sim_oracle
from test_lucid_live import oracle as live_oracle


def cooldown_end(day):
    return (date.fromisoformat(day)+timedelta(days=14)).isoformat()


def run(paths, qty):
    remaining=sorted((p for p in paths if p['traded']),key=lambda p:p['day'])
    original=[p['day'] for p in remaining]
    assert len(original)==len(set(original))
    policy=Policy(payout=True, minimum_cents=1, maximum_cents=sum(max(0,p['gain_ticks'])*qty*50 for p in remaining)+1, split_bps=9000,reserve_cents=200000)
    stages=[]; ledger=[]; payouts=[]; skipped=[]; phase='SIM'
    while remaining:
        result=sim(remaining,qty) if phase=='SIM' else live(remaining,qty,'0000-00-00',policy)
        expected=sim_oracle(remaining,qty) if phase=='SIM' else live_oracle(remaining,qty,'0000-00-00',policy)
        for k,v in expected.items(): assert result[k]==v,(phase,k,result[k],v)
        n=result['trades']; assert n>0
        tag=dict(stage=len(stages)+1,phase=phase)
        rows=result.pop('ledger'); pays=result.pop('payouts')
        assert [l['day'] for l in rows]==[p['day'] for p in remaining[:n]]
        balance=result['ending_balance_cents'] if phase=='SIM' else result['balance_cents']
        assert balance==(5000000 if phase=='SIM' else 0)+sum(l['pnl_cents'] for l in rows)-sum(p['gross_cents'] for p in pays)
        stages.append(dict(**tag,first_day=remaining[0]['day'],last_day=result['last_day'],status=result['status'],trades=n,payout_count=len(pays),received_cents=result['received_cents'],balance_cents=balance))
        ledger.extend(dict(**tag,**r) for r in rows);payouts.extend(dict(**tag,**p) for p in pays)
        remaining=remaining[n:]
        if result['status']=='END_OF_VALID_SAMPLE': assert not remaining
        elif phase=='SIM':
            phase='LIVE' if result['status']=='LIVE_TRANSITION_AFTER_5_PAYOUTS' else 'SIM'
        else:
            assert result['status']=='LIVE_MLL_BREACH'
            eligible=cooldown_end(result['last_day'])
            skipped.extend(dict(day=p['day'],after_breach=result['last_day'],eligible_on=eligible) for p in remaining if p['day']<eligible)
            remaining=[p for p in remaining if p['day']>=eligible];phase='SIM'
    used=[r['day'] for r in ledger];missed=[r['day'] for r in skipped]
    assert len(used)==len(set(used)) and not set(used)&set(missed)
    assert sorted(used+missed)==original
    totals={p:sum(s['received_cents'] for s in stages if s['phase']==p) for p in ['SIM','LIVE']}
    summary=dict(qty=qty,sim_accounts=sum(s['phase']=='SIM' for s in stages),live_accounts=sum(s['phase']=='LIVE' for s in stages),sim_breaches=sum(s['status']=='MLL_BREACH' for s in stages),live_breaches=sum(s['status']=='LIVE_MLL_BREACH' for s in stages),trades=len(ledger),skipped_signals=len(skipped),payout_count=len(payouts),sim_received_cents=totals['SIM'],live_received_cents=totals['LIVE'],total_received_cents=sum(totals.values()),last_phase=stages[-1]['phase'],last_status=stages[-1]['status'],last_balance_cents=stages[-1]['balance_cents'],purchase_cost_cents=None)
    return summary,stages,ledger,payouts,skipped


def main():
    src=Path('/home/hoang/fleet-1017/out/lucid50k-EDA-3v6-20261022')
    out=Path('/home/hoang/fleet-1017/out/lucid50k-AE-lifecycle14-20261023');out.mkdir(exist_ok=False)
    raw=(src/'paths.json').read_bytes();digest=hashlib.sha256(raw).hexdigest()
    assert digest==json.loads((src/'audit.json').read_text())['artifacts']['paths.json']
    paths=json.loads(raw);groups=[[],[],[],[],[]]
    for method in ['A','E']:
        for execution in ['trade_print_proxy','stress1tick']:
            for qty in [3,6]:
                result=run([p for p in paths if p['method']==method and p['execution']==execution],qty)
                for dst,rows in zip(groups,[[result[0]],*result[1:]]):dst.extend(dict(method=method,execution=execution,qty=qty,**{k:v for k,v in r.items() if k!='qty'}) for r in rows)
    names=['summary','stages','ledger','payouts','skipped']
    for name,rows in zip(names,groups):
        fields=list(dict.fromkeys(k for r in rows for k in r))
        with (out/(name+'.csv')).open('x') as f:
            w=csv.DictWriter(f,fieldnames=fields);w.writeheader();w.writerows(rows)
    audit=dict(status='PASS',source_sha256=digest,independent_decimal_oracle_every_stage=True,unique_dates_and_full_signal_partition=True,stage_cash_conservation=True,calendar_cooldown_days=14,eligible_boundary_inclusive=True,files={n+'.csv':hashlib.sha256((out/(n+'.csv')).read_bytes()).hexdigest() for n in names},engines={n:hashlib.sha256(Path(n).read_bytes()).hexdigest() for n in ['lucid_lifecycle14.py','lucid_payout.py','lucid_live.py']})
    with (out/'verification.json').open('x') as f:json.dump(audit,f,indent=2)
    for r in groups[0]:print(json.dumps(r))
if __name__=='__main__':main()
