package core

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var errScriptURLDenied = errors.New("script URL is not permitted")
var errScriptURLInvalid = errors.New("invalid script URL")
var errScriptFetch = errors.New("could not fetch a bounded script response")

type scriptFetcher struct {
	origins map[string]bool
	lookup  func(context.Context, string) ([]net.IPAddr, error)
}

func parseScriptURL(raw string) (*url.URL, error) {
	if len(raw) > 2048 {
		return nil, errScriptURLDenied
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || u.Scheme != "http" && u.Scheme != "https" || strings.Contains(u.Hostname(), "%") {
		return nil, errScriptURLDenied
	}
	if u.Port() != "" {
		p, err := strconv.Atoi(u.Port())
		if err != nil || p < 1 || p > 65535 {
			return nil, errScriptURLDenied
		}
	}
	return u, nil
}
func scriptOrigin(u *url.URL) string { return u.Scheme + "://" + strings.ToLower(u.Host) }

func newScriptFetcher(origins []string) (*scriptFetcher, error) {
	f := &scriptFetcher{origins: make(map[string]bool), lookup: net.DefaultResolver.LookupIPAddr}
	if len(origins) > 32 {
		return nil, errors.New("at most 32 script URL origins are allowed")
	}
	for _, origin := range origins {
		u, err := parseScriptURL(origin)
		if err != nil || u.RawQuery != "" || u.ForceQuery || u.Path != "" && u.Path != "/" {
			return nil, errors.New("scriptURLOrigins must contain HTTP(S) origins without credentials, paths or queries")
		}
		f.origins[scriptOrigin(u)] = true
	}
	return f, nil
}

func (f *scriptFetcher) allowed(u *url.URL) bool {
	parsed, err := parseScriptURL(u.String())
	return err == nil && f.origins[scriptOrigin(parsed)]
}

func allowedScriptIP(ip net.IP, literal bool) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	for _, prefix := range scriptNonPublicRanges {
		if prefix.Contains(address) {
			return false
		}
	}
	if ip.IsLoopback() || ip.IsPrivate() {
		return literal
	}
	return ip.IsGlobalUnicast()
}

// net.IP.IsGlobalUnicast also includes shared and special-use IPv4 ranges.
var scriptNonPublicRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("fec0::/10"),
}

func (f *scriptFetcher) dial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errScriptURLDenied
	}
	ip := net.ParseIP(host)
	literal := ip != nil
	addresses := []net.IPAddr{{IP: ip}}
	if !literal {
		addresses, err = f.lookup(ctx, host)
	}
	if err != nil || len(addresses) == 0 || len(addresses) > 32 {
		return nil, errScriptFetch
	}
	// Validate the complete answer, then dial a pinned IP. A second DNS lookup
	// cannot redirect an approved public hostname into the LAN or loopback.
	for _, addr := range addresses {
		if addr.Zone != "" || !allowedScriptIP(addr.IP, literal) {
			return nil, errScriptURLDenied
		}
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	for _, addr := range addresses {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, errScriptFetch
}

func (f *scriptFetcher) fetch(ctx context.Context, raw string) ([]byte, error) {
	// Distinguish a missing/relative URL from an absolute URL rejected by local
	// origin policy. Neither path performs DNS or sends an HTTP request.
	parsed, parseErr := url.Parse(raw)
	if parseErr != nil || parsed.Scheme == "" {
		return nil, errScriptURLInvalid
	}
	u, err := parseScriptURL(raw)
	if err != nil || !f.allowed(u) {
		return nil, errScriptURLDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	transport := &http.Transport{Proxy: nil, DialContext: f.dial, DisableKeepAlives: true, DisableCompression: true, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second, MaxResponseHeaderBytes: 16 << 10}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return errScriptURLDenied }}
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, errScriptURLDenied
	}
	// No device token, cookies or caller headers are forwarded.
	req.Header.Set("Accept", "application/json,text/csv,application/octet-stream")
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, errScriptURLDenied) {
			return nil, errScriptURLDenied
		}
		return nil, errScriptFetch
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || response.ContentLength > maxScriptBytes || response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" {
		return nil, errScriptFetch
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxScriptBytes+1))
	if err != nil || len(data) > maxScriptBytes {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errScriptFetch
	}
	return data, nil
}
