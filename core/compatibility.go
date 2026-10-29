package core

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/nd4y/atblse/internal/protocol"
)

func isCompatibilityPath(path string) bool {
	return strings.HasPrefix(path, "/vacuglide/") || path == "/autoblow/connected" || path == "/cluster/time" || path == "/events/stream"
}

// Script routes remain opt-in until physical acceptance. The wire contracts
// are implemented; a disabled feature must not report simulated success.
var scriptFeatureRoutes = map[string]bool{
	"PUT /vacuglide/local-script":                     true,
	"PUT /vacuglide/local-script/start":               true,
	"PUT /vacuglide/local-script/stop":                true,
	"GET /vacuglide/local-script/loaded":              true,
	"PUT /vacuglide/local-script/replace":             true,
	"PUT /vacuglide/local-script/swap":                true,
	"PUT /vacuglide/local-script/move":                true,
	"PUT /vacuglide/sync-script/load-token":           true,
	"PUT /vacuglide/sync-script/upload-funscript":     true,
	"PUT /vacuglide/sync-script/upload-csv":           true,
	"PUT /vacuglide/sync-script/upload-funscript-url": true,
	"PUT /vacuglide/sync-script/upload-csv-url":       true,
	"PUT /vacuglide/sync-script/start":                true,
	"PUT /vacuglide/sync-script/stop":                 true,
	"PUT /vacuglide/sync-script/offset":               true,
	"PUT /vacuglide/sync-script/loop":                 true,
}

type compatibleState struct {
	OperationalMode       string `json:"operationalMode"`
	LocalScript           int    `json:"localScript"`
	TargetSpeed           int    `json:"targetSpeed"`
	StrokePlusValve       bool   `json:"strokePlusValve"`
	StrokeMinusValve      bool   `json:"strokeMinusValve"`
	SyncScriptCurrentTime int    `json:"syncScriptCurrentTime"`
	SyncScriptOffsetTime  int    `json:"syncScriptOffsetTime"`
	SyncScriptToken       string `json:"syncScriptToken"`
	SyncScriptLoop        bool   `json:"syncScriptLoop"`
}

func compatibilityState(st protocol.State) compatibleState {
	return compatibleState{st.OperationalMode, st.LocalScript, st.TargetSpeed, st.StrokePlusValve, st.StrokeMinusValve,
		st.SyncScriptCurrentTime, st.SyncScriptOffsetTime, st.ScriptToken, st.SyncScriptLoop}
}

func compatibilityError(w http.ResponseWriter, status int, code, message string) {
	jsonReply(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func (s *Server) compatibleCluster(r *http.Request) string {
	if s.cfg.CompatibilityCluster != "" {
		return s.cfg.CompatibilityCluster
	}
	if r.TLS != nil {
		return r.Host
	}
	host, _, _ := net.SplitHostPort(s.DeviceAddress())
	_, port, _ := net.SplitHostPort(s.DeviceAddress())
	ip := net.ParseIP(host)
	if host == "" || ip != nil && ip.IsUnspecified() {
		host, _, _ = net.SplitHostPort(r.Host)
		if host == "" {
			host = r.Host
		}
	}
	return net.JoinHostPort(host, port)
}

func (s *Server) serveCompatibility(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	token := r.Header.Get("x-device-token")
	if r.URL.Path == "/events/stream" {
		token = r.URL.Query().Get("deviceToken")
	}
	digest := sha256.Sum256([]byte(token))
	s.mu.Lock()
	d, paired, deviceHash := s.device, s.paired, s.deviceTokenHash
	if s.closing {
		d = nil
	}
	s.mu.Unlock()
	local := subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) == 1
	device := d != nil && subtle.ConstantTimeCompare(digest[:], deviceHash[:]) == 1
	if token == "" || !local && !device {
		compatibilityError(w, 401, "UNAUTHORIZED", "valid x-device-token required")
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
		compatibilityError(w, 403, "ORIGIN_REJECTED", "cross-origin request rejected")
		return
	}
	key := r.Method + " " + r.URL.Path
	if key == "GET /events/stream" {
		s.serveDeviceEvents(w, r, token, d, local)
		return
	}
	if key == "GET /cluster/time" {
		jsonReply(w, 200, map[string]int64{"time": time.Now().UnixMilli()})
		return
	}
	if key == "GET /vacuglide/connected" || key == "GET /autoblow/connected" {
		if d == nil {
			jsonReply(w, 200, map[string]bool{"connected": false})
			return
		}
		jsonReply(w, 200, map[string]any{"connected": true, "cluster": s.compatibleCluster(r), "deviceType": "vacuglide"})
		return
	}
	if isImplementedSyncRoute(key) && (s.cfg.ExperimentalSyncScripts || key == "PUT /vacuglide/sync-script/stop") {
		s.serveSyncScript(w, r, d, paired)
		return
	}
	if isImplementedLocalRoute(key) && (s.cfg.ExperimentalLocalScripts || key == "GET /vacuglide/local-script/loaded" || key == "PUT /vacuglide/local-script/stop") {
		s.serveLocalScript(w, r, d, paired)
		return
	}
	if scriptFeatureRoutes[key] {
		compatibilityError(w, 501, "EXPERIMENTAL_FEATURE_DISABLED", "enable the corresponding experimental script feature")
		return
	}
	switch key {
	case "GET /vacuglide/info", "GET /vacuglide/state", "PUT /vacuglide/target-speed", "PUT /vacuglide/target-speed/stop", "PUT /vacuglide/valve/stroke-plus", "PUT /vacuglide/valve/stroke-minus":
	default:
		compatibilityError(w, 404, "NOT_FOUND", "unknown device-control route")
		return
	}
	if d == nil {
		compatibilityError(w, 502, "DEVICE_NOT_CONNECTED", "device is offline")
		return
	}
	if key == "GET /vacuglide/info" {
		// The local server intentionally offers no firmware update. This status is
		// relative to its preserve-current profile, not the manufacturer's latest.
		jsonReply(w, 200, map[string]any{"firmwareVersion": 1.03, "firmwareBranch": d.branch,
			"firmwareStatus": "UP_TO_DATE", "hardwareVersion": "vacuglide", "mac": d.mac, "deviceType": "vacuglide"})
		return
	}
	if key == "GET /vacuglide/state" {
		st, err := d.rpc(r.Context(), "state")
		if err != nil {
			status := 504
			if errors.Is(err, ErrUnavailable) {
				status = 502
			}
			compatibilityError(w, status, "DEVICE_UNAVAILABLE", "no confirmed device response")
			return
		}
		jsonReply(w, 200, compatibilityState(st))
		return
	}
	stop := key == "PUT /vacuglide/target-speed/stop"
	requestGeneration := d.generation.Load()
	if !stop && (!s.cfg.CompatibilityControl || paired != d.mac) {
		compatibilityError(w, 403, "CONTROL_NOT_ENABLED", "confirm pairing and enable compatibilityControl first")
		return
	}
	name := "target-speed-stop"
	args := []int{}
	var numeric *wireNumber
	if key == "PUT /vacuglide/target-speed" {
		var body struct {
			Speed apiNumber `json:"targetSpeed"`
		}
		if !readCompatibilityJSON(w, r, map[string]any{"targetSpeed": &body.Speed}) {
			return
		}
		if !numberRange(w, "targetSpeed", body.Speed, 0, 100) {
			return
		}
		var err error
		numeric, err = body.Speed.native()
		if err != nil || body.Speed.Value > float64(s.cfg.MaxSpeed) || numeric.Integer > s.cfg.MaxSpeed {
			compatibilityError(w, 400, "VALIDATION_ERROR", "targetSpeed outside configured limits")
			return
		}
		name, args = "target-speed-set", []int{numeric.Integer}
	} else if !stop {
		var body struct {
			Open apiBool `json:"valveState"`
		}
		if !readCompatibilityJSON(w, r, map[string]any{"valveState": &body.Open}) {
			return
		}
		if !requireBool(w, "valveState", body.Open) {
			return
		}
		name = "valve-stroke-plus-set"
		if strings.HasSuffix(r.URL.Path, "stroke-minus") {
			name = "valve-stroke-minus-set"
		}
		value := 0
		if body.Open.Value {
			value = 1
		}
		args = []int{value}
	} else if r.ContentLength > 0 || strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		compatibilityError(w, 400, "VALIDATION_ERROR", "send STOP without a JSON body or Content-Type")
		return
	}
	ctx := r.Context()
	if stop {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
	}
	st, err := d.compatibleNumberOperation(ctx, name, stop, "", 0, requestGeneration, numeric, nil, args...)
	if err != nil {
		status := commandErrorStatus(err)
		compatibilityError(w, status, "COMMAND_NOT_CONFIRMED", err.Error())
		return
	}
	jsonReply(w, 200, compatibilityState(st))
}

// The compatibility API has no panel lease. One non-STOP operation may be
// in flight; STOP bypasses that reservation and invalidates its generation.
func (d *Device) compatibleOperation(ctx context.Context, name string, stop bool, token string, end int, generation uint64, args ...int) (protocol.State, error) {
	return d.compatibleNumberOperation(ctx, name, stop, token, end, generation, nil, nil, args...)
}
func (d *Device) compatibleNumberOperation(ctx context.Context, name string, stop bool, token string, end int, generation uint64, numeric *wireNumber, payload []byte, args ...int) (protocol.State, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if name == "local-script-stop" {
		d.mu.Lock()
		native := d.epoch != ""
		d.mu.Unlock()
		if native {
			return protocol.State{}, errors.New("panel session active; use the global STOP")
		}
	}
	var id, wire string
	var err error
	if name == "sync-script-load-token" {
		id, wire, err = protocol.SyncLoadCommand(token)
	} else {
		id, wire, err = protocol.Command(name, args...)
	}
	if err != nil {
		return protocol.State{}, err
	}
	if numeric != nil {
		if len(args) != 1 || args[0] != numeric.Integer || !jsDecimal.MatchString(numeric.Text) {
			return protocol.State{}, errors.New("invalid numeric wire value")
		}
		wire = "/" + name + "/" + numeric.Text + "/" + id
	}
	if stop {
		d.stopRequests.Add(1)
		d.generation.Add(1)
		d.cancelOperation()
		defer d.stopRequests.Add(-1)
	}
	d.gate.Lock()
	d.mu.Lock()
	reject := func(err error) (protocol.State, error) { d.mu.Unlock(); d.gate.Unlock(); return protocol.State{}, err }
	if err := ctx.Err(); err != nil {
		return reject(err)
	}
	if !d.ready || d.closing {
		return reject(ErrUnavailable)
	}
	if d.stopping || !stop && (d.compatibilityBusy || d.stopRequests.Load() != 0 || generation != d.generation.Load() || d.epoch != "") {
		return reject(errors.New("another control operation owns the device"))
	}
	if name == "local-script-stop" && d.epoch != "" {
		return reject(errors.New("panel session active; use the global STOP"))
	}
	if name == "sync-script-start" {
		if d.syncToken == "" || d.state == nil || d.state.ScriptToken != d.syncToken {
			return reject(errNoLocalSyncScript)
		}
		if name == "sync-script-start" && args[0] > d.syncEnd {
			return reject(errSyncStartRange)
		}
	}
	if !stop {
		d.compatibilityBusy = true
		d.operationCancel = cancel
		defer func() {
			d.mu.Lock()
			d.compatibilityBusy = false
			d.operationCancel = nil
			if name == "sync-script-load-token" && d.deliveryKind == "sync" && d.deliveryGeneration == generation {
				d.deliveryToken, d.deliveryKind = "", ""
				d.deliveryBinary = nil
			}
			d.mu.Unlock()
		}()
	}
	if name == "sync-script-load-token" {
		d.syncToken = ""
		d.deliveryToken = token
		d.deliveryKind = "sync"
		d.deliveryBinary = append([]byte(nil), payload...)
	}
	expectedToken := d.syncToken
	d.epoch = ""
	d.expires = time.Time{}
	d.compatibilityActive = !stop
	generation = d.generation.Add(1)
	if name == "sync-script-load-token" {
		d.deliveryGeneration = generation
	}
	d.mu.Unlock()
	id, ch, err := d.sendWireLocked(id, wire)
	d.gate.Unlock()
	var st protocol.State
	if err == nil {
		timeout := 2 * time.Second
		if name == "sync-script-load-token" {
			timeout = 65 * time.Second
		}
		st, err = d.waitFor(ctx, id, ch, timeout)
	}
	d.mu.Lock()
	superseded := generation != d.generation.Load()
	if err == nil && !superseded {
		switch name {
		case "target-speed-stop":
			if st.RawOperationalMode != protocol.ModePaused {
				err = errors.New("motor STOP not confirmed")
			}
		case "target-speed-set":
			if st.RawOperationalMode != protocol.ModePlaying || st.TargetSpeed != args[0] {
				err = errors.New("target speed not confirmed")
			}
		case "valve-stroke-plus-set":
			if st.StrokePlusValve != (args[0] == 1) {
				err = errors.New("stroke plus valve not confirmed")
			}
		case "valve-stroke-minus-set":
			if st.StrokeMinusValve != (args[0] == 1) {
				err = errors.New("stroke minus valve not confirmed")
			}
		case "sync-script-load-token":
			if st.RawOperationalMode != 70 || st.ScriptToken != token {
				err = errors.New("script load not confirmed")
			} else {
				d.syncToken, d.syncEnd = token, end
			}
		case "sync-script-start":
			playing := st.RawOperationalMode == 60
			finished := st.RawOperationalMode == 70 && int64(st.SyncScriptCurrentTime)+int64(st.SyncScriptOffsetTime) >= int64(d.syncEnd)
			if (!playing && !finished) || st.ScriptToken != expectedToken || st.StrokePlusValve || st.StrokeMinusValve {
				err = errors.New("script start not confirmed")
			}
		case "sync-script-stop":
			if st.RawOperationalMode != 70 {
				err = errors.New("script STOP not confirmed")
			}
		case "sync-script-offset":
			if st.SyncScriptOffsetTime != args[0] {
				err = errors.New("script offset not confirmed")
			}
		case "sync-script-loop":
			if st.SyncScriptLoop != (args[0] == 1) {
				err = errors.New("script loop not confirmed")
			}
		case "local-script-set":
			if st.RawOperationalMode != 41 || st.LocalScript != args[0] || st.StrokePlusValve || st.StrokeMinusValve {
				err = errors.New("local script selection not confirmed")
			}
		case "local-script-start":
			if st.RawOperationalMode == 42 {
				err = errors.New("local script start not confirmed")
			}
		case "local-script-stop":
			if st.RawOperationalMode == 41 {
				err = errors.New("local script STOP not confirmed")
			}
			if err == nil {
				d.compatibilityActive = st.RawOperationalMode == 41 || st.RawOperationalMode == 43 || st.RawOperationalMode == 60
			}
		}
	}
	d.mu.Unlock()
	// A superseded result cannot claim success or stop a newer operation.
	if superseded {
		return protocol.State{}, ErrStopped
	}
	if err != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		if d.Stop(stopCtx) != nil {
			d.disconnect()
			return protocol.State{}, ErrUnavailable
		}
	}
	return st, err
}
