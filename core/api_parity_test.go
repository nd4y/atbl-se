package core

import (
	"encoding/json"
	"github.com/nd4y/atblse/internal/protocol"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemainingCompatibilityJSONContracts(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, testScript) }))
	defer source.Close()
	s, sim := fixture(t, func(c *Config) {
		c.CompatibilityControl = true
		c.ExperimentalSyncScripts = true
		c.ExperimentalLocalScripts = true
		c.MaxSpeed = 100
		c.ScriptURLOrigins = []string{source.URL}
	})
	pair(t, s)
	token, _, err := s.scripts.put([]protocol.SyncAction{{At: 0, Pos: 0}, {At: 1000, Pos: 60}, {At: 2000, Pos: 0}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path string
		body       any
		status     int
	}{
		{"token-extra", "/vacuglide/sync-script/load-token", map[string]any{"scriptToken": token, "extra": true}, 200},
		{"url-extra", "/vacuglide/sync-script/upload-funscript-url", map[string]any{"url": source.URL, "extra": true}, 200},
		{"swap-extra", "/vacuglide/local-script/swap", map[string]any{"scriptIndexA": 0, "scriptIndexB": 1, "extra": true}, 200},
		{"move-extra", "/vacuglide/local-script/move", map[string]any{"fromIndex": 1, "toIndex": 0, "extra": true}, 200},
		{"replace-extra", "/vacuglide/local-script/replace", map[string]any{"replacedLocalScriptIndex": 1, "newLocalScriptUuid": token, "extra": true}, 200},
		{"move-other-operation-fields", "/vacuglide/local-script/move", map[string]any{"fromIndex": 0, "toIndex": 1, "scriptIndexA": map[string]any{}}, 200},
		{"token-missing", "/vacuglide/sync-script/load-token", map[string]any{}, 400},
		{"url-missing", "/vacuglide/sync-script/upload-funscript-url", map[string]any{}, 400},
		{"token-case", "/vacuglide/sync-script/load-token", map[string]any{"SCRIPTTOKEN": token}, 400},
		{"url-case", "/vacuglide/sync-script/upload-funscript-url", map[string]any{"URL": source.URL}, 400},
		{"swap-case", "/vacuglide/local-script/swap", map[string]any{"SCRIPTINDEXA": 0, "scriptIndexB": 1}, 400},
		{"move-case", "/vacuglide/local-script/move", map[string]any{"fromIndex": 0, "TOINDEX": 1}, 400},
		{"replace-case", "/vacuglide/local-script/replace", map[string]any{"replacedLocalScriptIndex": 1, "NEWLOCALSCRIPTUUID": token}, 400},
		{"token-type", "/vacuglide/sync-script/load-token", map[string]any{"scriptToken": map[string]any{}}, 400},
		{"swap-type", "/vacuglide/local-script/swap", map[string]any{"scriptIndexA": map[string]any{}, "scriptIndexB": 1}, 400},
		{"swap-negative", "/vacuglide/local-script/swap", map[string]any{"scriptIndexA": -1, "scriptIndexB": 1}, 400},
		{"move-outside", "/vacuglide/local-script/move", map[string]any{"fromIndex": 0, "toIndex": 30}, 400},
		{"replace-outside", "/vacuglide/local-script/replace", map[string]any{"replacedLocalScriptIndex": 30, "newLocalScriptUuid": token}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sim.mu.Lock()
			before := len(sim.wires)
			sim.mu.Unlock()
			status, result := compatCall(t, s, "PUT", tc.path, tc.body, s.Token())
			if status != tc.status {
				t.Fatal(status, result)
			}
			if status == 400 {
				e, ok := result["error"].(map[string]any)
				if !ok || e["code"] != "FST_ERR_VALIDATION" || e["message"] == "" {
					t.Fatal("wrong compatibility error", result)
				}
				sim.mu.Lock()
				sent := len(sim.wires) - before
				sim.mu.Unlock()
				if sent != 0 {
					t.Fatal("invalid request sent commands", sent)
				}
			}
		})
	}
}

func TestValidNonObjectFunscriptIsNotSyntaxError(t *testing.T) {
	for _, raw := range []string{`[]`, `true`, `123`, `"abc"`} {
		if !json.Valid([]byte(raw)) {
			t.Fatal("invalid reference")
		}
		_, err := parseActions([]byte(raw), "upload-funscript")
		e, ok := err.(*protocol.ConversionError)
		if !ok || e.Code != "Error" || e.Message != "Funscript data is missing actions" {
			t.Fatal(raw, err)
		}
	}
}
