package store

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func countRequests(t *testing.T, s *SQLite) int {
	t.Helper()
	var n int
	if err := s.db.QueryRowContext(t.Context(), "SELECT count(*) FROM requests").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRequestLogSavesEverything(t *testing.T) {
	s := openTest(t)
	var logs bytes.Buffer
	l := NewRequestLog(s, slog.New(slog.NewTextHandler(&logs, nil)))

	const total = requestLogBatch*2 + 10 // two full batches and a partial one
	var wg sync.WaitGroup
	for i := range total {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.Add(Request{ID: fmt.Sprintf("r%d", i), Time: time.Now(), APIFamily: "openai", Status: 200})
		}()
	}
	wg.Wait()
	l.Close()
	l.Close() // a second Close is harmless

	if n := countRequests(t, s); n != total {
		t.Errorf("saved %d records, want %d", n, total)
	}
	l.Add(Request{ID: "late"})
	if !strings.Contains(logs.String(), "request record arrived after shutdown") {
		t.Errorf("a record after Close wasn't reported:\n%s", logs.String())
	}
}

func TestRequestLogFlushesOnInterval(t *testing.T) {
	s := openTest(t)
	l := NewRequestLog(s, slog.New(slog.DiscardHandler))
	defer l.Close()
	l.Add(Request{ID: "r1", Time: time.Now()})

	deadline := time.Now().Add(5 * requestLogInterval)
	for countRequests(t, s) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("a queued record wasn't saved within the flush interval")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRequestLogReportsErrors(t *testing.T) {
	s := openTest(t)
	var logs bytes.Buffer
	l := NewRequestLog(s, slog.New(slog.NewTextHandler(&logs, nil)))
	_ = s.Close() // make InsertRequests fail
	l.Add(Request{ID: "r1"})
	l.Close()
	if !strings.Contains(logs.String(), "save request records") {
		t.Errorf("the failed save wasn't logged:\n%s", logs.String())
	}
}

func TestRunRetention(t *testing.T) {
	s := openTest(t)
	old := Request{ID: "old", Time: time.Now().AddDate(0, 0, -40)}
	recent := Request{ID: "recent", Time: time.Now().AddDate(0, 0, -5)}
	if err := s.InsertRequests(t.Context(), []Request{old, recent}); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunRetention(ctx, s, 30, time.Hour, slog.New(slog.NewTextHandler(&logs, nil)))
	}()
	deadline := time.Now().Add(5 * time.Second)
	for countRequests(t, s) != 1 {
		if time.Now().After(deadline) {
			t.Fatal("the old record wasn't deleted")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if !strings.Contains(logs.String(), "count=1") {
		t.Errorf("the deletion wasn't logged:\n%s", logs.String())
	}
}
