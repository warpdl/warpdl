package warplib

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewTransportUsesHTTP1OverTLS(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Proto)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()

	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	transport := NewTransport()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots}
	client := &http.Client{Transport: transport}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.ProtoMajor != 1 || string(body) != "HTTP/1.1" {
		t.Fatalf("negotiated %s (server saw %s), want HTTP/1.1", resp.Proto, body)
	}
	if transport.MaxIdleConnsPerHost < 24 {
		t.Fatalf("MaxIdleConnsPerHost = %d, want room for the default connection fan-out",
			transport.MaxIdleConnsPerHost)
	}
}

func TestNewHTTPClientWithProxyUsesDownloadTransport(t *testing.T) {
	client, err := NewHTTPClientWithProxy("")
	if err != nil {
		t.Fatalf("NewHTTPClientWithProxy: %v", err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Protocols == nil || transport.Protocols.HTTP2() {
		t.Fatalf("direct client transport = %#v, want HTTP/1-only download transport", client.Transport)
	}

	client, err = NewHTTPClientWithProxy("socks5://127.0.0.1:1080")
	if err != nil {
		t.Fatalf("NewHTTPClientWithProxy socks5: %v", err)
	}
	transport = client.Transport.(*http.Transport)
	//nolint:staticcheck // asserting the SOCKS5 dialer installed by the constructor
	if transport.DialContext != nil || transport.Dial == nil || transport.Proxy != nil {
		t.Fatal("SOCKS5 transport must dial only through the proxy")
	}
}
