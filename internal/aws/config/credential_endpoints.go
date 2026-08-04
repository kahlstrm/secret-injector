package config

import (
	"context"
	"slices"

	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/ssocreds"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sso"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

func credentialEndpointLoadOptions(settings profileSettings) []func(*awscfg.LoadOptions) error {
	return []func(*awscfg.LoadOptions) error{
		awscfg.WithAssumeRoleCredentialOptions(func(options *stscreds.AssumeRoleOptions) {
			if options.Client != nil {
				options.Client = &assumeRoleClient{next: options.Client, settings: settings}
			}
		}),
		awscfg.WithWebIdentityRoleCredentialOptions(func(options *stscreds.WebIdentityRoleOptions) {
			if options.Client != nil {
				options.Client = &webIdentityClient{next: options.Client, settings: settings}
			}
		}),
		awscfg.WithSSOProviderOptions(func(options *ssocreds.Options) {
			if options.Client != nil {
				options.Client = &ssoClient{next: options.Client, settings: settings}
			}
		}),
		awscfg.WithSSOTokenProviderOptions(func(options *ssocreds.SSOTokenProviderOptions) {
			options.ClientOptions = appendFinalOption(options.ClientOptions, ssoOIDCEndpointOption(settings))
		}),
	}
}

type assumeRoleClient struct {
	next     stscreds.AssumeRoleAPIClient
	settings profileSettings
}

func (c *assumeRoleClient) AssumeRole(ctx context.Context, input *sts.AssumeRoleInput, options ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	return c.next.AssumeRole(ctx, input, appendFinalOption(options, stsEndpointOption(c.settings))...)
}

type webIdentityClient struct {
	next     stscreds.AssumeRoleWithWebIdentityAPIClient
	settings profileSettings
}

func (c *webIdentityClient) AssumeRoleWithWebIdentity(ctx context.Context, input *sts.AssumeRoleWithWebIdentityInput, options ...func(*sts.Options)) (*sts.AssumeRoleWithWebIdentityOutput, error) {
	return c.next.AssumeRoleWithWebIdentity(ctx, input, appendFinalOption(options, stsEndpointOption(c.settings))...)
}

type ssoClient struct {
	next     ssocreds.GetRoleCredentialsAPIClient
	settings profileSettings
}

func (c *ssoClient) GetRoleCredentials(ctx context.Context, input *sso.GetRoleCredentialsInput, options ...func(*sso.Options)) (*sso.GetRoleCredentialsOutput, error) {
	return c.next.GetRoleCredentials(ctx, input, appendFinalOption(options, ssoEndpointOption(c.settings))...)
}

func stsEndpointOption(settings profileSettings) func(*sts.Options) {
	return func(options *sts.Options) {
		options.BaseEndpoint = settings.endpoint(sts.ServiceID)
		options.Region = settings.region
	}
}

func ssoEndpointOption(settings profileSettings) func(*sso.Options) {
	return func(options *sso.Options) {
		options.BaseEndpoint = settings.endpoint(sso.ServiceID)
	}
}

func ssoOIDCEndpointOption(settings profileSettings) func(*ssooidc.Options) {
	return func(options *ssooidc.Options) {
		options.BaseEndpoint = settings.endpoint(ssooidc.ServiceID)
	}
}

// appendFinalOption copies before appending so the caller's slice is not
// mutated through a shared backing array.
func appendFinalOption[T any](options []func(*T), final func(*T)) []func(*T) {
	return append(slices.Clone(options), final)
}
