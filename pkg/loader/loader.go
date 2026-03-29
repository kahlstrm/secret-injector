package loader

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	cfgpkg "github.com/kahlstrm/secret-injector/pkg/config"
)

// SecretLoader resolves secret references for a specific source (e.g., "aws_ssm").
// Implementations should prefer efficient batch retrieval and may internally
// fall back to per-item requests when batch operations are not permitted.
type SecretLoader interface {
	// Source returns the source key this loader supports (e.g., "aws_ssm").
	Source() string

	// Resolve takes one or more refs and returns a map of ref -> value.
	// Implementations should not return secret values in wrapped error strings.
	Resolve(ctx context.Context, refs []string) (map[string]string, error)
}

// ErrUnknownSource indicates that a secret references an unsupported source.
var ErrUnknownSource = errors.New("unknown source")

type envBinding struct {
	env      string
	optional bool
}

// ResolveAll loads all secrets from cfg using the provided registry of loaders.
// Returns a map of env var name -> secret value.
func ResolveAll(ctx context.Context, cfg cfgpkg.Config, reg Registry, onWarning func(context.Context, string)) (map[string]string, error) {
	// Group refs by source and track which env vars map to which ref.
	bySource := make(map[string]map[string][]envBinding) // source -> ref -> []env bindings
	for env, entry := range cfg.Secrets {
		if _, ok := reg[entry.Source]; !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnknownSource, entry.Source)
		}
		refBindings, ok := bySource[entry.Source]
		if !ok {
			refBindings = make(map[string][]envBinding)
			bySource[entry.Source] = refBindings
		}
		refBindings[entry.Ref] = append(refBindings[entry.Ref], envBinding{env: env, optional: entry.Optional})
	}

	out := make(map[string]string, len(cfg.Secrets))
	var missingRequired []string

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

		for ref, bindings := range refToEnvs {
			val, ok := values[ref]
			if !ok {
				for _, binding := range bindings {
					if binding.optional {
						if onWarning != nil {
							onWarning(ctx, fmt.Sprintf("optional secret not found for env %q (source %q, ref %q)", binding.env, source, ref))
						}
						continue
					}
					missingRequired = append(missingRequired, fmt.Sprintf("env %q (source %q, ref %q)", binding.env, source, ref))
				}
				continue
			}
			for _, binding := range bindings {
				out[binding.env] = val
			}
		}
	}

	if len(missingRequired) > 0 {
		sort.Strings(missingRequired)
		return nil, fmt.Errorf("missing required secrets: %s", strings.Join(missingRequired, ", "))
	}

	return out, nil
}
