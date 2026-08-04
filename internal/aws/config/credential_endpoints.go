package config

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/ssocreds"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sso"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

type profileSettings struct {
	profile        awscfg.SharedConfig
	explicitRegion string
}

func (s profileSettings) endpoint(service string) *string {
	if s.profile.IgnoreConfiguredEndpoints != nil && *s.profile.IgnoreConfiguredEndpoints {
		return nil
	}
	if endpoint, found, _ := s.profile.GetServiceBaseEndpoint(context.Background(), service); found {
		return aws.String(endpoint)
	}
	if s.profile.BaseEndpoint != "" {
		return aws.String(s.profile.BaseEndpoint)
	}
	return nil
}

func (s profileSettings) region() string {
	if s.explicitRegion != "" {
		return s.explicitRegion
	}
	return s.profile.Region
}

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
			options.ClientOptions = appendFinalOption(options.ClientOptions, ssoOIDCEndpointOption(settings, ssooidc.ServiceID))
		}),
	}
}

type assumeRoleClient struct {
	next     stscreds.AssumeRoleAPIClient
	settings profileSettings
}

func (c *assumeRoleClient) AssumeRole(ctx context.Context, input *sts.AssumeRoleInput, options ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	return c.next.AssumeRole(ctx, input, appendFinalOption(options, stsEndpointOption(c.settings, sts.ServiceID))...)
}

type webIdentityClient struct {
	next     stscreds.AssumeRoleWithWebIdentityAPIClient
	settings profileSettings
}

func (c *webIdentityClient) AssumeRoleWithWebIdentity(ctx context.Context, input *sts.AssumeRoleWithWebIdentityInput, options ...func(*sts.Options)) (*sts.AssumeRoleWithWebIdentityOutput, error) {
	return c.next.AssumeRoleWithWebIdentity(ctx, input, appendFinalOption(options, stsEndpointOption(c.settings, sts.ServiceID))...)
}

type ssoClient struct {
	next     ssocreds.GetRoleCredentialsAPIClient
	settings profileSettings
}

func (c *ssoClient) GetRoleCredentials(ctx context.Context, input *sso.GetRoleCredentialsInput, options ...func(*sso.Options)) (*sso.GetRoleCredentialsOutput, error) {
	return c.next.GetRoleCredentials(ctx, input, appendFinalOption(options, ssoEndpointOption(c.settings, sso.ServiceID))...)
}

func stsEndpointOption(settings profileSettings, service string) func(*sts.Options) {
	return func(options *sts.Options) {
		options.BaseEndpoint = settings.endpoint(service)
		if region := settings.region(); region != "" {
			options.Region = region
		}
	}
}

func ssoEndpointOption(settings profileSettings, service string) func(*sso.Options) {
	return func(options *sso.Options) {
		options.BaseEndpoint = settings.endpoint(service)
	}
}

func ssoOIDCEndpointOption(settings profileSettings, service string) func(*ssooidc.Options) {
	return func(options *ssooidc.Options) {
		options.BaseEndpoint = settings.endpoint(service)
	}
}

func appendFinalOption[T any](options []func(*T), final func(*T)) []func(*T) {
	result := make([]func(*T), 0, len(options)+1)
	result = append(result, options...)
	return append(result, final)
}
