package loader

import (
	"context"
	"errors"
	"testing"

	cfgpkg "github.com/kahlstrm/secret-injector/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeLoader struct {
	source       string
	result       map[string]string
	err          error
	resolvedArgs [][]string
}

func (f *fakeLoader) Source() string { return f.source }

func (f *fakeLoader) Resolve(_ context.Context, refs []string) (map[string]string, error) {
	// record a copy of refs to avoid external mutation affecting assertions
	cp := append([]string(nil), refs...)
	f.resolvedArgs = append(f.resolvedArgs, cp)
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func TestResolveAll_GroupsBySourceAndCallsOnce(t *testing.T) {
	cfg := cfgpkg.Config{Secrets: cfgpkg.Secrets{
		"A": {Source: "ssm", Ref: "/p1"}, // duplicate ref in same source
		"B": {Source: "ssm", Ref: "/p1"},
		"C": {Source: "ssm", Ref: "/p2"},
		"D": {Source: "sm", Ref: "/k1"}, // different source
		"E": {Source: "sm", Ref: "/k2"},
	}}

	ssm := &fakeLoader{source: "ssm", result: map[string]string{"/p1": "v1", "/p2": "v2"}}
	sm := &fakeLoader{source: "sm", result: map[string]string{"/k1": "x1", "/k2": "x2"}}
	reg := Registry{"ssm": ssm, "sm": sm}

	out, err := ResolveAll(context.Background(), cfg, reg)
	require.NoError(t, err)

	// Verify output mapping
	assert.Equal(t, "v1", out["A"]) // same ref as B
	assert.Equal(t, "v1", out["B"]) // deduped fetch
	assert.Equal(t, "v2", out["C"])
	assert.Equal(t, "x1", out["D"])
	assert.Equal(t, "x2", out["E"])

	// Resolve is called exactly once per source with deduped refs
	require.Len(t, ssm.resolvedArgs, 1, "ssm Resolve should be called once")
	require.Len(t, sm.resolvedArgs, 1, "sm Resolve should be called once")

	assert.ElementsMatch(t, []string{"/p1", "/p2"}, ssm.resolvedArgs[0])
	assert.ElementsMatch(t, []string{"/k1", "/k2"}, sm.resolvedArgs[0])

	// Explicitly assert uniqueness of refs passed to each Resolve call
	assertUniqueRefs(t, ssm.resolvedArgs[0])
	assertUniqueRefs(t, sm.resolvedArgs[0])
}

func TestResolveAll_UnknownSource(t *testing.T) {
	cfg := cfgpkg.Config{Secrets: cfgpkg.Secrets{
		"A": {Source: "unknown", Ref: "/p1"},
	}}
	_, err := ResolveAll(context.Background(), cfg, Registry{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnknownSource))
}

func TestResolveAll_ErrorOnMissingValue(t *testing.T) {
	cfg := cfgpkg.Config{Secrets: cfgpkg.Secrets{
		"A": {Source: "ssm", Ref: "/missing"},
		"B": {Source: "ssm", Ref: "/ok"},
	}}

	ssm := &fakeLoader{source: "ssm", result: map[string]string{"/ok": "value"}}
	reg := Registry{"ssm": ssm}

	_, err := ResolveAll(context.Background(), cfg, reg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing value for ref \"/missing\"")
}

// assertUniqueRefs fails the test if any value appears more than once.
func assertUniqueRefs(t *testing.T, refs []string) {
	t.Helper()
	seen := make(map[string]struct{}, len(refs))
	for _, r := range refs {
		if _, ok := seen[r]; ok {
			t.Fatalf("duplicate ref in Resolve args: %q (args=%v)", r, refs)
		}
		seen[r] = struct{}{}
	}
}
