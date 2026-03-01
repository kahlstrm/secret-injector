package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildVersion(t *testing.T) {
	origVersion := version
	origCommit := commit
	origDate := date
	t.Cleanup(func() {
		version = origVersion
		commit = origCommit
		date = origDate
	})

	version = "1.2.3"
	commit = "abc123"
	date = "2026-02-28T00:00:00Z"

	assert.Equal(t, "1.2.3 (commit=abc123 date=2026-02-28T00:00:00Z)", buildVersion())
}

func TestVersionFlag_WithBuildMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	binary := filepath.Join(tmpDir, "secret-injector")

	ldflags := "-X main.version=1.2.3 -X main.commit=abc123 -X main.date=2026-02-28T00:00:00Z"
	buildCmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", binary, ".")
	buildOut, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "build failed: %s", string(buildOut))

	runCmd := exec.Command(binary, "--version")
	runOut, err := runCmd.CombinedOutput()
	require.NoError(t, err, "version command failed: %s", string(runOut))

	assert.Equal(t, "secret-injector version 1.2.3 (commit=abc123 date=2026-02-28T00:00:00Z)", strings.TrimSpace(string(runOut)))
}
