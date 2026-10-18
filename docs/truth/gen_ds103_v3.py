#!/usr/bin/env python3
# DS-103 Phase 1b v3 — tight ranges (instruction-only) + correct per-rule refs,
# after DS-101 cross-audit (000035). Ref lines verified: filters.go:144/145,
# eval.go:1030/1501/2159/2161/1016/1193/1581/1880, box_trade.go:189/212/270,
# box.go:315/317/828, trendline.go:280, isb.go:72/125/182, isb_box.go:74,
# htf_direction.go:53, mentor.go:530/537/596.
import os, re
from collections import defaultdict

BASE = "/home/hoang/mm-course/transcripts"
OUT  = "/home/hoang/fleet-1017/out/truth/annotated"
TS = re.compile(r'^(\d{2}):(\d{2}):(\d{2})[.,]\d{3}\s*-->\s*\d{2}:\d{2}:\d{2}[.,]\d{3}')

def parse(path):
    cues = []
    with open(path, encoding='utf-8') as f:
        lines = f.read().splitlines()
    i = 0
    while i < len(lines):
        m = TS.match(lines[i].strip())
        if m:
            mm, ss = int(m.group(2)), int(m.group(3))
            mmss = f"{mm:02d}:{ss:02d}"
            i += 1
            txt = []
            while i < len(lines) and lines[i].strip() != '':
                txt.append(lines[i].strip()); i += 1
            cues.append((mmss, ' '.join(txt)))
        else:
            i += 1
    return cues

def to_s(mmss):
    mm, ss = mmss.split(':'); return int(mm)*60 + int(ss)

R = [
 ("Ngày 3.1 Go over ngày 2","part1","00:26","00:34","","NOT-IN-BOT",1),
 ("Ngày 3.1 Go over ngày 2","part1","01:03","01:08","","NOT-IN-BOT",2),
 ("Ngày 3.1 Go over ngày 2","part1","01:31","01:45","kernel/mentor/isb.go:72 [isb_stop_limit]","CODE",3),
 ("Ngày 3.1 Go over ngày 2","part1","04:22","04:34","kernel/mentor/isb.go:182 [isb_stack_cancel]","COURSE-SPLIT",4),
 ("Ngày 3.1 Go over ngày 2","part1","11:12","11:18","","NOT-IN-BOT",5),
 ("Ngày 3.1 Go over ngày 2","part1","13:30","14:01","kernel/mentor/eval.go:1581 [exec_2m_after_30m]","MISMATCH",6),
 ("Ngày 3.1 Go over ngày 2","part1","14:18","14:38","","NOT-IN-BOT",7),
 ("Ngày 3.2 FTGH FTGL","part1","03:21","03:35","kernel/mentor/box.go:315 [ftgh_key]","CODE",8),
 ("Ngày 3.2 FTGH FTGL","part1","03:56","04:23","kernel/mentor/box.go:315 [ftgh_key]","CODE",9),
 ("Ngày 3.2 FTGH FTGL","part1","04:29","04:53","kernel/mentor/box_trade.go:212 [box_return]","CODE",10),
 ("Ngày 3.2 FTGH FTGL","part1","06:00","06:13","","NOT-IN-BOT",11),
 ("Ngày 3.2 FTGH FTGL","part1","07:07","07:31","kernel/mentor/box_trade.go:189 [close_inside_cancel]","CODE",12),
 ("Ngày 3.2 FTGH FTGL","part1","08:21","08:38","kernel/mentor/box_trade.go:189 [close_inside_cancel]","CODE",13),
 ("Ngày 3.2 FTGH FTGL","part1","09:50","10:08","kernel/mentor/trendline.go:280 [trendline_valid]","CODE",14),
 ("Ngày 3.2 FTGH FTGL","part1","11:08","11:14","kernel/mentor/mentor.go:530 [key_level_prune_pts]","CODE",15),
 ("Ngày 3.2 FTGH FTGL","part1","13:31","13:42","kernel/mentor/isb.go:125 [isb_max_stop]","CODE",16),
 ("Ngày 3.2 FTGH FTGL","part1","18:02","18:26","kernel/mentor/box.go:317 [ftgl_key]","CODE",17),
 ("Ngày 3.2 FTGH FTGL","part1","21:04","21:39","kernel/mentor/box_trade.go:212 [box_return]","CODE",18),
 ("Ngày 3.2 FTGH FTGL","part1","22:12","23:08","kernel/mentor/box.go:828 [near_box_refusal]","CODE",19),
 ("Ngày 3.2 FTGH FTGL","part2","00:24","00:28","","NOT-IN-BOT",20),
 ("Ngày 3.2 FTGH FTGL","part2","00:38","01:06","kernel/mentor/trendline.go:280 [trendline_valid]","CODE",21),
 ("Ngày 3.2 FTGH FTGL","part2","03:34","04:06","kernel/mentor/trendline.go:280 [trendline_3rd_touch]","CODE",22),
 ("Ngày 3.2 FTGH FTGL","part2","06:25","07:28","kernel/mentor/box_trade.go:189 [close_inside_cancel]","CODE",23),
 ("Ngày 3.2 FTGH FTGL","part2","08:10","08:22","kernel/mentor/box_trade.go:212 [box_return]","CODE",24),
 ("Ngày 3.2 FTGH FTGL","part2","08:34","09:02","kernel/mentor/eval.go:1016 [trendline_between_boxes]","CODE",25),
 ("Ngày 3.2 FTGH FTGL","part2","09:47","09:54","kernel/mentor/mentor.go:596 [box_new_zone_after_escape]","CODE",26),
 ("Ngày 3.2 FTGH FTGL","part2","10:56","11:20","kernel/mentor/eval.go:1016 [trendline_between_boxes]","CODE",27),
 ("Ngày 3.3 Kinh nghiệm FTGH FTGL","part1","00:00","00:13","kernel/mentor/phl.go:99 [stop_ceiling_pts]","CODE",28),
 ("Ngày 3.3 Kinh nghiệm FTGH FTGL","part1","00:13","00:31","kernel/mentor/mentor.go:537 [touch_band_pts]","CODE",29),
 ("Ngày 3.3 Kinh nghiệm FTGH FTGL","part1","00:40","01:05","kernel/mentor/phl.go:99 [stop_placement]","CODE",30),
 ("Ngày 3.3 Kinh nghiệm FTGH FTGL","part1","01:09","01:20","kernel/mentor/phl.go:99 [stop_ceiling_pts]","CODE",31),
 ("Ngày 3.3 Kinh nghiệm FTGH FTGL","part1","01:57","02:24","kernel/mentor/phl.go:99 [stop_ceiling_pts]","CODE",32),
 ("Ngày 3.3 Kinh nghiệm FTGH FTGL","part1","05:07","05:35","kernel/mentor/eval.go:1030 [next_level_target]","CODE",33),
 ("Ngày 3.3 Kinh nghiệm FTGH FTGL","part1","06:02","06:10","kernel/mentor/eval.go:1030 [next_level_target]","CODE",34),
 ("Ngày 3.3 Kinh nghiệm FTGH FTGL","part1","06:42","06:49","kernel/mentor/eval.go:1030 [next_level_target]","CODE",35),
 ("Ngày 3.3 Kinh nghiệm FTGH FTGL","part1","07:23","07:46","kernel/mentor/eval.go:1088 [partial_1_1]","CODE",36),
 ("Ngày 3.3 Kinh nghiệm FTGH FTGL","part1","07:55","08:04","kernel/mentor/eval.go:1501 [trendline_is_target]","CODE",37),
 ("Ngày 3.3 Kinh nghiệm FTGH FTGL","part1","08:12","08:56","","NOT-IN-BOT",38),
 ("Ngày 3.4 BUY SELL TRIGGER","part1","00:58","01:30","kernel/mentor/htf_direction.go:53 [htf_agree]","CODE",39),
 ("Ngày 3.4 BUY SELL TRIGGER","part1","03:10","03:23","kernel/mentor/htf_direction.go:53 [htf_agree]","CODE",40),
 ("Ngày 3.4 BUY SELL TRIGGER","part1","03:52","04:46","kernel/mentor/filters.go:144 [trigger_break_high]","CODE",41),
 ("Ngày 3.4 BUY SELL TRIGGER","part1","05:52","06:07","kernel/mentor/filters.go:144 [trigger_break_high]","CODE",42),
 ("Ngày 3.4 BUY SELL TRIGGER","part1","07:19","07:28","kernel/mentor/filters.go:144 [trigger_break_high]","CODE",43),
 ("Ngày 3.4 BUY SELL TRIGGER","part1","07:37","08:25","kernel/mentor/filters.go:145 [trigger_break_low]","CODE",44),
 ("Ngày 3.4 BUY SELL TRIGGER","part1","11:48","12:25","kernel/mentor/filters.go:144 [trigger_break_high]","CODE",45),
 ("Ngày 3.4 BUY SELL TRIGGER","part1","13:53","14:08","kernel/mentor/htf_direction.go:53 [htf_agree]","CODE",46),
 ("Ngày 3.4 BUY SELL TRIGGER","part1","14:41","14:51","kernel/mentor/htf_direction.go:53 [htf_agree]","CODE",47),
 ("Ngày 3.4 BUY SELL TRIGGER","part1","16:38","17:17","kernel/mentor/filters.go:144 [trigger_break_high]","CODE",48),
 ("Ngày 3.4 BUY SELL TRIGGER","part1","17:39","18:18","kernel/mentor/eval.go:2159 [level_entries_need_trigger_side]","OWNER-ORDER",49),
 ("Ngày 3.4 BUY SELL TRIGGER","part2","00:14","00:58","kernel/mentor/isb_box.go:74 [isb_box_escape]","CODE",50),
 ("Ngày 3.4 BUY SELL TRIGGER","part2","05:37","05:53","kernel/mentor/isb_box.go:74 [isb_box_escape]","CODE",51),
 ("Ngày 3.4 BUY SELL TRIGGER","part2","09:03","10:35","kernel/mentor/isb_box.go:74 [isb_box_same_dir]","CODE",52),
 ("Ngày 3.4 BUY SELL TRIGGER","part2","11:30","12:25","kernel/mentor/isb_box.go:74 [isb_box_delete_flip]","CODE",53),
 ("Ngày 3.4 BUY SELL TRIGGER","part2","17:20","17:52","","NOT-IN-BOT",54),
 ("Ngày 3.4 BUY SELL TRIGGER","part2","20:51","21:24","kernel/mentor/eval.go:1193 [trigger_retest]","CODE",55),
 ("Ngày 3.4 BUY SELL TRIGGER","part3","01:44","02:55","kernel/mentor/eval.go:1016 [trendline_between_boxes]","CODE",56),
 ("Ngày 3.4 BUY SELL TRIGGER","part3","03:05","03:39","kernel/mentor/isb.go:72 [isb_stop_limit]","CODE",57),
 ("Ngày 3.4 BUY SELL TRIGGER","part3","07:38","08:27","kernel/mentor/box_trade.go:270 [confluence]","CODE",58),
 ("Ngày 3.4 BUY SELL TRIGGER","part3","10:14","10:27","kernel/mentor/eval.go:2161 [trigger_school]","CODE",59),
 ("Ngày 3.4 BUY SELL TRIGGER","part3","12:38","12:59","kernel/mentor/eval.go:2161 [trigger_school]","CODE",60),
]

def ref_for(r):
    _,_,_,_,ref,verdict,rownum = r
    if verdict == "CODE":   return f"CODE {ref} (row {rownum})"
    if verdict == "MISMATCH": return f"MISMATCH {ref} (row {rownum})"
    if verdict == "NOT-IN-BOT": return f"NOT-IN-BOT (row {rownum})"
    if verdict == "OWNER-ORDER": return f"OWNER-ORDER (row {rownum})"
    if verdict == "COURSE-SPLIT": return f"COURSE-SPLIT (row {rownum})"
    return "—"

idx = defaultdict(list)
for r in R:
    idx[(r[0], r[1])].append(r)

def annotate(folder, part):
    cues = parse(os.path.join(BASE, folder, part + ".srt"))
    rules = idx[(folder, part)]
    counts = {"CODE":0,"MISMATCH":0,"NOT-IN-BOT":0,"OWNER-ORDER":0,"COURSE-SPLIT":0,"—":0}
    nrule = 0
    out = []
    for mmss, text in cues:
        s = to_s(mmss); ref = "—"
        for r in rules:
            if to_s(r[2]) <= s <= to_s(r[3]):
                ref = ref_for(r); break
        if ref != "—": nrule += 1
        tag = ref.split(" ")[0] if ref != "—" else "—"
        counts[tag] += 1
        out.append(f"{mmss} | {text} | {ref}")
    header = (f"cues: {len(cues)}/{len(cues)} marked · rules: {nrule} · CODE: {counts['CODE']} · "
              f"MISMATCH: {counts['MISMATCH']} · NOT-IN-BOT: {counts['NOT-IN-BOT']} · "
              f"OWNER-ORDER: {counts['OWNER-ORDER']} · COURSE-SPLIT: {counts['COURSE-SPLIT']} · —: {counts['—']}")
    os.makedirs(os.path.join(OUT, folder), exist_ok=True)
    with open(os.path.join(OUT, folder, part + ".md"), "w", encoding="utf-8") as f:
        f.write(header + "\n")
        for line in out: f.write(line + "\n")
    print(f"{folder}/{part}: {header}")

for folder, part in [("Ngày 3.1 Go over ngày 2","part1"),
                     ("Ngày 3.2 FTGH FTGL","part1"), ("Ngày 3.2 FTGH FTGL","part2"),
                     ("Ngày 3.3 Kinh nghiệm FTGH FTGL","part1"),
                     ("Ngày 3.4 BUY SELL TRIGGER","part1"),
                     ("Ngày 3.4 BUY SELL TRIGGER","part2"),
                     ("Ngày 3.4 BUY SELL TRIGGER","part3")]:
    annotate(folder, part)
