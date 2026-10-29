package trader

import "vl/internal/ownerpush"

// m442 item 6 - REFUSAL STORM ALERT. One call at each refusal site; the counting,
// the threshold (ownerpush.StormThreshold within ownerpush.StormWindow), the
// one-push-per-episode rule and the budget live in internal/ownerpush. The
// refusal itself, its log line and its counter are unchanged.

// noteRefusalStorm records one refusal of class for subject (a position, signal
// or arm) on this trader. Never blocks; safe on a nil trader.
func (at *AutoTrader) noteRefusalStorm(class, subject, detail string) {
	if at == nil {
		return
	}
	ownerpush.NoteRefusal(ownerpush.Refusal{Class: class, TraderID: at.id, Subject: subject, Detail: detail})
}
