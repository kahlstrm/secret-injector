package config

import (
	"context"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
)

// Options selects AWS configuration for the built-in resolvers.
type Options struct {
	Profile string
	Region  string
}

// Loader lazily resolves and caches shared AWS SDK configuration.
type Loader struct {
	mu     sync.Mutex
	load   func(context.Context) (aws.Config, error)
	loaded bool
	cfg    aws.Config
}

// NewLoader returns a lazy AWS config loader. When load is nil, LoadDefault is used.
func NewLoader(load func(context.Context) (aws.Config, error)) *Loader {
	if load == nil {
		load = LoadDefault
	}
	return &Loader{load: load}
}

// Load returns cached AWS SDK config or resolves it on first successful call.
func (l *Loader) Load(ctx context.Context) (aws.Config, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.loaded {
		return l.cfg, nil
	}

	cfg, err := l.load(ctx)
	if err != nil {
		return aws.Config{}, err
	}

	l.cfg = cfg
	l.loaded = true
	return l.cfg, nil
}

// LoadDefault resolves shared AWS SDK configuration for aws_* providers.
func LoadDefault(ctx context.Context) (aws.Config, error) {
	return awscfg.LoadDefaultConfig(ctx)
}

// Load resolves AWS configuration with explicit profile and region overrides.
func Load(ctx context.Context, options Options) (aws.Config, error) {
	var loadOptions []func(*awscfg.LoadOptions) error
	if options.Profile != "" {
		loadOptions = append(loadOptions, awscfg.WithSharedConfigProfile(options.Profile))
	}
	if options.Region != "" {
		loadOptions = append(loadOptions, awscfg.WithRegion(options.Region))
	}
	return awscfg.LoadDefaultConfig(ctx, loadOptions...)
}
