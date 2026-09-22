package warplib

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/proxy"
)

type ifaceClient struct {
	name      string
	ip        net.IP
	client    *http.Client
	transport *http.Transport
}

type partIfaceState struct {
	index   int
	oneShot bool
	tried   map[int]bool
}

func (d *Downloader) selectPinnedInterfaces(bindings []InterfaceBinding, explicit bool) ([]InterfaceBinding, error) {
	kept := make([]InterfaceBinding, 0, len(bindings))
	for _, binding := range bindings {
		if err := d.probeInterfacePin(binding); err != nil {
			d.Log("%s %s: %v", logSkippedInterface, binding.Name, err)
			if explicit {
				return nil, fmt.Errorf("%w: %s", ErrInterfacePinRefused, binding.Name)
			}
			continue
		}
		kept = append(kept, binding)
	}
	return kept, nil
}

func (d *Downloader) probeInterfacePin(binding InterfaceBinding) error {
	ctx := d.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	conn, err := d.dialBound(ctx, "", binding)
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil || errors.Is(err, ErrInterfacePinRefused) {
		return err
	}
	return fmt.Errorf("%w: %s: %v", ErrInterfacePinRefused, binding.Name, err)
}

func (d *Downloader) dialBound(ctx context.Context, address string, binding InterfaceBinding) (net.Conn, error) {
	if d.interfaceDial != nil {
		return d.interfaceDial(ctx, "tcp4", address, binding.IP, binding.Name)
	}
	return defaultInterfaceDial(ctx, "tcp4", address, binding.IP, binding.Name)
}

func defaultInterfaceDial(ctx context.Context, network, address string, local net.IP, device string) (net.Conn, error) {
	if address == "" {
		return nil, probeDevicePin(device)
	}
	ip := local.To4()
	if ip == nil {
		return nil, fmt.Errorf("%w: %s", ErrInterfaceNoIPv4, device)
	}
	dialer := &net.Dialer{
		LocalAddr:     &net.TCPAddr{IP: ip},
		FallbackDelay: -1, // IPv4 only; do not race an IPv6 dial
		Control: func(network, address string, conn syscall.RawConn) error {
			var pinErr error
			if err := conn.Control(func(fd uintptr) {
				pinErr = pinSocket(fd, device)
			}); err != nil {
				return err
			}
			return pinErr
		},
	}
	if network == "" {
		network = "tcp4"
	}
	return dialer.DialContext(ctx, "tcp4", address)
}

func (d *Downloader) buildInterfaceClients(bindings []InterfaceBinding) ([]ifaceClient, error) {
	clients := make([]ifaceClient, 0, len(bindings))
	for _, binding := range bindings {
		client, err := d.buildInterfaceClient(binding)
		if err != nil {
			for _, existing := range clients {
				if existing.transport != nil {
					existing.transport.CloseIdleConnections()
				}
			}
			return nil, err
		}
		clients = append(clients, client)
	}
	return clients, nil
}

func (d *Downloader) buildInterfaceClient(binding InterfaceBinding) (ifaceClient, error) {
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		return d.dialBound(ctx, addr, binding)
	}
	transport, err := transportForInterface(d.client, d.proxyURL, dial)
	if err != nil {
		return ifaceClient{}, err
	}
	client := &http.Client{Transport: transport}
	if d.client != nil {
		client.Jar = d.client.Jar
		client.CheckRedirect = d.client.CheckRedirect
		client.Timeout = d.client.Timeout
	}
	return ifaceClient{
		name:      binding.Name,
		ip:        append(net.IP(nil), binding.IP...),
		client:    client,
		transport: transport,
	}, nil
}

type contextForwardDialer struct {
	dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

func (d contextForwardDialer) Dial(network, addr string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, addr)
}

func (d contextForwardDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return d.dial(ctx, network, addr)
}

func transportForInterface(base *http.Client, proxyURL string, dial func(ctx context.Context, network, addr string) (net.Conn, error)) (*http.Transport, error) {
	transport := &http.Transport{
		DialContext:         dial,
		ForceAttemptHTTP2:   false,
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     90 * time.Second,
		MaxIdleConnsPerHost: interfaceMaxWorkersPerIface,
	}
	if proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidProxyURL, err)
		}
		if strings.EqualFold(parsed.Scheme, "socks5") {
			var auth *proxy.Auth
			if parsed.User != nil {
				password, _ := parsed.User.Password()
				auth = &proxy.Auth{User: parsed.User.Username(), Password: password}
			}
			// The SOCKS handshake dials through the same bound, context-aware
			// dialer as a direct HTTP connection, so the pin is the socket to
			// the proxy rather than a later socket the proxy opens.
			socksDialer, err := proxy.SOCKS5("tcp", parsed.Host, auth, contextForwardDialer{dial: dial})
			if err != nil {
				return nil, err
			}
			transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				if contextDialer, ok := socksDialer.(proxy.ContextDialer); ok {
					return contextDialer.DialContext(ctx, "tcp4", addr)
				}
				return socksDialer.Dial("tcp4", addr)
			}
			return transport, nil
		}
		transport.Proxy = http.ProxyURL(parsed)
		return transport, nil
	}
	baseTransport := http.DefaultTransport
	if base != nil && base.Transport != nil {
		baseTransport = base.Transport
	}
	if httpTransport, ok := baseTransport.(*http.Transport); ok && httpTransport != nil {
		transport.Proxy = httpTransport.Proxy
		transport.TLSClientConfig = httpTransport.TLSClientConfig
		transport.DisableCompression = httpTransport.DisableCompression
	}
	return transport, nil
}

func (d *Downloader) closeInterfaceClients() {
	if d == nil {
		return
	}
	for _, slot := range d.ifaceClients {
		if slot.transport != nil {
			slot.transport.CloseIdleConnections()
		}
	}
}

func (d *Downloader) bondedWorkerCount() int {
	nIface := len(d.ifaceClients)
	if nIface == 0 {
		return 0
	}
	limit := int(d.maxConn)
	if limit <= 0 {
		// Callers that pass no connection limit already mean DEF_MAX_CONNS.
		// Do not substitute the CLI default of 24 here.
		limit = int(DEF_MAX_CONNS)
	}
	if limit > interfaceMaxWorkers {
		limit = interfaceMaxWorkers
	}
	if capByIface := nIface * interfaceMaxWorkersPerIface; limit > capByIface {
		limit = capByIface
	}
	if limit < 1 {
		limit = 1
	}
	return limit
}

func (d *Downloader) assignPartInterface(hash string, index int) {
	d.partIfaceMu.Lock()
	defer d.partIfaceMu.Unlock()
	if d.partIface == nil {
		d.partIface = make(map[string]*partIfaceState)
	}
	d.partIface[hash] = &partIfaceState{
		index: index,
		tried: make(map[int]bool),
	}
}

func (d *Downloader) partInterfaceOneShot(hash string) bool {
	d.partIfaceMu.Lock()
	defer d.partIfaceMu.Unlock()
	state := d.partIface[hash]
	return state != nil && state.oneShot
}

func (d *Downloader) shiftPartInterface(part *Part, cause error) bool {
	if part == nil || !d.movableInterfaceError(cause) || len(d.ifaceClients) < 2 {
		return false
	}
	d.partIfaceMu.Lock()
	state := d.partIface[part.hash]
	if state == nil {
		d.partIfaceMu.Unlock()
		return false
	}
	state.tried[state.index] = true
	next := -1
	for i := range d.ifaceClients {
		if !state.tried[i] {
			next = i
			break
		}
	}
	if next < 0 {
		d.partIfaceMu.Unlock()
		return false
	}
	from := d.ifaceClients[state.index].name
	to := d.ifaceClients[next].name
	client := d.ifaceClients[next].client
	state.index = next
	state.oneShot = true
	d.partIfaceMu.Unlock()
	part.client = client
	d.Log("%s: %s %s to %s", part.hash, logMovedPart, from, to)
	return true
}

func (d *Downloader) movableInterfaceError(err error) bool {
	if err == nil || !d.multiActive {
		return false
	}
	// A response that means the resource changed or the request was rejected
	// stays on the interface that received it and fails the download.
	if errors.Is(err, ErrResourceChanged) || errors.Is(err, ErrInvalidRangeResponse) || errors.Is(err, ErrDownloadSizeMismatch) {
		return false
	}
	var httpErr *HTTPStatusError
	if errors.As(err, &httpErr) {
		return false
	}
	return ClassifyError(err) == ErrCategoryRetryable
}

func (d *Downloader) refreshSpeedShares() {
	if d == nil || !d.multiActive {
		return
	}
	share := d.currentPartSpeedLimit()
	for _, part := range d.snapshotSpeedParts() {
		part.applySpeedLimit(share)
	}
}
