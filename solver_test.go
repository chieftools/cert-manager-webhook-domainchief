package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	extapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestPresentUsesAPIKeyFromSecretInResourceNamespace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer ctp_secret" {
			t.Errorf("unexpected authorization header %q", got)
		}

		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	s := &solver{kubeClient: fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "domainchief-api-key", Namespace: "cert-manager"},
		Data:       map[string][]byte{"api-key": []byte("ctp_secret\n")},
	})}

	err := s.Present(challengeRequest("cert-manager", `{"apiKeySecretRef":{"name":"domainchief-api-key","key":"api-key"},"apiBaseUrl":"`+server.URL+`"}`))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
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

func challengeRequest(namespace, config string) *v1alpha1.ChallengeRequest {
	return &v1alpha1.ChallengeRequest{
		ResourceNamespace: namespace,
		ResolvedFQDN:      "_acme-challenge.example.com.",
		ResolvedZone:      "example.com.",
		Key:               "token",
		Config:            &extapi.JSON{Raw: []byte(config)},
	}
}
