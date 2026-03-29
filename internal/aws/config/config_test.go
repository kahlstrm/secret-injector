package config

import (
	"context"
	"errors"
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
