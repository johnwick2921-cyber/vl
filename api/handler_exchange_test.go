package api

import (
	"encoding/json"
	"testing"

	"vl/crypto"
	"vl/store"
)

func TestSafeExchangeConfigFromStoreIncludesCredentialPresenceFlags(t *testing.T) {
	// A STORED legacy row (C1: stored rows still load) — the safe shape must
	// still surface API-key/secret/passphrase PRESENCE without the secrets.
	cfg := &store.Exchange{
		ID:           "ex-1",
		ExchangeType: "legacy-cex",
		AccountName:  "Legacy Main",
		Name:         "Legacy Main",
		Type:         "cex",
		Enabled:      true,
		APIKey:       crypto.EncryptedString("api-test-123"),
		SecretKey:    crypto.EncryptedString("secret-test-123"),
		Passphrase:   crypto.EncryptedString("passphrase-test-123"),
	}

	safe := safeExchangeConfigFromStore(cfg)
	if !safe.HasAPIKey {
		t.Fatalf("expected has_api_key to be true")
	}
	if !safe.HasSecretKey {
		t.Fatalf("expected has_secret_key to be true")
	}
	if !safe.HasPassphrase {
		t.Fatalf("expected has_passphrase to be true")
	}
	// Crypto-specific presence flags were cut with the venues.
}

// TestSafeExchangeConfigFromStore_NinjaTraderFields verifies that NT-specific
// columns (no secrets — all non-sensitive) are surfaced verbatim via the safe
// response shape so the UI can prefill the form when editing.
func TestSafeExchangeConfigFromStore_NinjaTraderFields(t *testing.T) {
	cfg := &store.Exchange{
		ID:                   "ex-nt-1",
		ExchangeType:         "ninjatrader",
		AccountName:          "NT8 Sim",
		Name:                 "NinjaTrader",
		Type:                 "futures",
		Enabled:              true,
		NTDataDir:            "/mnt/c/Users/foo/VlTrader/data",
		NTInstrumentName:     "MNQ",
		NTDefaultContractQty: 1,
	}

	safe := safeExchangeConfigFromStore(cfg)
	if safe.NTDataDir != cfg.NTDataDir {
		t.Fatalf("nt_data_dir mismatch: got %q want %q", safe.NTDataDir, cfg.NTDataDir)
	}
	if safe.NTInstrumentName != "MNQ" {
		t.Fatalf("nt_instrument_name mismatch: got %q", safe.NTInstrumentName)
	}
	if safe.NTDefaultContractQty != 1 {
		t.Fatalf("nt_default_contract_qty mismatch: got %d", safe.NTDefaultContractQty)
	}
	if safe.HasAPIKey || safe.HasSecretKey || safe.HasPassphrase {
		t.Fatalf("NT row must not advertise API key/secret/passphrase flags")
	}
}

// TestCreateExchangeRequest_NinjaTraderJSON verifies the API accepts a JSON
// body for type=ninjatrader without API key / secret fields.
func TestCreateExchangeRequest_NinjaTraderJSON(t *testing.T) {
	body := `{
		"exchange_type": "ninjatrader",
		"account_name": "NT8 Sim",
		"enabled": true,
		"nt_data_dir": "/mnt/c/Users/foo/VlTrader/data",
		"nt_instrument_name": "MNQ",
		"nt_default_contract_qty": 1
	}`
	var req CreateExchangeRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if req.ExchangeType != "ninjatrader" {
		t.Fatalf("exchange_type mismatch: %q", req.ExchangeType)
	}
	if req.NTDataDir == "" {
		t.Fatalf("nt_data_dir not parsed")
	}
	if req.NTInstrumentName != "MNQ" {
		t.Fatalf("nt_instrument_name not parsed")
	}
	if req.NTDefaultContractQty != 1 {
		t.Fatalf("nt_default_contract_qty not parsed: %d", req.NTDefaultContractQty)
	}
	// happy path: validation must pass
	missing := store.MissingRequiredExchangeCredentialFields(
		req.ExchangeType, req.APIKey, req.SecretKey, req.Passphrase,
		req.NTDataDir,
	)
	if len(missing) != 0 {
		t.Fatalf("expected 0 missing fields, got %v", missing)
	}
}

// TestCreateExchangeRequest_NinjaTraderMissingDataDir verifies the validator
// flags nt_data_dir as required for type=ninjatrader.
func TestCreateExchangeRequest_NinjaTraderMissingDataDir(t *testing.T) {
	missing := store.MissingRequiredExchangeCredentialFields(
		"ninjatrader", "", "", "",
		"", "", "", "",
		"", "",
		"", // empty NTDataDir
	)
	if len(missing) != 1 || missing[0] != "nt_data_dir" {
		t.Fatalf("expected [nt_data_dir] missing, got %v", missing)
	}
}
