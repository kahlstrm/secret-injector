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

// Fixture provides provider-specific setup for shared integration behavior.
type Fixture struct {
	Resolver         resolver
	FallbackResolver resolver
	Ref              func(string) string
	Create           func(context.Context, string, string) error
}

type resolverPath struct {
	name     string
	resolver resolver
}

// Run verifies common found and missing semantics against each provided resolver.
func Run(t *testing.T, fixture Fixture) {
	t.Helper()
	require.NotNil(t, fixture.Resolver)
	require.NotNil(t, fixture.Ref)
	require.NotNil(t, fixture.Create)

	seeds := []struct {
		name  string
		value string
	}{
		{name: "present-1", value: "value-1"},
		{name: "present-2", value: "value-2"},
	}
	expectedValues := make(map[string]string, len(seeds))
	for _, seed := range seeds {
		ref := fixture.Ref(seed.name)
		require.NotEmpty(t, ref)
		require.NoError(t, fixture.Create(t.Context(), ref, seed.value))
		expectedValues[ref] = seed.value
	}
	missingRef := fixture.Ref("missing")
	require.NotEmpty(t, missingRef)
	_, exists := expectedValues[missingRef]
	require.False(t, exists, "missing ref must not match a seeded ref")

	paths := []resolverPath{
		{name: "default", resolver: fixture.Resolver},
	}
	if fixture.FallbackResolver != nil {
		paths = append(paths, resolverPath{name: "fallback", resolver: fixture.FallbackResolver})
	}

	refs := make([]string, 0, len(expectedValues))
	for ref := range expectedValues {
		refs = append(refs, ref)
	}
	sort.Strings(refs)

	for _, path := range paths {
		t.Run(path.name, func(t *testing.T) {
			t.Run("returns found refs", func(t *testing.T) {
				actual, err := path.resolver.Resolve(t.Context(), refs)

				require.NoError(t, err)
				assert.Equal(t, expectedValues, actual)
			})

			t.Run("omits missing ref", func(t *testing.T) {
				values, err := path.resolver.Resolve(t.Context(), []string{missingRef})

				require.NoError(t, err)
				assert.Empty(t, values)
			})

			t.Run("returns found and omits missing ref", func(t *testing.T) {
				requested := append(append([]string(nil), refs...), missingRef)
				actual, err := path.resolver.Resolve(t.Context(), requested)

				require.NoError(t, err)
				assert.Equal(t, expectedValues, actual)
			})
		})
	}
}
