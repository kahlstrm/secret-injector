package loader

import (
	"context"

	ssm "github.com/kahlstrm/secret-injector/internal/ssm"
)

// Registry maps a source name to its loader.
type Registry map[string]SecretLoader

// New creates a Registry from the provided loaders.
func New(loaders ...SecretLoader) Registry {
	r := make(Registry, len(loaders))
	for _, l := range loaders {
		r.Register(l)
	}
	return r
}

// Register adds or replaces a loader for its Source key.
func (r Registry) Register(l SecretLoader) {
	if l == nil {
		return
	}
	r[l.Source()] = l
}

// Default returns a Registry with the built-in SSM loader using
// AWS default credential and region resolution.
// onWarning is optional and can be nil.
func Default(ctx context.Context, onWarning func(context.Context, string)) (Registry, error) {
	s, err := ssm.NewDefault(ctx, onWarning)
	if err != nil {
		return nil, err
	}
	return New(s), nil
}
