//go:build integration

package ssm

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/kahlstrm/secret-injector/internal/testutil"
	"github.com/kahlstrm/secret-injector/internal/testutil/resolvercontract"
	"github.com/stretchr/testify/require"
)

type batchFailingClient struct {
	ssmClient
}

func (c batchFailingClient) GetParameters(context.Context, *awsssm.GetParametersInput, ...func(*awsssm.Options)) (*awsssm.GetParametersOutput, error) {
	return nil, errors.New("forced batch failure")
}

func TestIntegration_SSMResolverContract(t *testing.T) {
	ctx := context.Background()
	ls := testutil.MustSetupLocalStack(t, ctx)
	cfg := ls.MustAWSConfig(t, ctx)
	client := awsssm.NewFromConfig(cfg)
	resolver := NewResolverFromAWSConfig(cfg, nil)
	seed := func(ctx context.Context, secret resolvercontract.Secret) error {
		_, err := client.PutParameter(ctx, &awsssm.PutParameterInput{
			Name:      aws.String(secret.Ref),
			Type:      ssmtypes.ParameterTypeString,
			Value:     aws.String(secret.Value),
			Overwrite: aws.Bool(true),
		})
		return err
	}

	resolvercontract.Run(t, resolvercontract.Fixture{
		Resolve:         resolver.Resolve,
		FallbackResolve: NewResolver(batchFailingClient{ssmClient: client}, nil).Resolve,
		Seed:            seed,
	})

	t.Run("slash-prefixed refs", func(t *testing.T) {
		secrets := []resolvercontract.Secret{
			{Ref: "/it/resolver-contract/p1", Value: "path-value-1"},
			{Ref: "/it/resolver-contract/p2", Value: "path-value-2"},
		}
		expected := make(map[string]string, len(secrets))
		refs := make([]string, 0, len(secrets))
		for _, secret := range secrets {
			require.NoError(t, seed(t.Context(), secret))
			expected[secret.Ref] = secret.Value
			refs = append(refs, secret.Ref)
		}

		actual, err := resolver.Resolve(t.Context(), refs)

		require.NoError(t, err)
		require.Equal(t, expected, actual)
	})
}
