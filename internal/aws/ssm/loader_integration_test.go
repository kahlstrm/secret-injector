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

	resolvercontract.Run(t, resolvercontract.Fixture{
		Resolver:         NewResolverFromAWSConfig(cfg, nil),
		FallbackResolver: NewResolver(batchFailingClient{ssmClient: client}, nil),
		Ref:              func(name string) string { return "/it/contract/" + name },
		Create: func(ctx context.Context, ref, value string) error {
			_, err := client.PutParameter(ctx, &awsssm.PutParameterInput{
				Name:      aws.String(ref),
				Type:      ssmtypes.ParameterTypeString,
				Value:     aws.String(value),
				Overwrite: aws.Bool(true),
			})
			return err
		},
	})
}
