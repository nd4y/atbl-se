package core

import (
	"encoding/binary"
	"github.com/nd4y/atblse/internal/protocol"
	"strings"
	"testing"
	"time"
)

func slotFixture(t *testing.T) (*Server, *simulator) {
	s, sim := fixture(t, func(c *Config) { c.ExperimentalLocalScripts = true; c.CompatibilityControl = true; c.MaxSpeed = 100 })
	pair(t, s)
	return s, sim
}

func TestNativeSwapMoveAndValveIndependence(t *testing.T) {
	s, sim := slotFixture(t)
	if code, _ := compatCall(t, s, "PUT", "/vacuglide/valve/stroke-plus", map[string]bool{"valveState": true}, s.Token()); code != 200 {
		t.Fatal(code)
	}
	sim.mu.Lock()
	before := len(sim.commands)
	sim.mu.Unlock()
	if code, r := compatCall(t, s, "GET", "/vacuglide/local-script/loaded", nil, s.Token()); code != 200 {
		t.Fatal(code, r)
	}
	if code, r := compatCall(t, s, "GET", "/vacuglide/state", nil, s.Token()); code != 200 || r["operationalMode"] != "LOCAL_SCRIPT_PAUSED" {
		t.Fatal("loaded native pause lost", code, r)
	}
	for _, tc := range []struct {
		path          string
		body          any
		first, second float64
	}{
		{"swap", map[string]int{"scriptIndexA": 0, "scriptIndexB": 29}, 30, 2},
		{"move", map[string]int{"fromIndex": 29, "toIndex": 0}, 1, 30},
		{"move", map[string]int{"fromIndex": 0, "toIndex": 29}, 30, 2},
		{"move", map[string]int{"fromIndex": 0, "toIndex": 0}, 30, 2},
	} {
		code, r := compatCall(t, s, "PUT", "/vacuglide/local-script/"+tc.path, tc.body, s.Token())
		if code != 200 {
			t.Fatal(tc.path, code, r)
		}
		ids := r["loadedScripts"].([]any)
		if ids[0] != tc.first || ids[1] != tc.second || len(ids) != 30 {
			t.Fatal(tc.path, ids)
		}
		code, r = compatCall(t, s, "GET", "/vacuglide/state", nil, s.Token())
		if code != 200 || r["operationalMode"] != "LOCAL_SCRIPT_PAUSED" || r["strokePlusValve"] != true {
			t.Fatal(r)
		}
	}
	sim.mu.Lock()
	defer sim.mu.Unlock()
	for _, cmd := range sim.commands[before:] {
		if cmd == "valve-stroke-plus-set" || cmd == "valve-stroke-minus-set" {
			t.Fatal("mutation issued extra valve operation", sim.commands)
		}
	}
}

func TestSlotListCannotInterruptPanelLease(t *testing.T) {
	s, sim := slotFixture(t)
	if code, r := call(t, s, "POST", "/api/start", startRequest(t, s, 15), s.Token()); code != 200 {
		t.Fatal(code, r)
	}
	sim.mu.Lock()
	before := len(sim.commands)
	sim.mu.Unlock()
	if code, r := compatCall(t, s, "GET", "/vacuglide/local-script/loaded", nil, s.Token()); code != 409 {
		t.Fatal(code, r)
	}
	sim.mu.Lock()
	after := len(sim.commands)
	sim.mu.Unlock()
	if before != after {
		t.Fatal("read-looking native command interrupted panel")
	}
}

func TestLocalReplacePullsAuthorizedBinaryAndConfirmsIdentity(t *testing.T) {
	s, sim := slotFixture(t)
	token, original, err := s.scripts.put([]protocol.SyncAction{{At: 0, Pos: 0}, {At: 1000, Pos: 60}, {At: 2000, Pos: 0}})
	if err != nil {
		t.Fatal(err)
	}
	code, r := compatCall(t, s, "PUT", "/vacuglide/local-script/replace", map[string]any{"replacedLocalScriptIndex": 29, "newLocalScriptUuid": token}, s.Token())
	if code != 200 {
		t.Fatal(code, r)
	}
	id := uint32(r["loadedScripts"].([]any)[29].(float64))
	sim.mu.Lock()
	download := append([]byte(nil), sim.localLoadedBytes...)
	sim.mu.Unlock()
	if len(download) != len(original.binary) || id == 0 || binary.LittleEndian.Uint32(download[:4]) != id || string(download[4:]) != string(original.binary[4:]) {
		t.Fatal("wrong local download/header")
	}
	// Local transfer authorization is scoped to this operation and path kind.
	if _, err := sim.fetchScriptKind("local", token); err == nil {
		t.Fatal("expired download authorization retained")
	}
	if _, err := sim.fetchScript(token); err == nil {
		t.Fatal("local grant authorized sync download")
	}
	code, r = compatCall(t, s, "PUT", "/vacuglide/sync-script/start", map[string]int{"startTimeMs": 0}, s.Token())
	if code != 501 {
		t.Fatal("local download enabled sync feature", code, r)
	}
}

func TestLocalMutationsRejectInvalidRequestsAndPropagateNativeError(t *testing.T) {
	s, sim := slotFixture(t)
	bad := []struct {
		path string
		body any
	}{
		{"swap", map[string]int{"scriptIndexA": 0}},
		{"swap", map[string]int{"scriptIndexA": -1, "scriptIndexB": 0}},
		{"move", map[string]int{"fromIndex": 0, "toIndex": 30}},
		{"move", map[string]int{"fromIndex": 0, "scriptIndexA": 1}},
		{"replace", map[string]any{"replacedLocalScriptIndex": 0, "newLocalScriptUuid": "../outside"}},
	}
	sim.mu.Lock()
	before := len(sim.commands)
	sim.mu.Unlock()
	for _, tc := range bad {
		if code, r := compatCall(t, s, "PUT", "/vacuglide/local-script/"+tc.path, tc.body, s.Token()); code != 400 {
			t.Fatal(tc.path, code, r)
		}
	}
	sim.mu.Lock()
	after := len(sim.commands)
	sim.mu.Unlock()
	if before != after {
		t.Fatal("invalid request sent command")
	}
	code, r := compatCall(t, s, "PUT", "/vacuglide/local-script/swap", map[string]int{"scriptIndexA": 0, "scriptIndexB": 0}, s.Token())
	if code != 400 || !strings.Contains(r["error"].(map[string]any)["message"].(string), "ERROR_SWAP") {
		t.Fatal(code, r)
	}
}

func TestLocalMutationRequiresPauseAndCorrectReplyKind(t *testing.T) {
	for _, failure := range []string{"pause", "kind", "id"} {
		t.Run(failure, func(t *testing.T) {
			s, sim := slotFixture(t)
			sim.mu.Lock()
			sim.mutationWrongPause = failure == "pause"
			sim.listWrongKind = failure == "kind"
			sim.wrongID = failure == "id"
			sim.mu.Unlock()
			code, _ := compatCall(t, s, "PUT", "/vacuglide/local-script/move", map[string]int{"fromIndex": 0, "toIndex": 1}, s.Token())
			if code == 200 {
				t.Fatal("unconfirmed operation reported success", failure)
			}
		})
	}
}

func TestStopSupersedesPendingSlotMutation(t *testing.T) {
	for _, operation := range []string{"move", "replace"} {
		t.Run(operation, func(t *testing.T) {
			s, sim := slotFixture(t)
			hold := make(chan struct{})
			defer close(hold)
			sim.mu.Lock()
			sim.holdMutation = hold
			sim.mu.Unlock()
			var token string
			var body any = map[string]int{"fromIndex": 0, "toIndex": 1}
			if operation == "replace" {
				var err error
				token, _, err = s.scripts.put([]protocol.SyncAction{{At: 0, Pos: 0}, {At: 1000, Pos: 60}, {At: 2000, Pos: 0}})
				if err != nil {
					t.Fatal(err)
				}
				body = map[string]any{"replacedLocalScriptIndex": 0, "newLocalScriptUuid": token}
			}
			done := make(chan int, 1)
			go func() {
				code, _ := compatCall(t, s, "PUT", "/vacuglide/local-script/"+operation, body, s.Token())
				done <- code
			}()
			deadline := time.Now().Add(time.Second)
			for {
				sim.mu.Lock()
				sent := false
				for _, cmd := range sim.commands {
					sent = sent || cmd == "local-script-"+operation
				}
				sim.mu.Unlock()
				if sent {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("mutation not sent")
				}
				time.Sleep(time.Millisecond)
			}
			if token != "" {
				if _, err := sim.fetchScriptKind("local", token); err != nil {
					t.Fatal("live local grant failed", err)
				}
				if _, err := sim.fetchScript(token); err == nil {
					t.Fatal("local grant leaked into sync path")
				}
			}
			if code, _ := compatCall(t, s, "PUT", "/vacuglide/target-speed/stop", nil, s.Token()); code != 200 {
				t.Fatal(code)
			}
			select {
			case code := <-done:
				if code == 200 {
					t.Fatal("superseded operation reported success")
				}
			case <-time.After(300 * time.Millisecond):
				t.Fatal("STOP did not cancel pending slot reply wait")
			}
			if token != "" {
				if _, err := sim.fetchScriptKind("local", token); err == nil {
					t.Fatal("STOP retained local download grant")
				}
			}
		})
	}
}
