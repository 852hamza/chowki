package scan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// WriteText writes the findings for people: one line each, then a summary.
func WriteText(w io.Writer, res *Result) error {
	var b strings.Builder
	files := map[string]bool{}
	for _, f := range res.Findings {
		fmt.Fprintf(&b, "%s:%d:%d: %s: %s\n", f.Path, f.Line, f.Column, f.Rule.ID, f.Rule.Description)
		files[f.Path] = true
	}
	switch n := len(res.Findings); n {
	case 0:
		fmt.Fprintf(&b, "No secrets found in %s.\n", plural(res.Files, "file"))
	default:
		fmt.Fprintf(&b, "Found %s in %s (%s scanned).\n", plural(n, "secret"), plural(len(files), "file"),
			plural(res.Files, "file"))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// WriteJSON writes the findings as a JSON document.
func WriteJSON(w io.Writer, res *Result) error {
	type finding struct {
		Path        string `json:"path"`
		Line        int    `json:"line"`
		Column      int    `json:"column"`
		EndLine     int    `json:"end_line"`
		EndColumn   int    `json:"end_column"`
		Type        string `json:"type"`
		Description string `json:"description"`
	}
	out := struct {
		Findings     []finding `json:"findings"`
		FilesScanned int       `json:"files_scanned"`
	}{Findings: []finding{}, FilesScanned: res.Files}
	for _, f := range res.Findings {
		out.Findings = append(out.Findings, finding{f.Path, f.Line, f.Column, f.EndLine, f.EndColumn, f.Rule.ID,
			f.Rule.Description})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// Tool describes the scanner in SARIF reports.
type Tool struct {
	Name, Version, InformationURI string
}

// SARIFSchema is the schema of SARIF 2.1.0 that GitHub code scanning wants.
const SARIFSchema = "https://json.schemastore.org/sarif-2.1.0.json"

// WriteSARIF writes the findings as a SARIF 2.1.0 log with the properties
// that GitHub code scanning requires. Paths are relative to the scanned
// directory, which GitHub reads from the repository's root.
func WriteSARIF(w io.Writer, res *Result, tool Tool) error {
	type message struct {
		Text string `json:"text"`
	}
	type rule struct {
		ID               string            `json:"id"`
		Name             string            `json:"name"`
		ShortDescription message           `json:"shortDescription"`
		FullDescription  message           `json:"fullDescription"`
		Help             message           `json:"help"`
		Properties       map[string]any    `json:"properties"`
		DefaultConfig    map[string]string `json:"defaultConfiguration"`
	}
	type region struct {
		StartLine   int `json:"startLine"`
		StartColumn int `json:"startColumn"`
		EndLine     int `json:"endLine"`
		EndColumn   int `json:"endColumn"`
	}
	type location struct {
		PhysicalLocation struct {
			ArtifactLocation struct {
				URI string `json:"uri"`
			} `json:"artifactLocation"`
			Region region `json:"region"`
		} `json:"physicalLocation"`
	}
	type result struct {
		RuleID              string            `json:"ruleId"`
		Level               string            `json:"level"`
		Message             message           `json:"message"`
		Locations           []location        `json:"locations"`
		PartialFingerprints map[string]string `json:"partialFingerprints"`
	}
	var rules []rule
	for _, r := range Rules() {
		level := "error"
		if r.PII {
			level = "warning"
		}
		rules = append(rules, rule{ID: r.ID, Name: r.ID, ShortDescription: message{r.Description},
			FullDescription: message{r.Description}, Help: message{r.Help},
			Properties:    map[string]any{"security-severity": r.Severity, "tags": []string{"security", "secret"}},
			DefaultConfig: map[string]string{"level": level}})
	}
	results := []result{}
	for _, f := range res.Findings {
		var loc location
		loc.PhysicalLocation.ArtifactLocation.URI = f.Path
		loc.PhysicalLocation.Region = region{f.Line, f.Column, f.EndLine, f.EndColumn}
		level := "error"
		if f.Rule.PII {
			level = "warning"
		}
		// The fingerprint names the file, the rule and the occurrence, never
		// the value, whose hash could be guessed for weak secrets.
		sum := sha256.Sum256(fmt.Appendf(nil, "%s\x00%s\x00%d", f.Path, f.Rule.ID, f.Occurrence))
		results = append(results, result{RuleID: f.Rule.ID, Level: level,
			Message:             message{fmt.Sprintf("%s Chowki doesn't show the value.", f.Rule.Description)},
			Locations:           []location{loc},
			PartialFingerprints: map[string]string{"chowkiFindingHash/v1": hex.EncodeToString(sum[:16])}})
	}
	log := map[string]any{
		"$schema": SARIFSchema,
		"version": "2.1.0",
		"runs": []any{map[string]any{
			"tool": map[string]any{"driver": map[string]any{"name": tool.Name, "version": tool.Version,
				"informationUri": tool.InformationURI, "rules": rules}},
			"columnKind": "unicodeCodePoints",
			"results":    results,
		}},
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
}
