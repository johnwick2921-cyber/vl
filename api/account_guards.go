package api

import (
	"strings"
)

// REL18-C — ONE TRADER PER (SYMBOL, ACCOUNT) + NO ACCOUNT CHANGE WHILE RUNNING.
//
// The "symbol" is the NinjaTrader instrument (exchange.NTInstrumentName, e.g.
// "MNQ") and the "account" is the trader's persisted NT8 sub-account binding
// (trader.Account). Both live in the DB; the running state is the in-memory
// roster's HealthIsRunning(). The ordered-execution worker keys on the same
// (symbol, account), so a duplicate would double-apply fills at the broker.

// traderSymbolAccount resolves a trader's (symbol, account) from the DB.
// An empty symbol (no NT instrument) or empty account (unbound) is legal and
// returned as-is — both guards are no-ops for an unbound/unknown pair.
func (s *Server) traderSymbolAccount(userID, traderID string) (symbol, account string, err error) {
	cfg, err := s.store.Trader().GetFullConfig(userID, traderID)
	if err != nil || cfg == nil {
		return "", "", err
	}
	if cfg.Exchange != nil {
		symbol = strings.TrimSpace(cfg.Exchange.NTInstrumentName)
	}
	if cfg.Trader != nil {
		account = strings.TrimSpace(cfg.Trader.Account)
	}
	return symbol, account, nil
}

// runningTraderOnSameSymbolAccount returns the first in-memory RUNNING trader
// (other than excludeID) whose DB (symbol, account) equals the given pair.
// ok=false when no such trader is running. Used by handleStartTrader (O3b).
func (s *Server) runningTraderOnSameSymbolAccount(userID, excludeID, symbol, account string) (id, name string, ok bool) {
	if strings.TrimSpace(symbol) == "" || strings.TrimSpace(account) == "" {
		return "", "", false
	}
	for id, at := range s.traderManager.GetAllTraders() {
		if id == excludeID || at == nil || !at.HealthIsRunning() {
			continue
		}
		otherSymbol, otherAccount, err := s.traderSymbolAccount(userID, id)
		if err != nil {
			continue
		}
		if strings.EqualFold(otherSymbol, symbol) && otherAccount == account {
			return id, at.GetName(), true
		}
	}
	return "", "", false
}

// otherTraderBoundToSameSymbolAccount returns the first trader (other than
// excludeID) whose DB account equals `account` and whose symbol equals `symbol`,
// running or not. ok=false when none. Used by handleSelectAccount (O3a).
func (s *Server) otherTraderBoundToSameSymbolAccount(userID, excludeID, symbol, account string) (id, name string, ok bool) {
	if strings.TrimSpace(symbol) == "" || strings.TrimSpace(account) == "" {
		return "", "", false
	}
	traders, err := s.store.Trader().List(userID)
	if err != nil {
		return "", "", false
	}
	for _, t := range traders {
		if t == nil || t.ID == excludeID || strings.TrimSpace(t.Account) != account {
			continue
		}
		otherSymbol, _, serr := s.traderSymbolAccount(userID, t.ID)
		if serr != nil {
			continue
		}
		if strings.EqualFold(otherSymbol, symbol) {
			name := strings.TrimSpace(t.Name)
			if name == "" {
				name = t.ID
			}
			return t.ID, name, true
		}
	}
	return "", "", false
}
