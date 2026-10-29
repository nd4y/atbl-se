package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nd4y/atblse/internal/protocol"
)

var ErrUnavailable = errors.New("device is not ready")
var ErrStopped = errors.New("control session expired or stopped")
var ErrResponseTimeout = errors.New("firmware response timeout")

type reply struct {
	kind string
	seq  uint64
	ch   chan protocol.Response
}

// Device owns one connection. gate serializes command admission and writes, but
// is never held waiting for a firmware response, so STOP can preempt a start.
type Device struct {
	conn                *websocket.Conn
	gate                sync.Mutex
	mu                  sync.Mutex
	pending             map[string]reply
	done                chan struct{}
	finish              sync.Once
	seq                 uint64
	applied             uint64
	state               *protocol.State
	ready               bool
	closing             bool
	stopping            bool
	stopDone            chan struct{}
	stopErr             error
	generation          atomic.Uint64
	stopRequests        atomic.Int32
	epoch               string
	expires             time.Time
	runEnd              time.Time
	mac                 string
	firmware            string
	branch              string
	compatibilityActive bool
	compatibilityBusy   bool
	deliveryToken       string
	syncToken           string
	syncEnd             int
	deliveryKind        string
	deliveryGeneration  uint64
	deliveryBinary      []byte
	operationCancel     context.CancelFunc
	onEvent             func(string)
}

func newDevice(c *websocket.Conn, mac, firmware string) *Device {
	return &Device{conn: c, pending: make(map[string]reply), done: make(chan struct{}), mac: mac, firmware: firmware}
}

func (d *Device) disconnect() {
	d.finish.Do(func() {
		d.mu.Lock()
		d.ready = false
		d.closing = true
		d.epoch = ""
		d.compatibilityActive = false
		d.mu.Unlock()
		close(d.done)
		d.conn.Close()
	})
}

func (d *Device) beginClosing() {
	d.gate.Lock()
	d.mu.Lock()
	d.closing = true
	d.ready = false
	d.mu.Unlock()
	d.gate.Unlock()
}

func (d *Device) read() {
	defer d.disconnect()
	d.conn.SetReadLimit(4096)
	d.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	d.conn.SetPongHandler(func(string) error { return d.conn.SetReadDeadline(time.Now().Add(30 * time.Second)) })
	for {
		kind, b, err := d.conn.ReadMessage()
		if err != nil {
			return
		}
		d.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		if kind != websocket.TextMessage {
			continue
		}
		if name, ok := protocol.ParseEvent(string(b)); ok {
			d.mu.Lock()
			ready, publish := d.ready && !d.closing, d.onEvent
			d.mu.Unlock()
			if ready && publish != nil {
				publish(name)
			}
			continue
		}
		response, err := protocol.ParseResponse(string(b))
		if err != nil {
			continue
		}
		d.mu.Lock()
		p, ok := d.pending[response.Correlation]
		if ok && (response.Kind == p.kind || response.Kind == "error") {
			delete(d.pending, response.Correlation)
			if response.Kind == "state" && p.seq >= d.applied {
				copy := response.State
				d.state = &copy
				d.applied = p.seq
			}
			p.ch <- response
		}
		d.mu.Unlock()
	}
}

// sendLocked must be called with gate held. No response wait occurs here.
func (d *Device) sendLocked(name string, args ...int) (string, chan protocol.Response, error) {
	id, wire, err := protocol.Command(name, args...)
	if err != nil {
		return "", nil, err
	}
	return d.sendWireLocked(id, wire)
}

func (d *Device) sendWireLocked(id, wire string) (string, chan protocol.Response, error) {
	return d.sendRequestLocked(id, wire, "state")
}

func (d *Device) sendRequestLocked(id, wire, kind string) (string, chan protocol.Response, error) {
	ch := make(chan protocol.Response, 1)
	d.mu.Lock()
	d.seq++
	d.pending[id] = reply{kind: kind, seq: d.seq, ch: ch}
	d.mu.Unlock()
	d.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := d.conn.WriteMessage(websocket.TextMessage, []byte(wire)); err != nil {
		d.mu.Lock()
		delete(d.pending, id)
		d.mu.Unlock()
		return "", nil, err
	}
	return id, ch, nil
}

func (d *Device) wait(ctx context.Context, id string, ch chan protocol.Response) (protocol.State, error) {
	return d.waitFor(ctx, id, ch, 2*time.Second)
}

func (d *Device) waitFor(ctx context.Context, id string, ch chan protocol.Response, timeout time.Duration) (protocol.State, error) {
	response, err := d.waitResponse(ctx, id, ch, timeout)
	return response.State, err
}

func (d *Device) waitResponse(ctx context.Context, id string, ch chan protocol.Response, timeout time.Duration) (protocol.Response, error) {
	defer func() { d.mu.Lock(); delete(d.pending, id); d.mu.Unlock() }()
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case s := <-ch:
		return s, s.Err
	case <-ctx.Done():
		return protocol.Response{}, ctx.Err()
	case <-d.done:
		return protocol.Response{}, ErrUnavailable
	case <-t.C:
		return protocol.Response{}, ErrResponseTimeout
	}
}

func (d *Device) rpc(ctx context.Context, name string, args ...int) (protocol.State, error) {
	d.gate.Lock()
	id, ch, err := d.sendLocked(name, args...)
	d.gate.Unlock()
	if err != nil {
		return protocol.State{}, err
	}
	return d.wait(ctx, id, ch)
}

func (d *Device) bootstrap(ctx context.Context) error {
	_, wire, err := protocol.Command("connected-ok")
	if err != nil {
		return err
	}
	d.gate.Lock()
	d.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	err = d.conn.WriteMessage(websocket.TextMessage, []byte(wire))
	d.gate.Unlock()
	if err != nil {
		return err
	}
	if err = d.Stop(ctx); err != nil {
		return err
	}
	d.mu.Lock()
	d.ready = !d.closing
	d.mu.Unlock()
	return nil
}

func (d *Device) Stop(ctx context.Context) error {
	return d.stop(ctx, "", 0)
}

// Automatic cleanup must only stop the panel epoch that triggered it. Check
// ownership under the write gate, so STOP plus a newer start cannot race it.
func (d *Device) stopOwned(ctx context.Context, epoch string, generation uint64) error {
	if epoch == "" {
		return ErrStopped
	}
	return d.stop(ctx, epoch, generation)
}

func (d *Device) stop(ctx context.Context, epoch string, generation uint64) error {
	conditional := epoch != ""
	if conditional {
		d.gate.Lock()
		d.mu.Lock()
		owns := d.epoch == epoch && d.generation.Load() == generation && d.stopRequests.Load() == 0
		d.mu.Unlock()
		if !owns {
			d.gate.Unlock()
			return ErrStopped
		}
	}
	d.stopRequests.Add(1)
	d.generation.Add(1)
	d.cancelOperation()
	defer d.stopRequests.Add(-1)
	if !conditional {
		d.gate.Lock()
		// An operation already admitted when STOP was announced may have
		// advanced its generation before yielding the gate. Fence it again.
		d.generation.Add(1)
	}
	d.mu.Lock()
	if d.stopping {
		done := d.stopDone
		d.mu.Unlock()
		d.gate.Unlock()
		select {
		case <-done:
			d.mu.Lock()
			err := d.stopErr
			d.mu.Unlock()
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-d.done:
			return ErrUnavailable
		}
	}
	d.stopping = true
	d.stopDone = make(chan struct{})
	d.epoch = ""
	d.compatibilityActive = false
	d.expires = time.Time{}
	d.mu.Unlock()
	id, ch, err := d.sendLocked("target-speed-stop")
	d.gate.Unlock()
	var st protocol.State
	if err == nil {
		st, err = d.wait(ctx, id, ch)
	}
	if err == nil && st.RawOperationalMode != protocol.ModePaused {
		err = errors.New("STOP not confirmed by firmware")
	}
	if err == nil {
		st, err = d.rpc(ctx, "valve-stroke-plus-set", 0)
	}
	if err == nil {
		st, err = d.rpc(ctx, "valve-stroke-minus-set", 0)
	}
	if err == nil && (st.StrokePlusValve || st.StrokeMinusValve) {
		err = errors.New("valve closure not confirmed")
	}
	d.mu.Lock()
	d.stopErr = err
	d.stopping = false
	if err != nil {
		d.ready = false
	}
	close(d.stopDone)
	d.mu.Unlock()
	return err
}

func (d *Device) Start(ctx context.Context, speed, seconds int, generation uint64) (string, error) {
	d.gate.Lock()
	d.mu.Lock()
	if err := ctx.Err(); err != nil {
		d.mu.Unlock()
		d.gate.Unlock()
		return "", err
	}
	if !d.ready || d.closing || d.stopping || d.stopRequests.Load() != 0 || generation != d.generation.Load() {
		d.mu.Unlock()
		d.gate.Unlock()
		return "", ErrUnavailable
	}
	if d.epoch != "" || d.compatibilityActive || d.compatibilityBusy {
		d.mu.Unlock()
		d.gate.Unlock()
		return "", errors.New("another control session is active; stop it first")
	}
	epoch, _, err := protocol.Command("state")
	if err != nil {
		d.mu.Unlock()
		d.gate.Unlock()
		return "", err
	}
	d.epoch = epoch
	d.expires = time.Now().Add(2 * time.Second)
	d.runEnd = time.Now().Add(time.Duration(seconds) * time.Second)
	d.mu.Unlock()
	id, ch, err := d.sendLocked("target-speed-set", speed)
	d.gate.Unlock()
	var st protocol.State
	if err == nil {
		st, err = d.wait(ctx, id, ch)
	}
	d.mu.Lock()
	valid := d.epoch == epoch && !d.stopping && d.stopRequests.Load() == 0 && generation == d.generation.Load()
	d.mu.Unlock()
	if err == nil && valid && (st.RawOperationalMode != protocol.ModePlaying || st.TargetSpeed != speed) {
		err = errors.New("panel target speed not confirmed")
	}
	if err != nil || !valid {
		// An ambiguous start must be followed by STOP, independent of HTTP cancellation.
		stopCtx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		stopErr := d.stopOwned(stopCtx, epoch, generation)
		if errors.Is(stopErr, ErrStopped) {
			return "", ErrStopped
		}
		if stopErr != nil {
			d.disconnect()
			return "", fmt.Errorf("start failed; STOP unconfirmed")
		}
		if err != nil {
			return "", err
		}
		return "", ErrStopped
	}
	return epoch, nil
}

func (d *Device) Keepalive(epoch string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	if epoch == "" || epoch != d.epoch || d.stopping || !now.Before(d.expires) || !now.Before(d.runEnd) {
		return ErrStopped
	}
	d.expires = now.Add(2 * time.Second)
	return nil
}

func (d *Device) watch() {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	ping := time.NewTicker(5 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-d.done:
			return
		case <-ping.C:
			if d.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second)) != nil {
				d.disconnect()
				return
			}
		case now := <-tick.C:
			d.mu.Lock()
			epoch, generation := d.epoch, d.generation.Load()
			expired := epoch != "" && (!now.Before(d.expires) || !now.Before(d.runEnd))
			d.mu.Unlock()
			if expired {
				ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
				err := d.stopOwned(ctx, epoch, generation)
				cancel()
				if err != nil && !errors.Is(err, ErrStopped) {
					d.disconnect()
					return
				}
			}
		}
	}
}
