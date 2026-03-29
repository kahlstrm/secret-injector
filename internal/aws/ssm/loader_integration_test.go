//go:build integration

package ssm

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/kahlstrm/secret-injector/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestIntegration_SSMLoaderWithLocalstack(t *testing.T) {
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

	l := NewFromAWSConfig(cfg, nil)
	vals, err := l.Resolve(ctx, []string{"/it/p1", "/it/p2"})
	require.NoError(t, err)
	require.Equal(t, "v1", vals["/it/p1"])
	require.Equal(t, "v2", vals["/it/p2"])
}
