//go:build integration

package testutil

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/localstack"
)

const localStackImage = "localstack/localstack:4.14.0"

// LocalStackContainer wraps a LocalStack testcontainer with helper methods.
type LocalStackContainer struct {
	*localstack.LocalStackContainer
}

// SetupLocalStack starts a LocalStack container with SSM and Secrets Manager enabled.
// Returns an error if Docker is unavailable.
func SetupLocalStack(ctx context.Context) (*LocalStackContainer, error) {
	// Pin the latest community image explicitly because `latest` now requires auth.
	container, err := localstack.Run(ctx, localStackImage,
		testcontainers.WithEnv(map[string]string{
			"SERVICES":           "ssm,secretsmanager",
			"DEBUG":              "0",
			"LS_LOG":             "warn",
			"AWS_DEFAULT_REGION": "us-east-1",
		}),
	)
	if err != nil {
		return nil, err
	}
	return &LocalStackContainer{container}, nil
}

// MustSetupLocalStack starts LocalStack and fails the test if it can't.
func MustSetupLocalStack(t *testing.T, ctx context.Context) *LocalStackContainer {
	t.Helper()
	ls, err := SetupLocalStack(ctx)
	if err != nil {
		t.Fatalf("failed to start LocalStack: %v", err)
	}
	t.Cleanup(func() {
		if err := ls.Terminate(ctx); err != nil {
			t.Logf("failed to terminate LocalStack: %v", err)
		}
	})
	return ls
}

// Endpoint returns the LocalStack endpoint URL.
func (c *LocalStackContainer) Endpoint(ctx context.Context) (string, error) {
	return c.PortEndpoint(ctx, "4566/tcp", "http")
}

// MustEndpoint returns the endpoint or fails the test.
func (c *LocalStackContainer) MustEndpoint(t *testing.T, ctx context.Context) string {
	t.Helper()
	endpoint, err := c.Endpoint(ctx)
	if err != nil {
		t.Fatalf("failed to get LocalStack endpoint: %v", err)
	}
	return endpoint
}

// AWSConfig returns an AWS SDK config configured to use this LocalStack container.
func (c *LocalStackContainer) AWSConfig(ctx context.Context) (aws.Config, error) {
	endpoint, err := c.Endpoint(ctx)
	if err != nil {
		return aws.Config{}, err
	}

	return awscfg.LoadDefaultConfig(ctx,
		awscfg.WithRegion("us-east-1"),
		awscfg.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
		awscfg.WithBaseEndpoint(endpoint),
	)
}

// MustAWSConfig returns an AWS config or fails the test.
func (c *LocalStackContainer) MustAWSConfig(t *testing.T, ctx context.Context) aws.Config {
	t.Helper()
	cfg, err := c.AWSConfig(ctx)
	if err != nil {
		t.Fatalf("failed to create AWS config: %v", err)
	}
	return cfg
}
