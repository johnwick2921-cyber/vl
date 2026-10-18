#!/usr/bin/env python3
# DS-103 Phase 1b v2 — annotate every srt cue of my slice (Ngày 3.1–3.4) with a
# REF DERIVED from my truth table (not keyword-mapped). Every cue inside a row's
# time range(s) gets that row's REF; every other cue gets "—".
import os, re

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
                txt.append(lines[i].strip())
                i += 1
            cues.append((mmss, ' '.join(txt)))
        else:
            i += 1
    return cues

def to_s(mmss):
    mm, ss = mmss.split(':')
    return int(mm) * 60 + int(ss)

# Each rule: (folder, part, start, end, ref, verdict, rownum)
# ref is the CODE <file>:<line> [id] for MATCH, or the MISMATCH target line, or '' for the
# verdict-only rows (NOT-IN-BOT / OWNER-ORDER / COURSE-SPLIT).
R = [
 # ---- Ngày 3.1 "Go over ngày 2" ----
 ("Ngày 3.1 Go over ngày 2","part1","00:26","00:59","","NOT-IN-BOT",1),
 ("Ngày 3.1 Go over ngày 2","part1","01:03","01:08","","NOT-IN-BOT",2),
 ("Ngày 3.1 Go over ngày 2","part1","01:31","03:20","kernel/mentor/isb.go:78 [isb_stop_limit]","CODE",3),
 ("Ngày 3.1 Go over ngày 2","part1","04:22","04:34","kernel/mentor/isb.go:182 [isb_stack_cancel]","COURSE-SPLIT",4),
 ("Ngày 3.1 Go over ngày 2","part1","04:50","05:12","","NOT-IN-BOT",5),
 ("Ngày 3.1 Go over ngày 2","part1","06:31","07:49","","NOT-IN-BOT",6),
 ("Ngày 3.1 Go over ngày 2","part1","09:41","10:09","","NOT-IN-BOT",7),
 ("Ngày 3.1 Go over ngày 2","part1","10:22","11:08","","NOT-IN-BOT",8),
 ("Ngày 3.1 Go over ngày 2","part1","11:34","12:23","","NOT-IN-BOT",9),
 ("Ngày 3.1 Go over ngày 2","part1","13:30","14:01","kernel/mentor/eval.go:1581 [exec_2m_after_30m]","MISMATCH",10),
 ("Ngày 3.1 Go over ngày 2","part1","14:18","14:38","","NOT-IN-BOT",11),
 # ---- Ngày 3.2 "FTGH FTGL" part1 ----
 ("Ngày 3.2 FTGH FTGL","part1","00:00","00:43","","NOT-IN-BOT",12),
 ("Ngày 3.2 FTGH FTGL","part1","03:21","04:23","kernel/mentor/box.go:68 [ftgh_top_bottom]","CODE",13),
 ("Ngày 3.2 FTGH FTGL","part1","04:29","04:53","kernel/mentor/box_trade.go:212 [box_return]","CODE",14),
 ("Ngày 3.2 FTGH FTGL","part1","06:00","06:13","","NOT-IN-BOT",15),
 ("Ngày 3.2 FTGH FTGL","part1","07:07","08:38","kernel/mentor/box_trade.go:189 [close_inside_cancel]","CODE",16),
 ("Ngày 3.2 FTGH FTGL","part1","08:47","09:04","kernel/mentor/eval.go:1965 [trigger_side]","CODE",17),
 ("Ngày 3.2 FTGH FTGL","part1","09:50","10:08","kernel/mentor/trendline.go:280 [trendline_valid]","CODE",18),
 ("Ngày 3.2 FTGH FTGL","part1","11:08","11:14","kernel/mentor/mentor.go:535 [key_level_prune_pts]","CODE",19),
 ("Ngày 3.2 FTGH FTGL","part1","13:31","13:42","kernel/mentor/isb.go:125 [isb_max_stop]","CODE",20),
 ("Ngày 3.2 FTGH FTGL","part1","15:30","16:37","","NOT-IN-BOT",21),
 ("Ngày 3.2 FTGH FTGL","part1","18:02","18:26","kernel/mentor/box.go:69 [ftgl_bottom]","CODE",22),
 ("Ngày 3.2 FTGH FTGL","part1","21:04","21:39","kernel/mentor/box_trade.go:212 [box_return]","CODE",23),
 ("Ngày 3.2 FTGH FTGL","part1","22:12","23:08","kernel/mentor/box.go:828 [near_box_refusal]","CODE",24),
 # ---- Ngày 3.2 part2 ----
 ("Ngày 3.2 FTGH FTGL","part2","00:24","00:28","","NOT-IN-BOT",25),
 ("Ngày 3.2 FTGH FTGL","part2","00:38","01:06","kernel/mentor/trendline.go:280 [trendline_valid]","CODE",26),
 ("Ngày 3.2 FTGH FTGL","part2","03:34","04:06","kernel/mentor/trendline.go:280 [trendline_3rd_touch]","CODE",27),
 ("Ngày 3.2 FTGH FTGL","part2","05:43","05:59","kernel/mentor/trendline.go:280 [trendline_valid]","CODE",28),
 ("Ngày 3.2 FTGH FTGL","part2","06:25","07:28","kernel/mentor/box_trade.go:189 [close_inside_cancel]","CODE",29),
 ("Ngày 3.2 FTGH FTGL","part2","07:28","08:04","kernel/mentor/box_trade.go:212 [box_return]","CODE",30),
 ("Ngày 3.2 FTGH FTGL","part2","08:34","09:02","kernel/mentor/eval.go:1016 [trendline_between_boxes]","CODE",31),
 ("Ngày 3.2 FTGH FTGL","part2","09:47","09:54","kernel/mentor/mentor.go:596 [box_new_zone_after_escape]","CODE",32),
 ("Ngày 3.2 FTGH FTGL","part2","10:56","11:20","kernel/mentor/eval.go:1016 [trendline_between_boxes]","CODE",33),
 # ---- Ngày 3.3 ----
 ("Ngày 3.3 Kinh nghiệm FTGH FTGL","part1","00:00","00:13","kernel/mentor/phl.go:99 [stop_ceiling_pts]","CODE",34),
 ("Ngày 3.3 Kinh nghiệm FTGH FTGL","part1","00:13","00:31","kernel/mentor/mentor.go:537 [touch_band_pts]","CODE",35),
 ("Ngày 3.3 Kinh nghiệm FTGH FTGL","part1","00:40","01:05","kernel/mentor/phl.go:99 [stop_placement]","CODE",36),
 ("Ngày 3.3 Kinh nghiệm FTGH FTGL","part1","01:09","02:24","kernel/mentor/phl.go:99 [stop_ceiling_pts]","CODE",37),
 ("Ngày 3.3 Kinh nghiệm FTGH FTGL","part1","03:48","05:35","kernel/mentor/eval.go:1501 [trendline_is_target]","CODE",38),
 ("Ngày 3.3 Kinh nghiệm FTGH FTGL","part1","06:33","07:15","kernel/mentor/eval.go:1501 [trendline_is_target]","CODE",39),
 ("Ngày 3.3 Kinh nghiệm FTGH FTGL","part1","07:23","07:46","kernel/mentor/eval.go:1084 [runner_partial_line]","CODE",40),
 ("Ngày 3.3 Kinh nghiệm FTGH FTGL","part1","07:55","08:04","kernel/mentor/eval.go:1501 [trendline_is_target]","CODE",41),
 ("Ngày 3.3 Kinh nghiệm FTGH FTGL","part1","08:12","08:56","","NOT-IN-BOT",42),
 # ---- Ngày 3.4 part1 ----
 ("Ngày 3.4 BUY SELL TRIGGER","part1","00:58","01:30","kernel/mentor/eval.go:1965 [trigger_side]","CODE",43),
 ("Ngày 3.4 BUY SELL TRIGGER","part1","03:10","03:23","kernel/mentor/eval.go:1965 [trigger_side]","CODE",44),
 ("Ngày 3.4 BUY SELL TRIGGER","part1","03:52","06:07","kernel/mentor/eval.go:1965 [trigger_side]","CODE",45),
 ("Ngày 3.4 BUY SELL TRIGGER","part1","07:37","08:25","kernel/mentor/eval.go:1965 [trigger_side]","CODE",46),
 ("Ngày 3.4 BUY SELL TRIGGER","part1","11:48","13:30","kernel/mentor/eval.go:1965 [trigger_side]","CODE",47),
 ("Ngày 3.4 BUY SELL TRIGGER","part1","13:53","14:08","kernel/mentor/eval.go:1965 [trigger_side]","CODE",48),
 ("Ngày 3.4 BUY SELL TRIGGER","part1","16:38","17:17","kernel/mentor/eval.go:1965 [trigger_side]","CODE",49),
 ("Ngày 3.4 BUY SELL TRIGGER","part1","17:39","18:18","kernel/mentor/eval.go:2159 [level_entries_need_trigger_side]","OWNER-ORDER",50),
 # ---- Ngày 3.4 part2 ----
 ("Ngày 3.4 BUY SELL TRIGGER","part2","00:14","00:58","kernel/mentor/isb_box.go:1 [isb_box]","CODE",51),
 ("Ngày 3.4 BUY SELL TRIGGER","part2","02:33","02:35","kernel/mentor/isb_box.go:1 [isb_box]","CODE",52),
 ("Ngày 3.4 BUY SELL TRIGGER","part2","05:37","05:53","kernel/mentor/isb_box.go:1 [isb_box_escape]","CODE",53),
 ("Ngày 3.4 BUY SELL TRIGGER","part2","06:05","07:19","kernel/mentor/eval.go:1965 [trigger_side]","CODE",54),
 ("Ngày 3.4 BUY SELL TRIGGER","part2","07:27","10:35","kernel/mentor/isb_box.go:1 [isb_box_same_dir]","CODE",55),
 ("Ngày 3.4 BUY SELL TRIGGER","part2","11:30","13:00","kernel/mentor/isb_box.go:1 [isb_box_delete_flip]","CODE",56),
 ("Ngày 3.4 BUY SELL TRIGGER","part2","15:40","16:05","kernel/mentor/eval.go:1965 [trigger_side]","CODE",57),
 ("Ngày 3.4 BUY SELL TRIGGER","part2","17:20","17:52","","NOT-IN-BOT",58),
 ("Ngày 3.4 BUY SELL TRIGGER","part2","20:51","22:08","kernel/mentor/mentor.go:74 [kind_trigger_retest]","CODE",59),
 # ---- Ngày 3.4 part3 ----
 ("Ngày 3.4 BUY SELL TRIGGER","part3","01:44","02:55","kernel/mentor/eval.go:1016 [trendline_between_boxes]","CODE",60),
 ("Ngày 3.4 BUY SELL TRIGGER","part3","03:05","03:39","kernel/mentor/isb.go:78 [isb_stop_limit]","CODE",61),
 ("Ngày 3.4 BUY SELL TRIGGER","part3","05:20","08:27","kernel/mentor/htf_direction.go:1 [htf_agree]","CODE",62),
 ("Ngày 3.4 BUY SELL TRIGGER","part3","10:14","12:59","kernel/mentor/school_one.go:53 [trigger_school]","CODE",63),
]

def ref_for(rule):
    folder, part, start, end, ref, verdict, rownum = rule
    if verdict == "CODE":
        return f"CODE {ref} (row {rownum})"
    if verdict == "MISMATCH":
        return f"MISMATCH {ref} (row {rownum})"
    if verdict == "NOT-IN-BOT":
        return f"NOT-IN-BOT (row {rownum})"
    if verdict == "OWNER-ORDER":
        return f"OWNER-ORDER (row {rownum})"
    if verdict == "COURSE-SPLIT":
        return f"COURSE-SPLIT (row {rownum})"
    return "—"

# Build per-(folder,part) rule index; ranges may overlap → earliest row wins (list order).
from collections import defaultdict
idx = defaultdict(list)
for r in R:
    idx[(r[0], r[1])].append(r)

def annotate(folder, part):
    path = os.path.join(BASE, folder, part + ".srt")
    cues = parse(path)
    rules = idx[(folder, part)]
    out = []
    nrule = 0
    counts = {"CODE":0,"MISMATCH":0,"NOT-IN-BOT":0,"OWNER-ORDER":0,"COURSE-SPLIT":0,"—":0}
    for mmss, text in cues:
        s = to_s(mmss)
        ref = "—"
        for r in rules:
            if to_s(r[2]) <= s <= to_s(r[3]):
                ref = ref_for(r)
                break
        if ref != "—":
            nrule += 1
        tag = ref.split(" ")[0] if ref != "—" else "—"
        if tag in ("CODE","MISMATCH"):
            tag = {"CODE":"CODE","MISMATCH":"MISMATCH"}[tag]
        counts[tag] = counts.get(tag, 0) + 1
        out.append(f"{mmss} | {text} | {ref}")
    header = (f"cues: {len(cues)}/{len(cues)} marked · rules: {nrule} · "
              f"CODE: {counts.get('CODE',0)} · MISMATCH: {counts.get('MISMATCH',0)} · "
              f"NOT-IN-BOT: {counts.get('NOT-IN-BOT',0)} · OWNER-ORDER: {counts.get('OWNER-ORDER',0)} · "
              f"COURSE-SPLIT: {counts.get('COURSE-SPLIT',0)} · —: {counts.get('—',0)}")
    os.makedirs(os.path.join(OUT, folder), exist_ok=True)
    with open(os.path.join(OUT, folder, part + ".md"), "w", encoding="utf-8") as f:
        f.write(header + "\n")
        for line in out:
            f.write(line + "\n")
    print(f"{folder}/{part}: {header}")

for folder, part in [("Ngày 3.1 Go over ngày 2","part1"),
                     ("Ngày 3.2 FTGH FTGL","part1"), ("Ngày 3.2 FTGH FTGL","part2"),
                     ("Ngày 3.3 Kinh nghiệm FTGH FTGL","part1"),
                     ("Ngày 3.4 BUY SELL TRIGGER","part1"),
                     ("Ngày 3.4 BUY SELL TRIGGER","part2"),
                     ("Ngày 3.4 BUY SELL TRIGGER","part3")]:
    annotate(folder, part)
