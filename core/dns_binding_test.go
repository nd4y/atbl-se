package core

import (
	"errors"
	"net"
	"syscall"
	"testing"
	"time"
)

func TestDNSEphemeralPairRetriesExcludedUDPAndClosesTCP(t *testing.T) {
	var listeners []net.Listener
	bindTCP := func(network, address string) (net.Listener, error) {
		l, err := net.Listen(network, address)
		if err == nil {
			listeners = append(listeners, l)
		}
		return l, err
	}
	calls := 0
	bindUDP := func(network string, addr *net.UDPAddr) (*net.UDPConn, error) {
		calls++
		if calls == 1 {
			return nil, &net.OpError{Op: "listen", Net: "udp4", Err: syscall.Errno(10013)}
		}
		return net.ListenUDP(network, addr)
	}
	d, err := startDNSBindings("127.0.0.1:0", "127.0.0.1", bindTCP, bindUDP)
	if err != nil {
		t.Fatal(err)
	}
	defer d.close()
	if calls < 2 {
		t.Fatal("excluded UDP port was not retried")
	}
	for _, l := range listeners[:len(listeners)-1] {
		_ = l.(*net.TCPListener).SetDeadline(time.Now().Add(100 * time.Millisecond))
		if _, err := l.Accept(); !errors.Is(err, net.ErrClosed) {
			t.Fatal("TCP socket leaked after UDP reservation", err)
		}
	}
}

func TestDNSFixedPortAndUnrelatedErrorsAreNotRetried(t *testing.T) {
	for _, test := range []struct {
		address string
		failure error
	}{{"127.0.0.1:53", syscall.Errno(10013)}, {"127.0.0.1:0", errors.New("unrelated failure")}} {
		calls := 0
		// The controlled binder uses an ephemeral socket, never real system DNS.
		bindTCP := func(network, address string) (net.Listener, error) { return net.Listen(network, "127.0.0.1:0") }
		bindUDP := func(string, *net.UDPAddr) (*net.UDPConn, error) { calls++; return nil, test.failure }
		_, err := startDNSBindings(test.address, "127.0.0.1", bindTCP, bindUDP)
		if !errors.Is(err, test.failure) || calls != 1 {
			t.Fatal(test.address, err, calls)
		}
	}
}
