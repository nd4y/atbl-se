package core

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type catalogReference struct {
	Catalog struct {
		UUID, SHA256, Hex string
		ID                uint32
	}
	Cases []struct {
		Name, Rest string
		Request    json.RawMessage `json:"request_body"`
		Status     int
		Response   map[string]any
		Policy     string `json:"local_policy"`
		Wires      []struct {
			Command string
			Args    []string
		}
	}
}

func readCatalogReference(t *testing.T) catalogReference {
	t.Helper()
	raw, err := os.ReadFile("../internal/protocol/testdata/cloud-catalog-edge-20261029.json")
	if err != nil {
		t.Fatal(err)
	}
	var ref catalogReference
	if err = json.Unmarshal(raw, &ref); err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestOfficialCatalogAndValidationCorpus(t *testing.T) {
	ref := readCatalogReference(t)
	if len(ref.Cases) != 27 {
		t.Fatal("incomplete catalog corpus")
	}
	binary, err := hex.DecodeString(ref.Catalog.Hex)
	if err != nil {
		t.Fatal(err)
	}
	s, sim := slotFixture(t)
	dir := filepath.Join(s.cfg.DataDir, "local-catalog")
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	record, _ := json.Marshal(map[string]any{"version": 1, "uuid": ref.Catalog.UUID, "sha256": ref.Catalog.SHA256, "data": binary})
	if err = os.WriteFile(filepath.Join(dir, ref.Catalog.UUID+".json"), record, 0600); err != nil {
		t.Fatal(err)
	}
	for _, c := range ref.Cases {
		t.Run(c.Name, func(t *testing.T) {
			var request any
			if err := json.Unmarshal(bytes.ReplaceAll(c.Request, []byte("$CATALOG"), []byte(ref.Catalog.UUID)), &request); err != nil {
				t.Fatal(err)
			}
			sim.mu.Lock()
			before := len(sim.wires)
			sim.mu.Unlock()
			status, body := compatCall(t, s, "PUT", c.Rest, request, s.Token())
			sim.mu.Lock()
			all := append([]string(nil), sim.wires[before:]...)
			delivered := append([]byte(nil), sim.localLoadedBytes...)
			sim.mu.Unlock()
			if c.Policy == "bounded-slot" {
				if status != 400 || len(all) != 0 {
					t.Fatal("unsafe slot admitted", status, all)
				}
				return
			}
			if status != c.Status {
				t.Fatal("HTTP differs", status, c.Status, body)
			}
			if status == 400 {
				if len(all) != 0 || !reflect.DeepEqual(body["error"], c.Response["error"]) {
					t.Fatal("validation differs", body, c.Response, all)
				}
				return
			}
			var commands []string
			for _, wire := range all {
				if !strings.HasPrefix(wire, "/state/") {
					commands = append(commands, wire)
				}
			}
			if len(commands) != len(c.Wires) {
				t.Fatal(commands, c.Wires)
			}
			for i, w := range c.Wires {
				if !strings.HasPrefix(commands[i], "/"+w.Command+"/") || !reflect.DeepEqual(splitWireArguments(commands[i]), w.Args) {
					t.Fatal("cloud wire differs", commands, w)
				}
			}
			if strings.HasPrefix(c.Name, "catalog-") && !bytes.Equal(delivered, binary) {
				t.Fatal("catalog bytes changed during offline delivery")
			}
		})
	}
	if _, err := sim.fetchScriptKind("local", "1"); err == nil || err.Error() != "download: 404" {
		t.Fatal("catalog remains available after operation", err)
	}
}
