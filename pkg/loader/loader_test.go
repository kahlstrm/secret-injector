package loader

import (
	"context"
	"errors"
	"testing"

	cfgpkg "github.com/kahlstrm/secret-injector/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeResolver struct {
	result       map[string]string
	err          error
	resolvedArgs [][]string
	resolveFn    func(context.Context, []string) (map[string]string, error)
}

func (f *fakeResolver) Resolve(ctx context.Context, refs []string) (map[string]string, error) {
	cp := append([]string(nil), refs...)
	f.resolvedArgs = append(f.resolvedArgs, cp)
	if f.resolveFn != nil {
		return f.resolveFn(ctx, refs)
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

type fakeProvider struct {
	source     string
	resolver   Resolver
	err        error
	buildCount int
	buildFn    func(context.Context, WarningHandler) (Resolver, error)
}

func (f *fakeProvider) Source() string { return f.source }

func (f *fakeProvider) Build(ctx context.Context, onWarning WarningHandler) (Resolver, error) {
	f.buildCount++
	if f.buildFn != nil {
		return f.buildFn(ctx, onWarning)
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.resolver, nil
}

func TestRegistry_ResolveAll_GroupsBySourceAndCallsOnce(t *testing.T) {
	cfg := cfgpkg.Config{Secrets: cfgpkg.Secrets{
		"A": {Source: "aws_ssm", Ref: "/p1"},
		"B": {Source: "aws_ssm", Ref: "/p1"},
		"C": {Source: "aws_ssm", Ref: "/p2"},
		"D": {Source: "aws_secretsmanager", Ref: "/k1"},
		"E": {Source: "aws_secretsmanager", Ref: "/k2"},
	}}

	ssmResolver := &fakeResolver{result: map[string]string{"/p1": "v1", "/p2": "v2"}}
	smResolver := &fakeResolver{result: map[string]string{"/k1": "x1", "/k2": "x2"}}
	ssmProvider := &fakeProvider{source: "aws_ssm", resolver: ssmResolver}
	smProvider := &fakeProvider{source: "aws_secretsmanager", resolver: smResolver}

	reg := New(nil, ssmProvider, smProvider)
	out, err := reg.ResolveAll(context.Background(), cfg)
	require.NoError(t, err)

	assert.Equal(t, "v1", out["A"])
	assert.Equal(t, "v1", out["B"])
	assert.Equal(t, "v2", out["C"])
	assert.Equal(t, "x1", out["D"])
	assert.Equal(t, "x2", out["E"])

	require.Equal(t, 1, ssmProvider.buildCount)
	require.Equal(t, 1, smProvider.buildCount)
	require.Len(t, ssmResolver.resolvedArgs, 1)
	require.Len(t, smResolver.resolvedArgs, 1)
	assert.ElementsMatch(t, []string{"/p1", "/p2"}, ssmResolver.resolvedArgs[0])
	assert.ElementsMatch(t, []string{"/k1", "/k2"}, smResolver.resolvedArgs[0])
	assertUniqueRefs(t, ssmResolver.resolvedArgs[0])
	assertUniqueRefs(t, smResolver.resolvedArgs[0])
}

func TestRegistry_ResolveAll_UnknownSource(t *testing.T) {
	cfg := cfgpkg.Config{Secrets: cfgpkg.Secrets{
		"A": {Source: "unknown", Ref: "/p1"},
	}}

	reg := New(nil)
	_, err := reg.ResolveAll(context.Background(), cfg)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnknownSource))
}

func TestRegistry_ResolveAll_ErrorOnMissingRequiredValues(t *testing.T) {
	cfg := cfgpkg.Config{Secrets: cfgpkg.Secrets{
		"A": {Source: "aws_ssm", Ref: "/missing-a"},
		"B": {Source: "aws_ssm", Ref: "/missing-b"},
	}}

	provider := &fakeProvider{source: "aws_ssm", resolver: &fakeResolver{result: map[string]string{}}}
	reg := New(nil, provider)

	_, err := reg.ResolveAll(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required secrets")
	assert.Contains(t, err.Error(), "env \"A\" (source \"aws_ssm\", ref \"/missing-a\")")
	assert.Contains(t, err.Error(), "env \"B\" (source \"aws_ssm\", ref \"/missing-b\")")
}

func TestRegistry_ResolveAll_MissingOptionalWarnsAndContinues(t *testing.T) {
	cfg := cfgpkg.Config{Secrets: cfgpkg.Secrets{
		"OPTIONAL": {Source: "aws_ssm", Ref: "/missing", Optional: true},
		"REQ":      {Source: "aws_ssm", Ref: "/ok"},
	}}

	provider := &fakeProvider{source: "aws_ssm", resolver: &fakeResolver{result: map[string]string{"/ok": "value"}}}
	var warnings []string
	warn := func(_ context.Context, msg string) { warnings = append(warnings, msg) }
	reg := New(warn, provider)

	out, err := reg.ResolveAll(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"REQ": "value"}, out)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "optional secret not found for env \"OPTIONAL\"")
}

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
