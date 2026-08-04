package config

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kahlstrm/secret-injector/internal/testutil"
)

func TestLoader_Load_IsLazy(t *testing.T) {
	var calls int
	_ = NewLoader(func(context.Context) (aws.Config, error) {
		calls++
		return aws.Config{}, nil
	})
	assert.Equal(t, 0, calls)
}

func TestLoader_Load_CachesSuccessfulResult(t *testing.T) {
	var calls int
	loader := NewLoader(func(context.Context) (aws.Config, error) {
		calls++
		return aws.Config{Region: "us-east-1"}, nil
	})

	cfg, err := loader.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "us-east-1", cfg.Region)

	cfg, err = loader.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "us-east-1", cfg.Region)
	assert.Equal(t, 1, calls)
}

func TestLoader_Load_DoesNotCacheErrors(t *testing.T) {
	var calls int
	loader := NewLoader(func(context.Context) (aws.Config, error) {
		calls++
		if calls == 1 {
			return aws.Config{}, errors.New("boom")
		}
		return aws.Config{Region: "us-east-1"}, nil
	})

	_, err := loader.Load(context.Background())
	require.Error(t, err)

	cfg, err := loader.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "us-east-1", cfg.Region)
	assert.Equal(t, 2, calls)
}

func TestLoad_ExplicitProfileAndRegionOverrideEnvironment(t *testing.T) {
	testutil.UseSharedConfig(t, `[profile injector]
aws_access_key_id = profile-key
aws_secret_access_key = profile-secret
`)
	t.Setenv("AWS_ACCESS_KEY_ID", "ambient-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "ambient-secret")
	t.Setenv("AWS_REGION", "ambient-region")

	cfg, err := Load(context.Background(), Options{Profile: "injector", Region: "eu-west-1"})
	require.NoError(t, err)
	credentials, err := cfg.Credentials.Retrieve(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "profile-key", credentials.AccessKeyID)
	assert.Equal(t, "eu-west-1", cfg.Region)
}

func TestLoad_ProfileIgnoresAmbientRegionAndEndpoints(t *testing.T) {
	tests := []struct {
		name            string
		profileEndpoint string
		wantEndpoint    string
	}{
		{
			name:            "profile endpoint wins",
			profileEndpoint: "endpoint_url = http://profile.example\n",
			wantEndpoint:    "http://profile.example",
		},
		{
			name:            "profile service endpoint wins",
			profileEndpoint: "services = injector-services\n",
			wantEndpoint:    "http://profile-ssm.example",
		},
		{
			name: "profile without endpoint resolves normally",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testutil.UseSharedConfig(t, "[profile injector]\nregion = eu-west-1\naws_access_key_id = profile-key\naws_secret_access_key = profile-secret\n"+
				test.profileEndpoint+
				"\n[services injector-services]\nssm =\n  endpoint_url = http://profile-ssm.example\n")
			t.Setenv("AWS_REGION", "ambient-region")
			t.Setenv("AWS_ENDPOINT_URL", "http://ambient.example")
			t.Setenv("AWS_ENDPOINT_URL_SSM", "http://ambient-ssm.example")

			cfg, err := Load(context.Background(), Options{Profile: "injector"})
			require.NoError(t, err)

			assert.Equal(t, "eu-west-1", cfg.Region)
			clientOptions := awsssm.NewFromConfig(cfg).Options()
			assert.Equal(t, test.wantEndpoint, aws.ToString(clientOptions.BaseEndpoint))
		})
	}
}

// A missing profile must fail instead of falling back to the ambient credential
// chain the selected profile is meant to replace. The SDK enforces this only
// while a profile is explicitly selected, so pin the behaviour we rely on.
func TestLoad_MissingProfileIsRejected(t *testing.T) {
	testutil.UseSharedConfig(t, `[profile injector]
region = eu-west-1
`)
	t.Setenv("AWS_ACCESS_KEY_ID", "ambient-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "ambient-secret")
	t.Setenv("AWS_REGION", "ambient-region")

	_, err := Load(context.Background(), Options{Profile: "typo"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "typo")
}

func TestLoad_ProfileWithoutRegionIsRejected(t *testing.T) {
	testutil.UseSharedConfig(t, `[profile injector]
aws_access_key_id = profile-key
aws_secret_access_key = profile-secret
`)
	t.Setenv("AWS_REGION", "ambient-region")

	_, err := Load(context.Background(), Options{Profile: "injector"})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRegionRequired)
	assert.Contains(t, err.Error(), "selected AWS profile has no region")
}

func TestLoad_ProfileIgnoresAmbientEndpointSettings(t *testing.T) {
	tests := []struct {
		name             string
		profileSettings  string
		ambientFIPS      string
		ambientDualStack string
		ambientRetryMode string
		wantFIPS         aws.FIPSEndpointState
		wantDualStack    aws.DualStackEndpointState
		wantRetryMode    aws.RetryMode
	}{
		{
			name:             "profile values win",
			profileSettings:  "use_fips_endpoint = true\nuse_dualstack_endpoint = true\nretry_mode = adaptive\n",
			ambientFIPS:      "false",
			ambientDualStack: "false",
			ambientRetryMode: "standard",
			wantFIPS:         aws.FIPSEndpointStateEnabled,
			wantDualStack:    aws.DualStackEndpointStateEnabled,
			wantRetryMode:    aws.RetryModeAdaptive,
		},
		{
			name:             "profile defaults win",
			ambientFIPS:      "true",
			ambientDualStack: "true",
			ambientRetryMode: "adaptive",
			wantFIPS:         aws.FIPSEndpointStateDisabled,
			wantDualStack:    aws.DualStackEndpointStateDisabled,
			wantRetryMode:    aws.RetryModeStandard,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testutil.UseSharedConfig(t, "[profile injector]\nregion = eu-west-1\naws_access_key_id = profile-key\naws_secret_access_key = profile-secret\n"+test.profileSettings)
			t.Setenv("AWS_USE_FIPS_ENDPOINT", test.ambientFIPS)
			t.Setenv("AWS_USE_DUALSTACK_ENDPOINT", test.ambientDualStack)
			t.Setenv("AWS_RETRY_MODE", test.ambientRetryMode)

			cfg, err := Load(context.Background(), Options{Profile: "injector"})
			require.NoError(t, err)
			clientOptions := awsssm.NewFromConfig(cfg).Options()

			assert.Equal(t, test.wantFIPS, clientOptions.EndpointOptions.UseFIPSEndpoint)
			assert.Equal(t, test.wantDualStack, clientOptions.EndpointOptions.UseDualStackEndpoint)
			assert.Equal(t, test.wantRetryMode, cfg.RetryMode)
		})
	}
}
