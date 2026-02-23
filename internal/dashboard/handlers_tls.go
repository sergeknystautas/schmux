package dashboard

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// handleTLSValidate validates TLS certificate and key paths.
func (s *Server) handleTLSValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req contracts.TLSValidateRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONResponse(w, contracts.TLSValidateResponse{Error: "Invalid request body"})
		return
	}

	certPath := expandHome(req.CertPath)
	keyPath := expandHome(req.KeyPath)

	if certPath == "" || keyPath == "" {
		writeJSONResponse(w, contracts.TLSValidateResponse{Error: "Both cert_path and key_path are required"})
		return
	}

	// Check files exist
	if _, err := os.Stat(certPath); err != nil {
		writeJSONResponse(w, contracts.TLSValidateResponse{Error: fmt.Sprintf("Certificate file not found: %s", req.CertPath)})
		return
	}
	if _, err := os.Stat(keyPath); err != nil {
		writeJSONResponse(w, contracts.TLSValidateResponse{Error: fmt.Sprintf("Key file not found: %s", req.KeyPath)})
		return
	}

	// Validate cert+key pair
	_, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		writeJSONResponse(w, contracts.TLSValidateResponse{Error: fmt.Sprintf("Certificate and key do not match: %v", err)})
		return
	}

	// Parse cert to extract hostname and expiry
	certData, err := os.ReadFile(certPath)
	if err != nil {
		writeJSONResponse(w, contracts.TLSValidateResponse{Error: fmt.Sprintf("Cannot read certificate: %v", err)})
		return
	}

	block, _ := pem.Decode(certData)
	if block == nil {
		writeJSONResponse(w, contracts.TLSValidateResponse{Error: "No PEM block found in certificate file"})
		return
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		writeJSONResponse(w, contracts.TLSValidateResponse{Error: fmt.Sprintf("Cannot parse certificate: %v", err)})
		return
	}

	// Extract hostname: prefer SAN DNS names, fall back to CN
	hostname := ""
	if len(cert.DNSNames) > 0 {
		hostname = cert.DNSNames[0]
	} else if cert.Subject.CommonName != "" {
		hostname = cert.Subject.CommonName
	}

	expires := cert.NotAfter.Format(time.RFC3339)

	writeJSONResponse(w, contracts.TLSValidateResponse{
		Valid:    true,
		Hostname: hostname,
		Expires:  expires,
	})
}

// expandHome expands ~ to the user's home directory.
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return home + path[1:]
		}
	}
	return path
}

// writeJSONResponse writes a JSON response.
func writeJSONResponse(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
