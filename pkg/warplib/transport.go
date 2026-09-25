package warplib

import (
	"net"
	"net/http"
	"time"
)

const (
	// transportMaxIdleConnsPerHost keeps enough warm connections for the
	// default segment fan-out, so work-steal and slow-split children reuse
	// an established TCP/TLS session instead of paying a new handshake.
	transportMaxIdleConnsPerHost = 64
	transportMaxIdleConns        = 256
)

// NewTransport returns an HTTP/1.1 transport tuned for segmented downloads.
//
// Segmented downloading only helps when every part owns its own TCP
// connection. Over HTTP/2 Go multiplexes every range request onto one
// connection, so the parts share a single congestion window and a single
// server-side per-connection limit, which on a lossy or high-latency link is
// slower than downloading with one part. HTTP/1.1 keeps the parts
// independent.
func NewTransport() *http.Transport {
	// Built from scratch rather than cloned from http.DefaultTransport: once
	// that transport has been used its TLS config advertises "h2" via ALPN,
	// and a clone would negotiate HTTP/2 that an HTTP/1-only transport then
	// cannot speak.
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		Protocols:             protocols,
		MaxIdleConns:          transportMaxIdleConns,
		MaxIdleConnsPerHost:   transportMaxIdleConnsPerHost,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
}
