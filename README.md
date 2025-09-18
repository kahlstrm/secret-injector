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
  "DATABASE_PASSWORD": "ssm:/app/${ENVIRONMENT}/db/password",
  "API_KEY": "secretsmanager:${ENVIRONMENT}/api/key",
  "REDIS_PASSWORD": "ssm:/app/${ENVIRONMENT}/cache/password#version=${VERSION}"
}
```

### Variable Interpolation

- `${VAR}` - Environment variable substitution
- Supports multi-environment configs

## Supported Backends

- AWS SSM Parameter Store (`ssm:`)
- AWS Secrets Manager (`secretsmanager:`)

## Usage

```sh
# Load from config file
secret-injector --config secrets.json --exec "myapp"

# Load from stdin
echo '{"API_KEY":"ssm:/key"}' | secret-injector --exec "myapp"

# Export to shell
eval $(secret-injector --config secrets.json --export)
```

## Library Usage

```go
import "github.com/kahlstrm/secret-injector/pkg/loader"

secrets, err := loader.Load(config)
```

## TODO

### Core Features

- [ ] JSON config parser
- [ ] Environment variable injection
- [ ] Process execution with injected env vars
- [ ] Shell export mode
- [ ] Static binary compilation

### Variable Interpolation

- [ ] Environment variable substitution (`${VAR}`)
- [ ] Validation for missing required variables

### AWS Implementation

- [ ] SSM Parameter Store client
- [ ] Secrets Manager client
- [ ] AWS credential resolution (IAM, env vars, profiles)
- [ ] Parameter versioning support
- [ ] Batch loading optimization
- [ ] Error handling and retries

### Advanced Features

- [ ] Config validation
- [ ] Prefix/suffix support for env var names
- [ ] Secret caching
- [ ] Multiple backend support in single config
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
- [ ] Graceful error handling
- [ ] Help documentation
