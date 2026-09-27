package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestStarterIsValid(t *testing.T) {
	data := Starter("https://docs.example.org")
	if !strings.Contains(string(data), "# Documentation: https://docs.example.org\n") {
		t.Errorf("starter file doesn't link the docs:\n%s", data)
	}
	cfg, err := Parse(data, envMap(nil))
	if err != nil {
		t.Fatalf("the starter file doesn't load: %v", err)
	}
	// The starter spells out the defaults, so editing starts from them.
	want := Default()
	got := *cfg
	got.Providers = nil
	if !reflect.DeepEqual(got, want) {
		t.Errorf("starter settings = %+v, want the defaults %+v", got, want)
	}
	if len(cfg.Providers) != 2 {
		t.Errorf("starter has %d providers, want 2", len(cfg.Providers))
	}
}
