package proxybridge

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

// StartSocksBridge starts a lightweight HTTP CONNECT proxy that forwards via the given socks5 URL.
// Returns local HTTP proxy URL and a stop function.
func StartSocksBridge(rawurl string) (string, func(), error) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return "", nil, fmt.Errorf("parse socks url: %w", err)
	}
	if u.Scheme != "socks5" && u.Scheme != "socks5h" {
		return "", nil, fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}

	auth := &proxy.Auth{}
	if u.User != nil {
		auth.User = u.User.Username()
		if p, ok := u.User.Password(); ok {
			auth.Password = p
		}
	}
	if auth.User == "" && auth.Password == "" {
		auth = nil
	}

	target := u.Host
	dialer, err := proxy.SOCKS5("tcp", target, auth, proxy.Direct)
	if err != nil {
		return "", nil, fmt.Errorf("create socks dialer: %w", err)
	}

	baseDial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		type c interface {
			DialContext(context.Context, string, string) (net.Conn, error)
		}
		if dctx, ok := dialer.(c); ok {
			return dctx.DialContext(ctx, network, addr)
		}
		return dialer.Dial(network, addr)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("listen: %w", err)
	}

	server := &http.Server{
		Handler: &bridgeHandler{dial: baseDial},
	}

	stopOnce := sync.Once{}
	stop := func() {
		stopOnce.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = server.Shutdown(ctx)
			_ = ln.Close()
		})
	}

	go func() { _ = server.Serve(ln) }()

	localURL := fmt.Sprintf("http://%s", ln.Addr().String())
	return localURL, stop, nil
}

type bridgeHandler struct {
	dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

func (h *bridgeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		h.handleConnect(w, r)
		return
	}
	h.handleHTTP(w, r)
}

func (h *bridgeHandler) handleConnect(w http.ResponseWriter, r *http.Request) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hj.Hijack()
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			_ = clientConn.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	targetConn, err := h.dial(ctx, "tcp", r.Host)
	if err != nil {
		return
	}

	_, _ = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	go io.Copy(targetConn, clientConn)
	go io.Copy(clientConn, targetConn)
}

func (h *bridgeHandler) handleHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	transport := &http.Transport{
		Proxy:       nil,
		DialContext: h.dial,
	}
	// Ensure URL has scheme/host
	if r.URL.Scheme == "" {
		r.URL.Scheme = "http"
	}
	if r.URL.Host == "" {
		r.URL.Host = r.Host
	}
	r.RequestURI = ""
	resp, err := transport.RoundTrip(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	br := bufio.NewReader(resp.Body)
	_, _ = br.WriteTo(w)
	_ = ctx
}
