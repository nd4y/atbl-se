package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func waitForSyncCommand(t *testing.T, sim *simulator) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		sim.mu.Lock()
		token := sim.scriptToken
		ready := len(sim.loadedBytes) > 0
		sim.mu.Unlock()
		if ready {
			return token
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no sync load command")
	return ""
}

func TestReviewStopCancelsSyncWait(t *testing.T) {
	for _, api := range []string{"panel", "compatibility"} {
		t.Run(api, func(t *testing.T) {
			s, sim := syncFixture(t)
			hold := make(chan struct{})
			defer close(hold)
			sim.mu.Lock()
			sim.holdSync = hold
			sim.mu.Unlock()
			done := make(chan int, 1)
			go func() { code, _ := uploadCall(t, s, "upload-funscript", testScript); done <- code }()
			token := waitForSyncCommand(t, sim)
			var code int
			if api == "panel" {
				code, _ = call(t, s, "POST", "/api/stop", map[string]any{}, s.Token())
			} else {
				code, _ = compatCall(t, s, "PUT", "/vacuglide/target-speed/stop", nil, s.Token())
			}
			if code != 200 {
				t.Fatal("STOP", code)
			}
			if _, err := sim.fetchScript(token); err == nil {
				t.Error("cancelled download remains authorized")
			}
			select {
			case code = <-done:
				if code != 409 {
					t.Fatal("cancelled request", code)
				}
			case <-time.After(time.Second):
				t.Fatal("STOP left upload holding control until device acknowledgement")
			}
			code, _ = compatCall(t, s, "PUT", "/vacuglide/target-speed", map[string]any{"targetSpeed": 10}, s.Token())
			if code != 200 {
				t.Fatal("new control blocked after STOP", code)
			}
		})
	}
}

func TestReviewSyncGrantExpiresAfterLoad(t *testing.T) {
	s, sim := syncFixture(t)
	code, result := uploadCall(t, s, "upload-funscript", testScript)
	if code != 200 {
		t.Fatal(code, result)
	}
	if _, err := sim.fetchScript(result["syncScriptToken"].(string)); err == nil {
		t.Fatal("completed download remains authorized")
	}
	code, result = compatCall(t, s, "PUT", "/vacuglide/sync-script/start", map[string]int{"startTimeMs": 0}, s.Token())
	if code != 200 {
		t.Fatal("delivery expiry invalidated loaded script", code, result)
	}
}

func TestReviewSyncDeliverySnapshot(t *testing.T) {
	s, sim := syncFixture(t)
	hold := make(chan struct{})
	defer close(hold)
	sim.mu.Lock()
	sim.holdSync = hold
	sim.mu.Unlock()
	done := make(chan int, 1)
	go func() { code, _ := uploadCall(t, s, "upload-funscript", testScript); done <- code }()
	token := waitForSyncCommand(t, sim)
	sim.mu.Lock()
	expected := append([]byte(nil), sim.loadedBytes...)
	sim.mu.Unlock()
	path := filepath.Join(s.scripts.dir, token+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record storedScript
	if err = json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	record.Actions[1].Pos = 6
	raw, _ = json.Marshal(record.Actions)
	sum := sha256.Sum256(raw)
	record.SHA256 = hex.EncodeToString(sum[:])
	raw, _ = json.Marshal(record)
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := sim.fetchScript(token)
	if err != nil || !bytes.Equal(got, expected) {
		t.Fatal("in-flight binary changed", err)
	}
	// End the request with STOP while the synthetic reply remains withheld.
	call(t, s, "POST", "/api/stop", map[string]any{}, s.Token())
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled snapshot request retained ownership")
	}
}
