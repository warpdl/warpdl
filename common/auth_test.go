package common

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestAuthTypesRoundTripJSON(t *testing.T) {
	p := AuthLoginParams{
		PluginID:    "gd",
		Account:     "default",
		Scopes:      []string{"drive.readonly"},
		Flow:        "pkce",
		RedirectURI: "http://127.0.0.1:12345/callback",
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded AuthLoginParams
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.PluginID != p.PluginID || decoded.Flow != p.Flow {
		t.Fatalf("round-trip lost fields: %+v", decoded)
	}
}

// TestAuthListResultWireContract pins the JSON the daemon sends for
// UPDATE_AUTH_LIST: the accounts array, each account's wire keys, and
// the values surviving a decode.
func TestAuthListResultWireContract(t *testing.T) {
	r := AuthListResult{
		Accounts: []AuthAccount{
			{PluginID: "p", Account: "a", Scopes: []string{"s"}, ExpiresAt: 123},
		},
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("wire JSON is not an object: %v", err)
	}
	accountsRaw, ok := object["accounts"]
	if !ok {
		t.Fatalf("missing accounts key: %v", object)
	}
	var accounts []map[string]json.RawMessage
	if err := json.Unmarshal(accountsRaw, &accounts); err != nil {
		t.Fatalf("accounts is not an array of objects: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("accounts rows = %d, want 1", len(accounts))
	}
	for _, key := range []string{"plugin_id", "account", "scopes", "expires_at"} {
		if _, ok := accounts[0][key]; !ok {
			t.Fatalf("missing wire key %q: %v", key, accounts[0])
		}
	}
	if len(accounts[0]) != 4 {
		t.Fatalf("unexpected wire keys: %v", accounts[0])
	}

	var decoded AuthListResult
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(decoded.Accounts, r.Accounts) {
		t.Fatalf("round-trip lost fields: %+v", decoded.Accounts)
	}
}

func TestAuthUpdateConstantsAreDistinct(t *testing.T) {
	seen := map[UpdateType]string{}
	for name, v := range map[string]UpdateType{
		"UPDATE_AUTH_REQUIRED":   UPDATE_AUTH_REQUIRED,
		"UPDATE_AUTH_COMPLETED":  UPDATE_AUTH_COMPLETED,
		"UPDATE_AUTH_FAILED":     UPDATE_AUTH_FAILED,
		"UPDATE_AUTH_LOGGED_OUT": UPDATE_AUTH_LOGGED_OUT,
		"UPDATE_AUTH_LIST":       UPDATE_AUTH_LIST,
	} {
		if prev, dup := seen[v]; dup {
			t.Fatalf("constants %s and %s have same value %s", name, prev, v)
		}
		seen[v] = name
	}
}
