//go:build integration

package resolvercontract

import (
	"context"
	"fmt"
	"testing"
)

type fakeResolver struct {
	values map[string]string
}

func (f *fakeResolver) Resolve(_ context.Context, refs []string) (map[string]string, error) {
	values := make(map[string]string, len(refs))
	for _, ref := range refs {
		if value, ok := f.values[ref]; ok {
			values[ref] = value
		}
	}
	return values, nil
}

func TestRunUsesUniqueRefs(t *testing.T) {
	values := make(map[string]string)
	fixture := Fixture{
		Resolve: (&fakeResolver{values: values}).Resolve,
		Seed: func(_ context.Context, secret Secret) error {
			if _, exists := values[secret.Ref]; exists {
				return fmt.Errorf("duplicate ref %q", secret.Ref)
			}
			values[secret.Ref] = secret.Value
			return nil
		},
	}

	t.Run("first run", func(t *testing.T) {
		Run(t, fixture)
	})
	t.Run("second run", func(t *testing.T) {
		Run(t, fixture)
	})
}
