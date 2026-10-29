package core

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"autoblow.local/emulator/internal/protocol"
)

type deviceEvent struct {
	Name string
	At   int64
}
type eventHub struct {
	mu          sync.Mutex
	closed      bool
	subscribers map[chan deviceEvent]bool
}

func newEventHub() *eventHub { return &eventHub{subscribers: make(map[chan deviceEvent]bool)} }
func (h *eventHub) subscribe() (chan deviceEvent, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || len(h.subscribers) >= 32 {
		return nil, false
	}
	ch := make(chan deviceEvent, 16)
	h.subscribers[ch] = true
	return ch, true
}
func (h *eventHub) unsubscribe(ch chan deviceEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subscribers[ch] {
		delete(h.subscribers, ch)
		close(ch)
	}
}

// Input comes from the exact firmware event decoder. HTTP callers cannot
// inject physical button events.
func (h *eventHub) publish(name string) {
	if name != "mode-button-pressed" && name != "speed-plus-button-pressed" && name != "speed-minus-button-pressed" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subscribers {
		select {
		case ch <- deviceEvent{name, time.Now().UnixMilli()}:
		default:
			delete(h.subscribers, ch)
			drainPendingEvents(ch)
			close(ch)
		}
	}
}
func (h *eventHub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for ch := range h.subscribers {
		delete(h.subscribers, ch)
		drainPendingEvents(ch)
		close(ch)
	}
}

func drainPendingEvents(ch chan deviceEvent) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func (s *Server) serveDeviceEvents(w http.ResponseWriter, r *http.Request, token string) {
	ch, ok := s.events.subscribe()
	if !ok {
		compatibilityError(w, 429, "EVENT_STREAM_CAPACITY", "event stream unavailable")
		return
	}
	defer s.events.unsubscribe(ch)
	id, _, err := protocol.Command("state")
	if err != nil {
		compatibilityError(w, 500, "EVENT_STREAM_FAILED", "cannot initialize event stream")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	send := func(name string, value any) error {
		b, err := json.Marshal(value)
		if err != nil {
			return err
		}
		_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, b); err != nil {
			return err
		}
		return controller.Flush()
	}
	if send("connected", map[string]string{"connectionId": id, "subscribedDevice": token}) != nil {
		return
	}
	timer := time.NewTicker(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case at := <-timer.C:
			if send("heartbeat", map[string]int64{"timestamp": at.UnixMilli()}) != nil {
				return
			}
		case event, ok := <-ch:
			if !ok {
				return
			}
			payload := map[string]any{"deviceToken": token, "payload": map[string]any{"type": event.Name, "rawData": nil, "args": []any{}}, "timestamp": event.At}
			if send(event.Name, payload) != nil {
				return
			}
		}
	}
}
