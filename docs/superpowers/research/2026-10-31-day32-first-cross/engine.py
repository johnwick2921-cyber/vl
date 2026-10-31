"""Offline first-cross engine. Price units are exchange ticks (0.25 points).
Research assumptions are in SPEC.md. No network, broker, or database access.
"""
from dataclasses import dataclass
from decimal import Decimal, ROUND_FLOOR, ROUND_CEILING, localcontext

D = Decimal
SPAN = 120_000_000_000

@dataclass(frozen=True)
class Tick:
    row: int
    event_ns: int
    recv_ns: int
    price: int


def seed(closes, period):
    if len(closes) < 1000:
        raise ValueError("1000 completed warmup bars required")
    with localcontext() as ctx:
        ctx.prec = 40
        alpha = D(2) / D(period + 1)
        v = D(closes[0])
        for price in closes[1:]:
            v += alpha * (D(price) - v)
        return v


def simulate(ticks, opened, cutoff, warm, stop_points):
    if stop_points not in (15, 16):
        raise ValueError("Only owner-approved SL15 and SL16")
    with localcontext() as ctx:
        ctx.prec = 40
        return _simulate(ticks, opened, cutoff, warm, stop_points)


def _simulate(ticks, opened, cutoff, warm, stop_points):
    base9, base34 = seed(warm, 9), seed(warm, 34)
    prevdiff = base9-base34
    cur9, cur34 = base9, base34
    bucket = None
    used = False
    side = 0
    order = None
    entry = None
    result = dict(outcome="NO_CROSS", stop_points=stop_points,
                  signal_row=None, entry_row=None, exit_row=None,
                  pnl_points=None, nominal_pnl_points=None)
    prevrow = -1
    prevrecv = -1
    prevevent = -1
    last = None
    for tick in ticks:
        if tick.row <= prevrow or tick.recv_ns < prevrecv:
            raise ValueError("Nonmonotone wire order")
        if tick.event_ns < prevevent:
            raise ValueError("Event regression: explicit review required")
        if tick.recv_ns < tick.event_ns:
            raise ValueError("Receipt before event")
        prevrow, prevrecv, prevevent = tick.row, tick.recv_ns, tick.event_ns
        if tick.event_ns < opened:
            continue
        if tick.event_ns >= cutoff or tick.recv_ns >= cutoff:
            break
        last = tick
        if entry is not None:
            gain = side*(tick.price-entry)
            if gain <= -stop_points*4 or gain >= 60:
                stopped = gain <= -stop_points*4
                result.update(outcome="SL" if stopped else "TP", exit_row=tick.row,
                              exit_event_ns=tick.event_ns,
                              pnl_points=gain/4 if stopped else 15,
                              nominal_pnl_points=-stop_points if stopped else 15)
                return result
            continue
        # Execute the already resting limit before seeing this tick's amendment.
        if order is not None and side*(tick.price-order) <= 0:
            entry = order
            result.update(outcome="OPEN_AT_CUTOFF", entry_row=tick.row,
                          entry_event_ns=tick.event_ns, entry_ticks=entry)
            continue
        newbucket = (tick.event_ns-opened)//SPAN
        if bucket is not None and newbucket != bucket:
            base9, base34 = cur9, cur34
        bucket = newbucket
        cur9 = base9 + (D(tick.price)-base9)/5
        cur34 = base34 + D(2)*(D(tick.price)-base34)/35
        diff = cur9-cur34
        cross = 1 if prevdiff <= 0 and diff > 0 else -1 if prevdiff >= 0 and diff < 0 else 0
        prevdiff = diff
        if not used and cross:
            used = True
            side = cross
            result.update(outcome="NO_RETEST", signal_row=tick.row,
                          signal_event_ns=tick.event_ns, side=side)
        elif side and cross == -side:
            side = 0
            order = None
            result["outcome"] = "FIRST_CROSS_CANCELLED"
        if side:
            rounding = ROUND_FLOOR if side == 1 else ROUND_CEILING
            candidate = int(cur9.to_integral_value(rounding=rounding))
            order = candidate if side*(tick.price-candidate) > 0 else None
    if entry is not None and last is not None:
        result["mark_pnl_points"] = side*(last.price-entry)/4
    return result
