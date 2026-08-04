package testutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// WriteSharedConfig writes contents as an AWS shared config file and returns its
// path, for tests that pass the location to a subprocess.
func WriteSharedConfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

// UseSharedConfig writes contents as the AWS shared config for the current test
// and points the SDK at it. The credentials file is redirected to an empty path
// as well, so a developer's real ~/.aws files cannot influence the result.
func UseSharedConfig(t *testing.T, contents string) string {
	t.Helper()

	path := WriteSharedConfig(t, contents)
	t.Setenv("AWS_CONFIG_FILE", path)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "credentials"))
	return path
}
