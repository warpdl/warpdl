package warplib

import (
	"errors"
	"fmt"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	B  int64 = 1
	KB       = 1024 * B
	MB       = 1024 * KB
	GB       = 1024 * MB
	TB       = 1024 * GB
)

const (
	_SECOND = int64(time.Second)
)

const (
	DEF_MAX_CONNS  = 1
	DEF_CHUNK_SIZE = 32 * KB
	DEF_USER_AGENT = "Warp/1.0"

	MIN_PART_SIZE = 512 * KB
)

const MAIN_HASH = "main"

const ConfigDirEnv = "WARPDL_CONFIG_DIR"

var (
	ConfigDir string
	DlDataDir string
)

func init() {
	dir := os.Getenv(ConfigDirEnv)
	if dir == "" {
		dir = defaultConfigDir()
	}
	if err := setConfigDir(dir); err != nil {
		panic(err)
	}
}

func defaultConfigDir() string {
	cdr, err := os.UserConfigDir()
	if err != nil {
		panic(err)
	}
	if !dirExists(cdr) {
		err = os.MkdirAll(cdr, os.ModePerm)
		if err != nil {
			panic(err)
		}
	}
	return filepath.Join(cdr, "warpdl")
}

func setConfigDir(dir string) error {
	if dir == "" {
		return errors.New("config dir is empty")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(abs, os.ModePerm); err != nil {
		return err
	}
	ConfigDir = abs
	DlDataDir = filepath.Join(abs, "dldata")
	if err := os.MkdirAll(DlDataDir, os.ModePerm); err != nil {
		return err
	}
	__USERDATA_FILE_NAME = filepath.Join(abs, "userdata.warp")
	return nil
}

func SetConfigDir(dir string) error {
	return setConfigDir(dir)
}

func GetPath(directory, file string) (path string) {
	path = strings.Join(
		[]string{
			directory, file,
		},
		"/",
	)
	return
}

func getSpeed(op func() error) (te time.Duration, err error) {
	tn := time.Now()
	err = op()
	if err != nil {
		return
	}
	te = time.Since(tn)
	return
}

func parseFileName(req *http.Request, cd string) (fn string) {
	if cd != "" {
		_, p, err := mime.ParseMediaType(cd)
		if err == nil {
			fn = p["filename"]
		}
	}
	if fn != "" {
		return
	}
	pa := strings.Split(req.URL.Path, "/")
	fn = pa[len(pa)-1]
	return
}

// Logic:
// sps = 1MB, tdb = 32KB
//
// 1MB => 1024*1024 B
// 1MB/s => 1024*1024 B per sec
// 32KB => 32*1024
//
// IF { 1 MB => 1 s } THEN { (1024)^2 B => 10^9 ns }
// HENCE { 1 b => 10^9 / 1024^2 }
//
// 32*1024 b => (10^9 * 32*1024) / 1024^2 ns
func getDownloadTime(sps int64, tdb int64) (eta time.Duration) {
	eta = time.Duration(((_SECOND * tdb) / sps))
	return
}

func getFileName(preName, hash string) string {
	return fmt.Sprintf("%s%s.warp", preName, hash)
}

func dirExists(name string) bool {
	_, err := os.ReadDir(name)
	return !os.IsNotExist(err)
}

func Place[t any](src []t, e t, index int) (dst []t) {
	dst = make([]t, len(src)+1)
	var o int
	for i, x := range src {
		if o == 0 && i == index {
			dst[i] = e
			o = 1
		}
		dst[i+o] = x
	}
	return
}

// type duration struct {
// 	t time.Duration
// 	n int64
// }

// func (d *duration) avg(t time.Duration) {
// 	d.n++
// 	d.t = time.Duration(int64(d.t+t) / d.n)
// }

// func (d *duration) get() (avg time.Duration) {
// 	avg = d.t
// 	return
// }

// var CacheDir = func() (warpDir string) {
// 	cdr, err := os.UserCacheDir()
// 	if err != nil {
// 		panic(err)
// 	}
// 	warpDir = cdr + "/warp"
// 	if dirExists(warpDir) {
// 		return
// 	}
// 	err = os.Mkdir(warpDir, os.ModePerm)
// 	if err != nil {
// 		panic(err)
// 	}
// 	return
// }()

func wlog(l *log.Logger, s string, a ...any) {
	esc := "\n"
	switch runtime.GOOS {
	case "windows":
		esc = "\r\n"
	}
	l.Printf(s+esc, a...)
}
