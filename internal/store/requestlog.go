package store

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// RequestLog saves request records in the background, in batches, so that
// a request never waits for the database.
type RequestLog struct {
	store  Store
	logger *slog.Logger

	mu     sync.RWMutex // guards closed against Add racing with Close
	closed bool
	queue  chan Request
	done   chan struct{}
}

// Batching limits. A full queue means the database can't keep up; Add then
// blocks rather than dropping records, because spend depends on them.
const (
	requestLogQueue    = 4096
	requestLogBatch    = 256
	requestLogInterval = time.Second
)

// NewRequestLog starts a background writer that saves records to st.
func NewRequestLog(st Store, logger *slog.Logger) *RequestLog {
	l := &RequestLog{
		store:  st,
		logger: logger,
		queue:  make(chan Request, requestLogQueue),
		done:   make(chan struct{}),
	}
	go l.run()
	return l
}

// Add queues a record. After Close, it logs and drops the record.
func (l *RequestLog) Add(r Request) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.closed {
		l.logger.Error("request record arrived after shutdown", "request_id", r.ID)
		return
	}
	l.queue <- r
}

// Close saves the queued records and stops the background writer.
func (l *RequestLog) Close() {
	l.mu.Lock()
	if !l.closed {
		l.closed = true
		close(l.queue)
	}
	l.mu.Unlock()
	<-l.done
}

func (l *RequestLog) run() {
	defer close(l.done)
	ticker := time.NewTicker(requestLogInterval)
	defer ticker.Stop()
	batch := make([]Request, 0, requestLogBatch)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := l.store.InsertRequests(ctx, batch); err != nil {
			l.logger.Error("save request records", "count", len(batch), "error", err)
		}
		batch = batch[:0]
	}
	for {
		select {
		case r, ok := <-l.queue:
			if !ok {
				flush()
				return
			}
			batch = append(batch, r)
			if len(batch) == requestLogBatch {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// RunRetention deletes the records of requests older than days, once now
// and then at every interval, until ctx ends.
func RunRetention(ctx context.Context, st Store, days int, interval time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		cutoff := time.Now().AddDate(0, 0, -days)
		if n, err := st.DeleteRequestsBefore(ctx, cutoff); err != nil {
			logger.Error("delete old request records", "error", err)
		} else if n > 0 {
			logger.Info("deleted old request records", "count", n, "retention_days", days)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
