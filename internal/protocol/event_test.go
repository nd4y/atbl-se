package protocol

import "testing"

func TestFirmwareButtonFrames(t *testing.T) {
	for _, name := range []string{"mode-button-pressed", "speed-plus-button-pressed", "speed-minus-button-pressed"} {
		if got, ok := ParseEvent("/event/" + name); !ok || got != name {
			t.Fatal(got, ok)
		}
		for _, wire := range []string{"event/" + name, "/event/" + name + "/", "/event/" + name + "/" + testUUID, "/event/" + name + "\x00", "/event/" + name + "\n"} {
			if _, ok := ParseEvent(wire); ok {
				t.Fatalf("accepted extended event %q", wire)
			}
		}
	}
	for _, wire := range []string{"/event/unknown", "/state/mode-button-pressed", "/event", ""} {
		if _, ok := ParseEvent(wire); ok {
			t.Fatalf("accepted %q", wire)
		}
	}
}
