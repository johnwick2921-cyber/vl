package ninjatrader

import (
	"net"
	"testing"
	"time"
)

// Each case is one sequence of books of one account; want is whether the LAST one
// is late. MUTANTS named per case.
func TestBookOrder(t *testing.T) {
	type put struct {
		after   time.Duration // Go's receive clock, since the put before
		emitted int64         // ms
		link    int           // 0 = not known
	}
	const sec = time.Second
	const proof = positionRegrowProof
	for name, c := range map[string]struct {
		puts []put
		want bool
	}{
		"the first book": {[]put{{0, 100, 1}}, false},
		"newer":          {[]put{{0, 100, 1}, {sec, 101, 1}}, false},
		"the same stamp": {[]put{{0, 100, 1}, {sec, 100, 1}}, false},
		"older":          {[]put{{0, 100, 1}, {sec, 99, 1}}, true},
		"older twice: newer than the late one only":    {[]put{{0, 100, 1}, {0, 50, 1}, {sec, 60, 1}}, true},
		"late, then newer":                             {[]put{{0, 100, 1}, {0, 50, 1}, {sec, 101, 1}}, false},
		"late, newer, late again long after":           {[]put{{0, 100, 1}, {0, 50, 1}, {sec, 200, 1}, {60 * sec, 150, 1}}, true},
		"no stamp":                                     {[]put{{0, 100, 1}, {sec, 0, 1}}, false},
		"no stamp keeps the newest":                    {[]put{{0, 100, 1}, {sec, 0, 1}, {sec, 99, 1}}, true},
		"no stamp ever":                                {[]put{{0, 0, 1}, {sec, 0, 1}}, false},
		"a negative stamp":                             {[]put{{0, 100, 1}, {sec, -5, 1}}, false},
		"stamped after an unstamped one":               {[]put{{0, 0, 1}, {sec, 100, 1}}, false},
		"a new connection":                             {[]put{{0, 100, 1}, {sec, 50, 2}}, false},
		"a new connection starts over":                 {[]put{{0, 100, 1}, {sec, 50, 2}, {sec, 49, 2}}, true},
		"a new connection ends a late run":             {[]put{{0, 100, 1}, {0, 50, 1}, {sec, 40, 2}}, false},
		"no connection":                                {[]put{{0, 100, 0}, {sec, 50, 0}}, false},
		"clock set back, the next book soon":           {[]put{{0, 1e9, 1}, {0, 50, 1}, {proof - 1, 12_050, 1}}, true},
		"clock set back, the next book later":          {[]put{{0, 1e9, 1}, {0, 50, 1}, {proof, 12_050, 1}}, false},
		"clock set back: the next 30 s book":           {[]put{{0, 1e9, 1}, {0, 50, 1}, {30 * sec, 30_050, 1}}, false},
		"clock set back: counted from the first":       {[]put{{0, 1e9, 1}, {0, 50, 1}, {6 * sec, 6050, 1}, {6 * sec, 12_050, 1}}, false},
		"clock set back, then in order":                {[]put{{0, 1e9, 1}, {0, 50, 1}, {proof, 12_050, 1}, {sec, 12_051, 1}}, false},
		"clock set back, then a late one":              {[]put{{0, 1e9, 1}, {0, 50, 1}, {proof, 12_050, 1}, {sec, 12_049, 1}}, true},
		"clock set back, its newest stamp again":       {[]put{{0, 1e9, 1}, {0, 50, 1}, {sec, 12_050, 1}, {proof, 12_050, 1}}, true},
		"clock set back, the same stamp proves 0":      {[]put{{0, 1e9, 1}, {0, 50, 1}, {proof, 50, 1}}, true},
		"clock set back twice":                         {[]put{{0, 1e9, 1}, {0, 5000, 1}, {sec, 50, 1}, {proof, 12_050, 1}}, false},
		"late among the late ones is a new run":        {[]put{{0, 1e9, 1}, {0, 5000, 1}, {proof, 50, 1}}, true},
		"the second run is dated from ITS first":       {[]put{{0, 1e9, 1}, {0, 5000, 1}, {6 * sec, 1000, 1}, {6 * sec, 15_000, 1}}, true},
		"older than the newest late one is a new run":  {[]put{{0, 1e9, 1}, {0, 50, 1}, {sec, 20_050, 1}, {proof, 15_050, 1}}, true},
		"two really late books 13 s apart":             {[]put{{0, 1e9, 1}, {0, 1e9 - 40, 1}, {13 * sec, 1e9 - 39, 1}}, true},
		"two really late books, then the resend":       {[]put{{0, 1e9, 1}, {0, 1e9 - 40, 1}, {13 * sec, 1e9 - 39, 1}, {17 * sec, 1e9 + 30_000, 1}}, false},
		"the stamps moved 1 ms less than a real step":  {[]put{{0, 1e9, 1}, {0, 50, 1}, {proof, 50 + 9999, 1}}, true},
		"the stamps moved as far as a real step":       {[]put{{0, 1e9, 1}, {0, 50, 1}, {proof, 50 + 10_000, 1}}, false},
		"an oscillating clock, 1 ms before the cap":    {[]put{{0, 1e9, 1}, {0, 9e6, 1}, {30 * sec, 8e6, 1}, {30 * sec, 7e6, 1}, {30*sec - time.Millisecond, 6e6, 1}}, true},
		"an oscillating clock, at the cap":             {[]put{{0, 1e9, 1}, {0, 9e6, 1}, {30 * sec, 8e6, 1}, {30 * sec, 7e6, 1}, {30 * sec, 6e6, 1}}, false},
		"an oscillating clock after the cap, in order": {[]put{{0, 1e9, 1}, {0, 9e6, 1}, {30 * sec, 8e6, 1}, {30 * sec, 7e6, 1}, {30 * sec, 6e6, 1}, {sec, 6e6 + 1, 1}}, false},
		"an oscillating clock: late again after it":    {[]put{{0, 1e9, 1}, {0, 9e6, 1}, {30 * sec, 8e6, 1}, {30 * sec, 7e6, 1}, {30 * sec, 6e6, 1}, {sec, 5e6, 1}}, true},
		"a newer book restarts the cap":                {[]put{{0, 1e9, 1}, {0, 9e6, 1}, {60 * sec, 1e9 + 1, 1}, {0, 8e6, 1}, {60 * sec, 7e6, 1}}, true},
		"only two late books, 90 s apart":              {[]put{{0, 1e9, 1}, {0, 9e6, 1}, {90 * sec, 8e6, 1}}, true},
		"only two late books, 10 min apart":            {[]put{{0, 1e9, 1}, {0, 9e6, 1}, {600 * sec, 8e6, 1}}, true},
		"the third late book, 1 ms before the cap":     {[]put{{0, 1e9, 1}, {0, 9e6, 1}, {sec, 8e6, 1}, {89*sec - time.Millisecond, 7e6, 1}}, true},
		"the third late book, at the cap":              {[]put{{0, 1e9, 1}, {0, 9e6, 1}, {sec, 8e6, 1}, {89 * sec, 7e6, 1}}, false},
		"three late books of one run (no stamp step)":  {[]put{{0, 1e9, 1}, {0, 9e6, 1}, {sec, 9e6, 1}, {89 * sec, 9e6, 1}}, false},
		"late books after the cap are counted anew":    {[]put{{0, 1e9, 1}, {0, 9e6, 1}, {sec, 8e6, 1}, {89 * sec, 7e6, 1}, {0, 6e6, 1}, {90 * sec, 5e6, 1}}, true},
		"a newer book restarts the count":              {[]put{{0, 1e9, 1}, {0, 9e6, 1}, {0, 8e6, 1}, {0, 1e9 + 1, 1}, {0, 7e6, 1}, {90 * sec, 6e6, 1}}, true},
		"no stamp restarts the count":                  {[]put{{0, 1e9, 1}, {0, 9e6, 1}, {0, 8e6, 1}, {0, 0, 1}, {0, 7e6, 1}, {90 * sec, 6e6, 1}}, true},
		"back at the old time":                         {[]put{{0, 1e9, 1}, {0, 50, 1}, {sec, 1e9 + 1, 1}}, false},
	} {
		links := []net.Conn{nil, &net.TCPConn{}, &net.TCPConn{}}
		cache := NewOrderSnapshotCache()
		now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
		for _, p := range c.puts {
			now = now.Add(p.after)
			cache.putFrom(OrderSnapshotPayload{Account: "Sim101", EmittedMs: p.emitted}, now, links[p.link])
			cache.putFrom(OrderSnapshotPayload{Account: "Sim102", EmittedMs: 1}, now, links[p.link]) // another account is not this one
		}
		last := c.puts[len(c.puts)-1]
		p, at, late, ok := cache.LatestOrder("sim101 ")
		if late != c.want || !ok {
			t.Errorf("%s: late = %v (ok %v), want %v", name, late, ok, c.want)
		}
		if p.EmittedMs != last.emitted || !at.Equal(now) {
			t.Errorf("%s: the cached book must be the last one received, got stamp %d at %s", name, p.EmittedMs, at)
		}
		if _, _, late, ok := cache.LatestOrder("Sim102"); late || !ok {
			t.Errorf("%s: another account is not this one", name)
		}
	}
	if _, _, late, ok := NewOrderSnapshotCache().LatestOrder("Sim101"); late || ok {
		t.Error("no book: not late, not there")
	}
}

// The PRODUCTION read loop dates the books by connection. MUTANT: PutAt there.
func TestBookOrderThroughTheReadLoop(t *testing.T) {
	s, dial := probeServer(t)
	c := dial()
	book := func(emitted int64) (late bool) {
		t.Helper()
		if err := WriteFrame(c, FrameOrderSnapshot, OrderSnapshotPayload{Account: "Sim101", EmittedMs: emitted, Orders: []NT8Order{}}); err != nil {
			t.Fatal(err)
		}
		probeSeen(t, s, c)
		p, _, late, ok := s.OrderSnapshots().LatestOrder("Sim101")
		if !ok || p.EmittedMs != emitted {
			t.Fatalf("the cache must hold the last book, got %d", p.EmittedMs)
		}
		return late
	}
	if book(100) || !book(99) || book(101) {
		t.Fatal("99 after 100 is late, nothing else is")
	}
	_ = c.Close()
	probeWait(t, "the disconnect", func() bool { return !s.IsConnected() })
	c = dial()
	if book(50) {
		t.Fatal("a new connection starts over")
	}
	if !book(49) {
		t.Fatal("… and is dated like the first")
	}
}
