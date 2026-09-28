package doctor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/852hamza/chowki/internal/catalog"
	"github.com/852hamza/chowki/internal/config"
	"github.com/852hamza/chowki/internal/netguard"
	"github.com/852hamza/chowki/internal/pipeline"
	"github.com/852hamza/chowki/internal/providerkeys"
	"github.com/852hamza/chowki/internal/secretbox"
	"github.com/852hamza/chowki/internal/server"
	"github.com/852hamza/chowki/internal/store"
)

// Status is the outcome of a check.
type Status int

// Statuses, from best to worst.
const (
	OK Status = iota
	// Warn is a problem that lets the gateway run, such as a provider key
	// that isn't set.
	Warn
	// Fail stops the gateway from starting.
	Fail
)

func (s Status) String() string {
	switch s {
	case Warn:
		return "warn"
	case Fail:
		return "fail"
	}
	return "ok"
}

// Result is the outcome of one check.
type Result struct {
	// Check names what was checked, such as "config" or "provider openai".
	Check  string
	Status Status
	// Message says what the check found and, for a problem, what to do.
	Message string
}

// Options locate what the checks read.
type Options struct {
	// ConfigPath is the configuration file, such as chowki.yaml.
	ConfigPath string
	// EnvPath is the .env file that chowki serve reads.
	EnvPath string
	// Now dates the check of the catalog's prices.
	Now time.Time
}

// staleAfter is the age from which the catalog's prices may be out of
// date: providers change prices every few months.
const staleAfter = 90 * 24 * time.Hour

// probeTimeout limits the request that asks whether a gateway runs on the
// configured address.
const probeTimeout = 2 * time.Second

// Run runs the checks, in the order in which chowki serve needs what they
// check. When the configuration doesn't load, the checks that need it don't
// run.
func Run(ctx context.Context, opts Options) []Result {
	env, r := checkEnvFile(opts.EnvPath)
	out := []Result{r}
	if env == nil {
		return out
	}
	cfg, err := config.Load(opts.ConfigPath, env)
	if err != nil {
		msg := err.Error()
		if errors.Is(err, fs.ErrNotExist) {
			msg += "; run chowki init to create it, or name another file with --config"
		}
		return append(out, Result{"config", Fail, msg})
	}
	out = append(out, Result{"config", OK, fmt.Sprintf("%s loads, with %s and %s", opts.ConfigPath,
		count(len(cfg.Providers), "provider"), count(len(cfg.Aliases), "alias"))})
	masterKey, r := checkMasterKey(cfg, env)
	out = append(out, r)
	cat, err := catalog.Default()
	if err != nil {
		return append(out, Result{"catalog", Fail, err.Error()})
	}
	in, dbErr := store.InspectSQLite(ctx, cfg.Storage.DSN)
	out = append(out, checkProviders(cfg, cat, in.ProviderKeys, masterKey)...)
	out = append(out, checkCatalog(cat, opts.Now))
	out = append(out, checkDatabase(in, dbErr)...)
	useTLS := cfg.Server.TLSCertFile != ""
	return append(out, checkTLS(cfg.Server, opts.Now), checkListen(ctx, cfg.Server.Listen, useTLS))
}

// checkEnvFile reads the .env file as chowki serve does, and returns the
// environment that it and the process give, or nil if it can't be read.
func checkEnvFile(path string) (func(string) (string, bool), Result) {
	env, err := config.DotEnv(path)
	if err != nil {
		return nil, Result{".env", Fail, err.Error()}
	}
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return env, Result{".env", OK, "none; provider keys come from the environment"}
	case err != nil:
		return env, Result{".env", Fail, err.Error()}
	case !private(info):
		return env, Result{".env", Warn, fmt.Sprintf("%s holds provider keys but is readable by other users; "+
			"run chmod 600 %s", path, path)}
	}
	return env, Result{".env", OK, path + readableByYou()}
}

// checkMasterKey returns the master key, or nil when it can't be loaded.
func checkMasterKey(cfg *config.Config, env func(string) (string, bool)) ([]byte, Result) {
	key, err := secretbox.LoadKey(cfg.Security.MasterKeyFile, env)
	if err != nil {
		return nil, Result{"master key", Fail, err.Error()}
	}
	if v, ok := env(secretbox.MasterKeyEnv); ok && v != "" {
		return key, Result{"master key", OK, "from " + secretbox.MasterKeyEnv}
	}
	return key, Result{"master key", OK, cfg.Security.MasterKeyFile + readableByYou()}
}

// checkProviders checks each provider, and opens its stored key, if it has
// one, with the master key, unless the master key is nil.
func checkProviders(cfg *config.Config, cat *catalog.Catalog, stored map[string][]byte, masterKey []byte) []Result {
	if len(cfg.Providers) == 0 {
		return []Result{{"providers", Fail, "the configuration has no providers; add one under providers"}}
	}
	priced := map[string]int{}
	for _, m := range cat.All() {
		priced[m.Provider]++
	}
	var keys *providerkeys.Keys
	if masterKey != nil {
		keys, _ = providerkeys.New(masterKey) // LoadKey checked the key's size, the only error
	}
	out := make([]Result, 0, len(cfg.Providers))
	for _, p := range cfg.Providers {
		out = append(out, checkProvider(p, priced[p.Name], stored[p.Name], keys))
	}
	return out
}

// checkProvider checks a provider's key, from the environment or stored
// sealed, how the key travels, and whether the catalog prices its models:
// the catalog finds prices by the provider's name.
func checkProvider(p config.Provider, models int, sealed []byte, keys *providerkeys.Keys) Result {
	r := Result{Check: "provider " + p.Name}
	var notes []string
	note := func(s Status, msg string) {
		r.Status = max(r.Status, s)
		notes = append(notes, msg)
	}
	stored := sealed != nil
	switch {
	case p.APIKey.Reveal() != "":
		note(OK, "key in "+p.APIKeyEnv) // it wins over a stored key, as in chowki serve
	case stored && keys == nil:
		note(Warn, "its stored key can't be checked without the master key")
	case stored:
		if _, err := keys.Open(p.Name, sealed); err != nil {
			note(Warn, err.Error())
		} else {
			note(OK, "key stored in the database")
		}
	case p.APIKeyEnv == "":
		note(OK, "needs no key")
	default:
		note(Warn, p.APIKeyEnv+" isn't set, so its requests fail until it is")
	}
	keyed := p.APIKeyEnv != "" || stored
	if u, err := url.Parse(p.BaseURL); err == nil && u.Scheme == "http" && keyed && !local(u.Hostname()) {
		note(Warn, "base_url uses http, so the key crosses the network unencrypted; use https")
	}
	switch {
	case p.FreeTier:
		note(OK, "free tier, so its requests cost $0")
	case models > 0:
		note(OK, count(models, "model")+" priced")
	case !keyed:
		note(OK, "no prices, so its requests cost $0")
	default:
		note(Warn, fmt.Sprintf("the catalog has no prices for a provider named %s, so its requests are "+
			"unpriced and budgets don't limit them", p.Name))
	}
	r.Message = strings.Join(notes, "; ")
	return r
}

// local reports whether host is this machine or on a private network,
// where plain http doesn't cross the internet. A host name counts as
// remote: it's only known once it resolves.
func local(host string) bool {
	return netguard.Policy{AllowPrivate: false}.CheckHost(host) != nil
}

// checkCatalog reports the date of the oldest price in the catalog.
func checkCatalog(cat *catalog.Catalog, now time.Time) Result {
	oldest := ""
	for _, m := range cat.All() {
		if oldest == "" || m.Updated < oldest {
			oldest = m.Updated
		}
	}
	checked, err := time.Parse(time.DateOnly, oldest)
	if err != nil {
		return Result{"catalog", Fail, "the catalog has no models"}
	}
	if now.Sub(checked) > staleAfter {
		return Result{"catalog", Warn, fmt.Sprintf("some prices were checked on %s, and may have changed "+
			"since; a newer Chowki has newer prices", oldest)}
	}
	return Result{"catalog", OK, fmt.Sprintf("%s, with prices checked on %s or later", count(cat.Len(), "model"),
		oldest)}
}

// checkDatabase checks what InspectSQLite found in the database, and
// counts its keys: without a virtual key, no app can use the gateway.
func checkDatabase(in store.Inspection, err error) []Result {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return []Result{{"database", OK, in.Path + " doesn't exist yet; chowki serve creates it"}, noKeys()}
	case err != nil:
		return []Result{{"database", Fail, err.Error()}}
	case in.Path == "":
		return []Result{{"database", Warn, "in memory, so the records are lost when chowki serve stops; " +
			"set storage.dsn to a file"}}
	case in.Version > in.Latest:
		return []Result{{"database", Fail, fmt.Sprintf("%s has schema version %d, but this Chowki knows only "+
			"up to %d; use a newer Chowki", in.Path, in.Version, in.Latest)}}
	}
	db := Result{"database", OK, fmt.Sprintf("%s, schema version %d, up to date", in.Path, in.Version)}
	switch {
	case in.Version == 0:
		db.Message = in.Path + " is empty; chowki serve sets it up"
	case in.Version < in.Latest:
		db.Message = fmt.Sprintf("%s has schema version %d; chowki serve migrates it to %d when it starts, so "+
			"back it up first", in.Path, in.Version, in.Latest)
	}
	if info, err := os.Stat(in.Path); err == nil && !private(info) {
		db = Result{"database", Warn, fmt.Sprintf("%s is readable by other users; run chmod 600 %s", in.Path,
			in.Path)}
	}
	if in.Version < in.Latest {
		return []Result{db} // the keys are counted once the schema is current
	}
	keys := Result{"keys", OK, count(in.Keys, "active virtual key")}
	if in.Keys == 0 {
		keys = noKeys()
	}
	admins := Result{"admin tokens", OK, count(in.AdminTokens, "active admin token")}
	if in.AdminTokens == 0 {
		admins.Message = "none; chowki admin create --name <NAME> makes one for the dashboard and the admin API"
	}
	return []Result{db, keys, admins}
}

func noKeys() Result {
	return Result{"keys", Warn, "no active virtual keys, so no app can use the gateway; create one with " +
		"chowki key create --name <NAME>"}
}

// certWarning is how long before its expiry a certificate gets a warning:
// long enough to renew it.
const certWarning = 14 * 24 * time.Hour

// checkTLS checks the certificate that the gateway serves, if any.
func checkTLS(srv config.Server, now time.Time) Result {
	if srv.TLSCertFile == "" {
		if loopback(srv.Listen) {
			return Result{"tls", OK, "off; only this machine can connect, which needs no HTTPS"}
		}
		return Result{"tls", OK, "off; serve HTTPS, or put a reverse proxy with HTTPS in front, when clients " +
			"connect over a network"}
	}
	pair, err := tls.LoadX509KeyPair(srv.TLSCertFile, srv.TLSKeyFile)
	if err != nil {
		return Result{"tls", Fail, "load the certificate: " + err.Error()}
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return Result{"tls", Fail, "parse the certificate: " + err.Error()}
	}
	names := strings.Join(append(leaf.DNSNames, ipStrings(leaf.IPAddresses)...), ", ")
	until := leaf.NotAfter.UTC().Format(time.DateOnly)
	switch {
	case now.After(leaf.NotAfter):
		return Result{"tls", Fail, fmt.Sprintf("the certificate for %s expired on %s; renew it", names, until)}
	case leaf.NotAfter.Sub(now) < certWarning:
		return Result{"tls", Warn, fmt.Sprintf("the certificate for %s expires on %s; renew it; the gateway "+
			"loads the new files without a restart", names, until)}
	}
	if info, err := os.Stat(srv.TLSKeyFile); err == nil && !private(info) {
		return Result{"tls", Warn, fmt.Sprintf("%s is readable by other users; run chmod 600 %s", srv.TLSKeyFile,
			srv.TLSKeyFile)}
	}
	return Result{"tls", OK, fmt.Sprintf("certificate for %s, valid until %s", names, until)}
}

func ipStrings(ips []net.IP) []string {
	out := make([]string, len(ips))
	for i, ip := range ips {
		out[i] = ip.String()
	}
	return out
}

// checkListen reports whether chowki serve can listen on addr, or already
// does.
func checkListen(ctx context.Context, addr string, useTLS bool) Result {
	ln, err := server.Listen(ctx, addr)
	if err == nil {
		_ = ln.Close() // it was only a test
		return Result{"listen", OK, addr + " is free for chowki serve"}
	}
	if base, ok := runningGateway(ctx, addr, useTLS); ok {
		return Result{"listen", OK, "chowki serve is running at " + base}
	}
	fix := "set server.listen or CHOWKI_SERVER_LISTEN to another address"
	if errors.Is(err, syscall.EADDRINUSE) {
		return Result{"listen", Fail, fmt.Sprintf("another program listens on %s; stop it, or %s", addr, fix)}
	}
	return Result{"listen", Fail, fmt.Sprintf("chowki serve can't listen on %s: %v; %s", addr, err, fix)}
}

// runningGateway reports whether Chowki answers on addr, and at which base
// URL. It asks for /healthz, which answers without a key, and logs nothing,
// with Chowki's request ID header.
func runningGateway(ctx context.Context, addr string, useTLS bool) (string, bool) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", false
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	scheme := "http"
	if useTLS {
		scheme = "https"
	}
	base := scheme + "://" + net.JoinHostPort(host, port)
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/healthz", nil)
	if err != nil {
		return "", false
	}
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: nil, // the address is local; a proxy can't reach it
			// The request only asks who answers, and sends nothing secret,
			// so the certificate, which names a public host, needn't match.
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}, //nolint:gosec // G402
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return "", false
	}
	_ = resp.Body.Close() // only the header matters
	return base, resp.Header.Get(pipeline.RequestIDHeader) != ""
}

// loopback reports whether a listen address takes only this machine.
func loopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return host == "localhost" || ip != nil && ip.IsLoopback()
}

// private reports whether only the owner can read a file. Windows has no
// such permission bits, so it passes there.
func private(info fs.FileInfo) bool {
	return runtime.GOOS == "windows" || info.Mode().Perm()&0o077 == 0
}

func readableByYou() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	return " is readable only by you"
}

// count returns n and a noun, in the plural unless n is 1.
func count(n int, noun string) string {
	switch {
	case n == 1:
		return "1 " + noun
	case strings.HasSuffix(noun, "s"):
		return fmt.Sprintf("%d %ses", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
