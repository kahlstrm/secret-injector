package secretmanager

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"strings"
	"sync"
	"unicode/utf8"

	gcpcredentials "cloud.google.com/go/auth/credentials"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/googleapis/gax-go/v2"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kahlstrm/secret-injector/pkg/util/uniq"
)

type secretManagerClient interface {
	AccessSecretVersion(ctx context.Context, req *secretmanagerpb.AccessSecretVersionRequest, opts ...gax.CallOption) (*secretmanagerpb.AccessSecretVersionResponse, error)
}

const cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

// ErrProjectRequired indicates a short ref was used without a configured project.
var ErrProjectRequired = errors.New("gcp project required to resolve short secret refs")

// ErrPayloadCorrupt indicates the payload failed its CRC32C integrity check.
var ErrPayloadCorrupt = errors.New("secret payload failed checksum verification")

// ErrPayloadNotEnvSafe indicates a payload cannot survive being an environment value.
var ErrPayloadNotEnvSafe = errors.New("secret payload cannot be used as an environment value")

// Resolver resolves secrets from Google Secret Manager, one request per ref:
// there is no batch access API. Its client speaks REST and so owns nothing a
// caller has to release.
type Resolver struct {
	project   string
	onWarning func(context.Context, string)

	// Deferred so a bare ref with no project reports ErrProjectRequired rather
	// than an authentication failure.
	newClient func(context.Context) (secretManagerClient, error)

	mu     sync.Mutex
	client secretManagerClient
}

func (l *Resolver) clientFor(ctx context.Context) (secretManagerClient, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.client != nil {
		return l.client, nil
	}
	client, err := l.newClient(ctx)
	if err != nil {
		return nil, err
	}
	l.client = client
	return client, nil
}

// Options configures Secret Manager resolution.
type Options struct {
	// Project qualifies bare secret refs. Full resource names ignore it.
	Project string
	// CredentialsFile overrides Application Default Credentials.
	CredentialsFile string
}

// NewResolverFromOptions returns a resolver that builds its client on first use,
// from Application Default Credentials or the configured credentials file.
func NewResolverFromOptions(opts Options, onWarning func(context.Context, string)) *Resolver {
	return &Resolver{
		project:   opts.Project,
		onWarning: onWarning,
		newClient: func(ctx context.Context) (secretManagerClient, error) {
			var clientOpts []option.ClientOption
			if opts.CredentialsFile != "" {
				// option.WithCredentialsFile is deprecated for handing a path
				// straight to the client.
				creds, err := gcpcredentials.DetectDefault(&gcpcredentials.DetectOptions{
					CredentialsFile: opts.CredentialsFile,
					Scopes:          []string{cloudPlatformScope},
				})
				if err != nil {
					return nil, fmt.Errorf("load gcp credentials: %w", err)
				}
				clientOpts = append(clientOpts, option.WithAuthCredentials(creds))
			}

			// REST, not the default gRPC transport: same generated client and retry
			// policy, without a connection pool to own.
			client, err := secretmanager.NewRESTClient(ctx, clientOpts...)
			if err != nil {
				return nil, fmt.Errorf("create secret manager client: %w", err)
			}
			return client, nil
		},
	}
}

// Resolve accepts a full resource name ("projects/p/secrets/name/versions/1") or a
// bare name, expanded against the configured project and pinned to latest. Missing
// secrets are omitted; the registry decides whether that is fatal.
func (l *Resolver) Resolve(ctx context.Context, refs []string) (map[string]string, error) {
	unique := uniq.UniqueSorted(refs)

	names := make([]string, len(unique))
	for i, ref := range unique {
		name, err := l.resourceName(ref)
		if err != nil {
			return nil, err
		}
		names[i] = name
	}

	client, err := l.clientFor(ctx)
	if err != nil {
		return nil, err
	}

	values := make(map[string]string, len(unique))
	for i, ref := range unique {
		out, err := client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{Name: names[i]})
		if err != nil {
			if status.Code(err) == codes.NotFound {
				continue
			}
			return nil, fmt.Errorf("access secret version failed for %q: %w", ref, err)
		}

		value, err := payloadValue(ref, out.GetPayload())
		if err != nil {
			return nil, err
		}
		values[ref] = value
	}

	return values, nil
}

func (l *Resolver) resourceName(ref string) (string, error) {
	name := ref
	if !strings.HasPrefix(name, "projects/") {
		if l.project == "" {
			return "", fmt.Errorf("%w: %q", ErrProjectRequired, ref)
		}
		name = fmt.Sprintf("projects/%s/secrets/%s", l.project, name)
	}
	if !strings.Contains(name, "/versions/") {
		name += "/versions/latest"
	}
	return name, nil
}

func payloadValue(ref string, payload *secretmanagerpb.SecretPayload) (string, error) {
	if payload == nil {
		return "", fmt.Errorf("secret %q returned no payload", ref)
	}
	data := payload.GetData()
	if payload.DataCrc32C != nil {
		// Compared as int64: narrowing to uint32 would truncate real+2^32 into a match.
		got := int64(crc32.Checksum(data, crc32.MakeTable(crc32.Castagnoli)))
		if payload.GetDataCrc32C() != got {
			return "", fmt.Errorf("%w: %s", ErrPayloadCorrupt, ref)
		}
	}

	// A NUL makes syscall.Exec fail with EINVAL; invalid UTF-8 becomes U+FFFD in
	// fetch --format=json.
	if bytes.IndexByte(data, 0) >= 0 {
		return "", fmt.Errorf("%w: %s contains a NUL byte", ErrPayloadNotEnvSafe, ref)
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("%w: %s is not valid UTF-8", ErrPayloadNotEnvSafe, ref)
	}
	return string(data), nil
}
