# secret-injector

Static Go binary for loading secrets from cloud services into environment variables.

## Overview

Maps environment variable names to cloud secret locations via configuration files (YAML first-class, JSON also supported). Loads secrets and injects them as environment variables for downstream processes.

## Use Cases

- Development Docker containers
- CI/CD pipelines
- Kubernetes init containers
- Static binary distribution

## Configuration Format

```yaml
secrets:
  DATABASE_PASSWORD: aws_ssm:/app/{{.STAGE}}/db/password
  API_KEY: aws_ssm:/app/{{.STAGE}}/api/key
  REGION_KEY: aws_ssm:/shared/{{printf "%s" .AWS_REGION}}/api
optional:
  - API_KEY
```

Format: `"ENV_VAR_NAME": "<source>:<ref>"`

- `secrets` entries are required by default.
- `optional` lists secret env names that should not fail execution when missing.
- Missing optional secrets emit warnings and are skipped.
- Config is parsed as YAML; JSON input is accepted because JSON is valid YAML.
- `{{.VAR}}` placeholders in refs are resolved from repeatable `--var NAME=VALUE` flags.
- Refs use Go `text/template`, so simple pipelines/conditionals are supported.
- Missing placeholders fail validation.

### Ref Template Rules

- Ref values are rendered with Go `text/template`.
- Variable values come from `--var NAME=VALUE` flags and are available as `.NAME`.
- Missing variables fail with a template execution error.
- Extra `--var` values are ignored.
- Legacy `${VAR}` syntax is not expanded.

Examples:

```yaml
secrets:
  DB_PASSWORD: aws_ssm:/app/{{.STAGE}}/db/password
  API_KEY: aws_ssm:/app/{{if eq .STAGE "prod"}}stable{{else}}preview{{end}}/api-key
  REGION_KEY: aws_ssm:/shared/{{.AWS_REGION | printf "%s"}}/key
```

## Supported Backends

- AWS SSM Parameter Store (`aws_ssm:`)
- AWS Secrets Manager (`aws_secretsmanager:`)

## Installation

### `go install`

```sh
go install github.com/kahlstrm/secret-injector/cmd/secret-injector@latest
```

### GitHub Releases

Download prebuilt archives and checksums from [GitHub Releases](https://github.com/kahlstrm/secret-injector/releases).

### Container image

The GHCR image supports `linux/amd64` and `linux/arm64`. Use it as a binary source in multi-stage Docker builds:

```dockerfile
FROM ghcr.io/kahlstrm/secret-injector:0.1.0 AS secret-injector

FROM alpine:3.22
COPY --from=secret-injector /secret-injector /usr/local/bin/secret-injector
```

Stable releases also update `ghcr.io/kahlstrm/secret-injector:latest`. Prereleases do not update `latest`; use a version tag for reproducible builds.

## Release Process

See the [release runbook](docs/release.md) for the release contract, artifact publication flow, and verification checklist.

## Usage

Provide config with exactly one of:

- `--config-file <path>` (or `--config-file -` for stdin)
- `--config '<inline YAML/JSON>'`

### Version Information

```sh
# Print embedded build metadata
secret-injector --version

# Build with explicit metadata
make build VERSION=v0.1.0 COMMIT=$(git rev-parse --short HEAD)
```

Release/static builds should always use `CGO_ENABLED=0` (the repository `Makefile` enforces this by default).

### Validate Configuration

```sh
# Validate config file
secret-injector validate --config-file secrets.yaml

# Validate refs with required variables
secret-injector validate --config-file secrets.yaml --var STAGE=prod

# Validate with debug output
secret-injector validate --config-file secrets.yaml --debug

# Validate from stdin
cat secrets.yaml | secret-injector validate --config-file -
```

Example (`--debug`) with a conditional ref template:

```sh
secret-injector validate \
  --config '{"secrets":{"API_KEY":"aws_ssm:/app/{{if eq .STAGE \"prod\"}}stable{{else}}preview{{end}}/api-key"}}' \
  --var STAGE=prod \
  --debug

# output:
# {
#   "secrets": {
#     "API_KEY": "aws_ssm:/app/stable/api-key"
#   }
# }
```

### Fetch Secrets

```sh
# Fetch and output as KEY=VALUE lines (default)
secret-injector fetch --config-file secrets.yaml --var STAGE=prod

# Fetch and output as JSON
secret-injector fetch --config-file secrets.yaml --var STAGE=prod --format=json

# Fetch as shell export statements (for sourcing)
secret-injector fetch --config-file secrets.yaml --var STAGE=prod --format=export

# Source secrets into current shell
eval "$(secret-injector fetch --config-file secrets.yaml --var STAGE=prod --format=export)"

# Fetch from inline config
secret-injector fetch --config '{"secrets":{"API_KEY":"aws_ssm:/app/{{.STAGE}}/key"}}' --var STAGE=prod
```

### Execute with Secrets

```sh
# Load secrets and run a command
secret-injector exec --config-file secrets.yaml --var STAGE=prod -- ./myapp --flag arg

# Secrets are injected as environment variables
secret-injector exec --config-file secrets.yaml --var STAGE=prod -- printenv DATABASE_PASSWORD

# With inline config
secret-injector exec --config '{"secrets":{"DB_PASS":"aws_ssm:/app/{{.STAGE}}/db/pass"}}' --var STAGE=prod -- ./myapp
```

## Library Usage

```go
import (
    "github.com/kahlstrm/secret-injector/pkg/config"
    "github.com/kahlstrm/secret-injector/pkg/loader"
)

cfg, err := config.Load(
    reader,
    config.WithVars(map[string]string{"STAGE": "prod"}),
)
if err != nil {
    return err
}
registry := loader.Default(nil)
secrets, err := registry.ResolveAll(ctx, cfg)
```

## TODO

### Core Features

- [x] Shell export mode (`--export` flag)
- [x] Environment variable substitution in refs (`{{.VAR}}`)
- [x] Validation for missing required variables

### Correctness & Hardening

- [x] Validate environment variable names before execution or shell export
- [x] Reject refs that render empty after template expansion
- [x] Reject trailing YAML documents
- [x] Preserve optional SSM not-found semantics during per-parameter fallback

### AWS Implementation

- [x] Secrets Manager client (`aws_secretsmanager:` prefix)
- [ ] Parameter versioning support (`#version=X`) (skipped for now)

### Advanced Features

- [ ] Secret caching
- [ ] Kubernetes operator library interface
- [ ] Docker init container mode
- [ ] Health checks for secret availability

### Security

- [ ] Memory cleanup for loaded secrets
- [ ] Audit logging
- [ ] Permission validation
- [ ] Secret rotation detection

### Operational

- [ ] Prometheus metrics
- [ ] Structured logging
- [ ] Configuration hot-reload

### Release & Distribution (v1)

- [x] Define release contract (SemVer tags `vX.Y.Z`, prerelease rules, supported OS/arch matrix)
- [x] Add CLI build metadata (`version`, `commit`, `date`) exposed via `--version`
- [x] Add CI workflow for `make fmt`, `make vet`, `make lint`, `make test`, and `make build`
- [x] Add CI integration test job for `make itest` with Docker availability required (fail if Docker unavailable)
- [x] Add CI Docker smoke tests (run image with `--version` and verify Docker `COPY --from` flow)
- [x] Add GoReleaser config for binary artifacts (archives + checksums + changelog)
- [x] Add GoReleaser Docker image publishing to GHCR (multi-arch: `linux/amd64`, `linux/arm64`)
- [x] Add tag-triggered release workflow (`v*`) that publishes GitHub Release assets and GHCR images
- [x] Document install paths: GitHub Releases and `go install`
- [x] Document Docker copy-from usage for local development containers
- [x] Add release runbook and first-cut checklist for `v0.1.0`

### Testing

- [x] Migrate LocalStack to testcontainers for self-contained integration tests
- [x] Add end-to-end CLI tests for template-based refs (`validate`, `fetch`, `exec`)
- [x] Add shared resolver integration contract tests for found/missing semantics and supported fallback paths
