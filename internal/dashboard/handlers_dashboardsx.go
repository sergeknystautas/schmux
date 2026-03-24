//go:build !nodashboardsx

package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/dashboardsx"
)

// setDSXProvisionStatus updates the in-memory provision status.
func (s *Server) setDSXProvisionStatus(status, domain, message string) {
	s.dsxProvisionMu.Lock()
	defer s.dsxProvisionMu.Unlock()
	s.dsxProvision = dsxProvisionStatus{
		Status:  status,
		Domain:  domain,
		Message: message,
	}
}

// handleDashboardSXProvisionStatus returns the current provisioning status.
//
// GET /api/dashboardsx/provision-status
func (s *Server) handleDashboardSXProvisionStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.dsxProvisionMu.Lock()
	status := s.dsxProvision
	s.dsxProvisionMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handleDashboardSXCallback handles the OAuth callback from dashboard.sx.
// After the user authenticates with GitHub on dashboard.sx, the browser is
// redirected here with a one-time callback_token. We exchange it for the
// registration info, kick off cert provisioning in the background, and
// return a page telling the user to check their terminal.
//
// GET /api/dashboardsx/callback?callback_token=<token>
func (s *Server) handleDashboardSXCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	callbackToken := strings.TrimSpace(r.URL.Query().Get("callback_token"))
	if callbackToken == "" {
		http.Error(w, "missing callback_token parameter", http.StatusBadRequest)
		return
	}

	// Determine service URL from config
	serviceURL := dashboardsx.DefaultServiceURL
	if s.config.Network != nil && s.config.Network.DashboardSX != nil && s.config.Network.DashboardSX.ServiceURL != "" {
		serviceURL = s.config.Network.DashboardSX.ServiceURL
	}

	// Read the local instance key
	instanceKey, err := dashboardsx.EnsureInstanceKey()
	if err != nil {
		s.setDSXProvisionStatus("error", "", fmt.Sprintf("Failed to read instance key: %v", err))
		http.Error(w, "Failed to read instance key", http.StatusInternalServerError)
		return
	}

	// Exchange callback token with dashboard.sx
	client := dashboardsx.NewClient(serviceURL, instanceKey, "")
	client.OnLog = func(msg string) {
		// Route client HTTP logs through provision status so CLI can see them
		s.dsxProvisionMu.Lock()
		domain := s.dsxProvision.Domain
		status := s.dsxProvision.Status
		s.dsxProvisionMu.Unlock()
		s.setDSXProvisionStatus(status, domain, msg)
	}

	resp, err := client.CallbackExchange(callbackToken)
	if err != nil {
		s.setDSXProvisionStatus("error", "", fmt.Sprintf("Token exchange failed: %v", err))
		http.Error(w, "Token exchange failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	// Validate returned instance key matches ours
	if resp.InstanceKey != instanceKey {
		s.setDSXProvisionStatus("error", "", "Instance key mismatch after token exchange")
		http.Error(w, "Instance key mismatch", http.StatusForbidden)
		return
	}

	code := resp.Code
	email := resp.Email
	domain := code + ".dashboard.sx"

	// Save initial config (code, email, IP) — TLS paths will be added after cert provisioning
	ip := s.config.GetDashboardSXIP()
	if ip == "" {
		ips, err := dashboardsx.DetectBindableIPs()
		if err == nil && len(ips) > 0 {
			ip = ips[0]
		}
	}

	if s.config.Network == nil {
		s.config.Network = &config.NetworkConfig{}
	}
	s.config.Network.DashboardSX = &config.DashboardSXConfig{
		Enabled:    true,
		Code:       code,
		Email:      email,
		IP:         ip,
		ServiceURL: serviceURL,
	}
	if err := s.config.Save(); err != nil {
		s.setDSXProvisionStatus("error", domain, fmt.Sprintf("Failed to save config: %v", err))
	}

	// Set status and return the browser page immediately
	s.setDSXProvisionStatus("registered", domain, "Registered as "+domain+". Provisioning certificate...")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>dashboard.sx</title></head>
<body style="font-family: system-ui; max-width: 500px; margin: 100px auto; padding: 0 20px; text-align: center;">
<h2>Setup received</h2>
<p>Registered as <strong>%s</strong></p>
<p>Return to your terminal for progress.<br>You can close this tab.</p>
</body>
</html>`, domain)

	// Run cert provisioning in the background
	go s.provisionDashboardSXCert(client, code, email, domain, ip, serviceURL)
}

// provisionDashboardSXCert runs cert provisioning in a background goroutine
// and updates the provision status as it progresses.
func (s *Server) provisionDashboardSXCert(client *dashboardsx.Client, code, email, domain, ip, serviceURL string) {
	// Route all client HTTP logs through provision status
	client.OnLog = func(msg string) {
		s.dsxProvisionMu.Lock()
		status := s.dsxProvision.Status
		s.dsxProvisionMu.Unlock()
		s.setDSXProvisionStatus(status, domain, msg)
	}

	s.setDSXProvisionStatus("starting", domain, "Requesting challenge token from dashboard.sx...")

	client.Code = code
	provider, err := dashboardsx.NewServiceDNSProvider(client)
	if err != nil {
		s.setDSXProvisionStatus("error", domain, fmt.Sprintf("Failed to start cert provisioning: %v", err))
		return
	}

	// Wire up status updates from the DNS provider
	provider.OnStatus = func(status, message string) {
		s.setDSXProvisionStatus(status, domain, message)
	}

	// Wire up status updates from ProvisionCert's internal steps
	onStatus := func(status, message string) {
		s.setDSXProvisionStatus(status, domain, message)
	}

	if err := dashboardsx.ProvisionCert(code, email, false, provider, onStatus); err != nil {
		s.setDSXProvisionStatus("error", domain, fmt.Sprintf("Certificate provisioning failed: %v", err))
		return
	}

	// Update config with TLS paths
	certPath, _ := dashboardsx.CertPath()
	keyPath, _ := dashboardsx.KeyPath()

	port := s.config.GetPort()
	s.config.Network.TLS = &config.TLSConfig{
		CertPath: certPath,
		KeyPath:  keyPath,
	}
	s.config.Network.BindAddress = "0.0.0.0"
	s.config.Network.PublicBaseURL = fmt.Sprintf("https://%s:%d", domain, port)

	if err := s.config.Save(); err != nil {
		s.setDSXProvisionStatus("error", domain, fmt.Sprintf("Certificate saved but failed to update config: %v", err))
		return
	}

	s.setDSXProvisionStatus("complete", domain, fmt.Sprintf("Certificate provisioned for %s", domain))
}
