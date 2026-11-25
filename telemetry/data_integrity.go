package telemetry

import (
	"sort"
	"sync"
)

// DATA-INTEGRITY GATE (m445, release #54) — a counter per PAUSE EPISODE and per
// reason. Refusals themselves are counted in the per-trader gate-block table
// (IncGateBlock, classes data_integrity_hole / data_integrity_unknown); these
// count episodes (one per pause, however many entries it refused) and the
// resumes, so "how many times did missing data pause trading today" is a read.
// In-memory, since boot — a restart re-derives the state from the hole tracker.

var (
	dataIntegrityMu       sync.Mutex
	dataIntegrityEpisodes = map[string]int64{}
	dataIntegrityResumes  int64
)

// IncDataIntegrityEpisode records one pause episode for a reason class.
func IncDataIntegrityEpisode(reason string) {
	if reason == "" {
		return
	}
	dataIntegrityMu.Lock()
	dataIntegrityEpisodes[reason]++
	dataIntegrityMu.Unlock()
}

// IncDataIntegrityResume records one resume.
func IncDataIntegrityResume() {
	dataIntegrityMu.Lock()
	dataIntegrityResumes++
	dataIntegrityMu.Unlock()
}

// DataIntegrityCounts is the READ view for /api/health.
type DataIntegrityCounts struct {
	Episodes map[string]int64 `json:"episodes"` // by reason class
	Resumes  int64            `json:"resumes"`
}

// DataIntegrityCountsSnapshot reads the counters once (a copy).
func DataIntegrityCountsSnapshot() DataIntegrityCounts {
	dataIntegrityMu.Lock()
	defer dataIntegrityMu.Unlock()
	out := DataIntegrityCounts{Episodes: make(map[string]int64, len(dataIntegrityEpisodes)), Resumes: dataIntegrityResumes}
	keys := make([]string, 0, len(dataIntegrityEpisodes))
	for k := range dataIntegrityEpisodes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out.Episodes[k] = dataIntegrityEpisodes[k]
	}
	return out
}
