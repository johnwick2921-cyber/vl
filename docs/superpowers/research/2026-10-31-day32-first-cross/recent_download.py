"""Retrieve only zero-quoted requests; never overwrite existing evidence."""
from pathlib import Path
from concurrent.futures import ThreadPoolExecutor
from decimal import Decimal
import json,hashlib,stat
import databento as db
ROOT=Path('/home/hoang/fleet-1017/out/day32-recent-six-months-20261031')

def run():
    keypath=Path('/home/hoang/.config/vl-research/databento.key')
    if keypath.is_symlink() or stat.S_IMODE(keypath.stat().st_mode)&0o077:
        raise RuntimeError('Private key file permissions')
    key=keypath.read_text().strip()
    plan=json.loads((ROOT/'plan.json').read_text())
    prior={r['day']:r for r in json.loads((ROOT/'quote.json').read_text())['days_quotes']}
    out=ROOT/'included-download';out.mkdir(exist_ok=False)
    def one(r):
        day=r['day'];result={'day':day}
        if Decimal(prior[day].get('usd','1'))!=0:
            return dict(result,status='HELD_NONZERO_QUOTE')
        target=out/day;target.mkdir(exist_ok=False)
        try:
            client=db.Historical(key)
            cost=Decimal(str(client.metadata.get_cost(**r['request'])))
            result['fresh_quote_usd']=str(cost)
            if cost!=0:
                result['status']='HELD_NONZERO_FRESH_QUOTE'
            else:
                path=target/'ticks.dbn'
                store=client.timeseries.get_range(**r['request'],path=path)
                store.reader.close()
                h=hashlib.sha256()
                with path.open('rb') as source:
                    for block in iter(lambda:source.read(1048576),b''):h.update(block)
                result.update(status='DOWNLOADED_NOT_YET_AUDITED',bytes=path.stat().st_size,sha256=h.hexdigest())
        except Exception as exc:
            result.update(status='ERROR',error_type=type(exc).__name__)
        with (target/'receipt.json').open('x') as f:json.dump(result,f,indent=2)
        return result
    results=[]
    with ThreadPoolExecutor(max_workers=4) as pool:
        for r in pool.map(one,plan):
            results.append(r);print(r['day'],r['status'],flush=True)
    with (out/'manifest.json').open('x') as f:json.dump(results,f,indent=2)
    print('DONE',len(results),flush=True)
if __name__=='__main__':run()
