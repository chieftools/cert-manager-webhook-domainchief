package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	extapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// fakeAPI answers ACME challenge requests with the given status and records what it received.
type fakeAPI struct {
	mu       sync.Mutex
	status   string
	requests []*http.Request
	bodies   []map[string]string
}

func newFakeAPI(t *testing.T, status string) (*fakeAPI, string) {
	api := &fakeAPI{status: status}

	server := httptest.NewServer(api)
	t.Cleanup(server.Close)

	return api, server.URL
}

func (api *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	api.mu.Lock()
	defer api.mu.Unlock()

	body := map[string]string{}
	_ = json.NewDecoder(r.Body).Decode(&body)

	api.requests = append(api.requests, r)
	api.bodies = append(api.bodies, body)

	if r.Method == http.MethodDelete {
		w.WriteHeader(http.StatusNoContent)

		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
		"id": "rec1", "name": body["name"], "value": body["value"], "ttl": 60, "status": api.status,
	}})
}

func (api *fakeAPI) snapshot() ([]*http.Request, []map[string]string) {
	api.mu.Lock()
	defer api.mu.Unlock()

	return append([]*http.Request(nil), api.requests...), append([]map[string]string(nil), api.bodies...)
}

func newSolver() *solver {
	return &solver{kubeClient: fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "domainchief-api-key", Namespace: "cert-manager"},
		Data:       map[string][]byte{"api-key": []byte("ctp_secret\n")},
	})}
}

func TestPresentPublishesTheChallengeWithTheAPIKeyFromTheSecret(t *testing.T) {
	api, url := newFakeAPI(t, "published")

	if err := newSolver().Present(challengeRequest("cert-manager", configFor(url))); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	requests, bodies := api.snapshot()
	if len(requests) != 1 || requests[0].Method != http.MethodPost || requests[0].URL.Path != "/acme/challenges" {
		t.Fatalf("expected a single publish request, got %v", requests)
	}

	if got := requests[0].Header.Get("Authorization"); got != "Bearer ctp_secret" {
		t.Errorf("unexpected authorization header %q", got)
	}

	if got := requests[0].Header.Get("User-Agent"); !strings.HasPrefix(got, "cert-manager-webhook-domainchief/dev libdns-domainchief/") {
		t.Errorf("unexpected user agent %q", got)
	}

	if bodies[0]["name"] != "_acme-challenge.example.com." || bodies[0]["value"] != "token" {
		t.Errorf("unexpected request body %v", bodies[0])
	}
}

func TestPresentFailsUntilEveryNameserverServesTheChallenge(t *testing.T) {
	_, url := newFakeAPI(t, "publishing")

	s := newSolver()
	s.presentTimeout = 100 * time.Millisecond

	err := s.Present(challengeRequest("cert-manager", configFor(url)))
	if err == nil || !strings.Contains(err.Error(), "is not served by every hosted DNS nameserver yet") {
		t.Fatalf("expected a not yet published error, got %v", err)
	}
}

func TestCleanUpDeletesOnlyTheChallengeValue(t *testing.T) {
	api, url := newFakeAPI(t, "published")

	if err := newSolver().CleanUp(challengeRequest("cert-manager", configFor(url))); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	requests, _ := api.snapshot()
	if len(requests) != 1 || requests[0].Method != http.MethodDelete {
		t.Fatalf("expected a single delete request, got %v", requests)
	}

	query := requests[0].URL.Query()
	if query.Get("name") != "_acme-challenge.example.com." || query.Get("value") != "token" {
		t.Errorf("unexpected query %v", query)
	}
}

func TestPresentRejectsNamesWithoutACMEChallengeLabel(t *testing.T) {
	api, url := newFakeAPI(t, "published")

	ch := challengeRequest("cert-manager", configFor(url))
	ch.ResolvedFQDN = "_acme-challenge-target.example.net."

	err := newSolver().Present(ch)
	if err == nil || !strings.Contains(err.Error(), "cnameStrategy: Follow") {
		t.Fatalf("expected a cnameStrategy error, got %v", err)
	}

	if requests, _ := api.snapshot(); len(requests) != 0 {
		t.Errorf("expected no API requests, got %v", requests)
	}
}

func TestPresentFailsWithoutSecretReference(t *testing.T) {
	s := &solver{kubeClient: fake.NewClientset()}

	err := s.Present(challengeRequest("cert-manager", `{}`))
	if err == nil || !strings.Contains(err.Error(), "apiKeySecretRef") {
		t.Fatalf("expected apiKeySecretRef error, got %v", err)
	}
}

func TestPresentFailsWhenSecretKeyIsMissing(t *testing.T) {
	s := &solver{kubeClient: fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "domainchief-api-key", Namespace: "cert-manager"},
		Data:       map[string][]byte{"other": []byte("ctp_secret")},
	})}

	err := s.Present(challengeRequest("cert-manager", `{"apiKeySecretRef":{"name":"domainchief-api-key","key":"api-key"}}`))
	if err == nil || !strings.Contains(err.Error(), `key "api-key" not found`) {
		t.Fatalf("expected missing key error, got %v", err)
	}
}

func configFor(baseURL string) string {
	return `{"apiKeySecretRef":{"name":"domainchief-api-key","key":"api-key"},"apiBaseUrl":"` + baseURL + `"}`
}

func challengeRequest(namespace, config string) *v1alpha1.ChallengeRequest {
	return &v1alpha1.ChallengeRequest{
		ResourceNamespace: namespace,
		ResolvedFQDN:      "_acme-challenge.example.com.",
		ResolvedZone:      "example.com.",
		Key:               "token",
		Config:            &extapi.JSON{Raw: []byte(config)},
	}
}
