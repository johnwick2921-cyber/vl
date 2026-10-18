import os
SRT="/home/hoang/mm-course/extra/transcripts"
OUT="/home/hoang/fleet-1017/out/truth/annotated"
# tight instruction spans (start_sec, end_sec, REF)
R = {
"Cách vẽ box": {"part1.srt": [
 (15,48,"CODE kernel/mentor/phl.go:90"), (134,183,"CODE kernel/mentor/box.go:555"), (198,230,"CODE kernel/mentor/box.go:545")]},
"Cách trade FTGH": {"part1.srt": [
 (61,97,"CODE kernel/mentor/eval.go:1560"), (132,145,"CODE kernel/mentor/box_trade.go:187"), (155,170,"CODE kernel/mentor/box.go:326")]},
"Major & Key Levels": {"part1.srt": [
 (0,26,"CODE kernel/mentor/mentor.go:548"), (124,139,"CODE kernel/mentor/box_trade.go:187"), (398,419,"CODE kernel/mentor/mentor.go:548"), (430,439,"CODE kernel/mentor/htf_direction.go:47"), (593,691,"RULING kernel/mentor/key_levels.go:21"), (827,865,"RULING kernel/mentor/key_levels.go:14"), (867,946,"RULING kernel/mentor/key_levels.go:28"), (1006,1030,"RULING kernel/mentor/key_levels.go:28"), (1242,1310,"CODE kernel/mentor/box.go:545"), (1465,1497,"CODE kernel/mentor/box.go:555"), (1494,1508,"CODE kernel/mentor/day_seed.go")],
 "part2.srt": [(0,5,"RULING kernel/mentor/key_levels.go:28"), (43,90,"CODE kernel/mentor/day_gate.go:18")]},
"Cách trade key levels 1H": {"part1.srt": [
 (0,30,"CODE kernel/mentor/mentor.go:548"), (230,270,"CODE kernel/mentor/box_trade.go:187"), (270,300,"CODE kernel/mentor/box_trade.go:188"), (394,420,"CODE kernel/mentor/box_trade.go:205"), (494,536,"CODE kernel/mentor/box_trade.go:205")]},
"Routine trước khi trade": {"part1.srt": [
 (0,38,"CODE kernel/mentor/mentor.go:417"), (41,77,"MISMATCH kernel/mentor/eval.go:1581"), (91,129,"CODE kernel/mentor/orb.go:54"), (145,187,"CODE kernel/mentor/orb.go:115"), (317,352,"CODE kernel/mentor/orb.go:60"), (466,509,"CODE kernel/mentor/orb.go:164"), (530,554,"NOT-IN-BOT -> row 29")]},
"Buy-Sell Setup Trigger": {"part1.srt": [
 (0,50,"CODE kernel/mentor/filters.go:43"), (117,130,"CODE kernel/mentor/filters.go:43"), (230,250,"CODE kernel/mentor/mtf.go:22"), (420,490,"CODE kernel/mentor/mtf.go:22"), (745,777,"CODE kernel/mentor/filters.go:143"), (1385,1405,"CODE kernel/mentor/eval.go:1571")],
 "part2.srt": [(0,60,"CODE kernel/mentor/mtf.go:22")]},
"Ping pong": {"part1.srt": [
 (0,87,"CODE kernel/mentor/box_trade.go:205"), (87,110,"CODE kernel/mentor/isb.go:137"), (275,310,"CODE kernel/mentor/isb_box.go:90"), (428,460,"CODE kernel/mentor/box.go:569"), (577,665,"CODE kernel/mentor/box.go:569")]},
"Cách né trap & Setup gồng lời": {"part1.srt": [
 (0,63,"NOT-IN-BOT -> row 41"), (227,260,"CODE kernel/mentor/htf_direction.go:47"), (520,590,"CODE kernel/mentor/htf_direction.go:47"), (691,790,"CODE kernel/mentor/eval.go:1853"), (940,980,"CODE kernel/mentor/exec_frame.go:5"), (1050,1096,"CODE kernel/mentor/eval.go:1915")]},
"Cách vẽ trendline": {"part1.srt": [
 (0,120,"CODE kernel/mentor/trendline.go:74"), (131,180,"CODE kernel/mentor/trendline.go:11"), (292,339,"CODE kernel/mentor/flag_wall.go:11"), (347,412,"CODE kernel/mentor/flag_wall.go:20")]},
"Cách vẽ supply & demand trong ngày": {"part1.srt": [
 (0,44,"CODE kernel/mentor/box.go:545"), (148,172,"CODE kernel/mentor/box.go:555")]},
"Go over lớp học": {"part1.srt": [
 (210,260,"CODE kernel/mentor/filters.go:43"), (280,340,"CODE kernel/mentor/isb_box.go:90"), (430,470,"CODE kernel/mentor/phl.go:90"), (520,570,"CODE kernel/mentor/isb.go:137"), (630,690,"CODE kernel/mentor/phl.go:90"), (750,780,"CODE kernel/mentor/box.go:545"), (810,880,"CODE kernel/mentor/mtf.go:22"), (1000,1030,"CODE kernel/mentor/orb.go:54"), (1030,1060,"MISMATCH kernel/mentor/eval.go:1581"), (1060,1110,"NOT-IN-BOT kernel/mentor/eval.go:1579"), (1215,1233,"CODE kernel/mentor/box_trade.go:187")]},
"Bull-Bear Flags": {"part1.srt": [
 (0,26,"CODE kernel/mentor/flag_wall.go:23"), (26,115,"CODE kernel/mentor/flag_wall.go:11"), (156,164,"CODE kernel/mentor/box.go:545"), (170,200,"CODE kernel/mentor/mtf.go:22")]},
}
def parse(p):
    c=[]; ls=open(p,encoding="utf-8").read().splitlines(); i=0
    while i<len(ls):
        if not ls[i].strip(): i+=1; continue
        if i+2<len(ls) and "-->" in ls[i+1]:
            ts=ls[i+1].split("-->")[0].strip().split(",")[0]; c.append((ts,ls[i+2].strip())); i+=4
        else: i+=1
    return c
def sec(ts):
    p=ts.split(":"); return int(p[1])*60+int(p[2])
def gen(folder,part,rules):
    cues=parse(os.path.join(SRT,folder,part)); out=[]; rc=nc=nmm=nnib=nrul=nsp=ndash=0
    for ts,txt in cues:
        m=sec(ts); ref=None
        for a,b,r in rules:
            if a<=m<=b: ref=r; break
        if ref:
            rc+=1
            if ref.startswith("CODE"): nc+=1
            elif ref.startswith("MISMATCH"): nmm+=1
            elif ref.startswith("NOT-IN-BOT"): nnib+=1
            elif ref.startswith("RULING"): nrul+=1
            out.append(f"{ts} | {txt} | {ref}")
        else:
            ndash+=1; out.append(f"{ts} | {txt} | —")
    hdr=f"cues: {len(cues)}/{len(cues)} · rule cues: {rc} · rows: {len(rules)} · CODE: {nc} · MISMATCH: {nmm} · NOT-IN-BOT: {nnib} · RULING: {nrul} · SPLIT: {nsp} · —: {ndash}"
    return hdr,out
def main():
    os.makedirs(OUT,exist_ok=True); rep=[]
    for folder,parts in R.items():
        for part,rules in parts.items():
            hdr,out=gen(folder,part,rules)
            d=os.path.join(OUT,folder); os.makedirs(d,exist_ok=True)
            open(os.path.join(d,part.replace(".srt",".md")),"w",encoding="utf-8").write(hdr+"\n\n"+"\n".join(out)+"\n")
            rep.append(f"{folder}/{part} — {hdr}")
    for r in rep: print(r)
main()
