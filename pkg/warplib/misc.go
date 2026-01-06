package warplib

import (
	"errors"
	"fmt"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Size unit constants for byte conversions.
const (
	// B represents one byte.
	B int64 = 1
	// KB represents one kilobyte (1024 bytes).
	KB = 1024 * B
	// MB represents one megabyte (1024 kilobytes).
	MB = 1024 * KB
	// GB represents one gigabyte (1024 megabytes).
	GB = 1024 * MB
	// TB represents one terabyte (1024 gigabytes).
	TB = 1024 * GB
)

const (
	_SECOND = int64(time.Second)
)

const (
	DEF_MAX_CONNS  = 1
	DEF_CHUNK_SIZE = 32 * KB
	DEF_USER_AGENT = "Warp/1.0"

	MIN_PART_SIZE = 512 * KB

	// DEF_MAX_FILE_SIZE is the default maximum file size limit (100GB).
	// Set to -1 to disable the limit.
	DEF_MAX_FILE_SIZE = 100 * GB
)

// getMinPartSize returns the minimum part size based on file size.
// Larger files use larger minimum part sizes to improve stability.
//
// Thresholds:
//   - <100MB:    512KB
//   - 100MB-1GB: 1MB
//   - 1GB-10GB:  2MB
//   - >10GB:     4MB (max cap)
func getMinPartSize(contentLength int64) int64 {
	switch {
	case contentLength <= 0:
		return 512 * KB
	case contentLength < 100*MB:
		return 512 * KB
	case contentLength < 1*GB:
		return 1 * MB
	case contentLength < 10*GB:
		return 2 * MB
	default:
		return 4 * MB
	}
}

const (

	// DefaultFileMode is the permission mode for created files.
	// Owner can read/write, group and others can only read.
	DefaultFileMode = 0644

	// DefaultDirMode is the permission mode for created directories.
	// Owner has full access, group and others can read and traverse.
	DefaultDirMode = 0755
)

// MAIN_HASH is the identifier used for the main download hash.
const MAIN_HASH = "main"

// ConfigDirEnv is the environment variable name used to override the default configuration directory.
const ConfigDirEnv = "WARPDL_CONFIG_DIR"

var (
	// ConfigDir is the absolute path to the warp configuration directory.
	ConfigDir string
	// DlDataDir is the absolute path to the download data directory where segment files are stored.
	DlDataDir string
)

func init() {
	dir := os.Getenv(ConfigDirEnv)
	if err := initConfigDir(dir); err != nil {
		log.Fatalf("fatal: failed to initialize config directory: %v", err)
	}
}

// initConfigDir initializes the configuration directory with fallback logic.
// It attempts to use the provided dir, falls back to defaultConfigDir, and finally to temp dir.
func initConfigDir(dir string) error {
	if dir == "" {
		var err error
		dir, err = defaultConfigDir()
		if err != nil {
			log.Printf("warning: failed to get default config dir: %v, falling back to temp dir", err)
			dir = os.TempDir()
		}
	}

	if err := setConfigDir(dir); err != nil {
		// Fallback to temp directory
		log.Printf("warning: failed to set config dir to %s: %v, falling back to temp dir", dir, err)
		tempDir := os.TempDir()
		if err2 := setConfigDir(tempDir); err2 != nil {
			return fmt.Errorf("failed to initialize config: %w (fallback to temp dir also failed: %v)", err, err2)
		}
		log.Printf("info: using temp directory for config: %s", tempDir)
	}

	return nil
}

// defaultConfigDir returns the default configuration directory path.
// It returns an error instead of panicking if the directory cannot be determined or created.
func defaultConfigDir() (string, error) {
	cdr, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("get user config dir: %w", err)
	}
	if !dirExists(cdr) {
		if err := WarpMkdirAll(cdr, 0755); err != nil {
			return "", fmt.Errorf("create config parent dir: %w", err)
		}
	}
	return filepath.Join(cdr, "warpdl"), nil
}

func setConfigDir(dir string) error {
	if dir == "" {
		return errors.New("config dir is empty")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err := WarpMkdirAll(abs, 0755); err != nil {
		return err
	}
	ConfigDir = abs
	DlDataDir = filepath.Join(abs, "dldata")
	if err := WarpMkdirAll(DlDataDir, 0755); err != nil {
		return err
	}
	__USERDATA_FILE_NAME = filepath.Join(abs, "userdata.warp")
	return nil
}

// SetConfigDir sets the configuration directory to the specified path.
// It creates the directory and its subdirectories if they do not exist.
func SetConfigDir(dir string) error {
	return setConfigDir(dir)
}

// GetPath joins a directory and file name using the OS-specific path separator.
func GetPath(directory, file string) string {
	return filepath.Join(directory, file)
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
	if fn == "" {
		pa := strings.Split(req.URL.Path, "/")
		fn = pa[len(pa)-1]
	}
	fn = SanitizeFilename(fn)
	return
}

// SanitizeFilename removes or replaces characters invalid on Windows/Unix filesystems.
// It preserves the file extension and handles URL-encoded characters.
func SanitizeFilename(name string) string {
	if name == "" {
		return name
	}

	// URL-decode first (handles %3F for ?, etc.)
	if decoded, err := url.PathUnescape(name); err == nil {
		name = decoded
	}

	// Invalid chars on Windows: < > : " / \ | ? *
	invalidChars := []string{"<", ">", ":", "\"", "/", "\\", "|", "?", "*"}
	for _, char := range invalidChars {
		name = strings.ReplaceAll(name, char, "_")
	}

	// Remove control characters (0x00-0x1F)
	var result strings.Builder
	for _, r := range name {
		if r >= 32 {
			result.WriteRune(r)
		}
	}
	name = result.String()

	// Handle Windows reserved names (case-insensitive)
	baseName, ext := name, ""
	if idx := strings.LastIndex(name, "."); idx > 0 {
		baseName, ext = name[:idx], name[idx:]
	}

	reserved := []string{
		"CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9",
	}
	for _, r := range reserved {
		if strings.EqualFold(baseName, r) {
			baseName = "_" + baseName
			break
		}
	}
	name = baseName + ext

	// Trim leading/trailing spaces and dots (Windows restriction)
	name = strings.Trim(name, " .")

	if name == "" {
		name = "download"
	}
	return name
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
	return filepath.Join(preName, hash+".warp")
}

func dirExists(name string) bool {
	_, err := os.ReadDir(name)
	return !os.IsNotExist(err)
}

// Place inserts element e at the specified index in src and returns a new slice.
// The original slice is not modified.
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
