# Repository Guidelines for Agentic Coding

This document provides guidelines for AI coding agents working in the `secret-injector` repository.

## Project Overview

**secret-injector** is a Go CLI tool that loads secrets from cloud providers (currently AWS SSM) and injects them into environment variables for child processes.

```
secret-injector/
├── cmd/secret-injector/   # CLI entry point (main package)
├── pkg/                   # Public libraries (config parsing, loader interfaces)
│   ├── config/            # JSON config parsing and validation
│   ├── loader/            # SecretLoader interface and registry
│   └── util/              # Small utility packages
├── internal/              # Private implementation details
│   ├── ssm/               # AWS SSM loader implementation
│   └── testutil/          # Test helpers (localstack setup)
├── examples/              # Sample configurations and demos
└── bin/                   # Build output directory
```

## Build, Test & Development Commands

### Quick Reference

| Command | Description |
|---------|-------------|
| `make build` | Build binary to `bin/secret-injector` |
| `make test` | Run all unit tests |
| `make lint` | Run golangci-lint |
| `make fmt` | Format all Go files |
| `make vet` | Run go vet |
| `make coverage` | Generate coverage.out |
| `make coverhtml` | Generate HTML coverage report |
| `make itest` | Run integration tests (requires Docker) |

### Running a Single Test

```bash
# Run a specific test by name
go test -run TestResolveAll_GroupsBySourceAndCallsOnce ./pkg/loader

# Run with verbose output
go test -v -run TestParseValue ./pkg/config
```

### Integration Tests

Integration tests use the `integration` build tag and require Docker (testcontainers):

```bash
make itest                                                      # Run all
go test -tags=integration -run TestExecIntegration ./cmd/...    # Run specific
```

## Code Style Guidelines

### Import Ordering

Imports must be grouped: (1) stdlib, (2) external deps, (3) internal packages. Use aliases to avoid stuttering (e.g., `cfgpkg "github.com/kahlstrm/secret-injector/pkg/config"`).

### Naming Conventions

| Element | Convention | Example |
|---------|------------|---------|
| Packages | short, lowercase | `loader`, `config`, `ssm` |
| Exported | PascalCase | `SecretLoader`, `ResolveAll` |
| Unexported | camelCase | `ssmClient`, `loadConfigFromInput` |
| Constructors | `NewType(...)` | `NewLoader(...)`, `NewDefault(...)` |
| Sentinel errors | `var ErrX = errors.New(...)` | `var ErrUnknownSource = ...` |
| CLI flags | kebab-case | `--config-file`, `--config` |
| Env vars | UPPER_SNAKE_CASE | `AWS_REGION`, `CGO_ENABLED` |

### Interface Design

Define small interfaces at the point of use for testability:

```go
type ssmClient interface {
    GetParameters(ctx context.Context, params *awsssm.GetParametersInput, ...) (*awsssm.GetParametersOutput, error)
    GetParameter(ctx context.Context, params *awsssm.GetParameterInput, ...) (*awsssm.GetParameterOutput, error)
}
```

## Error Handling

### Sentinel Errors

```go
var ErrUnknownSource = errors.New("unknown source")
// Usage: return fmt.Errorf("%w: %s", ErrUnknownSource, entry.Source)
```

### Error Wrapping

- Use `%w` to wrap errors and preserve the chain
- Include context about what failed, not internal values
- **Never include secret values in error messages**

### Fail-Fast Validation

Validate inputs early and return errors immediately.

## Testing Guidelines

### Test File Location

Tests live next to code as `*_test.go`. Integration tests use `//go:build integration`.

### Test Structure

Use table-driven tests:

```go
func TestParseValue(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    Entry
        wantErr bool
    }{
        {name: "valid ssm", input: "ssm:/path/to/secret", want: Entry{Source: "ssm", Ref: "/path/to/secret"}},
        {name: "empty input", input: "", wantErr: true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := ParseValue(tt.input)
            if tt.wantErr {
                require.Error(t, err)
                return
            }
            require.NoError(t, err)
            assert.Equal(t, tt.want, got)
        })
    }
}
```

### testify Usage

- `require.*` — fails immediately (use for setup, prerequisites)
- `assert.*` — continues test (use for multiple assertions)

### Mocking

Create fake implementations of interfaces for unit tests:

```go
type fakeLoader struct {
    source string
    result map[string]string
    err    error
}
func (f *fakeLoader) Source() string { return f.source }
func (f *fakeLoader) Resolve(_ context.Context, refs []string) (map[string]string, error) {
    return f.result, f.err
}
```

### Coverage

Target ≥80% in core packages (`pkg/loader`, `pkg/config`, `internal/ssm`).

## Commit & Pull Request Guidelines

### Commit Messages

Follow Conventional Commits: `feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`, `ci:`. Use scope when helpful: `feat(loader): add SSM batch fetch`.

### Pull Request Checklist

- [ ] Tests added/updated for new functionality
- [ ] `make fmt && make vet && make lint` passes
- [ ] `make test` passes
- [ ] No secret values in logs, errors, or debug output
- [ ] README/CLI help updated if flags changed

## Security Guidelines

- **Never log or print secret values** — redact in errors and debug output
- **Prefer IAM roles** over static credentials
- **Validate all inputs** — fail fast on missing or malformed config
- **Keep cloud clients in `internal/`** — isolate external dependencies
