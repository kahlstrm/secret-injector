package ssm

import (
	"context"
	"fmt"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/kahlstrm/secret-injector/pkg/util/uniq"
)

// ssmClient describes the subset of AWS SSM API we use.
type ssmClient interface {
	GetParameters(ctx context.Context, params *awsssm.GetParametersInput, optFns ...func(*awsssm.Options)) (*awsssm.GetParametersOutput, error)
	GetParameter(ctx context.Context, params *awsssm.GetParameterInput, optFns ...func(*awsssm.Options)) (*awsssm.GetParameterOutput, error)
}

// Loader implements loader.SecretLoader for the "ssm" source.
// It prefers batched GetParameters and falls back to per-name GetParameter
// when the batch call fails (e.g., due to permissions). If the fallback
// succeeds, an optional onWarning callback is invoked.
type Loader struct {
	source             string
	client             ssmClient
	onWarningOrMissing func(context.Context, string)
}

// GetParameters supports up to 10 names per call
// https://docs.aws.amazon.com/systems-manager/latest/APIReference/API_GetParameters.html#API_GetParameters_RequestParameters
const ssmBatchMax = 10

// NewLoader constructs an SSM loader with a custom source label and client.
// onWarning is optional and may be nil.
func NewLoader(source string, client ssmClient, onWarningOrMissing func(context.Context, string)) *Loader {
	return &Loader{source: source, client: client, onWarningOrMissing: onWarningOrMissing}
}

// NewDefault uses AWS default configuration resolution and the standard
// source label "ssm".
func NewDefault(ctx context.Context, onWarning func(context.Context, string)) (*Loader, error) {
	cfg, err := awscfg.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	client := awsssm.NewFromConfig(cfg)
	return &Loader{source: "ssm", client: client, onWarningOrMissing: onWarning}, nil
}

// Source returns the configured source name.
func (l *Loader) Source() string { return l.source }

// Resolve implements batch-first secret resolution for SSM.
func (l *Loader) Resolve(ctx context.Context, refs []string) (map[string]string, error) {
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
		// Verify completeness
		for _, r := range unique {
			if _, ok := values[r]; !ok {
				// for now, let's just call onWarning for missing
				if l.onWarningOrMissing != nil {
					l.onWarningOrMissing(ctx, fmt.Sprintf("missing value for ref %q from source %q", r, l.source))
				}
			}
		}
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
			// for now, let's just call onWarning for missing
			if l.onWarningOrMissing != nil {
				l.onWarningOrMissing(ctx, fmt.Sprintf("missing value for ref %q from source %q", r, l.source))
			}
			continue
		}
		fallbackValues[r] = aws.ToString(out.Parameter.Value)
	}

	if firstErr == nil {
		if l.onWarningOrMissing != nil {
			l.onWarningOrMissing(ctx, fmt.Sprintf("GetParameters batch failed: %v; fell back to per-name requests", batchErr))
		}
		return fallbackValues, nil
	}
	return nil, firstErr
}
