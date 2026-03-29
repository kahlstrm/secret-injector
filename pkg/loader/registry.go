package loader

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/kahlstrm/secret-injector/internal/aws/config"
	secretsmanager "github.com/kahlstrm/secret-injector/internal/aws/secretsmanager"
	ssm "github.com/kahlstrm/secret-injector/internal/aws/ssm"
)

// Registry maps a source name to its loader.
type Registry map[string]SecretLoader

// New creates a Registry from the provided loaders.
func New(loaders ...SecretLoader) Registry {
	r := make(Registry, len(loaders))
	for _, l := range loaders {
		r.Register(l)
	}
	return r
}

// Register adds or replaces a loader for its Source key.
func (r Registry) Register(l SecretLoader) {
	if l == nil {
		return
	}
	r[l.Source()] = l
}

// Default returns a Registry with the built-in SSM and Secrets Manager loaders
// using shared AWS default credential and region resolution.
// onWarning is optional and can be nil.
func Default(ctx context.Context, onWarning func(context.Context, string)) (Registry, error) {
	cfg, err := awsconfig.LoadDefault(ctx)
	if err != nil {
		return nil, err
	}

	return defaultRegistry(cfg, onWarning), nil
}

func defaultRegistry(cfg aws.Config, onWarning func(context.Context, string)) Registry {
	return New(
		ssm.NewFromAWSConfig(cfg, onWarning),
		secretsmanager.NewFromAWSConfig(cfg, onWarning),
	)
}
