package protocol

import (
	"fmt"
	"strconv"
	"strings"
)

const LocalScriptSlots = 30

// Response distinguishes state, slot-list and error frames. /ok must never
// satisfy an unrelated state command merely because its UUID matches.
type Response struct {
	Kind        string
	Correlation string
	State       State
	Scripts     []uint32
	Err         error
}

type FirmwareError struct{ Code string }

func (e *FirmwareError) Error() string { return "firmware rejected command: " + e.Code }

func ParseResponse(text string) (Response, error) {
	if strings.HasPrefix(text, "/state/") {
		state, err := ParseState(text)
		return Response{Kind: "state", Correlation: state.Correlation, State: state}, err
	}
	if len(text) > maxStateTextLength {
		return Response{}, fmt.Errorf("reply too long")
	}
	parts := strings.Split(text, "/")
	if len(parts) != 4 || parts[0] != "" || !isCanonicalUUID(parts[3]) {
		return Response{}, fmt.Errorf("invalid reply framing")
	}
	switch parts[1] {
	case "ok":
		values := strings.Split(parts[2], ",")
		if len(values) != LocalScriptSlots {
			return Response{}, fmt.Errorf("expected 30 slot identifiers")
		}
		scripts := make([]uint32, len(values))
		for i, v := range values {
			n, err := strconv.ParseUint(v, 10, 32)
			if err != nil || strconv.FormatUint(n, 10) != v {
				return Response{}, fmt.Errorf("invalid slot identifier")
			}
			scripts[i] = uint32(n)
		}
		return Response{Kind: "slots", Correlation: parts[3], Scripts: scripts}, nil
	case "error":
		code := parts[2]
		if len(code) == 0 || len(code) > 64 {
			return Response{}, fmt.Errorf("invalid error code")
		}
		for _, c := range code {
			if c != '_' && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
				return Response{}, fmt.Errorf("invalid error code")
			}
		}
		return Response{Kind: "error", Correlation: parts[3], Err: &FirmwareError{Code: code}}, nil
	default:
		return Response{}, fmt.Errorf("unsupported reply")
	}
}
