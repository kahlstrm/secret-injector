package loader

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	cfgpkg "github.com/kahlstrm/secret-injector/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_DoesNotBuildProvidersEagerly(t *testing.T) {
	provider := &fakeProvider{source: "custom", resolver: &fakeResolver{}}
	_ = New(nil, provider)
	assert.Equal(t, 0, provider.buildCount)
}

func TestRegistry_Validate_DoesNotBuildProviders(t *testing.T) {
	provider := &fakeProvider{source: "custom", resolver: &fakeResolver{}}
	reg := New(nil, provider)
	err := reg.Validate(cfgpkg.Config{Secrets: cfgpkg.Secrets{"A": {Source: "custom", Ref: "/a"}}})
	require.NoError(t, err)
	assert.Equal(t, 0, provider.buildCount)
}

func TestRegistry_Validate_UnknownSource(t *testing.T) {
	provider := &fakeProvider{source: "custom", resolver: &fakeResolver{}}
	reg := New(nil, provider)
	err := reg.Validate(cfgpkg.Config{Secrets: cfgpkg.Secrets{"A": {Source: "missing", Ref: "/a"}}})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnknownSource))
	assert.Equal(t, 0, provider.buildCount)
}

func TestRegistry_ResolveAll_CachesBuiltResolver(t *testing.T) {
	resolver := &fakeResolver{result: map[string]string{"/a": "value"}}
	provider := &fakeProvider{source: "custom", resolver: resolver}
	reg := New(nil, provider)
	cfg := cfgpkg.Config{Secrets: cfgpkg.Secrets{"A": {Source: "custom", Ref: "/a"}}}

	_, err := reg.ResolveAll(context.Background(), cfg)
	require.NoError(t, err)
	_, err = reg.ResolveAll(context.Background(), cfg)
	require.NoError(t, err)

	assert.Equal(t, 1, provider.buildCount)
	require.Len(t, resolver.resolvedArgs, 2)
}

func TestRegistry_ResolveAll_DoesNotCacheBuildErrors(t *testing.T) {
	resolver := &fakeResolver{result: map[string]string{"/a": "value"}}
	provider := &fakeProvider{source: "custom"}
	provider.buildFn = func(context.Context, WarningHandler) (Resolver, error) {
		if provider.buildCount == 1 {
			return nil, errors.New("boom")
		}
		return resolver, nil
	}
	reg := New(nil, provider)
	cfg := cfgpkg.Config{Secrets: cfgpkg.Secrets{"A": {Source: "custom", Ref: "/a"}}}

	_, err := reg.ResolveAll(context.Background(), cfg)
	require.Error(t, err)
	_, err = reg.ResolveAll(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, 2, provider.buildCount)
}

func TestRegistry_ResolveAll_RejectsNilResolver(t *testing.T) {
	provider := &fakeProvider{source: "custom"}
	provider.buildFn = func(context.Context, WarningHandler) (Resolver, error) {
		return nil, nil
	}
	reg := New(nil, provider)
	cfg := cfgpkg.Config{Secrets: cfgpkg.Secrets{"A": {Source: "custom", Ref: "/a"}}}

	_, err := reg.ResolveAll(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned a nil resolver")
}

func TestRegistry_ResolveAll_RejectsTypedNilResolver(t *testing.T) {
	provider := &fakeProvider{source: "custom"}
	provider.buildFn = func(context.Context, WarningHandler) (Resolver, error) {
		var resolver *fakeResolver
		return resolver, nil
	}
	reg := New(nil, provider)
	cfg := cfgpkg.Config{Secrets: cfgpkg.Secrets{"A": {Source: "custom", Ref: "/a"}}}

	_, err := reg.ResolveAll(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned a nil resolver")
}

func TestDefault_IncludesExtraProviders(t *testing.T) {
	provider := &fakeProvider{source: "custom", resolver: &fakeResolver{}}
	reg := Default(nil, provider)
	err := reg.Validate(cfgpkg.Config{Secrets: cfgpkg.Secrets{
		"A": {Source: "aws_ssm", Ref: "/a"},
		"B": {Source: "custom", Ref: "/b"},
	}})
	require.NoError(t, err)
	assert.Equal(t, 0, provider.buildCount)
}

func TestDefaultProviders_LoadAWSConfigLazily(t *testing.T) {
	var calls int
	reg := New(nil, defaultProviders(func(context.Context) (aws.Config, error) {
		calls++
		return aws.Config{}, nil
	})...)

	require.NotNil(t, reg)
	assert.Equal(t, 0, calls)
}

func TestDefaultProviders_ShareAWSConfigLoader(t *testing.T) {
	var calls int
	reg := New(nil, defaultProviders(func(context.Context) (aws.Config, error) {
		calls++
		return aws.Config{}, nil
	})...)

	_, err := reg.resolverForSource(context.Background(), "aws_ssm")
	require.NoError(t, err)
	_, err = reg.resolverForSource(context.Background(), "aws_secretsmanager")
	require.NoError(t, err)
	assert.Equal(t, 1, calls)
}
