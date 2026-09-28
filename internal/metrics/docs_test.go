package metrics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReferenceListsEveryMetric keeps the metrics reference, which is
// written by hand, in step with the code.
func TestReferenceListsEveryMetric(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "reference", "metrics.md"))
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := New().Write(&b); err != nil {
		t.Fatal(err)
	}
	types, _, err := parse(b.String())
	if err != nil {
		t.Fatal(err)
	}
	for name := range types {
		if !strings.Contains(string(data), "## `"+name+"`") {
			t.Errorf("docs/reference/metrics.md has no section for %s", name)
		}
	}
}
