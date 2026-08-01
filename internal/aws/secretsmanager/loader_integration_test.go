//go:build integration

package secretsmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssecretsmanager "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
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

func TestIntegration_SecretsManagerResolverContract(t *testing.T) {
	ctx := context.Background()
	ls := testutil.MustSetupLocalStack(t, ctx)
	cfg := ls.MustAWSConfig(t, ctx)
	client := awssecretsmanager.NewFromConfig(cfg)

	values := map[string]string{
		"it-contract-sm-p1": "v1",
		"it-contract-sm-p2": "v2",
	}
	for ref, value := range values {
		_, err := client.CreateSecret(ctx, &awssecretsmanager.CreateSecretInput{
			Name:         aws.String(ref),
			SecretString: aws.String(value),
		})
		require.NoError(t, err)
	}

	resolvercontract.Run(t, resolvercontract.Fixture{
		Resolver:         NewResolverFromAWSConfig(cfg, nil),
		FallbackResolver: NewResolver(batchFailingClient{secretsManagerClient: client}, nil),
		Values:           values,
		MissingRef:       "it-contract-sm-missing",
	})
}
