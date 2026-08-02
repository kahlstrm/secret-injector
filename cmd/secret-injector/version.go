package main

import (
	"fmt"
	"runtime/debug"
	"strings"
)

var (
	version = ""
	commit  = "none"
	date    = "unknown"
)

func buildVersion() string {
	info, _ := debug.ReadBuildInfo()
	return fmt.Sprintf("%s (commit=%s date=%s)", resolveVersion(version, info), commit, date)
}

func resolveVersion(linkerVersion string, info *debug.BuildInfo) string {
	if linkerVersion != "" {
		return linkerVersion
	}
	if info == nil || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return "dev"
	}
	return strings.TrimPrefix(info.Main.Version, "v")
}
