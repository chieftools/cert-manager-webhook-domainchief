//go:build conformance

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	acmetest "github.com/cert-manager/cert-manager/test/acme"
	"github.com/go-logr/logr"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

func init() {
	// The envtest control plane logs through controller-runtime, which warns with a stack trace when no logger is set.
	ctrllog.SetLogger(logr.Discard())
}

// TestConformance runs the cert-manager DNS01 webhook conformance suite against the real Domain Chief API.
//
// It requires a hosted DNS zone in Domain Chief and an API key with the `domainchief:acme:write` scope:
//
//	TEST_ZONE_NAME=example.com. DOMAINCHIEF_API_KEY=ctt_... make test-conformance
func TestConformance(t *testing.T) {
	zone := os.Getenv("TEST_ZONE_NAME")
	apiKey := os.Getenv("DOMAINCHIEF_API_KEY")

	if zone == "" || apiKey == "" {
		t.Skip("TEST_ZONE_NAME and DOMAINCHIEF_API_KEY must be set to run the conformance suite")
	}

	manifestPath := t.TempDir()

	secret := `apiVersion: v1
kind: Secret
metadata:
  name: domainchief-api-key
type: Opaque
stringData:
  api-key: ` + apiKey + "\n"

	if err := os.WriteFile(filepath.Join(manifestPath, "api-key.yaml"), []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}

	config := `{"apiKeySecretRef":{"name":"domainchief-api-key","key":"api-key"}`
	if baseURL := os.Getenv("DOMAINCHIEF_API_BASE_URL"); baseURL != "" {
		config += `,"apiBaseUrl":"` + baseURL + `"`
	}
	config += "}"

	if err := os.WriteFile(filepath.Join(manifestPath, "config.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	fixture := acmetest.NewFixture(&solver{},
		acmetest.SetResolvedZone(zone),
		// The API only accepts `_acme-challenge` names, so we cannot use the default test FQDN.
		acmetest.SetResolvedFQDN("_acme-challenge.cert-manager-conformance."+zone),
		// Real ACME keys are unpadded base64url, the default test key contains `=` which the API rejects.
		acmetest.SetDNSChallengeKey("cert-manager-webhook-domainchief-conformance"),
		acmetest.SetAllowAmbientCredentials(false),
		acmetest.SetManifestPath(manifestPath),
		acmetest.SetStrict(true),
		acmetest.SetPropagationLimit(5*time.Minute),
	)

	fixture.RunConformance(t)
}
