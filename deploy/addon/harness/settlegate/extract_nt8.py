#!/usr/bin/env python3
"""Turn NT8's own files into the event file Replay.cs reads. Read only.
   extract_nt8.py log|trace <dir> > events.tsv     (keep the output out of the repo)
log   = NT8 log files, closes = the AddOn's own close_position lines
trace = NT8 trace files, closes = every Cbi.Position.Close0 of a Sim account"""
import sys, re, glob, datetime, os
mode, d = sys.argv[1], sys.argv[2]
PH = {'Initialized':1,'Submitted':2,'Accepted':3,'Trigger pending':4,'TriggerPending':4,'Change pending':5,'ChangePending':5,
      'Change submitted':5,'ChangeSubmitted':5,'Working':6,'Partially filled':7,'PartFilled':7,'Cancel pending':8,'CancelPending':8,
      'Cancel submitted':8,'CancelSubmitted':8,'Filled':9,'Cancelled':10,'Rejected':11}
def kind(t):
    t = t.replace(' ', '')
    return 1 if t in ('StopMarket','StopLimit') else 2 if t == 'Market' else 0
def emit(*a): print('\t'.join(str(x) for x in a))
def ms(a, b): return int(datetime.datetime.strptime(a, '%Y-%m-%d %H:%M:%S').timestamp()*1000 + int(b))
def g(s, k):
    m = re.search(r'(?:^| )' + k + r"=(?:'([^']*)'|([^ ]*))", s)
    return (m[1] if m[1] is not None else m[2]) if m else ''
for f in sorted(glob.glob(os.path.join(d, ('log' if mode == 'log' else 'trace') + '.2026*.txt'))):
    if f.endswith('.en.txt'): continue
    inst = {}
    for l in open(f, errors='replace', encoding='utf-8'):
        if mode == 'log':
            m = re.match(r'(\d{4}-\d\d-\d\d \d\d:\d\d:\d\d):(\d{3})\|\d+\|(\d+)\|(.*)', l)
            if not m: continue
            t, k, s = ms(m[1], m[2]), m[3], m[4]
            if k == '32':
                x = re.match(r"Order='([^/]*)/([^']*)' Name='[^']*' New state='([^']*)' Instrument='([^']*)' Action='([^']*)'.*Quantity=\d+ Type='([^']*)'.* Filled=(\d+)", s)
                if not x or not x[2].startswith('Sim'): continue
                inst[x[2]] = x[4]
                emit('O', t, x[2], x[4], x[1], int(x[5].startswith('Buy')), int(x[5] in ('Sell','Buy to cover')), kind(x[6]), PH.get(x[3], 0), x[7])
            elif k == '64':
                x = re.match(r"Instrument='([^']*)' Account='([^']*)' Average price=[\d.]+ Quantity=(\d+) Market position=(\w+)", s)
                if not x or not x[2].startswith('Sim'): continue
                emit('P', t, x[2], x[1], int(x[3])*(1 if x[4]=='Long' else -1 if x[4]=='Short' else 0))
            else:
                x = re.search(r'flatten submitted .* on account=(\S+)', s)
                if x and x[1] in inst: emit('F', t, x[1], inst[x[1]])
                x = re.search(r'close_position (\S+) \S+ account=(\S+) .*holds=(\w+)(?: (\d+))?', s)
                if not x or x[2] not in inst: continue
                held = 0 if x[3] == 'flat' else int(x[4])*(1 if x[3]=='Long' else -1)
                emit('C', t, x[2], inst[x[2]], held, m[1]+':'+m[2]+' '+x[2])
        else:
            m = re.match(r'(\d{4}-\d\d-\d\d \d\d:\d\d:\d\d):(\d{3}) \([^)]*\) (Cbi\.\S+?): (.*)', l)
            if not m: continue
            t, k, s = ms(m[1], m[2]), m[3], m[4]
            a, i = g(s, 'account'), g(s, 'instrument')
            if not a.startswith('Sim'): continue
            if k == 'Cbi.Account.OrderUpdateCallback':
                act = g(s, 'orderAction')
                emit('O', t, a, i, g(s, 'orderId'), int(act.startswith('Buy')), int(act in ('Sell','BuyToCover')), kind(g(s, 'orderType')), PH.get(g(s, 'orderState'), 0), g(s, 'filled') or 0)
            elif k == 'Cbi.Account.PositionUpdateCallback':
                mp = g(s, 'marketPosition')
                emit('P', t, a, i, int(g(s, 'quantity'))*(1 if mp=='Long' else -1 if mp=='Short' else 0))
            elif k == 'Cbi.Position.Close0':
                emit('C', t, a, i, g(s, 'currentQuantity'), m[1]+':'+m[2]+' '+a); emit('F', t, a, i)
