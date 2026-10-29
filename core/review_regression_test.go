package core

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"testing"
	"time"
)

func TestCancelledPanelStartCannotStopNewSession(t *testing.T) {
	s, sim := fixture(t)
	s.mu.Lock()
	d := s.device
	s.mu.Unlock()
	hold := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(hold)
		}
	}()
	sim.mu.Lock()
	sim.holdSpeedReply = hold
	before := len(sim.wires)
	sim.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	generation := d.generation.Load()
	go func() { _, err := d.Start(ctx, 5, 60, generation); done <- err }()
	until := time.Now().Add(time.Second)
	for {
		sim.mu.Lock()
		sent := len(sim.wires) > before
		sim.mu.Unlock()
		if sent {
			break
		}
		if time.Now().After(until) {
			t.Fatal("first start was not sent")
		}
		time.Sleep(time.Millisecond)
	}
	if err := d.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	sim.mu.Lock()
	sim.holdSpeedReply = nil
	sim.mu.Unlock()
	epoch, err := d.Start(ctx, 7, 60, d.generation.Load())
	if err != nil {
		t.Fatal(err)
	}
	close(hold)
	released = true
	select {
	case err := <-done:
		if !errors.Is(err, ErrStopped) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("old start did not finish")
	}
	if err := d.Keepalive(epoch); err != nil {
		t.Fatal("late old start stopped the new session", err)
	}
	sim.mu.Lock()
	mode, speed := sim.mode, sim.speed
	sim.mu.Unlock()
	if mode != 43 || speed != 7 {
		t.Fatal("new motion was changed", mode, speed)
	}
}

func TestPanelStartRequiresConfirmedSpeed(t *testing.T) {
	s, sim := fixture(t)
	s.mu.Lock()
	d := s.device
	s.mu.Unlock()
	sim.mu.Lock()
	sim.wrongSpeedReply = true
	sim.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if epoch, err := d.Start(ctx, 5, 60, d.generation.Load()); err == nil || epoch != "" {
		t.Fatal("wrong speed acknowledged", epoch, err)
	}
	sim.mu.Lock()
	mode, plus, minus := sim.mode, sim.plus, sim.minus
	sim.mu.Unlock()
	if mode != 44 || plus || minus {
		t.Fatal("ambiguous start not stopped")
	}
}

func TestCompatibilityJSONPropertyNamesAreExact(t *testing.T) {
	s, sim := fixture(t, func(c *Config) { c.CompatibilityControl = true })
	pair(t, s)
	for _, tc := range []struct {
		name, path, raw, field string
		status                 int
		value                  any
	}{
		{"upper-number", "/vacuglide/target-speed", `{"TARGETSPEED":5}`, "", 400, nil},
		{"upper-bool", "/vacuglide/valve/stroke-plus", `{"VALVESTATE":true}`, "", 400, nil},
		{"number-unknown-does-not-shadow", "/vacuglide/target-speed", `{"targetSpeed":5,"TARGETSPEED":10}`, "targetSpeed", 200, float64(5)},
		{"bool-unknown-does-not-shadow", "/vacuglide/valve/stroke-plus", `{"valveState":true,"VALVESTATE":false}`, "strokePlusValve", 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sim.mu.Lock()
			before := len(sim.wires)
			sim.mu.Unlock()
			code, result := compatCall(t, s, "PUT", tc.path, json.RawMessage(tc.raw), s.Token())
			if code != tc.status {
				t.Fatal(code, result)
			}
			sim.mu.Lock()
			sent := len(sim.wires) - before
			sim.mu.Unlock()
			if tc.status == 400 {
				if sent != 0 {
					t.Fatal("wrong-case field reached device")
				}
			} else if result[tc.field] != tc.value {
				t.Fatal(result)
			}
		})
	}
}

func TestJSNumberRuntimeReference(t *testing.T) {
	raw, err := os.ReadFile("../internal/protocol/testdata/js-number-review-20261028.json")
	if err != nil {
		t.Fatal(err)
	}
	type number struct {
		Kind  string
		Value float64
	}
	var fixture struct {
		Cases []struct {
			Input       string
			Number, CSV number
		}
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	check := func(t *testing.T, got float64, want number) {
		t.Helper()
		switch want.Kind {
		case "nan":
			if !math.IsNaN(got) {
				t.Fatal(got, want)
			}
		case "positive-infinity":
			if !math.IsInf(got, 1) {
				t.Fatal(got, want)
			}
		case "negative-infinity":
			if !math.IsInf(got, -1) {
				t.Fatal(got, want)
			}
		case "finite":
			if got != want.Value {
				t.Fatal(got, want)
			}
		default:
			t.Fatal("unknown reference", want)
		}
	}
	for _, c := range fixture.Cases {
		t.Run(c.Input, func(t *testing.T) {
			got, err := jsNumber(c.Input)
			if err != nil {
				t.Fatal(err)
			}
			check(t, got, c.Number)
			want := c.CSV
			if want.Kind == "nan" {
				want = number{Kind: "finite", Value: 0}
			}
			check(t, csvInteger(c.Input), want)
		})
	}
}

// A lease watcher can observe the old epoch during STOP admission after the
// generation changes. Both captured identities must match at cleanup admission.
func TestLeaseCleanupCannotClaimDifferentEpoch(t *testing.T) {
	s, sim := fixture(t)
	s.mu.Lock()
	d := s.device
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	old, err := d.Start(ctx, 5, 60, d.generation.Load())
	if err != nil {
		t.Fatal(err)
	}
	if err = d.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := d.Start(ctx, 7, 60, d.generation.Load())
	if err != nil {
		t.Fatal(err)
	}
	sim.mu.Lock()
	before := len(sim.wires)
	sim.mu.Unlock()
	if err = d.stopOwned(ctx, old, d.generation.Load()); !errors.Is(err, ErrStopped) {
		t.Fatal(err)
	}
	sim.mu.Lock()
	sent := len(sim.wires) - before
	sim.mu.Unlock()
	if sent != 0 {
		t.Fatal("stale expiry sent STOP", sent)
	}
	if err = d.Keepalive(current); err != nil {
		t.Fatal(err)
	}
}
