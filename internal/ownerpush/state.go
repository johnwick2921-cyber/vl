package ownerpush

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"vl/logger"
	"vl/safe"
)

// m442 #24 - held pushes and the dedupe map were memory only: a restart (and a
// deploy IS a kill -9 restart) dropped every held P0 and forgot which episodes
// had been told. This file is the small, atomic, owner-only record of both.
//
// What is saved: the held messages AND the in-flight ones (admitted, not yet
// confirmed delivered - a crash between enqueue and send must not lose them),
// the seen keys (24 h), and how the process last ended (clean signal exit vs
// not), which is the "reason if known" the boot push reports.
//
// What is not: delivery counters (a process-lifetime view), refusal-storm state.
// No DB schema; no deletion - the temp file is overwritten by the next write and
// consumed by the rename.

const stateVersion = 1

// PrevExit values: how the PREVIOUS process ended, as far as the file knows.
const (
	PrevExitNone    = "first-boot"   // no state file yet
	PrevExitClean   = "clean-signal" // SIGINT/SIGTERM handler reached MarkCleanShutdown
	PrevExitUnclean = "not-clean"    // file said "running": killed, crashed, power loss
	PrevExitUnknown = "state-unreadable"
)

// BootInfo is what Start learned about the previous process.
type BootInfo struct {
	PrevExit     string
	PrevBootUnix int64
	Restored     int // held / in-flight messages restored from the file
}

type stateMsg struct {
	Key      string `json:"key"`
	Text     string `json:"text"`
	AtUnix   int64  `json:"at_unix"`
	Attempts int    `json:"attempts,omitempty"`
}

type stateDisk struct {
	Version  int              `json:"version"`
	SavedAt  int64            `json:"saved_unix"`
	BootUnix int64            `json:"boot_unix"`
	Exit     string           `json:"exit"` // "running" | "clean"
	Held     []stateMsg       `json:"held"`
	Seen     map[string]int64 `json:"seen"`
}

type stateFile struct {
	path     string
	bootUnix int64
	exit     string
	kick     chan struct{}
	fileMu   sync.Mutex
}

// EnableState loads path (if any), restores held + seen into p, marks this
// process "running", and starts the writer. Safe to call once per pusher.
func (p *Pusher) EnableState(path string) BootInfo {
	info := BootInfo{PrevExit: PrevExitNone}
	if p == nil || path == "" {
		return info
	}
	prev, err := readState(path)
	switch {
	case err == nil && prev != nil:
		info.PrevBootUnix = prev.BootUnix
		if prev.Exit == "clean" {
			info.PrevExit = PrevExitClean
		} else {
			info.PrevExit = PrevExitUnclean
		}
	case err != nil:
		info.PrevExit = PrevExitUnknown
		logger.Warnf("owner push: state file %s unreadable (%v) - starting with empty held/seen", path, err)
	}
	p.mu.Lock()
	now := p.now()
	if prev != nil {
		for _, m := range prev.Held {
			at := time.Unix(m.AtUnix, 0)
			if now.Sub(at) > holdTTL {
				continue // stale news
			}
			p.held = append(p.held, heldMsg{key: m.Key, text: m.Text, at: at, attempts: m.Attempts})
			info.Restored++
		}
		for k, u := range prev.Seen {
			at := time.Unix(u, 0)
			if now.Sub(at) <= dedupeTTL {
				p.seen[k] = at
			}
		}
		// A key that is both held and seen would never be re-offered (Retry skips
		// seen keys). The file records in-flight keys as held, so un-see them.
		for _, h := range p.held {
			delete(p.seen, h.key)
		}
	}
	p.state = &stateFile{path: path, bootUnix: now.Unix(), exit: "running", kick: make(chan struct{}, 1)}
	p.mu.Unlock()
	st := p.state
	safe.GoNamed("owner-push-state", func() {
		for range st.kick {
			p.writeState()
		}
	})
	p.writeState() // mark "running" at once
	if info.Restored > 0 {
		logger.Infof("owner push: restored %d held message(s) from %s (previous exit: %s)", info.Restored, path, info.PrevExit)
	}
	return info
}

func readState(path string) (*stateDisk, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var d stateDisk
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, err
	}
	if d.Version != stateVersion {
		return nil, fmt.Errorf("state version %d, want %d", d.Version, stateVersion)
	}
	return &d, nil
}

// persistLocked asks the writer for a snapshot; it never blocks and never does
// I/O under p.mu (the caller holds it).
func (p *Pusher) persistLocked() {
	if p.state == nil {
		return
	}
	select {
	case p.state.kick <- struct{}{}:
	default: // a write is already pending; it will read the latest state
	}
}

// writeState snapshots under p.mu, writes outside it. Atomic: temp file in the
// same directory, fsync, rename.
func (p *Pusher) writeState() {
	p.mu.Lock()
	st := p.state
	if st == nil {
		p.mu.Unlock()
		return
	}
	d := stateDisk{Version: stateVersion, SavedAt: p.now().Unix(), BootUnix: st.bootUnix, Exit: st.exit,
		Held: []stateMsg{}, Seen: map[string]int64{}}
	for _, h := range p.held {
		d.Held = append(d.Held, stateMsg{Key: h.key, Text: h.text, AtUnix: h.at.Unix(), Attempts: h.attempts})
	}
	for _, m := range p.inflight { // not yet confirmed: a crash now must not lose it
		d.Held = append(d.Held, stateMsg{Key: m.key, Text: m.text, AtUnix: m.at.Unix(), Attempts: m.attempts})
	}
	for k, at := range p.seen {
		if _, inflight := p.inflight[k]; inflight {
			continue
		}
		d.Seen[k] = at.Unix()
	}
	p.mu.Unlock()

	b, err := json.Marshal(d)
	if err != nil {
		logger.Errorf("owner push: state encode: %v", err)
		return
	}
	st.fileMu.Lock()
	defer st.fileMu.Unlock()
	if err := writeAtomic(st.path, b); err != nil {
		logger.Errorf("owner push: state write %s: %v", st.path, err)
	}
}

// FlushState writes the current snapshot now. Tests; production uses the writer.
func (p *Pusher) FlushState() { p.writeState() }

func writeAtomic(path string, b []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp" // overwritten by the next write, consumed by the rename
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// MarkCleanShutdown records that this process is exiting on a signal, so the
// next boot can say "restarted" rather than "died". Synchronous. No default or
// no state file = no-op.
func MarkCleanShutdown() {
	defMu.RLock()
	p := def
	defMu.RUnlock()
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.state != nil {
		p.state.exit = "clean"
	}
	p.mu.Unlock()
	p.writeState()
}

// BootMessage renders the owner's "the bot is up" push. rev is the running
// revision (READ, never a literal); refused says trading was refused at boot.
// The key is unique per process start.
func BootMessage(rev string, info BootInfo, refused bool, now time.Time) (key, text string) {
	var why string
	switch info.PrevExit {
	case PrevExitClean:
		why = "previous process was stopped cleanly (restart / deploy)"
	case PrevExitUnclean:
		why = "previous process did NOT shut down cleanly (crash, kill -9 as the deploy does, or power loss)"
	case PrevExitNone:
		why = "first start with this state file"
	default:
		why = "how the previous process ended is unknown (state file unreadable)"
	}
	if info.PrevBootUnix > 0 {
		why += fmt.Sprintf("; it had started %s", time.Unix(info.PrevBootUnix, 0).UTC().Format("2006-01-02 15:04 UTC"))
	}
	if rev == "" {
		rev = "n/a"
	}
	text = fmt.Sprintf("\U0001F7E2 vl is UP - rev %s. %s.", rev, why)
	if info.Restored > 0 {
		text += fmt.Sprintf(" %d held alert(s) restored and will be re-sent.", info.Restored)
	}
	if refused {
		text = "\U0001F6A8 vl is UP but TRADING IS REFUSED - rev " + rev + ". " + why + ". Check the boot integrity line."
	}
	return fmt.Sprintf("boot|%d", now.Unix()), text
}
