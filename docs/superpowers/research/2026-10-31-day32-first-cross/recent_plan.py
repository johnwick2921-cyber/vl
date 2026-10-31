"""Read-only Databento metadata quote for new overnight study. No data purchase."""
from pathlib import Path
from datetime import date,datetime,time,timedelta,timezone
from zoneinfo import ZoneInfo
from concurrent.futures import ThreadPoolExecutor
from decimal import Decimal
import json
import databento as db
CT=ZoneInfo('America/Chicago')
OUT=Path('/home/hoang/fleet-1017/out/day32-recent-six-months-20261031')
SYMBOLS=['MNQM6','MNQU6','MNQZ6','MNQH7']
def plan():
 rows=[];d=date(2026,5,1)
 while d<date(2026,10,31):
  if d.weekday()<5:
   prior=d-timedelta(days=1)
   a=datetime.combine(prior,time(17),CT);b=datetime.combine(d,time(8,30),CT)
   rows.append(dict(day=d.isoformat(),open_ns=int(a.timestamp())*10**9,end_ns=int(b.timestamp())*10**9,request=dict(dataset='GLBX.MDP3',schema='tbbo',symbols=SYMBOLS,stype_in='raw_symbol',start=a.astimezone(timezone.utc).isoformat(),end=(b+timedelta(minutes=1)).astimezone(timezone.utc).isoformat())))
  d+=timedelta(days=1)
 return rows
if __name__=='__main__':
 OUT.mkdir(exist_ok=False)
 rows=plan()
 with (OUT/'plan.json').open('x') as f:json.dump(rows,f,indent=2)
 key=Path('/home/hoang/.config/vl-research/databento.key').read_text().strip()
 def quote(r):
  try:
   c=db.Historical(key);cost=c.metadata.get_cost(**r['request'])
   return dict(day=r['day'],usd=str(cost))
  except Exception as e:return dict(day=r['day'],error=type(e).__name__)
 with ThreadPoolExecutor(max_workers=4) as ex:result=list(ex.map(quote,rows))
 total=sum((Decimal(r['usd']) for r in result if 'usd' in r),Decimal(0))
 data=dict(days=len(rows),failed=sum('error' in r for r in result),quote_usd=str(total),days_quotes=result)
 with (OUT/'quote.json').open('x') as f:json.dump(data,f,indent=2)
 print('Days',data['days'],'failed',data['failed'],'total USD',total,flush=True)
