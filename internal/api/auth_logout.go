package api

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/extl/auth"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/credman/types"
)

// authLogoutHandler deletes the stored credential for a (plugin,
// account) pair and, if the plugin configured a revoke_url, asks the
// IdP to invalidate the refresh/access token server-side. The in-store
// deletion is the source of truth; revoke is best-effort.
func (s *Api) authLogoutHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
	var p common.AuthLogoutParams
	if err := json.Unmarshal(body, &p); err != nil {
		return common.UPDATE_AUTH_LOGGED_OUT, nil, err
	}
	if p.PluginID == "" {
		return common.UPDATE_AUTH_LOGGED_OUT, nil, errors.New("plugin_id required")
	}
	m := s.elEngine.GetModule(p.PluginID)
	if m == nil {
		return common.UPDATE_AUTH_LOGGED_OUT, nil, fmt.Errorf("unknown plugin: %s", p.PluginID)
	}
	prov, ok := m.Provider().(*auth.OAuth2Provider)
	if !ok || prov == nil {
		return common.UPDATE_AUTH_LOGGED_OUT, nil, fmt.Errorf("plugin %s has no auth provider", p.PluginID)
	}

	key := types.TokenKey{PluginID: p.PluginID, Account: p.Account}.WithDefaultAccount()
	if err := prov.Logout(s.daemonCtx(), key); err != nil {
		return common.UPDATE_AUTH_LOGGED_OUT, nil, err
	}
	return common.UPDATE_AUTH_LOGGED_OUT, nil, nil
}
