package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeCert writes a self-signed certificate for 127.0.0.1 with serial to
// cert.pem and key.pem in dir, modified at mod, and returns it.
func writeCert(t *testing.T, dir string, serial int64, mod time.Time) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(serial), NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	for name, block := range map[string]*pem.Block{"cert.pem": {Type: "CERTIFICATE", Bytes: der},
		"key.pem": {Type: "EC PRIVATE KEY", Bytes: keyDER}} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func TestRunTLS(t *testing.T) {
	dir := t.TempDir()
	cert := writeCert(t, dir, 1, time.Now())
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	ctx, cancel := context.WithCancel(t.Context())
	errc := make(chan error, 1)
	go func() {
		errc <- Run(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "tls=%t proto=%s", r.TLS != nil, r.Proto)
		}), slog.New(slog.NewTextHandler(&logs, nil)), Options{CertFile: filepath.Join(dir, "cert.pem"),
			KeyFile: filepath.Join(dir, "key.pem")})
	}()
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2: true}}
	var body []byte
	for range 50 { // until the server is up
		var resp *http.Response
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://"+ln.Addr().String()+"/", nil)
		if resp, err = client.Do(req); err == nil {
			body, _ = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || string(body) != "tls=true proto=HTTP/2.0" {
		t.Errorf("HTTPS request: %q, %v", body, err)
	}
	cancel()
	if err := <-errc; err != nil {
		t.Errorf("Run() = %v", err)
	}
	if !strings.Contains(logs.String(), "scheme=https") {
		t.Errorf("logs:\n%s", logs.String())
	}

	// A certificate that doesn't load stops Run before it serves.
	ln2, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln2.Close() }()
	for _, opts := range []Options{{CertFile: filepath.Join(dir, "missing.pem"), KeyFile: filepath.Join(dir, "key.pem")},
		{CertFile: filepath.Join(dir, "key.pem"), KeyFile: filepath.Join(dir, "key.pem")},
		{CertFile: filepath.Join(dir, "cert.pem")}} {
		if err := Run(t.Context(), ln2, http.NotFoundHandler(), slog.New(slog.DiscardHandler), opts); err == nil {
			t.Errorf("Run(%+v) succeeded", opts)
		}
	}
}

func TestCertificateReload(t *testing.T) {
	dir := t.TempDir()
	start := time.Now()
	writeCert(t, dir, 1, start.Add(-time.Hour))
	var logs bytes.Buffer
	c, err := loadCertificate(filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem"),
		slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	now := start
	c.now = func() time.Time { return now }
	c.checked = now
	serial := func() int64 {
		t.Helper()
		got, err := c.get(nil)
		if err != nil {
			t.Fatal(err)
		}
		leaf, err := x509.ParseCertificate(got.Certificate[0])
		if err != nil {
			t.Fatal(err)
		}
		return leaf.SerialNumber.Int64()
	}

	writeCert(t, dir, 2, start) // renewed
	if got := serial(); got != 1 {
		t.Errorf("before the check interval, serial %d, want 1", got)
	}
	now = now.Add(certCheckInterval)
	if got := serial(); got != 2 || !strings.Contains(logs.String(), "loaded the renewed TLS certificate") {
		t.Errorf("after the check interval, serial %d, want 2; logs:\n%s", got, logs.String())
	}

	// A renewal that doesn't load keeps the certificate in service.
	if err := os.WriteFile(filepath.Join(dir, "cert.pem"), []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	later := start.Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "cert.pem"), later, later); err != nil {
		t.Fatal(err)
	}
	now = now.Add(certCheckInterval)
	if got := serial(); got != 2 || !strings.Contains(logs.String(), "serving the one loaded before") {
		t.Errorf("after a bad renewal, serial %d, want 2; logs:\n%s", got, logs.String())
	}
}

// The read timeout stops a client that sends its body too slowly, but not
// a response that streams for longer.
func TestReadTimeout(t *testing.T) {
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	readErrs := make(chan error, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			readErrs <- err
			return
		}
		for i := range 3 {
			fmt.Fprintf(w, "chunk %d\n", i)
			w.(http.Flusher).Flush()
			time.Sleep(150 * time.Millisecond)
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		_ = Run(ctx, ln, handler, slog.New(slog.DiscardHandler), Options{ReadTimeout: 200 * time.Millisecond})
	}()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+ln.Addr().String()+"/",
		strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || string(body) != "chunk 0\nchunk 1\nchunk 2\n" {
		t.Errorf("a stream longer than the read timeout: %q, %v", body, err)
	}

	conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	fmt.Fprintf(conn, "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 10\r\n\r\nhalf")
	select {
	case err := <-readErrs:
		if !strings.Contains(err.Error(), "timeout") {
			t.Errorf("the slow body failed with %v, want a timeout", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("a slow body wasn't cut off")
	}
	_, _ = bufio.NewReader(conn).ReadString('\n')
}

// Over HTTP/2 too, the read timeout doesn't cut a stream short.
func TestReadTimeoutHTTP2(t *testing.T) {
	dir := t.TempDir()
	cert := writeCert(t, dir, 1, time.Now())
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		for i := range 3 {
			fmt.Fprintf(w, "chunk %d %s\n", i, r.Proto)
			w.(http.Flusher).Flush()
			time.Sleep(150 * time.Millisecond)
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		_ = Run(ctx, ln, handler, slog.New(slog.DiscardHandler), Options{ReadTimeout: 200 * time.Millisecond,
			CertFile: filepath.Join(dir, "cert.pem"), KeyFile: filepath.Join(dir, "key.pem")})
	}()
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots,
		MinVersion: tls.VersionTLS12}, ForceAttemptHTTP2: true}}
	var body []byte
	for range 50 {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://"+ln.Addr().String()+"/",
			strings.NewReader("hello"))
		var resp *http.Response
		if resp, err = client.Do(req); err == nil {
			body, err = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if want := "chunk 0 HTTP/2.0\nchunk 1 HTTP/2.0\nchunk 2 HTTP/2.0\n"; err != nil || string(body) != want {
		t.Errorf("an HTTP/2 stream longer than the read timeout: %q, %v", body, err)
	}
}
