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

	values := map[string]string{
		"/it/contract/p1": "v1",
		"/it/contract/p2": "v2",
	}
	for ref, value := range values {
		_, err := client.PutParameter(ctx, &awsssm.PutParameterInput{
			Name:      aws.String(ref),
			Type:      ssmtypes.ParameterTypeString,
			Value:     aws.String(value),
			Overwrite: aws.Bool(true),
		})
		require.NoError(t, err)
	}

	resolvercontract.Run(t, resolvercontract.Fixture{
		Resolver:         NewResolverFromAWSConfig(cfg, nil),
		FallbackResolver: NewResolver(batchFailingClient{ssmClient: client}, nil),
		Values:           values,
		MissingRef:       "/it/contract/missing",
	})
}
