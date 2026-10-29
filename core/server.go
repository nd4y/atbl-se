// Package core contains no platform-specific network configuration. Outbound
// script fetches require explicit origin allowlisting; hosts own its lifecycle.
package core

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"embed"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const Version = "0.2.0-rc.2"

//go:embed web/*
var web embed.FS

type Config struct {
	DeviceListen             string   `json:"deviceListen"`
	APIListen                string   `json:"apiListen"`
	DNSListen                string   `json:"dnsListen"`
	DNSAnswer                string   `json:"dnsAnswer"`
	DataDir                  string   `json:"dataDir"`
	MaxSpeed                 int      `json:"maxSpeed"`
	AllowShutdown            bool     `json:"allowShutdown"`
	CompatibilityControl     bool     `json:"compatibilityControl"`
	ExperimentalSyncScripts  bool     `json:"experimentalSyncScripts"`
	ExperimentalLocalScripts bool     `json:"experimentalLocalScripts"`
	ScriptURLOrigins         []string `json:"scriptURLOrigins"`
	CompatibilityCluster     string   `json:"compatibilityCluster"`
	TLSCertificateFile       string   `json:"tlsCertificateFile"`
	TLSPrivateKeyFile        string   `json:"tlsPrivateKeyFile"`
}

func DefaultConfig() Config {
	return Config{DeviceListen: "0.0.0.0:443", APIListen: "127.0.0.1:20880", DataDir: "data", MaxSpeed: 15}
}

type Server struct {
	cfg             Config
	token           string
	paired          string
	mu              sync.Mutex
	device          *Device
	api             *http.Server
	deviceHTTP      *http.Server
	apiListener     net.Listener
	deviceListener  net.Listener
	dns             *dnsService
	shutdown        chan struct{}
	shutdownOnce    sync.Once
	deviceTokenHash [32]byte
	closing         bool
	events          *eventHub
	scripts         *scriptStore
	scriptURLs      *scriptFetcher
}

func New(cfg Config) (*Server, error) {
	fetcher, err := newScriptFetcher(cfg.ScriptURLOrigins)
	if err != nil {
		return nil, err
	}
	if (cfg.TLSCertificateFile == "") != (cfg.TLSPrivateKeyFile == "") {
		return nil, errors.New("both TLS certificate and private key paths are required")
	}
	if cfg.CompatibilityCluster != "" {
		u, err := url.Parse("https://" + cfg.CompatibilityCluster)
		if err != nil || u.Host != cfg.CompatibilityCluster || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("compatibilityCluster must be a bare host[:port]")
		}
	}
	if cfg.MaxSpeed < 1 || cfg.MaxSpeed > 100 {
		return nil, errors.New("maxSpeed must be 1..100")
	}
	if cfg.DataDir == "" {
		return nil, errors.New("dataDir is required")
	}
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, shutdown: make(chan struct{}), events: newEventHub(), scriptURLs: fetcher}
	s.scripts = &scriptStore{dir: filepath.Join(cfg.DataDir, "sync-scripts")}
	p := filepath.Join(cfg.DataDir, "control-token")
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		b := make([]byte, 32)
		if _, err = rand.Read(b); err != nil {
			return nil, err
		}
		data = []byte(hex.EncodeToString(b))
		err = os.WriteFile(p, data, 0600)
	}
	if err != nil {
		return nil, err
	}
	s.token = strings.TrimSpace(string(data))
	if len(s.token) != 64 {
		return nil, errors.New("invalid control-token file")
	}
	if b, e := os.ReadFile(filepath.Join(cfg.DataDir, "paired-device")); e == nil {
		s.paired = strings.TrimSpace(string(b))
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	return s, nil
}

func (s *Server) Start() error {
	var cert tls.Certificate
	var err error
	if s.cfg.TLSCertificateFile != "" {
		cert, err = tls.LoadX509KeyPair(s.cfg.TLSCertificateFile, s.cfg.TLSPrivateKeyFile)
	} else {
		cert, err = makeCertificate()
	}
	if err != nil {
		return err
	}
	s.deviceListener, err = tls.Listen("tcp", s.cfg.DeviceListen, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if err != nil {
		return fmt.Errorf("device listener: %w", err)
	}
	s.apiListener, err = net.Listen("tcp", s.cfg.APIListen)
	if err != nil {
		s.deviceListener.Close()
		return fmt.Errorf("API listener: %w", err)
	}
	if s.cfg.DNSListen != "" {
		s.dns, err = startDNS(s.cfg.DNSListen, s.cfg.DNSAnswer)
		if err != nil {
			s.apiListener.Close()
			s.deviceListener.Close()
			return err
		}
	}
	s.deviceHTTP = &http.Server{Handler: http.HandlerFunc(s.acceptDevice), ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 16 * 1024}
	s.api = &http.Server{Handler: http.HandlerFunc(s.serveAPI), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	go s.deviceHTTP.Serve(s.deviceListener)
	go s.api.Serve(s.apiListener)
	return nil
}

func (s *Server) Address() string       { return s.apiListener.Addr().String() }
func (s *Server) DeviceAddress() string { return s.deviceListener.Addr().String() }

// Token is only for a same-user native launcher; it is never exposed by HTTP.
func (s *Server) Token() string                      { return s.token }
func (s *Server) ShutdownRequested() <-chan struct{} { return s.shutdown }

func (s *Server) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closing = true
	d := s.device
	s.mu.Unlock()
	s.events.close()
	var err error
	if d != nil {
		d.beginClosing()
		err = d.Stop(ctx)
		d.disconnect()
	}
	if s.dns != nil {
		s.dns.close()
	}
	if s.deviceHTTP != nil {
		s.deviceHTTP.Shutdown(ctx)
	}
	if s.api != nil {
		s.api.Shutdown(ctx)
	}
	return err
}

func jsonReply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, err error) {
	jsonReply(w, status, map[string]any{"error": err.Error()})
}

func (s *Server) serveAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	if isCompatibilityPath(r.URL.Path) {
		s.serveCompatibility(w, r)
		return
	}
	if r.Method == "GET" && r.URL.Path == "/health" {
		jsonReply(w, 200, map[string]any{"version": Version, "offline": true})
		return
	}
	if r.Method == "GET" && (r.URL.Path == "/" || r.URL.Path == "/app.js" || r.URL.Path == "/style.css") {
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}
		b, err := web.ReadFile("web" + path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(path, ".js") {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		} else if strings.HasSuffix(path, ".css") {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		} else {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Write(b)
		return
	}
	auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(auth), []byte(s.token)) != 1 {
		fail(w, 401, errors.New("local authorization required"))
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
		fail(w, 403, errors.New("cross-origin request rejected"))
		return
	}
	s.mu.Lock()
	d, paired := s.device, s.paired
	s.mu.Unlock()
	if r.Method == "GET" && r.URL.Path == "/api/status" {
		out := map[string]any{"version": Version, "connected": false, "paired": false, "maxSpeed": s.cfg.MaxSpeed}
		if d != nil {
			d.mu.Lock()
			out["connected"] = true
			out["paired"] = paired == d.mac
			out["mac"] = d.mac
			out["firmware"] = d.firmware
			out["ready"] = d.ready
			out["state"] = d.state
			out["active"] = d.epoch != "" || d.compatibilityActive
			out["compatibilityControl"] = d.compatibilityActive
			out["generation"] = d.generation.Load()
			out["stopping"] = d.stopping
			out["stopConfirmed"] = d.state != nil && d.state.RawOperationalMode == 44 && !d.state.StrokePlusValve && !d.state.StrokeMinusValve && d.epoch == "" && !d.compatibilityActive && !d.stopping && d.stopErr == nil
			d.mu.Unlock()
		}
		jsonReply(w, 200, out)
		return
	}
	if r.Method == "POST" && r.URL.Path == "/api/shutdown" && s.cfg.AllowShutdown {
		jsonReply(w, 200, map[string]bool{"shutdownRequested": true})
		s.shutdownOnce.Do(func() { close(s.shutdown) })
		return
	}
	if d == nil {
		fail(w, 409, ErrUnavailable)
		return
	}
	switch r.Method + " " + r.URL.Path {
	case "POST /api/pair":
		var b struct {
			MAC string `json:"mac"`
		}
		if !readJSON(w, r, &b) {
			return
		}
		if b.MAC != d.mac {
			fail(w, 409, errors.New("device identity changed"))
			return
		}
		s.mu.Lock()
		if s.device != d || s.paired != "" && s.paired != d.mac {
			s.mu.Unlock()
			fail(w, 409, errors.New("another device is paired"))
			return
		}
		err := os.WriteFile(filepath.Join(s.cfg.DataDir, "paired-device"), []byte(d.mac), 0600)
		if err == nil {
			s.paired = d.mac
		}
		s.mu.Unlock()
		if err != nil {
			fail(w, 500, errors.New("cannot save pairing"))
			return
		}
		jsonReply(w, 200, map[string]bool{"paired": true})
	case "POST /api/stop":
		// STOP is allowed for a pending device as well; it never enables motion.
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		if err := d.Stop(ctx); err != nil {
			d.disconnect()
			fail(w, 502, err)
			return
		}
		jsonReply(w, 200, map[string]bool{"stopped": true})
	case "GET /api/state":
		st, err := d.rpc(r.Context(), "state")
		if err != nil {
			fail(w, 502, err)
			return
		}
		jsonReply(w, 200, st)
	case "POST /api/start":
		if paired != d.mac {
			fail(w, 403, errors.New("confirm pairing first"))
			return
		}
		var b struct {
			Speed      int    `json:"speed"`
			Seconds    int    `json:"seconds"`
			Generation uint64 `json:"generation"`
		}
		if !readJSON(w, r, &b) {
			return
		}
		if b.Speed < 1 || b.Speed > s.cfg.MaxSpeed || b.Seconds < 1 || b.Seconds > 3600 {
			fail(w, 400, errors.New("speed or duration outside configured limits"))
			return
		}
		epoch, err := d.Start(r.Context(), b.Speed, b.Seconds, b.Generation)
		if err != nil {
			fail(w, 409, err)
			return
		}
		jsonReply(w, 200, map[string]string{"controlEpoch": epoch})
	case "POST /api/keepalive":
		var b struct {
			Epoch string `json:"controlEpoch"`
		}
		if !readJSON(w, r, &b) {
			return
		}
		if err := d.Keepalive(b.Epoch); err != nil {
			fail(w, 409, err)
			return
		}
		jsonReply(w, 200, map[string]bool{"renewed": true})
	default:
		http.NotFound(w, r)
	}
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		fail(w, 400, errors.New("invalid JSON request"))
		return false
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		fail(w, 400, errors.New("trailing JSON data"))
		return false
	}
	return true
}

func makeCertificate() (tls.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return tls.Certificate{}, err
	}
	t := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "latency.autoblowapi.com"}, DNSNames: []string{"latency.autoblowapi.com", "*.autoblowapi.com", "localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}, NotBefore: time.Now().Add(-24 * time.Hour), NotAfter: time.Now().AddDate(5, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, t, t, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}
