package config

import (
	"context"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
)

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
