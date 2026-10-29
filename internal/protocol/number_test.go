package protocol

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"testing"
)

func TestBufferNumericErrorsMatchPinnedNode(t *testing.T) {
	raw, err := os.ReadFile("testdata/node-buffer-review-20261029.json")
	if err != nil {
		t.Fatal(err)
	}
	var ref struct {
		Cases []struct {
			Field         string
			Source        any
			Accepted      bool
			Code, Message string
		}
	}
	if err = json.Unmarshal(raw, &ref); err != nil {
		t.Fatal(err)
	}
	if len(ref.Cases) != 32 {
		t.Fatal("incomplete runtime reference", len(ref.Cases))
	}
	for _, c := range ref.Cases {
		t.Run(c.Field+"/"+string(mustJSON(c.Source)), func(t *testing.T) {
			var n float64
			switch v := c.Source.(type) {
			case float64:
				n = v
			case string:
				switch v {
				case "Infinity":
					n = math.Inf(1)
				case "-Infinity":
					n = math.Inf(-1)
				case "NaN":
					n = math.NaN()
				default:
					t.Fatal(v)
				}
			}
			actions := []SyncAction{{At: 0, Pos: 0}, {At: 1000, Pos: 0}}
			if c.Field == "time" {
				actions[0].At = n
			} else {
				actions[0].Pos = n
			}
			_, err := EncodeSyncScriptCandidate(actions)
			if c.Accepted {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var e *ConversionError
			if !errors.As(err, &e) || e.Code != c.Code || e.Message != c.Message {
				t.Fatalf("got %v, want %s", err, c.Message)
			}
		})
	}
}
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
