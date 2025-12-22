package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warplib"
	"golang.org/x/net/websocket"
)

type WebServer struct {
	port   int
	l      *log.Logger
	m      *warplib.Manager
	pool   *Pool
	server *http.Server
	mu     sync.Mutex
}

type capturedDownload struct {
	Url     string          `json:"url"`
	Headers warplib.Headers `json:"headers"`
	Cookies []*http.Cookie  `json:"cookies"`
}

func NewWebServer(l *log.Logger, m *warplib.Manager, pool *Pool, port int) *WebServer {
	return &WebServer{port: port, l: l, m: m, pool: pool}
}

func (s *WebServer) processDownload(cd *capturedDownload) error {
	parsedURL, err := url.Parse(cd.Url)
	if err != nil {
		return err
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	client := &http.Client{
		Jar: jar,
	}
	client.Jar.SetCookies(parsedURL, cd.Cookies)
	var d *warplib.Downloader
	d, err = warplib.NewDownloader(client, cd.Url, &warplib.DownloaderOpts{
		Headers:        cd.Headers,
		MaxConnections: 24,
		MaxSegments:    200,
		Handlers: &warplib.Handlers{
			ErrorHandler: func(_ string, err error) {
				uid := d.GetHash()
				s.pool.Broadcast(uid, InitError(err))
				s.pool.WriteError(uid, ErrorTypeCritical, err.Error())
				s.pool.StopDownload(uid)
				d.Stop()
			},
			DownloadProgressHandler: func(hash string, nread int) {
				uid := d.GetHash()
				s.pool.Broadcast(uid, MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: uid,
					Action:     common.DownloadProgress,
					Value:      int64(nread),
					Hash:       hash,
				}))
			},
			DownloadCompleteHandler: func(hash string, tread int64) {
				uid := d.GetHash()
				s.pool.Broadcast(uid, MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: uid,
					Action:     common.DownloadComplete,
					Value:      tread,
					Hash:       hash,
				}))
			},
			DownloadStoppedHandler: func() {
				uid := d.GetHash()
				s.pool.Broadcast(uid, MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: uid,
					Action:     common.DownloadStopped,
				}))
			},
			CompileStartHandler: func(hash string) {
				uid := d.GetHash()
				s.pool.Broadcast(uid, MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: uid,
					Action:     common.CompileStart,
					Hash:       hash,
				}))
			},
			CompileProgressHandler: func(hash string, nread int) {
				uid := d.GetHash()
				s.pool.Broadcast(uid, MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: uid,
					Action:     common.CompileProgress,
					Value:      int64(nread),
					Hash:       hash,
				}))
			},
			CompileCompleteHandler: func(hash string, tread int64) {
				uid := d.GetHash()
				s.pool.Broadcast(uid, MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: uid,
					Action:     common.CompileComplete,
					Value:      tread,
					Hash:       hash,
				}))
			},
		},
	})
	if err != nil {
		return err
	}
	err = s.m.AddDownload(d, &warplib.AddDownloadOpts{
		AbsoluteLocation: d.GetDownloadDirectory(),
	})
	if err != nil {
		return err
	}
	s.pool.AddDownload(d.GetHash(), nil)
	go func(l *log.Logger) {
		err := d.Start()
		if err != nil {
			l.Println("Error starting download: ", err)
		}
	}(s.l)
	return nil
}

func (s *WebServer) handleConnection(conn *websocket.Conn) {
	defer conn.Close()
	for {
		var data []byte
		err := websocket.Message.Receive(conn, &data)
		if err != nil {
			if err == io.EOF {
				s.l.Println("Connection closed")
				return
			}
			s.l.Println("Error receiving message: ", err)
			return
		}
		var cd capturedDownload
		err = json.Unmarshal(data, &cd)
		if err != nil {
			s.l.Println("Error unmarshalling data: ", err)
			continue
		}
		err = s.processDownload(&cd)
		if err != nil {
			s.l.Println("Error processing download: ", err)
			continue
		}
	}
}

func (s *WebServer) handler() http.Handler {
	return websocket.Handler(s.handleConnection)
}

func (s *WebServer) addr() string {
	return fmt.Sprintf(":%d", s.port)
}

func (s *WebServer) Start() error {
	s.mu.Lock()
	s.server = &http.Server{
		Addr:    s.addr(),
		Handler: s.handler(),
	}
	s.mu.Unlock()

	err := s.server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil // Expected during shutdown
	}
	return err
}

// Shutdown gracefully stops the web server.
func (s *WebServer) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}
