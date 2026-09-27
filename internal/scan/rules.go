package scan

import (
	"bytes"
	"encoding/json"
	"math"
	"path"
	"regexp"
	"strings"

	"github.com/852hamza/chowki/internal/redact"
)

// Rule is a kind of finding, as reports describe it.
type Rule struct {
	ID          string
	Description string
	// Help says what to do about a finding.
	Help string
	// Severity is a score from 0.1 to 10, as GitHub code scanning reads
	// it: above 9.0 is critical, from 7.0 high.
	Severity string
	// PII marks personal data, which scans report only when asked.
	PII bool
}

const (
	rotate = "Revoke the key at its provider and create a new one, then remove it from the file and from the " +
		"repository's history. Keep keys in environment variables or a secret manager."
	piiHelp = "Remove the personal data, or replace it with made-up example data."
)

// rules are the rules, by redaction type.
var rules = map[string]*Rule{}

func init() {
	for _, r := range []*Rule{
		{redact.TypePrivateKey, "A private key.", rotate, "9.5", false},
		{redact.TypeAWSKey, "An AWS access key ID.", rotate, "9.1", false},
		{redact.TypeGitHubToken, "A GitHub token.", rotate, "9.1", false},
		{redact.TypeSlackToken, "A Slack token.", rotate, "9.1", false},
		{redact.TypeAnthropicKey, "An Anthropic API key.", rotate, "9.1", false},
		{redact.TypeOpenAIKey, "An OpenAI API key.", rotate, "9.1", false},
		{redact.TypeGoogleKey, "A Google API key.", rotate, "9.1", false},
		{redact.TypeStripeKey, "A Stripe secret key.", rotate, "9.1", false},
		{redact.TypeChowkiKey, "A Chowki virtual key or admin token.",
			"Revoke it with chowki key revoke or chowki admin revoke, and create a new one. " + rotate, "9.1", false},
		{redact.TypeJWT, "A JSON Web Token.", "Remove the token; if it's still valid, revoke it or rotate the key " +
			"that signed it.", "7.5", false},
		{redact.TypeSecret, "A password or secret assigned to a setting.", rotate, "7.5", false},
		{redact.TypeEmail, "An email address.", piiHelp, "3.0", true},
		{redact.TypeCard, "A payment card number.", piiHelp, "7.0", true},
		{redact.TypeIBAN, "A bank account number (IBAN).", piiHelp, "5.0", true},
		{redact.TypeCNIC, "A Pakistani national identity card number (CNIC).", piiHelp, "5.0", true},
		{redact.TypePKMobile, "A Pakistani mobile number.", piiHelp, "3.0", true},
		{redact.TypePhone, "A phone number.", piiHelp, "3.0", true},
	} {
		rules[r.ID] = r
	}
}

// Rules returns every rule, secrets first.
func Rules() []*Rule {
	ids := []string{redact.TypePrivateKey, redact.TypeAWSKey, redact.TypeGitHubToken, redact.TypeSlackToken,
		redact.TypeAnthropicKey, redact.TypeOpenAIKey, redact.TypeGoogleKey, redact.TypeStripeKey,
		redact.TypeChowkiKey, redact.TypeJWT, redact.TypeSecret, redact.TypeEmail, redact.TypeCard, redact.TypeIBAN,
		redact.TypeCNIC, redact.TypePKMobile, redact.TypePhone}
	out := make([]*Rule, 0, len(ids))
	for _, id := range ids {
		out = append(out, rules[id])
	}
	return out
}

// isEnvFile reports whether a file holds environment settings: .env,
// .env.production, prod.env and the like.
func isEnvFile(name string) bool {
	base := path.Base(name)
	return base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasSuffix(base, ".env")
}

// secretName matches the names of settings that hold secrets.
var secretName = regexp.MustCompile(`(?i)(key|token|secret|passw(or)?d|pwd|credential|auth|bearer|cookie|session)`)

// envLine matches a KEY=VALUE line, with an optional export.
var envLine = regexp.MustCompile(`(?m)^[ \t]*(?:export[ \t]+)?([A-Za-z_][A-Za-z0-9_.]*)[ \t]*=[ \t]*(.*?)[ \t]*$`)

// envSecrets finds the values of .env settings whose names say they're
// secrets and that look like one.
func envSecrets(text string) []span {
	var out []span
	for _, m := range envLine.FindAllStringSubmatchIndex(text, -1) {
		name, start, end := text[m[2]:m[3]], m[4], m[5]
		value := text[start:end]
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value, start, end = value[1:len(value)-1], start+1, end-1
		}
		if secretName.MatchString(name) && secretLike(value) {
			out = append(out, span{start, end, rules[redact.TypeSecret]})
		}
	}
	return out
}

// mcpSecrets finds secret values in the env and headers of the servers of
// an MCP configuration: the mcpServers of Claude Code, Claude Desktop and
// others, or the servers of VS Code, at any depth, as ~/.claude.json keeps
// them for each project. Other JSON files have none.
func mcpSecrets(data []byte) []span {
	var root any
	if json.Unmarshal(data, &root) != nil {
		return nil
	}
	var out []span
	var visit func(v any)
	visit = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for key, child := range v {
				if servers, ok := child.(map[string]any); ok && (key == "mcpServers" || key == "servers") {
					for _, s := range servers {
						out = append(out, serverSecrets(data, s)...)
					}
				}
				visit(child)
			}
		case []any:
			for _, child := range v {
				visit(child)
			}
		}
	}
	visit(root)
	return out
}

// serverSecrets finds secret values in the env and headers of a server.
func serverSecrets(data []byte, server any) []span {
	s, ok := server.(map[string]any)
	if !ok {
		return nil
	}
	var out []span
	for _, field := range []string{"env", "headers"} {
		settings, _ := s[field].(map[string]any)
		for name, v := range settings {
			value, ok := v.(string)
			if !ok || !secretName.MatchString(name) {
				continue
			}
			// A value such as "Bearer <token>" hides the secret after its
			// scheme.
			if scheme, rest, ok := strings.Cut(value, " "); ok && !strings.Contains(rest, " ") && len(scheme) < 10 {
				value = rest
			}
			if secretLike(value) {
				out = append(out, locate(data, value)...)
			}
		}
	}
	return out
}

// locate returns the spans of a value in a JSON file, as JSON encodes it.
func locate(data []byte, value string) []span {
	quoted, _ := json.Marshal(value) // a string always marshals
	needle := quoted[1 : len(quoted)-1]
	var out []span
	for i := 0; ; {
		j := bytes.Index(data[i:], needle)
		if j < 0 {
			return out
		}
		out = append(out, span{i + j, i + j + len(needle), rules[redact.TypeSecret]})
		i += j + len(needle)
	}
}

// placeholders are values that stand for a secret rather than being one.
var placeholders = []string{"example", "changeme", "change-me", "change_me", "placeholder", "your", "xxx",
	"dummy", "redacted", "todo", "<", "${", "$(", "{{", "%"}

// secretLike reports whether a value looks like a real secret: long
// enough, varied enough, and not a reference to a variable or a
// placeholder.
func secretLike(value string) bool {
	if len(value) < 8 || strings.HasPrefix(value, "$") {
		return false
	}
	lower := strings.ToLower(value)
	for _, p := range placeholders {
		if strings.Contains(lower, p) {
			return false
		}
	}
	return entropy(value) >= 3.0
}

// entropy returns the Shannon entropy of a string, in bits per character.
func entropy(s string) float64 {
	counts := map[rune]int{}
	n := 0
	for _, r := range s {
		counts[r]++
		n++
	}
	var h float64
	for _, c := range counts {
		p := float64(c) / float64(n)
		h -= p * math.Log2(p)
	}
	return h
}
