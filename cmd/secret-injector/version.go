package main

import "fmt"

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func buildVersion() string {
	return fmt.Sprintf("%s (commit=%s date=%s)", version, commit, date)
}
