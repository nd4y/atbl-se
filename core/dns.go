package core

import (
	"encoding/binary"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The responder handles one standard uncompressed question, never forwards and
// never reflects arbitrary names. Unrelated domains receive NXDOMAIN. It is a
// device-only DNS service, not a replacement resolver for an entire home LAN.
type dnsService struct {
	udp *net.UDPConn
	tcp net.Listener
	ip  net.IP
}

func startDNS(address, answer string) (*dnsService, error) {
	return startDNSBindings(address, answer, net.Listen, net.ListenUDP)
}

func startDNSBindings(address, answer string, tcpBind func(string, string) (net.Listener, error), udpBind func(string, *net.UDPAddr) (*net.UDPConn, error)) (*dnsService, error) {
	ip := net.ParseIP(answer).To4()
	if ip == nil || !ip.IsPrivate() && !ip.IsLoopback() {
		return nil, errors.New("DNS answer must be a local IPv4 address")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	// TCP and UDP have separate Windows exclusion ranges. For port zero only,
	// choose a fresh pair after a reservation/collision; never change a fixed port.
	var last error
	retriable := func(err error) bool {
		return errors.Is(err, syscall.EADDRINUSE) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.Errno(10013))
	}
	for attempt := 0; attempt < 64; attempt++ {
		candidate := address
		if port == "0" && attempt > 0 {
			candidate = net.JoinHostPort(host, strconv.Itoa(49152+rand.IntN(16384)))
		}
		tcp, err := tcpBind("tcp4", candidate)
		if err != nil {
			if port == "0" && retriable(err) {
				last = err
				continue
			}
			return nil, err
		}
		addr, err := net.ResolveUDPAddr("udp4", tcp.Addr().String())
		if err != nil {
			tcp.Close()
			return nil, err
		}
		udp, err := udpBind("udp4", addr)
		if err != nil {
			tcp.Close()
			last = err
			// 10013 is Winsock WSAEACCES, used for excluded UDP ports.
			if port == "0" && retriable(err) {
				continue
			}
			return nil, err
		}
		d := &dnsService{udp: udp, tcp: tcp, ip: ip}
		go d.serveUDP()
		go d.serveTCP()
		return d, nil
	}
	return nil, last
}
func (d *dnsService) close() { d.udp.Close(); d.tcp.Close() }
func (d *dnsService) serveUDP() {
	b := make([]byte, 1232)
	for {
		n, peer, err := d.udp.ReadFromUDP(b)
		if err != nil {
			return
		}
		if !peer.IP.IsPrivate() && !peer.IP.IsLoopback() {
			continue
		}
		if out := dnsAnswer(b[:n], d.ip); out != nil {
			d.udp.WriteToUDP(out, peer)
		}
	}
}
func (d *dnsService) serveTCP() {
	slots := make(chan struct{}, 8)
	for {
		c, err := d.tcp.Accept()
		if err != nil {
			return
		}
		select {
		case slots <- struct{}{}:
			go func() {
				defer func() { <-slots; c.Close() }()
				c.SetDeadline(time.Now().Add(2 * time.Second))
				var length [2]byte
				if _, err := io.ReadFull(c, length[:]); err != nil {
					return
				}
				n := int(binary.BigEndian.Uint16(length[:]))
				if n > 1232 {
					return
				}
				b := make([]byte, n)
				if _, err := io.ReadFull(c, b); err != nil {
					return
				}
				out := dnsAnswer(b, d.ip)
				if out == nil {
					return
				}
				binary.BigEndian.PutUint16(length[:], uint16(len(out)))
				c.Write(append(length[:], out...))
			}()
		default:
			c.Close()
		}
	}
}
func dnsAnswer(q []byte, ip net.IP) []byte {
	if len(q) < 12 || len(q) > 1232 || q[2]&0xf8 != 0 || binary.BigEndian.Uint16(q[4:6]) != 1 || binary.BigEndian.Uint16(q[6:8]) != 0 {
		return nil
	}
	pos := 12
	var labels []string
	total := 0
	for {
		if pos >= len(q) {
			return nil
		}
		n := int(q[pos])
		pos++
		if n == 0 {
			break
		}
		if n > 63 || pos+n > len(q) {
			return nil
		}
		total += n + 1
		if total > 254 {
			return nil
		}
		labels = append(labels, string(q[pos:pos+n]))
		pos += n
	}
	if pos+4 > len(q) {
		return nil
	}
	end := pos + 4
	name := strings.ToLower(strings.Join(labels, "."))
	typ := binary.BigEndian.Uint16(q[pos : pos+2])
	class := binary.BigEndian.Uint16(q[pos+2 : end])
	out := append([]byte(nil), q[:end]...)
	out[2] = 0x84 | (q[2] & 1)
	out[3] = 0
	for i := 6; i < 12; i++ {
		out[i] = 0
	}
	if class != 1 || name != "autoblowapi.com" && !strings.HasSuffix(name, ".autoblowapi.com") {
		out[3] = 3
		return out
	}
	if typ != 1 {
		return out
	}
	out[7] = 1
	return append(out, append([]byte{0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 30, 0, 4}, ip.To4()...)...)
}
