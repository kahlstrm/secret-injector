//go:build integration

package secretsmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssecretsmanager "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/kahlstrm/secret-injector/internal/testutil"
	"github.com/kahlstrm/secret-injector/internal/testutil/resolvercontract"
	"github.com/stretchr/testify/require"
)

type batchFailingClient struct {
	secretsManagerClient
}

func (c batchFailingClient) BatchGetSecretValue(context.Context, *awssecretsmanager.BatchGetSecretValueInput, ...func(*awssecretsmanager.Options)) (*awssecretsmanager.BatchGetSecretValueOutput, error) {
	return nil, errors.New("forced batch failure")
}

type batchItemFailingClient struct {
	secretsManagerClient
}

func (c batchItemFailingClient) BatchGetSecretValue(_ context.Context, in *awssecretsmanager.BatchGetSecretValueInput, _ ...func(*awssecretsmanager.Options)) (*awssecretsmanager.BatchGetSecretValueOutput, error) {
	return &awssecretsmanager.BatchGetSecretValueOutput{Errors: []smtypes.APIErrorType{
		{ErrorCode: aws.String("AccessDeniedException"), SecretId: aws.String(in.SecretIdList[0])},
	}}, nil
}

func TestIntegration_SecretsManagerResolverContract(t *testing.T) {
	ctx := context.Background()
	ls := testutil.MustSetupLocalStack(t, ctx)
	cfg := ls.MustAWSConfig(t, ctx)
	client := awssecretsmanager.NewFromConfig(cfg)

	seededValues := map[string]string{
		"resolver-contract-sm-p1": "value-1",
		"resolver-contract-sm-p2": "value-2",
	}
	values := make(map[string]string, len(seededValues))
	for name, value := range seededValues {
		out, err := client.CreateSecret(ctx, &awssecretsmanager.CreateSecretInput{
			Name:         aws.String(name),
			SecretString: aws.String(value),
		})
		require.NoError(t, err)

		ref := name
		if name == "resolver-contract-sm-p1" {
			arn := aws.ToString(out.ARN)
			require.Greater(t, len(arn), secretARNGeneratedSuffixLength)
			ref = arn[:len(arn)-secretARNGeneratedSuffixLength]
		}
		values[ref] = value
	}

	resolvercontract.Run(t, resolvercontract.Fixture{
		Resolve:         NewResolverFromAWSConfig(cfg, nil).Resolve,
		FallbackResolve: NewResolver(batchFailingClient{secretsManagerClient: client}, nil).Resolve,
		Values:          values,
		MissingRef:      "resolver-contract-sm-missing",
	})

	t.Run("falls back after batch item error", func(t *testing.T) {
		refs := make([]string, 0, len(values))
		for ref := range values {
			refs = append(refs, ref)
		}

		actual, err := NewResolver(batchItemFailingClient{secretsManagerClient: client}, nil).Resolve(t.Context(), refs)

		require.NoError(t, err)
		require.Equal(t, values, actual)
	})
}
