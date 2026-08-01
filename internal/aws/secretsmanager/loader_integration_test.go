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

	resolvercontract.Run(t, resolvercontract.Fixture{
		Resolver:         NewResolverFromAWSConfig(cfg, nil),
		FallbackResolver: NewResolver(batchFailingClient{secretsManagerClient: client}, nil),
		Create: func(ctx context.Context, ref, value string) error {
			_, err := client.CreateSecret(ctx, &awssecretsmanager.CreateSecretInput{
				Name:         aws.String(ref),
				SecretString: aws.String(value),
			})
			return err
		},
	})
}
