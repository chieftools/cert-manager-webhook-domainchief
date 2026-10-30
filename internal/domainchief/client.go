// Package domainchief contains a minimal client for the Domain Chief ACME challenge API.
package domainchief

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL is the base URL of the Domain Chief API.
const DefaultBaseURL = "https://domain.chief.app/api/v1"

const (
	maxAttempts  = 4
	retryBackoff = 2 * time.Second
)

// Client publishes and removes ACME DNS-01 challenge values using the Domain Chief API.
type Client struct {
	baseURL    string
	apiKey     string
	userAgent  string
	httpClient *http.Client
}

// NewClient creates a client for the given API base URL and API key.
//
// An empty baseURL falls back to DefaultBaseURL.
func NewClient(baseURL, apiKey, userAgent string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		userAgent:  userAgent,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// APIError is returned when the Domain Chief API responds with an unexpected status code.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("domain chief API error (%d, %s): %s", e.StatusCode, e.Code, e.Message)
	}

	return fmt.Sprintf("domain chief API error (%d): %s", e.StatusCode, e.Message)
}

// PresentChallenge publishes a challenge value on the given fully qualified name.
//
// Publishing a value that already exists is a no-op, so this is safe to call repeatedly.
func (c *Client) PresentChallenge(ctx context.Context, name, value string) error {
	payload, err := json.Marshal(map[string]string{
		"name":  name,
		"value": value,
	})
	if err != nil {
		return err
	}

	return c.do(ctx, http.MethodPost, "/acme/challenges", payload)
}

// CleanUpChallenge removes the challenge value from the given fully qualified name.
//
// Only the record with this exact value is removed, so concurrent challenges for the same name are kept.
// Removing a value that does not exist (anymore) is not an error.
func (c *Client) CleanUpChallenge(ctx context.Context, name, value string) error {
	query := url.Values{
		"name":  {name},
		"value": {value},
	}

	err := c.do(ctx, http.MethodDelete, "/acme/challenges?"+query.Encode(), nil)

	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		return nil
	}

	return err
}

// do sends the request, retrying when another DNS write for the same domain is still running.
func (c *Client) do(ctx context.Context, method, path string, body []byte) error {
	var err error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err = c.send(ctx, method, path, body)

		var apiErr *APIError
		if !errors.As(err, &apiErr) || !apiErr.retryable() || attempt == maxAttempts {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * retryBackoff):
		}
	}

	return err
}

func (c *Client) send(ctx context.Context, method, path string, body []byte) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", c.userAgent)

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("domain chief API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, resp.Body)

		return nil
	}

	return parseError(resp)
}

func parseError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	apiErr := &APIError{StatusCode: resp.StatusCode}

	var decoded struct {
		Message string              `json:"message"`
		Code    string              `json:"code"`
		Errors  map[string][]string `json:"errors"`
	}

	if json.Unmarshal(raw, &decoded) == nil && decoded.Message != "" {
		apiErr.Code = decoded.Code
		apiErr.Message = decoded.Message

		for field, messages := range decoded.Errors {
			apiErr.Message += fmt.Sprintf(" [%s: %s]", field, strings.Join(messages, " "))
		}
	} else {
		apiErr.Message = strings.TrimSpace(string(raw))
	}

	if apiErr.Message == "" {
		apiErr.Message = http.StatusText(resp.StatusCode)
	}

	return apiErr
}

// retryable reports whether the request may succeed when sent again.
//
// A conflict is only retried when another write for the same domain is still running,
// not when it is permanent (e.g. the domain does not use hosted DNS).
func (e *APIError) retryable() bool {
	switch {
	case e.StatusCode == http.StatusConflict:
		return e.Code == "domain.concurrent_request" || e.Code == "dns.zone_busy" || e.Code == "dns.zone_changed"
	case e.StatusCode == http.StatusTooManyRequests:
		return true
	default:
		return e.StatusCode >= http.StatusInternalServerError
	}
}
