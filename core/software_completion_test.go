package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOfficialHTTPEnvelopeCorpus(t *testing.T) {
	raw, err := os.ReadFile("../internal/protocol/testdata/cloud-http-edge-20261029.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Rest  string
		Body        string `json:"http_body"`
		ContentType string `json:"content_type"`
		Status      int
		Response    map[string]any
	}
	if err = json.Unmarshal(raw, &cases); err != nil || len(cases) != 14 {
		t.Fatal(err, len(cases))
	}
	s, sim := slotFixture(t)
	sim.mu.Lock()
	for i := range sim.slots {
		sim.slots[i] = uint32(i + 1)
	}
	sim.mu.Unlock()
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			r, _ := http.NewRequest("PUT", "http://"+s.Address()+c.Rest, strings.NewReader(c.Body))
			r.Header.Set("x-device-token", s.Token())
			r.Header.Set("Content-Type", c.ContentType)
			response, err := http.DefaultClient.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			var result map[string]any
			if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != c.Status || !reflect.DeepEqual(result, c.Response) {
				t.Fatalf("got%d %v want%d %v", response.StatusCode, result, c.Status, c.Response)
			}
		})
	}
}

func TestAllFactoryCatalogReplacements(t *testing.T) {
	var records []catalogRecord
	if err := json.Unmarshal(factoryCatalog, &records); err != nil || len(records) != 30 {
		t.Fatal("factory inventory", err, len(records))
	}
	s, sim := slotFixture(t)
	ids := make(map[uint32]bool)
	for _, record := range records {
		t.Run(record.UUID, func(t *testing.T) {
			data, err := s.localReplacement(record.UUID)
			if err != nil {
				t.Fatal(err)
			}
			if data.id < 1 || data.id > 30 || ids[data.id] {
				t.Fatal("duplicate or unexpected ID", data.id)
			}
			ids[data.id] = true
			code, result := compatCall(t, s, "PUT", "/vacuglide/local-script/replace", map[string]any{"replacedLocalScriptIndex": 29, "newLocalScriptUuid": record.UUID}, s.Token())
			if code != 200 {
				t.Fatal(code, result)
			}
			sim.mu.Lock()
			got := append([]byte(nil), sim.localLoadedBytes...)
			sim.mu.Unlock()
			if !bytes.Equal(got, record.Data) {
				t.Fatal("factory binary changed during delivery")
			}
		})
	}
}

func TestCatalogQuotaLockAndOverride(t *testing.T) {
	var records []catalogRecord
	json.Unmarshal(factoryCatalog, &records)
	r := records[0]
	dir := t.TempDir()
	catalogDir := filepath.Join(dir, "local-catalog")
	os.Mkdir(catalogDir, 0700)
	lock := filepath.Join(catalogDir, ".import-lock")
	os.Mkdir(lock, 0700)
	if err := ImportLocalScript(dir, r.UUID, r.Data); err == nil {
		t.Fatal("concurrent import lock ignored")
	}
	os.Remove(lock)
	if err := ImportLocalScript(dir, r.UUID, r.Data); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < maxStoredScripts; i++ {
		if err := os.WriteFile(filepath.Join(catalogDir, fmt.Sprintf("filler-%d.json", i)), []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := ImportLocalScript(dir, records[1].UUID, records[1].Data); err == nil {
		t.Fatal("quota bypassed")
	}
	changed := append([]byte(nil), r.Data...)
	changed[13] = 1
	if err := ImportLocalScript(dir, r.UUID, changed); err != nil {
		t.Fatal("replacement at quota rejected", err)
	}
	s, err := New(Config{DataDir: dir, MaxSpeed: 100})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.localReplacement(r.UUID)
	if err != nil || !bytes.Equal(got.binary, changed) {
		t.Fatal("override lost", err)
	}
	os.WriteFile(filepath.Join(catalogDir, r.UUID+".json"), []byte("broken"), 0600)
	if _, err = s.localReplacement(r.UUID); err == nil {
		t.Fatal("corrupt override silently fell back to factory")
	}
}
