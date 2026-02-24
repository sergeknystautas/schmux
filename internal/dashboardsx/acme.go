package dashboardsx

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
)

// acmeUser implements the lego registration.User interface.
type acmeUser struct {
	Email        string
	Registration *registration.Resource
	Key          crypto.PrivateKey
}

func (u *acmeUser) GetEmail() string                        { return u.Email }
func (u *acmeUser) GetRegistration() *registration.Resource { return u.Registration }
func (u *acmeUser) GetPrivateKey() crypto.PrivateKey        { return u.Key }

// StatusFunc is called by ServiceDNSProvider and ProvisionCert to report progress.
type StatusFunc func(status, message string)

// ServiceDNSProvider implements the lego challenge.Provider interface using the
// dashboard.sx service API for automated DNS-01 challenges.
type ServiceDNSProvider struct {
	client         *Client
	challengeToken string // set after CertProvisioningStart
	OnStatus       StatusFunc
}

var _ challenge.Provider = (*ServiceDNSProvider)(nil)

// NewServiceDNSProvider creates a provider that uses the dashboard.sx API.
// It calls /cert-provisioning/start to get a challenge token before use.
func NewServiceDNSProvider(client *Client) (*ServiceDNSProvider, error) {
	resp, err := client.CertProvisioningStart()
	if err != nil {
		return nil, fmt.Errorf("failed to start cert provisioning: %w", err)
	}
	return &ServiceDNSProvider{
		client:         client,
		challengeToken: resp.ChallengeToken,
	}, nil
}

// Present calls dashboard.sx to create a TXT record for the ACME challenge.
// The keyAuth must be hashed (base64url(SHA256(keyAuth))) per the ACME DNS-01 spec.
func (p *ServiceDNSProvider) Present(domain, token, keyAuth string) error {
	fqdn, value := dns01.GetRecord(domain, keyAuth)
	if p.OnStatus != nil {
		p.OnStatus("dns_create", fmt.Sprintf("Creating DNS TXT record for %s...", fqdn))
	}
	if err := p.client.DNSChallengeCreate(p.challengeToken, value); err != nil {
		return err
	}
	if p.OnStatus != nil {
		p.OnStatus("dns_verify", fmt.Sprintf("TXT record created. Verifying DNS propagation for %s...", fqdn))
	}
	return nil
}

// CleanUp calls dashboard.sx to remove the TXT record and invalidate the challenge token.
func (p *ServiceDNSProvider) CleanUp(domain, token, keyAuth string) error {
	if p.OnStatus != nil {
		p.OnStatus("dns_cleanup", fmt.Sprintf("DNS verified. Cleaning up TXT record for %s...", domain))
	}
	return p.client.DNSChallengeDelete(p.challengeToken)
}

// ProvisionCert runs the full ACME certificate provisioning flow.
// It obtains a certificate for <code>.dashboard.sx using the given DNS provider.
// If staging is true, uses the Let's Encrypt staging server.
// onStatus is called at each step so callers can display progress.
func ProvisionCert(code, email string, staging bool, provider challenge.Provider, onStatus StatusFunc) error {
	report := func(status, msg string) {
		if onStatus != nil {
			onStatus(status, msg)
		}
	}

	if err := EnsureDir(); err != nil {
		return fmt.Errorf("failed to create dashboardsx directory: %w", err)
	}

	domain := code + ".dashboard.sx"

	report("acme_account", "Loading ACME account...")
	reg, key, err := LoadOrCreateAccount(email)
	if err != nil {
		return fmt.Errorf("failed to load/create ACME account: %w", err)
	}

	user := &acmeUser{
		Email:        email,
		Registration: reg,
		Key:          key,
	}

	caServer := "production"
	legoCfg := lego.NewConfig(user)
	if staging {
		legoCfg.CADirURL = "https://acme-staging-v02.api.letsencrypt.org/directory"
		caServer = "staging"
	}
	legoCfg.Certificate.KeyType = certcrypto.EC256

	report("acme_client", fmt.Sprintf("Creating ACME client (Let's Encrypt %s)...", caServer))
	client, err := lego.NewClient(legoCfg)
	if err != nil {
		return fmt.Errorf("failed to create ACME client: %w", err)
	}

	// Set DNS provider. Query dashboard.sx's authoritative nameservers directly
	// to verify TXT records, bypassing recursive resolvers that cache negative responses.
	if err := client.Challenge.SetDNS01Provider(provider,
		dns01.AddRecursiveNameservers([]string{
			"ns-1174.awsdns-18.org:53",
			"ns-1727.awsdns-23.co.uk:53",
			"ns-357.awsdns-44.com:53",
			"ns-751.awsdns-29.net:53",
		}),
	); err != nil {
		return fmt.Errorf("failed to set DNS provider: %w", err)
	}

	// Register account if not already registered
	if user.Registration == nil {
		report("acme_register", "Registering with Let's Encrypt...")
		reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if err != nil {
			return fmt.Errorf("failed to register ACME account: %w", err)
		}
		user.Registration = reg
	}

	report("cert_request", fmt.Sprintf("Requesting certificate for %s (DNS challenge)...", domain))
	request := certificate.ObtainRequest{
		Domains: []string{domain},
		Bundle:  true,
	}

	certificates, err := client.Certificate.Obtain(request)
	if err != nil {
		return fmt.Errorf("failed to obtain certificate: %w", err)
	}

	report("cert_save", "Certificate received. Saving...")
	return SaveCert(certificates.Certificate, certificates.PrivateKey)
}

// LoadOrCreateAccount loads an existing ACME account key or creates a new one.
// Returns the registration resource (nil if new) and the private key.
func LoadOrCreateAccount(email string) (*registration.Resource, crypto.PrivateKey, error) {
	accountPath, err := ACMEAccountPath()
	if err != nil {
		return nil, nil, err
	}

	// Try to load existing key
	data, err := os.ReadFile(accountPath)
	if err == nil {
		block, _ := pem.Decode(data)
		if block != nil {
			key, err := x509.ParseECPrivateKey(block.Bytes)
			if err == nil {
				// Existing key found - return with nil registration (will be looked up)
				return nil, key, nil
			}
		}
	}

	// Generate new EC key
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate account key: %w", err)
	}

	// Save key
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal account key: %w", err)
	}

	pemBlock := &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	}

	if err := os.WriteFile(accountPath, pem.EncodeToMemory(pemBlock), 0600); err != nil {
		return nil, nil, fmt.Errorf("failed to save account key: %w", err)
	}

	return nil, key, nil
}

// SaveCert saves the certificate and private key to the dashboardsx directory.
func SaveCert(certBytes, keyBytes []byte) error {
	certPath, err := CertPath()
	if err != nil {
		return err
	}
	keyPath, err := KeyPath()
	if err != nil {
		return err
	}

	if err := os.WriteFile(certPath, certBytes, 0600); err != nil {
		return fmt.Errorf("failed to write certificate: %w", err)
	}
	if err := os.WriteFile(keyPath, keyBytes, 0600); err != nil {
		return fmt.Errorf("failed to write key: %w", err)
	}

	return nil
}

// GetCertExpiry parses the certificate and returns the NotAfter time.
func GetCertExpiry() (time.Time, error) {
	certPath, err := CertPath()
	if err != nil {
		return time.Time{}, err
	}

	data, err := os.ReadFile(certPath)
	if err != nil {
		return time.Time{}, err
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return time.Time{}, fmt.Errorf("no PEM block found in certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, err
	}

	return cert.NotAfter, nil
}
