package cmd

import (
	"encoding/hex"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"

	"github.com/warpdl/warpdl/internal/api"
	"github.com/warpdl/warpdl/internal/extl"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/credman"
	"github.com/warpdl/warpdl/pkg/logger"
	"github.com/warpdl/warpdl/pkg/warplib"
)

type loggerKeyringAdapter struct {
	log logger.Logger
}

func (l *loggerKeyringAdapter) Warning(format string, args ...interface{}) {
	l.log.Warning(format, args...)
}

// DaemonComponents holds all initialized daemon components.
// This allows for unified initialization and cleanup across
// console mode and Windows service mode.
type DaemonComponents struct {
	CookieManager *credman.CookieManager
	ExtEngine     *extl.Engine
	Manager       *warplib.Manager
	Api           *api.Api
	Server        *server.Server
	logger        logger.Logger
	stdLogger     interface{ Println(v ...interface{}) }
}

// Close releases all daemon component resources in reverse order of initialization.
// This ensures proper cleanup regardless of how the daemon was started.
func (c *DaemonComponents) Close() {
	if c.stdLogger != nil {
		c.stdLogger.Println("Shutting down daemon...")
	}

	// Stop all active downloads (progress is auto-persisted via UpdateItem)
	if c.Manager != nil {
		for _, item := range c.Manager.GetItems() {
			if item.IsDownloading() {
				if c.stdLogger != nil {
					c.stdLogger.Println("Stopping download:", item.Hash)
				}
				item.StopDownload()
			}
		}
	}

	// Close API (closes manager, flushes state)
	if c.Api != nil {
		_ = c.Api.Close()
	}

	// Close extension engine
	if c.ExtEngine != nil {
		c.ExtEngine.Close()
	}

	// Close cookie manager
	if c.CookieManager != nil {
		_ = c.CookieManager.Close()
	}

	if c.stdLogger != nil {
		c.stdLogger.Println("Daemon stopped")
	}
}

// initDaemonComponents initializes all daemon components with the provided logger.
// This is the shared initialization used by both console mode and Windows service mode.
// Returns the initialized components or an error if initialization fails.
//
// On error, any partially initialized components are cleaned up before returning.
var initDaemonComponents = func(log logger.Logger) (*DaemonComponents, error) {
	stdLog := logger.ToStdLogger(log)

	// Initialize cookie manager
	cm, err := getCookieManagerWithLogger(log)
	if err != nil {
		return nil, err
	}

	// Initialize extension engine
	elEng, err := extl.NewEngine(stdLog, cm, false)
	if err != nil {
		log.Error("Extension engine initialization failed: %v", err)
		cm.Close()
		return nil, err
	}

	// Create HTTP client with cookie jar
	jar, err := cookiejar.New(nil)
	if err != nil {
		log.Error("Cookie jar creation failed: %v", err)
		elEng.Close()
		cm.Close()
		return nil, err
	}
	client := &http.Client{Jar: jar}

	// Initialize warplib manager
	m, err := warplib.InitManager()
	if err != nil {
		log.Error("WarpLib manager initialization failed: %v", err)
		elEng.Close()
		cm.Close()
		return nil, err
	}

	// Create API
	s, err := api.NewApi(stdLog, m, client, elEng,
		currentBuildArgs.Version, currentBuildArgs.Commit, currentBuildArgs.BuildType)
	if err != nil {
		log.Error("API initialization failed: %v", err)
		m.Close()
		elEng.Close()
		cm.Close()
		return nil, err
	}

	// Create server
	serv := server.NewServer(stdLog, m, DEF_PORT)
	s.RegisterHandlers(serv)

	return &DaemonComponents{
		CookieManager: cm,
		ExtEngine:     elEng,
		Manager:       m,
		Api:           s,
		Server:        serv,
		logger:        log,
		stdLogger:     stdLog,
	}, nil
}

// getCookieManagerWithLogger initializes the cookie manager using the Logger interface.
// This is used in service mode where cli.Context is not available.
func getCookieManagerWithLogger(log logger.Logger) (*credman.CookieManager, error) {
	if keyHex := os.Getenv(cookieKeyEnv); keyHex != "" {
		key, err := hex.DecodeString(keyHex)
		if err != nil {
			log.Error("Invalid cookie key hex: %v", err)
			return nil, err
		}
		cookieFile := filepath.Join(warplib.ConfigDir, "cookies.warp")
		cm, err := credman.NewCookieManager(cookieFile, key)
		if err != nil {
			log.Error("Cookie manager initialization failed: %v", err)
			return nil, err
		}
		return cm, nil
	}

	kr := newKeyring(warplib.ConfigDir, &loggerKeyringAdapter{log: log})
	key, err := kr.GetKey()
	if err != nil {
		key, err = kr.SetKey()
		if err != nil {
			log.Error("Keyring initialization failed: %v", err)
			return nil, err
		}
	}

	cookieFile := filepath.Join(warplib.ConfigDir, "cookies.warp")
	cm, err := credman.NewCookieManager(cookieFile, key)
	if err != nil {
		log.Error("Cookie manager initialization failed: %v", err)
		return nil, err
	}
	return cm, nil
}
