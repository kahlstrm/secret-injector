package secretsmanager

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssecretsmanager "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/aws/smithy-go"
	"github.com/kahlstrm/secret-injector/pkg/util/uniq"
)

type secretsManagerClient interface {
	BatchGetSecretValue(ctx context.Context, params *awssecretsmanager.BatchGetSecretValueInput, optFns ...func(*awssecretsmanager.Options)) (*awssecretsmanager.BatchGetSecretValueOutput, error)
	GetSecretValue(ctx context.Context, params *awssecretsmanager.GetSecretValueInput, optFns ...func(*awssecretsmanager.Options)) (*awssecretsmanager.GetSecretValueOutput, error)
}

// secretARNGeneratedSuffixLength includes the hyphen and six characters appended by Secrets Manager.
const secretARNGeneratedSuffixLength = 7

// Resolver resolves secrets from AWS Secrets Manager.
type Resolver struct {
	client    secretsManagerClient
	onWarning func(context.Context, string)
}

const secretsManagerBatchMax = 20

// NewResolver creates a secrets manager resolver with an injected client.
func NewResolver(client secretsManagerClient, onWarning func(context.Context, string)) *Resolver {
	return &Resolver{client: client, onWarning: onWarning}
}

// NewResolverFromAWSConfig constructs a secrets manager resolver from shared AWS SDK config.
func NewResolverFromAWSConfig(cfg aws.Config, onWarning func(context.Context, string)) *Resolver {
	return &Resolver{client: awssecretsmanager.NewFromConfig(cfg), onWarning: onWarning}
}

// Resolve resolves the requested secret refs.
func (l *Resolver) Resolve(ctx context.Context, refs []string) (map[string]string, error) {
	unique := uniq.UniqueSorted(refs)

	values := make(map[string]string, len(unique))
	var batchErr error

	for chunk := range slices.Chunk(unique, secretsManagerBatchMax) {
		out, err := l.client.BatchGetSecretValue(ctx, &awssecretsmanager.BatchGetSecretValueInput{SecretIdList: chunk})
		if err != nil {
			batchErr = err
			break
		}

		chunkValues, err := collectBatchValues(chunk, out)
		if err != nil {
			return nil, err
		}
		for ref, val := range chunkValues {
			values[ref] = val
		}
	}

	if batchErr == nil {
		return values, nil
	}

	fallbackValues := make(map[string]string, len(unique))
	var firstErr error

	for _, ref := range unique {
		out, err := l.client.GetSecretValue(ctx, &awssecretsmanager.GetSecretValueInput{SecretId: &ref})
		if err != nil {
			if isNotFound(err) {
				continue
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("fallback GetSecretValue failed for %q: %w (batch error: %v)", ref, err, batchErr)
			}
			continue
		}

		value, err := extractSecretString(ref, out.SecretString, out.SecretBinary)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("fallback GetSecretValue failed for %q: %w (batch error: %v)", ref, err, batchErr)
			}
			continue
		}
		fallbackValues[ref] = value
	}

	if firstErr == nil {
		if l.onWarning != nil {
			l.onWarning(ctx, fmt.Sprintf("BatchGetSecretValue failed: %v; fell back to per-name requests", batchErr))
		}
		return fallbackValues, nil
	}

	return nil, firstErr
}

func collectBatchValues(requested []string, out *awssecretsmanager.BatchGetSecretValueOutput) (map[string]string, error) {
	values := make(map[string]string, len(requested))
	requestedSet := make(map[string]struct{}, len(requested))
	for _, ref := range requested {
		requestedSet[ref] = struct{}{}
	}

	for _, entryErr := range out.Errors {
		if isNotFoundCode(aws.ToString(entryErr.ErrorCode)) {
			continue
		}

		secretID := aws.ToString(entryErr.SecretId)
		if secretID == "" {
			secretID = "<unknown>"
		}
		code := aws.ToString(entryErr.ErrorCode)
		if code == "" {
			code = "UnknownError"
		}
		msg := aws.ToString(entryErr.Message)
		if msg == "" {
			return nil, fmt.Errorf("batch get secret failed for %q: %s", secretID, code)
		}
		return nil, fmt.Errorf("batch get secret failed for %q: %s (%s)", secretID, code, msg)
	}

	for _, entry := range out.SecretValues {
		ref := matchRequestedRef(requestedSet, aws.ToString(entry.Name), aws.ToString(entry.ARN))
		if ref == "" {
			return nil, errors.New("batch get secret returned a value that does not match requested refs")
		}

		value, err := extractSecretString(ref, entry.SecretString, entry.SecretBinary)
		if err != nil {
			return nil, err
		}
		values[ref] = value
	}

	return values, nil
}

func matchRequestedRef(requested map[string]struct{}, name, arn string) string {
	if _, ok := requested[name]; ok {
		return name
	}
	if _, ok := requested[arn]; ok {
		return arn
	}

	for ref := range requested {
		if len(arn) == len(ref)+secretARNGeneratedSuffixLength && strings.HasPrefix(arn, ref+"-") {
			return ref
		}
	}

	return ""
}

func extractSecretString(secretID string, secretString *string, secretBinary []byte) (string, error) {
	if secretString != nil {
		return aws.ToString(secretString), nil
	}
	if len(secretBinary) > 0 {
		return "", fmt.Errorf("secret %q returned binary data; only SecretString is supported", secretID)
	}
	return "", fmt.Errorf("secret %q returned no secret value", secretID)
}

func isNotFound(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return isNotFoundCode(apiErr.ErrorCode())
}

func isNotFoundCode(code string) bool {
	return code == (&smtypes.ResourceNotFoundException{}).ErrorCode()
}
