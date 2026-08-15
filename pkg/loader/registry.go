package loader

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/kahlstrm/secret-injector/internal/aws/config"
	awssecretsmanager "github.com/kahlstrm/secret-injector/internal/aws/secretsmanager"
	awsssm "github.com/kahlstrm/secret-injector/internal/aws/ssm"
	gcpsecretmanager "github.com/kahlstrm/secret-injector/internal/gcp/secretmanager"
)

// AWSOptions configures AWS resolution for the built-in providers.
type AWSOptions struct {
	Profile string
	Region  string
}

// GCPOptions configures Google Secret Manager resolution.
type GCPOptions struct {
	// Project qualifies bare secret refs. Full resource names ignore it.
	Project string
	// CredentialsFile overrides Application Default Credentials discovery.
	CredentialsFile string
}

// ErrGCPProjectRequired indicates a bare secret ref was used without a project.
var ErrGCPProjectRequired = gcpsecretmanager.ErrProjectRequired

// ErrAWSRegionRequired indicates that configured AWS resolution has no region.
var ErrAWSRegionRequired = awsconfig.ErrRegionRequired

// DefaultOptions configures the built-in providers.
type DefaultOptions struct {
	AWS AWSOptions
	GCP GCPOptions
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
	providers := defaultProviders(func(ctx context.Context) (aws.Config, error) {
		return awsconfig.Load(ctx, awsconfig.Options{
			Profile: options.AWS.Profile,
			Region:  options.AWS.Region,
		})
	}, options.GCP)
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

func defaultProviders(loadAWSConfig func(context.Context) (aws.Config, error), gcp GCPOptions) []Provider {
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
		providerFunc{
			source: "gcp_secretmanager",
			build: func(_ context.Context, onWarning WarningHandler) (Resolver, error) {
				return gcpsecretmanager.NewResolverFromOptions(gcpsecretmanager.Options{
					Project:         gcp.Project,
					CredentialsFile: gcp.CredentialsFile,
				}, onWarning), nil
			},
		},
	}
}

// ValidateSource reports whether the registry can resolve the given source.
//
// Commands pass this to config.Load so parsing rejects exactly the sources the
// registry cannot serve. Without it, config keeps a second list of backends that
// has to be updated in step, and a backend present in only one of them is either
// rejected before resolution or accepted and then unresolvable.
func (r *Registry) ValidateSource(source string) error {
	if r.hasSource(source) {
		return nil
	}
	available := r.Sources()
	if len(available) == 0 {
		return fmt.Errorf("%w: %s", ErrUnknownSource, source)
	}
	quoted := "'" + strings.Join(available, "', '") + "'"
	return fmt.Errorf("%w %q: supported sources are %s", ErrUnknownSource, source, quoted)
}

// Sources lists the sources the registry can resolve, sorted.
func (r *Registry) Sources() []string {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	sources := make([]string, 0, len(r.providers))
	for source := range r.providers {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	return sources
}

// No Close: every backend's client is HTTP, holding no connection pool or
// goroutines, so a discarded registry leaks nothing.
