package api

import (
	"encoding/json"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/server"
)

// versionHandler returns the daemon's version information.
// It responds to UPDATE_VERSION requests with the version, commit hash,
// and build type that were set when the daemon was started.
func (s *Api) versionHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
	return common.UPDATE_VERSION, &common.VersionResponse{
		Version:   s.version,
		Commit:    s.commit,
		BuildType: s.buildType,
	}, nil
}
