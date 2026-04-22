package common

import (
	"encoding/json"
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

func TestAuthListResultShape(t *testing.T) {
	r := AuthListResult{
		Accounts: []AuthAccount{
			{PluginID: "p", Account: "a", Scopes: []string{"s"}, ExpiresAt: 123},
		},
	}
	if len(r.Accounts) != 1 {
		t.Fatalf("len accounts = %d", len(r.Accounts))
	}
	if r.Accounts[0].PluginID != "p" {
		t.Fatalf("bad PluginID")
	}
}

func TestAuthUpdateConstantsAreDistinct(t *testing.T) {
	seen := map[UpdateType]string{}
	for name, v := range map[string]UpdateType{
		"UPDATE_AUTH_REQUIRED":   UPDATE_AUTH_REQUIRED,
		"UPDATE_AUTH_COMPLETED":  UPDATE_AUTH_COMPLETED,
		"UPDATE_AUTH_FAILED":     UPDATE_AUTH_FAILED,
		"UPDATE_AUTH_LOGGED_OUT": UPDATE_AUTH_LOGGED_OUT,
	} {
		if prev, dup := seen[v]; dup {
			t.Fatalf("constants %s and %s have same value %s", name, prev, v)
		}
		seen[v] = name
	}
}
