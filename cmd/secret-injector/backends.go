package main

import (
	"slices"

	"github.com/urfave/cli/v3"

	"github.com/kahlstrm/secret-injector/pkg/loader"
)

// backendFlags gathers every backend's flags, so a new backend is wired into each
// command in one place.
func backendFlags() []cli.Flag {
	return slices.Concat(awsFlags(), gcpFlags())
}

func backendOptions(c *cli.Command) loader.DefaultOptions {
	return loader.DefaultOptions{
		AWS: awsOptions(c),
		GCP: gcpOptions(c),
	}
}
