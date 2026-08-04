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

var (
	testBinary string
	testLS     *testutil.LocalStackContainer
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	tmpDir, err := os.MkdirTemp("", "secret-injector-integration-")
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to create temp directory: %v\n", err)
		os.Exit(1)
	}
	testBinary = filepath.Join(tmpDir, "secret-injector")
	cmd := exec.Command("go", "build", "-o", testBinary, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to build secret-injector: %v: %s\n", err, out)
		_ = os.RemoveAll(tmpDir)
		os.Exit(1)
	}

	testLS, err = testutil.SetupLocalStack(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to start LocalStack: %v\n", err)
		_ = os.RemoveAll(tmpDir)
		os.Exit(1)
	}

	code := m.Run()

	_ = testLS.Terminate(ctx)
	_ = os.RemoveAll(tmpDir)
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

func writeIntegrationAWSProfile(t *testing.T, endpoint string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config")
	contents := fmt.Sprintf(`[profile injector]
region = us-east-1
endpoint_url = %s
aws_access_key_id = profile-key
aws_secret_access_key = profile-secret
`, endpoint)
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

func writeIntegrationAssumeRoleAWSProfile(t *testing.T, endpoint string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config")
	contents := fmt.Sprintf(`[profile source]
aws_access_key_id = source-key
aws_secret_access_key = source-secret

[profile injector]
region = us-east-1
endpoint_url = %s
role_arn = arn:aws:iam::000000000000:role/secret-injector-test
source_profile = source
`, endpoint)
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

func TestIntegration_ExecWithIsolatedAWSProfile(t *testing.T) {
	ctx := context.Background()
	cfg := testLS.MustAWSConfig(t, ctx)
	ssmClient := awsssm.NewFromConfig(cfg)
	secretsManagerClient := awssecretsmanager.NewFromConfig(cfg)
	endpoint := testLS.MustEndpoint(t, ctx)
	seedParameter(t, ctx, ssmClient, "/isolated-profile/value", "resolved")
	_, err := secretsManagerClient.CreateSecret(ctx, &awssecretsmanager.CreateSecretInput{
		Name:         aws.String("isolated-profile-secret"),
		SecretString: aws.String("secret-resolved"),
	})
	require.NoError(t, err)

	configJSON := `{"secrets":{"VALUE":{"source":"aws_ssm","ref":"/isolated-profile/value"},"SECRET_VALUE":{"source":"aws_secretsmanager","ref":"isolated-profile-secret"}}}`
	ambientEndpoint := "http://127.0.0.1:1"
	cmd := exec.Command(testBinary, "exec", "--config", configJSON, "--", "sh", "-c", `printf '%s|%s|%s|%s|%s\n' "$VALUE" "$SECRET_VALUE" "$AWS_ENDPOINT_URL" "$AWS_ACCESS_KEY_ID" "$AWS_REGION"`)
	cmd.Env = []string{
		"AWS_ACCESS_KEY_ID=ambient-key",
		"AWS_SECRET_ACCESS_KEY=ambient-secret",
		"AWS_REGION=eu-north-1",
		"AWS_ENDPOINT_URL=" + ambientEndpoint,
		"AWS_ENDPOINT_URL_SSM=" + ambientEndpoint,
		"AWS_ENDPOINT_URL_SECRETS_MANAGER=" + ambientEndpoint,
		"AWS_USE_FIPS_ENDPOINT=true",
		"AWS_USE_DUALSTACK_ENDPOINT=true",
		"AWS_RETRY_MODE=adaptive",
		"AWS_CONFIG_FILE=" + writeIntegrationAWSProfile(t, endpoint),
		"AWS_SHARED_CREDENTIALS_FILE=" + filepath.Join(t.TempDir(), "credentials"),
		"SECRET_INJECTOR_AWS_PROFILE=injector",
		"PATH=/usr/bin:/bin",
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	require.NoError(t, err, "exec failed: stderr=%s", stderr.String())
	assert.Equal(t, "resolved|secret-resolved|"+ambientEndpoint+"|ambient-key|eu-north-1\n", stdout.String())
	assert.Empty(t, stderr.String())
}

func TestIntegration_FetchWithIsolatedAssumeRoleProfile(t *testing.T) {
	ctx := context.Background()
	cfg := testLS.MustAWSConfig(t, ctx)
	endpoint := testLS.MustEndpoint(t, ctx)
	seedParameter(t, ctx, awsssm.NewFromConfig(cfg), "/isolated-assume-role/value", "assumed")

	configJSON := `{"secrets":{"VALUE":{"source":"aws_ssm","ref":"/isolated-assume-role/value"}}}`
	ambientEndpoint := "http://127.0.0.1:1"
	cmd := exec.Command(testBinary, "fetch", "--config", configJSON)
	cmd.Env = []string{
		"AWS_ACCESS_KEY_ID=ambient-key",
		"AWS_SECRET_ACCESS_KEY=ambient-secret",
		"AWS_REGION=eu-north-1",
		"AWS_ENDPOINT_URL=" + ambientEndpoint,
		"AWS_ENDPOINT_URL_SSM=" + ambientEndpoint,
		"AWS_ENDPOINT_URL_STS=" + ambientEndpoint,
		"AWS_CONFIG_FILE=" + writeIntegrationAssumeRoleAWSProfile(t, endpoint),
		"AWS_SHARED_CREDENTIALS_FILE=" + filepath.Join(t.TempDir(), "credentials"),
		"SECRET_INJECTOR_AWS_PROFILE=injector",
		"PATH=/usr/bin:/bin",
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	require.NoError(t, err, "fetch failed: stderr=%s", stderr.String())
	assert.Equal(t, "VALUE=assumed\n", stdout.String())
	assert.Empty(t, stderr.String())
}

func TestIntegration_AWSProfileFlagOverridesEnvironment(t *testing.T) {
	ctx := context.Background()
	cfg := testLS.MustAWSConfig(t, ctx)
	client := awsssm.NewFromConfig(cfg)
	endpoint := testLS.MustEndpoint(t, ctx)
	seedParameter(t, ctx, client, "/isolated-profile/flag", "from-flag-profile")

	configJSON := `{"secrets":{"VALUE":{"source":"aws_ssm","ref":"/isolated-profile/flag"}}}`
	cmd := exec.Command(testBinary, "fetch", "--aws-profile", "injector", "--config", configJSON)
	cmd.Env = []string{
		"AWS_ACCESS_KEY_ID=ambient-key",
		"AWS_SECRET_ACCESS_KEY=ambient-secret",
		"AWS_REGION=eu-north-1",
		"AWS_ENDPOINT_URL=http://127.0.0.1:1",
		"AWS_CONFIG_FILE=" + writeIntegrationAWSProfile(t, endpoint),
		"AWS_SHARED_CREDENTIALS_FILE=" + filepath.Join(t.TempDir(), "credentials"),
		"SECRET_INJECTOR_AWS_PROFILE=missing",
		"PATH=/usr/bin:/bin",
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	require.NoError(t, err, "fetch failed: stderr=%s", stderr.String())
	assert.Equal(t, "VALUE=from-flag-profile\n", stdout.String())
	assert.Empty(t, stderr.String())
}

func TestIntegration_AWSProfileWithoutRegionFailsClearly(t *testing.T) {
	profilePath := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(profilePath, []byte(`[profile injector]
aws_access_key_id = profile-key
aws_secret_access_key = profile-secret
`), 0o600))

	configJSON := `{"secrets":{"VALUE":{"source":"aws_ssm","ref":"/value"}}}`
	cmd := exec.Command(testBinary, "fetch", "--aws-profile", "injector", "--config", configJSON)
	cmd.Env = []string{
		"AWS_REGION=eu-north-1",
		"AWS_ENDPOINT_URL=http://127.0.0.1:1",
		"AWS_CONFIG_FILE=" + profilePath,
		"AWS_SHARED_CREDENTIALS_FILE=" + filepath.Join(t.TempDir(), "credentials"),
		"PATH=/usr/bin:/bin",
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()

	require.Error(t, err)
	assert.Contains(t, stderr.String(), "AWS region is required: selected AWS profile has no region")
}

func TestIntegration_ExecWithSSM(t *testing.T) {
	ctx := context.Background()
	cfg := testLS.MustAWSConfig(t, ctx)
	client := awsssm.NewFromConfig(cfg)
	endpoint := testLS.MustEndpoint(t, ctx)

	// Seed test parameters
	seedParameter(t, ctx, client, "/exec-test/db-password", "secret123")
	seedParameter(t, ctx, client, "/exec-test/api-key", "key456")

	configJSON := `{"secrets":{"DB_PASSWORD":{"source":"aws_ssm","ref":"/exec-test/db-password"},"API_KEY":{"source":"aws_ssm","ref":"/exec-test/api-key"}}}`

	// Run exec command that prints the injected env vars
	cmd := exec.Command(testBinary, "exec", "--config", configJSON, "--", "sh", "-c", "echo $DB_PASSWORD:$API_KEY")
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

	configJSON := `{"secrets":{"OVERRIDE_VAR":{"source":"aws_ssm","ref":"/exec-test/override"}}}`

	// Run with an existing OVERRIDE_VAR that should be replaced by SSM value
	// Also test that INHERITED_VAR is preserved
	cmd := exec.Command(testBinary, "exec", "--config", configJSON, "--", "sh", "-c", "echo $OVERRIDE_VAR:$INHERITED_VAR")
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

	// Reference a parameter that doesn't exist
	configJSON := `{"secrets":{"MISSING":{"source":"aws_ssm","ref":"/nonexistent/param"}}}`

	cmd := exec.Command(testBinary, "exec", "--config", configJSON, "--", "echo", "should-not-print")
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
	configJSON := `{"secrets":{"DB_PASSWORD":{"source":"aws_ssm","ref":"/validate-test/{{.STAGE}}/db-password"}}}`

	cmd := exec.Command(testBinary, "validate", "--config", configJSON, "--var", "STAGE=prod", "--debug")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	require.NoError(t, err, "validate failed: stderr=%s", stderr.String())
	assert.Contains(t, stdout.String(), `"ref": "/validate-test/prod/db-password"`)
	assert.Empty(t, stderr.String())
}

func TestIntegration_ValidateWithTemplateRefsMissingVar(t *testing.T) {
	configJSON := `{"secrets":{"DB_PASSWORD":{"source":"aws_ssm","ref":"/validate-test/{{.STAGE}}/db-password"}}}`

	cmd := exec.Command(testBinary, "validate", "--config", configJSON)
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

	configJSON := `{"secrets":{"DB_PASSWORD":{"source":"aws_ssm","ref":"/fetch-test/{{.STAGE}}/db-password"}}}`

	cmd := exec.Command(testBinary, "fetch", "--config", configJSON, "--var", "STAGE=prod")
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

	configJSON := `{"secrets":{"API_KEY":{"source":"aws_secretsmanager","ref":"fetch-sm-api-key"}}}`

	cmd := exec.Command(testBinary, "fetch", "--config", configJSON)
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

func TestIntegration_FetchWithMixedAWSBackends(t *testing.T) {
	ctx := context.Background()
	cfg := testLS.MustAWSConfig(t, ctx)
	ssmClient := awsssm.NewFromConfig(cfg)
	smClient := awssecretsmanager.NewFromConfig(cfg)
	endpoint := testLS.MustEndpoint(t, ctx)

	seedParameter(t, ctx, ssmClient, "/fetch-mixed/db-password", "ssm-secret")
	_, err := smClient.CreateSecret(ctx, &awssecretsmanager.CreateSecretInput{
		Name:         aws.String("fetch-mixed-api-key"),
		SecretString: aws.String("sm-secret"),
	})
	require.NoError(t, err)

	configJSON := `{"secrets":{"DB_PASSWORD":{"source":"aws_ssm","ref":"/fetch-mixed/db-password"},"API_KEY":{"source":"aws_secretsmanager","ref":"fetch-mixed-api-key"}}}`

	cmd := exec.Command(testBinary, "fetch", "--config", configJSON)
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
	assert.Equal(t, "API_KEY=sm-secret\nDB_PASSWORD=ssm-secret\n", stdout.String())
	assert.Empty(t, stderr.String())
}

func TestIntegration_ExecWithTemplateRefs(t *testing.T) {
	ctx := context.Background()
	cfg := testLS.MustAWSConfig(t, ctx)
	client := awsssm.NewFromConfig(cfg)
	endpoint := testLS.MustEndpoint(t, ctx)

	seedParameter(t, ctx, client, "/exec-template/prod/api-key", "exec-secret")

	configJSON := `{"secrets":{"API_KEY":{"source":"aws_ssm","ref":"/exec-template/{{.STAGE}}/api-key"}}}`

	cmd := exec.Command(testBinary, "exec", "--config", configJSON, "--var", "STAGE=prod", "--", "sh", "-c", "echo $API_KEY")
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
