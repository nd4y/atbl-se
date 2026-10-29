package core

import (
	"autoblow.local/emulator/internal/protocol"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func splitWireArguments(wire string) []string {
	parts := strings.Split(wire, "/")
	return parts[2 : len(parts)-1]
}

type cloudConversionCase struct {
	Case      string          `json:"case"`
	Format    string          `json:"format"`
	Input     json.RawMessage `json:"input"`
	Raw       string          `json:"raw"`
	Generator *struct {
		Count int `json:"count"`
	} `json:"generator"`
	Status int                            `json:"cloud_status"`
	Hex    string                         `json:"hex"`
	SHA    string                         `json:"sha256"`
	Bytes  int                            `json:"bytes"`
	Policy string                         `json:"delivery_policy"`
	Error  struct{ Code, Message string } `json:"error"`
}

func TestOfficialConverterEdgeCorpus(t *testing.T) {
	raw, err := os.ReadFile("../internal/protocol/testdata/cloud-converter-edge-20261028.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []cloudConversionCase
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 80 {
		t.Fatal("incomplete official corpus", len(cases))
	}
	for _, c := range cases {
		t.Run(c.Case, func(t *testing.T) {
			input := c.Input
			if c.Format == "csv" {
				input = []byte(c.Raw)
			}
			if c.Generator != nil {
				a := make([]protocol.SyncAction, c.Generator.Count)
				for i := range a {
					a[i] = protocol.SyncAction{At: float64(i), Pos: float64(i % 2 * 100)}
				}
				input, _ = json.Marshal(map[string]any{"actions": a})
			}
			actions, err := parseActions(input, "upload-"+c.Format)
			var binary []byte
			if err == nil {
				binary, err = protocol.EncodeSyncScriptCandidate(actions)
			}
			if c.Status == 400 {
				var conversion *protocol.ConversionError
				if !errors.As(err, &conversion) || conversion.Code != c.Error.Code || conversion.Message != c.Error.Message {
					t.Fatalf("cloud rejection differs: got%v want%+v", err, c.Error)
				}
				return
			}
			if err != nil {
				t.Fatal("cloud accepted input rejected", err)
			}
			sum := sha256.Sum256(binary)
			if len(binary) != c.Bytes || hex.EncodeToString(sum[:]) != c.SHA || c.Hex != "" && hex.EncodeToString(binary) != c.Hex {
				t.Fatalf("official binary differs: got%x want%s", binary[:min(len(binary), 100)], c.Hex)
			}
			_, err = makeScript(actions)
			if c.Policy == "playable" && err != nil {
				t.Fatal("safe binary rejected for delivery", err)
			}
			if c.Policy != "playable" && err == nil {
				t.Fatal("unsafe cloud artifact admitted for firmware load", c.Policy)
			}
		})
	}
}

func TestOfficialRESTCoercionCorpus(t *testing.T) {
	raw, err := os.ReadFile("../internal/protocol/testdata/cloud-rest-coercion-20261028.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Case, Path string
		Request    json.RawMessage
		Status     int
		Wire       []string `json:"wire_arguments"`
		Response   map[string]any
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 29 {
		t.Fatal(len(cases))
	}
	s, sim := fixture(t, func(c *Config) {
		c.CompatibilityControl = true
		c.ExperimentalLocalScripts = true
		c.ExperimentalSyncScripts = true
		c.MaxSpeed = 100
	})
	pair(t, s)
	if code, r := uploadCall(t, s, "upload-funscript", testScript); code != 200 {
		t.Fatal(code, r)
	}
	for _, c := range cases {
		t.Run(c.Case, func(t *testing.T) {
			var body any
			if err := json.Unmarshal(c.Request, &body); err != nil {
				t.Fatal(err)
			}
			sim.mu.Lock()
			before := len(sim.wires)
			sim.mu.Unlock()
			code, result := compatCall(t, s, "PUT", c.Path, body, s.Token())
			if code != c.Status {
				t.Fatal("HTTP mismatch", code, c.Status, result)
			}
			sim.mu.Lock()
			wires := append([]string(nil), sim.wires[before:]...)
			sim.mu.Unlock()
			if code == 400 {
				if len(wires) != 0 {
					t.Fatal("invalid input reached firmware", wires)
				}
				want := c.Response["error"].(map[string]any)
				got := result["error"].(map[string]any)
				if got["code"] != want["code"] || got["message"] != want["message"] {
					t.Fatal(got, want)
				}
				return
			}
			if len(wires) != 1 {
				t.Fatal("unexpected wire operations", wires)
			}
			parts := splitWireArguments(wires[0])
			if len(parts) != len(c.Wire) {
				t.Fatal(parts, c.Wire)
			}
			for i := range parts {
				if parts[i] != c.Wire[i] {
					t.Fatal(parts, c.Wire)
				}
			}
			// Cloud response state is supplied by a software peer; compare only
			// the affected field, whose atoi semantics were recovered statically.
			field := map[string]string{"/vacuglide/target-speed": "targetSpeed", "/vacuglide/local-script": "localScript", "/vacuglide/sync-script/start": "syncScriptCurrentTime", "/vacuglide/sync-script/offset": "syncScriptOffsetTime", "/vacuglide/sync-script/loop": "syncScriptLoop", "/vacuglide/valve/stroke-plus": "strokePlusValve"}[c.Path]
			if result[field] != c.Response[field] {
				t.Fatal("native effect mismatch", field, result[field], c.Response[field])
			}
		})
	}
}
