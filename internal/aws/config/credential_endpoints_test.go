package config

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/ssocreds"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sso"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/stretchr/testify/assert"
)

func TestCredentialClientsApplyFinalEndpoint(t *testing.T) {
	want := "http://profile.example"
	settings := profileSettings{
		profile: awscfg.SharedConfig{BaseEndpoint: want},
		region:  "eu-west-1",
	}

	t.Run("assume role", func(t *testing.T) {
		client := &fakeAssumeRoleClient{}
		wrapped := &assumeRoleClient{next: client, settings: settings}

		_, _ = wrapped.AssumeRole(context.Background(), &sts.AssumeRoleInput{})

		assert.Equal(t, want, client.endpoint)
		assert.Equal(t, "eu-west-1", client.region)
	})

	t.Run("web identity", func(t *testing.T) {
		client := &fakeWebIdentityClient{}
		wrapped := &webIdentityClient{next: client, settings: settings}

		_, _ = wrapped.AssumeRoleWithWebIdentity(context.Background(), &sts.AssumeRoleWithWebIdentityInput{})

		assert.Equal(t, want, client.endpoint)
	})

	t.Run("sso", func(t *testing.T) {
		client := &fakeSSOClient{}
		wrapped := &ssoClient{next: client, settings: settings}

		_, _ = wrapped.GetRoleCredentials(context.Background(), &sso.GetRoleCredentialsInput{})

		assert.Equal(t, want, client.endpoint)
	})

	t.Run("sso oidc", func(t *testing.T) {
		var options ssooidc.Options
		options.BaseEndpoint = aws.String("http://ambient.example")

		ssoOIDCEndpointOption(settings)(&options)

		assert.Equal(t, want, aws.ToString(options.BaseEndpoint))
	})
}

func TestCredentialClientsClearAmbientEndpoint(t *testing.T) {
	client := &fakeAssumeRoleClient{}
	wrapped := &assumeRoleClient{
		next: client,
	}

	_, _ = wrapped.AssumeRole(context.Background(), &sts.AssumeRoleInput{})

	assert.Empty(t, client.endpoint)
}

func TestCredentialEndpointLoadOptionsWrapSDKClients(t *testing.T) {
	want := "http://profile.example"
	settings := profileSettings{profile: awscfg.SharedConfig{BaseEndpoint: want}}
	var loadOptions awscfg.LoadOptions
	for _, option := range credentialEndpointLoadOptions(settings) {
		assert.NoError(t, option(&loadOptions))
	}

	var validationOptions stscreds.AssumeRoleOptions
	loadOptions.AssumeRoleCredentialOptions(&validationOptions)
	assert.Nil(t, validationOptions.Client)

	assumeRole := &fakeAssumeRoleClient{}
	assumeRoleOptions := stscreds.AssumeRoleOptions{Client: assumeRole}
	loadOptions.AssumeRoleCredentialOptions(&assumeRoleOptions)
	_, _ = assumeRoleOptions.Client.AssumeRole(context.Background(), &sts.AssumeRoleInput{})
	assert.Equal(t, want, assumeRole.endpoint)

	webIdentity := &fakeWebIdentityClient{}
	webIdentityOptions := stscreds.WebIdentityRoleOptions{Client: webIdentity}
	loadOptions.WebIdentityRoleCredentialOptions(&webIdentityOptions)
	_, _ = webIdentityOptions.Client.AssumeRoleWithWebIdentity(context.Background(), &sts.AssumeRoleWithWebIdentityInput{})
	assert.Equal(t, want, webIdentity.endpoint)

	ssoRole := &fakeSSOClient{}
	ssoOptions := ssocreds.Options{Client: ssoRole}
	loadOptions.SSOProviderOptions(&ssoOptions)
	_, _ = ssoOptions.Client.GetRoleCredentials(context.Background(), &sso.GetRoleCredentialsInput{})
	assert.Equal(t, want, ssoRole.endpoint)

	var tokenOptions ssocreds.SSOTokenProviderOptions
	loadOptions.SSOTokenProviderOptions(&tokenOptions)
	resolved := ssooidc.Options{BaseEndpoint: aws.String("http://ambient.example")}
	for _, option := range tokenOptions.ClientOptions {
		option(&resolved)
	}
	assert.Equal(t, want, aws.ToString(resolved.BaseEndpoint))
}

func TestProfileSettingsEndpoint(t *testing.T) {
	ignored := true
	tests := []struct {
		name    string
		profile awscfg.SharedConfig
		service string
		want    string
	}{
		{name: "standard endpoint", service: "SSM"},
		{name: "global profile endpoint", profile: awscfg.SharedConfig{BaseEndpoint: "http://global.example"}, service: "SSM", want: "http://global.example"},
		{
			name: "service endpoint",
			profile: awscfg.SharedConfig{
				BaseEndpoint: "http://global.example",
				Services: awscfg.Services{ServiceValues: map[string]map[string]string{
					"ssm": {"endpoint_url": "http://ssm.example"},
				}},
			},
			service: "SSM",
			want:    "http://ssm.example",
		},
		{name: "configured endpoints ignored", profile: awscfg.SharedConfig{BaseEndpoint: "http://global.example", IgnoreConfiguredEndpoints: &ignored}, service: "SSM"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := profileSettings{profile: test.profile}
			assert.Equal(t, test.want, aws.ToString(settings.endpoint(test.service)))
		})
	}
}

type fakeAssumeRoleClient struct {
	endpoint string
	region   string
}

func (c *fakeAssumeRoleClient) AssumeRole(_ context.Context, _ *sts.AssumeRoleInput, options ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	c.endpoint, c.region = applySTSOptions(options)
	return &sts.AssumeRoleOutput{}, nil
}

type fakeWebIdentityClient struct{ endpoint string }

func (c *fakeWebIdentityClient) AssumeRoleWithWebIdentity(_ context.Context, _ *sts.AssumeRoleWithWebIdentityInput, options ...func(*sts.Options)) (*sts.AssumeRoleWithWebIdentityOutput, error) {
	c.endpoint, _ = applySTSOptions(options)
	return &sts.AssumeRoleWithWebIdentityOutput{}, nil
}

type fakeSSOClient struct{ endpoint string }

func (c *fakeSSOClient) GetRoleCredentials(_ context.Context, _ *sso.GetRoleCredentialsInput, options ...func(*sso.Options)) (*sso.GetRoleCredentialsOutput, error) {
	resolved := sso.Options{BaseEndpoint: aws.String("http://ambient.example")}
	for _, option := range options {
		option(&resolved)
	}
	c.endpoint = aws.ToString(resolved.BaseEndpoint)
	return &sso.GetRoleCredentialsOutput{}, nil
}

func applySTSOptions(options []func(*sts.Options)) (string, string) {
	resolved := sts.Options{BaseEndpoint: aws.String("http://ambient.example"), Region: "ambient-region"}
	for _, option := range options {
		option(&resolved)
	}
	return aws.ToString(resolved.BaseEndpoint), resolved.Region
}
