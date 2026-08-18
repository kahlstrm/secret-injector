package loader

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kahlstrm/secret-injector/internal/testutil"
	cfgpkg "github.com/kahlstrm/secret-injector/pkg/config"
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

func TestDefaultWithOptions_DoesNotChangeEnvironmentForExtraProviders(t *testing.T) {
	t.Setenv("AWS_REGION", "ambient-region")
	t.Setenv("AWS_ENDPOINT_URL", "http://ambient.example")
	t.Setenv("AWS_ENDPOINT_URL_SSM", "http://ambient-ssm.example")

	assertAmbient := func() {
		assert.Equal(t, "ambient-region", os.Getenv("AWS_REGION"))
		assert.Equal(t, "http://ambient.example", os.Getenv("AWS_ENDPOINT_URL"))
		assert.Equal(t, "http://ambient-ssm.example", os.Getenv("AWS_ENDPOINT_URL_SSM"))
	}
	provider := &fakeProvider{source: "custom"}
	provider.buildFn = func(context.Context, WarningHandler) (Resolver, error) {
		assertAmbient()
		return &fakeResolver{resolveFn: func(context.Context, []string) (map[string]string, error) {
			assertAmbient()
			return map[string]string{"/value": "resolved"}, nil
		}}, nil
	}
	reg := DefaultWithOptions(nil, DefaultOptions{AWS: AWSOptions{Profile: "injector", Region: "eu-west-1"}}, provider)

	values, err := reg.ResolveAll(context.Background(), cfgpkg.Config{Secrets: cfgpkg.Secrets{
		"VALUE": {Source: "custom", Ref: "/value"},
	}})

	require.NoError(t, err)
	assert.Equal(t, map[string]string{"VALUE": "resolved"}, values)
	assert.Equal(t, "ambient-region", os.Getenv("AWS_REGION"))
	assert.Equal(t, "http://ambient.example", os.Getenv("AWS_ENDPOINT_URL"))
	assert.Equal(t, "http://ambient-ssm.example", os.Getenv("AWS_ENDPOINT_URL_SSM"))
}

func TestDefaultWithOptions_ProfileWithoutRegionFailsClearly(t *testing.T) {
	testutil.UseSharedConfig(t, `[profile injector]
aws_access_key_id = profile-key
aws_secret_access_key = profile-secret
`)
	t.Setenv("AWS_REGION", "ambient-region")
	t.Setenv("AWS_ENDPOINT_URL", "http://127.0.0.1:1")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	reg := DefaultWithOptions(nil, DefaultOptions{AWS: AWSOptions{Profile: "injector"}})
	_, err := reg.ResolveAll(ctx, cfgpkg.Config{Secrets: cfgpkg.Secrets{
		"VALUE": {Source: "aws_ssm", Ref: "/value"},
	}})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAWSRegionRequired)
	assert.Contains(t, err.Error(), "selected AWS profile has no region")
	assert.Equal(t, "ambient-region", os.Getenv("AWS_REGION"))
}

func TestDefaultProviders_LoadAWSConfigLazily(t *testing.T) {
	var calls int
	reg := New(nil, defaultProviders(func(context.Context) (aws.Config, error) {
		calls++
		return aws.Config{}, nil
	}, GCPOptions{})...)

	require.NotNil(t, reg)
	assert.Equal(t, 0, calls)
}

func TestDefaultProviders_ShareAWSConfigLoader(t *testing.T) {
	var calls int
	reg := New(nil, defaultProviders(func(context.Context) (aws.Config, error) {
		calls++
		return aws.Config{}, nil
	}, GCPOptions{})...)

	_, err := reg.resolverForSource(context.Background(), "aws_ssm")
	require.NoError(t, err)
	_, err = reg.resolverForSource(context.Background(), "aws_secretsmanager")
	require.NoError(t, err)
	assert.Equal(t, 1, calls)
}
func TestRegistry_ValidateSource(t *testing.T) {
	reg := Default(nil)

	require.NoError(t, reg.ValidateSource("gcp_secretmanager"))

	err := reg.ValidateSource("custom")
	require.ErrorIs(t, err, ErrUnknownSource)
	assert.Contains(t, err.Error(), "supported sources are")
	assert.Contains(t, err.Error(), "'aws_ssm'", "the message should name what is available")
}

func TestRegistry_SourcesAreSorted(t *testing.T) {
	assert.Equal(t,
		[]string{"aws_secretsmanager", "aws_ssm", "gcp_secretmanager"},
		Default(nil).Sources())
}

func TestRegistry_ValidateSourceOnNilRegistry(t *testing.T) {
	var reg *Registry

	// Matches Validate and ResolveAll, which report an unknown source rather than
	// panicking, since the method is handed to config.Load as a validator.
	err := reg.ValidateSource("aws_ssm")

	require.ErrorIs(t, err, ErrUnknownSource)
	assert.Empty(t, reg.Sources())
}
