package core

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDeviceCredentialAndStreamRevokedOnDisconnect(t *testing.T) {
	s, sim := fixture(t)
	token := "synthetic-secret-never-persisted"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+s.Address()+"/events/stream?deviceToken="+url.QueryEscape(token), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	reader := bufio.NewReader(resp.Body)
	for {
		line, e := reader.ReadString('\n')
		if e != nil {
			t.Fatal(e)
		}
		if line == "\n" {
			break
		}
	}
	sim.conn.Close()
	// A device-bound stream must terminate promptly, without waiting for heartbeat.
	if _, err = io.ReadAll(reader); err != nil && ctx.Err() != nil {
		t.Fatal("revoked stream remained open", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		code, _ := compatCall(t, s, "GET", "/cluster/time", nil, token)
		if code == 401 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	for _, route := range []string{"/cluster/time", "/vacuglide/connected", "/events/stream?deviceToken=" + url.QueryEscape(token)} {
		if code, _ := compatCall(t, s, "GET", route, nil, token); code != 401 {
			t.Fatalf("%s: stale credential accepted: %d", route, code)
		}
	}
	if code, _ := compatCall(t, s, "GET", "/vacuglide/connected", nil, s.Token()); code != 200 {
		t.Fatal("local owner lost offline access", code)
	}
}

func TestStateCommitPreservesPreviousAndRejectsDamagedIdentity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "paired-device")
	if err := writeStateFile(path, []byte("001122334455"), false); err != nil {
		t.Fatal(err)
	}
	if err := writeStateFile(path, []byte("aabbccddeeff"), false); !os.IsExist(err) {
		t.Fatal("existing identity overwritten", err)
	}
	// An interrupted pre-commit write remains inert and does not replace pairing.
	if err := os.WriteFile(filepath.Join(dir, ".pending-interrupted"), []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.DataDir = dir
	s, err := New(cfg)
	if err != nil || s.paired != "001122334455" {
		t.Fatal(s, err)
	}
	if err = writeStateFile(path, []byte("bad"), true); err != nil {
		t.Fatal(err)
	}
	if _, err = New(cfg); err == nil || !strings.Contains(err.Error(), "paired-device") {
		t.Fatal("damaged pairing accepted", err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = writeStateFile(filepath.Join(dir, "control-token"), []byte(strings.Repeat("z", 64)), true); err != nil {
		t.Fatal(err)
	}
	if _, err = New(cfg); err == nil || !strings.Contains(err.Error(), "control-token") {
		t.Fatal("non-hex key accepted", err)
	}
}

func TestConcurrentInitializationKeepsOneCompleteKey(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DataDir = t.TempDir()
	var wg sync.WaitGroup
	results := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, e := New(cfg)
			if e != nil {
				results <- "error: " + e.Error()
			} else {
				results <- s.Token()
			}
		}()
	}
	wg.Wait()
	close(results)
	first := ""
	for value := range results {
		if len(value) != 64 {
			t.Fatal(value)
		}
		if first == "" {
			first = value
		}
		if value != first {
			t.Fatal("conflicting control keys")
		}
	}
}

func TestRequestAdmissionReservesStopAndReleasesCancelledCaller(t *testing.T) {
	s := &Server{requestSlots: make(chan struct{}, 1)}
	entered := make(chan struct{})
	handler := s.boundedHTTP(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/stop" {
			w.WriteHeader(204)
			return
		}
		close(entered)
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/state", nil).WithContext(ctx))
	}()
	<-entered
	blocked := httptest.NewRecorder()
	handler(blocked, httptest.NewRequest("GET", "/api/state", nil))
	if blocked.Code != 429 {
		t.Fatal("ordinary request was not bounded", blocked.Code)
	}
	stop := httptest.NewRecorder()
	handler(stop, httptest.NewRequest("POST", "/api/stop", nil))
	if stop.Code != 204 {
		t.Fatal("STOP did not retain admission", stop.Code)
	}
	cancel()
	<-done
	if len(s.requestSlots) != 0 {
		t.Fatal("cancelled caller leaked admission")
	}
}

func TestUploadOverloadRejectsAndStopRemainsAvailable(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		io.WriteString(w, "not valid")
	}))
	defer origin.Close()
	defer close(release)
	s, _ := fixture(t, func(c *Config) {
		c.CompatibilityControl = true
		c.ExperimentalSyncScripts = true
		c.ScriptURLOrigins = []string{origin.URL}
	})
	pair(t, s)
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); wg.Wait() }()
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequestWithContext(ctx, "PUT", "http://"+s.Address()+"/vacuglide/sync-script/upload-funscript-url", strings.NewReader(`{"url":"`+origin.URL+`/script"}`))
			req.Header.Set("x-device-token", s.Token())
			req.Header.Set("Content-Type", "application/json")
			resp, e := http.DefaultClient.Do(req)
			if e == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("uploads never reached origin")
		}
	}
	if code, _ := compatCall(t, s, "PUT", "/vacuglide/sync-script/upload-funscript-url", map[string]string{"url": origin.URL + "/script"}, s.Token()); code != 429 {
		t.Fatal("overload not rejected", code)
	}
	if code, _ := compatCall(t, s, "PUT", "/vacuglide/target-speed/stop", nil, s.Token()); code != 200 {
		t.Fatal("STOP blocked by uploads", code)
	}
	cancel()
	wg.Wait()
	// Cancelled callers release admission, allowing a new attempt.
	deadline := time.Now().Add(time.Second)
	for len(s.uploadSlots) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(s.uploadSlots) != 0 {
		t.Fatal("upload permits leaked after cancellation")
	}
}
