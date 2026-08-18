package config

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

var (
	sourcePattern  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// Entry represents a single secret binding.
type Entry struct {
	Source   string `json:"source" yaml:"source"`
	Ref      string `json:"ref" yaml:"ref"`
	Optional bool   `json:"optional,omitempty" yaml:"optional,omitempty"`
}

// Secrets maps environment variable names to secret entries.
type Secrets map[string]Entry

// Config is the top-level configuration structure with a required `secrets` field.
type Config struct {
	Secrets Secrets `json:"secrets" yaml:"secrets"`
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

func isValidSource(source string) bool {
	return sourcePattern.MatchString(source)
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
	var trailing any
	if err := dec.Decode(&trailing); err == nil {
		return Config{}, errors.New("multiple YAML documents are not supported")
	} else if !errors.Is(err, io.EOF) {
		return Config{}, err
	}
	if cfg.Secrets == nil {
		return Config{}, errors.New("missing required field 'secrets'")
	}

	for env, entry := range cfg.Secrets {
		if !isValidEnvName(env) {
			return Config{}, fmt.Errorf("invalid environment variable name %q: expected letters or underscore followed by letters, digits, or underscores", env)
		}
		if strings.TrimSpace(entry.Source) == "" {
			return Config{}, fmt.Errorf("empty source for environment variable %q", env)
		}
		if !isValidSource(entry.Source) {
			return Config{}, fmt.Errorf("invalid source %q for environment variable %q: expected lowercase letters, digits, and underscores", entry.Source, env)
		}

		if options.sourceValidator != nil {
			if err := options.sourceValidator(entry.Source); err != nil {
				return Config{}, err
			}
		}

		ref, err := expandRef(entry.Ref, options.vars)
		if err != nil {
			return Config{}, err
		}
		if strings.TrimSpace(ref) == "" {
			return Config{}, fmt.Errorf("empty ref for environment variable %q after template expansion", env)
		}
		entry.Ref = ref
		cfg.Secrets[env] = entry
	}

	return cfg, nil
}

func isValidEnvName(name string) bool {
	return envNamePattern.MatchString(name)
}

// builtinSources is the fallback for callers using Load without a validator. The
// commands pass the registry's own sources instead, so this is not what the CLI
// enforces.
var builtinSources = []string{"aws_ssm", "aws_secretsmanager", "gcp_secretmanager"}

func defaultSourceValidator(source string) error {
	if slices.Contains(builtinSources, source) {
		return nil
	}
	quoted := "'" + strings.Join(builtinSources, "', '") + "'"
	return fmt.Errorf("unsupported source %q: supported sources are %s", source, quoted)
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
