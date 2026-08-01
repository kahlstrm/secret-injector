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
		"/it/resolver-contract/p1": "value-1",
		"/it/resolver-contract/p2": "value-2",
	}
	for ref, value := range values {
		_, err := client.PutParameter(ctx, &awsssm.PutParameterInput{
			Name:  aws.String(ref),
			Type:  ssmtypes.ParameterTypeString,
			Value: aws.String(value),
		})
		require.NoError(t, err)
	}

	resolvercontract.Run(t, resolvercontract.Fixture{
		Resolve:         NewResolverFromAWSConfig(cfg, nil).Resolve,
		FallbackResolve: NewResolver(batchFailingClient{ssmClient: client}, nil).Resolve,
		Values:          values,
		MissingRef:      "/it/resolver-contract/missing",
	})
}
