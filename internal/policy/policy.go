package policy

import "path"

// AllowsModel reports whether a key whose allowlist is patterns may request
// model, as the client names it; an empty list allows every model. Patterns
// match as path.Match does, so "openai/*" allows every model of the
// provider openai. An alias is allowed only by its own name, not by the
// models it resolves to, so that an administrator can offer a key "fast"
// without offering every model behind it.
func AllowsModel(patterns []string, model string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if ok, _ := path.Match(p, model); ok {
			return true
		}
	}
	return false
}
