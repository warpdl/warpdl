package warplib

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

// ProxyConfig holds the parsed proxy configuration.
type ProxyConfig struct {
	Scheme   string
	Host     string
	Username string
	Password string
}

// URL returns the proxy URL as a string.
func (p *ProxyConfig) URL() string {
	var sb strings.Builder
	sb.WriteString(p.Scheme)
	sb.WriteString("://")
	if p.Username != "" {
		sb.WriteString(p.Username)
		if p.Password != "" {
			sb.WriteString(":")
			sb.WriteString(p.Password)
		}
		sb.WriteString("@")
	}
	sb.WriteString(p.Host)
	return sb.String()
}

var (
	ErrEmptyProxyURL     = errors.New("proxy URL cannot be empty")
	ErrUnsupportedScheme = errors.New("unsupported proxy scheme")
	ErrInvalidProxyURL   = errors.New("invalid proxy URL")
)

var supportedSchemes = map[string]bool{
	"http":   true,
	"https":  true,
	"socks5": true,
}

// ParseProxyURL parses and validates a proxy URL string.
func ParseProxyURL(proxyURL string) (*ProxyConfig, error) {
	if proxyURL == "" {
		return nil, ErrEmptyProxyURL
	}

	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return nil, ErrInvalidProxyURL
	}

	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, ErrInvalidProxyURL
	}

	if !supportedSchemes[parsed.Scheme] {
		return nil, ErrUnsupportedScheme
	}

	config := &ProxyConfig{
		Scheme: parsed.Scheme,
		Host:   parsed.Host,
	}

	if parsed.User != nil {
		config.Username = parsed.User.Username()
		config.Password, _ = parsed.User.Password()
	}

	return config, nil
}

// NewHTTPClientWithProxy creates an HTTP client configured to use the specified proxy.
// If proxyURL is empty, returns a default HTTP client without proxy.
// The returned client always has CheckRedirect set to enforce redirect policy.
func NewHTTPClientWithProxy(proxyURL string) (*http.Client, error) {
	if proxyURL == "" {
		return &http.Client{
			CheckRedirect: RedirectPolicy(DefaultMaxRedirects),
		}, nil
	}

	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return nil, ErrInvalidProxyURL
	}

	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, ErrInvalidProxyURL
	}

	if !supportedSchemes[parsed.Scheme] {
		return nil, ErrUnsupportedScheme
	}

	transport := &http.Transport{}

	if parsed.Scheme == "socks5" {
		var auth *proxy.Auth
		if parsed.User != nil {
			pass, _ := parsed.User.Password()
			auth = &proxy.Auth{
				User:     parsed.User.Username(),
				Password: pass,
			}
		}
		dialer, err := proxy.SOCKS5("tcp", parsed.Host, auth, proxy.Direct)
		if err != nil {
			return nil, err
		}
		transport.Dial = dialer.Dial
	} else {
		transport.Proxy = http.ProxyURL(parsed)
	}

	return &http.Client{
		Transport:     transport,
		CheckRedirect: RedirectPolicy(DefaultMaxRedirects),
	}, nil
}

// NewHTTPClientFromEnvironment creates an HTTP client using proxy settings from environment variables.
// It checks HTTP_PROXY, http_proxy, HTTPS_PROXY, https_proxy, and ALL_PROXY.
func NewHTTPClientFromEnvironment() (*http.Client, error) {
	// Check for proxy environment variables
	proxyURL := os.Getenv("HTTP_PROXY")
	if proxyURL == "" {
		proxyURL = os.Getenv("http_proxy")
	}
	if proxyURL == "" {
		proxyURL = os.Getenv("HTTPS_PROXY")
	}
	if proxyURL == "" {
		proxyURL = os.Getenv("https_proxy")
	}
	if proxyURL == "" {
		proxyURL = os.Getenv("ALL_PROXY")
	}
	if proxyURL == "" {
		proxyURL = os.Getenv("all_proxy")
	}

	// Use ProxyFromEnvironment which handles NO_PROXY automatically
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
	}

	return &http.Client{
		Transport:     transport,
		CheckRedirect: RedirectPolicy(DefaultMaxRedirects),
	}, nil
}

// NewHTTPClientWithProxyAndTimeout creates an HTTP client with proxy and custom timeout.
// Timeout is specified in milliseconds.
func NewHTTPClientWithProxyAndTimeout(proxyURL string, timeoutMs int) (*http.Client, error) {
	client, err := NewHTTPClientWithProxy(proxyURL)
	if err != nil {
		return nil, err
	}

	client.Timeout = time.Duration(timeoutMs) * time.Millisecond
	return client, nil
}
