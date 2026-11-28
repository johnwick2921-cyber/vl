package trader

import (
	"fmt"
	"strings"
	"sync/atomic"

	"vl/config"
	"vl/internal/datatruth/checks"
	"vl/internal/ownerpush"
	"vl/kernel"
)

// RegisterFillPrice wires the m447 (7) fill-price guard: the CME session key the
// "2 far fills in a session" count rolls on, the owner-push sender for the one
// alert per far fill, and the datatruth check. The integrator calls it once at
// boot (with RegisterBrokerAgreement, or through RegisterBrokerTruthChecks).
func RegisterFillPrice() {
	g := checks.DefaultFills()
	g.SetSession(kernel.CMESessionDayKey)
	g.SetAlert(func(kind, text string) { ownerpush.Push(kind, text) })
	// the pause text prints a runnable acknowledgement URL (review 4 D6): the API's
	// own host and port, read from the config, never a literal
	checks.SetAckBaseURL(ackBaseURLFromConfig())
	// m451: the strikes and the owner's acknowledgement survive a restart (a deploy
	// is a kill -9). The path is set by main beside the DB; unset (tests) = memory
	// only. After SetAlert, so an unreadable file reaches the owner.
	if p, _ := fillPriceStatePath.Load().(string); p != "" {
		g.EnableState(p)
	}
	checks.RegisterFillPrice(g)
}

// ackBaseURLFromConfig is where the owner reaches the API from the box the bot
// runs on: the configured port, and the configured host unless it is a wildcard
// bind (0.0.0.0 / :: / empty), which is reached through loopback.
func ackBaseURLFromConfig() string {
	cfg := config.Get()
	host := strings.TrimSpace(cfg.APIServerHost)
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	port := cfg.APIServerPort
	if port <= 0 {
		port = 8080
	}
	return fmt.Sprintf("http://%s:%d", host, port)
}

// fillPriceStatePath is the file the fill_price strikes are kept in across a
// restart (m451), beside the DB like the owner-push state file.
var fillPriceStatePath atomic.Value

// SetFillPriceStatePath records the fill_price state file; main calls it before
// the traders load.
func SetFillPriceStatePath(path string) { fillPriceStatePath.Store(path) }

// RegisterBrokerTruthChecks registers both m447 checks (broker_agreement and
// fill_price). Exits, stops and protection never consult either.
func RegisterBrokerTruthChecks() {
	RegisterBrokerAgreement()
	RegisterFillPrice()
}
