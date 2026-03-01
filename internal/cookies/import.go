package cookies

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ImportCookies imports cookies from a browser cookie store file for the given domain.
// It detects the format, copies SQLite files safely, parses cookies, and returns
// the filtered cookies and source metadata.
func ImportCookies(sourcePath string, domain string) ([]Cookie, *CookieSource, error) {
	format, err := DetectFormat(sourcePath)
	if err != nil {
		return nil, nil, err
	}

	source := &CookieSource{
		Path:   sourcePath,
		Format: format,
	}

	var cookies []Cookie

	switch format {
	case FormatFirefox:
		source.Browser = "Firefox"
		cookies, err = importSQLite(sourcePath, domain, ParseFirefox)
	case FormatChrome:
		source.Browser = "Chrome"
		cookies, err = importSQLite(sourcePath, domain, ParseChrome)
	case FormatNetscape:
		source.Browser = "Netscape"
		cookies, err = ParseNetscape(sourcePath, domain)
	default:
		return nil, nil, fmt.Errorf("error: unsupported cookie database schema at %s", sourcePath)
	}

	if err != nil {
		return nil, nil, err
	}

	return cookies, source, nil
}

// importSQLite copies a SQLite cookie file safely and parses it with the given parser.
func importSQLite(sourcePath, domain string, parser func(string, string) ([]Cookie, error)) ([]Cookie, error) {
	tempDir, cleanup, err := SafeCopy(sourcePath)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	copiedPath := filepath.Join(tempDir, filepath.Base(sourcePath))
	return parser(copiedPath, domain)
}

// BuildCookieHeader builds an HTTP Cookie header value from a slice of cookies.
// Format: "name1=val1; name2=val2"
func BuildCookieHeader(cookies []Cookie) string {
	if len(cookies) == 0 {
		return ""
	}

	parts := make([]string, len(cookies))
	for i, c := range cookies {
		parts[i] = c.Name + "=" + c.Value
	}
	return strings.Join(parts, "; ")
}
