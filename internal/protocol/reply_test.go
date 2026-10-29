package protocol

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestCapturedStockListAndErrorReplies(t *testing.T) {
	raw, err := os.ReadFile("testdata/stock-replies-20261027.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Wire string `json:"wire"`
	}
	if err = json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) != 3 {
		t.Fatal(len(fixtures))
	}
	for _, f := range fixtures {
		r, err := ParseResponse(f.Wire)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(f.Wire, "/ok/") {
			if r.Kind != "slots" || len(r.Scripts) != 30 || r.Scripts[3] != 11265 || r.Scripts[29] != 30 {
				t.Fatal(r)
			}
		} else {
			var e *FirmwareError
			if !errors.As(r.Err, &e) || e.Code != "SYNC_SCRIPT_LOAD_TOKEN_ERROR" {
				t.Fatal(r)
			}
		}
	}
}
func TestReplyShapesAndLocalCommands(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	for _, raw := range []string{"/ok/1,2/" + id, "/ok/" + strings.Repeat("1,", 29) + "-1/" + id, "/ok/" + strings.Repeat("1,", 29) + "4294967296/" + id, "/error/code/" + id, "/error/ANY_ERROR/not-a-uuid", "/ok/" + strings.Repeat("1,", 29) + "1/" + id + "/extra"} {
		if _, err := ParseResponse(raw); err == nil {
			t.Fatal(raw)
		}
	}
	if r, err := ParseResponse("/ok/" + strings.Repeat("1,", 29) + "4294967295/" + id); err != nil || r.Scripts[29] != 4294967295 {
		t.Fatal("unsigned firmware script identity rejected", r, err)
	}
	for _, name := range []string{"local-script-start", "local-script-stop", "local-script-loaded"} {
		id, wire, err := Command(name)
		if err != nil || wire != "/"+name+"/"+id {
			t.Fatal(wire, err)
		}
	}
	for _, n := range []int{-1, 30} {
		if _, _, err := Command("local-script-set", n); err == nil {
			t.Fatal(n)
		}
	}
	id, wire, err := Command("local-script-set", 29)
	if err != nil || wire != "/local-script-set/29/"+id {
		t.Fatal(wire, err)
	}
}
