package config

import "log/slog"

const redacted = "[redacted]"

// Secret is a string, such as a provider key, that fmt, slog and text
// encoders print as "[redacted]". Call Reveal to use the value.
type Secret string

// Reveal returns the secret value.
func (s Secret) Reveal() string { return string(s) }

// String implements fmt.Stringer.
func (Secret) String() string { return redacted }

// GoString implements fmt.GoStringer, for %#v.
func (Secret) GoString() string { return `"` + redacted + `"` }

// LogValue implements slog.LogValuer.
func (Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalText implements encoding.TextMarshaler, which JSON and YAML use.
func (Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }
