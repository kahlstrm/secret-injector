package ssm

import (
	"context"
	"fmt"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/kahlstrm/secret-injector/pkg/util/uniq"
)

// ssmClient describes the subset of AWS SSM API we use.
type ssmClient interface {
	GetParameters(ctx context.Context, params *awsssm.GetParametersInput, optFns ...func(*awsssm.Options)) (*awsssm.GetParametersOutput, error)
	GetParameter(ctx context.Context, params *awsssm.GetParameterInput, optFns ...func(*awsssm.Options)) (*awsssm.GetParameterOutput, error)
}

// Resolver resolves refs from AWS SSM.
// It prefers batched GetParameters and falls back to per-name GetParameter
// when the batch call fails (e.g., due to permissions). If the fallback
// succeeds, an optional onWarning callback is invoked.
type Resolver struct {
	client    ssmClient
	onWarning func(context.Context, string)
}

// GetParameters supports up to 10 names per call
// https://docs.aws.amazon.com/systems-manager/latest/APIReference/API_GetParameters.html#API_GetParameters_RequestParameters
const ssmBatchMax = 10

// NewResolver constructs an SSM resolver with an injected client.
// onWarning is optional and may be nil.
func NewResolver(client ssmClient, onWarning func(context.Context, string)) *Resolver {
	return &Resolver{client: client, onWarning: onWarning}
}

// NewResolverFromAWSConfig constructs an SSM resolver from shared AWS SDK config.
func NewResolverFromAWSConfig(cfg aws.Config, onWarning func(context.Context, string)) *Resolver {
	return &Resolver{client: awsssm.NewFromConfig(cfg), onWarning: onWarning}
}

// Resolve implements batch-first secret resolution for SSM.
func (l *Resolver) Resolve(ctx context.Context, refs []string) (map[string]string, error) {
	// Ensure unique, deterministic refs
	unique := uniq.UniqueSorted(refs)

	// First attempt: batched GetParameters
	values := make(map[string]string, len(unique))
	var batchErr error

	for chunk := range slices.Chunk(unique, ssmBatchMax) {
		out, err := l.client.GetParameters(ctx, &awsssm.GetParametersInput{
			Names:          chunk,
			WithDecryption: aws.Bool(true),
		})
		if err != nil {
			batchErr = err
			break
		}
		for _, p := range out.Parameters {
			if p.Name != nil && p.Value != nil {
				values[aws.ToString(p.Name)] = aws.ToString(p.Value)
			}
		}
	}

	if batchErr == nil {
		return values, nil
	}

	// Fallback: per-name GetParameter
	fallbackValues := make(map[string]string, len(unique))
	var firstErr error
	for _, r := range unique {
		out, err := l.client.GetParameter(ctx, &awsssm.GetParameterInput{
			Name:           &r,
			WithDecryption: aws.Bool(true),
		})
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("fallback GetParameter failed for %q: %w (batch error: %v)", r, err, batchErr)
			}
			continue
		}
		if out.Parameter == nil || out.Parameter.Value == nil {
			continue
		}
		fallbackValues[r] = aws.ToString(out.Parameter.Value)
	}

	if firstErr == nil {
		if l.onWarning != nil {
			l.onWarning(ctx, fmt.Sprintf("GetParameters batch failed: %v; fell back to per-name requests", batchErr))
		}
		return fallbackValues, nil
	}
	return nil, firstErr
}
