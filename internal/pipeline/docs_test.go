package pipeline

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestReferenceListsEveryErrorCode keeps the API reference, which is
// written by hand, in step with the error codes in errors.go and the
// router.
func TestReferenceListsEveryErrorCode(t *testing.T) {
	page, err := os.ReadFile(filepath.Join("..", "..", "docs", "reference", "api.md"))
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, file := range []string{"errors.go", filepath.Join("..", "router", "router.go")} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range regexp.MustCompile(`(?m)^\s*(?:code|Code)\w+\s*=\s*"([a-z_]+)"`).FindAllStringSubmatch(string(src), -1) {
			codes = append(codes, m[1])
		}
	}
	if len(codes) < 10 {
		t.Fatalf("found %d error codes; the pattern no longer matches the source", len(codes))
	}
	for _, code := range codes {
		if !strings.Contains(string(page), "| `"+code+"` |") {
			t.Errorf("docs/reference/api.md doesn't list the error code %s", code)
		}
	}
}
