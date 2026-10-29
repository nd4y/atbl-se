package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"autoblow.local/emulator/internal/protocol"
)

const maxScriptBytes = 4 << 20
const maxStoredScripts = 128

// The store keeps coerced source numbers and a checksum; binary data and limits
// are recomputed on every load. No source filename or URL becomes a local path.
type scriptStore struct {
	mu  sync.Mutex
	dir string
}
type storedScript struct {
	Version int                   `json:"version"`
	Actions []protocol.SyncAction `json:"actions"`
	SHA256  string                `json:"sha256"`
}
type scriptData struct {
	binary    []byte
	end, peak int
}

func makeScript(actions []protocol.SyncAction) (scriptData, error) {
	b, err := protocol.EncodeSyncScriptCandidate(actions)
	if err != nil {
		return scriptData{}, err
	}
	if len(b) < 14 {
		return scriptData{}, errors.New("script has no playable records; header-only files are not sent to firmware")
	}
	var previous uint32
	for i := 8; i < len(b); i += 6 {
		at := binary.LittleEndian.Uint32(b[i : i+4])
		if at > protocol.MaxScriptTime || i > 8 && at < previous {
			return scriptData{}, errors.New("encoded timeline must be ordered and within the supported firmware seek range")
		}
		previous = at
	}
	peak := 0
	for i := 13; i < len(b); i += 6 {
		if int(b[i]) > peak {
			peak = int(b[i])
		}
	}
	// Firmware metadata uses the timestamp of the final encoded record, not
	// the source Funscript endpoint which the stock converter omits. Seeking
	// beyond this record can overrun the firmware's unchecked start scan.
	end := int(binary.LittleEndian.Uint32(b[len(b)-6:]))
	return scriptData{b, end, peak}, nil
}

func (store *scriptStore) put(actions []protocol.SyncAction) (string, scriptData, error) {
	data, err := makeScript(actions)
	if err != nil {
		return "", data, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := os.MkdirAll(store.dir, 0700); err != nil {
		return "", data, err
	}
	files, err := os.ReadDir(store.dir)
	if err != nil {
		return "", data, err
	}
	count := 0
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".json") {
			count++
		}
	}
	if count >= maxStoredScripts {
		return "", data, errors.New("local script store is full")
	}
	raw, err := json.Marshal(actions)
	if err != nil {
		return "", data, err
	}
	sum := sha256.Sum256(raw)
	record, err := json.Marshal(storedScript{1, actions, hex.EncodeToString(sum[:])})
	if err != nil || len(record) > maxScriptBytes {
		return "", data, errors.New("script too large")
	}
	token, _, err := protocol.Command("state")
	if err != nil {
		return "", data, err
	}
	temp, err := os.CreateTemp(store.dir, ".pending-")
	if err != nil {
		return "", data, err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(record); err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temp.Name(), filepath.Join(store.dir, token+".json"))
	}
	return token, data, err
}

func (store *scriptStore) get(token string) (scriptData, error) {
	if !protocol.ValidScriptToken(token) {
		return scriptData{}, errors.New("invalid script token")
	}
	path := filepath.Join(store.dir, token+".json")
	info, err := os.Lstat(path)
	if err != nil {
		return scriptData{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxScriptBytes {
		return scriptData{}, errors.New("invalid script file")
	}
	f, err := os.Open(path)
	if err != nil {
		return scriptData{}, err
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, maxScriptBytes+1))
	dec.DisallowUnknownFields()
	var record storedScript
	if err := dec.Decode(&record); err != nil {
		return scriptData{}, err
	}
	if dec.Decode(new(any)) != io.EOF || record.Version != 1 {
		return scriptData{}, errors.New("invalid script record")
	}
	raw, err := json.Marshal(record.Actions)
	if err != nil {
		return scriptData{}, err
	}
	sum := sha256.Sum256(raw)
	if record.SHA256 != hex.EncodeToString(sum[:]) {
		return scriptData{}, errors.New("script checksum mismatch")
	}
	return makeScript(record.Actions)
}

func isImplementedSyncRoute(key string) bool {
	switch key {
	case "PUT /vacuglide/sync-script/upload-funscript", "PUT /vacuglide/sync-script/upload-csv", "PUT /vacuglide/sync-script/load-token", "PUT /vacuglide/sync-script/start", "PUT /vacuglide/sync-script/stop", "PUT /vacuglide/sync-script/offset", "PUT /vacuglide/sync-script/loop", "PUT /vacuglide/sync-script/upload-funscript-url", "PUT /vacuglide/sync-script/upload-csv-url":
		return true
	}
	return false
}

func parseActions(raw []byte, format string) ([]protocol.SyncAction, error) {
	if format == "upload-csv" {
		lines := strings.Split(string(raw), "\n")
		if len(lines) < 2 {
			return nil, protocol.ScriptError("No lines found in CSV script")
		}
		lines = lines[:len(lines)-1]
		if len(lines) > 100000 {
			return nil, protocol.ScriptError("Funscript actions is too long, autoblow accepts max 100000 actions / funscript")
		}
		actions := make([]protocol.SyncAction, 0, len(lines))
		for _, line := range lines {
			fields := strings.Split(line, ",")
			if len(fields) != 2 {
				return nil, protocol.ScriptError("Invalid CSV line: " + line)
			}
			actions = append(actions, protocol.SyncAction{At: csvInteger(fields[0]), Pos: csvInteger(fields[1])})
		}
		return actions, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var valueJSON any
	if err := decoder.Decode(&valueJSON); err != nil {
		return nil, &protocol.ConversionError{Code: "SyntaxError", Message: "invalid Funscript JSON"}
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, &protocol.ConversionError{Code: "SyntaxError", Message: "invalid Funscript JSON"}
	}
	missing := valueJSON == nil
	switch v := valueJSON.(type) {
	case bool:
		missing = !v
	case string:
		missing = v == ""
	case json.Number:
		n, _ := jsNumber(v)
		missing = n == 0
	}
	if missing {
		return nil, protocol.ScriptError("No funscript data provided")
	}
	root, _ := valueJSON.(map[string]any)
	value, exists := root["actions"]
	if !exists || value == nil {
		return nil, protocol.ScriptError("Funscript data is missing actions")
	}
	rows, ok := value.([]any)
	if !ok || len(rows) == 0 {
		return nil, protocol.ScriptError("Funscript actions is not an array")
	}
	if len(rows) > 100000 {
		return nil, protocol.ScriptError("Funscript actions is too long, autoblow accepts max 100000 actions / funscript")
	}
	actions := make([]protocol.SyncAction, len(rows))
	for i, row := range rows {
		fields, ok := row.(map[string]any)
		if !ok {
			return nil, protocol.ScriptError("Funscript actions are missing at or pos")
		}
		at, hasAt := fields["at"]
		pos, hasPos := fields["pos"]
		if !hasAt || !hasPos {
			return nil, protocol.ScriptError("Funscript actions are missing at or pos")
		}
		var err error
		actions[i].At, err = jsNumber(at)
		if err != nil {
			return nil, err
		}
		actions[i].Pos, err = jsNumber(pos)
		if err != nil {
			return nil, err
		}
		// Buffer's NaN-to-zero coercion occurs before any persisted JSON state.
		if math.IsNaN(actions[i].At) {
			actions[i].At = 0
		}
		if math.IsNaN(actions[i].Pos) {
			actions[i].Pos = 0
		}
	}
	return actions, nil
}

func readScriptUpload(w http.ResponseWriter, r *http.Request, format string) ([]protocol.SyncAction, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxScriptBytes)
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, errors.New("multipart file required")
	}
	part, err := reader.NextPart()
	if err != nil {
		return nil, errors.New("multipart file required")
	}
	defer part.Close()
	if part.FormName() != "file" || part.FileName() == "" {
		return nil, errors.New("expected file field")
	}
	raw, err := io.ReadAll(io.LimitReader(part, maxScriptBytes+1))
	if err != nil || len(raw) > maxScriptBytes {
		return nil, errors.New("upload exceeds limit or is incomplete")
	}
	if _, err = reader.NextPart(); err != io.EOF {
		return nil, errors.New("expected exactly one file")
	}
	return parseActions(raw, format)
}

func (s *Server) serveSyncScript(w http.ResponseWriter, r *http.Request, d *Device, paired string) {
	if d == nil {
		compatibilityError(w, 502, "DEVICE_NOT_CONNECTED", "device is offline")
		return
	}
	operation := strings.TrimPrefix(r.URL.Path, "/vacuglide/sync-script/")
	requestGeneration := d.generation.Load()
	stop := operation == "stop"
	if !stop && (!s.cfg.CompatibilityControl || paired != d.mac) {
		compatibilityError(w, 403, "CONTROL_NOT_ENABLED", "confirm pairing and enable compatibilityControl first")
		return
	}
	var token string
	var data scriptData
	var err error
	name := "sync-script-" + operation
	var args []int
	var numeric *wireNumber
	switch operation {
	case "upload-funscript", "upload-csv", "upload-funscript-url", "upload-csv-url":
		var actions []protocol.SyncAction
		if strings.HasSuffix(operation, "-url") {
			var body struct {
				URL apiString `json:"url"`
			}
			if !readCompatibilityJSON(w, r, map[string]any{"url": &body.URL}) || !requireString(w, "url", body.URL) {
				return
			}
			var raw []byte
			raw, err = s.scriptURLs.fetch(r.Context(), body.URL.Value)
			if err != nil {
				if errors.Is(err, errScriptURLInvalid) {
					compatibilityError(w, 400, "Error", "SyncScriptService: Error creating sync script from url "+body.URL.Value+": TypeError: Invalid URL")
					return
				}
				status := 502
				if errors.Is(err, errScriptURLDenied) {
					status = 403
				} else if errors.Is(err, context.DeadlineExceeded) {
					status = 504
				}
				compatibilityError(w, status, "SCRIPT_FETCH_FAILED", err.Error())
				return
			}
			actions, err = parseActions(raw, strings.TrimSuffix(operation, "-url"))
		} else {
			actions, err = readScriptUpload(w, r, operation)
		}
		if err == nil {
			data, err = makeScript(actions)
		}
		if err == nil && data.peak > s.cfg.MaxSpeed {
			err = errors.New("script exceeds configured maxSpeed")
		}
		if err != nil {
			code := "VALIDATION_ERROR"
			var conversion *protocol.ConversionError
			if errors.As(err, &conversion) {
				code = conversion.Code
			}
			compatibilityError(w, 400, code, err.Error())
			return
		}
		token, data, err = s.scripts.put(actions)
		if err != nil {
			compatibilityError(w, 507, "SCRIPT_STORAGE_ERROR", "could not persist script locally")
			return
		}
		name = "sync-script-load-token"
	case "load-token":
		var body struct {
			Token apiString `json:"scriptToken"`
		}
		if !readCompatibilityJSON(w, r, map[string]any{"scriptToken": &body.Token}) || !requireString(w, "scriptToken", body.Token) {
			return
		}
		token = body.Token.Value
		data, err = s.scripts.get(token)
		if err != nil {
			compatibilityError(w, 404, "SCRIPT_NOT_FOUND", "valid local script required")
			return
		}
		if data.peak > s.cfg.MaxSpeed {
			compatibilityError(w, 400, "VALIDATION_ERROR", "script exceeds configured maxSpeed")
			return
		}
	case "start":
		var body struct {
			Time apiNumber `json:"startTimeMs"`
		}
		if !readCompatibilityJSON(w, r, map[string]any{"startTimeMs": &body.Time}) {
			return
		}
		if !requireNumber(w, "startTimeMs", body.Time) {
			return
		}
		numeric, err = body.Time.native()
		if err != nil || numeric.Integer < 0 || numeric.Integer > protocol.MaxScriptTime {
			compatibilityError(w, 400, "VALIDATION_ERROR", "invalid startTimeMs")
			return
		}
		args = []int{numeric.Integer}
	case "offset":
		var body struct {
			Time apiNumber `json:"offsetTimeMs"`
		}
		if !readCompatibilityJSON(w, r, map[string]any{"offsetTimeMs": &body.Time}) {
			return
		}
		if !requireNumber(w, "offsetTimeMs", body.Time) {
			return
		}
		numeric, err = body.Time.native()
		if err != nil || numeric.Integer < -protocol.MaxScriptTime || numeric.Integer > protocol.MaxScriptTime {
			compatibilityError(w, 400, "VALIDATION_ERROR", "invalid offsetTimeMs")
			return
		}
		args = []int{numeric.Integer}
	case "loop":
		var body struct {
			Loop apiBool `json:"loop"`
		}
		if !readCompatibilityJSON(w, r, map[string]any{"loop": &body.Loop}) {
			return
		}
		if !requireBool(w, "loop", body.Loop) {
			return
		}
		args = []int{0}
		if body.Loop.Value {
			args[0] = 1
		}
	case "stop":
		if r.ContentLength != 0 || strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			compatibilityError(w, 400, "VALIDATION_ERROR", "send STOP without a body")
			return
		}
	}
	ctx := r.Context()
	if stop {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
	}
	st, err := d.compatibleNumberOperation(ctx, name, stop, token, data.end, requestGeneration, numeric, data.binary, args...)
	if err != nil {
		compatibilityError(w, commandErrorStatus(err), "COMMAND_NOT_CONFIRMED", err.Error())
		return
	}
	jsonReply(w, 200, compatibilityState(st))
}

// The stock firmware pulls this exact path over its device TLS endpoint. Only
// the current paired connection's identity and selected token may fetch it.
func (s *Server) serveScriptBinary(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	kind, prefix, enabled := "sync", "/device-backend/sync-script-bin/", s.cfg.ExperimentalSyncScripts
	if strings.HasPrefix(r.URL.Path, "/device-backend/local-script-bin/") {
		kind, prefix, enabled = "local", "/device-backend/local-script-bin/", s.cfg.ExperimentalLocalScripts
	}
	token := strings.TrimPrefix(r.URL.Path, prefix)
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	if !enabled || r.Method != "GET" || ip == nil || !ip.IsPrivate() && !ip.IsLoopback() {
		http.NotFound(w, r)
		return
	}
	digest := sha256.Sum256([]byte(r.Header.Get("x-device-token")))
	mac := strings.ToLower(strings.ReplaceAll(r.Header.Get("x-device-mac"), ":", ""))
	s.mu.Lock()
	d, paired, hash, closing := s.device, s.paired, s.deviceTokenHash, s.closing
	s.mu.Unlock()
	if closing || d == nil || paired != d.mac || mac != d.mac || r.Header.Get("x-device-token") == "" || subtle.ConstantTimeCompare(digest[:], hash[:]) != 1 {
		http.Error(w, "device authorization required", 401)
		return
	}
	d.mu.Lock()
	allowed := d.ready && !d.closing && d.deliveryToken == token && d.deliveryKind == kind
	allowed = allowed && d.deliveryGeneration == d.generation.Load() && d.stopRequests.Load() == 0
	var payload []byte
	if allowed {
		payload = append([]byte(nil), d.deliveryBinary...)
	}
	d.mu.Unlock()
	if !allowed {
		http.NotFound(w, r)
		return
	}
	if len(payload) == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	w.WriteHeader(200)
	_, _ = w.Write(payload)
}
