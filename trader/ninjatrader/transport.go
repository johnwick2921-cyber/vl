// Package ninjatrader — env-var router for the Plan 1/Plan 1.5 transport split.
//
// NT_TRANSPORT=csv  (default — Plan 1, SIM-validated)
// NT_TRANSPORT=tcp  (Plan 1.5 opt-in)
//
// Zero-downtime flip-back: change the env var, restart the bot, no code
// rebuild required. The TCP path starts a listener on 127.0.0.1:36974; the
// C# AddOn dials in on NT startup (see ninjascript/VLTraderTCPClient.cs
// scaffolded under Plan 1.5 spec L4370).
package ninjatrader

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"vl/logger"
	ntwire "vl/provider/ninjatrader"
	"vl/trader/types"
)

// TransportEnvVar is the env-var name read by NewTraderFromEnv. Exported so
// tests / docs can reference the canonical key without string duplication.
const TransportEnvVar = "NT_TRANSPORT"

// TCP server is a process-singleton: one bot → one listener on 127.0.0.1:36974
// → one connected NT AddOn → many NT traders share the wire. Repeated
// NewTraderFromEnv calls (e.g. when the API reloads a trader via
// RemoveTrader + LoadUserTradersFromStore) must NOT re-bind the port.
var (
	tcpServerOnce sync.Once
	tcpServerInst *ntwire.TCPServer
	tcpServerErr  error
)

// orderSnapshotSink (F12) is the optional persistence hook for received
// order_snapshot frames. Registered by main.go before the server starts; nil
// disables persistence WITHOUT disabling the cache, because the cutover gate
// reads the cache and must never depend on a database being reachable.
var orderSnapshotSink func(ntwire.OrderSnapshotPayload)

// SetOrderSnapshotSink registers the hook. Call before the first trader is
// built; later calls are ignored by design, since the server is a singleton
// started once.
func SetOrderSnapshotSink(fn func(ntwire.OrderSnapshotPayload)) { orderSnapshotSink = fn }

// getOrStartTCPServer lazily starts the singleton TCP server on first call.
// Subsequent calls return the same instance. The startup error (if any) is
// cached and returned to every caller so a bind failure surfaces consistently.
func getOrStartTCPServer() (*ntwire.TCPServer, error) {
	tcpServerOnce.Do(func() {
		server := ntwire.NewTCPServer(nil)
		if err := server.Start(context.Background()); err != nil {
			tcpServerErr = fmt.Errorf("transport: start tcp server: %w", err)
			return
		}
		tcpServerInst = server
		// F12 — install the forensic sink if one was registered. The store is
		// NOT imported here: main.go owns the closure, so this package keeps its
		// single responsibility and the gate's in-memory read stays independent
		// of whether persistence is wired at all.
		if fn := orderSnapshotSink; fn != nil {
			server.SetOrderSnapshotSink(fn)
		}
		// Stage 3: route the kernel's futures kline reads to this server's
		// live BarCache (NT8 bars), bypassing the removed crypto providers. Crypto path untouched.
		wireFuturesBarsProvider(server)
	})
	return tcpServerInst, tcpServerErr
}

// TCPServer exposes the singleton wire server for maintenance paths that ride
// the running process (HISTORY IMPORT, wave 101): the wire holds exactly one
// connected client, so a named-contract historical pull must go through the
// live server, never a second process. Returns the server, or the cached bind
// error.
func TCPServer() (*ntwire.TCPServer, error) { return getOrStartTCPServer() }

// SetTCPServerForTest installs a server as the process singleton so a
// production-call-site test can drive NewTraderFromEnv against a controlled
// wire WITHOUT binding the live 127.0.0.1:36974 port (the live bot may hold
// it, and the singleton is one-bot-one-listener). TEST-ONLY — never called in
// production; mirrors SetTraderForTest / SetAddrForTest. Marks the once done so
// a later getOrStartTCPServer returns the injected instance instead of binding.
func SetTCPServerForTest(server *ntwire.TCPServer) {
	tcpServerOnce.Do(func() {})
	tcpServerInst = server
	tcpServerErr = nil
}

// SplitSymbolList parses the P5.2 symbol-as-list config form: the Exchange
// row's NT instrument field may be a comma-separated list ("MNQ,ES,NQ"). The
// FIRST element is the PRIMARY (the trading symbol — orders, ticks, gate); the
// rest are EXTRA bar-subscription roots (data feeds only until P5.4). The
// legacy single-symbol form ("MNQ") is the one-element case — byte-identical
// behavior, no migration needed.
func SplitSymbolList(symbolList string) (primary string, extras []string) {
	parts := strings.Split(symbolList, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if primary == "" {
			primary = p
			continue
		}
		extras = append(extras, p)
	}
	return primary, extras
}

// equalSymbol compares two symbol roots case-insensitively (the wire echoes
// whatever case the AddOn resolved; "MNQ" == "mnq").
func equalSymbol(a, b string) bool { return strings.EqualFold(a, b) }

// NewTraderFromEnv returns either the CSV Trader (default — Plan 1 SIM-validated)
// or the TCPTrader (Plan 1.5 opt-in), based on the NT_TRANSPORT env var.
//
// Unknown values are an error — fail-fast instead of silently defaulting,
// because a typo'd env var on a live-money bot is a footgun.
//
// P5.2: cfg.Symbol may be a comma-separated list — the primary drives the
// trader; extras become additional bar subscriptions (TCP transport only; the
// CSV transport logs + ignores extras — it has no bar feed).
func NewTraderFromEnv(cfg Config) (types.Trader, error) {
	transport := strings.ToLower(strings.TrimSpace(os.Getenv(TransportEnvVar)))
	primary, extras := SplitSymbolList(cfg.Symbol)
	if primary != "" {
		cfg.Symbol = primary // every downstream consumer sees ONLY the trading symbol
	}
	switch transport {
	case "":
		// P2-18 — an UNSET transport no longer falls back SILENTLY: the boot
		// line names the effective (deprecated) transport so a config that
		// forgot the var is visible instead of silently running legacy CSV.
		// Behaviour is unchanged (unset still resolves to the CSV path —
		// flipping the default would be a trading-behaviour change); only the
		// silence is fixed. Live .env sets NT_TRANSPORT, so this WARN fires
		// only where the setting is genuinely absent.
		logger.Warnf("⚠️ transport: %s UNSET — using the deprecated CSV transport (set NT_TRANSPORT=tcp for the live TCP bridge); effective transport: csv", TransportEnvVar)
		if len(extras) > 0 {
			fmt.Printf("⚠️ transport: extra symbols %v ignored on the CSV transport (no bar feed); trading %s\n", extras, cfg.Symbol)
		}
		return New(cfg), nil
	case "csv":
		// Explicit opt-in to the deprecated CSV path — honored as chosen.
		if len(extras) > 0 {
			fmt.Printf("⚠️ transport: extra symbols %v ignored on the CSV transport (no bar feed); trading %s\n", extras, cfg.Symbol)
		}
		return New(cfg), nil
	case "tcp":
		server, err := getOrStartTCPServer()
		if err != nil {
			return nil, err
		}
		t := NewTCPTrader(server, cfg.Symbol, cfg.Account) // registers the trading symbol + binds the account
		// REPLACE the extras from config (source of truth on every trader
		// (re)load — a symbol removed from the Exchange row must not linger on
		// the process-singleton server), then append the NT_EXTRA_SYMBOLS
		// testing override. Both run AFTER the primary is set so dedup is
		// against the real primary (e.g. "NQ,MNQ" keeps MNQ under primary NQ).
		server.SetExtraBarsSymbolsFor(cfg.Symbol, extras...)
		if env := strings.TrimSpace(os.Getenv("NT_EXTRA_SYMBOLS")); env != "" {
			server.AddBarsSubscribeSymbols(strings.Split(env, ",")...)
			fmt.Printf("📡 NT_EXTRA_SYMBOLS override active: %s (config extras: %v, primary: %s)\n", env, extras, cfg.Symbol)
		}
		return t, nil
	default:
		return nil, fmt.Errorf("transport: unknown %s=%q (want csv or tcp)", TransportEnvVar, transport)
	}
}
