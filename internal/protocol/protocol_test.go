package protocol

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const (
	testUUID = "01234567-89ab-4cde-8f01-23456789abcd"
	// stateLimit is the documented maximum in the task, kept independent of
	// the implementation constant so a regression in either is visible.
	stateLimit = 4096
)

var uuidV4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func validStateParts() []string {
	// Empty leading part represents the leading slash. The remaining fields
	// are: message, mode, script, speed, plus, minus, current, offset, loop,
	// token, correlation.
	return []string{
		"",
		"state",
		"43",
		"0",
		"55",
		"1",
		"0",
		"1234",
		"5678",
		"1",
		"script-token",
		testUUID,
	}
}

func joinedState(parts []string) string {
	return strings.Join(parts, "/")
}

func stateWith(mutate func([]string)) string {
	parts := validStateParts()
	mutate(parts)
	return joinedState(parts)
}

func TestCommandFrames(t *testing.T) {
	tests := []struct {
		name     string
		args     []int
		wantHead string
	}{
		{name: "connected-ok", wantHead: "/connected-ok/"},
		{name: "state", wantHead: "/state/"},
		{name: "target-speed-stop", wantHead: "/target-speed-stop/"},
		{name: "target-speed-set", args: []int{0}, wantHead: "/target-speed-set/0/"},
		{name: "target-speed-set", args: []int{100}, wantHead: "/target-speed-set/100/"},
		{name: "valve-stroke-plus-set", args: []int{0}, wantHead: "/valve-stroke-plus-set/0/"},
		{name: "valve-stroke-minus-set", args: []int{1}, wantHead: "/valve-stroke-minus-set/1/"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name+tc.wantHead, func(t *testing.T) {
			id, wire, err := Command(tc.name, tc.args...)
			if err != nil {
				t.Fatalf("Command(%q, %v) returned error: %v", tc.name, tc.args, err)
			}
			if !uuidV4Pattern.MatchString(id) {
				t.Fatalf("correlation %q is not a canonical UUID v4", id)
			}
			if want := tc.wantHead + id; wire != want {
				t.Fatalf("wire = %q, want %q", wire, want)
			}

			parts := strings.Split(wire, "/")
			if parts[0] != "" {
				t.Fatalf("wire %q does not start with a slash", wire)
			}
			if got := parts[len(parts)-1]; got != id {
				t.Fatalf("wire correlation = %q, want %q", got, id)
			}
		})
	}
}

func TestCommandCorrelationsAreUnique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 32; i++ {
		id, _, err := Command("state")
		if err != nil {
			t.Fatalf("Command returned error: %v", err)
		}
		if seen[id] {
			t.Fatalf("duplicate correlation %q", id)
		}
		seen[id] = true
	}
}

func TestCommandRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		args []int
	}{
		{name: ""},
		{name: "unknown"},
		{name: "State"},
		{name: "connected-ok", args: []int{0}},
		{name: "state", args: []int{1}},
		{name: "target-speed-stop", args: []int{0}},
		{name: "target-speed-set"},
		{name: "target-speed-set", args: []int{0, 1}},
		{name: "target-speed-set", args: []int{-1}},
		{name: "target-speed-set", args: []int{101}},
		{name: "valve-stroke-plus-set"},
		{name: "valve-stroke-plus-set", args: []int{-1}},
		{name: "valve-stroke-plus-set", args: []int{2}},
		{name: "valve-stroke-minus-set", args: []int{0, 1}},
	}

	for i, tc := range tests {
		tc := tc
		t.Run(tc.name+strconv.Itoa(len(tc.args))+"-"+strconv.Itoa(i), func(t *testing.T) {
			id, wire, err := Command(tc.name, tc.args...)
			if err == nil {
				t.Fatalf("Command(%q, %v) unexpectedly succeeded: id=%q wire=%q", tc.name, tc.args, id, wire)
			}
			if id != "" || wire != "" {
				t.Fatalf("invalid command returned id=%q wire=%q, want empty values", id, wire)
			}
		})
	}
}

func TestParseStateValid(t *testing.T) {
	state, err := ParseState(joinedState(validStateParts()))
	if err != nil {
		t.Fatalf("ParseState returned error: %v", err)
	}

	want := State{
		Correlation:           testUUID,
		OperationalMode:       "TARGET_SPEED_PLAYING",
		RawOperationalMode:    ModePlaying,
		LocalScript:           0,
		TargetSpeed:           55,
		StrokePlusValve:       true,
		StrokeMinusValve:      false,
		SyncScriptCurrentTime: 1234,
		SyncScriptOffsetTime:  5678,
		SyncScriptLoop:        true,
		ScriptToken:           "script-token",
	}
	if state != want {
		t.Fatalf("state = %#v, want %#v", state, want)
	}
}

func TestParseStateOperationalModes(t *testing.T) {
	tests := []struct {
		raw  int
		name string
	}{
		{raw: ModePlaying, name: "TARGET_SPEED_PLAYING"},
		{raw: ModePaused, name: "TARGET_SPEED_PAUSED"},
		{raw: ModeConnected, name: "ONLINE_CONNECTED"},
		{raw: 0, name: "UNKNOWN_0"},
		{raw: 99, name: "UNKNOWN_99"},
		{raw: -7, name: "UNKNOWN_-7"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			text := stateWith(func(parts []string) {
				parts[2] = strconv.Itoa(tc.raw)
			})
			state, err := ParseState(text)
			if err != nil {
				t.Fatalf("ParseState returned error: %v", err)
			}
			if state.RawOperationalMode != tc.raw {
				t.Fatalf("RawOperationalMode = %d, want %d", state.RawOperationalMode, tc.raw)
			}
			if state.OperationalMode != tc.name {
				t.Fatalf("OperationalMode = %q, want %q", state.OperationalMode, tc.name)
			}
		})
	}
}

func TestParseStateRetainsPausedSetpoint(t *testing.T) {
	// A STOP leaves the firmware reporting TARGET_SPEED_PAUSED with the last
	// target-speed setpoint still present. Parsing must not zero or otherwise
	// reinterpret that retained value as physical motion.
	text := stateWith(func(parts []string) {
		parts[2] = strconv.Itoa(ModePaused)
		parts[4] = "37"
	})

	state, err := ParseState(text)
	if err != nil {
		t.Fatalf("ParseState returned error: %v", err)
	}
	if state.RawOperationalMode != ModePaused {
		t.Fatalf("RawOperationalMode = %d, want %d", state.RawOperationalMode, ModePaused)
	}
	if state.OperationalMode != "TARGET_SPEED_PAUSED" {
		t.Fatalf("OperationalMode = %q, want TARGET_SPEED_PAUSED", state.OperationalMode)
	}
	if state.TargetSpeed != 37 {
		t.Fatalf("TargetSpeed = %d, want retained setpoint 37", state.TargetSpeed)
	}
}

func TestParseStateMalformed(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{name: "empty", text: ""},
		{name: "too long", text: strings.Repeat("x", stateLimit+1)},
		{name: "eleven parts", text: joinedState(validStateParts()[:11])},
		{name: "thirteen parts", text: joinedState(append(validStateParts(), "extra"))},
		{
			name: "nonempty first part",
			text: stateWith(func(parts []string) { parts[0] = "not-empty" }),
		},
		{
			name: "wrong message name",
			text: stateWith(func(parts []string) { parts[1] = "status" }),
		},
		{
			name: "slash in script token",
			text: stateWith(func(parts []string) { parts[10] = "script/token" }),
		},
		{
			name: "invalid mode",
			text: stateWith(func(parts []string) { parts[2] = "abc" }),
		},
		{
			name: "invalid local script",
			text: stateWith(func(parts []string) { parts[3] = "1.5" }),
		},
		{
			name: "empty speed",
			text: stateWith(func(parts []string) { parts[4] = "" }),
		},
		{
			name: "speed below range",
			text: stateWith(func(parts []string) { parts[4] = "-1" }),
		},
		{
			name: "speed above range",
			text: stateWith(func(parts []string) { parts[4] = "101" }),
		},
		{
			name: "invalid current time",
			text: stateWith(func(parts []string) { parts[7] = "now" }),
		},
		{
			name: "invalid offset time",
			text: stateWith(func(parts []string) { parts[8] = "2x" }),
		},
		{
			name: "plus valve two",
			text: stateWith(func(parts []string) { parts[5] = "2" }),
		},
		{
			name: "minus valve negative",
			text: stateWith(func(parts []string) { parts[6] = "-1" }),
		},
		{
			name: "plus valve leading zero",
			text: stateWith(func(parts []string) { parts[5] = "01" }),
		},
		{
			name: "loop word",
			text: stateWith(func(parts []string) { parts[9] = "true" }),
		},
		{
			name: "loop leading zero",
			text: stateWith(func(parts []string) { parts[9] = "01" }),
		},
		{
			name: "empty correlation",
			text: stateWith(func(parts []string) { parts[11] = "" }),
		},
		{
			name: "uppercase correlation",
			text: stateWith(func(parts []string) { parts[11] = strings.ToUpper(testUUID) }),
		},
		{
			name: "braced correlation",
			text: stateWith(func(parts []string) { parts[11] = "{" + testUUID + "}" }),
		},
		{
			name: "correlation without hyphens",
			text: stateWith(func(parts []string) { parts[11] = strings.ReplaceAll(testUUID, "-", "") }),
		},
		{
			name: "non-hex correlation",
			text: stateWith(func(parts []string) { parts[11] = "g1234567-89ab-4cde-8f01-23456789abcd" }),
		},
		{
			name: "misplaced correlation hyphen",
			text: stateWith(func(parts []string) { parts[11] = "01234567-89ab-4cde-8f01-2345678-9abcd" }),
		},
		{
			name: "short correlation",
			text: stateWith(func(parts []string) { parts[11] = testUUID[:35] }),
		},
		{
			name: "leading space",
			text: " " + joinedState(validStateParts()),
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseState(tc.text); err == nil {
				t.Fatalf("ParseState accepted malformed message %q", tc.text)
			}
		})
	}
}

func TestParseStateLengthBoundary(t *testing.T) {
	if maxStateTextLength != stateLimit {
		t.Fatalf("maxStateTextLength = %d, want %d", maxStateTextLength, stateLimit)
	}

	parts := validStateParts()
	parts[10] = ""
	padding := stateLimit - len(joinedState(parts))
	if padding <= 0 {
		t.Fatalf("base state is already too long: padding=%d", padding)
	}
	parts[10] = strings.Repeat("x", padding)
	text := joinedState(parts)
	if len(text) != stateLimit {
		t.Fatalf("test frame length = %d, want %d", len(text), stateLimit)
	}

	state, err := ParseState(text)
	if err != nil {
		t.Fatalf("ParseState rejected frame at the length limit: %v", err)
	}
	if len(state.ScriptToken) != padding {
		t.Fatalf("ScriptToken length = %d, want %d", len(state.ScriptToken), padding)
	}

	if _, err := ParseState(text + "x"); err == nil {
		t.Fatal("ParseState accepted a frame one byte over the length limit")
	}
}

func TestStateJSONExcludesPrivateFields(t *testing.T) {
	state, err := ParseState(joinedState(validStateParts()))
	if err != nil {
		t.Fatalf("ParseState returned error: %v", err)
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}

	wantKeys := map[string]bool{
		"operationalMode":       true,
		"rawOperationalMode":    true,
		"localScript":           true,
		"targetSpeed":           true,
		"strokePlusValve":       true,
		"strokeMinusValve":      true,
		"syncScriptCurrentTime": true,
		"syncScriptOffsetTime":  true,
		"syncScriptLoop":        true,
	}
	if len(got) != len(wantKeys) {
		t.Fatalf("JSON keys = %v, want exactly %d public fields", got, len(wantKeys))
	}
	for key := range wantKeys {
		if _, ok := got[key]; !ok {
			t.Fatalf("JSON missing key %q: %s", key, data)
		}
	}

	for _, forbidden := range []string{
		"syncScriptToken", "ScriptToken", "scriptToken",
		"correlation", "Correlation",
	} {
		if _, ok := got[forbidden]; ok {
			t.Fatalf("JSON exposed private field %q: %s", forbidden, data)
		}
	}
	if strings.Contains(string(data), "script-token") {
		t.Fatalf("JSON exposed script token value: %s", data)
	}
	if strings.Contains(string(data), testUUID) {
		t.Fatalf("JSON exposed correlation value: %s", data)
	}

	if got["operationalMode"] != "TARGET_SPEED_PLAYING" {
		t.Fatalf("operationalMode = %v, want TARGET_SPEED_PLAYING", got["operationalMode"])
	}
	if got["rawOperationalMode"] != float64(ModePlaying) {
		t.Fatalf("rawOperationalMode = %v, want %d", got["rawOperationalMode"], ModePlaying)
	}
	if got["targetSpeed"] != float64(55) {
		t.Fatalf("targetSpeed = %v, want 55", got["targetSpeed"])
	}
	if got["strokePlusValve"] != true {
		t.Fatalf("strokePlusValve = %v, want true", got["strokePlusValve"])
	}
	if got["strokeMinusValve"] != false {
		t.Fatalf("strokeMinusValve = %v, want false", got["strokeMinusValve"])
	}
}
