package pipeline

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/852hamza/chowki/internal/router"
	"github.com/852hamza/chowki/internal/testutil"
)

// errorCodes document the gateway's own error codes, for the table of the
// API reference that TestErrorCodesReference writes. The test checks them
// against the code, so that no code goes undocumented or keeps an old
// status.
var errorCodes = []struct {
	code    string
	status  int
	meaning string
}{
	{codeMissingKey, 401, "The request has no virtual key."},
	{codeInvalidKey, 401, "The virtual key is malformed, unknown or wrong."},
	{codeRevokedKey, 401, "The virtual key was revoked."},
	{codeInvalidRequest, 400, "The body isn't valid JSON, or a field that Chowki reads is invalid."},
	{codeTooLarge, 413, "The body is larger than `server.max_body_mb`."},
	{codeModelNotAllowed, 403, "The key may not use the model."},
	{router.CodeUnknownProvider, 400, "Chowki can't tell which provider serves the model."},
	{router.CodeWrongEndpoint, 400, "The model's provider speaks another API than the endpoint's, and the " +
		"endpoint doesn't translate."},
	{codeUnsupportedOption, 400, "The request is translated for a provider of another API, which can't honor " +
		"an option; `param` names it."},
	{codeSensitiveData, 400, "Redaction in `block` mode found secrets or personal data."},
	{codeRateLimited, 429, "The key reached its limit of requests or tokens per minute."},
	{codeBudgetExceeded, 429, "The key's or its project's monthly budget is used up."},
	{codeProviderKeyMissing, 500, "The provider has no key: the variable that its `api_key_env` names isn't " +
		"set, and no key is stored."},
	{codeUpstreamFailed, 502, "The provider can't be reached."},
	{codeUpstreamTimeout, 504, "The provider didn't answer within `server.upstream_timeout`."},
	{codeInternal, 500, "Chowki failed; its log has the details."},
	{codeNotFound, 404, "The path isn't an endpoint of the gateway."},
}

// statuses are the statuses that the gateway's errors use, by name.
var statuses = map[string]int{"StatusBadRequest": 400, "StatusUnauthorized": 401, "StatusForbidden": 403,
	"StatusNotFound": 404, "StatusRequestEntityTooLarge": 413, "StatusTooManyRequests": 429,
	"StatusInternalServerError": 500, "StatusBadGateway": 502, "StatusGatewayTimeout": 504}

func TestErrorCodesReference(t *testing.T) {
	documented := map[string]int{}
	for _, e := range errorCodes {
		documented[e.code] = e.status
	}
	// Every code constant is documented: those of this package and the
	// router's, which the gateway sends with 400.
	for file, prefix := range map[string]string{"errors.go": "code", filepath.Join("..", "router", "router.go"): "Code"} {
		for name, value := range stringConsts(t, file, prefix) {
			if _, ok := documented[value]; !ok {
				t.Errorf("%s (%q) isn't in errorCodes", name, value)
			}
		}
	}
	// Each code is documented with the status that the code sends it with.
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	codes := stringConsts(t, "errors.go", "code")
	codes["re.Code"] = router.CodeUnknownProvider // the router's codes all go out this way
	use := regexp.MustCompile(`apiError\{http\.(Status\w+), (code\w+|re\.Code)`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range use.FindAllStringSubmatch(string(src), -1) {
			status, ok := statuses[m[1]]
			if !ok {
				t.Errorf("%s: add http.%s to statuses", f, m[1])
				continue
			}
			if got := documented[codes[m[2]]]; got != status {
				t.Errorf("%s: %s goes out with %d, but errorCodes says %d", f, m[2], status, got)
			}
		}
	}
	if documented[router.CodeWrongEndpoint] != http.StatusBadRequest {
		t.Error("the router's codes go out with 400")
	}

	var b strings.Builder
	b.WriteString("\n| Code | Status | Meaning |\n|---|---|---|\n")
	for _, e := range errorCodes {
		fmt.Fprintf(&b, "| `%s` | %d | %s |\n", e.code, e.status, e.meaning)
	}
	b.WriteString("\n")
	testutil.CheckGenerated(t, "docs/reference/api.md", "error-codes", b.String())
}

// stringConsts returns the string constants of a Go file whose names start
// with prefix, by name.
func stringConsts(t *testing.T, file, prefix string) map[string]string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	consts := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range spec.Names {
			if !strings.HasPrefix(name.Name, prefix) || i >= len(spec.Values) {
				continue
			}
			if lit, ok := spec.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				v, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				consts[name.Name] = v
			}
		}
		return true
	})
	if len(consts) == 0 {
		t.Fatalf("%s has no constants that start with %s", file, prefix)
	}
	return consts
}
