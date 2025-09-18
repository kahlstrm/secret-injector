//go:build integration

package ssm

import (
	"context"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/stretchr/testify/require"
)

// This test requires LocalStack running with SSM enabled on localhost:4566.
// Run: `make localstack-up` then `make itest`.
func TestIntegration_SSMLoaderWithLocalstack(t *testing.T) {
	if os.Getenv("LOCALSTACK") != "1" {
		t.Skip("set LOCALSTACK=1 to run integration tests")
	}

	endpoint := os.Getenv("LOCALSTACK_URL")
	if endpoint == "" {
		endpoint = "http://localhost:4566"
	}

	ctx := context.Background()
	resolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, _ ...interface{}) (aws.Endpoint, error) {
		if service == awsssm.ServiceID {
			return aws.Endpoint{URL: endpoint, HostnameImmutable: true, PartitionID: "aws", SigningRegion: region}, nil
		}
		return aws.Endpoint{}, &aws.EndpointNotFoundError{}
	})

	cfg, err := awscfg.LoadDefaultConfig(ctx,
		awscfg.WithRegion("us-east-1"),
		awscfg.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
		awscfg.WithEndpointResolverWithOptions(resolver),
	)
	require.NoError(t, err)

	// Seed a couple of parameters in LocalStack
	client := awsssm.NewFromConfig(cfg)
	_, err = client.PutParameter(ctx, &awsssm.PutParameterInput{
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

	l := NewLoader("ssm", client, nil)
	vals, err := l.Resolve(ctx, []string{"/it/p1", "/it/p2"})
	require.NoError(t, err)
	require.Equal(t, "v1", vals["/it/p1"])
	require.Equal(t, "v2", vals["/it/p2"])
}
