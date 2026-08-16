# secret-injector

`secret-injector` resolves secrets from AWS and Google Cloud and exposes them as environment variables to a child process. It can also validate configurations or print resolved values for CI and shell workflows.

Supported backends:

- AWS Systems Manager Parameter Store (`aws_ssm`)
- AWS Secrets Manager (`aws_secretsmanager`)
- Google Secret Manager (`gcp_secretmanager`)

All backends ship in one binary today. See [per-backend distribution](docs/backend-variants.md) for the proposed split into variants.

## Quick Start

Install the CLI:

```sh
go install github.com/kahlstrm/secret-injector/cmd/secret-injector@latest
```

Create `secret-injector.yaml`:

```yaml
secrets:
  DATABASE_PASSWORD:
    source: aws_ssm
    ref: /app/{{.STAGE}}/db/password
```

Run an application with the resolved secret:

```sh
secret-injector exec --var STAGE=prod -- ./myapp
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
FROM ghcr.io/kahlstrm/secret-injector:0.1.1 AS secret-injector

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
| `source` | Yes | Backend identifier: `aws_ssm`, `aws_secretsmanager`, or `gcp_secretmanager` |
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
  --var STAGE=prod \
  --var AWS_REGION=eu-west-1
```

Missing template variables and refs that become empty after rendering fail validation. Environment variables are not imported into template data automatically; pass them explicitly, for example `--var AWS_REGION="$AWS_REGION"`.

## AWS Setup

The built-in providers use the AWS SDK default configuration chain. Standard AWS environment variables, shared configuration and credential files, SSO profiles, web identity, and workload IAM roles work without project-specific flags.

For example:

```sh
AWS_PROFILE=development AWS_REGION=eu-west-1 \
  secret-injector exec -- ./myapp
```

AWS configuration and ref-template variables are separate concerns. Setting `AWS_REGION` configures the SDK, but a ref containing `{{.AWS_REGION}}` still requires `--var AWS_REGION="$AWS_REGION"`.

### Separate AWS Configuration

Use an injector-specific profile when the child process has different AWS settings, such as LocalStack credentials and endpoints:

```sh
AWS_ENDPOINT_URL=http://localhost:4566 \
AWS_ACCESS_KEY_ID=test \
AWS_SECRET_ACCESS_KEY=test \
SECRET_INJECTOR_AWS_PROFILE=production \
secret-injector exec -- ./myapp
```

Selecting a profile this way uses its credential chain and isolates secret resolution from ordinary AWS region and configured endpoint environment variables. Ambient static credentials are used only when the profile explicitly selects `Environment` as its credential source; a profile without a credential source can still fall back to container or instance-role credentials. Region and endpoints defined by the selected profile remain available to the injector; the profile must define a region unless `--aws-region` is set. The child process still receives its original environment unchanged.

| Flag | Environment variable | Description |
| --- | --- | --- |
| `--aws-profile` | `SECRET_INJECTOR_AWS_PROFILE` | Shared AWS configuration profile used for secret resolution |
| `--aws-region` | `SECRET_INJECTOR_AWS_REGION` | Region override used for secret resolution |

Flags take precedence over their environment variables. Without an injector-specific profile, the normal AWS default configuration chain remains active; a region override by itself does not isolate endpoints.

| Source | Accepted ref | AWS API access |
| --- | --- | --- |
| `aws_ssm` | Parameter name or ARN | `ssm:GetParameters`; fallback may use `ssm:GetParameter` |
| `aws_secretsmanager` | Secret name, ARN, or partial ARN | `secretsmanager:BatchGetSecretValue`; fallback may use `secretsmanager:GetSecretValue` |

SSM parameters are requested with decryption enabled. Secrets Manager supports `SecretString`; binary secrets are rejected. Customer-managed KMS keys may also require `kms:Decrypt`.

### Google Secret Manager

```yaml
secrets:
  MODEM_PASSWORD:
    source: gcp_secretmanager
    ref: cable-modem
  API_KEY:
    source: gcp_secretmanager
    ref: projects/my-project/secrets/api-key/versions/3
```

Refs are either a bare secret name, qualified by `--gcp-project` and pinned to the latest version, or a full resource name. A ref without a `/versions/` suffix resolves `latest`.

| Flag | Environment variable | Description |
| --- | --- | --- |
| `--gcp-project` | `SECRET_INJECTOR_GCP_PROJECT`, then `GOOGLE_CLOUD_PROJECT` | Project qualifying bare secret refs |
| `--gcp-credentials-file` | `SECRET_INJECTOR_GCP_CREDENTIALS_FILE` | Service account key file, overriding Application Default Credentials |

A project must be set for bare refs; full resource names carry their own. The flag wins, then `SECRET_INJECTOR_GCP_PROJECT`, then `GOOGLE_CLOUD_PROJECT`, which is read when something has exported it. Note that neither `gcloud auth application-default login` nor Cloud Run exports it, so on those the project still has to be set explicitly. It is not derived from credentials or the metadata server, which keeps a bare ref with no project reporting a missing project rather than an authentication failure.

Credentials come from Application Default Credentials unless `--gcp-credentials-file` is set, so `GOOGLE_APPLICATION_CREDENTIALS`, `gcloud auth application-default login`, and workload identity all work unchanged. Access requires `secretmanager.versions.access` on each secret.

Secret Manager has no batch access API, so refs are resolved one request at a time. Payloads are verified against the CRC32C checksum the API returns, and are rejected if they contain a NUL byte or invalid UTF-8, since neither survives being an environment variable.

## Commands

Commands load `./secret-injector.yaml` by default. Override it with one of:

- `--config-file <path>` or `-f <path>`
- `--config-file -` for stdin
- `--config '<inline YAML or JSON>'`

Use repeatable `--var NAME=VALUE` flags when refs contain templates.

### Validate

Parse configuration and render ref templates without contacting AWS:

```sh
secret-injector validate --var STAGE=prod
```

Add `--debug` to print the parsed configuration with rendered refs. It does not print resolved secret values.

### Fetch

Resolve and print environment variable bindings:

```sh
# Raw KEY=VALUE lines
secret-injector fetch --format=env

# JSON object
secret-injector fetch --format=json

# POSIX shell-quoted exports
secret-injector fetch --format=export
```

The default format is `env`. Use `export` rather than `env` when evaluating output in a shell:

```sh
eval "$(secret-injector fetch --format=export)"
```

### Exec

Resolve secrets and replace the current process with a child command:

```sh
secret-injector exec -- ./myapp --flag value
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
Use `loader.DefaultWithOptions` to select an AWS profile or region; it applies the same AWS isolation and validation behavior as the CLI flags.

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
