//go:build !nodashboardsx

package dashboardsx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	DefaultServiceURL = "https://dashboard.sx"
	clientTimeout     = 30 * time.Second
)

// Client is an HTTP client for the dashboard.sx service API.
type Client struct {
	ServiceURL  string
	InstanceKey string
	Code        string
	HTTPClient  *http.Client
	OnLog       func(msg string) // called for every HTTP request/response
}

// NewClient creates a new dashboard.sx API client.
func NewClient(serviceURL, instanceKey, code string) *Client {
	if serviceURL == "" {
		serviceURL = DefaultServiceURL
	}
	return &Client{
		ServiceURL:  serviceURL,
		InstanceKey: instanceKey,
		Code:        code,
		HTTPClient:  &http.Client{Timeout: clientTimeout},
	}
}

func (c *Client) log(format string, args ...interface{}) {
	if c.OnLog != nil {
		c.OnLog(fmt.Sprintf(format, args...))
	}
}

// HeartbeatRequest is the request body for POST /heartbeat.
type HeartbeatRequest struct {
	InstanceKey string `json:"instance_key"`
}

// Heartbeat sends a keep-alive signal to dashboard.sx.
// Returns the HTTP status code and any error.
func (c *Client) Heartbeat() (int, error) {
	body := HeartbeatRequest{
		InstanceKey: c.InstanceKey,
	}
	c.log("POST %s/heartbeat", c.ServiceURL)
	status, _, err := c.post("/heartbeat", body)
	if err != nil {
		c.log("  → error: %v", err)
	} else {
		c.log("  → OK")
	}
	return status, err
}

// CertProvisioningStartRequest is the request body for POST /cert-provisioning/start.
type CertProvisioningStartRequest struct {
	InstanceKey string `json:"instance_key"`
	Code        string `json:"code"`
}

// CertProvisioningStartResponse is the response from POST /cert-provisioning/start.
type CertProvisioningStartResponse struct {
	ChallengeToken string `json:"challenge_token"`
	Domain         string `json:"domain"`
	ExpiresIn      int    `json:"expires_in"`
}

// CertProvisioningStart requests a short-lived challenge token for cert provisioning.
func (c *Client) CertProvisioningStart() (*CertProvisioningStartResponse, error) {
	body := CertProvisioningStartRequest{
		InstanceKey: c.InstanceKey,
		Code:        c.Code,
	}
	c.log("POST %s/cert-provisioning/start (code=%s)", c.ServiceURL, c.Code)
	_, data, err := c.post("/cert-provisioning/start", body)
	if err != nil {
		c.log("  → error: %v", err)
		return nil, err
	}
	var resp CertProvisioningStartResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		c.log("  → failed to parse response: %s", string(data))
		return nil, fmt.Errorf("failed to parse cert-provisioning/start response: %w", err)
	}
	tokenPreview := resp.ChallengeToken
	if len(tokenPreview) > 8 {
		tokenPreview = tokenPreview[:8] + "..."
	}
	c.log("  → OK (challenge_token=%s, expires_in=%d)", tokenPreview, resp.ExpiresIn)
	return &resp, nil
}

// DNSChallengeRequest is the request body for POST and DELETE /dns-challenge.
type DNSChallengeRequest struct {
	InstanceKey    string `json:"instance_key"`
	ChallengeToken string `json:"challenge_token"`
	ChallengeValue string `json:"challenge_value"`
}

// DNSChallengeCreate creates a TXT record for ACME DNS-01 challenge.
func (c *Client) DNSChallengeCreate(challengeToken, value string) error {
	body := DNSChallengeRequest{
		InstanceKey:    c.InstanceKey,
		ChallengeToken: challengeToken,
		ChallengeValue: value,
	}
	c.log("POST %s/dns-challenge", c.ServiceURL)
	_, _, err := c.post("/dns-challenge", body)
	if err != nil {
		c.log("  → error: %v", err)
	} else {
		c.log("  → OK")
	}
	return err
}

// DNSChallengeDelete removes a TXT record after certificate validation.
func (c *Client) DNSChallengeDelete(challengeToken string) error {
	body := DNSChallengeRequest{
		InstanceKey:    c.InstanceKey,
		ChallengeToken: challengeToken,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	c.log("DELETE %s/dns-challenge", c.ServiceURL)
	req, err := http.NewRequest("DELETE", c.ServiceURL+"/dns-challenge", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		c.log("  → error: %v", err)
		return fmt.Errorf("dns-challenge DELETE failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(resp.Body)
		c.log("  → %d: %s", resp.StatusCode, string(errBody))
		return fmt.Errorf("dns-challenge DELETE returned %d: %s", resp.StatusCode, string(errBody))
	}
	c.log("  → OK")
	return nil
}

// CallbackExchangeRequest is the request body for POST /callback/exchange.
type CallbackExchangeRequest struct {
	CallbackToken string `json:"callback_token"`
}

// CallbackExchangeResponse is the response from POST /callback/exchange.
type CallbackExchangeResponse struct {
	InstanceKey string `json:"instance_key"`
	Code        string `json:"code"`
	Email       string `json:"email"`
}

// CallbackExchange exchanges a one-time callback token for registration info.
func (c *Client) CallbackExchange(callbackToken string) (*CallbackExchangeResponse, error) {
	body := CallbackExchangeRequest{
		CallbackToken: callbackToken,
	}
	c.log("POST %s/callback/exchange", c.ServiceURL)
	_, data, err := c.post("/callback/exchange", body)
	if err != nil {
		c.log("  → error: %v", err)
		return nil, err
	}
	var resp CallbackExchangeResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		c.log("  → failed to parse response: %s", string(data))
		return nil, fmt.Errorf("failed to parse callback/exchange response: %w", err)
	}
	c.log("  → OK (code=%s, email=%s)", resp.Code, resp.Email)
	return &resp, nil
}

// post sends a POST request with JSON body and returns the HTTP status code and response body.
func (c *Client) post(path string, body interface{}) (int, []byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.HTTPClient.Post(c.ServiceURL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		return 0, nil, fmt.Errorf("%s request failed: %w", path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("failed to read %s response: %w", path, err)
	}

	if resp.StatusCode >= 400 {
		return resp.StatusCode, nil, fmt.Errorf("%s returned %d: %s", path, resp.StatusCode, string(respBody))
	}

	return resp.StatusCode, respBody, nil
}
