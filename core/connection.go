package core

import (
	"context"
	"crypto/sha256"
	"github.com/gorilla/websocket"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var macPattern = regexp.MustCompile(`^[0-9a-f]{12}$`)

func (s *Server) acceptDevice(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/device-backend/sync-script-bin/") || strings.HasPrefix(r.URL.Path, "/device-backend/local-script-bin/") {
		s.serveScriptBinary(w, r)
		return
	}
	if isCompatibilityPath(r.URL.Path) {
		s.serveCompatibility(w, r)
		return
	}
	if r.Method != "GET" || r.URL.Path != "/device-backend/ws" {
		http.NotFound(w, r)
		return
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsPrivate() && !ip.IsLoopback() {
		http.Error(w, "local clients only", 403)
		return
	}
	mac := strings.ToLower(strings.ReplaceAll(r.Header.Get("x-device-mac"), ":", ""))
	firmware := r.Header.Get("x-firmware-version")
	if !macPattern.MatchString(mac) || r.Header.Get("x-hardware-version") != "vacuglide" || (firmware != "1.03" && firmware != "1.0.3") || r.Header.Get("x-device-token") == "" {
		http.Error(w, "unsupported device or firmware", 400)
		return
	}
	// Credentials are intentionally not retained: this connection is locally
	// paired by the owner; all control clients use an independent local token.
	s.mu.Lock()
	if s.closing || s.device != nil || (s.paired != "" && s.paired != mac) {
		s.mu.Unlock()
		http.Error(w, "device slot already owned", 409)
		return
	}
	upgrader := websocket.Upgrader{HandshakeTimeout: 5 * time.Second, CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "" }}
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.mu.Unlock()
		return
	}
	d := newDevice(c, mac, firmware)
	d.onEvent = s.events.publish
	d.branch = r.Header.Get("x-firmware-branch")
	if d.branch == "" {
		d.branch = "unknown"
	}
	s.deviceTokenHash = sha256.Sum256([]byte(r.Header.Get("x-device-token")))
	s.device = d
	s.mu.Unlock()
	defer func() {
		d.disconnect()
		s.mu.Lock()
		if s.device == d {
			s.device = nil
			s.deviceTokenHash = [32]byte{}
		}
		s.mu.Unlock()
	}()
	go d.read()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	err = d.bootstrap(ctx)
	cancel()
	if err != nil {
		return
	}
	go d.watch()
	<-d.done
}
