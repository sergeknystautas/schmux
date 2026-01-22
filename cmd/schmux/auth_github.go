package main

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/sergeknystautas/schmux/internal/config"
)

type AuthGitHubCommand struct {
	style   *termStyle
	homeDir string

	// Collected values
	hostname      string
	port          int
	certPath      string
	keyPath       string
	clientID      string
	clientSecret  string
	networkAccess bool
	sessionTTL    int
}

// publicBaseURL builds the full URL from hostname and port
func (cmd *AuthGitHubCommand) publicBaseURL() string {
	if cmd.port == 443 {
		return "https://" + cmd.hostname
	}
	return fmt.Sprintf("https://%s:%d", cmd.hostname, cmd.port)
}

func NewAuthGitHubCommand() *AuthGitHubCommand {
	return &AuthGitHubCommand{
		style: newTermStyle(),
	}
}

func (cmd *AuthGitHubCommand) Run(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("unknown arguments: %s", strings.Join(args, " "))
	}

	// Ensure config exists
	if !config.ConfigExists() {
		ok, err := config.EnsureExists()
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("config not created")
		}
	}

	// Load existing config and secrets
	var err error
	cmd.homeDir, err = os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	configPath := filepath.Join(cmd.homeDir, ".schmux", "config.json")
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	secrets, _ := config.GetAuthSecrets()

	// Initialize defaults from existing config
	cmd.initDefaults(cfg, &secrets)

	// Show introduction and ask if they want to enable auth
	cmd.showIntroduction()

	// Step 0: Ask if they want to enable auth
	enabled, err := cmd.stepEnableAuth(cfg)
	if err != nil {
		return err
	}
	if !enabled {
		return cmd.disableAuth(cfg)
	}

	// Step 1: Hostname
	if err := cmd.stepHostname(); err != nil {
		return err
	}

	// Step 2: TLS Certificates
	if err := cmd.stepTLSSetup(cfg); err != nil {
		return err
	}

	// Step 3: GitHub OAuth
	if err := cmd.stepGitHubOAuth(&secrets); err != nil {
		return err
	}

	// Step 4: Additional settings
	if err := cmd.stepAdditionalSettings(); err != nil {
		return err
	}

	// Step 5: Summary and save
	return cmd.stepSummaryAndSave(cfg)
}

func (cmd *AuthGitHubCommand) initDefaults(cfg *config.Config, secrets *config.AuthSecrets) {
	// Hostname from existing config
	cmd.hostname = "schmux.local"
	if existing := cfg.GetPublicBaseURL(); existing != "" {
		if parsed, err := url.Parse(existing); err == nil && parsed.Hostname() != "" {
			cmd.hostname = parsed.Hostname()
		}
	}

	// Port from config (GetPort returns default 7337 if not set)
	cmd.port = cfg.GetPort()

	// TLS paths from existing config
	cmd.certPath = cfg.GetTLSCertPath()
	cmd.keyPath = cfg.GetTLSKeyPath()

	// GitHub credentials from secrets
	if secrets != nil && secrets.GitHub != nil {
		cmd.clientID = secrets.GitHub.ClientID
		cmd.clientSecret = secrets.GitHub.ClientSecret
	}

	// Network access and session TTL
	cmd.networkAccess = cfg.GetNetworkAccess()
	cmd.sessionTTL = cfg.GetAuthSessionTTLMinutes()
}

func (cmd *AuthGitHubCommand) showIntroduction() {
	cmd.style.Header("GitHub Authentication Setup")

	cmd.style.Info(
		"GitHub auth lets you log into the schmux dashboard using your GitHub account.",
		"",
		"To set this up, you'll need:",
	)
	cmd.style.List([]string{
		"A hostname for the dashboard (e.g., schmux.local)",
		"TLS certificates for HTTPS",
		"A GitHub OAuth App",
	})
}

// stepEnableAuth asks if the user wants to enable GitHub authentication.
// Returns true if they want to enable, false if they want to disable.
func (cmd *AuthGitHubCommand) stepEnableAuth(cfg *config.Config) (bool, error) {
	cmd.style.Blank()

	// Show current status
	currentlyEnabled := cfg.GetAuthEnabled()
	if currentlyEnabled {
		cmd.style.Printf("Authentication is currently %s\n", cmd.style.Green("enabled"))
	} else {
		cmd.style.Printf("Authentication is currently %s\n", cmd.style.Yellow("disabled"))
	}
	cmd.style.Blank()

	enabled := currentlyEnabled
	err := huh.NewConfirm().
		Title("Enable GitHub authentication?").
		Description("Require GitHub login to access the dashboard").
		Affirmative("Yes, enable").
		Negative("No, disable").
		Value(&enabled).
		Run()

	if err != nil {
		return false, err
	}

	return enabled, nil
}

// disableAuth disables authentication and saves the config.
func (cmd *AuthGitHubCommand) disableAuth(cfg *config.Config) error {
	if cfg.AccessControl == nil {
		cfg.AccessControl = &config.AccessControlConfig{}
	}
	cfg.AccessControl.Enabled = false

	if err := cfg.Save(); err != nil {
		return err
	}

	cmd.style.Blank()
	cmd.style.Success("Authentication disabled")
	cmd.style.Blank()
	cmd.style.Info("The dashboard will be accessible without login.")
	cmd.style.Info("Restart the daemon for changes to take effect:")
	cmd.style.Code("./schmux stop && ./schmux start")
	cmd.style.Blank()

	return nil
}

// Step 1: Hostname and Port
func (cmd *AuthGitHubCommand) stepHostname() error {
	cmd.style.SubHeader("Step 1: Dashboard URL")

	portStr := fmt.Sprintf("%d", cmd.port)

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Hostname").
				Description("The hostname you'll type in your browser (e.g., schmux.local)").
				Placeholder("schmux.local").
				Value(&cmd.hostname).
				Validate(validateHostname),
			huh.NewInput().
				Title("Port").
				Description("Port in the dashboard URL (e.g., https://schmux.local:7337)").
				Placeholder("7337").
				Value(&portStr).
				Validate(validatePort),
		),
	)

	if err := form.Run(); err != nil {
		return err
	}

	// Clean up hostname
	cmd.hostname = cleanHostname(cmd.hostname)

	// Parse port (validation already ensured it's valid)
	cmd.port = parsePort(portStr)

	return nil
}

// Step 2: TLS certificate setup
func (cmd *AuthGitHubCommand) stepTLSSetup(cfg *config.Config) error {
	cmd.style.SubHeader("Step 2: TLS Certificates")

	// Check for existing certs in multiple locations
	existingCert := cmd.certPath
	existingKey := cmd.keyPath

	// Also check default location for this hostname
	defaultCertPath := filepath.Join(cmd.homeDir, ".schmux", "tls", cmd.hostname+".pem")
	defaultKeyPath := filepath.Join(cmd.homeDir, ".schmux", "tls", cmd.hostname+"-key.pem")

	// If config paths don't exist but default paths do, use defaults
	if (existingCert == "" || !fileExists(existingCert)) && fileExists(defaultCertPath) && fileExists(defaultKeyPath) {
		existingCert = defaultCertPath
		existingKey = defaultKeyPath
	}

	// If we have existing certs that exist on disk, offer to keep them
	if existingCert != "" && existingKey != "" && fileExists(existingCert) && fileExists(existingKey) {
		cmd.style.Printf("Found existing certificates:\n")
		cmd.style.Printf("  Cert: %s\n", cmd.style.Cyan(shortenPath(existingCert)))
		cmd.style.Printf("  Key:  %s\n", cmd.style.Cyan(shortenPath(existingKey)))
		cmd.style.Blank()

		keepExisting := true
		err := huh.NewConfirm().
			Title("Use these certificates?").
			Affirmative("Yes").
			Negative("No, configure new ones").
			Value(&keepExisting).
			Run()

		if err != nil {
			return err
		}
		if keepExisting {
			cmd.certPath = existingCert
			cmd.keyPath = existingKey
			return nil
		}
	}

	// Ask how to get certificates
	var certChoice string
	err := huh.NewSelect[string]().
		Title("TLS certificates for "+cmd.hostname).
		Description("HTTPS requires TLS certificates").
		Options(
			huh.NewOption("Generate automatically (requires mkcert)", "generate"),
			huh.NewOption("I have my own certificates", "manual"),
		).
		Value(&certChoice).
		Run()

	if err != nil {
		return err
	}

	switch certChoice {
	case "generate":
		return cmd.generateCerts()
	case "manual":
		return cmd.promptManualCerts()
	}

	return nil
}

func (cmd *AuthGitHubCommand) promptManualCerts() error {
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("TLS certificate path").
				Value(&cmd.certPath),
			huh.NewInput().
				Title("TLS key path").
				Value(&cmd.keyPath),
		),
	)
	return form.Run()
}

func (cmd *AuthGitHubCommand) generateCerts() error {
	// Check if mkcert is installed
	mkcertPath, err := exec.LookPath("mkcert")
	if err != nil {
		cmd.style.Error("mkcert is not installed")
		cmd.style.Blank()
		cmd.style.Info("Install it first:")
		cmd.style.Code(
			"macOS:   brew install mkcert",
			"Linux:   See https://github.com/FiloSottile/mkcert#installation",
		)
		cmd.style.Blank()
		cmd.style.Info("Then run this command again.")
		return fmt.Errorf("mkcert not installed")
	}

	// Check if CA is installed
	caRootCmd := exec.Command(mkcertPath, "-CAROOT")
	caRootOutput, err := caRootCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get mkcert CA root: %w", err)
	}
	caRoot := strings.TrimSpace(string(caRootOutput))
	rootCAPath := filepath.Join(caRoot, "rootCA.pem")

	if !fileExists(rootCAPath) {
		cmd.style.Info(
			"mkcert needs to install its CA certificate (one-time setup).",
			"This may prompt for your password.",
		)
		cmd.style.Blank()

		installCA := true
		err := huh.NewConfirm().
			Title("Install mkcert CA now?").
			Affirmative("Yes").
			Negative("No").
			Value(&installCA).
			Run()

		if err != nil {
			return err
		}
		if !installCA {
			return fmt.Errorf("mkcert CA installation required")
		}

		installCmd := exec.Command(mkcertPath, "-install")
		installCmd.Stdout = os.Stdout
		installCmd.Stderr = os.Stderr
		if err := installCmd.Run(); err != nil {
			cmd.style.Error("Failed to install mkcert CA")
			return fmt.Errorf("mkcert -install failed: %w", err)
		}
		cmd.style.Success("mkcert CA installed")
		cmd.style.Blank()
	}

	// Create TLS directory
	tlsDir := filepath.Join(cmd.homeDir, ".schmux", "tls")
	if err := os.MkdirAll(tlsDir, 0755); err != nil {
		return fmt.Errorf("failed to create TLS directory: %w", err)
	}

	// Generate certificates
	certPath := filepath.Join(tlsDir, cmd.hostname+".pem")
	keyPath := filepath.Join(tlsDir, cmd.hostname+"-key.pem")

	cmd.style.Printf("Generating certificates for %s...\n", cmd.style.Bold(cmd.hostname))

	genCmd := exec.Command(mkcertPath,
		"-cert-file", certPath,
		"-key-file", keyPath,
		cmd.hostname,
	)
	genCmd.Stdout = os.Stdout
	genCmd.Stderr = os.Stderr
	if err := genCmd.Run(); err != nil {
		cmd.style.Error("Failed to generate certificates")
		return fmt.Errorf("mkcert failed: %w", err)
	}

	// Verify files were created
	if !fileExists(certPath) || !fileExists(keyPath) {
		return fmt.Errorf("certificate files were not created")
	}

	cmd.style.Success("Certificates saved to " + cmd.style.Cyan(shortenPath(tlsDir)))

	cmd.certPath = certPath
	cmd.keyPath = keyPath
	return nil
}

// Step 3: GitHub OAuth App setup
func (cmd *AuthGitHubCommand) stepGitHubOAuth(secrets *config.AuthSecrets) error {
	cmd.style.SubHeader("Step 3: GitHub OAuth App")

	publicBaseURL := cmd.publicBaseURL()

	// Check existing credentials
	if cmd.clientID != "" {
		cmd.style.Printf("Current GitHub Client ID: %s\n", cmd.style.Cyan(cmd.clientID))
		cmd.style.Blank()

		keepExisting := true
		err := huh.NewConfirm().
			Title("Keep existing GitHub OAuth credentials?").
			Affirmative("Yes").
			Negative("No, enter new ones").
			Value(&keepExisting).
			Run()

		if err != nil {
			return err
		}
		if keepExisting {
			return nil
		}
	}

	// Ask if they have credentials
	var hasApp bool
	err := huh.NewConfirm().
		Title("Have you created a GitHub OAuth App?").
		Affirmative("Yes, I have the credentials").
		Negative("No, help me create one").
		Value(&hasApp).
		Run()

	if err != nil {
		return err
	}

	if !hasApp {
		cmd.showOAuthAppGuide(publicBaseURL)
	}

	// Collect credentials
	existingSecret := cmd.clientSecret

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("GitHub Client ID").
				Value(&cmd.clientID).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("client ID is required")
					}
					return nil
				}),
			huh.NewInput().
				Title("GitHub Client Secret").
				EchoMode(huh.EchoModePassword).
				Value(&cmd.clientSecret).
				Validate(func(s string) error {
					// Allow empty if we have an existing secret
					if strings.TrimSpace(s) == "" && existingSecret == "" {
						return fmt.Errorf("client secret is required")
					}
					return nil
				}),
		),
	)

	if err := form.Run(); err != nil {
		return err
	}

	// If secret is empty but we had existing, keep it
	if strings.TrimSpace(cmd.clientSecret) == "" && existingSecret != "" {
		cmd.clientSecret = existingSecret
		cmd.style.Info("(keeping existing client secret)")
	}

	return nil
}

func (cmd *AuthGitHubCommand) showOAuthAppGuide(publicBaseURL string) {
	cmd.style.SubHeader("Creating a GitHub OAuth App")

	cmd.style.Info("1. Open: " + cmd.style.Cyan("https://github.com/settings/developers"))
	cmd.style.Blank()
	cmd.style.Info("2. Click \"New OAuth App\" and enter:")
	cmd.style.Blank()
	cmd.style.Printf("   %-26s %s\n", cmd.style.Bold("Application name:"), "schmux (or anything you like)")
	cmd.style.Printf("   %-26s %s\n", cmd.style.Bold("Homepage URL:"), cmd.style.Cyan(publicBaseURL))
	cmd.style.Printf("   %-26s %s\n", cmd.style.Bold("Authorization callback:"), cmd.style.Cyan(publicBaseURL+"/auth/callback"))
	cmd.style.Blank()
	cmd.style.Info("3. Click \"Register application\"")
	cmd.style.Blank()
	cmd.style.Info("4. On the next page, click \"Generate a new client secret\"")
	cmd.style.Blank()
	cmd.style.Info("5. Copy the Client ID and Client Secret")
	cmd.style.Blank()

	// Simple pause - huh doesn't have a "press enter" so use confirm
	ready := true
	huh.NewConfirm().
		Title("Ready to continue?").
		Affirmative("Yes").
		Negative("Go back").
		Value(&ready).
		Run()
}

// Step 4: Additional settings
func (cmd *AuthGitHubCommand) stepAdditionalSettings() error {
	cmd.style.SubHeader("Step 4: Additional Settings")

	sessionTTLStr := fmt.Sprintf("%d", cmd.sessionTTL)

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Enable network access?").
				Description("Allow other devices on your local network to reach the dashboard").
				Affirmative("Yes").
				Negative("No (localhost only)").
				Value(&cmd.networkAccess),
			huh.NewInput().
				Title("Session TTL (minutes)").
				Description("How long you stay logged in before re-authenticating").
				Value(&sessionTTLStr).
				Validate(func(s string) error {
					if s == "" {
						return nil // Will use default
					}
					var v int
					if _, err := fmt.Sscanf(s, "%d", &v); err != nil {
						return fmt.Errorf("must be a number")
					}
					if v <= 0 {
						return fmt.Errorf("must be positive")
					}
					return nil
				}),
		),
	)

	if err := form.Run(); err != nil {
		return err
	}

	// Parse session TTL
	if sessionTTLStr != "" {
		fmt.Sscanf(sessionTTLStr, "%d", &cmd.sessionTTL)
	}

	return nil
}

// Step 5: Summary and save
func (cmd *AuthGitHubCommand) stepSummaryAndSave(cfg *config.Config) error {
	publicBaseURL := cmd.publicBaseURL()

	// Validation
	warnings := cmd.validateSetup()

	cmd.style.SubHeader("Configuration Summary")

	// Shorten paths for display
	shortCert := shortenPath(cmd.certPath)
	shortKey := shortenPath(cmd.keyPath)

	// Mask client ID for display
	maskedClientID := cmd.clientID
	if len(cmd.clientID) > 10 {
		maskedClientID = cmd.clientID[:10] + "..."
	}

	networkAccessStr := "No"
	if cmd.networkAccess {
		networkAccessStr = "Yes"
	}

	ttlDisplay := fmt.Sprintf("%d minutes", cmd.sessionTTL)
	if cmd.sessionTTL == 1440 {
		ttlDisplay += " (24 hours)"
	} else if cmd.sessionTTL == 60 {
		ttlDisplay += " (1 hour)"
	}

	cmd.style.KeyValue("Dashboard URL", cmd.style.Cyan(publicBaseURL))
	cmd.style.KeyValue("TLS Certificate", shortCert)
	cmd.style.KeyValue("TLS Key", shortKey)
	cmd.style.KeyValue("GitHub Client ID", maskedClientID)
	cmd.style.KeyValue("Network Access", networkAccessStr)
	cmd.style.KeyValue("Session TTL", ttlDisplay)

	cmd.style.Blank()

	// Show validation results
	if len(warnings) == 0 {
		cmd.style.Success("All validation checks passed")
	} else {
		cmd.style.Println(cmd.style.Yellow("Warnings:"))
		for _, w := range warnings {
			cmd.style.Printf("  %s %s\n", cmd.style.Yellow("⚠"), w)
		}
	}

	cmd.style.Blank()

	// Confirm save
	proceed := true
	title := "Save configuration?"
	if len(warnings) > 0 {
		title = "Save anyway?"
		proceed = false // Default to No when there are warnings
	}

	err := huh.NewConfirm().
		Title(title).
		Affirmative("Yes").
		Negative("No").
		Value(&proceed).
		Run()

	if err != nil {
		return err
	}
	if !proceed {
		cmd.style.Blank()
		cmd.style.Println("Setup cancelled.")
		return nil
	}

	// Save
	if err := cmd.saveConfig(cfg, publicBaseURL); err != nil {
		return err
	}
	if err := config.SaveGitHubAuthSecrets(cmd.clientID, cmd.clientSecret); err != nil {
		return err
	}

	cmd.showNextSteps()
	return nil
}

func (cmd *AuthGitHubCommand) validateSetup() []string {
	var warnings []string

	publicBaseURL := cmd.publicBaseURL()

	// Validate public base URL
	if !config.IsValidPublicBaseURL(publicBaseURL) {
		warnings = append(warnings, "Public base URL must be https (http://localhost allowed)")
	}

	// Validate cert files exist
	if cmd.certPath == "" {
		warnings = append(warnings, "TLS certificate path is required")
	} else if !fileExists(cmd.certPath) {
		warnings = append(warnings, fmt.Sprintf("TLS certificate not found: %s", cmd.certPath))
	}

	if cmd.keyPath == "" {
		warnings = append(warnings, "TLS key path is required")
	} else if !fileExists(cmd.keyPath) {
		warnings = append(warnings, fmt.Sprintf("TLS key not found: %s", cmd.keyPath))
	}

	// Validate cert matches hostname
	if cmd.certPath != "" && fileExists(cmd.certPath) {
		if err := certMatchesHost(cmd.certPath, cmd.hostname); err != nil {
			warnings = append(warnings, fmt.Sprintf("Certificate does not match hostname %s: %v", cmd.hostname, err))
		}
	}

	// Validate GitHub credentials
	if strings.TrimSpace(cmd.clientID) == "" {
		warnings = append(warnings, "GitHub Client ID is required")
	}
	if strings.TrimSpace(cmd.clientSecret) == "" {
		warnings = append(warnings, "GitHub Client Secret is required")
	}

	return warnings
}

func (cmd *AuthGitHubCommand) saveConfig(cfg *config.Config, publicBaseURL string) error {
	// Network config
	if cfg.Network == nil {
		cfg.Network = &config.NetworkConfig{}
	}
	if cmd.networkAccess {
		cfg.Network.BindAddress = "0.0.0.0"
	} else {
		cfg.Network.BindAddress = "127.0.0.1"
	}
	cfg.Network.Port = cmd.port
	cfg.Network.PublicBaseURL = publicBaseURL
	cfg.Network.TLS = &config.TLSConfig{
		CertPath: cmd.certPath,
		KeyPath:  cmd.keyPath,
	}

	// Access control config
	if cfg.AccessControl == nil {
		cfg.AccessControl = &config.AccessControlConfig{}
	}
	cfg.AccessControl.Enabled = true
	cfg.AccessControl.Provider = "github"
	cfg.AccessControl.SessionTTLMinutes = cmd.sessionTTL

	return cfg.Save()
}

func (cmd *AuthGitHubCommand) showNextSteps() {
	cmd.style.SubHeader("Setup Complete")

	cmd.style.Success("Configuration saved")
	cmd.style.Blank()

	cmd.style.Println("Next steps:")
	cmd.style.Blank()

	// Check if hostname is likely in /etc/hosts
	stepNum := 1
	if cmd.hostname != "localhost" && !strings.HasSuffix(cmd.hostname, ".localhost") {
		cmd.style.Info(fmt.Sprintf("%d. Add %s to /etc/hosts if you haven't already:", stepNum, cmd.hostname))
		cmd.style.Code(fmt.Sprintf("echo \"127.0.0.1 %s\" | sudo tee -a /etc/hosts", cmd.hostname))
		cmd.style.Blank()
		stepNum++
	}

	cmd.style.Info(fmt.Sprintf("%d. Restart the daemon:", stepNum))
	cmd.style.Code("./schmux stop && ./schmux start")
	cmd.style.Blank()
	stepNum++

	dashboardURL := cmd.publicBaseURL()
	cmd.style.Info(fmt.Sprintf("%d. Open %s in your browser", stepNum, cmd.style.Cyan(dashboardURL)))
	cmd.style.Blank()
}

// Validation and parsing helpers

func validateHostname(s string) error {
	s = cleanHostname(s)
	if s == "" {
		return fmt.Errorf("hostname cannot be empty")
	}
	if strings.Contains(s, " ") {
		return fmt.Errorf("hostname cannot contain spaces")
	}
	if strings.Contains(s, "/") {
		return fmt.Errorf("enter just the hostname, not a full URL path")
	}
	return nil
}

func validatePort(s string) error {
	if s == "" {
		return nil // Will use default
	}
	p := parsePort(s)
	if p < 1 || p > 65535 {
		return fmt.Errorf("must be between 1 and 65535")
	}
	return nil
}

func cleanHostname(s string) string {
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimSuffix(s, "/")
	// Remove port if included
	if idx := strings.LastIndex(s, ":"); idx > 0 {
		s = s[:idx]
	}
	return s
}

func parsePort(s string) int {
	if s == "" {
		return 7337
	}
	var p int
	if _, err := fmt.Sscanf(s, "%d", &p); err != nil || p < 1 {
		return 7337
	}
	return p
}

// File helpers

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func shortenPath(path string) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, homeDir) {
		return "~" + strings.TrimPrefix(path, homeDir)
	}
	return path
}

func certMatchesHost(certPath, host string) error {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return err
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return fmt.Errorf("no PEM block found")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}

	return cert.VerifyHostname(host)
}
