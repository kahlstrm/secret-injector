package loader

import (
	"context"
	"errors"
	"fmt"

	cfgpkg "github.com/kahlstrm/secret-injector/pkg/config"
)

// SecretLoader resolves secret references for a specific source (e.g., "ssm").
// Implementations should prefer efficient batch retrieval and may internally
// fall back to per-item requests when batch operations are not permitted.
type SecretLoader interface {
	// Source returns the source key this loader supports (e.g., "ssm").
	Source() string

	// Resolve takes one or more refs and returns a map of ref -> value.
	// Implementations should not return secret values in wrapped error strings.
	Resolve(ctx context.Context, refs []string) (map[string]string, error)
}

// ErrUnknownSource indicates that a secret references an unsupported source.
var ErrUnknownSource = errors.New("unknown source")

// ResolveAll loads all secrets from cfg using the provided registry of loaders.
// Returns a map of env var name -> secret value.
func ResolveAll(ctx context.Context, cfg cfgpkg.Config, reg Registry) (map[string]string, error) {
	// Group refs by source and track which env vars map to which ref.
	bySource := make(map[string]map[string][]string) // source -> ref -> []env
	for env, entry := range cfg.Secrets {
		if _, ok := reg[entry.Source]; !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnknownSource, entry.Source)
		}
		refs, ok := bySource[entry.Source]
		if !ok {
			refs = make(map[string][]string)
			bySource[entry.Source] = refs
		}
		refs[entry.Ref] = append(refs[entry.Ref], env)
	}

	out := make(map[string]string, len(cfg.Secrets))

	// For each source, resolve unique refs once and map results back to env names.
	for source, refToEnvs := range bySource {
		loader := reg[source]

		// Build unique ref list
		var refs []string
		for ref := range refToEnvs {
			refs = append(refs, ref)
		}

		values, err := loader.Resolve(ctx, refs)
		if err != nil {
			return nil, err
		}

		for ref, envs := range refToEnvs {
			val, ok := values[ref]
			if !ok {
				return nil, fmt.Errorf("missing value for ref %q from source %q", ref, source)
			}
			for _, env := range envs {
				out[env] = val
			}
		}
	}

	return out, nil
}
