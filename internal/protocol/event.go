package protocol

// ParseEvent accepts only the three complete text frames emitted by the
// VacuGlide 1.03 button handlers (firmware SHA256 in the static audit).
// Events have no arguments or correlation UUID and cannot satisfy an RPC.
func ParseEvent(text string) (string, bool) {
	switch text {
	case "/event/mode-button-pressed":
		return "mode-button-pressed", true
	case "/event/speed-plus-button-pressed":
		return "speed-plus-button-pressed", true
	case "/event/speed-minus-button-pressed":
		return "speed-minus-button-pressed", true
	default:
		return "", false
	}
}
