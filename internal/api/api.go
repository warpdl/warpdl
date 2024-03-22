package api

import (
	"log"
	"net/http"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/extloader"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/warplib"
)

type Api struct {
	log      *log.Logger
	manager  *warplib.Manager
	elEngine *extloader.Engine
	client   *http.Client
}

func NewApi(l *log.Logger, elEngine *extloader.Engine) (*Api, error) {
	m, err := warplib.InitManager()
	if err != nil {
		return nil, err
	}
	client := &http.Client{}
	return &Api{
		log:      l,
		manager:  m,
		client:   client,
		elEngine: elEngine,
	}, nil
}

func (s *Api) RegisterHandlers(server *server.Server) {
	// downloader API methods
	server.RegisterHandler(common.UPDATE_DOWNLOAD, s.downloadHandler)
	server.RegisterHandler(common.UPDATE_RESUME, s.resumeHandler)
	server.RegisterHandler(common.UPDATE_ATTACH, s.attachHandler)
	server.RegisterHandler(common.UPDATE_FLUSH, s.flushHandler)
	server.RegisterHandler(common.UPDATE_STOP, s.stopHandler)
	server.RegisterHandler(common.UPDATE_LIST, s.listHandler)

	// extension API methods
	server.RegisterHandler(common.UPDATE_LOAD_EXT, s.loadExtHandler)
}

func (s *Api) Close() error {
	return s.manager.Close()
}
