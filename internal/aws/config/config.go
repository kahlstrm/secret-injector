package config

import (
	"cmp"
	"context"
	"errors"
	"fmt"
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

// ErrRegionRequired indicates that configured AWS resolution has no region.
var ErrRegionRequired = errors.New("AWS region is required")

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
	if options.Profile == "" {
		if options.Region == "" {
			return awscfg.LoadDefaultConfig(ctx)
		}
		return awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(options.Region))
	}

	profile, err := loadSharedProfile(ctx, options.Profile)
	if err != nil {
		return aws.Config{}, err
	}
	region := cmp.Or(options.Region, profile.Region)
	if region == "" {
		return aws.Config{}, fmt.Errorf("%w: selected AWS profile has no region; set an explicit region", ErrRegionRequired)
	}

	settings := profileSettings{profile: profile, region: region}
	loadOptions := []func(*awscfg.LoadOptions) error{
		awscfg.WithSharedConfigProfile(options.Profile),
		awscfg.WithRegion(region),
	}
	loadOptions = append(loadOptions, profileIsolationLoadOptions(profile)...)
	loadOptions = append(loadOptions, credentialEndpointLoadOptions(settings)...)

	cfg, err := awscfg.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return aws.Config{}, err
	}
	return normalizeProfileConfig(cfg, settings), nil
}

// profileSettings is the selected profile plus the region that wins for it.
type profileSettings struct {
	profile awscfg.SharedConfig
	region  string
}

func (s profileSettings) ignoresConfiguredEndpoints() bool {
	return s.profile.IgnoreConfiguredEndpoints != nil && *s.profile.IgnoreConfiguredEndpoints
}

// baseEndpoint returns the profile's global endpoint, if it defines one.
func (s profileSettings) baseEndpoint() *string {
	if s.ignoresConfiguredEndpoints() || s.profile.BaseEndpoint == "" {
		return nil
	}
	return aws.String(s.profile.BaseEndpoint)
}

// endpoint returns the profile's endpoint for service, preferring a
// service-specific entry over the profile's global endpoint.
func (s profileSettings) endpoint(service string) *string {
	if s.ignoresConfiguredEndpoints() {
		return nil
	}
	if endpoint, found, _ := s.profile.GetServiceBaseEndpoint(context.Background(), service); found {
		return aws.String(endpoint)
	}
	return s.baseEndpoint()
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

// profileIsolationLoadOptions pins the settings the SDK resolves eagerly into
// aws.Config, which withoutEnvConfig cannot undo afterwards. Unset profile
// values fall back to SDK defaults rather than to the ambient environment.
func profileIsolationLoadOptions(profile awscfg.SharedConfig) []func(*awscfg.LoadOptions) error {
	return []func(*awscfg.LoadOptions) error{
		awscfg.WithUseFIPSEndpoint(cmp.Or(profile.UseFIPSEndpoint, aws.FIPSEndpointStateDisabled)),
		awscfg.WithUseDualStackEndpoint(cmp.Or(profile.UseDualStackEndpoint, aws.DualStackEndpointStateDisabled)),
		awscfg.WithRetryMode(cmp.Or(profile.RetryMode, aws.RetryModeStandard)),
	}
}

func normalizeProfileConfig(cfg aws.Config, settings profileSettings) aws.Config {
	cfg.Region = settings.region
	cfg.BaseEndpoint = settings.baseEndpoint()
	cfg.ConfigSources = withoutEnvConfig(cfg.ConfigSources)
	return cfg
}

// withoutEnvConfig drops the ambient environment source so service clients
// resolve their endpoints from the selected profile instead of AWS_ENDPOINT_URL*.
// Clients re-read these sources when they are constructed, so filtering the
// loaded config is what keeps every client isolated, not only the ones we build.
func withoutEnvConfig(sources []any) []any {
	filtered := make([]any, 0, len(sources))
	for _, source := range sources {
		switch source.(type) {
		case awscfg.EnvConfig, *awscfg.EnvConfig:
			continue
		}
		filtered = append(filtered, source)
	}
	return filtered
}
