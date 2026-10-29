package core

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCompatibilityEventStreamContract(t *testing.T) {
	s, sim := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+s.Address()+"/events/stream?deviceToken="+url.QueryEscape(s.Token()), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatal(resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	read := func() (string, map[string]any) {
		var name, data string
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				break
			}
			if strings.HasPrefix(line, "event:") {
				name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			}
			if strings.HasPrefix(line, "data:") {
				data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
		if scanner.Err() != nil {
			t.Fatal(scanner.Err())
		}
		var value map[string]any
		if json.Unmarshal([]byte(data), &value) != nil {
			t.Fatal("invalid SSE data")
		}
		return name, value
	}
	name, data := read()
	if name != "connected" || data["subscribedDevice"] != s.Token() {
		t.Fatal(name)
	}
	for _, expected := range []string{"mode-button-pressed", "speed-plus-button-pressed", "speed-minus-button-pressed"} {
		if err := sim.writeReply("/event/" + expected); err != nil {
			t.Fatal(err)
		}
		name, data = read()
		payload := data["payload"].(map[string]any)
		if name != expected || payload["type"] != name || payload["rawData"] != nil {
			t.Fatal(name, payload)
		}
	}
	// This crosses the actual WebSocket decoder and SSE transport, using a
	// software firmware peer. Physical button acceptance is separate.
}

func TestEventHubDropsSlowSubscriberByClosingStream(t *testing.T) {
	h := newEventHub()
	ch, ok := h.subscribe()
	if !ok {
		t.Fatal("subscription failed")
	}
	h.publish("unrecognized-device-input")
	if len(ch) != 0 {
		t.Fatal("unrecognized event accepted")
	}
	for i := 0; i < 17; i++ {
		h.publish("mode-button-pressed")
	}
	if _, ok := <-ch; ok {
		t.Fatal("overflow replayed stale button events")
	}
	h.close()
	if _, ok = h.subscribe(); ok {
		t.Fatal("closed hub accepted subscriber")
	}
}

func TestCancelledCompatibilityStopStillReachesDevice(t *testing.T) {
	s, _ := fixture(t, func(c *Config) { c.CompatibilityControl = true })
	pair(t, s)
	if code, _ := compatCall(t, s, "PUT", "/vacuglide/target-speed", map[string]int{"targetSpeed": 15}, s.Token()); code != 200 {
		t.Fatal(code)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("PUT", "http://localhost/vacuglide/target-speed/stop", nil).WithContext(ctx)
	req.Header.Set("x-device-token", s.Token())
	w := httptest.NewRecorder()
	s.serveCompatibility(w, req)
	if w.Code != 200 {
		t.Fatal("cancelled HTTP caller suppressed motor STOP", w.Code)
	}
	_, state := compatCall(t, s, "GET", "/vacuglide/state", nil, s.Token())
	if state["operationalMode"] != "TARGET_SPEED_PAUSED" {
		t.Fatal(state)
	}
}
