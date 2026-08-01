//go:build integration

package resolvercontract

import (
	"context"
	"fmt"
	"sort"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ResolveFunc resolves refs for one contract path.
type ResolveFunc func(context.Context, []string) (map[string]string, error)

// Secret describes synthetic secret data provisioned by an integration fixture.
type Secret struct {
	Ref   string
	Value string
}

// Fixture provides provider-specific setup for shared integration behavior.
type Fixture struct {
	Resolve         ResolveFunc
	FallbackResolve ResolveFunc
	Seed            func(context.Context, Secret) error
}

type resolverPath struct {
	name    string
	resolve ResolveFunc
}

var fixtureSequence atomic.Uint64

// Run verifies common found and missing semantics against each provided resolver.
func Run(t *testing.T, fixture Fixture) {
	t.Helper()
	require.NotNil(t, fixture.Resolve)
	require.NotNil(t, fixture.Seed)

	namespace := fmt.Sprintf("resolver-contract-%d", fixtureSequence.Add(1))
	secrets := []Secret{
		{Ref: namespace + "-present-1", Value: "value-1"},
		{Ref: namespace + "-present-2", Value: "value-2"},
	}
	expectedValues := make(map[string]string, len(secrets))
	for _, secret := range secrets {
		require.NoError(t, fixture.Seed(t.Context(), secret))
		expectedValues[secret.Ref] = secret.Value
	}
	missingRef := namespace + "-missing"

	paths := []resolverPath{
		{name: "default", resolve: fixture.Resolve},
	}
	if fixture.FallbackResolve != nil {
		paths = append(paths, resolverPath{name: "fallback", resolve: fixture.FallbackResolve})
	}

	refs := make([]string, 0, len(expectedValues))
	for ref := range expectedValues {
		refs = append(refs, ref)
	}
	sort.Strings(refs)

	for _, path := range paths {
		t.Run(path.name, func(t *testing.T) {
			t.Run("returns found refs", func(t *testing.T) {
				actual, err := path.resolve(t.Context(), refs)

				require.NoError(t, err)
				assert.Equal(t, expectedValues, actual)
			})

			t.Run("omits missing ref", func(t *testing.T) {
				values, err := path.resolve(t.Context(), []string{missingRef})

				require.NoError(t, err)
				assert.Empty(t, values)
			})

			t.Run("returns found and omits missing ref", func(t *testing.T) {
				requested := append(append([]string(nil), refs...), missingRef)
				actual, err := path.resolve(t.Context(), requested)

				require.NoError(t, err)
				assert.Equal(t, expectedValues, actual)
			})
		})
	}
}
