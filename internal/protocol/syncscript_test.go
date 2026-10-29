package protocol

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestObservedSyncScriptBinaries(t *testing.T) {
	data, err := os.ReadFile("testdata/sync-script-observed-20261027.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Actions []SyncAction `json:"actions"`
		Hex     string       `json:"hex"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) != 11 {
		t.Fatalf("got %d captured fixtures", len(fixtures))
	}
	for i, fixture := range fixtures {
		got, err := EncodeSyncScriptCandidate(fixture.Actions)
		if err != nil {
			t.Fatalf("fixture %d: %v", i+1, err)
		}
		if hex.EncodeToString(got) != fixture.Hex {
			t.Errorf("fixture %d differs from official bytes", i+1)
		}
	}
}

func TestObservedSyncCommands(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []int
		prefix string
	}{
		{"sync-script-start", []int{0}, "/sync-script-start/0/"},
		{"sync-script-stop", nil, "/sync-script-stop/"},
		{"sync-script-offset", []int{100}, "/sync-script-offset/100/"},
		{"sync-script-loop", []int{1}, "/sync-script-loop/1/"},
	} {
		id, wire, err := Command(tc.name, tc.args...)
		if err != nil || wire != tc.prefix+id || !isCanonicalUUID(id) {
			t.Fatal(wire, err)
		}
	}
	token := "11111111-1111-4111-8111-111111111111"
	id, wire, err := SyncLoadCommand(token)
	if err != nil || wire != "/sync-script-load-token/"+token+"/"+id {
		t.Fatal(wire, err)
	}
	for _, token := range []string{"", strings.Repeat("a", 36), "../x", token + "/extra"} {
		if _, _, err := SyncLoadCommand(token); err == nil {
			t.Fatal("invalid token accepted")
		}
	}
	for _, tc := range []struct {
		name string
		args []int
	}{
		{"sync-script-start", []int{-1}}, {"sync-script-start", []int{16777215}},
		{"sync-script-offset", []int{-16777215}}, {"sync-script-offset", nil},
		{"sync-script-loop", []int{2}}, {"sync-script-stop", []int{0}},
	} {
		if _, _, err := Command(tc.name, tc.args...); err == nil {
			t.Fatal(tc)
		}
	}
}

func TestSyncScriptCandidateRejectsUnsafeInputs(t *testing.T) {
	for _, actions := range [][]SyncAction{
		nil,
		{{At: 0, Pos: -1}, {At: 1, Pos: 0}},
		{{At: 0, Pos: 0}, {At: 1, Pos: 256}},
		{{At: 0, Pos: 0}, {At: 16777215, Pos: 100}},
	} {
		if _, err := EncodeSyncScriptCandidate(actions); err == nil {
			t.Errorf("accepted unsupported input %#v", actions)
		}
	}
}
