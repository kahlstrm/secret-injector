package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	configPath := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(configPath, []byte(`[profile injector]
aws_access_key_id = profile-key
aws_secret_access_key = profile-secret
`), 0o600))
	t.Setenv("AWS_CONFIG_FILE", configPath)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "credentials"))
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
