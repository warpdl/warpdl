package api

import (
	"encoding/json"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/extl/auth"
	"github.com/warpdl/warpdl/internal/server"
)

// authListHandler enumerates every stored (plugin, account) credential
// across all active modules that expose an OAuth2 provider. For each
// account it reads back the token via AccountDetails so the CLI gets
// the granted scopes + expiry alongside the identifier.
//
// An account whose token can't be read (stale entry, decrypt error) is
// skipped rather than failing the whole listing — the CLI surfacing
// a partial list is more useful than an empty one.
//
// The result slice is always non-nil; the zero case is an empty slice,
// which marshals to `[]` rather than `null`.
func (s *Api) authListHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
	accounts := []common.AuthAccount{}
	for _, info := range s.elEngine.ListModules(false) {
		m := s.elEngine.GetModule(info.ExtensionId)
		if m == nil {
			continue
		}
		prov, ok := m.Provider().(*auth.OAuth2Provider)
		if !ok || prov == nil {
			continue
		}
		for _, acct := range prov.ListAccounts() {
			scopes, expiresAt, err := prov.AccountDetails(acct)
			if err != nil {
				continue
			}
			accounts = append(accounts, common.AuthAccount{
				PluginID:  info.ExtensionId,
				Account:   acct,
				Scopes:    scopes,
				ExpiresAt: expiresAt,
			})
		}
	}
	return common.UPDATE_AUTH_LIST, &common.AuthListResult{Accounts: accounts}, nil
}
