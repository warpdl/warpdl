package service

import (
	"log"
	"net/http"

	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/warplib"
)

type Service struct {
	log     *log.Logger
	manager *warplib.Manager
	client  *http.Client
}

func NewService(l *log.Logger) (*Service, error) {
	m, err := warplib.InitManager()
	if err != nil {
		return nil, err
	}
	client := &http.Client{}
	return &Service{
		log:     l,
		manager: m,
		client:  client,
	}, nil
}

func (s *Service) RegisterHandlers(server *server.Server) {
	server.RegisterHandler("download", s.downloadHandler)
	server.RegisterHandler("list", s.listHandler)
}

func (s *Service) Close() error {
	return s.manager.Close()
}
