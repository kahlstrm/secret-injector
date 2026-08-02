package main

import (
	"os/exec"
	"path/filepath"
	"runtime/debug"
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

func TestResolveVersion(t *testing.T) {
	moduleInfo := &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}

	tests := []struct {
		name          string
		linkerVersion string
		info          *debug.BuildInfo
		want          string
	}{
		{
			name:          "module installed binary",
			linkerVersion: "dev",
			info:          moduleInfo,
			want:          "1.2.3",
		},
		{
			name:          "development build without module version",
			linkerVersion: "dev",
			info:          &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}},
			want:          "dev",
		},
		{
			name:          "module pseudo-version",
			linkerVersion: "dev",
			info:          &debug.BuildInfo{Main: debug.Module{Version: "v1.2.4-0.20260802120000-abc123def456"}},
			want:          "1.2.4-0.20260802120000-abc123def456",
		},
		{
			name:          "linker metadata wins",
			linkerVersion: "2.0.0",
			info:          moduleInfo,
			want:          "2.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, resolveVersion(tt.linkerVersion, tt.info))
		})
	}
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
