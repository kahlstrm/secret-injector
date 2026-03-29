package ssm

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSSMClient implements SSMClient for testing.
type fakeSSMClient struct {
	// Preconfigured behavior/values
	values             map[string]string
	getParametersError error

	// Call records
	getParametersCalls [][]string
	getParameterCalls  []string
}

func (f *fakeSSMClient) GetParameters(_ context.Context, in *awsssm.GetParametersInput, _ ...func(*awsssm.Options)) (*awsssm.GetParametersOutput, error) {
	// record a copy of the names slice
	names := append([]string(nil), in.Names...)
	f.getParametersCalls = append(f.getParametersCalls, names)

	if f.getParametersError != nil {
		return nil, f.getParametersError
	}

	out := &awsssm.GetParametersOutput{}
	for _, n := range in.Names {
		if v, ok := f.values[n]; ok {
			out.Parameters = append(out.Parameters, ssmtypes.Parameter{Name: aws.String(n), Value: aws.String(v)})
		}
	}
	return out, nil
}

func (f *fakeSSMClient) GetParameter(_ context.Context, in *awsssm.GetParameterInput, _ ...func(*awsssm.Options)) (*awsssm.GetParameterOutput, error) {
	name := aws.ToString(in.Name)
	f.getParameterCalls = append(f.getParameterCalls, name)
	v, ok := f.values[name]
	if !ok {
		return nil, errors.New("not found")
	}
	return &awsssm.GetParameterOutput{Parameter: &ssmtypes.Parameter{Name: aws.String(name), Value: aws.String(v)}}, nil
}

func TestSSMResolver_BatchChunkingSizes(t *testing.T) {
	// 23 refs -> 10, 10, 3 batch calls
	refs := make([]string, 23)
	values := make(map[string]string, 23)
	for i := range 23 {
		refs[i] = fmt.Sprintf("/p%02d", i)
		values[refs[i]] = "v" + fmt.Sprint(i)
	}

	fake := &fakeSSMClient{values: values}
	l := NewResolver(fake, nil)

	got, err := l.Resolve(context.Background(), refs)
	require.NoError(t, err)
	require.Len(t, got, len(refs))

	// Assert exactly three batch calls with sizes 10, 10, 3
	require.Len(t, fake.getParametersCalls, 3)
	assert.Len(t, fake.getParametersCalls[0], 10)
	assert.Len(t, fake.getParametersCalls[1], 10)
	assert.Len(t, fake.getParametersCalls[2], 3)

	// No fallback calls on success
	assert.Empty(t, fake.getParameterCalls)
}

func TestSSMResolver_FallbackOnBatchError_WarnsAndSucceeds(t *testing.T) {
	refs := []string{"/a", "/b", "/c", "/d", "/e"}
	values := map[string]string{"/a": "va", "/b": "vb", "/c": "vc", "/d": "vd", "/e": "ve"}
	fake := &fakeSSMClient{values: values, getParametersError: errors.New("access denied")}

	var warnings []string
	warn := func(_ context.Context, msg string) { warnings = append(warnings, msg) }
	l := NewResolver(fake, warn)

	got, err := l.Resolve(context.Background(), refs)
	require.NoError(t, err)
	require.Len(t, got, len(refs))

	// One failed batch attempt (all refs were in the first chunk since n<10)
	require.Len(t, fake.getParametersCalls, 1)
	assert.Len(t, fake.getParametersCalls[0], len(refs))

	// Fallback called for each ref
	assert.Len(t, fake.getParameterCalls, len(refs))
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "GetParameters batch failed")
}

func TestSSMResolver_BatchSuccessButMissingValue(t *testing.T) {
	refs := []string{"/present", "/missing"}
	fake := &fakeSSMClient{values: map[string]string{"/present": "x"}}
	l := NewResolver(fake, nil)

	got, err := l.Resolve(context.Background(), refs)
	require.NoError(t, err)
	// Only present values are included; missing is handled by the caller layer.
	assert.Equal(t, map[string]string{"/present": "x"}, got)
}
