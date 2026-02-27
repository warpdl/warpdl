package warplib

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// SchemeRouter maps URL schemes to DownloaderFactory implementations.
// It is the central dispatch point for protocol-agnostic download creation.
// The zero value is not usable; use NewSchemeRouter to create one.
type SchemeRouter struct {
	routes map[string]DownloaderFactory
}

// NewSchemeRouter creates a SchemeRouter pre-configured with HTTP and HTTPS factories
// that use the provided HTTP client.
func NewSchemeRouter(client *http.Client) *SchemeRouter {
	if client == nil {
		client = http.DefaultClient
	}
	r := &SchemeRouter{
		routes: make(map[string]DownloaderFactory),
	}
	// Register http/https using a closure that captures the client.
	httpFactory := func(rawURL string, opts *DownloaderOpts) (ProtocolDownloader, error) {
		return newHTTPProtocolDownloader(rawURL, opts, client)
	}
	r.routes["http"] = httpFactory
	r.routes["https"] = httpFactory

	// Register ftp/ftps factories.
	ftpFactory := func(rawURL string, opts *DownloaderOpts) (ProtocolDownloader, error) {
		return newFTPProtocolDownloader(rawURL, opts)
	}
	r.routes["ftp"] = ftpFactory
	r.routes["ftps"] = ftpFactory

	return r
}

// Register adds or replaces the factory for the given scheme.
// scheme must be lowercase (e.g., "ftp", "sftp").
func (r *SchemeRouter) Register(scheme string, factory DownloaderFactory) {
	r.routes[strings.ToLower(scheme)] = factory
}

// NewDownloader creates a ProtocolDownloader for the given raw URL.
// The scheme is extracted from the URL (case-insensitive: HTTP:// is treated as http://).
// Returns an error if the scheme is unsupported or the URL is invalid.
func (r *SchemeRouter) NewDownloader(rawURL string, opts *DownloaderOpts) (ProtocolDownloader, error) {
	if rawURL == "" {
		return nil, fmt.Errorf("%w: empty URL", ErrUnsupportedDownloadScheme)
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", rawURL, err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme == "" {
		return nil, fmt.Errorf("%w: no scheme in URL %q", ErrUnsupportedDownloadScheme, rawURL)
	}

	factory, ok := r.routes[scheme]
	if !ok {
		supported := SupportedSchemes(r)
		return nil, fmt.Errorf(
			"%w %q — supported: %s",
			ErrUnsupportedDownloadScheme,
			scheme,
			strings.Join(supported, ", "),
		)
	}

	return factory(rawURL, opts)
}

// SupportedSchemes returns a sorted list of all registered schemes.
func SupportedSchemes(r *SchemeRouter) []string {
	schemes := make([]string, 0, len(r.routes))
	for s := range r.routes {
		schemes = append(schemes, s)
	}
	sort.Strings(schemes)
	return schemes
}
