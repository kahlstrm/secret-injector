//go:build integration

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupLocalStackClient creates an SSM client pointing to LocalStack and seeds test parameters.
func setupLocalStackClient(t *testing.T, ctx context.Context) *awsssm.Client {
	endpoint := os.Getenv("LOCALSTACK_URL")
	if endpoint == "" {
		endpoint = "http://localhost:4566"
	}

	resolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, _ ...interface{}) (aws.Endpoint, error) {
		if service == awsssm.ServiceID {
			return aws.Endpoint{URL: endpoint, HostnameImmutable: true, PartitionID: "aws", SigningRegion: region}, nil
		}
		return aws.Endpoint{}, &aws.EndpointNotFoundError{}
	})

	cfg, err := awscfg.LoadDefaultConfig(ctx,
		awscfg.WithRegion("us-east-1"),
		awscfg.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
		awscfg.WithEndpointResolverWithOptions(resolver),
	)
	require.NoError(t, err)

	return awsssm.NewFromConfig(cfg)
}

// seedParameter creates or overwrites an SSM parameter.
func seedParameter(t *testing.T, ctx context.Context, client *awsssm.Client, name, value string) {
	_, err := client.PutParameter(ctx, &awsssm.PutParameterInput{
		Name:      aws.String(name),
		Type:      ssmtypes.ParameterTypeSecureString,
		Value:     aws.String(value),
		Overwrite: aws.Bool(true),
	})
	require.NoError(t, err)
}

// buildBinary builds secret-injector to a temp directory and returns the path.
func buildBinary(t *testing.T) string {
	tmpDir := t.TempDir()
	binary := filepath.Join(tmpDir, "secret-injector")

	cmd := exec.Command("go", "build", "-o", binary, ".")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "build failed: %s", string(out))

	return binary
}

func TestIntegration_ExecWithSSM(t *testing.T) {
	if os.Getenv("LOCALSTACK") != "1" {
		t.Skip("set LOCALSTACK=1 to run integration tests")
	}

	ctx := context.Background()
	client := setupLocalStackClient(t, ctx)

	// Seed test parameters
	seedParameter(t, ctx, client, "/exec-test/db-password", "secret123")
	seedParameter(t, ctx, client, "/exec-test/api-key", "key456")

	binary := buildBinary(t)

	// Get LocalStack URL for AWS endpoint override
	endpoint := os.Getenv("LOCALSTACK_URL")
	if endpoint == "" {
		endpoint = "http://localhost:4566"
	}

	configJSON := `{"secrets":{"DB_PASSWORD":"ssm:/exec-test/db-password","API_KEY":"ssm:/exec-test/api-key"}}`

	// Run exec command that prints the injected env vars
	cmd := exec.Command(binary, "exec", "--config-json", configJSON, "--", "sh", "-c", "echo $DB_PASSWORD:$API_KEY")
	cmd.Env = []string{
		"AWS_ACCESS_KEY_ID=test",
		"AWS_SECRET_ACCESS_KEY=test",
		"AWS_REGION=us-east-1",
		"AWS_ENDPOINT_URL=" + endpoint,
		"PATH=/usr/bin:/bin",
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	require.NoError(t, err, "exec failed: stderr=%s", stderr.String())
	assert.Equal(t, "secret123:key456\n", stdout.String())
}

func TestIntegration_ExecInheritsAndOverrides(t *testing.T) {
	if os.Getenv("LOCALSTACK") != "1" {
		t.Skip("set LOCALSTACK=1 to run integration tests")
	}

	ctx := context.Background()
	client := setupLocalStackClient(t, ctx)

	// Seed a parameter that will override an existing env var
	seedParameter(t, ctx, client, "/exec-test/override", "from-ssm")

	binary := buildBinary(t)

	endpoint := os.Getenv("LOCALSTACK_URL")
	if endpoint == "" {
		endpoint = "http://localhost:4566"
	}

	configJSON := `{"secrets":{"OVERRIDE_VAR":"ssm:/exec-test/override"}}`

	// Run with an existing OVERRIDE_VAR that should be replaced by SSM value
	// Also test that INHERITED_VAR is preserved
	cmd := exec.Command(binary, "exec", "--config-json", configJSON, "--", "sh", "-c", "echo $OVERRIDE_VAR:$INHERITED_VAR")
	cmd.Env = []string{
		"AWS_ACCESS_KEY_ID=test",
		"AWS_SECRET_ACCESS_KEY=test",
		"AWS_REGION=us-east-1",
		"AWS_ENDPOINT_URL=" + endpoint,
		"PATH=/usr/bin:/bin",
		"OVERRIDE_VAR=original-value",
		"INHERITED_VAR=kept",
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	require.NoError(t, err, "exec failed: stderr=%s", stderr.String())
	assert.Equal(t, "from-ssm:kept\n", stdout.String())
}

func TestIntegration_ExecWithMissingParameter(t *testing.T) {
	if os.Getenv("LOCALSTACK") != "1" {
		t.Skip("set LOCALSTACK=1 to run integration tests")
	}

	binary := buildBinary(t)

	endpoint := os.Getenv("LOCALSTACK_URL")
	if endpoint == "" {
		endpoint = "http://localhost:4566"
	}

	// Reference a parameter that doesn't exist
	configJSON := `{"secrets":{"MISSING":"ssm:/nonexistent/param"}}`

	cmd := exec.Command(binary, "exec", "--config-json", configJSON, "--", "echo", "should-not-print")
	cmd.Env = []string{
		"AWS_ACCESS_KEY_ID=test",
		"AWS_SECRET_ACCESS_KEY=test",
		"AWS_REGION=us-east-1",
		"AWS_ENDPOINT_URL=" + endpoint,
		"PATH=/usr/bin:/bin",
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	require.Error(t, err, "should fail with missing parameter")
	assert.Contains(t, stderr.String(), "missing value")
}
