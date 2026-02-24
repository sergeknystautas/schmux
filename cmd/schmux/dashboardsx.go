package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/huh"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/dashboardsx"
)

// DashboardSXCommand implements the dashboardsx command.
type DashboardSXCommand struct {
	style *termStyle
}

// NewDashboardSXCommand creates a new dashboardsx command.
func NewDashboardSXCommand() *DashboardSXCommand {
	return &DashboardSXCommand{
		style: newTermStyle(),
	}
}

// Run dispatches to the appropriate subcommand.
func (cmd *DashboardSXCommand) Run(args []string) error {
	if len(args) < 1 {
		fmt.Println("Usage: schmux dashboardsx <setup|status|disable|renew-cert>")
		return nil
	}

	switch args[0] {
	case "setup":
		return cmd.runSetup()
	case "status":
		return cmd.runStatus()
	case "disable":
		return cmd.runDisable()
	case "renew-cert":
		return cmd.runRenewCert()
	default:
		return fmt.Errorf("unknown subcommand: %s (use setup, status, disable, or renew-cert)", args[0])
	}
}

func (cmd *DashboardSXCommand) loadConfig() (*config.Config, error) {
	if !config.ConfigExists() {
		ok, err := config.EnsureExists()
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("config not created")
		}
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	return config.Load(filepath.Join(homeDir, ".schmux", "config.json"))
}

func (cmd *DashboardSXCommand) runSetup() error {
	cmd.style.Header("Dashboard.sx HTTPS Setup")
	cmd.style.Info(
		"dashboard.sx provides HTTPS certificates for your schmux dashboard,",
		"enabling secure clipboard and other browser features on your LAN.",
	)
	cmd.style.Blank()

	// Ensure config exists
	cfg, err := cmd.loadConfig()
	if err != nil {
		return err
	}

	// Step 1: Ensure instance key
	instanceKey, err := dashboardsx.EnsureInstanceKey()
	if err != nil {
		return fmt.Errorf("failed to ensure instance key: %w", err)
	}
	cmd.style.Success("Instance key ready")

	// Step 2: Detect and select private IP
	ips, err := dashboardsx.DetectPrivateIPs()
	if err != nil {
		return fmt.Errorf("failed to detect private IPs: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("no private IP addresses found; ensure you're connected to a network")
	}

	var selectedIP string
	if existingIP := cfg.GetDashboardSXIP(); existingIP != "" {
		selectedIP = existingIP
	} else {
		selectedIP = ips[0]
	}

	if len(ips) > 1 {
		options := make([]huh.Option[string], len(ips))
		for i, ip := range ips {
			options[i] = huh.NewOption(ip, ip)
		}
		err := huh.NewSelect[string]().
			Title("Which IP should the dashboard be accessible on?").
			Options(options...).
			Value(&selectedIP).
			Run()
		if err != nil {
			return err
		}
	}
	cmd.style.Printf("LAN IP: %s\n", cmd.style.Cyan(selectedIP))

	// Step 3: Build registration URL and open browser
	port := cfg.GetPort()
	returnURL := fmt.Sprintf("http://%s:%d/api/dashboardsx/callback", selectedIP, port)
	params := url.Values{}
	params.Set("instance_key", instanceKey)
	params.Set("ip", selectedIP)
	params.Set("return_url", returnURL)
	registerURL := fmt.Sprintf("%s/register?%s", dashboardsx.DefaultServiceURL, params.Encode())

	cmd.style.Blank()
	cmd.style.SubHeader("Open in your browser")
	cmd.style.Info("Authenticate with GitHub to get your dashboard.sx hostname.")
	cmd.style.Blank()
	cmd.style.Code(registerURL)
	cmd.style.Blank()

	// Try to open browser automatically
	if err := openBrowser(registerURL); err != nil {
		cmd.style.Info("(Could not open browser automatically. Please open the URL above.)")
	} else {
		cmd.style.Success("Browser opened")
	}

	cmd.style.Blank()
	cmd.style.Info("Waiting for you to authenticate in the browser...")
	cmd.style.Blank()

	// Poll the daemon's provision-status endpoint until complete or error
	statusURL := fmt.Sprintf("http://localhost:%d/api/dashboardsx/provision-status", port)
	lastStatus := ""
	lastMessage := ""
	for {
		time.Sleep(2 * time.Second)

		status, domain, message, err := pollProvisionStatus(statusURL)
		if err != nil {
			// Daemon may not be running yet — keep waiting silently
			if lastStatus == "" {
				continue
			}
			cmd.style.Warn(fmt.Sprintf("Could not reach daemon: %v", err))
			continue
		}

		if status == lastStatus && message == lastMessage {
			continue
		}
		lastStatus = status
		lastMessage = message

		switch status {
		case "registered":
			cmd.style.Success(fmt.Sprintf("Registered as %s", domain))
			cmd.style.Info("Starting certificate provisioning...")
		case "complete":
			cmd.style.Blank()
			cmd.style.Success(fmt.Sprintf("Certificate provisioned for %s", domain))
			cmd.style.Blank()
			cmd.style.SubHeader("Next Steps")
			cmd.style.Info("Restart the daemon to enable HTTPS:")
			cmd.style.Code("schmux stop && schmux start")
			cmd.style.Blank()
			cmd.style.Info(fmt.Sprintf("Then open %s", cmd.style.Cyan(fmt.Sprintf("https://%s:%d", domain, port))))
			cmd.style.Blank()
			return nil
		case "error":
			return fmt.Errorf("provisioning failed: %s", message)
		default:
			// All intermediate statuses (starting, acme_account, acme_client,
			// acme_register, cert_request, dns_create, dns_verify, dns_cleanup,
			// cert_save) — display the message directly
			cmd.style.Info(message)
		}
	}
}

func (cmd *DashboardSXCommand) runStatus() error {
	cfg, err := cmd.loadConfig()
	if err != nil {
		return err
	}

	status, err := dashboardsx.GetStatus(cfg)
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	cmd.style.Header("Dashboard.sx Status")

	if !status.HasInstanceKey && !status.HasCert && status.Code == "" {
		cmd.style.Info("Not configured. Run 'schmux dashboardsx setup' to get started.")
		return nil
	}

	enabledStr := cmd.style.Red("disabled")
	if status.Enabled {
		enabledStr = cmd.style.Green("enabled")
	}
	cmd.style.KeyValue("Enabled", enabledStr)

	if status.Code != "" {
		cmd.style.KeyValue("Code", status.Code)
		cmd.style.KeyValue("Hostname", cmd.style.Cyan(status.Hostname))
	}
	if status.IP != "" {
		cmd.style.KeyValue("LAN IP", status.IP)
	}

	instanceKeyStr := cmd.style.Red("no")
	if status.HasInstanceKey {
		instanceKeyStr = cmd.style.Green("yes")
	}
	cmd.style.KeyValue("Instance Key", instanceKeyStr)

	certStr := cmd.style.Red("no")
	if status.HasCert {
		certStr = cmd.style.Green("yes")
		cmd.style.KeyValue("Certificate", certStr)
		cmd.style.KeyValue("Cert Expiry", status.CertExpiry.Format("2006-01-02"))
		daysStr := fmt.Sprintf("%d days", status.DaysUntilExpiry)
		if status.DaysUntilExpiry < 30 {
			daysStr = cmd.style.Yellow(daysStr)
		}
		if status.DaysUntilExpiry < 7 {
			daysStr = cmd.style.Red(daysStr)
		}
		cmd.style.KeyValue("Days Left", daysStr)
	} else {
		cmd.style.KeyValue("Certificate", certStr)
	}

	cmd.style.Blank()
	return nil
}

func (cmd *DashboardSXCommand) runDisable() error {
	cfg, err := cmd.loadConfig()
	if err != nil {
		return err
	}

	if cfg.Network != nil && cfg.Network.DashboardSX != nil {
		cfg.Network.DashboardSX.Enabled = false
	}
	// Clear TLS paths so daemon falls back to HTTP
	if cfg.Network != nil {
		cfg.Network.TLS = nil
		cfg.Network.PublicBaseURL = ""
		cfg.Network.BindAddress = "127.0.0.1"
	}

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	cmd.style.Success("Dashboard.sx HTTPS disabled")
	cmd.style.Blank()
	cmd.style.Info("Restart the daemon for changes to take effect:")
	cmd.style.Code("schmux stop && schmux start")
	cmd.style.Blank()

	return nil
}

func (cmd *DashboardSXCommand) runRenewCert() error {
	cfg, err := cmd.loadConfig()
	if err != nil {
		return err
	}

	code := cfg.GetDashboardSXCode()
	if code == "" {
		return fmt.Errorf("dashboard.sx is not configured; run 'schmux dashboardsx setup' first")
	}

	email := cfg.GetDashboardSXEmail()
	if email == "" {
		err = huh.NewInput().
			Title("Email address").
			Description("For Let's Encrypt account").
			Value(&email).
			Validate(func(s string) error {
				s = strings.TrimSpace(s)
				if s == "" {
					return fmt.Errorf("email is required")
				}
				if !strings.Contains(s, "@") {
					return fmt.Errorf("invalid email address")
				}
				return nil
			}).
			Run()
		if err != nil {
			return err
		}
		email = strings.TrimSpace(email)
	}

	cmd.style.Header("Renew Certificate")
	hostname := code + ".dashboard.sx"
	cmd.style.Printf("Renewing certificate for %s...\n", cmd.style.Bold(hostname))
	cmd.style.Blank()

	// Create DNS provider via dashboard.sx service
	serviceURL := dashboardsx.DefaultServiceURL
	if cfg.Network != nil && cfg.Network.DashboardSX != nil && cfg.Network.DashboardSX.ServiceURL != "" {
		serviceURL = cfg.Network.DashboardSX.ServiceURL
	}

	instanceKey, err := dashboardsx.EnsureInstanceKey()
	if err != nil {
		return fmt.Errorf("failed to read instance key: %w", err)
	}

	client := dashboardsx.NewClient(serviceURL, instanceKey, code)
	client.OnLog = func(msg string) {
		cmd.style.Info(msg)
	}

	provider, err := dashboardsx.NewServiceDNSProvider(client)
	if err != nil {
		return fmt.Errorf("failed to start cert provisioning: %w", err)
	}
	provider.OnStatus = func(status, msg string) {
		cmd.style.Info(msg)
	}

	onStatus := func(status, msg string) {
		cmd.style.Info(msg)
	}

	if err := dashboardsx.ProvisionCert(code, email, false, provider, onStatus); err != nil {
		return fmt.Errorf("certificate renewal failed: %w", err)
	}

	cmd.style.Blank()
	cmd.style.Success("Certificate renewed successfully!")
	cmd.style.Blank()
	cmd.style.Info("Restart the daemon to use the new certificate:")
	cmd.style.Code("schmux stop && schmux start")
	cmd.style.Blank()

	return nil
}

// pollProvisionStatus fetches the provision status from the daemon.
func pollProvisionStatus(statusURL string) (status, domain, message string, err error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(statusURL)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	var result struct {
		Status  string `json:"status"`
		Domain  string `json:"domain"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", "", err
	}
	return result.Status, result.Domain, result.Message, nil
}

// openBrowser opens the given URL in the user's default browser.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
}
