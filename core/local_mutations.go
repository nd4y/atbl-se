package core

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"autoblow.local/emulator/internal/protocol"
)

// Offline script identities belong to our local library, not the cloud catalog.
// Stock firmware reports the first header word using unsigned %u.
func localScriptID(token string) uint32 {
	sum := sha256.Sum256([]byte(token))
	id := binary.LittleEndian.Uint32(sum[:4])
	if id == 0 {
		return 1
	}
	return id
}

func (s *Server) serveLocalMutation(w http.ResponseWriter, r *http.Request, d *Device, generation uint64) {
	name := "local-script-" + strings.TrimPrefix(r.URL.Path, "/vacuglide/local-script/")
	var id, wire, token string
	var replacement *localReplacement
	var err error
	var index int
	if name == "local-script-replace" {
		var body struct {
			Index apiNumber `json:"replacedLocalScriptIndex"`
			Token apiString `json:"newLocalScriptUuid"`
		}
		if !readCompatibilityJSON(w, r, map[string]any{"replacedLocalScriptIndex": &body.Index, "newLocalScriptUuid": &body.Token}) {
			return
		}
		if !requiredField(w, "replacedLocalScriptIndex", body.Index.Present) || !requiredField(w, "newLocalScriptUuid", body.Token.Present) {
			return
		}
		if !requireNumber(w, "replacedLocalScriptIndex", body.Index) || !requireString(w, "newLocalScriptUuid", body.Token) {
			return
		}
		length := utf8.RuneCountInString(body.Token.Value)
		if length != 36 {
			bound := "fewer"
			if length > 36 {
				bound = "more"
			}
			compatibilityError(w, 400, "FST_ERR_VALIDATION", "body/newLocalScriptUuid must NOT have "+bound+" than 36 characters")
			return
		}
		numeric, ok := slotNumber(w, "replacedLocalScriptIndex", body.Index)
		if !ok {
			return
		}
		index, token = numeric.Integer, body.Token.Value
		id, wire, err = protocol.LocalReplaceCommand(index, token)
		if err == nil {
			data, loadErr := s.localReplacement(token)
			if loadErr != nil {
				compatibilityError(w, 404, "SCRIPT_NOT_FOUND", "local script token not found")
				return
			}
			if data.peak > s.cfg.MaxSpeed {
				compatibilityError(w, 403, "SCRIPT_SPEED_LIMIT", "script exceeds maxSpeed")
				return
			}
			replacement = &data
			wire = "/" + name + "/" + numeric.Text + "/" + data.token + "/" + id
		}
	} else {
		var first, second apiNumber
		firstName, secondName := "scriptIndexA", "scriptIndexB"
		if name == "local-script-move" {
			firstName, secondName = "fromIndex", "toIndex"
		}
		if !readCompatibilityJSON(w, r, map[string]any{firstName: &first, secondName: &second}) {
			return
		}
		if !requiredField(w, firstName, first.Present) || !requiredField(w, secondName, second.Present) {
			return
		}
		a, ok := slotNumber(w, firstName, first)
		if !ok {
			return
		}
		b, ok := slotNumber(w, secondName, second)
		if !ok {
			return
		}
		id, wire, err = protocol.LocalSlotCommand(name, a.Integer, b.Integer)
		if err == nil {
			wire = "/" + name + "/" + a.Text + "/" + b.Text + "/" + id
		}
	}
	if err != nil {
		compatibilityError(w, 400, "VALIDATION_ERROR", err.Error())
		return
	}
	scripts, err := d.mutateLocalSlots(r.Context(), id, wire, replacement, index, generation)
	if err != nil {
		compatibilityError(w, commandErrorStatus(err), "COMMAND_NOT_CONFIRMED", err.Error())
		return
	}
	jsonReply(w, 200, map[string]any{"loadedScripts": scripts})
}

// The three mutation handlers and local-script-loaded set mode42. Reserve control
// until both the correlated list reply and a fresh state confirm that pause.
// STOP may preempt every wait; a superseded response cannot release newer control.
func (d *Device) mutateLocalSlots(ctx context.Context, id, wire string, replacement *localReplacement, index int, expected uint64) ([]uint32, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	d.gate.Lock()
	d.mu.Lock()
	reject := func(err error) ([]uint32, error) { d.mu.Unlock(); d.gate.Unlock(); return nil, err }
	if err := ctx.Err(); err != nil {
		return reject(err)
	}
	if !d.ready || d.closing {
		return reject(ErrUnavailable)
	}
	if d.stopping || d.compatibilityBusy || d.stopRequests.Load() != 0 || expected != d.generation.Load() || d.epoch != "" {
		return reject(errors.New("another control operation owns the device"))
	}
	d.compatibilityBusy = true
	d.operationCancel = cancel
	generation := d.generation.Add(1)
	if replacement != nil {
		d.deliveryToken, d.deliveryKind = replacement.token, "local"
		d.deliveryGeneration = generation
		d.deliveryBinary = append([]byte(nil), replacement.binary...)
		// Replacement deletes the on-device sync file as part of its download.
		d.syncToken, d.syncEnd = "", 0
	}
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.compatibilityBusy = false
		d.operationCancel = nil
		if replacement != nil && d.deliveryKind == "local" && d.deliveryGeneration == generation {
			d.deliveryToken, d.deliveryKind = "", ""
			d.deliveryBinary = nil
		}
		d.mu.Unlock()
	}()
	id, ch, err := d.sendRequestLocked(id, wire, "slots")
	d.gate.Unlock()
	var result protocol.Response
	if err == nil {
		timeout := 2 * time.Second
		if replacement != nil {
			timeout = 65 * time.Second
		}
		result, err = d.waitResponse(ctx, id, ch, timeout)
	}
	if generation != d.generation.Load() {
		return nil, ErrStopped
	}
	if err == nil && replacement != nil && result.Scripts[index] != replacement.id {
		err = errors.New("replacement script identity not confirmed")
	}
	if err == nil {
		d.gate.Lock()
		if generation != d.generation.Load() || d.stopRequests.Load() != 0 {
			d.gate.Unlock()
			return nil, ErrStopped
		}
		stateID, stateCh, sendErr := d.sendLocked("state")
		d.gate.Unlock()
		if sendErr != nil {
			err = sendErr
		} else {
			var state protocol.State
			state, err = d.wait(ctx, stateID, stateCh)
			if err == nil && state.RawOperationalMode != 42 {
				err = errors.New("slot mutation pause not confirmed")
			}
		}
	}
	d.mu.Lock()
	superseded := generation != d.generation.Load()
	if err == nil && !superseded {
		d.compatibilityActive = false
	}
	d.mu.Unlock()
	if superseded {
		return nil, ErrStopped
	}
	if err != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		if d.Stop(stopCtx) != nil {
			d.disconnect()
			return nil, ErrUnavailable
		}
		return nil, err
	}
	return result.Scripts, nil
}

func (d *Device) cancelOperation() {
	d.mu.Lock()
	cancel := d.operationCancel
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
