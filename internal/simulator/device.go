// Package simulator models protocol replies, not physical firmware behavior.
package simulator

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

	"github.com/nd4y/atblse/internal/protocol"
)

const MAC = "020000000001"
const DeviceToken = "local-simulator-no-cloud-identity"

type Device struct {
	Mode, Slot, Speed, Plus, Minus, Current, Offset, Loop int
	Token                                                 string
	Slots                                                 [30]uint32
}

func New() *Device {
	d := &Device{Mode: 50}
	for i := range d.Slots {
		d.Slots[i] = uint32(i + 1)
	}
	return d
}

// Handle has a single caller. Downloads are bounded and verified by the transport.
func (d *Device) Handle(wire string, fetch func(string, string) ([]byte, error)) (string, error) {
	p := strings.Split(wire, "/")
	if len(p) < 3 || p[0] != "" || !protocol.ValidScriptToken(p[len(p)-1]) {
		return "", fmt.Errorf("invalid command frame")
	}
	name, id := p[1], p[len(p)-1]
	args := p[2 : len(p)-1]
	count := func(n int) bool { return len(args) == n }
	integer := func(i, min, max int) (int, bool) {
		if i >= len(args) {
			return 0, false
		}
		v, e := strconv.Atoi(args[i])
		return v, e == nil && v >= min && v <= max
	}
	failure := func(code string) (string, error) { return "/error/" + code + "/" + id, nil }
	list := func() string {
		v := make([]string, len(d.Slots))
		for i, x := range d.Slots {
			v[i] = strconv.FormatUint(uint64(x), 10)
		}
		return "/ok/" + strings.Join(v, ",") + "/" + id
	}
	switch name {
	case "connected-ok":
		if !count(0) {
			return failure("INVALID_ARGUMENT")
		}
		return "", nil
	case "state":
		if !count(0) {
			return failure("INVALID_ARGUMENT")
		}
	case "target-speed-set":
		v, ok := integer(0, 0, 100)
		if !count(1) || !ok {
			return failure("INVALID_ARGUMENT")
		}
		d.Speed = v
		d.Mode = 43
	case "target-speed-stop":
		if !count(0) {
			return failure("INVALID_ARGUMENT")
		}
		d.Mode = 44
	case "valve-stroke-plus-set", "valve-stroke-minus-set", "sync-script-loop":
		v, ok := integer(0, 0, 1)
		if !count(1) || !ok {
			return failure("INVALID_ARGUMENT")
		}
		switch name {
		case "valve-stroke-plus-set":
			d.Plus = v
		case "valve-stroke-minus-set":
			d.Minus = v
		default:
			d.Loop = v
		}
	case "local-script-loaded":
		if !count(0) {
			return failure("INVALID_ARGUMENT")
		}
		d.Mode = 42
		return list(), nil
	case "local-script-set":
		v, ok := integer(0, 0, 29)
		if !count(1) || !ok {
			return failure("INVALID_ARGUMENT")
		}
		d.Slot = v
		d.Mode = 41
		d.Plus = 0
		d.Minus = 0
	case "local-script-start":
		if !count(0) {
			return failure("INVALID_ARGUMENT")
		}
		if d.Mode == 42 {
			d.Mode = 41
		}
	case "local-script-stop":
		if !count(0) {
			return failure("INVALID_ARGUMENT")
		}
		if d.Mode == 41 {
			d.Mode = 42
		}
	case "sync-script-load-token", "local-script-replace":
		kind, token := "sync", ""
		slot := 0
		if name == "sync-script-load-token" {
			if !count(1) {
				return failure("INVALID_ARGUMENT")
			}
			token = args[0]
		} else {
			var ok bool
			slot, ok = integer(0, 0, 29)
			if !count(2) || !ok {
				return failure("INVALID_ARGUMENT")
			}
			kind = "local"
			token = args[1]
		}
		if !protocol.ValidScriptToken(token) || fetch == nil {
			return failure("SCRIPT_DOWNLOAD_ERROR")
		}
		b, e := fetch(kind, token)
		if e != nil || len(b) < 8 || len(b) > 600008 || (len(b)-8)%6 != 0 || binary.LittleEndian.Uint32(b[4:8]) != 1 {
			return failure("SCRIPT_DOWNLOAD_ERROR")
		}
		if kind == "local" {
			d.Slots[slot] = binary.LittleEndian.Uint32(b[:4])
			d.Mode = 42
			return list(), nil
		}
		d.Token = token
		d.Current = 0
		d.Mode = 70
	case "sync-script-start":
		v, ok := integer(0, 0, protocol.MaxScriptTime)
		if !count(1) || !ok {
			return failure("INVALID_ARGUMENT")
		}
		if d.Token == "" {
			return failure("NO_SCRIPT")
		}
		d.Current = v
		d.Mode = 60
		d.Plus = 0
		d.Minus = 0
	case "sync-script-stop":
		if !count(0) {
			return failure("INVALID_ARGUMENT")
		}
		d.Mode = 70
	case "sync-script-offset":
		v, ok := integer(0, -protocol.MaxScriptTime, protocol.MaxScriptTime)
		if !count(1) || !ok {
			return failure("INVALID_ARGUMENT")
		}
		d.Offset = v
	case "local-script-swap", "local-script-move":
		a, okA := integer(0, 0, 29)
		b, okB := integer(1, 0, 29)
		if !count(2) || !okA || !okB {
			return failure("INVALID_ARGUMENT")
		}
		if name == "local-script-swap" {
			if a == b {
				return failure("ERROR_SWAP")
			}
			d.Slots[a], d.Slots[b] = d.Slots[b], d.Slots[a]
		} else {
			v := d.Slots[a]
			if a < b {
				copy(d.Slots[a:b], d.Slots[a+1:b+1])
			} else {
				copy(d.Slots[b+1:a+1], d.Slots[b:a])
			}
			d.Slots[b] = v
		}
		d.Mode = 42
		return list(), nil
	default:
		return failure("UNSUPPORTED_COMMAND")
	}
	return fmt.Sprintf("/state/%d/%d/%d/%d/%d/%d/%d/%d/%s/%s", d.Mode, d.Slot, d.Speed, d.Plus, d.Minus, d.Current, d.Offset, d.Loop, d.Token, id), nil
}
