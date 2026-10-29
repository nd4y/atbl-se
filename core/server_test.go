package core

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type simulator struct {
	mu                    sync.Mutex
	writeMu               sync.Mutex
	commands              []string
	mode, speed           int
	conn                  *websocket.Conn
	wrongID               bool
	holdStart             chan struct{}
	holdSpeedReply        chan struct{}
	wrongSpeedReply       bool
	holdStop              chan struct{}
	plus, minus           bool
	scriptToken           string
	current, offset, loop int
	downloadBase          string
	loadedBytes           []byte
	rejectSync            bool
	wrongScript           bool
	holdSync              chan struct{}
	localScript           int
	listWrongKind         bool
	errorCommand          string
	slots                 [30]uint32
	wires                 []string
	holdMutation          chan struct{}
	mutationWrongPause    bool
	localLoadedBytes      []byte
	syncFinishOnStart     bool
}

func fixture(t *testing.T, configure ...func(*Config)) (*Server, *simulator) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.DeviceListen = "127.0.0.1:0"
	cfg.APIListen = "127.0.0.1:0"
	cfg.DataDir = t.TempDir()
	for _, apply := range configure {
		apply(&cfg)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Start(); err != nil {
		t.Fatal(err)
	}
	h := http.Header{"X-Device-Mac": {"001122334455"}, "X-Hardware-Version": {"vacuglide"}, "X-Firmware-Version": {"1.03"}, "X-Firmware-Branch": {"prod"}, "X-Device-Token": {"synthetic-secret-never-persisted"}}
	dial := websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} // only the ephemeral loopback test server
	c, _, err := dial.Dial("wss://"+s.DeviceAddress()+"/device-backend/ws", h)
	if err != nil {
		t.Fatal(err)
	}
	sim := &simulator{conn: c, mode: 50, scriptToken: "private-script-token", downloadBase: "https://" + s.DeviceAddress()}
	sim.slots = [30]uint32{1, 2, 3, 11265, 11253, 11150, 11590, 11591, 11505, 11000, 11, 11115, 11116, 11117, 11118, 11114, 13809, 11263, 11178, 11253, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30}
	go sim.run()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		s.Close(ctx)
		c.Close()
	})
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		s.mu.Lock()
		d := s.device
		s.mu.Unlock()
		if d != nil {
			d.mu.Lock()
			ready := d.ready
			d.mu.Unlock()
			if ready {
				return s, sim
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("bootstrap never became ready")
	return nil, nil
}
func (sim *simulator) run() {
	for {
		_, b, err := sim.conn.ReadMessage()
		if err != nil {
			return
		}
		p := strings.Split(string(b), "/")
		if len(p) < 3 {
			return
		}
		name := p[1]
		if name == "connected-ok" {
			continue
		}
		var downloaded []byte
		if name == "sync-script-load-token" {
			downloaded, _ = sim.fetchScript(p[2])
		}
		if name == "local-script-replace" {
			downloaded, _ = sim.fetchScriptKind("local", p[3])
		}
		sim.mu.Lock()
		sim.commands = append(sim.commands, name)
		sim.wires = append(sim.wires, string(b))
		mutation := name == "local-script-replace" || name == "local-script-swap" || name == "local-script-move"
		var mutationError string
		if mutation {
			sim.mode = 42
			if sim.mutationWrongPause {
				sim.mode = 41
			}
			var a, b int
			fmt.Sscan(p[2], &a)
			if name == "local-script-replace" {
				if len(downloaded) < 8 {
					mutationError = "SCRIPT_DOWNLOAD_ERROR"
				} else {
					sim.slots[a] = binary.LittleEndian.Uint32(downloaded[:4])
					sim.localLoadedBytes = downloaded
				}
			} else {
				fmt.Sscan(p[3], &b)
				if name == "local-script-swap" {
					if a == b {
						mutationError = "ERROR_SWAP"
					} else {
						sim.slots[a], sim.slots[b] = sim.slots[b], sim.slots[a]
					}
				} else {
					value := sim.slots[a]
					if a < b {
						copy(sim.slots[a:b], sim.slots[a+1:b+1])
					} else {
						copy(sim.slots[b+1:a+1], sim.slots[b:a])
					}
					sim.slots[b] = value
				}
			}
		}
		switch name {
		case "local-script-loaded":
			sim.mode = 42
		case "local-script-set":
			fmt.Sscan(p[2], &sim.localScript)
			sim.mode = 41
			sim.plus = false
			sim.minus = false
		case "local-script-start":
			if sim.mode == 42 {
				sim.mode = 41
			}
		case "local-script-stop":
			if sim.mode == 41 {
				sim.mode = 42
			}
		case "target-speed-set":
			fmt.Sscan(p[2], &sim.speed)
			sim.mode = 43
		case "target-speed-stop":
			sim.mode = 44
		case "valve-stroke-plus-set":
			sim.plus = p[2] == "1"
		case "valve-stroke-minus-set":
			sim.minus = p[2] == "1"
		case "sync-script-load-token":
			if sim.rejectSync || len(downloaded) == 0 {
				sim.mode = 500
			} else {
				sim.mode = 70
				sim.scriptToken = p[2]
				sim.loadedBytes = downloaded
				if sim.wrongScript {
					sim.scriptToken = "11111111-1111-4111-8111-111111111111"
				}
			}
		case "sync-script-start":
			sim.plus = false
			sim.minus = false
			sim.mode = 60
			fmt.Sscan(p[2], &sim.current)
			if sim.syncFinishOnStart {
				sim.mode = 70
				if len(sim.loadedBytes) >= 14 {
					sim.current = max(sim.current, int(binary.LittleEndian.Uint32(sim.loadedBytes[len(sim.loadedBytes)-6:]))-sim.offset)
				}
			}
		case "sync-script-stop":
			sim.mode = 70
		case "sync-script-offset":
			fmt.Sscan(p[2], &sim.offset)
		case "sync-script-loop":
			fmt.Sscan(p[2], &sim.loop)
		}
		mode, speed, wrong, hold := sim.mode, sim.speed, sim.wrongID, sim.holdStart
		holdSpeedReply := sim.holdSpeedReply
		if sim.wrongSpeedReply && name == "target-speed-set" {
			speed++
		}
		holdStop := sim.holdStop
		local, listWrong, errorCommand := sim.localScript, sim.listWrongKind, sim.errorCommand
		token, current, offset, loop, holdSync := sim.scriptToken, sim.current, sim.offset, sim.loop, sim.holdSync
		plus, minus := 0, 0
		slots, holdMutation := sim.slots, sim.holdMutation
		if sim.plus {
			plus = 1
		}
		if sim.minus {
			minus = 1
		}
		sim.mu.Unlock()
		if name == "target-speed-set" && hold != nil {
			<-hold
		}
		if name == "target-speed-stop" && holdStop != nil {
			<-holdStop
		}
		id := p[len(p)-1]
		if wrong {
			id = "11111111-1111-4111-8111-111111111111"
		}
		wire := fmt.Sprintf("/state/%d/%d/%d/%d/%d/%d/%d/%d/%s/%s", mode, local, speed, plus, minus, current, offset, loop, token, id)
		if (name == "local-script-loaded" || mutation) && !listWrong {
			values := make([]string, len(slots))
			for i, v := range slots {
				values[i] = fmt.Sprint(v)
			}
			wire = "/ok/" + strings.Join(values, ",") + "/" + id
		}
		if mutationError != "" {
			wire = "/error/" + mutationError + "/" + id
		}
		if name == errorCommand {
			wire = "/error/SYNC_SCRIPT_LOAD_TOKEN_ERROR/" + id
		}
		if name == "target-speed-set" && holdSpeedReply != nil {
			go func() { <-holdSpeedReply; sim.writeReply(wire) }()
			continue
		}
		if mutation && holdMutation != nil {
			go func() { <-holdMutation; sim.writeReply(wire) }()
			continue
		}
		if (name == "sync-script-load-token" || name == "sync-script-start" || name == "local-script-set" || name == "local-script-start") && holdSync != nil {
			go func() { <-holdSync; sim.writeReply(wire) }()
			continue
		}
		if sim.writeReply(wire) != nil {
			return
		}
	}
}

func (sim *simulator) writeReply(wire string) error {
	sim.writeMu.Lock()
	defer sim.writeMu.Unlock()
	return sim.conn.WriteMessage(websocket.TextMessage, []byte(wire))
}

func (sim *simulator) fetchScript(token string) ([]byte, error) {
	return sim.fetchScriptKind("sync", token)
}
func (sim *simulator) fetchScriptKind(kind, token string) ([]byte, error) {
	r, err := http.NewRequest("GET", sim.downloadBase+"/device-backend/"+kind+"-script-bin/"+token, nil)
	if err != nil {
		return nil, err
	}
	r.Header.Set("x-device-mac", "001122334455")
	r.Header.Set("x-device-token", "synthetic-secret-never-persisted")
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} // isolated fixture only
	defer client.CloseIdleConnections()
	resp, err := client.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download: %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
func call(t *testing.T, s *Server, method, path string, body any, token string) (int, map[string]any) {
	t.Helper()
	var b io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		b = bytes.NewReader(raw)
	}
	r, _ := http.NewRequest(method, "http://"+s.Address()+path, b)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	c := http.Client{Timeout: 10 * time.Second}
	resp, err := c.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var data map[string]any
	if err = json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, data
}
func pair(t *testing.T, s *Server) {
	t.Helper()
	code, _ := call(t, s, "POST", "/api/pair", map[string]string{"mac": "001122334455"}, s.Token())
	if code != 200 {
		t.Fatal(code)
	}
}

func startRequest(t *testing.T, s *Server, speed int) map[string]any {
	_, status := call(t, s, "GET", "/api/status", nil, s.Token())
	return map[string]any{"speed": speed, "seconds": 60, "generation": status["generation"]}
}

func TestStopInvalidatesDelayedStartRequest(t *testing.T) {
	s, sim := fixture(t)
	pair(t, s)
	stale := startRequest(t, s, 15)
	if code, b := call(t, s, "POST", "/api/stop", struct{}{}, s.Token()); code != 200 {
		t.Fatal(b)
	}
	if code, _ := call(t, s, "POST", "/api/start", stale, s.Token()); code != 409 {
		t.Fatal("stale start admitted", code)
	}
	sim.mu.Lock()
	defer sim.mu.Unlock()
	for _, cmd := range sim.commands {
		if cmd == "target-speed-set" {
			t.Fatal("stale start reached firmware")
		}
	}
}

func TestCancelledStartNeverReachesDevice(t *testing.T) {
	s, sim := fixture(t)
	s.mu.Lock()
	d := s.device
	s.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.Start(ctx, 15, 60, d.generation.Load()); err != context.Canceled {
		t.Fatal(err)
	}
	sim.mu.Lock()
	defer sim.mu.Unlock()
	for _, cmd := range sim.commands {
		if cmd == "target-speed-set" {
			t.Fatal("cancelled start reached firmware")
		}
	}
}

func TestPairingAuthorizationAndCap(t *testing.T) {
	s, _ := fixture(t)
	if code, _ := call(t, s, "GET", "/api/status", nil, "bad"); code != 401 {
		t.Fatal(code)
	}
	if code, _ := call(t, s, "POST", "/api/start", startRequest(t, s, 15), s.Token()); code != 403 {
		t.Fatal(code)
	}
	pair(t, s)
	if code, _ := call(t, s, "POST", "/api/start", startRequest(t, s, 16), s.Token()); code != 400 {
		t.Fatal(code)
	}
	code, status := call(t, s, "GET", "/api/status", nil, s.Token())
	if code != 200 || status["paired"] != true {
		t.Fatal(status)
	}
	raw, _ := json.Marshal(status)
	if bytes.Contains(raw, []byte("private-script-token")) || bytes.Contains(raw, []byte("synthetic-secret")) {
		t.Fatal("secret exposed")
	}
}

func TestLeaseNoRepeatedSetterAndStopRetainsSetpoint(t *testing.T) {
	s, sim := fixture(t)
	pair(t, s)
	code, body := call(t, s, "POST", "/api/start", startRequest(t, s, 15), s.Token())
	if code != 200 {
		t.Fatal(body)
	}
	epoch := body["controlEpoch"].(string)
	for i := 0; i < 4; i++ {
		time.Sleep(550 * time.Millisecond)
		code, b := call(t, s, "POST", "/api/keepalive", map[string]string{"controlEpoch": epoch}, s.Token())
		if code != 200 {
			t.Fatal(b)
		}
	}
	sim.mu.Lock()
	count := 0
	for _, c := range sim.commands {
		if c == "target-speed-set" {
			count++
		}
	}
	sim.mu.Unlock()
	if count != 1 {
		t.Fatalf("speed sent %d times", count)
	}
	if code, b := call(t, s, "POST", "/api/stop", struct{}{}, s.Token()); code != 200 {
		t.Fatal(b)
	}
	if code, _ := call(t, s, "POST", "/api/keepalive", map[string]string{"controlEpoch": epoch}, s.Token()); code != 409 {
		t.Fatal(code)
	}
	_, state := call(t, s, "GET", "/api/state", nil, s.Token())
	if state["rawOperationalMode"] != float64(44) || state["targetSpeed"] != float64(15) {
		t.Fatal(state)
	}
}

func TestLeaseExpiryStopsWithoutUI(t *testing.T) {
	s, sim := fixture(t)
	pair(t, s)
	code, b := call(t, s, "POST", "/api/start", startRequest(t, s, 15), s.Token())
	if code != 200 {
		t.Fatal(b)
	}
	time.Sleep(2400 * time.Millisecond)
	sim.mu.Lock()
	mode := sim.mode
	sim.mu.Unlock()
	if mode != 44 {
		t.Fatal("lease did not stop device")
	}
	if code, _ := call(t, s, "POST", "/api/keepalive", map[string]string{"controlEpoch": b["controlEpoch"].(string)}, s.Token()); code != 409 {
		t.Fatal(code)
	}
}

func TestWrongUUIDCannotAcknowledge(t *testing.T) {
	s, sim := fixture(t)
	sim.mu.Lock()
	sim.wrongID = true
	sim.mu.Unlock()
	code, _ := call(t, s, "GET", "/api/state", nil, s.Token())
	if code != 502 {
		t.Fatal(code)
	}
	sim.mu.Lock()
	sim.wrongID = false
	sim.mu.Unlock()
}

func TestDisconnectDoesNotResume(t *testing.T) {
	s, sim := fixture(t)
	pair(t, s)
	sim.conn.Close()
	time.Sleep(30 * time.Millisecond)
	code, _ := call(t, s, "POST", "/api/start", startRequest(t, s, 15), s.Token())
	if code != 409 {
		t.Fatal(code)
	}
}

func query(name string, typ uint16) []byte {
	b := []byte{1, 2, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0}
	for _, p := range strings.Split(name, ".") {
		b = append(b, byte(len(p)))
		b = append(b, p...)
	}
	return append(b, 0, byte(typ>>8), byte(typ), 0, 1)
}
func TestLocalDNS(t *testing.T) {
	ip := net.ParseIP("192.168.1.50")
	for _, name := range []string{"latency.autoblowapi.com", "eu-central-1.autoblowapi.com", "AUTOBLOWAPI.COM"} {
		a := dnsAnswer(query(name, 1), ip)
		if a == nil || binary.BigEndian.Uint16(a[6:8]) != 1 || !bytes.Equal(a[len(a)-4:], ip.To4()) {
			t.Fatal(name, a)
		}
	}
	if a := dnsAnswer(query("example.org", 1), ip); a[3] != 3 {
		t.Fatal(a)
	}
	if a := dnsAnswer(query("latency.autoblowapi.com", 28), ip); a[7] != 0 || a[3] != 0 {
		t.Fatal(a)
	}
	if dnsAnswer([]byte{1, 2, 3}, ip) != nil {
		t.Fatal("accepted truncated DNS")
	}
}

func TestDNSTCPAndUDP(t *testing.T) {
	d, err := startDNS("127.0.0.1:0", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	defer d.close()
	q := query("latency.autoblowapi.com", 1)
	u, err := net.Dial("udp", d.udp.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	u.SetDeadline(time.Now().Add(time.Second))
	u.Write(q)
	b := make([]byte, 512)
	n, err := u.Read(b)
	if err != nil || n < 16 || b[7] != 1 {
		t.Fatal(err, n)
	}
	c, err := net.Dial("tcp", d.tcp.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Second))
	c.Write(append([]byte{0, byte(len(q))}, q...))
	var length [2]byte
	if _, err = io.ReadFull(c, length[:]); err != nil {
		t.Fatal(err)
	}
	n = int(binary.BigEndian.Uint16(length[:]))
	if _, err = io.ReadFull(c, b[:n]); err != nil || b[7] != 1 {
		t.Fatal(err)
	}
}
