package secretmanager

import (
	"context"
	"errors"
	"hash/crc32"
	"path/filepath"
	"testing"

	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/googleapis/gax-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeClient struct {
	values    map[string]string
	rawValues map[string][]byte
	requested []string
	err       error
	// crc replaces the checksum the API would report; omitChecksum drops it entirely.
	crc          *int64
	omitChecksum bool
}

func (f *fakeClient) AccessSecretVersion(_ context.Context, req *secretmanagerpb.AccessSecretVersionRequest, _ ...gax.CallOption) (*secretmanagerpb.AccessSecretVersionResponse, error) {
	f.requested = append(f.requested, req.GetName())
	if f.err != nil {
		return nil, f.err
	}
	data, ok := f.rawValues[req.GetName()]
	if !ok {
		value, found := f.values[req.GetName()]
		if !found {
			return nil, status.Errorf(codes.NotFound, "secret version not found")
		}
		data = []byte(value)
	}

	payload := &secretmanagerpb.SecretPayload{Data: data}
	switch {
	case f.omitChecksum:
	case f.crc != nil:
		payload.DataCrc32C = f.crc
	default:
		sum := checksum(data)
		payload.DataCrc32C = &sum
	}
	return &secretmanagerpb.AccessSecretVersionResponse{Name: req.GetName(), Payload: payload}, nil
}

func checksum(data []byte) int64 {
	return int64(crc32.Checksum(data, crc32.MakeTable(crc32.Castagnoli)))
}

// newResolverWithClient serves a fixed client through the seam production uses.
func newResolverWithClient(client secretManagerClient, project string) *Resolver {
	return &Resolver{
		project:   project,
		newClient: func(context.Context) (secretManagerClient, error) { return client, nil },
	}
}

func TestResolve(t *testing.T) {
	const project = "infra-464520"

	tests := []struct {
		name         string
		project      string
		values       map[string]string
		refs         []string
		want         map[string]string
		wantReq      []string
		omitChecksum bool
	}{
		{
			name:    "short ref expands to latest version",
			project: project,
			values:  map[string]string{"projects/infra-464520/secrets/local-networking/versions/latest": "blob"},
			refs:    []string{"local-networking"},
			want:    map[string]string{"local-networking": "blob"},
			wantReq: []string{"projects/infra-464520/secrets/local-networking/versions/latest"},
		},
		{
			name:    "full resource name is used verbatim",
			project: project,
			values:  map[string]string{"projects/other/secrets/api/versions/3": "v3"},
			refs:    []string{"projects/other/secrets/api/versions/3"},
			want:    map[string]string{"projects/other/secrets/api/versions/3": "v3"},
			wantReq: []string{"projects/other/secrets/api/versions/3"},
		},
		{
			name:    "project-qualified ref without version pins latest",
			project: project,
			values:  map[string]string{"projects/other/secrets/api/versions/latest": "v-latest"},
			refs:    []string{"projects/other/secrets/api"},
			want:    map[string]string{"projects/other/secrets/api": "v-latest"},
			wantReq: []string{"projects/other/secrets/api/versions/latest"},
		},
		{
			name:    "missing secret is omitted rather than failing",
			project: project,
			values:  map[string]string{"projects/infra-464520/secrets/present/versions/latest": "here"},
			refs:    []string{"present", "absent"},
			want:    map[string]string{"present": "here"},
		},
		{
			name:    "duplicate refs are requested once",
			project: project,
			values:  map[string]string{"projects/infra-464520/secrets/dup/versions/latest": "one"},
			refs:    []string{"dup", "dup", "dup"},
			want:    map[string]string{"dup": "one"},
			wantReq: []string{"projects/infra-464520/secrets/dup/versions/latest"},
		},
		{
			name:         "payload without checksum is accepted",
			project:      project,
			values:       map[string]string{"projects/infra-464520/secrets/nocrc/versions/latest": "fine"},
			refs:         []string{"nocrc"},
			want:         map[string]string{"nocrc": "fine"},
			omitChecksum: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeClient{values: tt.values, omitChecksum: tt.omitChecksum}
			resolver := newResolverWithClient(client, tt.project)

			got, err := resolver.Resolve(context.Background(), tt.refs)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			if tt.wantReq != nil {
				assert.Equal(t, tt.wantReq, client.requested)
			}
		})
	}
}

func TestResolveShortRefWithoutProject(t *testing.T) {
	resolver := newResolverWithClient(&fakeClient{}, "")

	_, err := resolver.Resolve(context.Background(), []string{"local-networking"})
	require.ErrorIs(t, err, ErrProjectRequired)
}

func TestResolveCorruptChecksum(t *testing.T) {
	mismatched := checksum([]byte("tampered")) + 1
	client := &fakeClient{
		values: map[string]string{"projects/p/secrets/s/versions/latest": "tampered"},
		crc:    &mismatched,
	}
	resolver := newResolverWithClient(client, "p")

	_, err := resolver.Resolve(context.Background(), []string{"s"})
	require.ErrorIs(t, err, ErrPayloadCorrupt)
	assert.NotContains(t, err.Error(), "tampered", "error must not leak the secret value")
}

func TestResolvePropagatesNonNotFoundErrors(t *testing.T) {
	client := &fakeClient{err: status.Error(codes.PermissionDenied, "denied")}
	resolver := newResolverWithClient(client, "p")

	_, err := resolver.Resolve(context.Background(), []string{"s"})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(errors.Unwrap(err)))
}

func TestUnreadableCredentialsFailOnFirstUse(t *testing.T) {
	resolver := NewResolverFromOptions(Options{
		Project:         "p",
		CredentialsFile: filepath.Join(t.TempDir(), "missing.json"),
	}, nil)

	_, err := resolver.Resolve(context.Background(), []string{"some-secret"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load gcp credentials")
}

func TestMissingProjectBeatsCredentialDiscovery(t *testing.T) {
	resolver := NewResolverFromOptions(Options{
		CredentialsFile: filepath.Join(t.TempDir(), "missing.json"),
	}, nil)

	_, err := resolver.Resolve(context.Background(), []string{"bare-ref"})

	require.ErrorIs(t, err, ErrProjectRequired)
	assert.NotContains(t, err.Error(), "credentials", "credential discovery must not run first")
}

func TestClientIsBuiltOnceAndReused(t *testing.T) {
	var builds int
	resolver := &Resolver{
		project: "p",
		newClient: func(context.Context) (secretManagerClient, error) {
			builds++
			return &fakeClient{values: map[string]string{
				"projects/p/secrets/s/versions/latest": "v",
			}}, nil
		},
	}

	for range 3 {
		_, err := resolver.Resolve(context.Background(), []string{"s"})
		require.NoError(t, err)
	}
	assert.Equal(t, 1, builds)
}

func TestResolveRejectsPayloadsUnusableAsEnvValues(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{name: "embedded NUL byte", data: []byte("abc\x00def")},
		{name: "invalid UTF-8", data: []byte{0xff, 0xfe, 0xfd}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeClient{rawValues: map[string][]byte{
				"projects/p/secrets/s/versions/latest": tt.data,
			}}

			_, err := newResolverWithClient(client, "p").Resolve(context.Background(), []string{"s"})

			require.ErrorIs(t, err, ErrPayloadNotEnvSafe)
			assert.Contains(t, err.Error(), "s", "the error should name the ref")
			assert.NotContains(t, err.Error(), string(tt.data), "the error must not leak the payload")
		})
	}
}

func TestResolveAcceptsMultibyteUTF8(t *testing.T) {
	client := &fakeClient{values: map[string]string{
		"projects/p/secrets/s/versions/latest": "pässwörd-\u00e4\u00f6",
	}}

	got, err := newResolverWithClient(client, "p").Resolve(context.Background(), []string{"s"})

	require.NoError(t, err)
	assert.Equal(t, "pässwörd-\u00e4\u00f6", got["s"])
}

func TestResolveRejectsOutOfRangeChecksum(t *testing.T) {
	const value = "payload"
	valid := checksum([]byte(value))

	tests := []struct {
		name string
		crc  int64
	}{
		{name: "real checksum plus 2^32", crc: valid + (1 << 32)},
		{name: "negative checksum", crc: -valid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeClient{
				values: map[string]string{"projects/p/secrets/s/versions/latest": value},
				crc:    &tt.crc,
			}

			_, err := newResolverWithClient(client, "p").Resolve(context.Background(), []string{"s"})

			require.ErrorIs(t, err, ErrPayloadCorrupt)
		})
	}
}
