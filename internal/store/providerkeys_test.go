package store

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func TestProviderKeys(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	if keys, err := s.ProviderKeys(ctx); err != nil || len(keys) != 0 {
		t.Fatalf("ProviderKeys() of a new database = %v, %v", keys, err)
	}
	first := time.UnixMilli(1790000000000)
	for _, k := range []ProviderKey{{"openai", []byte{1}, first}, {"anthropic", []byte{2}, first},
		{"openai", []byte{3}, first.Add(time.Hour)}} {
		if err := s.SetProviderKey(ctx, k); err != nil {
			t.Fatal(err)
		}
	}
	keys, err := s.ProviderKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0].Provider != "anthropic" || keys[1].Provider != "openai" ||
		!bytes.Equal(keys[1].Sealed, []byte{3}) || !keys[1].UpdatedAt.Equal(first.Add(time.Hour)) {
		t.Errorf("ProviderKeys() = %+v, want anthropic, then openai replaced", keys)
	}
	if err := s.DeleteProviderKey(ctx, "openai"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProviderKey(ctx, "openai"); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteProviderKey() of a deleted key = %v, want ErrNotFound", err)
	}
	if keys, _ := s.ProviderKeys(ctx); len(keys) != 1 {
		t.Errorf("after the delete, ProviderKeys() = %+v", keys)
	}
}
