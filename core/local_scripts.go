package core

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/nd4y/atblse/internal/protocol"
)

func isImplementedLocalRoute(key string) bool {
	switch key {
	case "PUT /vacuglide/local-script", "PUT /vacuglide/local-script/start", "PUT /vacuglide/local-script/stop", "GET /vacuglide/local-script/loaded", "PUT /vacuglide/local-script/replace", "PUT /vacuglide/local-script/swap", "PUT /vacuglide/local-script/move":
		return true
	}
	return false
}

var errNoLocalSyncScript = errors.New("load a local script before starting it")
var errSyncStartRange = errors.New("start time is outside the loaded script")

func commandErrorStatus(err error) int {
	var firmware *protocol.FirmwareError
	switch {
	case errors.As(err, &firmware), errors.Is(err, errNoLocalSyncScript), errors.Is(err, errSyncStartRange):
		return 400
	case errors.Is(err, ErrUnavailable):
		return 502
	case errors.Is(err, ErrResponseTimeout), errors.Is(err, context.DeadlineExceeded):
		return 504
	default:
		return 409
	}
}

func (d *Device) localScriptList(ctx context.Context) ([]uint32, error) {
	id, wire, err := protocol.Command("local-script-loaded")
	if err != nil {
		return nil, err
	}
	// Despite its GET API, the native loaded handler unconditionally sets
	// mode42. It must obey the same admission and STOP fencing as mutations.
	return d.mutateLocalSlots(ctx, id, wire, nil, 0, d.generation.Load())
}

func (s *Server) serveLocalScript(w http.ResponseWriter, r *http.Request, d *Device, paired string) {
	if d == nil {
		compatibilityError(w, 502, "DEVICE_NOT_CONNECTED", "device is offline")
		return
	}
	generation := d.generation.Load()
	if r.Method == "GET" {
		scripts, err := d.localScriptList(r.Context())
		if err != nil {
			compatibilityError(w, commandErrorStatus(err), "COMMAND_NOT_CONFIRMED", err.Error())
			return
		}
		jsonReply(w, 200, map[string]any{"loadedScripts": scripts})
		return
	}
	stop := strings.HasSuffix(r.URL.Path, "/stop")
	if !stop && (!s.cfg.CompatibilityControl || paired != d.mac) {
		compatibilityError(w, 403, "CONTROL_NOT_ENABLED", "confirm pairing and enable compatibilityControl first")
		return
	}
	if strings.HasSuffix(r.URL.Path, "/replace") || strings.HasSuffix(r.URL.Path, "/swap") || strings.HasSuffix(r.URL.Path, "/move") {
		s.serveLocalMutation(w, r, d, generation)
		return
	}
	// Firmware runs stored slots at their own speeds. The core cannot apply the
	// manual speed ceiling to an opaque on-device script.
	if !stop && s.cfg.MaxSpeed != 100 {
		compatibilityError(w, 403, "LOCAL_SCRIPT_SPEED_UNKNOWN", "local scripts require maxSpeed 100; their speeds cannot be capped by this server")
		return
	}
	name := "local-script-start"
	var args []int
	var numeric *wireNumber
	if stop {
		name = "local-script-stop"
	}
	if r.URL.Path == "/vacuglide/local-script" {
		var body struct {
			Index apiNumber `json:"localScriptIndex"`
		}
		if !readCompatibilityJSON(w, r, map[string]any{"localScriptIndex": &body.Index}) {
			return
		}
		if !numberRange(w, "localScriptIndex", body.Index, 0, protocol.LocalScriptSlots-1) {
			return
		}
		var err error
		numeric, err = body.Index.native()
		if err != nil {
			compatibilityError(w, 400, "VALIDATION_ERROR", err.Error())
			return
		}
		name, args = "local-script-set", []int{numeric.Integer}
	} else if r.ContentLength != 0 || strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		compatibilityError(w, 400, "VALIDATION_ERROR", "send command without a body")
		return
	}
	ctx := r.Context()
	if stop {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
	}
	state, err := d.compatibleNumberOperation(ctx, name, stop, "", 0, generation, numeric, nil, args...)
	if err != nil {
		compatibilityError(w, commandErrorStatus(err), "COMMAND_NOT_CONFIRMED", err.Error())
		return
	}
	jsonReply(w, 200, compatibilityState(state))
}
