package datatruth

import "vl/internal/ownerpush"

// UseOwnerPush routes the once-per-episode alert to the owner's bound channel
// through ownerpush's public Push API (m442 owns that package; this is the only
// call). The kind string datatruth passes is already the dedupe key.
func UseOwnerPush() {
	SetNotifier(func(kind, text string) { ownerpush.Push(kind, text) })
}
