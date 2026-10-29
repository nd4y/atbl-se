package core

import (
	"autoblow.local/emulator/internal/protocol"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const testScript = `{"actions":[{"at":0,"pos":0},{"at":1000,"pos":90},{"at":2000,"pos":0}]}`

func syncFixture(t *testing.T) (*Server, *simulator) {
	s, sim := fixture(t, func(c *Config) { c.CompatibilityControl = true; c.ExperimentalSyncScripts = true })
	pair(t, s)
	return s, sim
}
func uploadCall(t *testing.T, s *Server, path, raw string) (int, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "script.funscript")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte(raw))
	_ = writer.Close()
	req, _ := http.NewRequest("PUT", "http://"+s.Address()+"/vacuglide/sync-script/"+path, &body)
	req.Header.Set("x-device-token", s.Token())
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := (&http.Client{Timeout: 40 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result map[string]any
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, result
}
func TestSyncUploadDownloadAndControl(t *testing.T) {
	s, sim := syncFixture(t)
	for _, format := range []string{"upload-funscript", "upload-csv"} {
		raw := testScript
		if format == "upload-csv" {
			raw = "at,pos\n0,0\n1000,90\n2000,0\n"
		}
		code, result := uploadCall(t, s, format, raw)
		if code != 200 || result["operationalMode"] != "SYNC_SCRIPT_PAUSED" {
			t.Fatal(code, result)
		}
		token := result["syncScriptToken"].(string)
		if !protocol.ValidScriptToken(token) {
			t.Fatal(token)
		}
		sim.mu.Lock()
		binary := hex.EncodeToString(sim.loadedBytes)
		sim.mu.Unlock()
		if binary != "000000000100000000000000000fe8030000000f" {
			t.Fatal(binary)
		}
		for _, step := range []struct {
			path  string
			body  any
			field string
			value any
		}{
			{"start", map[string]int{"startTimeMs": 500}, "operationalMode", "SYNC_SCRIPT_PLAYING"},
			{"offset", map[string]int{"offsetTimeMs": -100}, "syncScriptOffsetTime", float64(-100)},
			{"loop", map[string]bool{"loop": true}, "syncScriptLoop", true},
			{"stop", nil, "operationalMode", "SYNC_SCRIPT_PAUSED"},
			{"load-token", map[string]string{"scriptToken": token}, "syncScriptToken", token},
		} {
			code, result = compatCall(t, s, "PUT", "/vacuglide/sync-script/"+step.path, step.body, s.Token())
			if code != 200 || result[step.field] != step.value {
				t.Fatal(step.path, code, result)
			}
		}
	}
}
func TestSyncValidationDoesNotWriteOrCommandDevice(t *testing.T) {
	s, sim := syncFixture(t)
	sim.mu.Lock()
	before := len(sim.commands)
	sim.mu.Unlock()
	for _, raw := range []string{
		`{}`, `{"actions":[{},{}]}`, `{"actions":[{"at":0,"pos":0},{"at":1.5,"pos":3}]}`,
		`{"actions":[{"at":0,"pos":0},{"at":1,"pos":100}]}`,
		`{"actions":[{"at":0,"pos":0},{"at":0,"pos":100}]}`,
		testScript + "{}", strings.Repeat(" ", maxScriptBytes) + testScript,
	} {
		if code, result := uploadCall(t, s, "upload-funscript", raw); code != 400 {
			t.Fatal(code, result)
		}
	}
	if code, _ := uploadCall(t, s, "upload-csv", "at_ms,speed_percent,reserved\n0,15,0\n"); code != 400 {
		t.Fatal(code)
	}
	for _, step := range []struct {
		path string
		body any
		code int
	}{
		{"start", map[string]int{"startTimeMs": 0}, 400},
		{"start", map[string]int{"startTimeMs": -1}, 400},
		{"offset", map[string]int{}, 400},
		{"offset", map[string]int{"offsetTimeMs": 16777215}, 400},
		{"loop", map[string]int{"loop": 2}, 400},
		{"load-token", map[string]string{"scriptToken": "../../control-token"}, 404},
		{"load-token", map[string]string{"scriptToken": "11111111-1111-4111-8111-111111111111"}, 404},
	} {
		code, result := compatCall(t, s, "PUT", "/vacuglide/sync-script/"+step.path, step.body, s.Token())
		if code != step.code {
			t.Fatal(step.path, code, result)
		}
	}
	sim.mu.Lock()
	after := len(sim.commands)
	sim.mu.Unlock()
	if after != before {
		t.Fatal("invalid input reached device", before, after)
	}
	files, _ := os.ReadDir(s.scripts.dir)
	if len(files) != 0 {
		t.Fatal("invalid upload stored files")
	}
}
func TestScriptStorePersistsAndDetectsCorruption(t *testing.T) {
	store := &scriptStore{dir: t.TempDir()}
	actions, _ := parseActions([]byte(testScript), "upload-funscript")
	token, want, err := store.put(actions)
	if err != nil {
		t.Fatal(err)
	}
	restarted := &scriptStore{dir: store.dir}
	got, err := restarted.get(token)
	if err != nil || !bytes.Equal(got.binary, want.binary) || got.end != 1000 || got.peak != 15 {
		t.Fatal(got, err)
	}
	file := filepath.Join(store.dir, token+".json")
	raw, _ := os.ReadFile(file)
	raw = bytes.Replace(raw, []byte(`"pos":90`), []byte(`"pos":80`), 1)
	if err = os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.get(token); err == nil {
		t.Fatal("corrupt data accepted")
	}
	if _, err = restarted.get("../control-token"); err == nil {
		t.Fatal("path accepted")
	}
	for i := 0; i < maxStoredScripts; i++ {
		if err = os.WriteFile(filepath.Join(store.dir, fmt.Sprintf("%d.json", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err = store.put(actions); err == nil {
		t.Fatal("quota not enforced")
	}
}
func TestScriptDownloadRequiresDeviceAndSelectedToken(t *testing.T) {
	s, sim := syncFixture(t)
	_, result := uploadCall(t, s, "upload-funscript", testScript)
	token := result["syncScriptToken"].(string)
	hold := make(chan struct{})
	defer close(hold)
	sim.mu.Lock()
	sim.holdSync = hold
	sim.loadedBytes = nil
	sim.mu.Unlock()
	done := make(chan int, 1)
	go func() {
		code, _ := compatCall(t, s, "PUT", "/vacuglide/sync-script/load-token", map[string]string{"scriptToken": token}, s.Token())
		done <- code
	}()
	waitForSyncCommand(t, sim)
	for _, tc := range []struct {
		token, auth, mac string
		want             int
	}{
		{token, "synthetic-secret-never-persisted", "001122334455", 200},
		{token, s.Token(), "001122334455", 401},
		{token, "", "001122334455", 401},
		{token, "synthetic-secret-never-persisted", "001122334456", 401},
		{"11111111-1111-4111-8111-111111111111", "synthetic-secret-never-persisted", "001122334455", 404},
	} {
		req := httptest.NewRequest("GET", "https://local/device-backend/sync-script-bin/"+tc.token, nil)
		req.RemoteAddr = "127.0.0.1:5000"
		req.Header.Set("x-device-token", tc.auth)
		req.Header.Set("x-device-mac", tc.mac)
		w := httptest.NewRecorder()
		s.serveScriptBinary(w, req)
		if w.Code != tc.want {
			t.Fatal(w.Code, tc.want)
		}
	}
	if raw, err := sim.fetchScript(token); err != nil || len(raw) != 20 {
		t.Fatal(err, len(raw))
	}
}
func TestSyncRejectsMismatchedAcknowledgementAndStops(t *testing.T) {
	s, sim := syncFixture(t)
	sim.mu.Lock()
	sim.wrongScript = true
	sim.mu.Unlock()
	code, result := uploadCall(t, s, "upload-funscript", testScript)
	if code != 409 {
		t.Fatal(code, result)
	}
	s.mu.Lock()
	d := s.device
	s.mu.Unlock()
	d.mu.Lock()
	token := d.syncToken
	d.mu.Unlock()
	if token != "" {
		t.Fatal("unconfirmed load bound")
	}
	sim.mu.Lock()
	mode := sim.mode
	commands := append([]string(nil), sim.commands...)
	sim.mu.Unlock()
	if mode != 44 || commands[len(commands)-3] != "target-speed-stop" {
		t.Fatal(mode, commands)
	}
}
func TestStopPreemptsSyncLoadAndDelayedStart(t *testing.T) {
	for _, operation := range []string{"load", "start"} {
		t.Run(operation, func(t *testing.T) {
			s, sim := syncFixture(t)
			if operation == "start" {
				if code, r := uploadCall(t, s, "upload-funscript", testScript); code != 200 {
					t.Fatal(code, r)
				}
			}
			hold := make(chan struct{})
			defer func() {
				select {
				case <-hold:
				default:
					close(hold)
				}
			}()
			sim.mu.Lock()
			sim.holdSync = hold
			before := len(sim.commands)
			sim.mu.Unlock()
			result := make(chan int, 1)
			go func() {
				if operation == "load" {
					code, _ := uploadCall(t, s, "upload-funscript", testScript)
					result <- code
				} else {
					code, _ := compatCall(t, s, "PUT", "/vacuglide/sync-script/start", map[string]int{"startTimeMs": 0}, s.Token())
					result <- code
				}
			}()
			deadline := time.Now().Add(2 * time.Second)
			for {
				sim.mu.Lock()
				sent := len(sim.commands) > before
				sim.mu.Unlock()
				if sent {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("no command")
				}
				time.Sleep(time.Millisecond)
			}
			if code, _ := compatCall(t, s, "PUT", "/vacuglide/sync-script/loop", map[string]bool{"loop": true}, s.Token()); code != 409 {
				t.Fatal("overlapping command accepted", code)
			}
			if code, r := compatCall(t, s, "PUT", "/vacuglide/target-speed/stop", nil, s.Token()); code != 200 {
				t.Fatal(code, r)
			}
			close(hold)
			select {
			case code := <-result:
				if code != 409 {
					t.Fatal("superseded command reported success", code)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("delayed operation stuck")
			}
			if code, st := compatCall(t, s, "GET", "/vacuglide/state", nil, s.Token()); code != 200 || st["operationalMode"] != "TARGET_SPEED_PAUSED" {
				t.Fatal(code, st)
			}
			if operation == "load" {
				if code, _ := compatCall(t, s, "PUT", "/vacuglide/sync-script/start", map[string]int{"startTimeMs": 0}, s.Token()); code != 400 {
					t.Fatal("preempted script bound", code)
				}
			}
		})
	}
}
func TestSyncDefaultOffAndPairingRequired(t *testing.T) {
	s, _ := fixture(t)
	if code, _ := uploadCall(t, s, "upload-funscript", testScript); code != 501 {
		t.Fatal(code)
	}
	s2, _ := fixture(t, func(c *Config) { c.ExperimentalSyncScripts = true; c.CompatibilityControl = true })
	if code, _ := uploadCall(t, s2, "upload-funscript", testScript); code != 403 {
		t.Fatal(code)
	}
}

type delayedRequestBody struct {
	io.Reader
	entered, release chan struct{}
	once             sync.Once
}

func (b *delayedRequestBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return b.Reader.Read(p)
}
func (b *delayedRequestBody) Close() error { return nil }

func TestStopInvalidatesRequestStillReadingBody(t *testing.T) {
	for _, path := range []string{"/vacuglide/target-speed", "/vacuglide/sync-script/start"} {
		t.Run(path, func(t *testing.T) {
			s, sim := syncFixture(t)
			if code, result := uploadCall(t, s, "upload-funscript", testScript); code != 200 {
				t.Fatal(code, result)
			}
			raw := `{"targetSpeed":15}`
			if strings.HasSuffix(path, "start") {
				raw = `{"startTimeMs":0}`
			}
			body := &delayedRequestBody{Reader: strings.NewReader(raw), entered: make(chan struct{}), release: make(chan struct{})}
			req := httptest.NewRequest("PUT", "http://local"+path, nil)
			req.Body = body
			req.Header.Set("x-device-token", s.Token())
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { s.serveCompatibility(w, req); close(done) }()
			defer func() {
				select {
				case <-body.release:
				default:
					close(body.release)
				}
			}()
			select {
			case <-body.entered:
			case <-time.After(time.Second):
				t.Fatal("body not read")
			}
			if code, r := compatCall(t, s, "PUT", "/vacuglide/target-speed/stop", nil, s.Token()); code != 200 {
				t.Fatal(code, r)
			}
			sim.mu.Lock()
			before := len(sim.commands)
			sim.mu.Unlock()
			close(body.release)
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("request stuck")
			}
			if w.Code != 409 {
				t.Fatal(w.Code, w.Body.String())
			}
			sim.mu.Lock()
			after := len(sim.commands)
			sim.mu.Unlock()
			if before != after {
				t.Fatal("pre-STOP request reached wire")
			}
		})
	}
}
