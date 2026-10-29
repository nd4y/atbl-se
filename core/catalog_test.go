package core

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCatalogImportIntegrityAndEvents(t *testing.T) {
	ref := readCatalogReference(t)
	data, _ := hex.DecodeString(ref.Catalog.Hex)
	dir := t.TempDir()
	if err := ImportLocalScript(dir, ref.Catalog.UUID, data); err != nil {
		t.Fatal(err)
	}
	got, err := readLocalCatalog(dir, ref.Catalog.UUID)
	if err != nil || got.id != 1 || got.token != "1" || got.peak != 95 || !bytes.Equal(got.binary, data) {
		t.Fatal(got, err)
	}
	// Reopening the persisted file must preserve nonzero valve bytes, not derive
	// a new speed-only timeline. Valve values must not inflate the speed ceiling.
	valves := make([]byte, 20)
	binary.LittleEndian.PutUint32(valves, 7)
	binary.LittleEndian.PutUint32(valves[4:], 1)
	valves[12], valves[13] = 1, 255
	valves[18], valves[19] = 2, 1
	v, err := nativeCatalogData(valves)
	if err != nil || v.peak != 0 || !bytes.Equal(v.binary, valves) {
		t.Fatal(v, err)
	}
	for _, tc := range []struct {
		name   string
		change func([]byte) []byte
	}{
		{"empty", func(b []byte) []byte { return nil }},
		{"header-only", func(b []byte) []byte { return b[:8] }},
		{"truncated", func(b []byte) []byte { return b[:len(b)-1] }},
		{"zero-id", func(b []byte) []byte { binary.LittleEndian.PutUint32(b, 0); return b }},
		{"header-version", func(b []byte) []byte { b[4] = 2; return b }},
		{"unknown-opcode", func(b []byte) []byte { b[12] = 3; return b }},
		{"speed", func(b []byte) []byte { b[12], b[13] = 0, 101; return b }},
		{"time-limit", func(b []byte) []byte { binary.LittleEndian.PutUint32(b[8:], 0xffffffff); return b }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ImportLocalScript(dir, ref.Catalog.UUID, tc.change(append([]byte(nil), data...))); err == nil {
				t.Fatal("bad binary imported")
			}
		})
	}
	path := filepath.Join(dir, "local-catalog", ref.Catalog.UUID+".json")
	raw, _ := os.ReadFile(path)
	var record catalogRecord
	if err = json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	record.Data[13] ^= 1
	bad, _ := json.Marshal(record)
	if err = os.WriteFile(path, bad, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = readLocalCatalog(dir, ref.Catalog.UUID); err == nil {
		t.Fatal("checksum corruption accepted")
	}
	if err = ImportLocalScript(dir, "../../outside", data); err == nil {
		t.Fatal("path accepted")
	}
}

func TestCatalogSpeedLimitAndSyncSeparation(t *testing.T) {
	ref := readCatalogReference(t)
	data, _ := hex.DecodeString(ref.Catalog.Hex)
	s, sim := fixture(t, func(c *Config) {
		c.CompatibilityControl = true
		c.ExperimentalLocalScripts = true
		c.ExperimentalSyncScripts = true
		c.MaxSpeed = 15
	})
	pair(t, s)
	if err := ImportLocalScript(s.cfg.DataDir, ref.Catalog.UUID, data); err != nil {
		t.Fatal(err)
	}
	sim.mu.Lock()
	before := len(sim.wires)
	sim.mu.Unlock()
	status, body := compatCall(t, s, "PUT", "/vacuglide/local-script/replace", map[string]any{"replacedLocalScriptIndex": 0, "newLocalScriptUuid": ref.Catalog.UUID}, s.Token())
	if status != 403 || body["error"].(map[string]any)["code"] != "SCRIPT_SPEED_LIMIT" {
		t.Fatal(status, body)
	}
	status, body = compatCall(t, s, "PUT", "/vacuglide/sync-script/load-token", map[string]any{"scriptToken": ref.Catalog.UUID}, s.Token())
	if status != 404 {
		t.Fatal("catalog authorized as sync script", status, body)
	}
	sim.mu.Lock()
	defer sim.mu.Unlock()
	if len(sim.wires) != before {
		t.Fatal("rejected import sent command")
	}
}

func TestCatalogDeliverySnapshotAndStop(t *testing.T) {
	ref := readCatalogReference(t)
	data, _ := hex.DecodeString(ref.Catalog.Hex)
	s, sim := slotFixture(t)
	if err := ImportLocalScript(s.cfg.DataDir, ref.Catalog.UUID, data); err != nil {
		t.Fatal(err)
	}
	hold := make(chan struct{})
	defer close(hold)
	sim.mu.Lock()
	sim.holdMutation = hold
	sim.mu.Unlock()
	done := make(chan int, 1)
	go func() {
		code, _ := compatCall(t, s, "PUT", "/vacuglide/local-script/replace", map[string]any{"replacedLocalScriptIndex": 0, "newLocalScriptUuid": ref.Catalog.UUID}, s.Token())
		done <- code
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		sim.mu.Lock()
		ready := len(sim.localLoadedBytes) > 0
		sim.mu.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no catalog delivery")
		}
		time.Sleep(5 * time.Millisecond)
	}
	changed := append([]byte(nil), data...)
	changed[13] ^= 1
	if err := ImportLocalScript(s.cfg.DataDir, ref.Catalog.UUID, changed); err != nil {
		t.Fatal(err)
	}
	got, err := sim.fetchScriptKind("local", "1")
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("pending delivery changed", err)
	}
	if _, err = sim.fetchScriptKind("local", ref.Catalog.UUID); err == nil {
		t.Fatal("UUID aliases authorized numeric download")
	}
	if _, err = sim.fetchScriptKind("sync", "1"); err == nil {
		t.Fatal("catalog grant leaked to sync path")
	}
	code, body := compatCall(t, s, "PUT", "/vacuglide/target-speed/stop", nil, s.Token())
	if code != 200 {
		t.Fatal(code, body)
	}
	if _, err = sim.fetchScriptKind("local", "1"); err == nil {
		t.Fatal("STOP retained catalog grant")
	}
	select {
	case code := <-done:
		if code == 200 {
			t.Fatal("superseded replace succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("replace did not cancel")
	}
}
