package main

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

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
