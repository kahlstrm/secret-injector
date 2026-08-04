package main

import (
	"github.com/urfave/cli/v3"

	"github.com/kahlstrm/secret-injector/pkg/loader"
)

const (
	awsProfileEnv = "SECRET_INJECTOR_AWS_PROFILE"
	awsRegionEnv  = "SECRET_INJECTOR_AWS_REGION"
)

func awsFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "aws-profile",
			Usage:   "AWS shared config profile for secret resolution",
			Sources: cli.EnvVars(awsProfileEnv),
		},
		&cli.StringFlag{
			Name:    "aws-region",
			Usage:   "AWS region for secret resolution",
			Sources: cli.EnvVars(awsRegionEnv),
		},
	}
}

func awsOptions(c *cli.Command) loader.AWSOptions {
	return loader.AWSOptions{
		Profile: c.String("aws-profile"),
		Region:  c.String("aws-region"),
	}
}
