package main

import (
	"github.com/urfave/cli/v3"

	"github.com/kahlstrm/secret-injector/pkg/loader"
)

const (
	gcpProjectEnv         = "SECRET_INJECTOR_GCP_PROJECT"
	gcpCredentialsFileEnv = "SECRET_INJECTOR_GCP_CREDENTIALS_FILE"
	// Neither ADC login nor Cloud Run exports it; listed last so an explicit
	// setting wins.
	googleProjectEnv = "GOOGLE_CLOUD_PROJECT"
)

func gcpFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "gcp-project",
			Usage:   "Google Cloud project qualifying bare Secret Manager refs",
			Sources: cli.EnvVars(gcpProjectEnv, googleProjectEnv),
		},
		&cli.StringFlag{
			Name:    "gcp-credentials-file",
			Usage:   "Service account key file for Secret Manager, overriding Application Default Credentials",
			Sources: cli.EnvVars(gcpCredentialsFileEnv),
		},
	}
}

func gcpOptions(c *cli.Command) loader.GCPOptions {
	return loader.GCPOptions{
		Project:         c.String("gcp-project"),
		CredentialsFile: c.String("gcp-credentials-file"),
	}
}
