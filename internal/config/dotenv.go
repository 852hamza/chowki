package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// DotEnv returns an environment lookup that reads the real environment
// first and then the .env file at path, so real variables always win. A
// missing file is not an error.
func DotEnv(path string) (func(string) (string, bool), error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return os.LookupEnv, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	vars, err := ParseDotEnv(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return func(name string) (string, bool) {
		if v, ok := os.LookupEnv(name); ok {
			return v, true
		}
		v, ok := vars[name]
		return v, ok
	}, nil
}

// ParseDotEnv parses a .env file: one KEY=VALUE per line, with blank lines,
// "#" comment lines and an optional "export " prefix. A value in single
// quotes is taken literally. A value in double quotes may use the escapes
// \n, \r, \t, \" and \\. An unquoted value is trimmed and ends before a
// " #" comment. Values never expand variables. Errors give the line number
// but never a value, because values are usually secrets.
func ParseDotEnv(data []byte) (map[string]string, error) {
	vars := map[string]string{}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "export"); ok && rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
			line = strings.TrimSpace(rest)
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || !envNameRE.MatchString(key) {
			return nil, fmt.Errorf("line %d: want KEY=VALUE with a variable name such as OPENAI_API_KEY", i+1)
		}
		v, err := parseDotEnvValue(value)
		if err != nil {
			return nil, fmt.Errorf("line %d: %s: %w", i+1, key, err)
		}
		vars[key] = v
	}
	return vars, nil
}

// parseDotEnvValue parses the text after "=".
func parseDotEnvValue(raw string) (string, error) {
	s := strings.TrimLeft(raw, " \t")
	if s == "" {
		return "", nil
	}
	switch s[0] {
	case '\'':
		end := strings.IndexByte(s[1:], '\'')
		if end < 0 {
			return "", errors.New("missing closing single quote")
		}
		return s[1 : end+1], checkTrailing(s[end+2:])
	case '"':
		var b strings.Builder
		for i := 1; i < len(s); i++ {
			switch c := s[i]; c {
			case '"':
				return b.String(), checkTrailing(s[i+1:])
			case '\\':
				if i+1 == len(s) {
					return "", errors.New("missing closing double quote")
				}
				i++
				switch s[i] {
				case 'n':
					b.WriteByte('\n')
				case 'r':
					b.WriteByte('\r')
				case 't':
					b.WriteByte('\t')
				case '"', '\\':
					b.WriteByte(s[i])
				default:
					return "", errors.New(`unknown escape; use \n, \r, \t, \" or \\`)
				}
			default:
				b.WriteByte(c)
			}
		}
		return "", errors.New("missing closing double quote")
	}
	if s[0] == '#' && len(s) < len(raw) {
		return "", nil // only a comment after the "="
	}
	if i := strings.Index(s, " #"); i >= 0 {
		s = s[:i]
	}
	if i := strings.Index(s, "\t#"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s), nil
}

// checkTrailing allows only a comment after a closing quote.
func checkTrailing(s string) error {
	if s = strings.TrimSpace(s); s != "" && !strings.HasPrefix(s, "#") {
		return errors.New("unexpected text after the closing quote")
	}
	return nil
}
