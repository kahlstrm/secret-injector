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
	seed := func(ctx context.Context, secret resolvercontract.Secret) error {
		_, err := client.CreateSecret(ctx, &awssecretsmanager.CreateSecretInput{
			Name:         aws.String(secret.Ref),
			SecretString: aws.String(secret.Value),
		})
		return err
	}

	resolvercontract.Run(t, resolvercontract.Fixture{
		Resolver:         NewResolverFromAWSConfig(cfg, nil),
		FallbackResolver: NewResolver(batchFailingClient{secretsManagerClient: client}, nil),
		Seed:             seed,
	})
}
