#!/usr/bin/env python3
# DS-107 Phase 1b: annotate EVERY srt cue with a REF. Nothing skipped.
import os, re, sys

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
            hh, mm, ss = int(m.group(1)), int(m.group(2)), int(m.group(3))
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

# Each file: list of (mmss, ref); a cue gets the ref of the last segment whose start <= cue start.
# ref is one of: CODE f:l [id] · MISMATCH f:l → row · NOT-IN-BOT → row · OWNER-ORDER → row ·
#                COURSE-SPLIT → row · SILENT → row · "—"
FILES = [
 ("Chị Tany nói về cách đánh retracement", "part1.srt", [
   ("00:00", "NOT-IN-BOT → 13.1"),
   ("00:40", "NOT-IN-BOT → 13.3"),
   ("01:28", "OWNER-ORDER → 13.5"),
   ("01:35", "—"),
   ("02:25", "NOT-IN-BOT → 13.4"),
   ("02:44", "—"),
   ("02:58", "CODE kernel/mentor/isb.go:72 [never-no-stop-loss]"),
 ]),
 ("Ví dụ đánh Retracement trong range", "part1.srt", [
   ("00:00", "CODE kernel/mentor/mentor.go:497 [flag_wall_target]"),
   ("01:05", "CODE kernel/mentor/mentor.go:555 [loc_trigger_filter]"),
   ("01:43", "CODE kernel/mentor/phl.go:63 [phl_entry_buffer_pts]"),
   ("01:51", "CODE kernel/mentor/mentor.go:555 [loc_trigger_filter]"),
   ("02:37", "—"),
   ("03:15", "—"),
   ("03:42", "NOT-IN-BOT → 14.4"),
   ("04:00", "—"),
   ("05:25", "NOT-IN-BOT → 14.5"),
   ("06:00", "—"),
 ]),
 ("Kinh nghiệm khi vô lệnh bị quét xong FOMO", "part1.srt", [
   ("00:00", "CODE kernel/mentor/mentor.go:488 [leg_reset_on_wave_break]"),
   ("00:23", "—"),
   ("00:30", "CODE kernel/mentor/mentor.go:580 [leg_budget_enabled]"),
   ("01:35", "—"),
   ("02:37", "CODE kernel/mentor/mentor.go:580 [leg_budget_enabled]"),
   ("02:53", "—"),
   ("04:48", "CODE kernel/mentor/mentor.go:595 [leg_reset_on_wave_break]"),
   ("05:11", "—"),
   ("06:00", "CODE kernel/mentor/mentor.go:580 [leg_budget_enabled]"),
   ("06:10", "—"),
   ("06:20", "CODE kernel/mentor/mentor.go:595 [leg_reset_on_wave_break]"),
   ("06:40", "—"),
 ]),
 ("VWAP Stddiv", "part1.srt", [
   ("00:00", "—"),
   ("00:27", "NOT-IN-BOT → 16.1"),
   ("02:55", "NOT-IN-BOT → 16.2"),
   ("03:58", "NOT-IN-BOT → 16.1"),
   ("04:45", "CODE kernel/mentor/orb.go:17 [orb_gate_enabled]"),
   ("05:41", "NOT-IN-BOT → 16.4"),
   ("05:47", "NOT-IN-BOT → 16.5"),
   ("06:20", "NOT-IN-BOT → 16.5"),
   ("07:06", "NOT-IN-BOT → 16.6"),
   ("08:15", "NOT-IN-BOT → 16.7"),
   ("09:45", "NOT-IN-BOT → 16.6"),
   ("10:11", "NOT-IN-BOT → 16.8"),
   ("11:29", "NOT-IN-BOT → 16.8"),
   ("13:33", "CODE kernel/mentor/mentor.go:576 [exec_2m_after_30m]"),
   ("13:39", "NOT-IN-BOT → 16.8"),
   ("14:55", "NOT-IN-BOT → 16.9"),
   ("15:10", "—"),
   ("16:43", "CODE kernel/mentor/orb.go:17 [orb_gate_enabled]"),
   ("17:46", "CODE kernel/mentor/phl.go:63 [phl_entry_buffer_pts]"),
   ("18:26", "—"),
   ("20:12", "NOT-IN-BOT → 16.1"),
   ("20:38", "NOT-IN-BOT → 16.8"),
   ("21:38", "NOT-IN-BOT → 16.9"),
   ("23:00", "—"),
 ]),
 ("Anh Levy chia sẻ kinh nghiệm", "part1.srt", [
   ("00:00", "NOT-IN-BOT → 17.1"),
   ("00:23", "CODE kernel/mentor/mentor.go:530 [key_level_prune_pts]"),
   ("01:38", "CODE kernel/mentor/mentor.go:530 [key_level_prune_pts]"),
   ("03:55", "NOT-IN-BOT → 17.3"),
   ("05:01", "CODE kernel/mentor/mentor.go:592 [trendline_is_target]"),
   ("06:22", "CODE kernel/mentor/trendline.go:21"),
   ("07:27", "NOT-IN-BOT → 17.7"),
   ("09:03", "NOT-IN-BOT → 17.7"),
   ("09:19", "—"),
   ("10:31", "COURSE-SPLIT → 17.8"),
   ("11:16", "CODE kernel/mentor/mentor.go:529 [key_level_rth_only]"),
   ("12:14", "CODE kernel/mentor/mentor.go:530 [key_level_prune_pts]"),
   ("13:36", "NOT-IN-BOT → 17.9"),
   ("14:31", "—"),
   ("15:22", "CODE kernel/mentor/mentor.go:530 [key_level_prune_pts]"),
   ("16:39", "CODE kernel/mentor/eval.go:531 [level_invalid_first_touch]"),
   ("18:20", "CODE kernel/mentor/mentor.go:555 [loc_trigger_filter]"),
   ("18:38", "CODE kernel/mentor/mentor.go:537 [touch_band_pts]"),
   ("19:00", "CODE kernel/mentor/phl.go:63 [phl_entry_buffer_pts]"),
   ("20:28", "—"),
 ]),
 ("Anh Levy chia sẻ kinh nghiệm", "part2.srt", [
   ("00:00", "—"),
   ("00:26", "CODE kernel/mentor/course_exits.go:40 [course_exits]"),
   ("03:12", "CODE kernel/mentor/mentor.go:592 [trendline_is_target]"),
   ("04:09", "CODE kernel/mentor/eval.go:531 [level_invalid_first_touch]"),
   ("06:54", "CODE kernel/mentor/eval.go:531 [level_invalid_first_touch]"),
   ("08:44", "CODE kernel/mentor/eval.go:531 [level_invalid_first_touch]"),
   ("09:44", "CODE kernel/mentor/mentor.go:531 [ema_period_34]"),
   ("11:36", "CODE kernel/mentor/mentor.go:531 [ema_period_34]"),
   ("13:46", "CODE kernel/mentor/mentor.go:531 [ema_period_34]"),
   ("14:37", "CODE kernel/mentor/isb.go:72 [isb_buffer_pts]"),
   ("15:00", "CODE kernel/mentor/phl.go:63 [phl_entry_buffer_pts]"),
   ("15:35", "CODE kernel/mentor/mentor.go:530 [key_level_prune_pts]"),
   ("15:56", "NOT-IN-BOT → 17.18"),
   ("16:50", "—"),
   ("17:30", "—"),
   ("18:03", "CODE kernel/mentor/trendline.go:21"),
   ("19:42", "CODE kernel/mentor/mentor.go:530 [key_level_prune_pts]"),
   ("21:16", "—"),
   ("22:33", "CODE kernel/mentor/mentor.go:530 [key_level_prune_pts]"),
   ("24:30", "NOT-IN-BOT → 17.20"),
 ]),
 ("Anh Levy chia sẻ kinh nghiệm", "part3.srt", [
   ("00:00", "—"),
   ("00:15", "CODE kernel/mentor/isb.go:72 [isb_buffer_pts]"),
   ("01:30", "CODE kernel/mentor/mentor.go:531 [ema_period_34]"),
   ("01:51", "NOT-IN-BOT → 17.20"),
   ("04:23", "—"),
   ("04:57", "CODE kernel/mentor/mentor.go:531 [ema_period_34]"),
   ("05:51", "—"),
   ("06:28", "CODE kernel/mentor/mentor.go:531 [ema_period_34]"),
   ("07:09", "—"),
   ("08:41", "CODE kernel/mentor/mentor.go:531 [ema_period_34]"),
   ("09:48", "—"),
   ("12:57", "CODE kernel/mentor/mentor.go:531 [ema_period_34]"),
   ("14:08", "—"),
   ("15:29", "CODE kernel/mentor/isb.go:72 [isb_buffer_pts]"),
   ("16:19", "CODE kernel/mentor/mentor.go:531 [ema_period_34]"),
   ("17:01", "NOT-IN-BOT → 17.19"),
   ("17:18", "CODE kernel/mentor/isb.go:72 [isb_buffer_pts]"),
   ("18:13", "NOT-IN-BOT → 17.21"),
   ("19:29", "—"),
   ("20:02", "CODE kernel/mentor/mentor.go:566 [day_off_recheck]"),
   ("21:10", "—"),
 ]),
 ("Luật Topstep", "part1.srt", [
   ("00:00", "—"),
   ("01:00", "NOT-IN-BOT → 18.1"),
   ("01:21", "—"),
   ("02:00", "OWNER-ORDER → 18.2"),
   ("02:25", "—"),
   ("05:00", "NOT-IN-BOT → 18.3"),
   ("07:28", "—"),
   ("07:56", "NOT-IN-BOT → 18.4"),
   ("09:19", "NOT-IN-BOT → 18.5"),
   ("11:40", "—"),
   ("16:59", "NOT-IN-BOT → 18.7"),
   ("17:15", "NOT-IN-BOT → 18.1"),
   ("18:10", "NOT-IN-BOT → 18.6"),
   ("18:34", "—"),
   ("19:32", "OWNER-ORDER → 18.8"),
   ("19:45", "—"),
   ("20:50", "NOT-IN-BOT → 18.5"),
   ("21:44", "—"),
 ]),
 ("Luật Topstep", "part2.srt", [
   ("00:00", "NOT-IN-BOT → 18.7"),
   ("02:00", "NOT-IN-BOT → 18.1"),
   ("05:02", "NOT-IN-BOT → 18.7"),
   ("08:30", "—"),
   ("10:59", "—"),
   ("12:52", "NOT-IN-BOT → 18.1"),
   ("14:46", "—"),
   ("16:38", "—"),
   ("20:30", "—"),
   ("21:51", "NOT-IN-BOT → 18.7"),
   ("24:00", "—"),
 ]),
 ("Luật Topstep", "part3.srt", [
   ("00:00", "—"),
   ("03:03", "NOT-IN-BOT → 18.4"),
   ("05:50", "NOT-IN-BOT → 18.1"),
   ("07:52", "—"),
   ("08:25", "NOT-IN-BOT → 18.9"),
   ("10:56", "NOT-IN-BOT → 18.9"),
   ("12:33", "NOT-IN-BOT → 18.12"),
   ("13:34", "—"),
   ("16:00", "NOT-IN-BOT → 18.9"),
   ("18:00", "—"),
   ("20:13", "CONTEXT → 18.10"),
   ("20:28", "—"),
   ("21:00", "NOT-IN-BOT → 18.1"),
   ("23:22", "—"),
 ]),
 ("Luật Topstep", "part4.srt", [
   ("00:00", "—"),
   ("01:14", "NOT-IN-BOT → 18.11"),
   ("01:34", "—"),
 ]),
]

def main():
    for folder, part, segs in FILES:
        path = os.path.join(BASE, folder, part)
        cues = parse(path)
        segs_sorted = sorted(segs, key=lambda s: to_s(s[0]))
        out = []
        ref = "—"
        j = 0
        for mmss, text in cues:
            while j < len(segs_sorted) and to_s(segs_sorted[j][0]) <= to_s(mmss):
                ref = segs_sorted[j][1]
                j += 1
            out.append((mmss, text, ref))
        n = len(cues)
        rules = sum(1 for _,_,r in out if r != "—")
        code = sum(1 for _,_,r in out if r.startswith("CODE"))
        mismatch = sum(1 for _,_,r in out if r.startswith("MISMATCH"))
        nib = sum(1 for _,_,r in out if r.startswith("NOT-IN-BOT"))
        dash = sum(1 for _,_,r in out if r == "—")
        other = n - rules
        os.makedirs(os.path.join(OUT, folder), exist_ok=True)
        outpath = os.path.join(OUT, folder, part.replace(".srt", ".md"))
        with open(outpath, 'w', encoding='utf-8') as f:
            f.write(f"cues: {n}/{n} marked · rules: {rules} · CODE: {code} · MISMATCH: {mismatch} · NOT-IN-BOT: {nib} · —: {dash}\n\n")
            for mmss, text, r in out:
                f.write(f"{mmss} | {text} | {r}\n")
        print(f"{folder}/{part}: cues {n}/{n} rules {rules} code {code} nib {nib} dash {dash}")

if __name__ == "__main__":
    main()
