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
