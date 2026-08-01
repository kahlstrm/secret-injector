//go:build integration

package resolvercontract

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type resolver interface {
	Resolve(context.Context, []string) (map[string]string, error)
}

// Secret describes synthetic secret data provisioned by an integration fixture.
type Secret struct {
	Ref   string
	Value string
}

// Fixture provides provider-specific setup for shared integration behavior.
type Fixture struct {
	Resolver         resolver
	FallbackResolver resolver
	Seed             func(context.Context, Secret) error
}

type resolverPath struct {
	name     string
	resolver resolver
}

var fixtureSequence atomic.Uint64

// Run verifies common found and missing semantics against each provided resolver.
func Run(t *testing.T, fixture Fixture) {
	t.Helper()
	require.False(t, isNilResolver(fixture.Resolver), "resolver must not be nil")
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
		{name: "default", resolver: fixture.Resolver},
	}
	if fixture.FallbackResolver != nil {
		require.False(t, isNilResolver(fixture.FallbackResolver), "fallback resolver must not be typed nil")
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

func isNilResolver(resolver resolver) bool {
	if resolver == nil {
		return true
	}

	value := reflect.ValueOf(resolver)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
