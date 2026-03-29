package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// Entry represents a single secret binding in the form "<source>:<ref>".
type Entry struct {
	Source   string
	Ref      string
	Optional bool
}

// Secrets maps environment variable names to secret entries.
type Secrets map[string]Entry

// Config is the top-level configuration structure with a required `secrets` field.
type Config struct {
	Secrets  Secrets  `json:"secrets" yaml:"secrets"`
	Optional []string `json:"optional,omitempty" yaml:"optional,omitempty"`
}

type loadOptions struct {
	vars               map[string]string
	sourceValidator    func(string) error
	hasSourceValidator bool
}

// LoadOption configures optional behavior for Load.
type LoadOption func(*loadOptions)

// WithVars enables {{.VAR}} substitution in refs using the provided values.
func WithVars(vars map[string]string) LoadOption {
	return func(o *loadOptions) {
		o.vars = make(map[string]string, len(vars))
		for k, v := range vars {
			o.vars[k] = v
		}
	}
}

// WithSourceValidator overrides the source validation used by Load.
// Passing nil disables source membership validation.
func WithSourceValidator(validator func(string) error) LoadOption {
	return func(o *loadOptions) {
		o.sourceValidator = validator
		o.hasSourceValidator = true
	}
}

// ParseValue parses a binding value of the form "<source>:<ref>".
// It trims whitespace, lowercases the source, and validates both parts are non-empty.
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

	if !isValidSource(source) {
		return Entry{}, fmt.Errorf("invalid source %q: expected lowercase letters, digits, and underscores", source)
	}

	return Entry{Source: source, Ref: ref}, nil
}

func isValidSource(source string) bool {
	for i, r := range source {
		switch {
		case r >= 'a' && r <= 'z':
		case i > 0 && r >= '0' && r <= '9':
		case i > 0 && r == '_':
		default:
			return false
		}
	}
	return true
}

// UnmarshalJSON allows Entry to be represented as a single JSON string
// in the form "<source>:<ref>".
func (e *Entry) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("entry must be a string: %w", err)
	}
	return e.unmarshalString(s)
}

// UnmarshalYAML allows Entry to be represented as a single YAML string
// in the form "<source>:<ref>".
func (e *Entry) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("entry must be a string: %w", err)
	}
	return e.unmarshalString(s)
}

func (e *Entry) unmarshalString(s string) error {
	parsed, err := ParseValue(s)
	if err != nil {
		return err
	}
	*e = parsed
	return nil
}

// Load reads and decodes a Config from the provided reader.
//
// {{.VAR}} placeholders in refs are resolved from WithVars when provided.
// Missing variables and malformed placeholders return errors.
func Load(r io.Reader, opts ...LoadOption) (Config, error) {
	options := loadOptions{vars: map[string]string{}}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	if !options.hasSourceValidator {
		options.sourceValidator = defaultSourceValidator
	}

	var cfg Config
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, err
	}
	if cfg.Secrets == nil {
		return Config{}, errors.New("missing required field 'secrets'")
	}

	for env, entry := range cfg.Secrets {
		if options.sourceValidator != nil {
			if err := options.sourceValidator(entry.Source); err != nil {
				return Config{}, err
			}
		}

		ref, err := expandRef(entry.Ref, options.vars)
		if err != nil {
			return Config{}, err
		}
		entry.Ref = ref
		cfg.Secrets[env] = entry
	}

	seenOptional := make(map[string]struct{}, len(cfg.Optional))
	for _, env := range cfg.Optional {
		env = strings.TrimSpace(env)
		if env == "" {
			return Config{}, errors.New("optional contains empty environment variable name")
		}
		if _, exists := seenOptional[env]; exists {
			return Config{}, fmt.Errorf("duplicate optional environment variable %q", env)
		}
		entry, exists := cfg.Secrets[env]
		if !exists {
			return Config{}, fmt.Errorf("optional environment variable %q is not defined in secrets", env)
		}
		entry.Optional = true
		cfg.Secrets[env] = entry
		seenOptional[env] = struct{}{}
	}

	return cfg, nil
}

func defaultSourceValidator(source string) error {
	switch source {
	case "aws_ssm", "aws_secretsmanager":
		return nil
	default:
		return fmt.Errorf("unsupported source %q: supported sources are 'aws_ssm' and 'aws_secretsmanager'", source)
	}
}

func expandRef(ref string, vars map[string]string) (string, error) {
	if !strings.Contains(ref, "{{") {
		return ref, nil
	}

	tmpl, err := template.New("ref").Option("missingkey=error").Parse(ref)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.Grow(len(ref))
	if err := tmpl.Execute(&b, vars); err != nil {
		return "", err
	}

	return b.String(), nil
}

// LoadFile opens and loads a Config from a file path.
// Note: file reading helpers are intentionally kept out of the library layer.
// Callers (e.g., CLI) should open files or handle stdin and pass an io.Reader
// to Load for decoding.
