package core

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

// This fixture is extracted from the published SDK, not from our mode decoder.
func TestOfficialVacuglideModeContract(t *testing.T) {
	data, err := os.ReadFile("testdata/vacuglide-sdk-2.6.0-modes.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Modes map[string]string `json:"modes"`
	}
	if err = json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if len(reference.Modes) != 14 {
		t.Fatal("incomplete published mode fixture")
	}
	s, sim := fixture(t)
	for code, name := range reference.Modes {
		t.Run(name, func(t *testing.T) {
			n, err := strconv.Atoi(code)
			if err != nil {
				t.Fatal(err)
			}
			sim.mu.Lock()
			sim.mode = n
			sim.mu.Unlock()
			status, result := call(t, s, "GET", "/api/state", nil, s.Token())
			if status != 200 || result["operationalMode"] != name || result["rawOperationalMode"] != float64(n) {
				t.Fatalf("HTTP %d, state %v; expected %s (%s)", status, result, name, code)
			}
		})
	}
}
