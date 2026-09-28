package server

import (
	"context"
	"errors"
	"net"
	"sync"
	"syscall"
)

// Listen listens on addr as net.Listen does, except that the host localhost
// listens on both loopback addresses, 127.0.0.1 and ::1: clients resolve
// localhost to either, and some try only the first, which a gateway on the
// other one would refuse. A machine without IPv6 gets 127.0.0.1 alone.
func Listen(ctx context.Context, addr string) (net.Listener, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	lc := &net.ListenConfig{}
	if host != "localhost" {
		return lc.Listen(ctx, "tcp", addr)
	}
	v4, err := lc.Listen(ctx, "tcp4", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		return nil, err
	}
	// With port 0, the system picks the port for 127.0.0.1, and ::1 takes
	// the same one.
	_, port, _ = net.SplitHostPort(v4.Addr().String())
	v6, err := lc.Listen(ctx, "tcp6", net.JoinHostPort("::1", port))
	switch {
	case errors.Is(err, syscall.EADDRINUSE):
		// Another program on ::1 would get the requests of the clients
		// that resolve localhost to it.
		_ = v4.Close()
		return nil, err
	case err != nil:
		return v4, nil //nolint:nilerr // without IPv6 there is no ::1, and 127.0.0.1 alone is localhost
	}
	return newDualListener(v4, v6, "localhost:"+port), nil
}

// dualListener accepts the connections of two listeners, as one.
type dualListener struct {
	lns    [2]net.Listener
	addr   net.Addr
	conns  chan accepted
	closed chan struct{}
	once   sync.Once
}

type accepted struct {
	conn net.Conn
	err  error
}

func newDualListener(a, b net.Listener, addr string) *dualListener {
	d := &dualListener{lns: [2]net.Listener{a, b}, addr: namedAddr(addr), conns: make(chan accepted),
		closed: make(chan struct{})}
	for _, ln := range d.lns {
		go d.accept(ln)
	}
	return d
}

// accept hands each connection of ln, or error, to Accept. It waits for
// Accept to take it, so that errors, such as too many open files, come as
// slowly as the server asks for connections.
func (d *dualListener) accept(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		select {
		case d.conns <- accepted{conn, err}:
		case <-d.closed:
			if conn != nil {
				_ = conn.Close()
			}
			return
		}
		if errors.Is(err, net.ErrClosed) {
			return
		}
	}
}

func (d *dualListener) Accept() (net.Conn, error) {
	select {
	case a := <-d.conns:
		return a.conn, a.err
	case <-d.closed:
		return nil, net.ErrClosed
	}
}

func (d *dualListener) Close() error {
	err := net.ErrClosed
	d.once.Do(func() {
		close(d.closed)
		err = errors.Join(d.lns[0].Close(), d.lns[1].Close())
	})
	return err
}

// Addr returns localhost and the port, as the configuration names them.
func (d *dualListener) Addr() net.Addr { return d.addr }

type namedAddr string

func (namedAddr) Network() string  { return "tcp" }
func (a namedAddr) String() string { return string(a) }
