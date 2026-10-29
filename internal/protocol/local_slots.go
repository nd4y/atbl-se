package protocol

import "fmt"

// LocalSlotCommand follows the registered VacuGlide 1.03 handlers: the two
// decimal indices precede the correlation; replies are the full slot list.
func LocalSlotCommand(name string, first, second int) (id, wire string, err error) {
	if name != "local-script-swap" && name != "local-script-move" {
		return "", "", fmt.Errorf("unsupported slot operation")
	}
	if first < 0 || first >= LocalScriptSlots || second < 0 || second >= LocalScriptSlots {
		return "", "", fmt.Errorf("slot indices must be 0..29")
	}
	id, err = newUUIDv4()
	if err != nil {
		return "", "", err
	}
	return id, fmt.Sprintf("/%s/%d/%d/%s", name, first, second, id), nil
}

// LocalReplaceCommand selects the slot then the download UUID. The firmware
// fetches /device-backend/local-script-bin/<token>, without starting playback.
func LocalReplaceCommand(index int, token string) (id, wire string, err error) {
	if index < 0 || index >= LocalScriptSlots || !ValidScriptToken(token) {
		return "", "", fmt.Errorf("invalid slot or local script token")
	}
	id, err = newUUIDv4()
	if err != nil {
		return "", "", err
	}
	return id, fmt.Sprintf("/local-script-replace/%d/%s/%s", index, token, id), nil
}
