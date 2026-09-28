package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path"
	"slices"
	"strings"
	"time"
	"unicode"
)

var (
	// ErrNotFound means that no record matched.
	ErrNotFound = errors.New("not found")
	// ErrExists means that a record with the same unique value exists.
	ErrExists = errors.New("already exists")
)

// Store is the gateway's persistent state. SQLite implements it; the
// interface leaves room for another database later.
type Store interface {
	// EnsureProject returns the project with the given name, creating it if
	// it doesn't exist.
	EnsureProject(ctx context.Context, name string) (Project, error)
	// CreateKey saves a new virtual key and returns it with its ID. It
	// returns ErrExists if a key with the same prefix exists.
	CreateKey(ctx context.Context, k Key) (Key, error)
	// KeyByPrefix returns the key with the given prefix, or ErrNotFound.
	KeyByPrefix(ctx context.Context, prefix string) (Key, error)
	// ListKeys returns every key, oldest first.
	ListKeys(ctx context.Context) ([]Key, error)
	// RevokeKey marks the key with the given prefix as revoked at the given
	// time, unless it's revoked already, and returns it as stored.
	RevokeKey(ctx context.Context, prefix string, at time.Time) (Key, error)
	// UpdateKey changes the settings of the key with the given prefix and
	// returns it as stored, or ErrNotFound.
	UpdateKey(ctx context.Context, prefix string, u KeyUpdate) (Key, error)
	// ListProjects returns every project, by name.
	ListProjects(ctx context.Context) ([]Project, error)
	// UpdateProject changes the settings of the project with the given name
	// and returns it as stored, or ErrNotFound.
	UpdateProject(ctx context.Context, name string, u ProjectUpdate) (Project, error)
	// InsertRequests saves request records in one transaction, and adds
	// their cost to the spend of their keys.
	InsertRequests(ctx context.Context, rs []Request) error
	// SpendByKey returns the spend of each key in a period, a calendar
	// month as Period formats it. Keys without spend are left out.
	SpendByKey(ctx context.Context, period string) ([]KeySpend, error)
	// CacheEntry returns the cache entry with the given hash unless it
	// expired by now, or ErrNotFound.
	CacheEntry(ctx context.Context, hash []byte, now time.Time) (CacheEntry, error)
	// PutCacheEntry saves a cache entry, replacing one with the same hash,
	// and returns the size of the entry it replaced, or 0.
	PutCacheEntry(ctx context.Context, e CacheEntry) (replaced int64, err error)
	// TouchCacheEntry counts a hit on the cache entry with the given hash.
	TouchCacheEntry(ctx context.Context, hash []byte, now time.Time) error
	// DeleteExpiredCacheEntries deletes the cache entries that expired by
	// now, and returns the size it freed.
	DeleteExpiredCacheEntries(ctx context.Context, now time.Time) (freed int64, err error)
	// EvictCacheEntries deletes the least recently used cache entries until
	// it has freed at least size bytes, and returns the size it freed.
	EvictCacheEntries(ctx context.Context, size int64) (freed int64, err error)
	// CacheSize returns the total size of the cache entries.
	CacheSize(ctx context.Context) (int64, error)
	// DeleteRequestsBefore deletes the records of requests that started
	// before t, and returns how many it deleted.
	DeleteRequestsBefore(ctx context.Context, t time.Time) (int64, error)
	// AddAudit records an administrative action.
	AddAudit(ctx context.Context, e AuditEvent) error
	// Ping checks that the database answers.
	Ping(ctx context.Context) error
	// CreateAdminToken saves a new admin token and returns it with its ID,
	// or ErrExists when a token with the same prefix exists.
	CreateAdminToken(ctx context.Context, t AdminToken) (AdminToken, error)
	// AdminTokenByPrefix returns the admin token with the given prefix, or
	// ErrNotFound.
	AdminTokenByPrefix(ctx context.Context, prefix string) (AdminToken, error)
	// ListAdminTokens returns every admin token, oldest first.
	ListAdminTokens(ctx context.Context) ([]AdminToken, error)
	// RevokeAdminToken marks the admin token with the given prefix as
	// revoked at the given time, unless it's revoked already, and returns
	// it as stored.
	RevokeAdminToken(ctx context.Context, prefix string, at time.Time) (AdminToken, error)
	// Totals sums the requests of whole days: the days in UTC that
	// [from, to) touches.
	Totals(ctx context.Context, from, to time.Time) (Totals, error)
	// Breakdown groups the requests of the days in UTC that [from, to)
	// touches by "key", "model" or "day", with the highest cost first, or
	// by date for days.
	Breakdown(ctx context.Context, by string, from, to time.Time) ([]Group, error)
	// RecentRequests returns up to limit records of requests that started
	// before t, newest first.
	RecentRequests(ctx context.Context, before time.Time, limit int) ([]Request, error)
	// ListRequests returns up to f.Limit records of requests that match f,
	// newest first.
	ListRequests(ctx context.Context, f RequestFilter) ([]Request, error)
	// ProviderStats sums up, for each provider, the requests that reached
	// it in [from, to), the most first.
	ProviderStats(ctx context.Context, from, to time.Time) ([]ProviderStat, error)
	// SetProviderKey saves the sealed key of a provider, replacing the one
	// it had.
	SetProviderKey(ctx context.Context, k ProviderKey) error
	// ProviderKeys returns the stored provider keys, by provider name.
	ProviderKeys(ctx context.Context) ([]ProviderKey, error)
	// DeleteProviderKey deletes the stored key of a provider, or returns
	// ErrNotFound when it has none.
	DeleteProviderKey(ctx context.Context, provider string) error
	// Close closes the database.
	Close() error
}

// Project groups virtual keys.
type Project struct {
	ID   int64
	Name string
	// BudgetUSD is the project's monthly budget; 0 means none.
	BudgetUSD float64
	CreatedAt time.Time
}

// ProjectUpdate lists the settings of a project to change; nil fields stay
// as they are.
type ProjectUpdate struct {
	// BudgetUSD is the new monthly budget; 0 removes it.
	BudgetUSD *float64
}

// Key is a stored virtual key. The key itself is never stored, only its
// prefix, which identifies it, and its SHA-256 hash.
type Key struct {
	ID        int64
	ProjectID int64
	// Project is the project name. Reads fill it in; CreateKey ignores it.
	Project string
	Name    string
	Prefix  string
	Hash    [32]byte
	// BudgetUSD is the key's monthly budget; 0 means none.
	BudgetUSD float64
	// ProjectBudgetUSD is the monthly budget of the key's project; 0 means
	// none. Reads fill it in; CreateKey ignores it.
	ProjectBudgetUSD float64
	// RPM and TPM limit the key's requests and tokens per minute; 0 means
	// no limit.
	RPM, TPM int64
	// CacheMode is exact or off; empty follows the configuration.
	CacheMode string
	// RedactionMode is off, mask, block or alert; empty follows the
	// configuration.
	RedactionMode string
	// AllowedModels are the model names, aliases and patterns such as
	// "openai/*" that the key may request; empty allows every model.
	AllowedModels []string
	CreatedAt     time.Time
	// RevokedAt is zero while the key is active.
	RevokedAt time.Time
}

// Revoked reports whether the key is revoked.
func (k Key) Revoked() bool { return !k.RevokedAt.IsZero() }

// KeyUpdate lists the settings of a key to change; nil fields stay as they
// are, and 0 removes a limit.
type KeyUpdate struct {
	BudgetUSD *float64
	RPM, TPM  *int64
	// CacheMode and RedactionMode "" follow the configuration.
	CacheMode, RedactionMode *string
	// AllowedModels empty allows every model.
	AllowedModels *[]string
}

// CheckName validates the name of a key, project or admin token, which
// appears in lists and logs.
func CheckName(s string) error {
	switch {
	case s == "":
		return errors.New("is required")
	case len(s) > 64:
		return errors.New("must be at most 64 characters")
	case strings.IndexFunc(s, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0:
		return errors.New("must contain only printable characters")
	}
	return nil
}

// Validate reports the first setting of u that isn't valid, in words
// that the admin API and the CLI can show.
func (u KeyUpdate) Validate() error {
	switch {
	case u.BudgetUSD != nil && (math.IsNaN(*u.BudgetUSD) || math.IsInf(*u.BudgetUSD, 0) || *u.BudgetUSD < 0):
		return errors.New("the monthly budget must be an amount of 0 or more, such as 50 or 12.5")
	case u.RPM != nil && *u.RPM < 0:
		return errors.New("the limit of requests per minute must be 0 or more")
	case u.TPM != nil && *u.TPM < 0:
		return errors.New("the limit of tokens per minute must be 0 or more")
	case u.CacheMode != nil && !slices.Contains([]string{"", "exact", "off"}, *u.CacheMode):
		return errors.New("the cache mode must be exact, off or default")
	case u.RedactionMode != nil && !slices.Contains([]string{"", "mask", "block", "alert", "off"}, *u.RedactionMode):
		return errors.New("the redaction mode must be mask, block, alert, off or default")
	}
	if u.AllowedModels != nil {
		for _, m := range *u.AllowedModels {
			if _, err := path.Match(m, ""); err != nil || m == "" {
				return fmt.Errorf("%q isn't a valid model or pattern", m)
			}
		}
	}
	return nil
}

// KeySpend is what a key spent in a period.
type KeySpend struct {
	KeyID, ProjectID int64
	USD              float64
}

// Period returns the calendar month in UTC that spend at t counts
// towards, such as "2026-09".
func Period(t time.Time) string { return t.UTC().Format("2006-01") }

// Request is the metadata of one gateway request. It never holds prompts,
// responses or keys.
type Request struct {
	ID        string
	Time      time.Time // when the request arrived
	KeyID     int64
	ProjectID int64
	APIFamily string // openai or anthropic
	Endpoint  string // such as /v1/chat/completions
	Provider  string // provider name from the configuration
	Model     string // model sent upstream
	Stream    bool
	Status    int    // HTTP status sent to the client
	ErrorType string // empty on success
	Latency   time.Duration
	// TTFB is the time until the first response byte; zero if unknown.
	TTFB time.Duration
	// Tokens is nil when the provider reported no usage.
	Tokens *Tokens
	// CostUSD is nil when the model is unpriced.
	CostUSD       *float64
	SavingsUSD    float64
	SavingsMethod string // empty when there are no savings
	// CacheStatus is hit, miss or bypass; empty when the request was
	// rejected before the exact cache.
	CacheStatus string
	// Redactions counts what redaction found, by type; nil when nothing.
	Redactions map[string]int
}

// RequestFilter selects the request records that ListRequests returns. Its
// zero values select every record.
type RequestFilter struct {
	KeyID int64
	// Provider and Model select the requests of one model, as recorded.
	Provider, Model string
	// Status is StatusFailed, StatusSucceeded, or empty for both.
	Status string
	// Before, when set, selects the records after it in the list, newest
	// first: the next page after the one that ended with it.
	Before *RequestCursor
	Limit  int
}

// Request statuses for RequestFilter. A request failed when the gateway
// answered it with a status of 400 or higher.
const (
	StatusFailed    = "failed"
	StatusSucceeded = "succeeded"
)

// RequestCursor is a place in the list of requests: the time and ID of a
// record, which together order the list, even within a millisecond.
type RequestCursor struct {
	Time time.Time
	ID   string
}

// ProviderStat sums up the requests that reached a provider: not the
// answers of the exact cache, nor the requests that the gateway refused.
type ProviderStat struct {
	Provider string
	Requests int64
	// Failed counts the requests that failed because of the provider: it
	// didn't answer, broke off its answer, or answered 429 or 5xx, even
	// after any fallback. Its other 4xx answers reject the request itself.
	Failed int64
	// P50 and P95 are the latencies of the provider's successful requests,
	// the gateway's time included; zero without any.
	P50, P95 time.Duration
}

// CacheEntry is a response in the exact cache.
type CacheEntry struct {
	// Hash identifies the request that the response answers.
	Hash []byte
	// Ciphertext is the response body, sealed so that only the gateway can
	// read it.
	Ciphertext  []byte
	ContentType string
	// CostUSD is the cost of the request that got the response; nil when
	// the model was unpriced.
	CostUSD   *float64
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Size is the size of the entry that counts towards the cache's limit.
func (e CacheEntry) Size() int64 { return int64(len(e.Ciphertext)) }

// Tokens is the token usage of a request. Input counts every prompt token,
// including CacheRead and CacheWrite; Output includes Reasoning.
type Tokens struct {
	Input, Output, CacheRead, CacheWrite, Reasoning int64
}

// AdminToken is a stored admin token. As for virtual keys, only its prefix
// and SHA-256 hash are stored.
type AdminToken struct {
	ID        int64
	Name      string
	Prefix    string
	Hash      [32]byte
	CreatedAt time.Time
	// RevokedAt is zero while the token is active.
	RevokedAt time.Time
}

// Revoked reports whether the token is revoked.
func (t AdminToken) Revoked() bool { return !t.RevokedAt.IsZero() }

// Totals are sums over requests.
type Totals struct {
	Requests int64
	// Errors are the requests that got a status of 400 or more.
	Errors int64
	// Unpriced are the requests with usage but no price, which count as $0.
	Unpriced int64
	CostUSD  float64
	// SavingsUSD are the net savings by method.
	SavingsUSD             map[string]float64
	Tokens                 Tokens
	CacheHits, CacheMisses int64
	// Redactions count the findings of redaction by type.
	Redactions map[string]int64
}

// Group is one row of a breakdown.
type Group struct {
	// ID is a key's prefix, a provider/model, or a day as YYYY-MM-DD;
	// Label is the key's name, and empty otherwise.
	ID, Label                 string
	Requests                  int64
	CostUSD, SavingsUSD       float64
	InputTokens, OutputTokens int64
}

// Breakdown groupings.
const (
	ByKey   = "key"
	ByModel = "model"
	ByDay   = "day"
)

// AuditEvent is an administrative action, such as creating a key.
type AuditEvent struct {
	Time   time.Time
	Actor  string // who acted, such as "cli"
	Action string // such as "key.create"
	Target string // such as a key prefix
	// Details holds extra facts. It must never hold secrets.
	Details map[string]string
}
