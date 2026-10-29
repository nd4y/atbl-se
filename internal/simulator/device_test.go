package simulator

import (
	"encoding/binary"
	"errors"
	"github.com/nd4y/atblse/internal/protocol"
	"strings"
	"testing"
)

const id = "11111111-1111-4111-8111-111111111111"

func command(t *testing.T, d *Device, text string, fetch func(string, string) ([]byte, error)) string {
	t.Helper()
	r, e := d.Handle("/"+text+"/"+id, fetch)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestModesAndValves(t *testing.T) {
	d := New()
	for _, step := range []struct {
		cmd                      string
		mode, speed, plus, minus int
	}{{"target-speed-set/15", 43, 15, 0, 0}, {"valve-stroke-plus-set/1", 43, 15, 1, 0}, {"valve-stroke-minus-set/1", 43, 15, 1, 1}, {"target-speed-stop", 44, 15, 1, 1}, {"local-script-set/29", 41, 15, 0, 0}} {
		r := command(t, d, step.cmd, nil)
		s, e := protocol.ParseState(r)
		if e != nil || s.RawOperationalMode != step.mode || s.TargetSpeed != step.speed || s.StrokePlusValve != (step.plus == 1) || s.StrokeMinusValve != (step.minus == 1) {
			t.Fatalf("%s: %s %v", step.cmd, r, e)
		}
	}
}
func TestDownloadsAndSlotMutations(t *testing.T) {
	d := New()
	fetch := func(kind, token string) ([]byte, error) {
		b := make([]byte, 8)
		binary.LittleEndian.PutUint32(b, 99)
		binary.LittleEndian.PutUint32(b[4:], 1)
		return b, nil
	}
	command(t, d, "sync-script-load-token/"+id, fetch)
	if d.Token != id || d.Mode != 70 {
		t.Fatal(d)
	}
	command(t, d, "sync-script-start/500", nil)
	command(t, d, "sync-script-offset/-100", nil)
	command(t, d, "sync-script-loop/1", nil)
	if d.Mode != 60 || d.Current != 500 || d.Offset != -100 || d.Loop != 1 {
		t.Fatal(d)
	}
	r := command(t, d, "local-script-replace/0/"+id, fetch)
	if !strings.HasPrefix(r, "/ok/99,") {
		t.Fatal(r)
	}
	command(t, d, "local-script-swap/0/29", nil)
	if d.Slots[29] != 99 {
		t.Fatal(d.Slots)
	}
	command(t, d, "local-script-move/29/0", nil)
	if d.Slots[0] != 99 {
		t.Fatal(d.Slots)
	}
}
func TestInvalidFramesAndFailedDownload(t *testing.T) {
	d := New()
	for _, cmd := range []string{"target-speed-set/101", "target-speed-set/-1", "local-script-set/30", "valve-stroke-plus-set/2", "local-script-swap/0/0", "sync-script-start/0", "firmware-update/1.0.4"} {
		if !strings.HasPrefix(command(t, d, cmd, nil), "/error/") {
			t.Fatal(cmd)
		}
	}
	r := command(t, d, "sync-script-load-token/"+id, func(string, string) ([]byte, error) { return nil, errors.New("offline") })
	if !strings.HasPrefix(r, "/error/") || d.Token != "" {
		t.Fatal(r)
	}
	if _, e := d.Handle("/state/not-a-correlation", nil); e == nil {
		t.Fatal("bad correlation accepted")
	}
}
func TestLoopbackBoundary(t *testing.T) {
	for _, raw := range []string{"wss://example.com/device-backend/ws", "wss://192.168.1.5/device-backend/ws", "ws://127.0.0.1/device-backend/ws", "wss://user@127.0.0.1/device-backend/ws", "wss://127.0.0.1/device-backend/ws?q=1"} {
		if _, e := LocalEndpoint(raw); e == nil {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{"wss://127.0.0.1:20443/device-backend/ws", "wss://[::1]:20443/device-backend/ws"} {
		if _, e := LocalEndpoint(raw); e != nil {
			t.Fatal(e)
		}
	}
}
