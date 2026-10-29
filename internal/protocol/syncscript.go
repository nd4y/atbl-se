package protocol

import (
	"encoding/binary"
	"fmt"
	"math"
)

// SyncAction retains the coerced source numbers. The official converter packs
// them before calculating speeds; premature integer/rational arithmetic differs.
type SyncAction struct {
	At  float64 `json:"at"`
	Pos float64 `json:"pos"`
}

type ConversionError struct{ Code, Message string }

func (e *ConversionError) Error() string { return e.Message }
func ScriptError(message string) error   { return &ConversionError{"Error", message} }

const MaxScriptTime = 16777214

// EncodeSyncScriptCandidate reproduces the controlled official cloud corpus
// including packing, signed source positions and floating-point rounding. The server uses it only behind the
// experimentalSyncScripts switch until physical playback is accepted.
func EncodeSyncScriptCandidate(actions []SyncAction) ([]byte, error) {
	if len(actions) == 0 {
		return nil, ScriptError("Funscript actions is not an array")
	}
	if len(actions) > 100000 {
		return nil, ScriptError("Funscript actions is too long, autoblow accepts max 100000 actions / funscript")
	}
	if actions[len(actions)-1].At > MaxScriptTime {
		return nil, ScriptError("Funscript actions are too long, autoblow accepts max 16777214ms / funscript")
	}
	type point struct {
		at  uint32
		pos int8
	}
	points := make([]point, 0, len(actions))
	for _, action := range actions {
		at, pos := action.At, action.Pos
		if math.IsNaN(at) {
			at = 0
		}
		if math.IsNaN(pos) {
			pos = 0
		}
		if at < 0 || at > math.MaxUint32 {
			return nil, &ConversionError{"ERR_OUT_OF_RANGE", fmt.Sprintf("The value of \"value\" is out of range. It must be >= 0 and <= 4294967295. Received %s", bufferNumberString(at))}
		}
		if pos < 0 || pos > 255 {
			return nil, &ConversionError{"ERR_OUT_OF_RANGE", fmt.Sprintf("The value of \"value\" is out of range. It must be >= 0 and <= 255. Received %s", bufferNumberString(pos))}
		}
		// Source points occupy a packed24-bit timestamp and signed8-bit position.
		p := point{uint32(at) & 0xffffff, int8(uint8(pos))}
		if len(points) > 0 && points[len(points)-1].at == p.at {
			points[len(points)-1] = p
		} else {
			points = append(points, p)
		}
	}
	// Observed header: little-endian zero followed by little-endian one.
	out := make([]byte, 8+6*(len(points)-1))
	binary.LittleEndian.PutUint32(out[4:8], 1)
	for i := 0; i < len(points)-1; i++ {
		a, b := points[i], points[i+1]
		distance := math.Abs(float64(int(b.pos) - int(a.pos)))
		duration := math.Abs(float64(int64(b.at) - int64(a.at)))
		// Keep the observed operation order; division by600 then multiplication
		// by100 is not interchangeable with division by6 at half boundaries.
		seconds := duration / 1000
		velocity := distance / seconds
		speed := math.Min(100, math.Round((velocity/600)*100))
		start := 8 + i*6
		binary.LittleEndian.PutUint32(out[start:start+4], a.at)
		out[start+5] = byte(speed)
	}
	return out, nil
}
