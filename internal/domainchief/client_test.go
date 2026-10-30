package domainchief

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestPresentChallengeSendsNameAndValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/acme/challenges" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}

		if got := r.Header.Get("Authorization"); got != "Bearer ctp_test" {
			t.Errorf("unexpected authorization header %q", got)
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}

		if body["name"] != "_acme-challenge.example.com." || body["value"] != "token" {
			t.Errorf("unexpected body %v", body)
		}

		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := NewClient(server.URL, "ctp_test", "test")

	if err := client.PresentChallenge(context.Background(), "_acme-challenge.example.com.", "token"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestCleanUpChallengeSendsNameAndValueAsQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/acme/challenges" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}

		if r.URL.Query().Get("name") != "_acme-challenge.example.com." || r.URL.Query().Get("value") != "token" {
			t.Errorf("unexpected query %v", r.URL.Query())
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL, "ctp_test", "test")

	if err := client.CleanUpChallenge(context.Background(), "_acme-challenge.example.com.", "token"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestCleanUpChallengeIgnoresUnknownDomain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Domain not found."}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "ctp_test", "test")

	if err := client.CleanUpChallenge(context.Background(), "_acme-challenge.example.com.", "token"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestPresentChallengeReturnsValidationErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"The name field is invalid.","errors":{"name":["The name field is invalid."]}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "ctp_test", "test")

	err := client.PresentChallenge(context.Background(), "example.com.", "token")

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %v", err)
	}

	if apiErr.StatusCode != http.StatusUnprocessableEntity || apiErr.Message != "The name field is invalid. [name: The name field is invalid.]" {
		t.Errorf("unexpected error %v", apiErr)
	}
}

func TestPresentChallengeRetriesWhileDomainIsBusy(t *testing.T) {
	var requests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"Busy.","code":"dns.zone_busy"}`))

			return
		}

		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := NewClient(server.URL, "ctp_test", "test")

	if err := client.PresentChallenge(context.Background(), "_acme-challenge.example.com.", "token"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if requests.Load() != 2 {
		t.Errorf("expected 2 requests, got %d", requests.Load())
	}
}

func TestPresentChallengeDoesNotRetryPermanentConflicts(t *testing.T) {
	var requests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)

		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"The domain does not use hosted DNS.","code":"dns.not_hosted"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "ctp_test", "test")

	if err := client.PresentChallenge(context.Background(), "_acme-challenge.example.com.", "token"); err == nil {
		t.Fatal("expected an error")
	}

	if requests.Load() != 1 {
		t.Errorf("expected 1 request, got %d", requests.Load())
	}
}
