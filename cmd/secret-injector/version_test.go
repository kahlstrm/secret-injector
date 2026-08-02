package main

import (
	"debug/buildinfo"
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
			linkerVersion: "",
			info:          moduleInfo,
			want:          "1.2.3",
		},
		{
			name:          "development build without module version",
			linkerVersion: "",
			info:          &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}},
			want:          "dev",
		},
		{
			name:          "missing build info",
			linkerVersion: "",
			info:          nil,
			want:          "dev",
		},
		{
			name:          "empty module version",
			linkerVersion: "",
			info:          &debug.BuildInfo{},
			want:          "dev",
		},
		{
			name:          "module pseudo-version",
			linkerVersion: "",
			info:          &debug.BuildInfo{Main: debug.Module{Version: "v1.2.4-0.20260802120000-abc123def456"}},
			want:          "1.2.4-0.20260802120000-abc123def456",
		},
		{
			name:          "explicit dev linker metadata wins",
			linkerVersion: "dev",
			info:          moduleInfo,
			want:          "dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, resolveVersion(tt.linkerVersion, tt.info))
		})
	}
}

func TestVersionFlag_WithBuildMetadata(t *testing.T) {
	ldflags := "-X main.version=1.2.3 -X main.commit=abc123 -X main.date=2026-02-28T00:00:00Z"
	binary := buildVersionBinary(t, ldflags)

	runCmd := exec.Command(binary, "--version")
	runOut, err := runCmd.CombinedOutput()
	require.NoError(t, err, "version command failed: %s", string(runOut))

	assert.Equal(t, "secret-injector version 1.2.3 (commit=abc123 date=2026-02-28T00:00:00Z)", strings.TrimSpace(string(runOut)))
}

func TestVersionFlag_WithoutBuildMetadata(t *testing.T) {
	binary := buildVersionBinary(t, "")
	info, err := buildinfo.ReadFile(binary)
	require.NoError(t, err)

	wantVersion := "dev"
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		wantVersion = strings.TrimPrefix(info.Main.Version, "v")
	}

	runCmd := exec.Command(binary, "--version")
	runOut, err := runCmd.CombinedOutput()
	require.NoError(t, err, "version command failed: %s", string(runOut))

	want := "secret-injector version " + wantVersion + " (commit=none date=unknown)"
	assert.Equal(t, want, strings.TrimSpace(string(runOut)))
}

func buildVersionBinary(t *testing.T, ldflags string) string {
	t.Helper()

	binary := filepath.Join(t.TempDir(), "secret-injector")
	args := []string{"build"}
	if ldflags != "" {
		args = append(args, "-ldflags", ldflags)
	}
	args = append(args, "-o", binary, ".")
	buildOut, err := exec.Command("go", args...).CombinedOutput()
	require.NoError(t, err, "build failed: %s", string(buildOut))
	return binary
}
