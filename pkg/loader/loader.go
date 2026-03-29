package loader

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	cfgpkg "github.com/kahlstrm/secret-injector/pkg/config"
)

// WarningHandler receives non-fatal warnings encountered during secret resolution.
type WarningHandler func(context.Context, string)

// Resolver resolves one or more secret refs for a single source.
type Resolver interface {
	// Resolve takes one or more refs and returns a map of ref -> value.
	// Implementations should not return secret values in wrapped error strings.
	Resolve(ctx context.Context, refs []string) (map[string]string, error)
}

// Provider builds a resolver for a single config source.
type Provider interface {
	// Source returns the config source this provider supports (for example, "aws_ssm").
	Source() string

	// Build constructs a resolver for Source. The registry calls this lazily on first use.
	Build(ctx context.Context, onWarning WarningHandler) (Resolver, error)
}

// ErrUnknownSource indicates that a secret references an unsupported source.
var ErrUnknownSource = errors.New("unknown source")

type envBinding struct {
	env      string
	optional bool
}

// Registry lazily builds resolvers for the providers it contains.
type Registry struct {
	onWarning WarningHandler

	mu        sync.Mutex
	providers map[string]Provider
	resolvers map[string]Resolver
}

// Validate checks whether cfg references only sources that exist in the registry.
func (r *Registry) Validate(cfg cfgpkg.Config) error {
	for _, entry := range cfg.Secrets {
		if !r.hasSource(entry.Source) {
			return fmt.Errorf("%w: %s", ErrUnknownSource, entry.Source)
		}
	}
	return nil
}

// ResolveAll loads all secrets from cfg using resolvers built from the registry's providers.
// Returns a map of env var name -> secret value.
func (r *Registry) ResolveAll(ctx context.Context, cfg cfgpkg.Config) (map[string]string, error) {
	// Group refs by source and track which env vars map to which ref.
	bySource := make(map[string]map[string][]envBinding) // source -> ref -> []env bindings
	for env, entry := range cfg.Secrets {
		if !r.hasSource(entry.Source) {
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
		resolver, err := r.resolverForSource(ctx, source)
		if err != nil {
			return nil, err
		}

		var refs []string
		for ref := range refToEnvs {
			refs = append(refs, ref)
		}

		values, err := resolver.Resolve(ctx, refs)
		if err != nil {
			return nil, err
		}

		for ref, bindings := range refToEnvs {
			val, ok := values[ref]
			if !ok {
				for _, binding := range bindings {
					if binding.optional {
						if r != nil && r.onWarning != nil {
							r.onWarning(ctx, fmt.Sprintf("optional secret not found for env %q (source %q, ref %q)", binding.env, source, ref))
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

func (r *Registry) hasSource(source string) bool {
	if r == nil {
		return false
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.providers[source]
	return ok
}

func (r *Registry) resolverForSource(ctx context.Context, source string) (Resolver, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnknownSource, source)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if resolver, ok := r.resolvers[source]; ok {
		return resolver, nil
	}

	provider, ok := r.providers[source]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownSource, source)
	}

	resolver, err := provider.Build(ctx, r.onWarning)
	if err != nil {
		return nil, err
	}
	if isNilResolver(resolver) {
		return nil, fmt.Errorf("provider %q returned a nil resolver", source)
	}

	r.resolvers[source] = resolver
	return resolver, nil
}

func isNilResolver(resolver Resolver) bool {
	if resolver == nil {
		return true
	}
	v := reflect.ValueOf(resolver)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
