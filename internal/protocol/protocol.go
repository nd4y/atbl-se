// Package protocol implements the stock VacuGlide 1.03 wire codec.
//
// The wire format is deliberately kept as a pure codec: parsing a state
// message does not imply that the device is physically moving, and a STOP
// command does not erase the last target-speed setpoint reported by the
// device.
package protocol

import (
	"crypto/rand"
	"fmt"
	"strconv"
	"strings"
)

// Operational modes observed on stock firmware 1.03.
const (
	ModePlaying   = 43
	ModePaused    = 44
	ModeConnected = 50
)

const (
	maxStateTextLength = 4096
	statePartCount     = 12
)

// Commands confirmed by the reference implementation.
const (
	commandConnectedOK      = "connected-ok"
	commandState            = "state"
	commandTargetSpeedStop  = "target-speed-stop"
	commandTargetSpeedSet   = "target-speed-set"
	commandValveStrokePlus  = "valve-stroke-plus-set"
	commandValveStrokeMinus = "valve-stroke-minus-set"
)

// State is the public device state reported by a "state" message.
//
// Correlation and ScriptToken are transport/host-local details and are not
// part of the public JSON state.
type State struct {
	Correlation string `json:"-"`

	OperationalMode    string `json:"operationalMode"`
	RawOperationalMode int    `json:"rawOperationalMode"`
	LocalScript        int    `json:"localScript"`
	TargetSpeed        int    `json:"targetSpeed"`
	StrokePlusValve    bool   `json:"strokePlusValve"`
	StrokeMinusValve   bool   `json:"strokeMinusValve"`

	SyncScriptCurrentTime int  `json:"syncScriptCurrentTime"`
	SyncScriptOffsetTime  int  `json:"syncScriptOffsetTime"`
	SyncScriptLoop        bool `json:"syncScriptLoop"`

	ScriptToken string `json:"-"`
}

// ParseState decodes a complete state frame:
//
//	/state/<mode>/<script>/<speed>/<plus>/<minus>/<current>/<offset>/<loop>/<token>/<correlation>
//
// Replies are validated strictly. Messages longer than 4096 bytes, messages
// with anything other than exactly twelve slash-separated parts, non-integer
// numeric fields, non-0/1 boolean fields, out-of-range speeds, and
// non-canonical correlation UUIDs are rejected.
func ParseState(text string) (State, error) {
	if len(text) > maxStateTextLength {
		return State{}, fmt.Errorf("state message exceeds %d bytes", maxStateTextLength)
	}

	parts := strings.Split(text, "/")
	if len(parts) != statePartCount {
		return State{}, fmt.Errorf("state message must have %d slash-separated parts, got %d", statePartCount, len(parts))
	}
	if parts[0] != "" || parts[1] != commandState {
		return State{}, fmt.Errorf("unsupported device message")
	}

	correlation := parts[11]
	if !isCanonicalUUID(correlation) {
		return State{}, fmt.Errorf("state correlation is not a canonical UUID")
	}

	mode, err := parseInteger("operational mode", parts[2])
	if err != nil {
		return State{}, err
	}
	localScript, err := parseInteger("local script", parts[3])
	if err != nil {
		return State{}, err
	}
	targetSpeed, err := parseInteger("target speed", parts[4])
	if err != nil {
		return State{}, err
	}
	if targetSpeed < 0 || targetSpeed > 100 {
		return State{}, fmt.Errorf("target speed must be an integer from 0 to 100, got %d", targetSpeed)
	}

	strokePlus, err := parseBoolean("stroke plus valve", parts[5])
	if err != nil {
		return State{}, err
	}
	strokeMinus, err := parseBoolean("stroke minus valve", parts[6])
	if err != nil {
		return State{}, err
	}
	currentTime, err := parseInteger("sync script current time", parts[7])
	if err != nil {
		return State{}, err
	}
	offsetTime, err := parseInteger("sync script offset time", parts[8])
	if err != nil {
		return State{}, err
	}
	loop, err := parseBoolean("sync script loop", parts[9])
	if err != nil {
		return State{}, err
	}

	return State{
		Correlation:           correlation,
		OperationalMode:       operationalModeName(mode),
		RawOperationalMode:    mode,
		LocalScript:           localScript,
		TargetSpeed:           targetSpeed,
		StrokePlusValve:       strokePlus,
		StrokeMinusValve:      strokeMinus,
		SyncScriptCurrentTime: currentTime,
		SyncScriptOffsetTime:  offsetTime,
		SyncScriptLoop:        loop,
		ScriptToken:           parts[10],
	}, nil
}

// Command builds a request frame and returns its fresh UUID v4 correlation.
//
// Only commands present in the reference implementation are accepted.
// Target speeds are limited to integers from 0 to 100 and valve arguments to
// 0 or 1.
func Command(name string, args ...int) (id string, wire string, err error) {
	switch name {
	case commandConnectedOK, commandState, commandTargetSpeedStop, "sync-script-stop", "local-script-loaded", "local-script-start", "local-script-stop":
		if len(args) != 0 {
			return "", "", fmt.Errorf("command %q takes no arguments", name)
		}
	case commandTargetSpeedSet:
		if len(args) != 1 {
			return "", "", fmt.Errorf("command %q takes exactly one argument", name)
		}
		if args[0] < 0 || args[0] > 100 {
			return "", "", fmt.Errorf("speed must be an integer from 0 to 100, got %d", args[0])
		}
	case commandValveStrokePlus, commandValveStrokeMinus, "sync-script-loop":
		if len(args) != 1 {
			return "", "", fmt.Errorf("command %q takes exactly one argument", name)
		}
		if args[0] != 0 && args[0] != 1 {
			return "", "", fmt.Errorf("valve must be 0 or 1, got %d", args[0])
		}
	case "sync-script-start", "sync-script-offset":
		if len(args) != 1 || args[0] > 16777214 || args[0] < -16777214 || name == "sync-script-start" && args[0] < 0 {
			return "", "", fmt.Errorf("invalid sync script time")
		}
	case "local-script-set":
		if len(args) != 1 || args[0] < 0 || args[0] >= LocalScriptSlots {
			return "", "", fmt.Errorf("invalid local script index")
		}
	default:
		return "", "", fmt.Errorf("unsupported command %q", name)
	}

	correlation, err := newUUIDv4()
	if err != nil {
		return "", "", err
	}

	if len(args) == 0 {
		return correlation, "/" + name + "/" + correlation, nil
	}
	return correlation, "/" + name + "/" + strconv.Itoa(args[0]) + "/" + correlation, nil
}

// SyncLoadCommand is the observed token-before-correlation download command.
// Only canonical tokens can form a wire/path segment.
func SyncLoadCommand(token string) (id, wire string, err error) {
	if !ValidScriptToken(token) {
		return "", "", fmt.Errorf("invalid script token")
	}
	id, err = newUUIDv4()
	if err != nil {
		return "", "", err
	}
	return id, "/sync-script-load-token/" + token + "/" + id, nil
}

func ValidScriptToken(token string) bool { return isCanonicalUUID(token) }

func operationalModeName(mode int) string {
	switch mode {
	case 41:
		return "LOCAL_SCRIPT_PLAYING"
	case 42:
		return "LOCAL_SCRIPT_PAUSED"
	case ModePlaying:
		return "TARGET_SPEED_PLAYING"
	case ModePaused:
		return "TARGET_SPEED_PAUSED"
	case ModeConnected:
		return "ONLINE_CONNECTED"
	case 60:
		return "SYNC_SCRIPT_PLAYING"
	case 70:
		return "SYNC_SCRIPT_PAUSED"
	case 130:
		return "SETUP"
	case 140:
		return "LOADING_SETUP"
	case 141:
		return "LOADING_INTERACTIVE"
	case 150:
		return "FIRMWARE_UPDATING"
	case 500:
		return "ERROR"
	case 510:
		return "ERROR_MOTOR_STUCK"
	case 511:
		return "ERROR_MOTOR_OVERRUN"
	default:
		return "UNKNOWN_" + strconv.Itoa(mode)
	}
}

func parseInteger(field, value string) (int, error) {
	if value == "" {
		return 0, fmt.Errorf("%s is not an integer", field)
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s is not an integer: %q", field, value)
	}
	return n, nil
}

func parseBoolean(field, value string) (bool, error) {
	switch value {
	case "0":
		return false, nil
	case "1":
		return true, nil
	default:
		return false, fmt.Errorf("%s must be 0 or 1", field)
	}
}

// isCanonicalUUID accepts only the lowercase, hyphenated 8-4-4-4-12 form.
// UUIDs with braces, URN prefixes, uppercase digits, missing hyphens, or
// misplaced hyphens are rejected.
func isCanonicalUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i := 0; i < len(value); i++ {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if value[i] != '-' {
				return false
			}
			continue
		}
		if !isLowerHex(value[i]) {
			return false
		}
	}
	return true
}

func isLowerHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f'
}

// newUUIDv4 generates an RFC 4122 version 4 UUID using crypto/rand.
func newUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate correlation UUID: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
