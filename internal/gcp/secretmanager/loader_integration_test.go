//go:build integration

package secretmanager

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"

	"github.com/kahlstrm/secret-injector/internal/testutil/resolvercontract"
)

// startFakeServer serves the subset of the Secret Manager REST API the resolver
// uses. Google ships no emulator and testcontainers does not cover the service,
// so it runs in-process: real client library, real HTTP, no Docker.
func startFakeServer(t *testing.T, values map[string]string) *secretmanager.Client {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/"), ":access")
		w.Header().Set("Content-Type", "application/json")

		value, ok := values[name]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, `{"error":{"code":404,"message":"Secret Version [%s] not found.","status":"NOT_FOUND"}}`, name)
			return
		}

		sum := crc32.Checksum([]byte(value), crc32.MakeTable(crc32.Castagnoli))
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"name": name,
			"payload": map[string]string{
				"data":       base64.StdEncoding.EncodeToString([]byte(value)),
				"dataCrc32c": fmt.Sprint(sum),
			},
		}))
	}))
	t.Cleanup(server.Close)

	client, err := secretmanager.NewRESTClient(t.Context(),
		option.WithEndpoint(server.URL),
		option.WithoutAuthentication(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	return client
}

func TestIntegration_SecretManagerResolverContract(t *testing.T) {
	const project = "resolver-contract"

	seeded := map[string]string{
		"resolver-contract-gsm-p1": "value-1",
		"resolver-contract-gsm-p2": "value-2",
	}

	served := make(map[string]string, len(seeded)+1)
	values := make(map[string]string, len(seeded))
	for name, value := range seeded {
		served["projects/"+project+"/secrets/"+name+"/versions/latest"] = value
		values[name] = value
	}
	// A full resource name must resolve identically to a bare ref.
	const pinnedRef = "projects/" + project + "/secrets/resolver-contract-gsm-pinned/versions/3"
	served[pinnedRef] = "value-pinned"
	values[pinnedRef] = "value-pinned"

	client := startFakeServer(t, served)

	resolvercontract.Run(t, resolvercontract.Fixture{
		Resolve:    newResolverWithClient(client, project).Resolve,
		Values:     values,
		MissingRef: "resolver-contract-gsm-missing",
	})

	t.Run("rejects a bare ref when no project is configured", func(t *testing.T) {
		_, err := newResolverWithClient(client, "").Resolve(t.Context(), []string{"resolver-contract-gsm-p1"})

		require.ErrorIs(t, err, ErrProjectRequired)
	})

	t.Run("resolves a bare ref and its full resource name to one value", func(t *testing.T) {
		const bare = "resolver-contract-gsm-p1"
		full := "projects/" + project + "/secrets/" + bare + "/versions/latest"

		actual, err := newResolverWithClient(client, project).Resolve(t.Context(), []string{bare, full})

		require.NoError(t, err)
		assert.Equal(t, map[string]string{bare: seeded[bare], full: seeded[bare]}, actual)
	})
}
