//go:build integration

package resolvercontract

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type resolver interface {
	Resolve(context.Context, []string) (map[string]string, error)
}

// Fixture provides seeded refs and resolvers for shared integration behavior.
type Fixture struct {
	Resolver         resolver
	FallbackResolver resolver
	Values           map[string]string
	MissingRef       string
}

type resolverPath struct {
	name     string
	resolver resolver
}

// Run verifies common found and missing semantics against each provided resolver.
func Run(t *testing.T, fixture Fixture) {
	t.Helper()
	require.NotNil(t, fixture.Resolver)
	require.NotEmpty(t, fixture.Values)
	require.NotEmpty(t, fixture.MissingRef)
	_, exists := fixture.Values[fixture.MissingRef]
	require.False(t, exists, "missing ref must not be seeded")

	paths := []resolverPath{
		{name: "default", resolver: fixture.Resolver},
	}
	if fixture.FallbackResolver != nil {
		paths = append(paths, resolverPath{name: "fallback", resolver: fixture.FallbackResolver})
	}

	refs := make([]string, 0, len(fixture.Values))
	for ref := range fixture.Values {
		refs = append(refs, ref)
	}
	sort.Strings(refs)

	for _, path := range paths {
		t.Run(path.name, func(t *testing.T) {
			t.Run("returns found refs", func(t *testing.T) {
				values, err := path.resolver.Resolve(t.Context(), refs)

				require.NoError(t, err)
				assert.Equal(t, fixture.Values, values)
			})

			t.Run("omits missing ref", func(t *testing.T) {
				values, err := path.resolver.Resolve(t.Context(), []string{fixture.MissingRef})

				require.NoError(t, err)
				assert.Empty(t, values)
			})

			t.Run("returns found and omits missing ref", func(t *testing.T) {
				requested := append(append([]string(nil), refs...), fixture.MissingRef)
				values, err := path.resolver.Resolve(t.Context(), requested)

				require.NoError(t, err)
				assert.Equal(t, fixture.Values, values)
			})
		})
	}
}
