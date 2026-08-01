package config

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseValue_Valid(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		source string
		ref    string
	}{
		{name: "simple", in: "aws_ssm:/app/db/password", source: "aws_ssm", ref: "/app/db/password"},
		{name: "secrets manager source", in: "aws_secretsmanager:my/service/secret", source: "aws_secretsmanager", ref: "my/service/secret"},
		{name: "custom source", in: "custom_provider:/path/to/secret", source: "custom_provider", ref: "/path/to/secret"},
		{name: "legacy source name now syntax-valid", in: "ssm:/x", source: "ssm", ref: "/x"},
		{name: "trim spaces", in: " aws_ssm : /with/spaces ", source: "aws_ssm", ref: "/with/spaces"},
		{name: "source lowercase", in: "AWS_SSM:/UpperCaseSource", source: "aws_ssm", ref: "/UpperCaseSource"},
		{name: "ref contains colon", in: "aws_ssm:/contains:colon", source: "aws_ssm", ref: "/contains:colon"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseValue(tt.in)
			require.NoError(t, err, tt.in)
			assert.Equal(t, tt.source, got.Source, tt.in)
			assert.Equal(t, tt.ref, got.Ref, tt.in)
		})
	}
}

func TestParseValue_Invalid(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{name: "empty", in: ""},
		{name: "missing colon", in: "no-colon"},
		{name: "empty source", in: ":ref-only"},
		{name: "empty ref", in: "aws_ssm:"},
		{name: "source starts with digit", in: "1custom:/x"},
		{name: "source contains dash", in: "custom-provider:/x"},
		{name: "source contains space", in: "custom provider:/x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseValue(tt.in)
			require.Error(t, err, tt.in)
		})
	}
}

func TestLoad_Valid(t *testing.T) {
	jsonStr := `{
        "secrets": {
            "DATABASE_PASSWORD": "aws_ssm:/app/prod/db/password",
            "REDIS_PASSWORD": "aws_ssm:/cache:password"
        },
        "optional": ["REDIS_PASSWORD"]
    }`
	cfg, err := Load(strings.NewReader(jsonStr))
	require.NoError(t, err)
	require.NotNil(t, cfg.Secrets)
	assert.Equal(t, "aws_ssm", cfg.Secrets["DATABASE_PASSWORD"].Source)
	assert.Equal(t, "/app/prod/db/password", cfg.Secrets["DATABASE_PASSWORD"].Ref)
	assert.Equal(t, "/cache:password", cfg.Secrets["REDIS_PASSWORD"].Ref)
	assert.False(t, cfg.Secrets["DATABASE_PASSWORD"].Optional)
	assert.True(t, cfg.Secrets["REDIS_PASSWORD"].Optional)
}

func TestLoad_ValidYAML(t *testing.T) {
	yamlStr := `secrets:
  DATABASE_PASSWORD: aws_ssm:/app/prod/db/password
  REDIS_PASSWORD: aws_ssm:/cache:password
optional:
  - REDIS_PASSWORD
`
	cfg, err := Load(strings.NewReader(yamlStr))
	require.NoError(t, err)
	require.NotNil(t, cfg.Secrets)
	assert.Equal(t, "aws_ssm", cfg.Secrets["DATABASE_PASSWORD"].Source)
	assert.Equal(t, "/app/prod/db/password", cfg.Secrets["DATABASE_PASSWORD"].Ref)
	assert.Equal(t, "/cache:password", cfg.Secrets["REDIS_PASSWORD"].Ref)
	assert.False(t, cfg.Secrets["DATABASE_PASSWORD"].Optional)
	assert.True(t, cfg.Secrets["REDIS_PASSWORD"].Optional)
}

func TestLoad_Valid_SecretsManager(t *testing.T) {
	jsonStr := `{
        "secrets": {
            "API_TOKEN": "aws_secretsmanager:my/service/token"
        }
    }`
	cfg, err := Load(strings.NewReader(jsonStr))
	require.NoError(t, err)
	require.NotNil(t, cfg.Secrets)
	assert.Equal(t, "aws_secretsmanager", cfg.Secrets["API_TOKEN"].Source)
	assert.Equal(t, "my/service/token", cfg.Secrets["API_TOKEN"].Ref)
}

func TestLoad_WithSourceValidator_AllowsCustomSource(t *testing.T) {
	jsonStr := `{
        "secrets": {
            "API_TOKEN": "custom_provider:my/service/token"
        }
    }`
	cfg, err := Load(strings.NewReader(jsonStr), WithSourceValidator(func(string) error { return nil }))
	require.NoError(t, err)
	require.NotNil(t, cfg.Secrets)
	assert.Equal(t, "custom_provider", cfg.Secrets["API_TOKEN"].Source)
	assert.Equal(t, "my/service/token", cfg.Secrets["API_TOKEN"].Ref)
}

func TestLoad_RejectsInvalidEnvironmentNames(t *testing.T) {
	tests := []struct {
		name string
		env  string
	}{
		{name: "empty", env: ""},
		{name: "starts with digit", env: "1SECRET"},
		{name: "contains equals", env: "BAD=NAME"},
		{name: "contains shell metacharacter", env: "BAD;NAME"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := fmt.Sprintf(`{"secrets":{%q:"aws_ssm:/x"}}`, tt.env)

			_, err := Load(strings.NewReader(input))

			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid environment variable name")
		})
	}
}

func TestLoad_Errors_YAML(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		_, err := Load(strings.NewReader("secrets: {}\nextra: 1\n"))
		require.Error(t, err)
	})

	t.Run("value not a string", func(t *testing.T) {
		_, err := Load(strings.NewReader("secrets:\n  X:\n    nested: true\n"))
		require.Error(t, err)
	})

	t.Run("trailing document", func(t *testing.T) {
		_, err := Load(strings.NewReader("secrets: {}\n---\nsecrets: {}\n"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "multiple YAML documents")
	})
}

func TestLoad_Errors(t *testing.T) {
	tests := []struct {
		name        string
		json        string
		errContains string
	}{
		{name: "missing secrets", json: `{}`},
		{name: "unknown field", json: `{"secrets": {}, "extra": 1}`},
		{name: "value not a string", json: `{"secrets": {"X": {}}}`},
		{name: "legacy ssm source surfaced", json: `{"secrets": {"X": "ssm:/x"}}`, errContains: "unsupported source"},
		{name: "legacy secrets manager source surfaced", json: `{"secrets": {"X": "secretsmanager:x"}}`, errContains: "unsupported source"},
		{name: "unsupported source surfaced", json: `{"secrets": {"X": "sm:/x"}}`, errContains: "unsupported source"},
		{name: "invalid source surfaced", json: `{"secrets": {"X": "custom-provider:/x"}}`, errContains: "invalid source"},
		{name: "optional references unknown secret", json: `{"secrets": {"X": "aws_ssm:/x"}, "optional": ["MISSING"]}`, errContains: "not defined in secrets"},
		{name: "optional contains duplicates", json: `{"secrets": {"X": "aws_ssm:/x"}, "optional": ["X", "X"]}`, errContains: "duplicate optional"},
		{name: "optional contains empty value", json: `{"secrets": {"X": "aws_ssm:/x"}, "optional": [""]}`, errContains: "empty environment variable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(strings.NewReader(tt.json))
			require.Error(t, err)
			if tt.errContains != "" {
				assert.Contains(t, err.Error(), tt.errContains)
			}
		})
	}
}

func TestLoad_WithVars_ExpandsRefs(t *testing.T) {
	jsonStr := `{
        "secrets": {
            "DATABASE_PASSWORD": "aws_ssm:/app/{{.STAGE}}/db/password",
            "API_KEY": "aws_ssm:/shared/{{.AWS_REGION}}/api/{{.STAGE}}"
        }
    }`

	cfg, err := Load(
		strings.NewReader(jsonStr),
		WithVars(map[string]string{
			"STAGE":      "prod",
			"AWS_REGION": "eu-west-1",
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, "/app/prod/db/password", cfg.Secrets["DATABASE_PASSWORD"].Ref)
	assert.Equal(t, "/shared/eu-west-1/api/prod", cfg.Secrets["API_KEY"].Ref)
}

func TestLoad_WithVars_ExpandsRefsYAML(t *testing.T) {
	yamlStr := `secrets:
  DATABASE_PASSWORD: aws_ssm:/app/{{.STAGE}}/db/password
  API_KEY: aws_ssm:/shared/{{.AWS_REGION}}/api/{{.STAGE}}
`

	cfg, err := Load(
		strings.NewReader(yamlStr),
		WithVars(map[string]string{
			"STAGE":      "prod",
			"AWS_REGION": "eu-west-1",
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, "/app/prod/db/password", cfg.Secrets["DATABASE_PASSWORD"].Ref)
	assert.Equal(t, "/shared/eu-west-1/api/prod", cfg.Secrets["API_KEY"].Ref)
}

func TestLoad_WithVars_AllowsTemplatePipelineAndConditional(t *testing.T) {
	jsonStr := `{
        "secrets": {
            "X": "aws_ssm:/app/{{printf \"%s\" .STAGE}}/{{if eq .STAGE \"prod\"}}stable{{else}}preview{{end}}"
        }
    }`

	cfg, err := Load(
		strings.NewReader(jsonStr),
		WithVars(map[string]string{"STAGE": "prod"}),
	)
	require.NoError(t, err)
	assert.Equal(t, "/app/prod/stable", cfg.Secrets["X"].Ref)
}

func TestLoad_WithVars_Errors(t *testing.T) {
	tests := []struct {
		name        string
		json        string
		vars        map[string]string
		setEnv      map[string]string
		errContains string
	}{
		{name: "missing variable", json: `{"secrets":{"X":"aws_ssm:/app/{{.STAGE}}/db"}}`, vars: map[string]string{}, errContains: `map has no entry for key "STAGE"`},
		{name: "malformed placeholder", json: `{"secrets":{"X":"aws_ssm:/app/{{.STAGE"}}`, vars: map[string]string{"STAGE": "prod"}, errContains: "unclosed action"},
		{name: "empty rendered ref", json: `{"secrets":{"X":"aws_ssm:{{.REF}}"}}`, vars: map[string]string{"REF": ""}, errContains: "empty ref"},
		{name: "whitespace rendered ref", json: `{"secrets":{"X":"aws_ssm:{{.REF}}"}}`, vars: map[string]string{"REF": " "}, errContains: "empty ref"},
		{name: "no os env fallback", json: `{"secrets":{"X":"aws_ssm:/app/{{.STAGE}}/db"}}`, vars: map[string]string{}, setEnv: map[string]string{"STAGE": "prod"}, errContains: `map has no entry for key "STAGE"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.setEnv {
				t.Setenv(k, v)
			}

			_, err := Load(strings.NewReader(tt.json), WithVars(tt.vars))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
}

func TestLoad_WithVars_IgnoresExtraVars(t *testing.T) {
	_, err := Load(
		strings.NewReader(`{"secrets":{"X":"aws_ssm:/app/{{.STAGE}}/db"}}`),
		WithVars(map[string]string{
			"STAGE": "prod",
			"EXTRA": "value",
		}),
	)
	require.NoError(t, err)
}

func TestExpandRef(t *testing.T) {
	tests := []struct {
		name        string
		ref         string
		vars        map[string]string
		want        string
		errContains string
	}{
		{name: "no placeholders", ref: "/app/prod/db", vars: map[string]string{"STAGE": "prod"}, want: "/app/prod/db"},
		{name: "single placeholder", ref: "/app/{{.STAGE}}/db", vars: map[string]string{"STAGE": "prod"}, want: "/app/prod/db"},
		{name: "multiple placeholders", ref: "/app/{{.STAGE}}/{{.AWS_REGION}}", vars: map[string]string{"STAGE": "prod", "AWS_REGION": "eu-west-1"}, want: "/app/prod/eu-west-1"},
		{name: "pipeline", ref: "/app/{{printf \"%s\" .STAGE}}/db", vars: map[string]string{"STAGE": "prod"}, want: "/app/prod/db"},
		{name: "conditional", ref: "/app/{{if eq .STAGE \"prod\"}}a{{else}}b{{end}}", vars: map[string]string{"STAGE": "prod"}, want: "/app/a"},
		{name: "missing variable", ref: "/app/{{.STAGE}}/db", vars: map[string]string{}, errContains: `map has no entry for key "STAGE"`},
		{name: "malformed template", ref: "/app/{{.STAGE", vars: map[string]string{"STAGE": "prod"}, errContains: "unclosed action"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := expandRef(tt.ref, tt.vars)
			if tt.errContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
