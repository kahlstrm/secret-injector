# secret-injector

Static Go binary for loading secrets from cloud services into environment variables.

## Overview

Maps environment variable names to cloud secret locations via JSON configuration. Loads secrets and injects them as environment variables for downstream processes.

## Use Cases

- Development Docker containers
- CI/CD pipelines
- Kubernetes init containers
- Static binary distribution

## Configuration Format

```json
{
  "secrets": {
    "DATABASE_PASSWORD": "ssm:/app/prod/db/password",
    "API_KEY": "ssm:/app/prod/api/key"
  }
}
```

Format: `"ENV_VAR_NAME": "<source>:<ref>"`

## Supported Backends

- AWS SSM Parameter Store (`ssm:`)

## Usage

### Validate Configuration

```sh
# Validate config file
secret-injector validate --config-file secrets.json

# Validate with debug output
secret-injector validate --config-file secrets.json --debug

# Validate from stdin
cat secrets.json | secret-injector validate --config-file -
```

### Fetch Secrets

```sh
# Fetch and output as KEY=VALUE lines (default)
secret-injector fetch --config-file secrets.json

# Fetch and output as JSON
secret-injector fetch --config-file secrets.json --format=json

# Fetch as shell export statements (for sourcing)
secret-injector fetch --config-file secrets.json --format=export

# Source secrets into current shell
eval "$(secret-injector fetch --config-file secrets.json --format=export)"

# Fetch from inline JSON
secret-injector fetch --config-json '{"secrets":{"API_KEY":"ssm:/app/key"}}'
```

### Execute with Secrets

```sh
# Load secrets and run a command
secret-injector exec --config-file secrets.json -- ./myapp --flag arg

# Secrets are injected as environment variables
secret-injector exec --config-file secrets.json -- printenv DATABASE_PASSWORD

# With inline config
secret-injector exec --config-json '{"secrets":{"DB_PASS":"ssm:/prod/db/pass"}}' -- ./myapp
```

## Library Usage

```go
import (
    "github.com/kahlstrm/secret-injector/pkg/config"
    "github.com/kahlstrm/secret-injector/pkg/loader"
)

cfg, err := config.Load(reader)
if err != nil {
    return err
}
registry, err := loader.Default(ctx, nil)
if err != nil {
    return err
}
secrets, err := loader.ResolveAll(ctx, cfg, registry)
```

## TODO

### Core Features

- [x] Shell export mode (`--export` flag)
- [ ] Environment variable substitution in refs (`${VAR}`)
- [ ] Validation for missing required variables

### AWS Implementation

- [ ] Secrets Manager client (`secretsmanager:` prefix)
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

### Testing

- [x] Migrate LocalStack to testcontainers for self-contained integration tests
