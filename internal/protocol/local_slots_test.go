package protocol

import (
	"strings"
	"testing"
)

func TestNativeSlotCommandArgumentOrder(t *testing.T) {
	// Independently reconstructed from registration at400d9244, handlers
	// 400d90a4/400d9024/400d90f8 and the slash dispatcher, not a cloud capture.
	for _, tc := range []struct{ name, head string }{
		{"local-script-swap", "/local-script-swap/0/29/"},
		{"local-script-move", "/local-script-move/0/29/"},
	} {
		id, wire, err := LocalSlotCommand(tc.name, 0, 29)
		if err != nil || wire != tc.head+id || !uuidV4Pattern.MatchString(id) {
			t.Fatal(wire, err)
		}
	}
	id, wire, err := LocalReplaceCommand(29, testUUID)
	if err != nil || wire != "/local-script-replace/29/"+testUUID+"/"+id {
		t.Fatal(wire, err)
	}
	for _, pair := range [][2]int{{-1, 0}, {0, -1}, {30, 0}, {0, 30}} {
		if _, _, err := LocalSlotCommand("local-script-move", pair[0], pair[1]); err == nil {
			t.Fatal(pair)
		}
	}
	for _, token := range []string{"", strings.ToUpper(testUUID), testUUID + "/extra"} {
		if _, _, err := LocalReplaceCommand(0, token); err == nil {
			t.Fatal(token)
		}
	}
	if _, _, err := LocalSlotCommand("local-script-set", 0, 1); err == nil {
		t.Fatal("accepted wrong operation")
	}
	if _, _, err := LocalReplaceCommand(30, testUUID); err == nil {
		t.Fatal("accepted invalid slot")
	}
}
