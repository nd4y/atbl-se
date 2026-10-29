package core

import "testing"

func TestSyncSeekStopsAtLastEncodedTimestamp(t *testing.T) {
	s, sim := syncFixture(t)
	if code, r := uploadCall(t, s, "upload-funscript", testScript); code != 200 {
		t.Fatal(code, r)
	}
	sim.mu.Lock()
	before := len(sim.commands)
	sim.mu.Unlock()
	for _, at := range []int{1001, 1500, 2000} {
		if code, r := compatCall(t, s, "PUT", "/vacuglide/sync-script/start", map[string]int{"startTimeMs": at}, s.Token()); code != 400 {
			t.Fatal(at, code, r)
		}
	}
	sim.mu.Lock()
	after := len(sim.commands)
	sim.mu.Unlock()
	if before != after {
		t.Fatal("out-of-bounds firmware seek sent")
	}
	if code, r := compatCall(t, s, "PUT", "/vacuglide/sync-script/start", map[string]int{"startTimeMs": 1000}, s.Token()); code != 200 {
		t.Fatal("last encoded timestamp rejected", code, r)
	}
}

func TestImmediateNaturalFinishIsNotAnUnconfirmedStart(t *testing.T) {
	s, sim := syncFixture(t)
	if code, r := uploadCall(t, s, "upload-funscript", `{"actions":[{"at":0,"pos":0},{"at":1000,"pos":90}]}`); code != 200 {
		t.Fatal(code, r)
	}
	sim.mu.Lock()
	sim.syncFinishOnStart = true
	sim.mu.Unlock()
	code, r := compatCall(t, s, "PUT", "/vacuglide/sync-script/start", map[string]int{"startTimeMs": 0}, s.Token())
	if code != 200 || r["operationalMode"] != "SYNC_SCRIPT_PAUSED" {
		t.Fatal(code, r)
	}
}

func TestPositiveOffsetNaturalFinishUsesEffectiveTime(t *testing.T) {
	s, sim := syncFixture(t)
	if code, r := uploadCall(t, s, "upload-funscript", testScript); code != 200 {
		t.Fatal(code, r)
	}
	if code, r := compatCall(t, s, "PUT", "/vacuglide/sync-script/offset", map[string]int{"offsetTimeMs": 3000}, s.Token()); code != 200 {
		t.Fatal(code, r)
	}
	sim.mu.Lock()
	sim.syncFinishOnStart = true
	sim.mu.Unlock()
	code, r := compatCall(t, s, "PUT", "/vacuglide/sync-script/start", map[string]int{"startTimeMs": 0}, s.Token())
	if code != 200 || r["operationalMode"] != "SYNC_SCRIPT_PAUSED" || r["syncScriptCurrentTime"] != float64(0) {
		t.Fatal(code, r)
	}
}

func TestScientificNumberCannotBypassEffectiveSpeedCeiling(t *testing.T) {
	s, sim := fixture(t, func(c *Config) { c.CompatibilityControl = true; c.MaxSpeed = 5 })
	pair(t, s)
	sim.mu.Lock()
	before := len(sim.commands)
	sim.mu.Unlock()
	if code, r := compatCall(t, s, "PUT", "/vacuglide/target-speed", map[string]float64{"targetSpeed": 9e-7}, s.Token()); code != 400 {
		t.Fatal(code, r)
	}
	sim.mu.Lock()
	after := len(sim.commands)
	sim.mu.Unlock()
	if before != after {
		t.Fatal("effective speed exceeded configured limit on wire")
	}
}
