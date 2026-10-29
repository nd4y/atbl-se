package core

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func replaceSeed(value any, seed string) any {
	switch v := value.(type) {
	case string:
		if v == "$SEED" {
			return seed
		}
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = replaceSeed(item, seed)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[k] = replaceSeed(item, seed)
		}
		return out
	}
	return value
}

func TestOfficialRemainingAPICorpus(t *testing.T) {
	raw, err := os.ReadFile("../internal/protocol/testdata/cloud-api-edge-20261029.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Rest, Raw string
		Request         any `json:"request_body"`
		Status          int
		Response        map[string]any
		Policy          string `json:"local_policy"`
		Wires           []struct {
			Command string
			Args    []string
		}
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 74 {
		t.Fatal("incomplete cloud reference", len(cases))
	}
	s, sim := fixture(t, func(c *Config) {
		c.CompatibilityControl = true
		c.ExperimentalLocalScripts = true
		c.ExperimentalSyncScripts = true
		c.MaxSpeed = 100
	})
	pair(t, s)
	seed := ""
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			sim.mu.Lock()
			before := len(sim.wires)
			sim.mu.Unlock()
			var status int
			var body map[string]any
			if c.Rest == "" {
				status, body = uploadCall(t, s, "upload-funscript", c.Raw)
			} else {
				status, body = compatCall(t, s, "PUT", c.Rest, replaceSeed(c.Request, seed), s.Token())
			}
			sim.mu.Lock()
			all := append([]string(nil), sim.wires[before:]...)
			sim.mu.Unlock()
			var commands []string
			for _, wire := range all {
				if !strings.HasPrefix(wire, "/state/") {
					commands = append(commands, wire)
				}
			}
			switch c.Policy {
			case "local-token-required":
				if status != 404 || len(all) != 0 {
					t.Fatal("missing local token admitted", status, body, all)
				}
				if body["error"].(map[string]any)["code"] != "SCRIPT_NOT_FOUND" {
					t.Fatal(body)
				}
				return
			case "local-slot-validation":
				if status != 400 || len(all) != 0 {
					t.Fatal("unsafe local slot input admitted", status, body, all)
				}
				return
			case "local-catalog":
				// Official sync tokens are not catalog entries. Our local library is
				// deliberately different; test delivery without claiming cloud wire evidence.
				index := "0"
				if c.Name == "replace-fractional-index" {
					index = "0.5"
				}
				if status != 200 || len(commands) != 1 {
					t.Fatal(status, body, commands)
				}
				if got := splitWireArguments(commands[0]); !reflect.DeepEqual(got, []string{index, seed}) {
					t.Fatal(got)
				}
				return
			case "exact":
			default:
				t.Fatal("unreviewed policy", c.Policy)
			}
			if status != c.Status {
				t.Fatal("cloud HTTP differs", status, c.Status, body)
			}
			if status == 400 {
				if len(all) != 0 {
					t.Fatal("rejected request reached device", all)
				}
				if !reflect.DeepEqual(body["error"], c.Response["error"]) {
					t.Fatal("cloud error differs", body["error"], c.Response["error"])
				}
				return
			}
			if c.Name == "seed" {
				seed, _ = body["syncScriptToken"].(string)
				if seed == "" {
					t.Fatal(body)
				}
			}
			if len(commands) != len(c.Wires) {
				t.Fatal(commands, c.Wires)
			}
			for i, observed := range c.Wires {
				parts := strings.Split(commands[i], "/")
				if parts[1] != observed.Command {
					t.Fatal(parts, observed)
				}
				want := make([]string, len(observed.Args))
				for j, v := range observed.Args {
					want[j] = strings.ReplaceAll(v, "$SEED", seed)
				}
				if !reflect.DeepEqual(splitWireArguments(commands[i]), want) {
					t.Fatal("cloud wire differs", commands[i], want)
				}
			}
		})
	}
}
