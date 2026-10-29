package simulator

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

func LocalEndpoint(raw string) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil {
		return nil, fmt.Errorf("invalid endpoint")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "wss" || ip == nil || !ip.IsLoopback() || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/device-backend/ws" {
		return nil, fmt.Errorf("simulator requires a wss loopback IP endpoint ending /device-backend/ws")
	}
	return u, nil
}

func Run(ctx context.Context, raw string, roots *x509.CertPool) error {
	u, e := LocalEndpoint(raw)
	if e != nil {
		return e
	}
	if roots == nil {
		return fmt.Errorf("explicit local certificate trust is required")
	}
	config := &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	dial := websocket.Dialer{HandshakeTimeout: 5 * time.Second, TLSClientConfig: config}
	headers := http.Header{"X-Device-Mac": {MAC}, "X-Hardware-Version": {"vacuglide"}, "X-Firmware-Version": {"1.03"}, "X-Firmware-Branch": {"synthetic"}, "X-Device-Token": {DeviceToken}}
	c, resp, e := dial.DialContext(ctx, u.String(), headers)
	if e != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return fmt.Errorf("local WebSocket connection failed: %w", e)
	}
	defer c.Close()
	c.SetReadLimit(4096)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			c.Close()
		case <-done:
		}
	}()
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: config}, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("redirect refused") }}
	defer client.CloseIdleConnections()
	fetch := func(kind, token string) ([]byte, error) {
		b := *u
		b.Scheme = "https"
		b.Path = "/device-backend/" + kind + "-script-bin/" + token
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, b.String(), nil)
		if e != nil {
			return nil, e
		}
		req.Header = headers.Clone()
		r, e := client.Do(req)
		if e != nil {
			return nil, e
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			return nil, fmt.Errorf("download HTTP %d", r.StatusCode)
		}
		data, e := io.ReadAll(io.LimitReader(r.Body, 600009))
		if e != nil || len(data) > 600008 {
			return nil, fmt.Errorf("download bound exceeded")
		}
		return data, nil
	}
	d := New()
	for {
		typ, b, e := c.ReadMessage()
		if e != nil {
			if ctx.Err() != nil {
				return nil
			}
			return e
		}
		if typ != websocket.TextMessage {
			return fmt.Errorf("text protocol required")
		}
		reply, e := d.Handle(string(b), fetch)
		if e != nil {
			return e
		}
		if reply != "" {
			c.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if e = c.WriteMessage(websocket.TextMessage, []byte(reply)); e != nil {
				return e
			}
		}
	}
}
