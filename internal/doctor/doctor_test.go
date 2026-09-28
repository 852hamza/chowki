package doctor

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/config"
	"github.com/852hamza/chowki/internal/pipeline"
	"github.com/852hamza/chowki/internal/providerkeys"
	"github.com/852hamza/chowki/internal/secretbox"
	"github.com/852hamza/chowki/internal/store"
)

// now is a date when the catalog's prices are recent.
var now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

// setup is a Chowki folder, as chowki init leaves it, with a .env file.
type setup struct {
	dir, config, env, key, db string
}

func newSetup(t *testing.T, providers string) setup {
	t.Helper()
	dir := t.TempDir()
	s := setup{dir: dir, config: filepath.Join(dir, "chowki.yaml"), env: filepath.Join(dir, ".env"),
		key: filepath.Join(dir, "master.key"), db: filepath.Join(dir, "data", "chowki.db")}
	yaml := fmt.Sprintf("server:\n  listen: 127.0.0.1:0\nstorage:\n  dsn: file:%s\nsecurity:\n  master_key_file: %s\n"+
		"providers:\n%s", s.db, s.key, providers)
	write(t, s.config, yaml, 0o600)
	write(t, s.env, "DOCTOR_TEST_KEY=not-a-real-key\n", 0o600)
	if err := secretbox.CreateKeyFile(s.key); err != nil {
		t.Fatal(err)
	}
	st, err := store.OpenSQLite(t.Context(), "file:"+s.db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	p, err := st.EnsureProject(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateKey(t.Context(), store.Key{ProjectID: p.ID, Name: "app", Prefix: "chowki_doctor",
		CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return s
}

func write(t *testing.T, path, data string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
}

const (
	openAI = "  - name: openai\n    type: openai\n    base_url: https://api.openai.com/v1\n" +
		"    api_key_env: DOCTOR_TEST_KEY\n"
	ollama = "  - name: ollama\n    type: openai\n    base_url: http://localhost:11434/v1\n"
)

func (s setup) run(t *testing.T) map[string]Result {
	t.Helper()
	results := Run(t.Context(), Options{ConfigPath: s.config, EnvPath: s.env, Now: now})
	byCheck := map[string]Result{}
	for _, r := range results {
		byCheck[r.Check] = r
	}
	return byCheck
}

// want is the status of a check, and a part of its message.
type want struct {
	status Status
	text   string
}

func check(t *testing.T, got map[string]Result, wants map[string]want) {
	t.Helper()
	for name, w := range wants {
		r, ok := got[name]
		switch {
		case !ok:
			t.Errorf("no %q check; got %v", name, got)
		case r.Status != w.status || !strings.Contains(r.Message, w.text):
			t.Errorf("%s = %s %q, want %s with %q", name, r.Status, r.Message, w.status, w.text)
		}
	}
}

func TestRunHealthy(t *testing.T) {
	s := newSetup(t, openAI+ollama)
	got := s.run(t)
	check(t, got, map[string]want{
		".env":            {OK, s.env},
		"config":          {OK, "with 2 providers and 0 aliases"},
		"master key":      {OK, s.key},
		"provider openai": {OK, "key in DOCTOR_TEST_KEY; "},
		"provider ollama": {OK, "needs no key; no prices, so its requests cost $0"},
		"catalog":         {OK, "with prices checked on"},
		"database":        {OK, "up to date"},
		"keys":            {OK, "1 active virtual key"},
		"admin tokens":    {OK, "none; chowki admin create --name <NAME> makes one"},
		"tls":             {OK, "off; only this machine can connect"},
		"listen":          {OK, "127.0.0.1:0 is free for chowki serve"},
	})
	if len(got) != 11 {
		t.Errorf("got %d checks, want 11: %v", len(got), got)
	}
	if !strings.Contains(got["provider openai"].Message, "models priced") {
		t.Errorf("provider openai = %q, want its priced models", got["provider openai"].Message)
	}
}

func TestRunConfigMissing(t *testing.T) {
	dir := t.TempDir()
	results := Run(t.Context(), Options{ConfigPath: filepath.Join(dir, "chowki.yaml"),
		EnvPath: filepath.Join(dir, ".env"), Now: now})
	if len(results) != 2 {
		t.Fatalf("got %v, want the .env and config checks only", results)
	}
	check(t, map[string]Result{".env": results[0], "config": results[1]}, map[string]want{
		".env":   {OK, "none; provider keys come from the environment"},
		"config": {Fail, "run chowki init to create it"},
	})
}

func TestRunProblems(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no permission bits to check")
	}
	s := newSetup(t, openAI+
		"  - name: anthropic\n    type: anthropic\n    base_url: https://api.anthropic.com\n"+
		"    api_key_env: DOCTOR_TEST_UNSET\n"+
		"  - name: deepseek\n    type: openai\n    base_url: http://api.deepseek.com\n"+
		"    api_key_env: DOCTOR_TEST_KEY\n")
	for _, p := range []string{s.env, s.db} {
		if err := os.Chmod(p, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	check(t, s.run(t), map[string]want{
		".env":               {Warn, "run chmod 600 " + s.env},
		"provider anthropic": {Warn, "DOCTOR_TEST_UNSET isn't set, so its requests fail until it is"},
		"provider deepseek": {Warn, "base_url uses http, so the key crosses the network unencrypted; use " +
			"https; the catalog has no prices for a provider named deepseek"},
		"database": {Warn, "run chmod 600 " + s.db},
	})

	if err := os.Chmod(s.key, 0o644); err != nil {
		t.Fatal(err)
	}
	check(t, s.run(t), map[string]want{"master key": {Fail, "readable by other users"}})
}

func TestRunEnvFileBroken(t *testing.T) {
	s := newSetup(t, openAI)
	write(t, s.env, "NOT A SETTING\n", 0o600)
	results := Run(t.Context(), Options{ConfigPath: s.config, EnvPath: s.env, Now: now})
	if len(results) != 1 || results[0].Status != Fail {
		t.Errorf("got %v, want only a failed .env check", results)
	}
}

func TestProvider(t *testing.T) {
	s := newSetup(t, "  - name: gemini\n    type: gemini\n"+
		"    base_url: https://generativelanguage.googleapis.com\n    api_key_env: DOCTOR_TEST_KEY\n"+
		"    free_tier: true\n")
	check(t, s.run(t), map[string]want{"provider gemini": {OK, "key in DOCTOR_TEST_KEY; free tier, so its " +
		"requests cost $0"}})
}

func TestDatabase(t *testing.T) {
	s := newSetup(t, openAI)
	byCheck := func(results []Result) map[string]Result {
		m := map[string]Result{}
		for _, r := range results {
			m[r.Check] = r
		}
		return m
	}
	st, err := store.OpenSQLite(t.Context(), "file:"+s.db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.RevokeKey(t.Context(), "chowki_doctor", time.Now()); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	check(t, byCheck(checkDatabase(store.InspectSQLite(t.Context(), "file:"+s.db))), map[string]want{
		"keys": {Warn, "no active virtual keys, so no app can use the gateway"}})

	if err := os.Remove(s.db); err != nil {
		t.Fatal(err)
	}
	check(t, byCheck(checkDatabase(store.InspectSQLite(t.Context(), "file:"+s.db))), map[string]want{
		"database": {OK, "doesn't exist yet; chowki serve creates it"},
		"keys":     {Warn, "create one with chowki key create --name <NAME>"},
	})
	if _, err := os.Stat(s.db); err == nil {
		t.Error("the check created the database")
	}

	check(t, byCheck(checkDatabase(store.InspectSQLite(t.Context(), "file::memory:"))), map[string]want{
		"database": {Warn, "in memory"}})

	write(t, s.db, "", 0o600) // created, but never set up
	got := checkDatabase(store.InspectSQLite(t.Context(), "file:"+s.db))
	check(t, byCheck(got), map[string]want{"database": {OK, "is empty; chowki serve sets it up"}})
	if len(got) != 1 {
		t.Errorf("got %v, want no key counts before the schema is current", got)
	}

	st, err = store.OpenSQLite(t.Context(), "file:"+s.db)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	db, err := sql.Open("sqlite", "file:"+s.db)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"DELETE FROM schema_migrations WHERE version > 1",
		"INSERT INTO schema_migrations VALUES (999, 0)"} {
		if _, err := db.ExecContext(t.Context(), q); err != nil {
			t.Fatal(err)
		}
		expect := map[string]want{"database": {OK, "has schema version 1; chowki serve migrates it"}}
		if strings.Contains(q, "999") {
			expect = map[string]want{"database": {Fail, "has schema version 999, but this Chowki knows only"}}
		}
		check(t, byCheck(checkDatabase(store.InspectSQLite(t.Context(), "file:"+s.db))), expect)
	}
	_ = db.Close()
}

func TestCatalogStale(t *testing.T) {
	s := newSetup(t, openAI)
	results := Run(t.Context(), Options{ConfigPath: s.config, EnvPath: s.env, Now: now.AddDate(1, 0, 0)})
	for _, r := range results {
		if r.Check == "catalog" && (r.Status != Warn || !strings.Contains(r.Message, "a newer Chowki has newer prices")) {
			t.Errorf("catalog = %s %q, want a warning", r.Status, r.Message)
		}
	}
}

func TestListen(t *testing.T) {
	chowki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(pipeline.RequestIDHeader, "req_1")
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer chowki.Close()
	other := httptest.NewServer(http.NotFoundHandler())
	defer other.Close()
	addr := func(s *httptest.Server) string { return s.Listener.Addr().String() }

	tests := []struct {
		name, addr string
		want       want
	}{
		{"free", "127.0.0.1:0", want{OK, "is free for chowki serve"}},
		{"chowki", addr(chowki), want{OK, "chowki serve is running at " + chowki.URL}},
		{"another program", addr(other), want{Fail, "another program listens on " + addr(other)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check(t, map[string]Result{"listen": checkListen(t.Context(), tt.addr, false)},
				map[string]want{"listen": tt.want})
		})
	}
}

func TestRunningGatewayHosts(t *testing.T) {
	chowki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(pipeline.RequestIDHeader, "req_1")
	}))
	defer chowki.Close()
	_, port, err := net.SplitHostPort(chowki.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	// A gateway that listens on all addresses answers on the loopback one.
	for _, addr := range []string{":" + port, "0.0.0.0:" + port} {
		if base, ok := runningGateway(t.Context(), addr, false); !ok || base != chowki.URL {
			t.Errorf("runningGateway(%q) = %q, %v; want %q", addr, base, ok, chowki.URL)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, ok := runningGateway(ctx, chowki.Listener.Addr().String(), false); ok {
		t.Error("runningGateway() with a canceled context found a gateway")
	}
}

func TestCount(t *testing.T) {
	for n, want := range map[int]string{0: "0 aliases", 1: "1 alias", 2: "2 aliases"} {
		if got := count(n, "alias"); got != want {
			t.Errorf("count(%d) = %q, want %q", n, got, want)
		}
	}
	if got := count(3, "model"); got != "3 models" {
		t.Errorf("count(3, model) = %q", got)
	}
	for s, want := range map[Status]string{OK: "ok", Warn: "warn", Fail: "fail"} {
		if s.String() != want {
			t.Errorf("Status(%d).String() = %q, want %q", s, s.String(), want)
		}
	}
}

func TestStoredProviderKeys(t *testing.T) {
	s := newSetup(t, openAI+
		"  - name: anthropic\n    type: anthropic\n    base_url: https://api.anthropic.com\n"+
		"    api_key_env: DOCTOR_TEST_UNSET\n"+
		"  - name: gemini\n    type: gemini\n    base_url: https://generativelanguage.googleapis.com\n"+
		"    api_key_env: DOCTOR_TEST_UNSET\n")
	masterKey, err := secretbox.LoadKey(s.key, func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	right, err := providerkeys.New(masterKey)
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := providerkeys.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.OpenSQLite(t.Context(), "file:"+s.db)
	if err != nil {
		t.Fatal(err)
	}
	for name, sealed := range map[string][]byte{"anthropic": right.Seal("anthropic", "stored"),
		"gemini": wrong.Seal("gemini", "stored"), "openai": right.Seal("openai", "stored")} {
		if err := st.SetProviderKey(t.Context(), store.ProviderKey{Provider: name, Sealed: sealed,
			UpdatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	_ = st.Close()
	check(t, s.run(t), map[string]want{
		"provider openai":    {OK, "key in DOCTOR_TEST_KEY"},
		"provider anthropic": {OK, "key stored in the database"},
		"provider gemini": {Warn, "the stored key of gemini doesn't open with this master key; store it again " +
			"with chowki provider set-key gemini"},
	})

	// Without the master key, the stored keys can't be checked.
	if err := os.Remove(s.key); err != nil {
		t.Fatal(err)
	}
	check(t, s.run(t), map[string]want{"provider anthropic": {Warn, "can't be checked without the master key"}})
}

// writeCert writes a self-signed certificate for 127.0.0.1 that expires at
// notAfter to cert.pem and key.pem in dir.
func writeCert(t *testing.T, dir string, notAfter time.Time) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: notAfter.AddDate(0, -3, 0), NotAfter: notAfter,
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)}, DNSNames: []string{"gateway.example.com"}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	write(t, certFile, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), 0o600)
	write(t, keyFile, string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})), 0o600)
	return certFile, keyFile
}

func TestTLS(t *testing.T) {
	for _, tc := range []struct {
		name     string
		notAfter time.Time
		want     want
	}{
		{"valid", now.AddDate(0, 2, 0), want{OK, "certificate for gateway.example.com, 127.0.0.1, valid until 2026-12-01"}},
		{"expiring", now.AddDate(0, 0, 10), want{Warn, "expires on 2026-10-11; renew it"}},
		{"expired", now.AddDate(0, 0, -1), want{Fail, "expired on 2026-09-30"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cert, key := writeCert(t, t.TempDir(), tc.notAfter)
			got := checkTLS(config.Server{TLSCertFile: cert, TLSKeyFile: key}, now)
			check(t, map[string]Result{"tls": got}, map[string]want{"tls": tc.want})
		})
	}
	// Without a certificate, the advice depends on who can connect.
	for listen, text := range map[string]string{
		"localhost:8080": "only this machine can connect", "127.0.0.1:8080": "only this machine can connect",
		"[::1]:8080": "only this machine can connect", ":8080": "serve HTTPS, or put a reverse proxy",
		"0.0.0.0:8080": "serve HTTPS, or put a reverse proxy", "192.168.1.5:8080": "serve HTTPS, or put a reverse proxy",
	} {
		check(t, map[string]Result{"tls": checkTLS(config.Server{Listen: listen}, now)},
			map[string]want{"tls": {OK, text}})
	}

	dir := t.TempDir()
	cert, key := writeCert(t, dir, now.AddDate(0, 2, 0))
	got := checkTLS(config.Server{TLSCertFile: cert, TLSKeyFile: cert}, now)
	check(t, map[string]Result{"tls": got}, map[string]want{"tls": {Fail, "load the certificate"}})
	if runtime.GOOS != "windows" {
		if err := os.Chmod(key, 0o644); err != nil {
			t.Fatal(err)
		}
		got = checkTLS(config.Server{TLSCertFile: cert, TLSKeyFile: key}, now)
		check(t, map[string]Result{"tls": got}, map[string]want{"tls": {Warn, "run chmod 600 " + key}})
	}

	// The listen check finds a gateway that serves HTTPS.
	gw := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(pipeline.RequestIDHeader, "req_1")
	}))
	defer gw.Close()
	check(t, map[string]Result{"listen": checkListen(t.Context(), gw.Listener.Addr().String(), true)},
		map[string]want{"listen": {OK, "chowki serve is running at " + gw.URL}})
}
