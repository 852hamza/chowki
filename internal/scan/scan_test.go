package scan

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/852hamza/chowki/internal/redact"
	"github.com/852hamza/chowki/internal/testutil"
)

// write creates files under dir, by relative path.
func write(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// Secrets that only the rules for .env files and MCP configurations find:
// they have no known format. Built from pieces, as the fixtures of testutil.
var (
	envSecret = "q8Zp" + "L2vX" + "9rT4" + "mW7k"
	mcpSecret = "n3Bf" + "Q7yR" + "5hK2" + "wT8d"
	mcpToken  = "Tz6p" + "M1eV" + "8cX3" + "rJ9u"
)

// planted is a repository with each secret of testutil in a file of its
// own kind, and returns where each one starts, as path:line:column.
func planted(t *testing.T) (dir string, want map[string]string) {
	t.Helper()
	dir = t.TempDir()
	want = map[string]string{}
	files := map[string]string{}
	for i, s := range testutil.Secrets() {
		if rules[s.Type].PII {
			continue
		}
		name := []string{"src/config.go", "deploy/values.yaml", "notes/setup.md"}[i%3]
		prefix := strings.Repeat("\n", i) + "value: " + s.Context
		files[name] += prefix + s.Value + "\n"
		line := strings.Count(files[name][:len(files[name])-len(s.Value)-1], "\n") + 1
		column := len([]rune(prefix)) - strings.LastIndex(prefix, "\n")
		want[s.Type+"@"+name+":"+itoa(line)+":"+itoa(column)] = s.Type
	}
	files[".env"] = "APP_NAME=shop\nDATABASE_PASSWORD=" + envSecret + "\n"
	want[redact.TypeSecret+"@.env:2:19"] = redact.TypeSecret
	line1 := `{"mcpServers": {"db": {"command": "db-mcp", "env": {"DB_TOKEN": "`
	line2 := `"api": {"type": "http", "url": "https://mcp.example.com", "headers": {"Authorization": "Bearer `
	files[".mcp.json"] = line1 + mcpSecret + `"}},` + "\n" + line2 + mcpToken + `"}}}}` + "\n"
	want[redact.TypeSecret+"@.mcp.json:1:"+itoa(len(line1)+1)] = redact.TypeSecret
	want[redact.TypeSecret+"@.mcp.json:2:"+itoa(len(line2)+1)] = redact.TypeSecret
	write(t, dir, files)
	return dir, want
}

func itoa(n int) string { return strconv.Itoa(n) }

func found(res *Result) map[string]string {
	got := map[string]string{}
	for _, f := range res.Findings {
		got[f.Rule.ID+"@"+f.Path+":"+itoa(f.Line)+":"+itoa(f.Column)] = f.Rule.ID
	}
	return got
}

// AC: the scanner finds every planted secret, where it is.
func TestFindsPlantedSecrets(t *testing.T) {
	dir, want := planted(t)
	res, err := Scan(t.Context(), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := found(res)
	for k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("missed %s", k)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("found %s, which isn't planted", k)
		}
	}
	if res.Files != 5 {
		t.Errorf("scanned %d files, want 5", res.Files)
	}
}

// AC: a clean repository, whose keys are placeholders and references, has
// no findings.
func TestCleanRepository(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"README.md": "Set OPENAI_API_KEY to your key, such as sk-EXAMPLE-key-for-docs, and create a virtual key:\n\n" +
			"    chowki_EXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLE\n\n" +
			"AWS documents AKIAIOSFODNN7EXAMPLE as an example. Redaction finds `-----BEGIN RSA PRIVATE KEY-----` blocks.\n",
		".env.example": "OPENAI_API_KEY=\nANTHROPIC_API_KEY=your-anthropic-key-here\nDB_PASSWORD=<DB_PASSWORD>\n" +
			"SESSION_SECRET=${SESSION_SECRET}\nAPI_TOKEN=changeme\nSHORT_KEY=abc123\nLOG_LEVEL=debug\n",
		".mcp.json": `{"mcpServers": {"github": {"command": "gh-mcp", "env": {"GITHUB_TOKEN": "${GITHUB_TOKEN}"}},` +
			`"api": {"type": "http", "url": "https://mcp.example.com", "headers": {"Authorization": "Bearer ${API_KEY}"}}}}`,
		".vscode/mcp.json": `{"servers": {"x": {"env": {"API_KEY": "${input:api-key}"}}}}`,
		"claude.json":      `{"projects": {"/app": {"mcpServers": {"x": {"env": {"TOKEN": "$TOKEN"}}}}}}`,
		"main.go":          "package main\n\nconst keyVariable = \"OPENAI_API_KEY\"\n\nfunc main() {}\n",
		"package.json":     `{"name": "app", "scripts": {"auth": "node auth.js"}}`,
	})
	res, err := Scan(t.Context(), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 0 {
		var b bytes.Buffer
		_ = WriteText(&b, res)
		t.Errorf("a clean repository has findings:\n%s", b.String())
	}
}

// ~/.claude.json keeps MCP servers for each project.
func TestNestedMCPServers(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{"claude.json": `{"mcpServers": {}, "projects": {"/app": {"mcpServers": ` +
		`{"db": {"env": {"DB_PASSWORD": "` + mcpSecret + `"}}}}}}`})
	res, err := Scan(t.Context(), dir, Options{})
	if err != nil || len(res.Findings) != 1 || res.Findings[0].Rule.ID != redact.TypeSecret {
		t.Errorf("findings = %+v, %v; want the project's server's secret", res, err)
	}
}

func TestPII(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{"contacts.txt": "jane.doe@company.io\n+14155552671\n"})
	for pii, want := range map[bool]int{false: 0, true: 2} {
		res, err := Scan(t.Context(), dir, Options{PII: pii})
		if err != nil || len(res.Findings) != want {
			t.Errorf("PII %v: %d findings, %v; want %d", pii, len(res.Findings), err, want)
		}
	}
}

func TestSkippedFiles(t *testing.T) {
	dir := t.TempDir()
	key := testutil.Secrets()[1].Value // an AWS key
	write(t, dir, map[string]string{
		"image.png":                  "\x89PNG\x00\x00" + key,
		"node_modules/lib/index.js":  key,
		"big.txt":                    key + strings.Repeat("x", 100),
		"src/app.js":                 "const k = '" + key + "';",
		"sub/.git/config":            key,
		"sub/project/settings.local": key,
	})
	res, err := Scan(t.Context(), dir, Options{MaxFileSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range res.Findings {
		paths = append(paths, f.Path)
	}
	if !slices.Equal(paths, []string{"src/app.js", "sub/project/settings.local"}) {
		t.Errorf("findings in %v; want binary, large, dependency and .git files skipped", paths)
	}
}

// In a git work tree, the scan reads what git tracks or would track: an
// ignored .env file only when it's named.
func TestGitWorkTree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git isn't installed")
	}
	dir, _ := planted(t)
	write(t, dir, map[string]string{".gitignore": ".env\n"})
	if out, err := exec.CommandContext(t.Context(), "git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	res, err := Scan(t.Context(), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range res.Findings {
		if f.Path == ".env" {
			t.Errorf("the ignored .env was scanned")
		}
	}
	res, err = Scan(t.Context(), filepath.Join(dir, ".env"), Options{})
	if err != nil || len(res.Findings) != 1 {
		t.Errorf("scanning the .env by name: %+v, %v", res, err)
	}
}

// Reports say where secrets are, never what they are.
func TestReports(t *testing.T) {
	dir, want := planted(t)
	res, err := Scan(t.Context(), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var text, js, sarif bytes.Buffer
	if err := WriteText(&text, res); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(&js, res); err != nil {
		t.Fatal(err)
	}
	if err := WriteSARIF(&sarif, res, Tool{Name: "chowki", Version: "test", InformationURI: "https://example.com"}); err != nil {
		t.Fatal(err)
	}
	for _, s := range testutil.Secrets() {
		for name, out := range map[string]string{"text": text.String(), "json": js.String(), "sarif": sarif.String()} {
			if strings.Contains(out, s.Value) {
				t.Errorf("the %s report shows a %s", name, s.Type)
			}
		}
	}
	for _, secret := range []string{envSecret, mcpSecret, mcpToken} {
		if strings.Contains(text.String()+js.String()+sarif.String(), secret) {
			t.Error("a report shows a generic secret")
		}
	}
	if !strings.Contains(text.String(), "Found "+itoa(len(want))+" secrets in 5 files (5 files scanned).") {
		t.Errorf("text report:\n%s", text.String())
	}

	var doc struct {
		Findings []struct {
			Path string `json:"path"`
			Line int    `json:"line"`
			Type string `json:"type"`
		} `json:"findings"`
		FilesScanned int `json:"files_scanned"`
	}
	if err := json.Unmarshal(js.Bytes(), &doc); err != nil || len(doc.Findings) != len(want) || doc.FilesScanned != 5 {
		t.Errorf("JSON report = %+v, %v", doc, err)
	}
	checkSARIF(t, sarif.Bytes(), len(want))
}

// checkSARIF checks the properties that GitHub code scanning requires, as
// https://docs.github.com/en/code-security/code-scanning/integrating-with-code-scanning/sarif-support-for-code-scanning
// lists them.
func checkSARIF(t *testing.T, data []byte, results int) {
	t.Helper()
	var log struct {
		Schema  string `json:"$schema"`
		Version string `json:"version"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					Name  string `json:"name"`
					Rules []struct {
						ID               string                    `json:"id"`
						ShortDescription struct{ Text string }     `json:"shortDescription"`
						FullDescription  struct{ Text string }     `json:"fullDescription"`
						Help             struct{ Text string }     `json:"help"`
						Properties       struct{ Severity string } `json:"properties"`
					} `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
			Results []struct {
				RuleID    string                 `json:"ruleId"`
				Message   struct{ Text string }  `json:"message"`
				Locations []json.RawMessage      `json:"locations"`
				Prints    map[string]string      `json:"partialFingerprints"`
				Region    map[string]json.Number `json:"-"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(data, &log); err != nil {
		t.Fatal(err)
	}
	if log.Schema != SARIFSchema || log.Version != "2.1.0" || len(log.Runs) != 1 {
		t.Fatalf("SARIF log = %+v", log)
	}
	run := log.Runs[0]
	if run.Tool.Driver.Name != "chowki" || len(run.Tool.Driver.Rules) != len(Rules()) || len(run.Results) != results {
		t.Errorf("run: %d rules, %d results", len(run.Tool.Driver.Rules), len(run.Results))
	}
	for _, r := range run.Tool.Driver.Rules {
		if r.ID == "" || r.ShortDescription.Text == "" || r.FullDescription.Text == "" || r.Help.Text == "" {
			t.Errorf("rule %+v lacks a required property", r)
		}
	}
	prints := map[string]bool{}
	for _, r := range run.Results {
		var loc struct {
			PhysicalLocation struct {
				ArtifactLocation struct{ URI string } `json:"artifactLocation"`
				Region           map[string]int       `json:"region"`
			} `json:"physicalLocation"`
		}
		if len(r.Locations) != 1 || json.Unmarshal(r.Locations[0], &loc) != nil {
			t.Fatalf("result %+v", r)
		}
		reg := loc.PhysicalLocation.Region
		if r.Message.Text == "" || loc.PhysicalLocation.ArtifactLocation.URI == "" || reg["startLine"] == 0 ||
			reg["startColumn"] == 0 || reg["endLine"] == 0 || reg["endColumn"] == 0 || len(r.Prints) != 1 {
			t.Errorf("result %+v lacks a required property: %s", r, r.Locations[0])
		}
		for _, p := range r.Prints {
			if prints[p] {
				t.Errorf("two results have the fingerprint %s", p)
			}
			prints[p] = true
		}
	}
}

func FuzzFind(f *testing.F) {
	f.Add(".env", "API_KEY="+envSecret+"\nexport TOKEN='"+mcpSecret+"'\n")
	f.Add("mcp.json", `{"mcpServers":{"a":{"env":{"TOKEN":"`+mcpSecret+`"},"headers":{"Authorization":"Bearer x"}}}}`)
	f.Add("notes.md", testutil.Secrets()[0].Value)
	f.Fuzz(func(t *testing.T, name, text string) {
		data := []byte(text)
		for _, pii := range []bool{false, true} {
			for _, s := range find(name, data, pii) {
				if s.start < 0 || s.end > len(data) || s.start >= s.end || s.rule == nil {
					t.Fatalf("find(%q) = a span %d-%d of %d bytes", name, s.start, s.end, len(data))
				}
				line, column := position(data, s.start)
				if line < 1 || column < 1 {
					t.Fatalf("position = %d:%d", line, column)
				}
			}
		}
	})
}
