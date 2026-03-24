//go:build !nodashboardsx

package dashboardsx

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// mockConfig implements ConfigReader for testing.
type mockConfig struct {
	enabled  bool
	code     string
	ip       string
	hostname string
}

func (m *mockConfig) GetDashboardSXEnabled() bool    { return m.enabled }
func (m *mockConfig) GetDashboardSXCode() string     { return m.code }
func (m *mockConfig) GetDashboardSXIP() string       { return m.ip }
func (m *mockConfig) GetDashboardSXHostname() string { return m.hostname }

func TestGetStatus_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// Create the dashboardsx dir (empty)
	if err := os.MkdirAll(filepath.Join(tmpDir, ".schmux", "dashboardsx"), 0700); err != nil {
		t.Fatal(err)
	}

	cfg := &mockConfig{}
	status, err := GetStatus(cfg)
	if err != nil {
		t.Fatalf("GetStatus() error: %v", err)
	}

	if status.HasInstanceKey {
		t.Error("HasInstanceKey should be false for empty dir")
	}
	if status.HasCert {
		t.Error("HasCert should be false for empty dir")
	}
	if status.Enabled {
		t.Error("Enabled should be false")
	}
}

func TestGetStatus_WithCert(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	dir := filepath.Join(tmpDir, ".schmux", "dashboardsx")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}

	// Create a self-signed test certificate
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	expiry := time.Now().Add(60 * 24 * time.Hour) // 60 days
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"Test"}},
		NotBefore:    time.Now(),
		NotAfter:     expiry,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	if err := os.WriteFile(filepath.Join(dir, "cert.pem"), certPEM, 0600); err != nil {
		t.Fatal(err)
	}

	// Also create instance key
	if err := os.WriteFile(filepath.Join(dir, "instance.key"), []byte("a"+string(make([]byte, 63))), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := &mockConfig{
		enabled:  true,
		code:     "12345",
		ip:       "192.168.1.100",
		hostname: "12345.dashboard.sx",
	}

	status, err := GetStatus(cfg)
	if err != nil {
		t.Fatalf("GetStatus() error: %v", err)
	}

	if !status.HasCert {
		t.Error("HasCert should be true")
	}
	if !status.Enabled {
		t.Error("Enabled should be true")
	}
	if status.Code != "12345" {
		t.Errorf("Code = %q, want %q", status.Code, "12345")
	}
	if status.Hostname != "12345.dashboard.sx" {
		t.Errorf("Hostname = %q, want %q", status.Hostname, "12345.dashboard.sx")
	}
	if status.DaysUntilExpiry < 59 || status.DaysUntilExpiry > 61 {
		t.Errorf("DaysUntilExpiry = %d, expected ~60", status.DaysUntilExpiry)
	}
}
