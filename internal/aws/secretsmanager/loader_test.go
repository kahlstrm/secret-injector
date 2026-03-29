package secretsmanager

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssecretsmanager "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSecretsManagerClient struct {
	values         map[string]string
	binaryValues   map[string][]byte
	batchErr       error
	batchItemCodes map[string]string
	getErrors      map[string]error

	batchCalls [][]string
	getCalls   []string
}

func (f *fakeSecretsManagerClient) BatchGetSecretValue(_ context.Context, in *awssecretsmanager.BatchGetSecretValueInput, _ ...func(*awssecretsmanager.Options)) (*awssecretsmanager.BatchGetSecretValueOutput, error) {
	ids := append([]string(nil), in.SecretIdList...)
	f.batchCalls = append(f.batchCalls, ids)

	if f.batchErr != nil {
		return nil, f.batchErr
	}

	out := &awssecretsmanager.BatchGetSecretValueOutput{}
	notFoundCode := (&smtypes.ResourceNotFoundException{}).ErrorCode()

	for _, id := range in.SecretIdList {
		if code, ok := f.batchItemCodes[id]; ok {
			out.Errors = append(out.Errors, smtypes.APIErrorType{
				ErrorCode: aws.String(code),
				SecretId:  aws.String(id),
				Message:   aws.String("batch item failed"),
			})
			continue
		}

		if binary, ok := f.binaryValues[id]; ok {
			out.SecretValues = append(out.SecretValues, smtypes.SecretValueEntry{
				Name:         aws.String(id),
				SecretBinary: binary,
			})
			continue
		}

		if v, ok := f.values[id]; ok {
			out.SecretValues = append(out.SecretValues, smtypes.SecretValueEntry{
				Name:         aws.String(id),
				SecretString: aws.String(v),
			})
			continue
		}

		out.Errors = append(out.Errors, smtypes.APIErrorType{
			ErrorCode: aws.String(notFoundCode),
			SecretId:  aws.String(id),
			Message:   aws.String("not found"),
		})
	}

	return out, nil
}

func (f *fakeSecretsManagerClient) GetSecretValue(_ context.Context, in *awssecretsmanager.GetSecretValueInput, _ ...func(*awssecretsmanager.Options)) (*awssecretsmanager.GetSecretValueOutput, error) {
	id := aws.ToString(in.SecretId)
	f.getCalls = append(f.getCalls, id)

	if err, ok := f.getErrors[id]; ok {
		return nil, err
	}

	if binary, ok := f.binaryValues[id]; ok {
		return &awssecretsmanager.GetSecretValueOutput{SecretBinary: binary}, nil
	}

	if v, ok := f.values[id]; ok {
		return &awssecretsmanager.GetSecretValueOutput{SecretString: aws.String(v)}, nil
	}

	return nil, &smtypes.ResourceNotFoundException{Message: aws.String("not found")}
}

func TestSecretsManagerLoader_BatchChunkingSizes(t *testing.T) {
	refs := make([]string, 23)
	values := make(map[string]string, 23)
	for i := range 23 {
		refs[i] = fmt.Sprintf("secret-%02d", i)
		values[refs[i]] = "v" + fmt.Sprint(i)
	}

	fake := &fakeSecretsManagerClient{values: values}
	l := NewLoader("aws_secretsmanager", fake, nil)

	got, err := l.Resolve(context.Background(), refs)
	require.NoError(t, err)
	require.Len(t, got, len(refs))

	require.Len(t, fake.batchCalls, 2)
	assert.Len(t, fake.batchCalls[0], 20)
	assert.Len(t, fake.batchCalls[1], 3)
	assert.Empty(t, fake.getCalls)
}

func TestSecretsManagerLoader_FallbackOnBatchError_WarnsAndSucceeds(t *testing.T) {
	refs := []string{"a", "b", "c", "d", "e"}
	values := map[string]string{"a": "va", "b": "vb", "c": "vc", "d": "vd", "e": "ve"}
	fake := &fakeSecretsManagerClient{values: values, batchErr: errors.New("access denied")}

	var warnings []string
	warn := func(_ context.Context, msg string) { warnings = append(warnings, msg) }
	l := NewLoader("aws_secretsmanager", fake, warn)

	got, err := l.Resolve(context.Background(), refs)
	require.NoError(t, err)
	require.Len(t, got, len(refs))

	require.Len(t, fake.batchCalls, 1)
	assert.Len(t, fake.batchCalls[0], len(refs))
	assert.Len(t, fake.getCalls, len(refs))
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "BatchGetSecretValue failed")
}

func TestSecretsManagerLoader_BatchSuccessButMissingValue(t *testing.T) {
	refs := []string{"present", "missing"}
	fake := &fakeSecretsManagerClient{values: map[string]string{"present": "x"}}
	l := NewLoader("aws_secretsmanager", fake, nil)

	got, err := l.Resolve(context.Background(), refs)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"present": "x"}, got)
}

func TestSecretsManagerLoader_BatchItemErrorFails(t *testing.T) {
	fake := &fakeSecretsManagerClient{
		batchItemCodes: map[string]string{"forbidden": "AccessDeniedException"},
	}
	l := NewLoader("aws_secretsmanager", fake, nil)

	_, err := l.Resolve(context.Background(), []string{"forbidden"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AccessDeniedException")
}

func TestSecretsManagerLoader_BinaryValueFails(t *testing.T) {
	fake := &fakeSecretsManagerClient{
		binaryValues: map[string][]byte{"binary": {0x01, 0x02}},
	}
	l := NewLoader("aws_secretsmanager", fake, nil)

	_, err := l.Resolve(context.Background(), []string{"binary"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only SecretString is supported")
}

func TestSecretsManagerLoader_FallbackFailureReturnsError(t *testing.T) {
	fake := &fakeSecretsManagerClient{
		batchErr:  errors.New("batch unavailable"),
		getErrors: map[string]error{"a": errors.New("boom")},
	}
	l := NewLoader("aws_secretsmanager", fake, nil)

	_, err := l.Resolve(context.Background(), []string{"a"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fallback GetSecretValue failed")
	assert.Contains(t, err.Error(), "batch error")
}
