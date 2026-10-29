package core

import (
	"context"
	"testing"
	"time"
)

func TestClosingDeviceCannotBeReenabledByBootstrap(t *testing.T) {
	s, _ := fixture(t, func(c *Config) { c.CompatibilityControl = true })
	pair(t, s)
	s.mu.Lock()
	d := s.device
	s.mu.Unlock()
	d.beginClosing()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := d.bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	ready := d.ready
	d.mu.Unlock()
	if ready {
		t.Fatal("bootstrap reopened a closing device")
	}
	if code, _ := compatCall(t, s, "PUT", "/vacuglide/target-speed", map[string]int{"targetSpeed": 15}, s.Token()); code != 502 {
		t.Fatal(code)
	}
	if code, _ := call(t, s, "POST", "/api/start", startRequest(t, s, 15), s.Token()); code != 409 {
		t.Fatal(code)
	}
}

func TestServerClosingRejectsControlBeforeStopCompletes(t *testing.T) {
	s, sim := fixture(t, func(c *Config) { c.CompatibilityControl = true })
	pair(t, s)
	if code, _ := compatCall(t, s, "PUT", "/vacuglide/target-speed", map[string]int{"targetSpeed": 15}, s.Token()); code != 200 {
		t.Fatal(code)
	}
	hold := make(chan struct{})
	sim.mu.Lock()
	sim.holdStop = hold
	sim.mu.Unlock()
	released := false
	defer func() {
		if !released {
			close(hold)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Close(ctx) }()
	deadline := time.Now().Add(time.Second)
	for {
		s.mu.Lock()
		closing := s.closing
		s.mu.Unlock()
		if closing {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("shutdown did not close admission")
		}
		time.Sleep(time.Millisecond)
	}
	_, status := compatCall(t, s, "GET", "/vacuglide/connected", nil, s.Token())
	if status["connected"] != false {
		t.Fatal("closing server still advertised control availability")
	}
	if code, _ := compatCall(t, s, "PUT", "/vacuglide/target-speed", map[string]int{"targetSpeed": 15}, s.Token()); code != 502 {
		t.Fatal("new control admitted during shutdown", code)
	}
	close(hold)
	released = true
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestClosingEventHubDiscardsPendingButtonEvents(t *testing.T) {
	h := newEventHub()
	ch, ok := h.subscribe()
	if !ok {
		t.Fatal("subscribe")
	}
	h.publish("mode-button-pressed")
	h.close()
	if _, ok := <-ch; ok {
		t.Fatal("shutdown replayed a pending button event")
	}
}
