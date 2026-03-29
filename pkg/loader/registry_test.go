package loader

import (
	"context"
	"testing"

	cfgpkg "github.com/kahlstrm/secret-injector/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_New_AndResolve(t *testing.T) {
	ssm := &fakeLoader{source: "aws_ssm", result: map[string]string{"/a": "va"}}
	sm := &fakeLoader{source: "aws_secretsmanager", result: map[string]string{"/k": "vk"}}

	reg := New(ssm, sm)
	require.NotNil(t, reg["aws_ssm"])
	require.NotNil(t, reg["aws_secretsmanager"])

	cfg := cfgpkg.Config{Secrets: cfgpkg.Secrets{
		"A": {Source: "aws_ssm", Ref: "/a"},
		"K": {Source: "aws_secretsmanager", Ref: "/k"},
	}}

	out, err := ResolveAll(context.Background(), cfg, reg, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"A": "va", "K": "vk"}, out)

	// Each loader called once
	require.Len(t, ssm.resolvedArgs, 1)
	require.Len(t, sm.resolvedArgs, 1)
}

func TestRegistry_Register_ReplacesExisting(t *testing.T) {
	// initial loader returns old value
	old := &fakeLoader{source: "aws_ssm", result: map[string]string{"/a": "old"}}
	reg := New(old)

	// replace with new loader
	newer := &fakeLoader{source: "aws_ssm", result: map[string]string{"/a": "new"}}
	reg.Register(newer)

	cfg := cfgpkg.Config{Secrets: cfgpkg.Secrets{"A": {Source: "aws_ssm", Ref: "/a"}}}
	out, err := ResolveAll(context.Background(), cfg, reg, nil)
	require.NoError(t, err)
	assert.Equal(t, "new", out["A"]) // picked the replacement

	// Ensure only the replacing loader was called
	assert.Len(t, old.resolvedArgs, 0)
	assert.Len(t, newer.resolvedArgs, 1)
}
