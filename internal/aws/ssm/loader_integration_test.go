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
	"github.com/stretchr/testify/require"
)

type batchFailingClient struct {
	ssmClient
}

func (c batchFailingClient) GetParameters(context.Context, *awsssm.GetParametersInput, ...func(*awsssm.Options)) (*awsssm.GetParametersOutput, error) {
	return nil, errors.New("forced batch failure")
}

func TestIntegration_SSMResolverWithLocalstack(t *testing.T) {
	ctx := context.Background()
	ls := testutil.MustSetupLocalStack(t, ctx)
	cfg := ls.MustAWSConfig(t, ctx)
	client := awsssm.NewFromConfig(cfg)

	// Seed test parameters
	_, err := client.PutParameter(ctx, &awsssm.PutParameterInput{
		Name:      aws.String("/it/p1"),
		Type:      ssmtypes.ParameterTypeString,
		Value:     aws.String("v1"),
		Overwrite: aws.Bool(true),
	})
	require.NoError(t, err)
	_, err = client.PutParameter(ctx, &awsssm.PutParameterInput{
		Name:      aws.String("/it/p2"),
		Type:      ssmtypes.ParameterTypeString,
		Value:     aws.String("v2"),
		Overwrite: aws.Bool(true),
	})
	require.NoError(t, err)

	l := NewResolverFromAWSConfig(cfg, nil)
	vals, err := l.Resolve(ctx, []string{"/it/p1", "/it/p2"})
	require.NoError(t, err)
	require.Equal(t, "v1", vals["/it/p1"])
	require.Equal(t, "v2", vals["/it/p2"])
}

func TestIntegration_SSMFallbackTreatsParameterNotFoundAsMissing(t *testing.T) {
	ctx := context.Background()
	ls := testutil.MustSetupLocalStack(t, ctx)
	cfg := ls.MustAWSConfig(t, ctx)
	client := awsssm.NewFromConfig(cfg)

	_, err := client.PutParameter(ctx, &awsssm.PutParameterInput{
		Name:      aws.String("/it/present"),
		Type:      ssmtypes.ParameterTypeString,
		Value:     aws.String("value"),
		Overwrite: aws.Bool(true),
	})
	require.NoError(t, err)

	resolver := NewResolver(batchFailingClient{ssmClient: client}, nil)
	values, err := resolver.Resolve(ctx, []string{"/it/present", "/it/missing"})

	require.NoError(t, err)
	require.Equal(t, map[string]string{"/it/present": "value"}, values)
}
