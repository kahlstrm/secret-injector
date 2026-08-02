# secret-injector

`secret-injector` resolves secrets from AWS and exposes them as environment variables to a child process. It can also validate configurations or print resolved values for CI and shell workflows.

The project intentionally focuses on one-shot secret resolution. Long-running synchronization, rotation monitoring, operators, and service-level observability are outside its scope.

Supported backends:

- AWS Systems Manager Parameter Store (`aws_ssm`)
- AWS Secrets Manager (`aws_secretsmanager`)

## Quick Start

Install the CLI:

```sh
go install github.com/kahlstrm/secret-injector/cmd/secret-injector@latest
```

Create `secrets.yaml`:

```yaml
secrets:
  DATABASE_PASSWORD:
    source: aws_ssm
    ref: /app/{{.STAGE}}/db/password
```

Run an application with the resolved secret:

```sh
secret-injector exec --config-file secrets.yaml --var STAGE=prod -- ./myapp
```

`DATABASE_PASSWORD` is added to the child process environment, replacing an inherited value with the same name.

## Installation

### Go

```sh
go install github.com/kahlstrm/secret-injector/cmd/secret-injector@latest
```

### Prebuilt Binaries

Download archives and `checksums.txt` from [GitHub Releases](https://github.com/kahlstrm/secret-injector/releases). Release binaries are available for Linux and macOS on `amd64` and `arm64`.

### Container Image

The GHCR image supports `linux/amd64` and `linux/arm64`. It is intended as a binary source for multi-stage builds:

```dockerfile
FROM ghcr.io/kahlstrm/secret-injector:0.1.0 AS secret-injector

FROM alpine:3.22
COPY --from=secret-injector /secret-injector /usr/local/bin/secret-injector
```

Stable releases update `ghcr.io/kahlstrm/secret-injector:latest`. Use a version tag for reproducible builds. Prereleases do not update `latest`.

## Configuration

Configuration is YAML or JSON with a required `secrets` map. Each key is the environment variable to populate.

```yaml
secrets:
  DATABASE_PASSWORD:
    source: aws_ssm
    ref: /app/{{.STAGE}}/db/password
  API_KEY:
    source: aws_secretsmanager
    ref: app/{{.STAGE}}/api-key
    optional: true
```

| Field | Required | Description |
| --- | --- | --- |
| `source` | Yes | Backend identifier: `aws_ssm` or `aws_secretsmanager` |
| `ref` | Yes | Backend-specific parameter or secret identifier |
| `optional` | No | Skip a missing value with a warning instead of failing; defaults to `false` |

Required entries fail resolution when their refs are missing. Optional entries are omitted and produce a warning on stderr. Unknown fields, invalid environment variable names, unsupported sources, and empty refs are rejected during validation.

### Ref Templates

Refs are Go `text/template` expressions. Values come only from repeatable `--var NAME=VALUE` flags:

```yaml
secrets:
  API_KEY:
    source: aws_secretsmanager
    ref: app/{{if eq .STAGE "prod"}}stable{{else}}preview{{end}}/api-key
  REGION_KEY:
    source: aws_ssm
    ref: /shared/{{.AWS_REGION}}/key
```

```sh
secret-injector validate \
  --config-file secrets.yaml \
  --var STAGE=prod \
  --var AWS_REGION=eu-west-1
```

Missing template variables and refs that become empty after rendering fail validation. Environment variables are not imported into template data automatically; pass them explicitly, for example `--var AWS_REGION="$AWS_REGION"`.

## AWS Setup

The built-in providers use the AWS SDK default configuration chain. Standard AWS environment variables, shared configuration and credential files, SSO profiles, web identity, and workload IAM roles work without project-specific flags.

For example:

```sh
AWS_PROFILE=development AWS_REGION=eu-west-1 \
  secret-injector exec --config-file secrets.yaml -- ./myapp
```

AWS configuration and ref-template variables are separate concerns. Setting `AWS_REGION` configures the SDK, but a ref containing `{{.AWS_REGION}}` still requires `--var AWS_REGION="$AWS_REGION"`.

| Source | Accepted ref | AWS API access |
| --- | --- | --- |
| `aws_ssm` | Parameter name or ARN | `ssm:GetParameters`; fallback may use `ssm:GetParameter` |
| `aws_secretsmanager` | Secret name, ARN, or partial ARN | `secretsmanager:BatchGetSecretValue`; fallback may use `secretsmanager:GetSecretValue` |

SSM parameters are requested with decryption enabled. Secrets Manager supports `SecretString`; binary secrets are rejected. Customer-managed KMS keys may also require `kms:Decrypt`.

## Commands

Every command that reads configuration accepts exactly one of:

- `--config-file <path>` or `-f <path>`
- `--config-file -` for stdin
- `--config '<inline YAML or JSON>'`

Use repeatable `--var NAME=VALUE` flags when refs contain templates.

### Validate

Parse configuration and render ref templates without contacting AWS:

```sh
secret-injector validate --config-file secrets.yaml --var STAGE=prod
```

Add `--debug` to print the parsed configuration with rendered refs. It does not print resolved secret values.

### Fetch

Resolve and print environment variable bindings:

```sh
# Raw KEY=VALUE lines
secret-injector fetch --config-file secrets.yaml --format=env

# JSON object
secret-injector fetch --config-file secrets.yaml --format=json

# POSIX shell-quoted exports
secret-injector fetch --config-file secrets.yaml --format=export
```

The default format is `env`. Use `export` rather than `env` when evaluating output in a shell:

```sh
eval "$(secret-injector fetch --config-file secrets.yaml --format=export)"
```

### Exec

Resolve secrets and replace the current process with a child command:

```sh
secret-injector exec --config-file secrets.yaml -- ./myapp --flag value
```

The `--` delimiter separates secret-injector options from the child command and its arguments.

### Version

```sh
secret-injector --version
```

## Library Usage

The configuration and loader packages can be used directly:

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

Custom providers can be added to `loader.New` or supplied as extras to `loader.Default`.

## Development

```sh
make build    # Build bin/secret-injector
make test     # Run unit tests
make lint     # Run golangci-lint
make itest    # Run integration tests with Docker
```

See the [release runbook](docs/release.md) for release and artifact verification details. Concrete future work is tracked in [GitHub Issues](https://github.com/kahlstrm/secret-injector/issues).

## License

[MIT](LICENSE)
