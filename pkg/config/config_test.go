package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseValue_Valid(t *testing.T) {
	cases := []struct {
		in     string
		source string
		ref    string
	}{
		{"ssm:/app/db/password", "ssm", "/app/db/password"},
		{" ssm : /with/spaces ", "ssm", "/with/spaces"},
		{"SSM:/UpperCaseSource", "ssm", "/UpperCaseSource"},
		{"ssm:/contains:colon", "ssm", "/contains:colon"},
	}

	for _, tc := range cases {
		got, err := ParseValue(tc.in)
		require.NoError(t, err, tc.in)
		assert.Equal(t, tc.source, got.Source, tc.in)
		assert.Equal(t, tc.ref, got.Ref, tc.in)
	}
}

func TestParseValue_Invalid(t *testing.T) {
	cases := []string{
		"",          // empty
		"no-colon",  // missing colon
		":ref-only", // empty source
		"ssm:",      // empty ref
		" smm : /x", // unsupported source
	}
	for _, in := range cases {
		_, err := ParseValue(in)
		require.Error(t, err, in)
	}
}

func TestLoad_Valid(t *testing.T) {
	jsonStr := `{
        "secrets": {
            "DATABASE_PASSWORD": "ssm:/app/prod/db/password",
            "REDIS_PASSWORD": "ssm:/cache:password"
        }
    }`
	cfg, err := Load(strings.NewReader(jsonStr))
	require.NoError(t, err)
	require.NotNil(t, cfg.Secrets)
	assert.Equal(t, "ssm", cfg.Secrets["DATABASE_PASSWORD"].Source)
	assert.Equal(t, "/app/prod/db/password", cfg.Secrets["DATABASE_PASSWORD"].Ref)
	assert.Equal(t, "/cache:password", cfg.Secrets["REDIS_PASSWORD"].Ref)
}

func TestLoad_Errors(t *testing.T) {
	t.Run("missing secrets", func(t *testing.T) {
		_, err := Load(strings.NewReader(`{}`))
		require.Error(t, err)
	})

	t.Run("unknown field", func(t *testing.T) {
		_, err := Load(strings.NewReader(`{"secrets": {}, "extra": 1}`))
		require.Error(t, err)
	})

	t.Run("value not a string", func(t *testing.T) {
		_, err := Load(strings.NewReader(`{"secrets": {"X": {}}}`))
		require.Error(t, err)
	})

	t.Run("unsupported source surfaced", func(t *testing.T) {
		_, err := Load(strings.NewReader(`{"secrets": {"X": "sm:/x"}}`))
		require.Error(t, err)
	})
}
