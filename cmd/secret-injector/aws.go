package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/kahlstrm/secret-injector/pkg/config"
	"github.com/kahlstrm/secret-injector/pkg/loader"
	"github.com/urfave/cli/v3"
)

const (
	awsProfileEnv = "SECRET_INJECTOR_AWS_PROFILE"
	awsRegionEnv  = "SECRET_INJECTOR_AWS_REGION"
)

var (
	awsEnvironmentMu = sync.Mutex{}
	awsIsolatedEnv   = []string{
		"AWS_REGION",
		"AWS_DEFAULT_REGION",
		"AWS_IGNORE_CONFIGURED_ENDPOINT_URLS",
	}
)

type environmentValue struct {
	value string
	set   bool
}

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

func resolveSecrets(ctx context.Context, c *cli.Command, cfg config.Config) (map[string]string, error) {
	options := awsOptions(c)
	restore, err := scopeAWSConfig(options)
	if err != nil {
		return nil, err
	}

	registry := loader.DefaultWithOptions(warnToStderr, loader.DefaultOptions{AWS: options})
	values, resolveErr := registry.ResolveAll(ctx, cfg)
	restoreErr := restore()
	if err := errors.Join(resolveErr, restoreErr); err != nil {
		return nil, err
	}
	return values, nil
}

func scopeAWSConfig(options loader.AWSOptions) (func() error, error) {
	if options.Profile == "" {
		return func() error { return nil }, nil
	}

	awsEnvironmentMu.Lock()
	names := append([]string(nil), awsIsolatedEnv...)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "AWS_ENDPOINT_URL") {
			names = append(names, name)
		}
	}

	original := make(map[string]environmentValue, len(names))
	for _, name := range names {
		value, set := os.LookupEnv(name)
		original[name] = environmentValue{value: value, set: set}
		if err := os.Unsetenv(name); err != nil {
			_ = restoreEnvironment(original)
			awsEnvironmentMu.Unlock()
			return nil, fmt.Errorf("isolate AWS environment: %w", err)
		}
	}

	return func() error {
		err := restoreEnvironment(original)
		awsEnvironmentMu.Unlock()
		if err != nil {
			return fmt.Errorf("restore AWS environment: %w", err)
		}
		return nil
	}, nil
}

func restoreEnvironment(values map[string]environmentValue) error {
	var restoreErr error
	for name, original := range values {
		var err error
		if original.set {
			err = os.Setenv(name, original.value)
		} else {
			err = os.Unsetenv(name)
		}
		if err != nil {
			restoreErr = errors.Join(restoreErr, err)
		}
	}
	return restoreErr
}
