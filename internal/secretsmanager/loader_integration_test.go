//go:build integration

package secretsmanager

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssecretsmanager "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/kahlstrm/secret-injector/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestIntegration_SecretsManagerLoaderWithLocalstack(t *testing.T) {
	ctx := context.Background()
	ls := testutil.MustSetupLocalStack(t, ctx)
	cfg := ls.MustAWSConfig(t, ctx)
	client := awssecretsmanager.NewFromConfig(cfg)

	_, err := client.CreateSecret(ctx, &awssecretsmanager.CreateSecretInput{
		Name:         aws.String("it-sm-p1"),
		SecretString: aws.String("v1"),
	})
	require.NoError(t, err)

	_, err = client.CreateSecret(ctx, &awssecretsmanager.CreateSecretInput{
		Name:         aws.String("it-sm-p2"),
		SecretString: aws.String("v2"),
	})
	require.NoError(t, err)

	l := NewLoader("aws_secretsmanager", client, nil)
	vals, err := l.Resolve(ctx, []string{"it-sm-p1", "it-sm-p2"})
	require.NoError(t, err)
	require.Equal(t, "v1", vals["it-sm-p1"])
	require.Equal(t, "v2", vals["it-sm-p2"])
}
