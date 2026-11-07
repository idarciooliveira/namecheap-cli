// Package ip finds the machine's public IPv4 address and provides an
// IPv4-only HTTP transport. Namecheap only whitelists IPv4, so every request
// must leave over IPv4 even on dual-stack networks.
package ip

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

var services = []string{
	"https://api.ipify.org",
	"https://checkip.amazonaws.com",
	"https://ifconfig.me/ip",
}

// Transport returns an HTTP transport that only dials IPv4.
func Transport() *http.Transport {
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			return d.DialContext(ctx, "tcp4", addr)
		},
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     60 * time.Second,
	}
}

// Public returns the public IPv4 address this machine uses for outbound traffic.
func Public(ctx context.Context) (string, error) {
	hc := &http.Client{Transport: Transport(), Timeout: 8 * time.Second}
	var lastErr error
	for _, s := range services {
		addr, err := fetch(ctx, hc, s)
		if err == nil {
			return addr, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no IP service reachable")
	}
	return "", lastErr
}

func fetch(ctx context.Context, hc *http.Client, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "namecheap-cli")
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(b))
	parsed := net.ParseIP(s)
	if parsed == nil || parsed.To4() == nil {
		return "", fmt.Errorf("%s: not an IPv4 address", u)
	}
	return parsed.To4().String(), nil
}
