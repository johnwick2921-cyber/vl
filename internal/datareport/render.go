package datareport

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"vl/kernel"
)

const (
	// partMaxRunes keeps each message under the owner-push text cap (600 runes;
	// internal/ownerpush.Scrub truncates past it). The margin carries the
	// "(n/m)" prefix.
	partMaxRunes = 560
	// maxParts is the owner-push per-kind hourly budget (3): a report that does
	// not fit in three messages is cut, and SAYS how much was cut.
	maxParts = 3
	// maxSetupsShown bounds one trader's setup list.
	maxSetupsShown = 4
)

func money(v float64) string {
	sign := "+"
	if v < 0 {
		sign, v = "-", -v
	}
	return fmt.Sprintf("%s$%.2f", sign, v)
}

func bootLabel(boot time.Time) string {
	if boot.IsZero() {
		return "since boot (boot time n/a)"
	}
	return "since boot " + kernel.FormatCT(boot)
}

// Lines renders the report, most important first (a cut drops from the end).
func (r Report) Lines() []string {
	var out []string
	out = append(out, fmt.Sprintf("📊 vl data report — session %s, %s to %s",
		r.Session.Key, kernel.ClockCT(r.Session.Start), kernel.ClockCT(r.Session.End)))

	// 1m bars, expected vs stored, per symbol.
	switch {
	case r.SymbolsErr != "":
		out = append(out, "1m bars: n/a ("+r.SymbolsErr+")")
	case len(r.Symbols) == 0:
		out = append(out, "1m bars: n/a (no 1m bars stored in the last 7 days)")
	default:
		for _, s := range r.Symbols {
			if s.Err != "" {
				out = append(out, s.Symbol+" 1m bars: n/a ("+s.Err+")")
				continue
			}
			l := fmt.Sprintf("%s 1m bars %d/%d stored", s.Symbol, s.Stored, s.Expected)
			if s.Missing() > 0 {
				l += fmt.Sprintf(" — MISSING %d in %d run(s), longest %dm", s.Missing(), s.Runs, s.Longest)
			}
			out = append(out, l)
		}
	}

	// FEED DOWN.
	fd := "FEED DOWN: alerts " + r.FeedDownAlerts.String()
	switch {
	case r.SymbolsErr != "" || len(r.Symbols) == 0:
		fd += " · minutes n/a"
	default:
		var parts []string
		for _, s := range r.Symbols {
			if s.Err != "" {
				parts = append(parts, s.Symbol+" n/a")
				continue
			}
			parts = append(parts, fmt.Sprintf("%s %dm", s.Symbol, s.FeedDownMin))
		}
		fd += " · minutes " + strings.Join(parts, ", ")
	}
	out = append(out, fd)

	// Bar-hole tracker.
	if r.Holes == nil {
		out = append(out, "Bar holes: n/a (tracker not wired)")
	} else {
		out = append(out, fmt.Sprintf("Bar holes %s: found %d, filled %d, unfilled %d",
			bootLabel(r.BootAt), r.Holes.Detected, r.Holes.Filled, r.Holes.Unfilled))
	}

	out = append(out, fmt.Sprintf("Bad prints %s · cross-frame mismatches %s · paused minutes %s",
		r.BadPrints, r.CrossFrame, r.PausedMin))
	out = append(out, fmt.Sprintf("Ingest drops %s: final %s, historical %s · clock steps %s back / %s fwd",
		bootLabel(r.BootAt), r.IngestFinal, r.IngestHist, r.ClockBack, r.ClockFwd))

	// Trades and P&L.
	switch {
	case r.TradesErr != "":
		out = append(out, "Trades: n/a ("+r.TradesErr+")")
	case len(r.Trades) == 0:
		out = append(out, "Trades: none closed this session")
	default:
		for _, t := range r.Trades {
			var names []string
			for n := range t.Setups {
				names = append(names, n)
			}
			sort.Slice(names, func(i, j int) bool {
				if t.Setups[names[i]].N != t.Setups[names[j]].N {
					return t.Setups[names[i]].N > t.Setups[names[j]].N
				}
				return names[i] < names[j]
			})
			var sp []string
			for i, n := range names {
				if i == maxSetupsShown {
					sp = append(sp, fmt.Sprintf("+%d more", len(names)-maxSetupsShown))
					break
				}
				sp = append(sp, fmt.Sprintf("%s %d %s", n, t.Setups[n].N, money(t.Setups[n].Pnl)))
			}
			out = append(out, fmt.Sprintf("%s: %d closed, %s (%s), unresolved %d",
				shortName(t.Trader), t.N, money(t.Pnl), strings.Join(sp, ", "), t.Unresolved))
		}
	}

	// Refusal classes repeated more than refusalMinRepeats times.
	switch {
	case !r.RefusalsOK:
		out = append(out, "Refusals: n/a (gate counters not readable for this session)")
	case len(r.Refusals) == 0:
		out = append(out, fmt.Sprintf("Refusals repeated >%d: none", refusalMinRepeats))
	default:
		var rs []string
		for _, f := range r.Refusals {
			rs = append(rs, fmt.Sprintf("%s %d", f.Gate, f.N))
		}
		out = append(out, fmt.Sprintf("Refusals repeated >%d: %s", refusalMinRepeats, strings.Join(rs, ", ")))
	}

	out = append(out, r.Restore.Line())
	return out
}

func shortName(s string) string {
	r := []rune(s)
	if len(r) > 14 {
		return string(r[:14]) + "…"
	}
	return s
}

// Line renders the backup-restore-test result.
func (b RestoreResult) Line() string {
	if !b.Have {
		return "Backup restore test: n/a (no result file: weekly timer not installed or never ran)"
	}
	l := fmt.Sprintf("Backup restore test: %s %s", b.Status, kernel.FormatCT(b.At))
	if b.Src != "" {
		l += " (" + b.Src + ")"
	}
	if b.Overdue {
		l += " — OVERDUE, weekly run missed"
	}
	return l
}

// Parts packs lines into messages of at most partMaxRunes, at most maxParts of
// them. Lines that do not fit are cut from the end and the last message says
// how many. A single line longer than a message is hard-cut with an ellipsis.
func Parts(lines []string) []string {
	var parts []string
	var cur []string
	curLen := 0
	flush := func() {
		if len(cur) > 0 {
			parts = append(parts, strings.Join(cur, "\n"))
			cur, curLen = nil, 0
		}
	}
	const prefixReserve = 8 // "(1/3)\n"
	limit := partMaxRunes - prefixReserve
	cut := 0
	for i, l := range lines {
		if utf8.RuneCountInString(l) > limit {
			l = string([]rune(l)[:limit-1]) + "…"
		}
		n := utf8.RuneCountInString(l)
		add := n
		if len(cur) > 0 {
			add++ // newline
		}
		if curLen+add > limit {
			flush()
			add = n
		}
		if len(parts) >= maxParts {
			cut = len(lines) - i
			break
		}
		cur = append(cur, l)
		curLen += add
	}
	flush()
	if cut > 0 && len(parts) > 0 {
		last := parts[len(parts)-1]
		note := fmt.Sprintf("\n(+%d lines cut)", cut)
		if utf8.RuneCountInString(last)+utf8.RuneCountInString(note) <= limit+prefixReserve-2 {
			parts[len(parts)-1] = last + note
		} else {
			r := []rune(last)
			parts[len(parts)-1] = string(r[:len(r)-utf8.RuneCountInString(note)]) + note
		}
	}
	if len(parts) > 1 {
		for i := range parts {
			parts[i] = fmt.Sprintf("(%d/%d)\n%s", i+1, len(parts), parts[i])
		}
	}
	return parts
}
