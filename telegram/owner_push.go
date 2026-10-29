package telegram

import "vl/store"

// NotifyOwner sends one OUTBOUND ops message (a feed-down, a protection alert,
// an unfillable-hole notice — release #52 m432) to the owner's bound Telegram
// chat. It is the same sender NotifyUpdateResult uses (token and bound chat
// are read from the store at call time; unbound or no token = a quiet no-op)
// under a name that says what it is for. It opens NO inbound surface: no
// handler, no command, no listener.
//
// The caller is internal/ownerpush, which owns dedupe, the hourly rate limit,
// the bounded queue and the friend-instance refusal; main.go wires this in
// only on the owner's box.
//
// It returns the delivery outcome (m442 N4) so ownerpush can retry a failure.
func NotifyOwner(st *store.Store, text string) error { return SendOwnerText(st, text) }
