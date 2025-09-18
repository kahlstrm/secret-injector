package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Entry represents a single secret binding in the form "<source>:<ref>".
// For the MVP, only source == "ssm" is supported.
type Entry struct {
	Source string
	Ref    string
}

// Secrets maps environment variable names to secret entries.
type Secrets map[string]Entry

// Config is the top-level configuration structure with a required `secrets` field.
type Config struct {
	Secrets Secrets `json:"secrets"`
}

// ParseValue parses a binding value of the form "<source>:<ref>".
// It trims whitespace, lowercases the source, validates both parts are non-empty,
// and enforces the MVP rule that only the "ssm" source is supported.
func ParseValue(s string) (Entry, error) {
	s = strings.TrimSpace(s)
	i := strings.IndexRune(s, ':')
	if i <= 0 || i >= len(s)-1 { // colon must not be at start or end
		return Entry{}, fmt.Errorf("invalid secret value %q: expected '<source>:<ref>'", s)
	}

	source := strings.ToLower(strings.TrimSpace(s[:i]))
	ref := strings.TrimSpace(s[i+1:])

	if source == "" || ref == "" {
		return Entry{}, errors.New("invalid secret value: empty source or ref")
	}

	if source != "ssm" { // MVP: only SSM supported
		return Entry{}, fmt.Errorf("unsupported source %q: only 'ssm' is supported in MVP", source)
	}

	return Entry{Source: source, Ref: ref}, nil
}

// UnmarshalJSON allows Entry to be represented as a single JSON string
// in the form "<source>:<ref>".
func (e *Entry) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("entry must be a string: %w", err)
	}
	parsed, err := ParseValue(s)
	if err != nil {
		return err
	}
	*e = parsed
	return nil
}

// Load reads and decodes a Config from the provided reader.
// Validation of individual entries is performed via Entry.UnmarshalJSON/ParseValue.
func Load(r io.Reader) (Config, error) {
	var cfg Config
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, err
	}
	if cfg.Secrets == nil {
		return Config{}, errors.New("missing required field 'secrets'")
	}
	return cfg, nil
}

// LoadFile opens and loads a Config from a file path.
// Note: file reading helpers are intentionally kept out of the library layer.
// Callers (e.g., CLI) should open files or handle stdin and pass an io.Reader
// to Load for decoding.
