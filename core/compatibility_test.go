package core

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func compatCall(t *testing.T, s *Server, method, path string, body any, token string) (int, map[string]any) {
	t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req, err := http.NewRequest(method, "http://"+s.Address()+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("x-device-token", token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
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

func TestCompatibilityReadContractAndAuthorization(t *testing.T) {
	s, sim := fixture(t)
	if code, _ := compatCall(t, s, "GET", "/vacuglide/state", nil, "wrong"); code != 401 {
		t.Fatal(code)
	}
	for _, token := range []string{s.Token(), "synthetic-secret-never-persisted"} {
		code, state := compatCall(t, s, "GET", "/vacuglide/state", nil, token)
		if code != 200 || len(state) != 9 || state["syncScriptToken"] != "private-script-token" || state["operationalMode"] != "TARGET_SPEED_PAUSED" {
			t.Fatal(code, state)
		}
		if _, exists := state["rawOperationalMode"]; exists {
			t.Fatal("native diagnostic leaked into SDK state")
		}
	}
	code, info := compatCall(t, s, "GET", "/vacuglide/info", nil, s.Token())
	if code != 200 || info["firmwareVersion"] != 1.03 || info["firmwareBranch"] != "prod" || info["deviceType"] != "vacuglide" {
		t.Fatal(code, info)
	}
	for _, path := range []string{"/vacuglide/connected", "/autoblow/connected"} {
		code, result := compatCall(t, s, "GET", path, nil, s.Token())
		if code != 200 || result["connected"] != true || result["cluster"] != s.DeviceAddress() {
			t.Fatal(code, result)
		}
	}
	before := time.Now().UnixMilli()
	code, clock := compatCall(t, s, "GET", "/cluster/time", nil, s.Token())
	if code != 200 || clock["time"].(float64) < float64(before) || clock["time"].(float64) > float64(time.Now().UnixMilli()) {
		t.Fatal(code, clock)
	}
	sim.conn.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		_, status := compatCall(t, s, "GET", "/vacuglide/connected", nil, s.Token())
		if status["connected"] == false {
			code, _ := compatCall(t, s, "GET", "/vacuglide/state", nil, s.Token())
			if code != 502 {
				t.Fatal(code)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("disconnected device still advertised online")
}

func TestCompatibilityManualControlDoesNotRequirePanelLease(t *testing.T) {
	s, sim := fixture(t, func(c *Config) { c.CompatibilityControl = true; c.MaxSpeed = 100 })
	if code, _ := compatCall(t, s, "PUT", "/vacuglide/target-speed", map[string]int{"targetSpeed": 15}, s.Token()); code != 403 {
		t.Fatal(code)
	}
	pair(t, s)
	for _, speed := range []int{0, 15, 100} {
		code, state := compatCall(t, s, "PUT", "/vacuglide/target-speed", map[string]int{"targetSpeed": speed}, s.Token())
		if code != 200 || state["targetSpeed"] != float64(speed) {
			t.Fatal(code, state)
		}
	}
	time.Sleep(2200 * time.Millisecond)
	_, state := compatCall(t, s, "GET", "/vacuglide/state", nil, s.Token())
	if state["operationalMode"] != "TARGET_SPEED_PLAYING" {
		t.Fatal("native panel lease interrupted SDK control", state)
	}
	if code, _ := call(t, s, "POST", "/api/start", startRequest(t, s, 15), s.Token()); code != 409 {
		t.Fatal("native session stole compatibility control", code)
	}
	_, state = compatCall(t, s, "PUT", "/vacuglide/valve/stroke-plus", map[string]bool{"valveState": true}, s.Token())
	if state["strokePlusValve"] != true {
		t.Fatal(state)
	}
	code, state := compatCall(t, s, "PUT", "/vacuglide/target-speed/stop", nil, s.Token())
	if code != 200 || state["operationalMode"] != "TARGET_SPEED_PAUSED" || state["strokePlusValve"] != true {
		t.Fatal("motor-only STOP changed valves", code, state)
	}
	if code, result := call(t, s, "POST", "/api/stop", struct{}{}, s.Token()); code != 200 {
		t.Fatal(code, result)
	}
	sim.mu.Lock()
	plus, minus := sim.plus, sim.minus
	sim.mu.Unlock()
	if plus || minus {
		t.Fatal("native STOP did not close valves")
	}
}

func TestCompatibilityValidationAndPendingOperationsNeverReachWire(t *testing.T) {
	s, sim := fixture(t, func(c *Config) { c.CompatibilityControl = true })
	pair(t, s)
	sim.mu.Lock()
	before := len(sim.commands)
	sim.mu.Unlock()
	for _, body := range []any{map[string]any{}, map[string]int{"targetSpeed": -1}, map[string]int{"targetSpeed": 16}, map[string]any{"targetSpeed": 15.5}} {
		if code, _ := compatCall(t, s, "PUT", "/vacuglide/target-speed", body, s.Token()); code != 400 {
			t.Fatal(code, body)
		}
	}
	if code, _ := compatCall(t, s, "PUT", "/vacuglide/valve/stroke-minus", map[string]any{}, s.Token()); code != 400 {
		t.Fatal(code)
	}
	if code, _ := compatCall(t, s, "PUT", "/vacuglide/sync-script/start", map[string]int{"startTimeMs": 0}, s.Token()); code != 501 {
		t.Fatal("disabled script feature reported success", code)
	}
	sim.mu.Lock()
	after := len(sim.commands)
	sim.mu.Unlock()
	if before != after {
		t.Fatal("invalid/disabled commands reached device")
	}
}

func TestCompatibilityTimeoutAndUnconfirmedStopAreNotReportedAsSuccess(t *testing.T) {
	s, sim := fixture(t, func(c *Config) { c.CompatibilityControl = true })
	pair(t, s)
	sim.mu.Lock()
	sim.wrongID = true
	sim.mu.Unlock()
	if code, _ := compatCall(t, s, "GET", "/vacuglide/state", nil, s.Token()); code != 504 {
		t.Fatal("wrong UUID did not become a device timeout", code)
	}
	if code, _ := compatCall(t, s, "PUT", "/vacuglide/target-speed", map[string]int{"targetSpeed": 15}, s.Token()); code != 502 {
		t.Fatal("unconfirmed safety STOP must invalidate connection", code)
	}
	_, state := compatCall(t, s, "GET", "/vacuglide/connected", nil, s.Token())
	if state["connected"] != false {
		t.Fatal("unsafe connection remained advertised")
	}
}

// JSON's last duplicate field must determine validation, including invalid values.
func TestDuplicateControlFieldsUseLastValue(t *testing.T) {
	s, sim := fixture(t, func(c *Config) { c.CompatibilityControl = true })
	pair(t, s)
	for _, tc := range []struct {
		name, path, raw, field string
		status                 int
		value                  any
	}{
		{"number-invalid-last", "/vacuglide/target-speed", `{"targetSpeed":1,"targetSpeed":{}}`, "", 400, nil},
		{"bool-invalid-last", "/vacuglide/valve/stroke-plus", `{"valveState":true,"valveState":{}}`, "", 400, nil},
		{"number-valid-last", "/vacuglide/target-speed", `{"targetSpeed":{},"targetSpeed":1}`, "targetSpeed", 200, float64(1)},
		{"bool-valid-last", "/vacuglide/valve/stroke-plus", `{"valveState":{},"valveState":false}`, "strokePlusValve", 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sim.mu.Lock()
			before := len(sim.wires)
			sim.mu.Unlock()
			code, result := compatCall(t, s, "PUT", tc.path, json.RawMessage(tc.raw), s.Token())
			if code != tc.status {
				t.Fatal(code, result)
			}
			sim.mu.Lock()
			count := len(sim.wires) - before
			sim.mu.Unlock()
			if code == 400 {
				if count != 0 {
					t.Fatal("invalid final field reached firmware", count)
				}
			} else if count != 1 || result[tc.field] != tc.value {
				t.Fatal(count, result)
			}
		})
	}
}
