#!/usr/bin/env python3
# DS-107 Phase 1b v2 — annotated files DERIVED from the truth-table rows (exact cue ranges).
# Only actual trading instructions are rules; narration/questions/repeats stay "—".
import os, re

BASE = "/home/hoang/mm-course/extra/transcripts"
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

def s(mmss):
    m, s2 = mmss.split(':')
    return int(m) * 60 + int(s2)

# rows: (folder, part, [(start_mmss, end_mmss, REF, rowid)])
ROWS = [
 ("Chị Tany nói về cách đánh retracement", "part1.srt", [
   ("00:00","00:40","NOT-IN-BOT → 13.1","13.1"),
   ("00:49","01:07","NOT-IN-BOT → 13.3","13.3"),
   ("01:10","01:14","NOT-IN-BOT → 13.3","13.3"),
   ("01:22","01:28","NOT-IN-BOT → 13.4","13.4"),
   ("01:28","01:33","OWNER-ORDER → 13.5","13.5"),
   ("02:25","02:33","NOT-IN-BOT → 13.4","13.4"),
   ("02:58","03:20","CODE kernel/mentor/isb.go:74","13.7"),
 ]),
 ("Ví dụ đánh Retracement trong range", "part1.srt", [
   ("00:00","01:05","CODE kernel/mentor/mentor.go:594 [flag_wall_target]","14.1"),
   ("01:05","01:11","CODE kernel/mentor/mentor.go:555 [loc_trigger_filter]","14.2"),
   ("01:31","01:39","CODE kernel/mentor/mentor.go:555 [loc_trigger_filter]","14.2"),
   ("01:43","01:49","CODE kernel/mentor/phl.go:63 [phl_entry_buffer_pts]","14.3"),
   ("02:39","03:01","CODE kernel/mentor/mentor.go:555 [loc_trigger_filter]","14.2"),
   ("03:42","03:54","NOT-IN-BOT → 14.4","14.4"),
   ("05:25","06:05","NOT-IN-BOT → 14.5","14.5"),
 ]),
 ("Kinh nghiệm khi vô lệnh bị quét xong FOMO", "part1.srt", [
   ("00:08","00:34","CODE kernel/mentor/mentor.go:595 [leg_reset_on_wave_break]","15.2"),
   ("00:36","00:52","CODE kernel/mentor/mentor.go:580 [leg_budget_enabled]","15.3"),
   ("01:20","01:30","CODE kernel/mentor/mentor.go:580 [leg_budget_enabled]","15.3"),
   ("02:37","02:48","CODE kernel/mentor/mentor.go:580 [leg_budget_enabled]","15.4"),
   ("04:48","05:10","CODE kernel/mentor/mentor.go:595 [leg_reset_on_wave_break]","15.5"),
   ("06:00","06:05","CODE kernel/mentor/mentor.go:580 [leg_budget_enabled]","15.7"),
   ("06:20","07:20","CODE kernel/mentor/mentor.go:595 [leg_reset_on_wave_break]","15.6"),
 ]),
 ("VWAP Stddiv", "part1.srt", [
   ("00:27","02:30","NOT-IN-BOT → 16.1","16.1"),
   ("02:55","03:56","NOT-IN-BOT → 16.2","16.2"),
   ("05:16","05:40","CODE kernel/mentor/mentor.go:572 [orb_gate_enabled]","16.3"),
   ("05:41","05:47","NOT-IN-BOT → 16.4","16.4"),
   ("05:47","06:22","NOT-IN-BOT → 16.5","16.5"),
   ("07:06","08:15","NOT-IN-BOT → 16.6","16.6"),
   ("08:15","09:45","NOT-IN-BOT → 16.7","16.7"),
   ("10:11","11:08","NOT-IN-BOT → 16.8","16.8"),
   ("13:33","13:35","CODE kernel/mentor/mentor.go:576 [exec_2m_after_30m]","16.10"),
   ("14:55","15:07","NOT-IN-BOT → 16.9","16.9"),
   ("17:46","17:55","CODE kernel/mentor/phl.go:63 [phl_entry_buffer_pts]","16.11"),
 ]),
 ("Anh Levy chia sẻ kinh nghiệm", "part1.srt", [
   ("00:00","00:20","NOT-IN-BOT → 17.1","17.1"),
   ("00:23","01:25","CODE kernel/mentor/mentor.go:528 [key_level_tf_minutes]","17.2"),
   ("03:55","04:07","NOT-IN-BOT → 17.3","17.3"),
   ("05:01","05:16","CODE kernel/mentor/mentor.go:592 [trendline_is_target]","17.4"),
   ("06:22","07:30","CODE kernel/mentor/trendline.go:123 [swing-pair]","17.5"),
   ("06:28","06:41","CODE kernel/mentor/trendline.go:123 [swing-pair]","17.6"),
   ("07:59","09:00","NOT-IN-BOT → 17.7","17.7"),
   ("10:31","10:48","COURSE-SPLIT → 17.8","17.8"),
   ("16:39","17:56","CODE kernel/mentor/eval.go:531 [level_invalid_first_touch]","17.10"),
   ("18:20","18:38","CODE kernel/mentor/mentor.go:555 [loc_trigger_filter]","17.12"),
   ("18:38","18:50","CODE kernel/mentor/mentor.go:537 [touch_band_pts]","17.13"),
 ]),
 ("Anh Levy chia sẻ kinh nghiệm", "part2.srt", [
   ("00:26","03:31","CODE kernel/mentor/eval.go:2638 [runnerPartialLine]","17.14"),
   ("08:44","09:16","CODE kernel/mentor/eval.go:531 [level_invalid_first_touch]","17.11"),
   ("09:44","12:29","CODE kernel/mentor/mentor.go:531 [ema_period_34]","17.15"),
   ("14:37","15:00","CODE kernel/mentor/isb.go:72 [isb_buffer_pts]","17.16"),
   ("15:00","15:08","CODE kernel/mentor/phl.go:63 [phl_entry_buffer_pts]","17.16"),
   ("15:35","15:58","CODE kernel/mentor/mentor.go:530 [key_level_prune_pts]","17.17"),
   ("15:56","16:24","NOT-IN-BOT → 17.18","17.18"),
 ]),
 ("Anh Levy chia sẻ kinh nghiệm", "part3.srt", [
   ("01:51","02:44","NOT-IN-BOT → 17.20","17.20"),
   ("17:01","17:11","NOT-IN-BOT → 17.19","17.19"),
   ("18:13","19:30","NOT-IN-BOT → 17.21","17.21"),
   ("20:02","21:04","CODE kernel/mentor/mentor.go:566 [day_off_recheck]","17.22"),
 ]),
 ("Luật Topstep", "part1.srt", [
   ("01:00","01:21","NOT-IN-BOT → 18.1","18.1"),
   ("02:00","02:25","OWNER-ORDER → 18.2","18.2"),
   ("05:14","07:28","NOT-IN-BOT → 18.3","18.3"),
   ("06:47","07:08","CODE kernel/mentor/isb.go:72","18.13"),
   ("07:56","08:47","NOT-IN-BOT → 18.4","18.4"),
   ("09:19","11:40","NOT-IN-BOT → 18.5","18.5"),
   ("16:59","17:15","NOT-IN-BOT → 18.7","18.7"),
   ("17:15","18:10","NOT-IN-BOT → 18.1","18.1"),
   ("18:10","18:34","NOT-IN-BOT → 18.6","18.6"),
   ("19:32","19:45","OWNER-ORDER → 18.8","18.8"),
   ("20:50","20:57","NOT-IN-BOT → 18.5","18.5"),
 ]),
 ("Luật Topstep", "part2.srt", [
   ("00:00","00:56","NOT-IN-BOT → 18.7","18.7"),
   ("02:00","04:06","NOT-IN-BOT → 18.1","18.1"),
   ("05:02","05:32","NOT-IN-BOT → 18.7","18.7"),
   ("14:46","15:02","NOT-IN-BOT → 18.1","18.1"),
   ("21:51","22:30","NOT-IN-BOT → 18.7","18.7"),
 ]),
 ("Luật Topstep", "part3.srt", [
   ("03:03","03:40","NOT-IN-BOT → 18.4","18.4"),
   ("05:50","06:15","NOT-IN-BOT → 18.1","18.1"),
   ("08:25","11:10","NOT-IN-BOT → 18.9","18.9"),
   ("12:33","12:59","NOT-IN-BOT → 18.12","18.12"),
   ("20:13","20:24","CONTEXT → 18.10","18.10"),
   ("21:00","22:00","NOT-IN-BOT → 18.1","18.1"),
 ]),
 ("Luật Topstep", "part4.srt", [
   ("01:14","01:34","NOT-IN-BOT → 18.11","18.11"),
 ]),
]

def main():
    for folder, part, rows in ROWS:
        cues = parse(os.path.join(BASE, folder, part))
        ref = {}
        for (a, b, r, row) in rows:
            for mmss, _ in cues:
                if s(a) <= s(mmss) < s(b):
                    ref[mmss] = (r, row)
        n = len(cues)
        rule_cues = sum(1 for mmss, _ in cues if mmss in ref)
        code = sum(1 for mmss, _ in cues if mmss in ref and ref[mmss][0].startswith("CODE"))
        mism = sum(1 for mmss, _ in cues if mmss in ref and ref[mmss][0].startswith("MISMATCH"))
        nib = sum(1 for mmss, _ in cues if mmss in ref and ref[mmss][0].startswith("NOT-IN-BOT"))
        oo = sum(1 for mmss, _ in cues if mmss in ref and ref[mmss][0].startswith("OWNER-ORDER"))
        spl = sum(1 for mmss, _ in cues if mmss in ref and ref[mmss][0].startswith(("COURSE-SPLIT","CONTEXT")))
        dash = n - rule_cues
        rows_n = len(rows)
        os.makedirs(os.path.join(OUT, folder), exist_ok=True)
        op = os.path.join(OUT, folder, part.replace(".srt", ".md"))
        with open(op, 'w', encoding='utf-8') as f:
            f.write(f"cues: {n}/{n} · rule cues: {rule_cues} · rows: {rows_n} · CODE: {code} · MISMATCH: {mism} · NOT-IN-BOT: {nib} · OWNER-ORDER: {oo} · SPLIT: {spl} · —: {dash}\n\n")
            for mmss, text in cues:
                if mmss in ref:
                    r, row = ref[mmss]
                    f.write(f"{mmss} | {text} | {r} (row {row})\n")
                else:
                    f.write(f"{mmss} | {text} | —\n")
        print(f"{folder}/{part}: cues {n}/{n} rule_cues {rule_cues} rows {rows_n} CODE {code} NIB {nib} OO {oo} SPLIT {spl} dash {dash}")

if __name__ == "__main__":
    main()
