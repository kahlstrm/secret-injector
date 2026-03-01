//go:build integration

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssecretsmanager "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/kahlstrm/secret-injector/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testLS *testutil.LocalStackContainer

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	testLS, err = testutil.SetupLocalStack(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to start LocalStack: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	_ = testLS.Terminate(ctx)
	os.Exit(code)
}

// seedParameter creates or overwrites an SSM parameter.
func seedParameter(t *testing.T, ctx context.Context, client *awsssm.Client, name, value string) {
	t.Helper()
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
	t.Helper()
	tmpDir := t.TempDir()
	binary := filepath.Join(tmpDir, "secret-injector")

	cmd := exec.Command("go", "build", "-o", binary, ".")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "build failed: %s", string(out))

	return binary
}

func TestIntegration_ExecWithSSM(t *testing.T) {
	ctx := context.Background()
	cfg := testLS.MustAWSConfig(t, ctx)
	client := awsssm.NewFromConfig(cfg)
	endpoint := testLS.MustEndpoint(t, ctx)

	// Seed test parameters
	seedParameter(t, ctx, client, "/exec-test/db-password", "secret123")
	seedParameter(t, ctx, client, "/exec-test/api-key", "key456")

	binary := buildBinary(t)

	configJSON := `{"secrets":{"DB_PASSWORD":"ssm:/exec-test/db-password","API_KEY":"ssm:/exec-test/api-key"}}`

	// Run exec command that prints the injected env vars
	cmd := exec.Command(binary, "exec", "--config", configJSON, "--", "sh", "-c", "echo $DB_PASSWORD:$API_KEY")
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
	ctx := context.Background()
	cfg := testLS.MustAWSConfig(t, ctx)
	client := awsssm.NewFromConfig(cfg)
	endpoint := testLS.MustEndpoint(t, ctx)

	// Seed a parameter that will override an existing env var
	seedParameter(t, ctx, client, "/exec-test/override", "from-ssm")

	binary := buildBinary(t)

	configJSON := `{"secrets":{"OVERRIDE_VAR":"ssm:/exec-test/override"}}`

	// Run with an existing OVERRIDE_VAR that should be replaced by SSM value
	// Also test that INHERITED_VAR is preserved
	cmd := exec.Command(binary, "exec", "--config", configJSON, "--", "sh", "-c", "echo $OVERRIDE_VAR:$INHERITED_VAR")
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
	ctx := context.Background()
	endpoint := testLS.MustEndpoint(t, ctx)

	binary := buildBinary(t)

	// Reference a parameter that doesn't exist
	configJSON := `{"secrets":{"MISSING":"ssm:/nonexistent/param"}}`

	cmd := exec.Command(binary, "exec", "--config", configJSON, "--", "echo", "should-not-print")
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
	assert.Contains(t, stderr.String(), "missing required secrets")
}

func TestIntegration_ValidateWithTemplateRefs(t *testing.T) {
	binary := buildBinary(t)

	configJSON := `{"secrets":{"DB_PASSWORD":"ssm:/validate-test/{{.STAGE}}/db-password"}}`

	cmd := exec.Command(binary, "validate", "--config", configJSON, "--var", "STAGE=prod", "--debug")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	require.NoError(t, err, "validate failed: stderr=%s", stderr.String())
	assert.Contains(t, stdout.String(), `"DB_PASSWORD": "ssm:/validate-test/prod/db-password"`)
	assert.Empty(t, stderr.String())
}

func TestIntegration_ValidateWithTemplateRefsMissingVar(t *testing.T) {
	binary := buildBinary(t)

	configJSON := `{"secrets":{"DB_PASSWORD":"ssm:/validate-test/{{.STAGE}}/db-password"}}`

	cmd := exec.Command(binary, "validate", "--config", configJSON)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	require.Error(t, err, "validate should fail when required template var is missing")
	assert.Contains(t, stderr.String(), `map has no entry for key "STAGE"`)
}

func TestIntegration_FetchWithTemplateRefs(t *testing.T) {
	ctx := context.Background()
	cfg := testLS.MustAWSConfig(t, ctx)
	client := awsssm.NewFromConfig(cfg)
	endpoint := testLS.MustEndpoint(t, ctx)

	seedParameter(t, ctx, client, "/fetch-test/prod/db-password", "fetch-secret")

	binary := buildBinary(t)

	configJSON := `{"secrets":{"DB_PASSWORD":"ssm:/fetch-test/{{.STAGE}}/db-password"}}`

	cmd := exec.Command(binary, "fetch", "--config", configJSON, "--var", "STAGE=prod")
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
	require.NoError(t, err, "fetch failed: stderr=%s", stderr.String())
	assert.Equal(t, "DB_PASSWORD=fetch-secret\n", stdout.String())
	assert.Empty(t, stderr.String())
}

func TestIntegration_FetchWithSecretsManager(t *testing.T) {
	ctx := context.Background()
	cfg := testLS.MustAWSConfig(t, ctx)
	client := awssecretsmanager.NewFromConfig(cfg)
	endpoint := testLS.MustEndpoint(t, ctx)

	_, err := client.CreateSecret(ctx, &awssecretsmanager.CreateSecretInput{
		Name:         aws.String("fetch-sm-api-key"),
		SecretString: aws.String("sm-secret"),
	})
	require.NoError(t, err)

	binary := buildBinary(t)

	configJSON := `{"secrets":{"API_KEY":"secretsmanager:fetch-sm-api-key"}}`

	cmd := exec.Command(binary, "fetch", "--config", configJSON)
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

	err = cmd.Run()
	require.NoError(t, err, "fetch failed: stderr=%s", stderr.String())
	assert.Equal(t, "API_KEY=sm-secret\n", stdout.String())
	assert.Empty(t, stderr.String())
}

func TestIntegration_ExecWithTemplateRefs(t *testing.T) {
	ctx := context.Background()
	cfg := testLS.MustAWSConfig(t, ctx)
	client := awsssm.NewFromConfig(cfg)
	endpoint := testLS.MustEndpoint(t, ctx)

	seedParameter(t, ctx, client, "/exec-template/prod/api-key", "exec-secret")

	binary := buildBinary(t)

	configJSON := `{"secrets":{"API_KEY":"ssm:/exec-template/{{.STAGE}}/api-key"}}`

	cmd := exec.Command(binary, "exec", "--config", configJSON, "--var", "STAGE=prod", "--", "sh", "-c", "echo $API_KEY")
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
	assert.Equal(t, "exec-secret\n", stdout.String())
	assert.Empty(t, stderr.String())
}
