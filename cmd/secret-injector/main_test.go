package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

func captureOutput(t *testing.T, fn func() error) (stdout string, stderr string, runErr error) {
	t.Helper()

	stdoutR, stdoutW, err := os.Pipe()
	require.NoError(t, err)
	stderrR, stderrW, err := os.Pipe()
	require.NoError(t, err)

	origStdout := os.Stdout
	origStderr := os.Stderr
	os.Stdout = stdoutW
	os.Stderr = stderrW
	defer func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
	}()

	runErr = fn()

	require.NoError(t, stdoutW.Close())
	require.NoError(t, stderrW.Close())

	stdoutBytes, err := io.ReadAll(stdoutR)
	require.NoError(t, err)
	stderrBytes, err := io.ReadAll(stderrR)
	require.NoError(t, err)

	require.NoError(t, stdoutR.Close())
	require.NoError(t, stderrR.Close())

	return string(stdoutBytes), string(stderrBytes), runErr
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "simple", input: "simple", want: "'simple'"},
		{name: "with space", input: "with space", want: "'with space'"},
		{name: "single quote", input: "it's quoted", want: `'it'\''s quoted'`},
		{name: "multiple quotes", input: "a'b'c", want: `'a'\''b'\''c'`},
		{name: "dollar sign", input: "$HOME", want: "'$HOME'"},
		{name: "backticks", input: "`cmd`", want: "'`cmd`'"},
		{name: "empty", input: "", want: "''"},
		{name: "newline", input: "line1\nline2", want: "'line1\nline2'"},
		{name: "special chars", input: "a&b|c;d", want: "'a&b|c;d'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellQuote(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMergeEnv(t *testing.T) {
	tests := []struct {
		name     string
		current  []string
		secrets  map[string]string
		wantKeys map[string]string
	}{
		{
			name:     "empty both",
			current:  nil,
			secrets:  nil,
			wantKeys: map[string]string{},
		},
		{
			name:     "only current env",
			current:  []string{"FOO=bar", "BAZ=qux"},
			secrets:  nil,
			wantKeys: map[string]string{"FOO": "bar", "BAZ": "qux"},
		},
		{
			name:     "only secrets",
			current:  nil,
			secrets:  map[string]string{"SECRET": "value"},
			wantKeys: map[string]string{"SECRET": "value"},
		},
		{
			name:     "secrets override existing",
			current:  []string{"FOO=old", "BAR=keep"},
			secrets:  map[string]string{"FOO": "new"},
			wantKeys: map[string]string{"FOO": "new", "BAR": "keep"},
		},
		{
			name:     "add new secrets to existing",
			current:  []string{"EXISTING=val"},
			secrets:  map[string]string{"NEW": "secret"},
			wantKeys: map[string]string{"EXISTING": "val", "NEW": "secret"},
		},
		{
			name:     "handles values with equals signs",
			current:  []string{"URL=http://host?a=1&b=2"},
			secrets:  map[string]string{"CONN": "user=admin"},
			wantKeys: map[string]string{"URL": "http://host?a=1&b=2", "CONN": "user=admin"},
		},
		{
			name:     "handles empty values",
			current:  []string{"EMPTY="},
			secrets:  map[string]string{"ALSO_EMPTY": ""},
			wantKeys: map[string]string{"EMPTY": "", "ALSO_EMPTY": ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeEnv(tt.current, tt.secrets)

			// Parse result back into map for easier assertion
			got := make(map[string]string)
			for _, e := range result {
				for i := 0; i < len(e); i++ {
					if e[i] == '=' {
						got[e[:i]] = e[i+1:]
						break
					}
				}
			}

			assert.Equal(t, tt.wantKeys, got)
		})
	}
}

func TestParseVars(t *testing.T) {
	tests := []struct {
		name        string
		in          []string
		want        map[string]string
		errContains string
	}{
		{name: "empty", in: nil, want: map[string]string{}},
		{name: "single", in: []string{"STAGE=prod"}, want: map[string]string{"STAGE": "prod"}},
		{name: "multiple", in: []string{"STAGE=prod", "AWS_REGION=eu-west-1"}, want: map[string]string{"STAGE": "prod", "AWS_REGION": "eu-west-1"}},
		{name: "value contains equals", in: []string{"TOKEN=a=b=c"}, want: map[string]string{"TOKEN": "a=b=c"}},
		{name: "empty value", in: []string{"STAGE="}, want: map[string]string{"STAGE": ""}},
		{name: "missing equals", in: []string{"STAGE"}, errContains: "expected NAME=VALUE"},
		{name: "empty name", in: []string{"=prod"}, errContains: "expected NAME=VALUE"},
		{name: "duplicate name", in: []string{"STAGE=prod", "STAGE=dev"}, errContains: "duplicate --var name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseVars(tt.in)
			if tt.errContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidateCmd_WithVarSubstitution(t *testing.T) {
	cmd := &cli.Command{Commands: []*cli.Command{validateCmd()}}

	stdout, stderr, err := captureOutput(t, func() error {
		return cmd.Run(context.Background(), []string{
			"app",
			"validate",
			"--config-json", `{"secrets":{"X":"ssm:/app/{{.STAGE}}/db"}}`,
			"--var", "STAGE=prod",
			"--debug",
		})
	})

	require.NoError(t, err)
	assert.Contains(t, stdout, `"X": "ssm:/app/prod/db"`)
	assert.Empty(t, stderr)
}

func TestValidateCmd_AllowsUnusedVar(t *testing.T) {
	cmd := &cli.Command{Commands: []*cli.Command{validateCmd()}}

	stdout, stderr, err := captureOutput(t, func() error {
		return cmd.Run(context.Background(), []string{
			"app",
			"validate",
			"--config-json", `{"secrets":{"X":"ssm:/app/{{.STAGE}}/db"}}`,
			"--var", "STAGE=prod",
			"--var", "EXTRA=value",
		})
	})

	require.NoError(t, err)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestValidateCmd_MissingVarFails(t *testing.T) {
	cmd := &cli.Command{Commands: []*cli.Command{validateCmd()}}

	err := cmd.Run(context.Background(), []string{
		"app",
		"validate",
		"--config-json", `{"secrets":{"X":"ssm:/app/{{.STAGE}}/db"}}`,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `map has no entry for key "STAGE"`)
}

func TestFetchCmd_AllowsUnusedVar(t *testing.T) {
	cmd := &cli.Command{Commands: []*cli.Command{fetchCmd()}}

	stdout, stderr, err := captureOutput(t, func() error {
		return cmd.Run(context.Background(), []string{
			"app",
			"fetch",
			"--config-json", `{"secrets":{}}`,
			"--var", "EXTRA=value",
		})
	})

	require.NoError(t, err)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestExecCmd_NoCommand(t *testing.T) {
	cmd := &cli.Command{
		Commands: []*cli.Command{execCmd()},
	}

	// exec with config but no command should error
	err := cmd.Run(context.Background(), []string{"app", "exec", "--config-json", `{"FOO":"ssm:/test"}`})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no command specified")
}

func TestExecCmd_CommandNotFound(t *testing.T) {
	cmd := &cli.Command{
		Commands: []*cli.Command{execCmd()},
	}

	// exec with non-existent command should error
	err := cmd.Run(context.Background(), []string{"app", "exec", "--config-json", `{"secrets":{}}`, "--", "this-command-does-not-exist-12345"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestExecCmd_Integration(t *testing.T) {
	// Build the binary to a temp location
	tmpDir := t.TempDir()
	binary := filepath.Join(tmpDir, "secret-injector")

	buildCmd := exec.Command("go", "build", "-o", binary, ".")
	buildCmd.Dir = "."
	out, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "build failed: %s", string(out))

	// Run exec with empty secrets config, executing 'echo hello'
	// This tests the full flow without needing AWS credentials
	runCmd := exec.Command(binary, "exec", "--config-json", `{"secrets":{}}`, "--", "echo", "hello")
	var stdout, stderr bytes.Buffer
	runCmd.Stdout = &stdout
	runCmd.Stderr = &stderr
	err = runCmd.Run()
	require.NoError(t, err, "exec failed: stderr=%s", stderr.String())
	assert.Equal(t, "hello\n", stdout.String())
}

func TestExecCmd_InheritsEnv(t *testing.T) {
	// Build the binary to a temp location
	tmpDir := t.TempDir()
	binary := filepath.Join(tmpDir, "secret-injector")

	buildCmd := exec.Command("go", "build", "-o", binary, ".")
	buildCmd.Dir = "."
	out, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "build failed: %s", string(out))

	// Run exec and verify it inherits environment
	runCmd := exec.Command(binary, "exec", "--config-json", `{"secrets":{}}`, "--", "printenv", "TEST_INHERIT_VAR")
	runCmd.Env = append(runCmd.Env, "TEST_INHERIT_VAR=inherited_value")
	// Need PATH for printenv to work
	runCmd.Env = append(runCmd.Env, "PATH=/usr/bin:/bin")
	var stdout, stderr bytes.Buffer
	runCmd.Stdout = &stdout
	runCmd.Stderr = &stderr
	err = runCmd.Run()
	require.NoError(t, err, "exec failed: stderr=%s", stderr.String())
	assert.Equal(t, "inherited_value\n", stdout.String())
}

func TestFetchCmd_FormatExport(t *testing.T) {
	// Build the binary to a temp location
	tmpDir := t.TempDir()
	binary := filepath.Join(tmpDir, "secret-injector")

	buildCmd := exec.Command("go", "build", "-o", binary, ".")
	buildCmd.Dir = "."
	out, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "build failed: %s", string(out))

	// Test --format=export with empty secrets
	runCmd := exec.Command(binary, "fetch", "--config-json", `{"secrets":{}}`, "--format=export")
	var stdout, stderr bytes.Buffer
	runCmd.Stdout = &stdout
	runCmd.Stderr = &stderr
	err = runCmd.Run()
	require.NoError(t, err, "fetch --format=export failed: stderr=%s", stderr.String())
	assert.Empty(t, stdout.String())
}

func TestFetchCmd_FormatFlag(t *testing.T) {
	tests := []struct {
		name    string
		format  string
		wantErr bool
	}{
		{name: "env format", format: "env", wantErr: false},
		{name: "json format", format: "json", wantErr: false},
		{name: "export format", format: "export", wantErr: false},
		{name: "invalid format", format: "invalid", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cli.Command{
				Commands: []*cli.Command{fetchCmd()},
			}
			err := cmd.Run(context.Background(), []string{"app", "fetch", "--config-json", `{"secrets":{}}`, "--format=" + tt.format})
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "unknown format")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFetchCmd_DefaultFormat(t *testing.T) {
	cmd := &cli.Command{
		Commands: []*cli.Command{fetchCmd()},
	}

	// No --format flag should use env format (default)
	err := cmd.Run(context.Background(), []string{"app", "fetch", "--config-json", `{"secrets":{}}`})
	require.NoError(t, err)
}
