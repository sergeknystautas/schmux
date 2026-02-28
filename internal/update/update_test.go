package update

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/version"
)

// interceptTransport redirects all HTTP requests to a test server,
// regardless of the original URL. This lets us test code that uses
// hardcoded const URLs without modifying them.
type interceptTransport struct {
	server *httptest.Server
}

func (t *interceptTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite the URL to point at the test server, preserving the path
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(t.server.URL, "http://")
	return http.DefaultTransport.RoundTrip(req)
}

// withTestServer replaces httpClient with one that routes all requests to the
// given handler, and returns a cleanup function to restore the original client.
func withTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	original := httpClient
	httpClient = &http.Client{Transport: &interceptTransport{server: server}}
	t.Cleanup(func() {
		httpClient = original
		server.Close()
	})
	return server
}

// withVersion temporarily overrides version.Version for the duration of the test.
func withVersion(t *testing.T, v string) {
	t.Helper()
	original := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = original })
}

func TestGetLatestVersion(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantVer string
		wantErr string
	}{
		{
			name:    "successful response",
			status:  http.StatusOK,
			body:    `{"tag_name": "v1.2.3"}`,
			wantVer: "1.2.3",
		},
		{
			name:    "strips v prefix",
			status:  http.StatusOK,
			body:    `{"tag_name": "v0.9.4"}`,
			wantVer: "0.9.4",
		},
		{
			name:    "no v prefix in tag",
			status:  http.StatusOK,
			body:    `{"tag_name": "2.0.0"}`,
			wantVer: "2.0.0",
		},
		{
			name:    "empty tag",
			status:  http.StatusOK,
			body:    `{"tag_name": ""}`,
			wantErr: "no release tag found",
		},
		{
			name:    "rate limited",
			status:  http.StatusForbidden,
			body:    `{"message": "rate limit exceeded"}`,
			wantErr: "rate limit exceeded",
		},
		{
			name:    "server error",
			status:  http.StatusInternalServerError,
			body:    `{}`,
			wantErr: "500",
		},
		{
			name:    "invalid JSON",
			status:  http.StatusOK,
			body:    `not json`,
			wantErr: "failed to parse release info",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))

			ver, err := GetLatestVersion()

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ver != tt.wantVer {
				t.Errorf("got version %q, want %q", ver, tt.wantVer)
			}
		})
	}
}

func TestCheckForUpdate(t *testing.T) {
	tests := []struct {
		name          string
		current       string
		latestTag     string
		wantAvailable bool
		wantVersion   string
		wantErr       bool
	}{
		{
			name:          "newer version available",
			current:       "0.9.0",
			latestTag:     "v1.0.0",
			wantAvailable: true,
			wantVersion:   "1.0.0",
		},
		{
			name:          "already up to date",
			current:       "1.0.0",
			latestTag:     "v1.0.0",
			wantAvailable: false,
			wantVersion:   "1.0.0",
		},
		{
			name:          "current is newer",
			current:       "1.0.0",
			latestTag:     "v0.9.0",
			wantAvailable: false,
			wantVersion:   "0.9.0",
		},
		{
			name:          "patch version newer",
			current:       "1.0.0",
			latestTag:     "v1.0.1",
			wantAvailable: true,
			wantVersion:   "1.0.1",
		},
		{
			name:          "minor version newer",
			current:       "1.0.9",
			latestTag:     "v1.1.0",
			wantAvailable: true,
			wantVersion:   "1.1.0",
		},
		{
			name:          "major version newer",
			current:       "1.9.9",
			latestTag:     "v2.0.0",
			wantAvailable: true,
			wantVersion:   "2.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withVersion(t, tt.current)
			withTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]string{"tag_name": tt.latestTag})
			}))

			latestVersion, updateAvailable, err := CheckForUpdate()

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if updateAvailable != tt.wantAvailable {
				t.Errorf("updateAvailable = %v, want %v", updateAvailable, tt.wantAvailable)
			}
			if latestVersion != tt.wantVersion {
				t.Errorf("latestVersion = %q, want %q", latestVersion, tt.wantVersion)
			}
		})
	}
}

func TestCheckForUpdate_DevVersion(t *testing.T) {
	withVersion(t, "dev")

	latestVersion, updateAvailable, err := CheckForUpdate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updateAvailable {
		t.Error("dev builds should never report update available")
	}
	if latestVersion != "" {
		t.Errorf("expected empty latest version for dev builds, got %q", latestVersion)
	}
}

func TestUpdate_DevVersion(t *testing.T) {
	withVersion(t, "dev")

	err := Update()
	if err == nil {
		t.Fatal("expected error for dev builds")
	}
	if !strings.Contains(err.Error(), "cannot update dev builds") {
		t.Errorf("expected 'cannot update dev builds' error, got: %v", err)
	}
}

func TestDownloadChecksums(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    map[string]string
		wantErr bool
	}{
		{
			name: "standard format",
			body: "abc123  schmux-darwin-arm64\ndef456  schmux-linux-amd64\n",
			want: map[string]string{
				"schmux-darwin-arm64": "abc123",
				"schmux-linux-amd64":  "def456",
			},
		},
		{
			name: "with assets entry",
			body: "aaa111  schmux-darwin-arm64\nbbb222  dashboard-assets.tar.gz\n",
			want: map[string]string{
				"schmux-darwin-arm64":     "aaa111",
				"dashboard-assets.tar.gz": "bbb222",
			},
		},
		{
			name: "ignores blank lines",
			body: "\nabc123  schmux-darwin-arm64\n\n",
			want: map[string]string{
				"schmux-darwin-arm64": "abc123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, tt.body)
			}))

			got, err := downloadChecksums("1.0.0")

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("checksums[%q] = %q, want %q", k, got[k], v)
				}
			}
			if len(got) != len(tt.want) {
				t.Errorf("got %d entries, want %d", len(got), len(tt.want))
			}
		})
	}
}

func TestCheckPlatformSupport(t *testing.T) {
	// This test verifies the current platform is supported (it should be,
	// since we're running tests on it). The actual supported platforms
	// are darwin/{amd64,arm64} and linux/{amd64,arm64}.
	err := checkPlatformSupport()
	if err != nil {
		t.Fatalf("current platform should be supported: %v", err)
	}
}
