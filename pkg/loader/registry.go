package loader

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/kahlstrm/secret-injector/internal/aws/config"
	awssecretsmanager "github.com/kahlstrm/secret-injector/internal/aws/secretsmanager"
	awsssm "github.com/kahlstrm/secret-injector/internal/aws/ssm"
)

// AWSOptions configures AWS resolution for the built-in providers.
type AWSOptions struct {
	Profile string
	Region  string
}

// ErrAWSRegionRequired indicates that configured AWS resolution has no region.
var ErrAWSRegionRequired = errors.New("AWS region is required")

// DefaultOptions configures the built-in providers.
type DefaultOptions struct {
	AWS AWSOptions
}

// New creates a registry from the provided providers.
// Later providers replace earlier providers with the same Source.
func New(onWarning WarningHandler, providers ...Provider) *Registry {
	r := &Registry{
		onWarning: onWarning,
		providers: make(map[string]Provider, len(providers)),
		resolvers: make(map[string]Resolver, len(providers)),
	}
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		r.providers[provider.Source()] = provider
	}
	return r
}

// Default creates a registry containing the built-in providers plus any extras.
// Extras replace built-ins when they use the same source.
func Default(onWarning WarningHandler, extra ...Provider) *Registry {
	return DefaultWithOptions(onWarning, DefaultOptions{}, extra...)
}

// DefaultWithOptions creates a configured registry containing the built-in
// providers plus any extras.
func DefaultWithOptions(onWarning WarningHandler, options DefaultOptions, extra ...Provider) *Registry {
	loadAWSConfig := awsconfig.LoadDefault
	if options.AWS != (AWSOptions{}) {
		loadAWSConfig = func(ctx context.Context) (aws.Config, error) {
			cfg, err := awsconfig.Load(ctx, awsconfig.Options{
				Profile: options.AWS.Profile,
				Region:  options.AWS.Region,
			})
			if err != nil {
				return aws.Config{}, err
			}
			if cfg.Region == "" {
				return aws.Config{}, fmt.Errorf("%w: selected AWS profile has no region; set an explicit region", ErrAWSRegionRequired)
			}
			return cfg, nil
		}
	}

	providers := defaultProviders(loadAWSConfig)
	providers = append(providers, extra...)
	return New(onWarning, providers...)
}

type providerFunc struct {
	source string
	build  func(context.Context, WarningHandler) (Resolver, error)
}

func (p providerFunc) Source() string { return p.source }

func (p providerFunc) Build(ctx context.Context, onWarning WarningHandler) (Resolver, error) {
	return p.build(ctx, onWarning)
}

func defaultProviders(loadAWSConfig func(context.Context) (aws.Config, error)) []Provider {
	sharedAWSConfig := awsconfig.NewLoader(loadAWSConfig)

	return []Provider{
		providerFunc{
			source: "aws_ssm",
			build: func(ctx context.Context, onWarning WarningHandler) (Resolver, error) {
				cfg, err := sharedAWSConfig.Load(ctx)
				if err != nil {
					return nil, err
				}
				return awsssm.NewResolverFromAWSConfig(cfg, onWarning), nil
			},
		},
		providerFunc{
			source: "aws_secretsmanager",
			build: func(ctx context.Context, onWarning WarningHandler) (Resolver, error) {
				cfg, err := sharedAWSConfig.Load(ctx)
				if err != nil {
					return nil, err
				}
				return awssecretsmanager.NewResolverFromAWSConfig(cfg, onWarning), nil
			},
		},
	}
}
