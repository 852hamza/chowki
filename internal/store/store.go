package store

import (
	"context"
	"errors"
	"time"
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
	// InsertRequests saves request records in one transaction.
	InsertRequests(ctx context.Context, rs []Request) error
	// DeleteRequestsBefore deletes the records of requests that started
	// before t, and returns how many it deleted.
	DeleteRequestsBefore(ctx context.Context, t time.Time) (int64, error)
	// AddAudit records an administrative action.
	AddAudit(ctx context.Context, e AuditEvent) error
	// Close closes the database.
	Close() error
}

// Project groups virtual keys.
type Project struct {
	ID        int64
	Name      string
	CreatedAt time.Time
}

// Key is a stored virtual key. The key itself is never stored, only its
// prefix, which identifies it, and its SHA-256 hash.
type Key struct {
	ID        int64
	ProjectID int64
	// Project is the project name. Reads fill it in; CreateKey ignores it.
	Project   string
	Name      string
	Prefix    string
	Hash      [32]byte
	CreatedAt time.Time
	// RevokedAt is zero while the key is active.
	RevokedAt time.Time
}

// Revoked reports whether the key is revoked.
func (k Key) Revoked() bool { return !k.RevokedAt.IsZero() }

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
}

// Tokens is the token usage of a request. Input counts every prompt token,
// including CacheRead and CacheWrite; Output includes Reasoning.
type Tokens struct {
	Input, Output, CacheRead, CacheWrite, Reasoning int64
}

// AuditEvent is an administrative action, such as creating a key.
type AuditEvent struct {
	Time   time.Time
	Actor  string // who acted, such as "cli"
	Action string // such as "key.create"
	Target string // such as a key prefix
	// Details holds extra facts. It must never hold secrets.
	Details map[string]string
}
