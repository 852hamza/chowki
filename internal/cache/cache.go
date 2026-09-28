package cache

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/852hamza/chowki/internal/secretbox"
	"github.com/852hamza/chowki/internal/store"
)

// Modes of the cache, for a key or the configuration.
const (
	ModeOff   = "off"
	ModeExact = "exact"
)

// Statuses of a request in the cache, which the x-chowki-cache response
// header shows.
const (
	StatusHit    = "hit"
	StatusMiss   = "miss"
	StatusBypass = "bypass"
)

// Store keeps the cache entries; store.Store implements it.
type Store interface {
	CacheEntry(ctx context.Context, hash []byte, now time.Time) (store.CacheEntry, error)
	PutCacheEntry(ctx context.Context, e store.CacheEntry) (replaced int64, err error)
	TouchCacheEntry(ctx context.Context, hash []byte, now time.Time) error
	DeleteExpiredCacheEntries(ctx context.Context, now time.Time) (freed int64, err error)
	EvictCacheEntries(ctx context.Context, size int64) (freed int64, err error)
	CacheSize(ctx context.Context) (int64, error)
}

// Options configure a cache.
type Options struct {
	// Default is the mode of keys that don't have their own: ModeOff or
	// ModeExact.
	Default string
	// TTL is how long a response is served.
	TTL time.Duration
	// MaxBytes limits the total size of the entries; 0 turns the cache off.
	MaxBytes int64
}

// Response is a cached response.
type Response struct {
	Body        []byte
	ContentType string
	// CostUSD is what the request that got the response cost; nil when the
	// model was unpriced.
	CostUSD *float64
}

// Background writing. A full queue drops writes: a missed write only costs
// a later hit, and a request must never wait for the cache.
const (
	writeQueue     = 1024
	writeTimeout   = 10 * time.Second
	expireInterval = time.Minute
)

// Cache is the exact response cache. It keeps successful responses, sealed
// with a key derived from the master key, and serves them again for the
// same request until they expire. When the entries outgrow the limit, the
// least recently used go. Writes happen in the background.
type Cache struct {
	store  Store
	box    *secretbox.Box
	opts   Options
	logger *slog.Logger

	mu     sync.RWMutex // guards closed against writes racing with Close
	closed bool
	writes chan write
	done   chan struct{}
	size   int64 // the total size of the entries; only the writer uses it
}

// write stores a response, or counts a hit when resp is nil.
type write struct {
	key  [32]byte
	resp *Response
	at   time.Time
}

// New returns a cache that keeps its entries in st and seals them with
// box, and starts its writer. Close stops it.
func New(ctx context.Context, st Store, box *secretbox.Box, opts Options, logger *slog.Logger) (*Cache, error) {
	size, err := st.CacheSize(ctx)
	if err != nil {
		return nil, err
	}
	c := &Cache{store: st, box: box, opts: opts, logger: logger, size: size, writes: make(chan write, writeQueue),
		done: make(chan struct{})}
	go c.run()
	return c, nil
}

// On reports whether a request uses the cache, given the mode of its key,
// "" for the default, and the request's x-chowki-cache header: "on",
// "off", or "" to follow the key.
func (c *Cache) On(keyMode, header string) bool {
	if c.opts.MaxBytes <= 0 {
		return false
	}
	switch header {
	case "on":
		return true
	case "off":
		return false
	}
	if keyMode == "" {
		keyMode = c.opts.Default
	}
	return keyMode == ModeExact
}

// Get returns the response stored for the request with key, unless it
// expired by now, and counts the hit.
func (c *Cache) Get(ctx context.Context, key [32]byte, now time.Time) (Response, bool) {
	e, err := c.store.CacheEntry(ctx, key[:], now)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) && ctx.Err() == nil {
			c.logger.Warn("read the exact cache", "error", err)
		}
		return Response{}, false
	}
	body, err := c.box.Open(e.Ciphertext, key[:])
	if err != nil {
		// The next response to the request replaces the entry.
		c.logger.Warn("a cache entry doesn't open; the master key may have changed", "error", err)
		return Response{}, false
	}
	c.enqueue(write{key: key, at: now})
	return Response{Body: body, ContentType: e.ContentType, CostUSD: e.CostUSD}, true
}

// Put stores the response to the request with key, received at now, in
// the background. A response larger than the whole cache isn't stored.
func (c *Cache) Put(key [32]byte, r Response, now time.Time) {
	if int64(len(r.Body)) >= c.opts.MaxBytes {
		return
	}
	c.enqueue(write{key: key, resp: &r, at: now})
}

func (c *Cache) enqueue(w write) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return
	}
	select {
	case c.writes <- w:
	default:
		c.logger.Warn("the exact cache is busy; a write was dropped")
	}
}

// Close finishes the queued writes and stops the writer.
func (c *Cache) Close() {
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		close(c.writes)
	}
	c.mu.Unlock()
	<-c.done
}

func (c *Cache) run() {
	defer close(c.done)
	ticker := time.NewTicker(expireInterval)
	defer ticker.Stop()
	for {
		select {
		case w, ok := <-c.writes:
			if !ok {
				return
			}
			c.apply(w)
		case now := <-ticker.C:
			c.expire(now)
		}
	}
}

func (c *Cache) apply(w write) {
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	if w.resp == nil {
		if err := c.store.TouchCacheEntry(ctx, w.key[:], w.at); err != nil {
			c.logger.Warn("count a cache hit", "error", err)
		}
		return
	}
	e := store.CacheEntry{Hash: w.key[:], Ciphertext: c.box.Seal(w.resp.Body, w.key[:]),
		ContentType: w.resp.ContentType, CostUSD: w.resp.CostUSD, CreatedAt: w.at, ExpiresAt: w.at.Add(c.opts.TTL)}
	replaced, err := c.store.PutCacheEntry(ctx, e)
	if err != nil {
		c.logger.Warn("store a response in the exact cache", "error", err)
		return
	}
	c.size += e.Size() - replaced
	if c.size > c.opts.MaxBytes {
		// Evict down to 90% of the limit, so that each of the next writes
		// doesn't evict again.
		freed, err := c.store.EvictCacheEntries(ctx, c.size-c.opts.MaxBytes*9/10)
		if err != nil {
			c.logger.Warn("evict from the exact cache", "error", err)
			return
		}
		c.size -= freed
	}
}

func (c *Cache) expire(now time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	freed, err := c.store.DeleteExpiredCacheEntries(ctx, now)
	if err != nil {
		c.logger.Warn("delete expired cache entries", "error", err)
		return
	}
	c.size -= freed
}
