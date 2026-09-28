package server

import (
	"errors"
	"net"
	"syscall"
	"testing"
	"time"
)

// hasIPv6 reports whether this machine can listen on ::1.
func hasIPv6(t *testing.T) bool {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp6", "[::1]:0")
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// A gateway on localhost takes connections on both loopback addresses.
func TestListenLocalhost(t *testing.T) {
	ln, err := Listen(t.Context(), "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil || ln.Addr().String() != "localhost:"+port {
		t.Fatalf("Addr() = %q, want localhost:<port>", ln.Addr())
	}
	hosts := []string{"127.0.0.1"}
	if hasIPv6(t) {
		hosts = append(hosts, "::1")
	}
	for _, host := range hosts {
		d := net.Dialer{Timeout: 2 * time.Second}
		c, err := d.DialContext(t.Context(), "tcp", net.JoinHostPort(host, port))
		if err != nil {
			t.Fatalf("dial %s: %v", host, err)
		}
		got, err := ln.Accept()
		if err != nil {
			t.Fatalf("accept from %s: %v", host, err)
		}
		if want := net.ParseIP(host); !got.RemoteAddr().(*net.TCPAddr).IP.Equal(want) {
			t.Errorf("accepted a connection from %s, want %s", got.RemoteAddr(), host)
		}
		_ = got.Close()
		_ = c.Close()
	}

	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ln.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Errorf("Accept after Close = %v, want net.ErrClosed", err)
	}
	if err := ln.Close(); !errors.Is(err, net.ErrClosed) {
		t.Errorf("second Close = %v, want net.ErrClosed", err)
	}
	// Close frees both addresses.
	again, err := Listen(t.Context(), "localhost:"+port)
	if err != nil {
		t.Fatalf("listen again on port %s: %v", port, err)
	}
	_ = again.Close()
}

// When another program holds ::1 on the port, Listen fails rather than let
// it answer the clients that resolve localhost to ::1.
func TestListenLocalhostTaken(t *testing.T) {
	if !hasIPv6(t) {
		t.Skip("no IPv6 on this machine")
	}
	other, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp6", "[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	_, port, _ := net.SplitHostPort(other.Addr().String())
	if ln, err := Listen(t.Context(), "localhost:"+port); !errors.Is(err, syscall.EADDRINUSE) {
		if ln != nil {
			_ = ln.Close()
		}
		t.Fatalf("Listen = %v, want the address in use", err)
	}
	// 127.0.0.1 was released again.
	v4, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp4", "127.0.0.1:"+port)
	if err != nil {
		t.Fatalf("127.0.0.1:%s is still taken: %v", port, err)
	}
	_ = v4.Close()
}

// Other hosts listen as net.Listen does.
func TestListenOtherHosts(t *testing.T) {
	ln, err := Listen(t.Context(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if _, ok := ln.Addr().(*net.TCPAddr); !ok {
		t.Errorf("Addr() = %T, want the plain listener's *net.TCPAddr", ln.Addr())
	}
	if _, err := Listen(t.Context(), "no-port"); err == nil {
		t.Error("Listen without a port succeeded")
	}
}
