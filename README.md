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
  DATABASE_PASSWORD: ssm:/app/{{.STAGE}}/db/password
  API_KEY: ssm:/app/{{.STAGE}}/api/key
  REGION_KEY: ssm:/shared/{{printf "%s" .AWS_REGION}}/api
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
  DB_PASSWORD: ssm:/app/{{.STAGE}}/db/password
  API_KEY: ssm:/app/{{if eq .STAGE "prod"}}stable{{else}}preview{{end}}/api-key
  REGION_KEY: ssm:/shared/{{.AWS_REGION | printf "%s"}}/key
```

## Supported Backends

- AWS SSM Parameter Store (`ssm:`)
- AWS Secrets Manager (`secretsmanager:`)

## Usage

Provide config with exactly one of:

- `--config-file <path>` (or `--config-file -` for stdin)
- `--config '<inline YAML/JSON>'`

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
  --config '{"secrets":{"API_KEY":"ssm:/app/{{if eq .STAGE \"prod\"}}stable{{else}}preview{{end}}/api-key"}}' \
  --var STAGE=prod \
  --debug

# output:
# {
#   "secrets": {
#     "API_KEY": "ssm:/app/stable/api-key"
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
secret-injector fetch --config '{"secrets":{"API_KEY":"ssm:/app/{{.STAGE}}/key"}}' --var STAGE=prod
```

### Execute with Secrets

```sh
# Load secrets and run a command
secret-injector exec --config-file secrets.yaml --var STAGE=prod -- ./myapp --flag arg

# Secrets are injected as environment variables
secret-injector exec --config-file secrets.yaml --var STAGE=prod -- printenv DATABASE_PASSWORD

# With inline config
secret-injector exec --config '{"secrets":{"DB_PASS":"ssm:/app/{{.STAGE}}/db/pass"}}' --var STAGE=prod -- ./myapp
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
registry, err := loader.Default(ctx, nil)
if err != nil {
    return err
}
secrets, err := loader.ResolveAll(ctx, cfg, registry, nil)
```

## TODO

### Core Features

- [x] Shell export mode (`--export` flag)
- [x] Environment variable substitution in refs (`{{.VAR}}`)
- [x] Validation for missing required variables

### AWS Implementation

- [x] Secrets Manager client (`secretsmanager:` prefix)
- [ ] Parameter versioning support (`#version=X`)

### Advanced Features

- [ ] Prefix/suffix support for env var names
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
- [ ] GitHub Actions CI/CD pipeline

### Testing

- [x] Migrate LocalStack to testcontainers for self-contained integration tests
- [x] Add end-to-end CLI tests for template-based refs (`validate`, `fetch`, `exec`)
