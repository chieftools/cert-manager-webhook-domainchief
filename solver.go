package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook"
	"github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	extapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/chieftools/cert-manager-webhook-domainchief/internal/domainchief"
)

const requestTimeout = 60 * time.Second

// solver implements the cert-manager webhook.Solver interface for Domain Chief.
type solver struct {
	kubeClient kubernetes.Interface
}

var _ webhook.Solver = (*solver)(nil)

// solverConfig is decoded from the `config` block of the issuer's webhook solver.
type solverConfig struct {
	// APIKeySecretRef references the Secret holding a Domain Chief API key with the `domainchief:acme:write` scope.
	APIKeySecretRef cmmeta.SecretKeySelector `json:"apiKeySecretRef"`

	// APIBaseURL overrides the Domain Chief API base URL, only useful for testing.
	APIBaseURL string `json:"apiBaseUrl,omitempty"`
}

// Name is used as the `solverName` when referencing this solver on an issuer.
func (s *solver) Name() string {
	return "domainchief"
}

// Present publishes the challenge value, it is safe to call this multiple times for the same value.
func (s *solver) Present(ch *v1alpha1.ChallengeRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	client, err := s.client(ctx, ch)
	if err != nil {
		return err
	}

	if err := client.PresentChallenge(ctx, ch.ResolvedFQDN, ch.Key); err != nil {
		return fmt.Errorf("failed to present challenge for %s: %w", ch.ResolvedFQDN, err)
	}

	return nil
}

// CleanUp removes only the challenge value from this request, leaving concurrent challenges for the same name in place.
func (s *solver) CleanUp(ch *v1alpha1.ChallengeRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	client, err := s.client(ctx, ch)
	if err != nil {
		return err
	}

	if err := client.CleanUpChallenge(ctx, ch.ResolvedFQDN, ch.Key); err != nil {
		return fmt.Errorf("failed to clean up challenge for %s: %w", ch.ResolvedFQDN, err)
	}

	return nil
}

// Initialize creates the Kubernetes client used to read the API key Secret.
func (s *solver) Initialize(kubeClientConfig *rest.Config, _ <-chan struct{}) error {
	client, err := kubernetes.NewForConfig(kubeClientConfig)
	if err != nil {
		return err
	}

	s.kubeClient = client

	return nil
}

func (s *solver) client(ctx context.Context, ch *v1alpha1.ChallengeRequest) (*domainchief.Client, error) {
	cfg, err := loadConfig(ch.Config)
	if err != nil {
		return nil, err
	}

	apiKey, err := s.apiKey(ctx, cfg.APIKeySecretRef, ch.ResourceNamespace)
	if err != nil {
		return nil, err
	}

	return domainchief.NewClient(cfg.APIBaseURL, apiKey, userAgent()), nil
}

func (s *solver) apiKey(ctx context.Context, ref cmmeta.SecretKeySelector, namespace string) (string, error) {
	if ref.Name == "" || ref.Key == "" {
		return "", fmt.Errorf("apiKeySecretRef.name and apiKeySecretRef.key must be set in the solver config")
	}

	secret, err := s.kubeClient.CoreV1().Secrets(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get secret %s/%s: %w", namespace, ref.Name, err)
	}

	value, ok := secret.Data[ref.Key]
	if !ok {
		return "", fmt.Errorf("key %q not found in secret %s/%s", ref.Key, namespace, ref.Name)
	}

	apiKey := strings.TrimSpace(string(value))
	if apiKey == "" {
		return "", fmt.Errorf("key %q in secret %s/%s is empty", ref.Key, namespace, ref.Name)
	}

	return apiKey, nil
}

func loadConfig(cfgJSON *extapi.JSON) (solverConfig, error) {
	cfg := solverConfig{}

	if cfgJSON == nil {
		return cfg, nil
	}

	if err := json.Unmarshal(cfgJSON.Raw, &cfg); err != nil {
		return cfg, fmt.Errorf("error decoding solver config: %w", err)
	}

	return cfg, nil
}
