"""Reuse existing daytime raw files; obtain nine missing zero-quoted sessions."""
from pathlib import Path
from datetime import date,datetime,time,timedelta,timezone
from zoneinfo import ZoneInfo
from concurrent.futures import ThreadPoolExecutor
from decimal import Decimal
import json,hashlib
import databento as db
ROOT=Path('/home/hoang/fleet-1017/out/orb3-recent-20261031')
OLD=Path('/home/hoang/fleet-1017/out/databento-mnq-20261020-data')
CT=ZoneInfo('America/Chicago')
def main():
    ROOT.mkdir(exist_ok=False);data=ROOT/'data';data.mkdir()
    days=[];missing=[];d=date(2026,5,1)
    while d<=date(2026,10,30):
        if d.weekday()<5:
            day=str(d);days.append(day)
            if (OLD/day/'complete.json').exists():(data/day).symlink_to(OLD/day,target_is_directory=True)
            else:missing.append(d)
        d+=timedelta(days=1)
    with (ROOT/'plan.json').open('x') as f:json.dump(dict(days=days,reused=len(days)-len(missing),new_dates=list(map(str,missing))),f,indent=2)
    key=Path('/home/hoang/.config/vl-research/databento.key').read_text().strip()
    def download(d):
        day=str(d);p=data/day;p.mkdir(exist_ok=False);c=db.Historical(key)
        req=dict(dataset='GLBX.MDP3',schema='tbbo',symbols=['MNQZ6'],stype_in='raw_symbol',start=datetime.combine(d,time(7,50),CT).astimezone(timezone.utc).isoformat(),end=datetime.combine(d,time(15,10),CT).astimezone(timezone.utc).isoformat())
        cost=Decimal(str(c.metadata.get_cost(**req)))
        with (p/'request.json').open('x') as f:json.dump(dict(request=req,usd=str(cost)),f,indent=2)
        if cost!=0:raise ValueError('Nonzero quote: '+day)
        raw=p/'ticks.dbn.zst';store=c.timeseries.get_range(**req,path=raw);store.reader.close()
        s=db.DBNStore.from_file(raw);n=sum(1 for _ in s);s.reader.close()
        receipt=dict(day=day,download_complete=True,quoted_usd=str(cost),records=n,bytes=raw.stat().st_size,sha256=hashlib.sha256(raw.read_bytes()).hexdigest(),research_ready=False)
        with (p/'complete.json').open('x') as f:json.dump(receipt,f,indent=2)
        return receipt
    with ThreadPoolExecutor(4) as pool:
        for r in pool.map(download,missing):print(r['day'],'downloaded',r['records'],flush=True)
    print('Data ready',len(days),'sessions',flush=True)
if __name__=='__main__':main()
