package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook"
	"github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	extapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/libdns/domainchief"
	"github.com/libdns/libdns"
)

const (
	// defaultPresentTimeout keeps a Present call below the 60 second request timeout of the Kubernetes API server,
	// which forwards cert-manager's call to the webhook.
	defaultPresentTimeout = 45 * time.Second

	cleanUpTimeout = 30 * time.Second
)

// httpClient is shared by all challenges so connections to the API are reused.
var httpClient = &http.Client{
	Timeout:   30 * time.Second,
	Transport: userAgentTransport{base: http.DefaultTransport},
}

// solver implements the cert-manager webhook.Solver interface for Domain Chief.
type solver struct {
	kubeClient kubernetes.Interface

	// presentTimeout limits how long Present waits until every nameserver serves the challenge value,
	// defaults to defaultPresentTimeout.
	presentTimeout time.Duration
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

// Present publishes the challenge value and waits until every hosted DNS nameserver serves it.
//
// cert-manager checks the nameservers it can reach before it asks the certificate authority to validate, and those
// can serve the value before the others do. When the value is not served everywhere within the timeout, Present
// returns an error so cert-manager calls it again later. Publishing the same value again is safe.
func (s *solver) Present(ch *v1alpha1.ChallengeRequest) error {
	timeout := s.presentTimeout
	if timeout == 0 {
		timeout = defaultPresentTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	provider, record, err := s.provider(ctx, ch)
	if err != nil {
		return err
	}

	_, err = provider.AppendRecords(ctx, ch.ResolvedZone, []libdns.Record{record})

	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("challenge for %s is not served by every hosted DNS nameserver yet, presenting it again later", ch.ResolvedFQDN)
	}

	if err != nil {
		return fmt.Errorf("failed to present challenge for %s: %w", ch.ResolvedFQDN, err)
	}

	return nil
}

// CleanUp removes only the challenge value from this request, leaving concurrent challenges for the same name in place.
func (s *solver) CleanUp(ch *v1alpha1.ChallengeRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), cleanUpTimeout)
	defer cancel()

	provider, record, err := s.provider(ctx, ch)
	if err != nil {
		return err
	}

	if _, err := provider.DeleteRecords(ctx, ch.ResolvedZone, []libdns.Record{record}); err != nil {
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

// provider returns the Domain Chief provider and the challenge TXT record for the request.
func (s *solver) provider(ctx context.Context, ch *v1alpha1.ChallengeRequest) (*domainchief.Provider, libdns.TXT, error) {
	// The ACME challenge endpoints only accept `_acme-challenge` names, which `cnameStrategy: Follow` can change.
	if !strings.HasPrefix(strings.ToLower(ch.ResolvedFQDN), "_acme-challenge.") {
		return nil, libdns.TXT{}, fmt.Errorf("challenge name %s does not start with _acme-challenge, the cnameStrategy: Follow solver option is not supported", ch.ResolvedFQDN)
	}

	cfg, err := loadConfig(ch.Config)
	if err != nil {
		return nil, libdns.TXT{}, err
	}

	apiKey, err := s.apiKey(ctx, cfg.APIKeySecretRef, ch.ResourceNamespace)
	if err != nil {
		return nil, libdns.TXT{}, err
	}

	provider := &domainchief.Provider{
		APIToken:           apiKey,
		BaseURL:            cfg.APIBaseURL,
		HTTPClient:         httpClient,
		WaitForPublication: true,
	}

	// The fully qualified name keeps its trailing dot, so it is used as is instead of being joined with the zone.
	return provider, libdns.TXT{Name: ch.ResolvedFQDN, Text: ch.Key}, nil
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

// userAgentTransport identifies the webhook in front of the User-Agent set by the libdns provider.
type userAgentTransport struct {
	base http.RoundTripper
}

func (t userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("User-Agent", strings.TrimSpace(userAgent()+" "+req.Header.Get("User-Agent")))

	return t.base.RoundTrip(req)
}
