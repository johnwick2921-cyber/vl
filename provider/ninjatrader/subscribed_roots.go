package ninjatrader

// SubscribedRoots is the dedup'd union of every root the server subscribes
// bars for (primary first). The bar-gap audit (m444) walks exactly these, so a
// symbol that is subscribed but has not yet delivered a live bar this process
// is still audited. Nil-safe.
//
// Its own file on purpose: tcp_server.go is a byte-for-byte protected file
// (web/src/brand-scope.test.ts), and this accessor only reads state it owns.
func (s *TCPServer) SubscribedRoots() []string {
	if s == nil {
		return nil
	}
	s.barsSubMu.RLock()
	defer s.barsSubMu.RUnlock()
	return s.subscribedRootsLocked()
}
