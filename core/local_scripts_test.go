package core

import (
	"autoblow.local/emulator/internal/protocol"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestLocalListAndPlaybackContracts(t *testing.T) {
	s, sim := fixture(t, func(c *Config) { c.ExperimentalLocalScripts = true; c.CompatibilityControl = true; c.MaxSpeed = 100 })
	pair(t, s)
	code, r := compatCall(t, s, "GET", "/vacuglide/local-script/loaded", nil, s.Token())
	if code != 200 || len(r["loadedScripts"].([]any)) != 30 || r["loadedScripts"].([]any)[3] != float64(11265) {
		t.Fatal(code, r)
	}
	_, _ = compatCall(t, s, "PUT", "/vacuglide/valve/stroke-plus", map[string]bool{"valveState": true}, s.Token())
	for _, step := range []struct {
		path string
		body any
		mode string
	}{
		{"", map[string]int{"localScriptIndex": 29}, "LOCAL_SCRIPT_PLAYING"}, {"/stop", nil, "LOCAL_SCRIPT_PAUSED"}, {"/start", nil, "LOCAL_SCRIPT_PLAYING"},
	} {
		code, r = compatCall(t, s, "PUT", "/vacuglide/local-script"+step.path, step.body, s.Token())
		if code != 200 || r["operationalMode"] != step.mode || r["localScript"] != float64(29) || r["strokePlusValve"] != false {
			t.Fatal(code, r)
		}
	}
	_, _ = compatCall(t, s, "PUT", "/vacuglide/target-speed", map[string]int{"targetSpeed": 15}, s.Token())
	for _, path := range []string{"start", "stop"} {
		code, r = compatCall(t, s, "PUT", "/vacuglide/local-script/"+path, nil, s.Token())
		if code != 200 || r["operationalMode"] != "TARGET_SPEED_PLAYING" {
			t.Fatal(path, code, r)
		}
	}
	if code, _ := call(t, s, "POST", "/api/start", startRequest(t, s, 15), s.Token()); code != 409 {
		t.Fatal("no-op local STOP released motion ownership", code)
	}
	sim.mu.Lock()
	before := len(sim.commands)
	sim.mu.Unlock()
	for _, n := range []int{-1, 30} {
		if code, _ := compatCall(t, s, "PUT", "/vacuglide/local-script", map[string]int{"localScriptIndex": n}, s.Token()); code != 400 {
			t.Fatal(code)
		}
	}
	for _, path := range []string{"replace", "swap", "move"} {
		if code, _ := compatCall(t, s, "PUT", "/vacuglide/local-script/"+path, map[string]int{}, s.Token()); code != 400 {
			t.Fatal(code)
		}
	}
	sim.mu.Lock()
	after := len(sim.commands)
	sim.mu.Unlock()
	if before != after {
		t.Fatal("invalid slot mutations reached wire")
	}
}
func TestLocalListRequiresCorrectReplyTypeAndID(t *testing.T) {
	for _, failure := range []string{"shape", "id"} {
		t.Run(failure, func(t *testing.T) {
			s, sim := fixture(t)
			sim.mu.Lock()
			sim.listWrongKind = failure == "shape"
			sim.wrongID = failure == "id"
			sim.mu.Unlock()
			code, _ := compatCall(t, s, "GET", "/vacuglide/local-script/loaded", nil, s.Token())
			sim.mu.Lock()
			sim.listWrongKind = false
			sim.wrongID = false
			sim.mu.Unlock()
			want := 504
			if failure == "id" {
				want = 502
			} // STOP is also unacknowledged; disconnect.
			if code != want {
				t.Fatal(code)
			}
		})
	}
}
func TestLocalMotionGatesAndReadOnlyList(t *testing.T) {
	s, _ := fixture(t)
	if code, _ := compatCall(t, s, "GET", "/vacuglide/local-script/loaded", nil, s.Token()); code != 200 {
		t.Fatal(code)
	}
	if code, r := compatCall(t, s, "PUT", "/vacuglide/local-script", map[string]int{"localScriptIndex": 0}, s.Token()); code != 501 || r["error"].(map[string]any)["code"] != "EXPERIMENTAL_FEATURE_DISABLED" {
		t.Fatal(code, r)
	}
	s2, _ := fixture(t, func(c *Config) { c.ExperimentalLocalScripts = true; c.CompatibilityControl = true })
	pair(t, s2)
	if code, _ := compatCall(t, s2, "PUT", "/vacuglide/local-script", map[string]int{"localScriptIndex": 0}, s2.Token()); code != 403 {
		t.Fatal("unknown slot speed bypassed ceiling", code)
	}
}
func TestFirmwareErrorIsPromptAndDoesNotReportSuccess(t *testing.T) {
	s, sim := syncFixture(t)
	sim.mu.Lock()
	sim.errorCommand = "sync-script-load-token"
	sim.mu.Unlock()
	before := time.Now()
	code, r := uploadCall(t, s, "upload-funscript", testScript)
	if code != 400 || time.Since(before) > 2*time.Second || !strings.Contains(r["error"].(map[string]any)["message"].(string), "SYNC_SCRIPT_LOAD_TOKEN_ERROR") {
		t.Fatal(code, r)
	}
	sim.mu.Lock()
	mode := sim.mode
	sim.mu.Unlock()
	if mode != protocol.ModePaused {
		t.Fatal("failure did not STOP", mode)
	}
}
func TestStopPreemptsLocalSelection(t *testing.T) {
	s, sim := fixture(t, func(c *Config) { c.ExperimentalLocalScripts = true; c.CompatibilityControl = true; c.MaxSpeed = 100 })
	pair(t, s)
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
		code, _ := compatCall(t, s, http.MethodPut, "/vacuglide/local-script", map[string]int{"localScriptIndex": 7}, s.Token())
		result <- code
	}()
	deadline := time.Now().Add(time.Second)
	for {
		sim.mu.Lock()
		sent := len(sim.commands) > before
		sim.mu.Unlock()
		if sent {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no selection")
		}
		time.Sleep(time.Millisecond)
	}
	if code, r := compatCall(t, s, "PUT", "/vacuglide/local-script/stop", nil, s.Token()); code != 200 {
		t.Fatal(code, r)
	}
	close(hold)
	select {
	case code := <-result:
		if code != 409 {
			t.Fatal(code)
		}
	case <-time.After(time.Second):
		t.Fatal("selection stuck")
	}
}
func TestCSVLineAndHeaderCompatibility(t *testing.T) {
	for i, raw := range []string{"0,0\n1000,90\n2000,0\n", "at,pos\n0,0\n1000,90\n2000,0\n"} {
		a, err := parseActions([]byte(raw), "upload-csv")
		if err != nil || len(a) != 3+i || a[len(a)-1].At != 2000 {
			t.Fatal(a, err)
		}
	}
	a, err := parseActions([]byte("0,0\n1000,90\n2000,0"), "upload-csv")
	if err != nil || len(a) != 2 {
		t.Fatal(a, err)
	}
}
func TestSyncSettingsBeforeUpload(t *testing.T) {
	s, _ := syncFixture(t)
	for _, step := range []struct {
		path string
		body any
	}{{"offset", map[string]int{"offsetTimeMs": 123}}, {"loop", map[string]bool{"loop": true}}} {
		if code, r := compatCall(t, s, "PUT", "/vacuglide/sync-script/"+step.path, step.body, s.Token()); code != 200 {
			t.Fatal(code, r)
		}
	}
}
