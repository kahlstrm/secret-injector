# Per-backend distribution

Status: proposed. Nothing here is implemented yet.

## Why

Every backend links its cloud SDK into one binary, so all users carry the cost of
backends they never enable. Adding Google Secret Manager made that concrete: it
pulled in gRPC and protobuf and grew the linux/amd64 release binary from 14.98 MiB
to 21.88 MiB, roughly +46%, for a backend most AWS users will not configure.

The release binary is a distribution artifact, commonly copied into application
images, so the weight is paid per image rather than once. Backends will keep
arriving, and each one raises the floor for everybody.

## Transport

Most of that weight is the SDK's gRPC transport rather than Secret Manager itself.
Measured on linux/amd64 with the release flags, resolving one real secret:

| build | binary | peak RSS | goroutines |
| --- | ---: | ---: | ---: |
| no GCP backend | 14.98 MiB | — | — |
| SDK, gRPC | 22.22 MiB | 23.97 MB | 7 |
| SDK, `NewRESTClient` (current) | 22.19 MiB | 17.4 MB | 4 |
| `google.golang.org/api/secretmanager/v1` | 21.43 MiB | — | — |
| hand-written REST call | 15.45 MiB | 17.42 MB | 4 |

Wall clock differences are noise: `net/http` negotiates HTTP/2 to
`secretmanager.googleapis.com` too, so JSON versus protobuf is not a factor at one
to five small payloads, and the 320-500 ms is TLS plus token plus one round trip
either way.

The backend uses the SDK over REST. That saves 32 KB, so it is not a size
decision — gRPC still links, because the generated package imports it whichever
constructor is used. It is a lifecycle decision: the gRPC transport owns a
connection pool and background goroutines, so a resolver holding one has to be
closed, and a registry holding resolvers has to expose that obligation to every
caller. `restClient.Close` only drops its `httpClient` reference, so over REST
there is nothing to release and the obligation disappears. Both transports are the
same generated client, retry on the same conditions (`Unavailable`/`ResourceExhausted`
versus 503/429, same backoff), and return `*apierror.APIError`, so `status.Code`
still reports `NotFound` — the `codes.NotFound` check needs no change. The resolver
contract enforces the property with `goleak`, which fails any backend that leaves a
goroutine behind.

Two alternatives were measured and rejected:

- Calling the REST API directly saves 6.8 MiB, the only option that does, but
  means owning ADC edge cases (impersonation, workload identity federation, quota
  project), retry and backoff, and error mapping.
- The discovery-generated `google.golang.org/api/secretmanager/v1` also has no
  lifecycle, but saves only 0.79 MiB — `google.golang.org/api/option` references
  gRPC types, so gRPC still links, and `gsm.New` is no escape hatch, being
  `NewService` plus `option.WithHTTPClient`. It has no retry policy of its own.

The build-tag split below returns the full weight to AWS-only users without either
cost: the code stays SDK-based and the linker does the work.

## Shape

Gate each backend behind a build tag and publish variants.

Provider registration splits per backend, so a tag decides whether the provider,
and therefore its SDK, is linked at all:

```go
// registry_gcp.go
//go:build gcp

func gcpProviders(o GCPOptions) []Provider { ... }

// registry_nogcp.go
//go:build !gcp

func gcpProviders(GCPOptions) []Provider { return nil }
```

Go's linker drops an unreferenced provider along with its transitive SDK, so an
untagged build returns to roughly the pre-GCP size.

Goreleaser then builds one entry per variant, distinguished by tags and artifact
name:

| variant | tags | contains |
| --- | --- | --- |
| `secret-injector` | none | AWS SSM, AWS Secrets Manager |
| `secret-injector-gcp` | `gcp` | the above plus Google Secret Manager |
| `secret-injector-all` | every backend tag | everything |

The same split applies to container images, so `FROM ghcr.io/kahlstrm/secret-injector`
stays lean and a tagged image is opt-in.

## Consequences

Users must choose a variant, which is a real cost: a wrong choice fails at config
validation with `unknown source`, not at build time. That error already lists the
sources the binary was built with, which makes the failure self-explanatory, but
the documentation has to be explicit about which artifact carries which backend.

Nix consumers pass tags directly, so a NixOS host wanting GCP builds with
`tags = [ "gcp" ]` rather than selecting a published artifact.

The budget check stays useful under this scheme, but it should measure the
*default* variant. A per-variant budget would be better still, since the point is
to keep the artifact most people consume small.

## Current state

Until this exists, all backends ship in one binary and the budget is set to 25 MiB
to accommodate Google Secret Manager. That is a deliberate interim: the budget's
purpose is to make dependency weight a conscious decision, and raising it records
the decision rather than hiding it.

The first step is the build-tag split, which is useful on its own and does not
require the release matrix to land at the same time.
