package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestScriptURLUploadAndFetchLimits(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-device-token") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("credentials forwarded")
		}
		switch r.URL.Path {
		case "/script":
			io.WriteString(w, testScript)
		case "/csv":
			io.WriteString(w, "0,0\n1000,90\n2000,0\n")
		case "/redirect":
			http.Redirect(w, r, "/script", 302)
		case "/large":
			io.WriteString(w, strings.Repeat(" ", maxScriptBytes+1))
		case "/gzip":
			w.Header().Set("Content-Encoding", "gzip")
			io.WriteString(w, "data")
		case "/bad":
			io.WriteString(w, "not a script")
		default:
			http.NotFound(w, r)
		}
	}))
	defer source.Close()
	s, _ := fixture(t, func(c *Config) {
		c.CompatibilityControl = true
		c.ExperimentalSyncScripts = true
		c.ScriptURLOrigins = []string{source.URL}
	})
	pair(t, s)
	for _, format := range []string{"funscript", "csv"} {
		path := "/script"
		if format == "csv" {
			path = "/csv"
		}
		code, r := compatCall(t, s, "PUT", "/vacuglide/sync-script/upload-"+format+"-url", map[string]string{"url": source.URL + path}, s.Token())
		if code != 200 || r["operationalMode"] != "SYNC_SCRIPT_PAUSED" {
			t.Fatal(code, r)
		}
	}
	for _, tc := range []struct {
		path string
		want int
	}{{"/redirect", 403}, {"/large", 502}, {"/gzip", 502}, {"/missing", 502}, {"/bad", 400}} {
		code, r := compatCall(t, s, "PUT", "/vacuglide/sync-script/upload-funscript-url", map[string]string{"url": source.URL + tc.path}, s.Token())
		if code != tc.want {
			t.Fatal(tc, code, r)
		}
	}
}
func TestScriptURLDenialAndDNSPinning(t *testing.T) {
	for _, origin := range []string{"file:///tmp", "https://user:pass@example.test", "https://example.test/path", "https://example.test?query", "http://[fe80::1%25zone]", "http://example.test:0"} {
		if _, err := newScriptFetcher([]string{origin}); err == nil {
			t.Fatal(origin)
		}
	}
	f, err := newScriptFetcher([]string{"http://example.test"})
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"http://other.test/script", "file:///etc/passwd", "http://user:pass@example.test/script", "http://example.test/script#fragment"} {
		if _, err := f.fetch(context.Background(), raw); !errors.Is(err, errScriptURLDenied) {
			t.Fatal(raw, err)
		}
	}
	for _, address := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "::1", "::ffff:127.0.0.1", "0.0.0.0", "ff02::1", "100.100.100.200", "198.18.0.1", "192.0.0.1", "fec0::1"} {
		f.lookup = func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP(address)}}, nil
		}
		if _, err := f.fetch(context.Background(), "http://example.test/script"); !errors.Is(err, errScriptURLDenied) {
			t.Fatal(address, err)
		}
	}
	for _, address := range []string{"169.254.169.254", "0.0.0.0", "ff02::1"} {
		if allowedScriptIP(net.ParseIP(address), true) {
			t.Fatal(address)
		}
	}
	if !allowedScriptIP(net.ParseIP("192.168.1.3"), true) {
		t.Fatal("explicit LAN literal rejected")
	}
	if allowedScriptIP(net.ParseIP("192.168.1.3"), false) {
		t.Fatal("DNS private address accepted")
	}
}
func TestScriptURLCancellationAndTLSVerification(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer source.Close()
	f, _ := newScriptFetcher([]string{source.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := f.fetch(ctx, source.URL+"/script"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	tlsSource := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("untrusted TLS accepted") }))
	defer tlsSource.Close()
	f, _ = newScriptFetcher([]string{tlsSource.URL})
	if _, err := f.fetch(context.Background(), tlsSource.URL); !errors.Is(err, errScriptFetch) {
		t.Fatal(err)
	}
}
func TestStopWhileFetchingURLNeverLoadsOrStarts(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; fmt.Fprint(w, testScript) }))
	defer source.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	s, sim := fixture(t, func(c *Config) {
		c.CompatibilityControl = true
		c.ExperimentalSyncScripts = true
		c.ScriptURLOrigins = []string{source.URL}
	})
	pair(t, s)
	result := make(chan int, 1)
	go func() {
		code, _ := compatCall(t, s, "PUT", "/vacuglide/sync-script/upload-funscript-url", map[string]string{"url": source.URL}, s.Token())
		result <- code
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("fetch did not start")
	}
	if code, r := compatCall(t, s, "PUT", "/vacuglide/target-speed/stop", nil, s.Token()); code != 200 {
		t.Fatal(code, r)
	}
	close(release)
	select {
	case code := <-result:
		if code != 409 {
			t.Fatal(code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("upload stuck")
	}
	sim.mu.Lock()
	defer sim.mu.Unlock()
	for _, name := range sim.commands {
		if strings.HasPrefix(name, "sync-script-") {
			t.Fatal("post-STOP script command", name)
		}
	}
}
