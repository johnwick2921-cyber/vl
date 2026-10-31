from pathlib import Path
import json,hashlib
from decimal import Decimal
import databento as db
ROOT=Path('/home/hoang/fleet-1017/out/day32-week-20261026/warmup')
def main():
    ROOT.mkdir(exist_ok=False)
    c=db.Historical(Path('/home/hoang/.config/vl-research/databento.key').read_text().strip())
    req=dict(dataset='GLBX.MDP3',schema='ohlcv-1m',symbols=['MNQU6','MNQZ6','MNQH7'],stype_in='raw_symbol',start='2026-10-19T00:00:00Z',end='2026-10-31T00:00:00Z')
    cost=Decimal(str(c.metadata.get_cost(**req)))
    with (ROOT/'quote.json').open('x') as f:json.dump(dict(request=req,cost=str(cost)),f,indent=2)
    print('warmup quote USD',cost,flush=True)
    if cost!=0:raise SystemExit('HELD: nonzero quote')
    p=ROOT/'eth-1m.dbn.zst';s=c.timeseries.get_range(**req,path=p);s.reader.close()
    conditions=c.metadata.get_dataset_condition(dataset='GLBX.MDP3',start_date='2026-10-19',end_date='2026-10-31')
    with (ROOT/'dataset-condition.json').open('x') as f:json.dump(conditions,f,indent=2)
    with (ROOT/'warmup-receipt.json').open('x') as f:json.dump(dict(bytes=p.stat().st_size,sha256=hashlib.sha256(p.read_bytes()).hexdigest()),f,indent=2)
    print('warmup complete',p.stat().st_size,flush=True)
if __name__=='__main__':main()
