package updaterjob

import (
	"net/url"
	"time"
)

// APIView is what GET /api/updates/jobs/:id serves (U5b wires it). Its keys
// are exactly M5's UpdateJobView (web/src/lib/api/updates.ts) minus
// "status", which the client fills from the HTTP status and the server never
// sends. Every key but job_id and state is ABSENT until computed (L7):
//
//   - step: the running side effect's name — only while phase is started;
//   - blocker: only when the worker recorded one;
//   - timestamps: state → the time it was DONE, for every state done so far;
//   - receipt_url: only when at least one receipt exists;
//   - error: only when a step failed;
//   - recovery_reason: only when the job went to recovery_needed (UI-GUIDE
//     N-5: stale-at-start, stale-at-resume and the attempts cap set ONLY this,
//     never error, so the page showed a recovery job with no reason at all).
//
// Nothing else leaves the job file through the API: no paths, identities,
// backup location, log offsets or AddOn build ids.
type APIView struct {
	JobID      string           `json:"job_id"`
	State      State            `json:"state"`
	Step       Effect           `json:"step,omitempty"`
	Blocker    string           `json:"blocker,omitempty"`
	Timestamps map[State]string `json:"timestamps,omitempty"`
	ReceiptURL string           `json:"receipt_url,omitempty"`
	Error      string           `json:"error,omitempty"`
	// RecoveryReason is the worker's own recovery text, verbatim (additive).
	RecoveryReason string `json:"recovery_reason,omitempty"`
}

// ReceiptsView is what GET /api/updates/jobs/:id/receipt serves: the job's
// receipts in order ([] when none yet — computed empty).
type ReceiptsView struct {
	JobID    string    `json:"job_id"`
	State    State     `json:"state"`
	Receipts []Receipt `json:"receipts"`
}

// ReceiptURL is the receipt route for a job id.
func ReceiptURL(jobID string) string {
	return "/api/updates/jobs/" + url.PathEscape(jobID) + "/receipt"
}

// View projects a job (as Read returns it) onto the API.
func View(j Job) APIView {
	v := APIView{JobID: j.JobID, State: j.State, Blocker: j.Blocker, Error: j.Error, RecoveryReason: j.RecoveryReason}
	if j.Phase == PhaseStarted {
		if r, ok := Lookup(j.State); ok {
			v.Step = r.Effect
		}
	}
	for _, t := range j.Transitions {
		if t.Phase != PhaseDone {
			continue
		}
		if v.Timestamps == nil {
			v.Timestamps = map[State]string{}
		}
		v.Timestamps[t.State] = t.At.UTC().Format(time.RFC3339Nano)
	}
	if len(j.Receipts) > 0 {
		v.ReceiptURL = ReceiptURL(j.JobID)
	}
	return v
}

// Receipts projects a job's receipts onto the API.
func Receipts(j Job) ReceiptsView {
	rs := make([]Receipt, len(j.Receipts))
	copy(rs, j.Receipts)
	return ReceiptsView{JobID: j.JobID, State: j.State, Receipts: rs}
}
