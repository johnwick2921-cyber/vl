package trader

import (
	"testing"
	"time"
)

// The tests of this package look for an order's fill every 100 ms (0.6 s in all)
// instead of every 500 ms: none of them pins the interval, and some thirty of
// them waited out the full 3 s. Production keeps 500 ms.
func init() { orderFillPollInterval = 100 * time.Millisecond }

func TestOrderFillPollProductionInterval(t *testing.T) {
	if orderFillPollDefault != 500*time.Millisecond {
		t.Fatalf("the production poll is %s, want 500 ms", orderFillPollDefault)
	}
}
