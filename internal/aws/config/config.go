package config

import (
	"context"
	"os"
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
	var settings profileSettings
	if options.Profile != "" {
		profile, err := loadSharedProfile(ctx, options.Profile)
		if err != nil {
			return aws.Config{}, err
		}
		settings = profileSettings{profile: profile, explicitRegion: options.Region}
		loadOptions = append(loadOptions, awscfg.WithSharedConfigProfile(options.Profile))
		loadOptions = append(loadOptions, profileIsolationLoadOptions(profile)...)
		loadOptions = append(loadOptions, credentialEndpointLoadOptions(settings)...)
	}
	if options.Region != "" {
		loadOptions = append(loadOptions, awscfg.WithRegion(options.Region))
	}
	cfg, err := awscfg.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil || options.Profile == "" {
		return cfg, err
	}
	return normalizeProfileConfig(cfg, settings), nil
}

func loadSharedProfile(ctx context.Context, name string) (awscfg.SharedConfig, error) {
	return awscfg.LoadSharedConfigProfile(ctx, name, func(options *awscfg.LoadSharedConfigOptions) {
		if path := os.Getenv("AWS_CONFIG_FILE"); path != "" {
			options.ConfigFiles = []string{path}
		}
		if path := os.Getenv("AWS_SHARED_CREDENTIALS_FILE"); path != "" {
			options.CredentialsFiles = []string{path}
		}
	})
}

func profileIsolationLoadOptions(profile awscfg.SharedConfig) []func(*awscfg.LoadOptions) error {
	fips := profile.UseFIPSEndpoint
	if fips == aws.FIPSEndpointStateUnset {
		fips = aws.FIPSEndpointStateDisabled
	}
	dualStack := profile.UseDualStackEndpoint
	if dualStack == aws.DualStackEndpointStateUnset {
		dualStack = aws.DualStackEndpointStateDisabled
	}
	retryMode := profile.RetryMode
	if retryMode == "" {
		retryMode = aws.RetryModeStandard
	}
	return []func(*awscfg.LoadOptions) error{
		awscfg.WithUseFIPSEndpoint(fips),
		awscfg.WithUseDualStackEndpoint(dualStack),
		awscfg.WithRetryMode(retryMode),
	}
}

func normalizeProfileConfig(cfg aws.Config, settings profileSettings) aws.Config {
	if settings.explicitRegion == "" {
		cfg.Region = settings.profile.Region
	}
	cfg.BaseEndpoint = nil
	if settings.profile.IgnoreConfiguredEndpoints == nil || !*settings.profile.IgnoreConfiguredEndpoints {
		if settings.profile.BaseEndpoint != "" {
			cfg.BaseEndpoint = aws.String(settings.profile.BaseEndpoint)
		}
	}
	return cfg
}

// ProfileEndpoint returns the selected profile's endpoint for service.
// The boolean reports whether an explicit profile was selected.
func ProfileEndpoint(cfg aws.Config, service string) (*string, bool) {
	if !hasSelectedProfile(cfg.ConfigSources) {
		return nil, false
	}
	profile, _ := sharedProfile(cfg.ConfigSources)
	settings := profileSettings{profile: profile}
	return settings.endpoint(service), true
}

func hasSelectedProfile(sources []any) bool {
	for _, source := range sources {
		switch source := source.(type) {
		case awscfg.LoadOptions:
			return source.SharedConfigProfile != ""
		case *awscfg.LoadOptions:
			return source.SharedConfigProfile != ""
		}
	}
	return false
}

func sharedProfile(sources []any) (awscfg.SharedConfig, bool) {
	for _, source := range sources {
		switch source := source.(type) {
		case awscfg.SharedConfig:
			return source, true
		case *awscfg.SharedConfig:
			return *source, true
		}
	}
	return awscfg.SharedConfig{}, false
}
