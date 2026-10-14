package telemetry

import "sync/atomic"

// FOLD 3 (DS-106, 10-14 15:04:28): a FEED DOWN verdict that fell inside the
// reconnect/boot grace — the link had just come up and no live 1m bar had
// arrived yet — is SAID as "⏳ feed: waiting for the first live bar" and
// counted here, never dropped. Read onto that line (A11). A grace that
// expires without a live bar still fires FEED DOWN as before.
var feedDownGraced atomic.Int64

// IncFeedDownGraced counts one graced FEED DOWN verdict (one per link-up).
func IncFeedDownGraced() { feedDownGraced.Add(1) }

// FeedDownGracedCount is the count since boot.
func FeedDownGracedCount() int64 { return feedDownGraced.Load() }
