//go:build vendorlocked

package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// All vendorlocked tests use DIRECT handler invocation (httptest.NewRecorder
// + handler call), NOT httptest.NewServer. Mirrors TestAPIContract_Healthz
// in api_contract_test.go:143 and uses the existing newTestServer(t) +
// newTestConfigHandlers(s) helpers from api_contract_test.go.

func sptr(s string) *string { return &s }
func bptr(b bool) *bool     { return &b }
func iptr(i int) *int       { return &i }

func postConfigUpdate(t *testing.T, configH *ConfigHandlers, req contracts.ConfigUpdateRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	httpReq := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	configH.handleConfigUpdate(rr, httpReq)
	return rr
}

func assertStatus(t *testing.T, rr *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rr.Code != want {
		t.Fatalf("status = %d, want %d (body: %s)", rr.Code, want, rr.Body.String())
	}
}

func assertBodyContains(t *testing.T, rr *httptest.ResponseRecorder, want string) {
	t.Helper()
	if !strings.Contains(rr.Body.String(), want) {
		t.Fatalf("body = %s, expected to contain %q", rr.Body.String(), want)
	}
}

func TestVendorLocked_RejectBindAddressWrite(t *testing.T) {
	server, _, _ := newTestServer(t)
	configH := newTestConfigHandlers(server)
	rr := postConfigUpdate(t, configH, contracts.ConfigUpdateRequest{
		Network: &contracts.NetworkUpdate{BindAddress: sptr("0.0.0.0")},
	})
	assertStatus(t, rr, http.StatusBadRequest)
	assertBodyContains(t, rr, "network.bind_address")
}

func TestVendorLocked_AcceptBindAddressLoopback(t *testing.T) {
	server, _, _ := newTestServer(t)
	configH := newTestConfigHandlers(server)
	rr := postConfigUpdate(t, configH, contracts.ConfigUpdateRequest{
		Network: &contracts.NetworkUpdate{BindAddress: sptr("127.0.0.1")},
	})
	assertStatus(t, rr, http.StatusOK)
}

func TestVendorLocked_RejectPublicBaseURLWrite(t *testing.T) {
	server, _, _ := newTestServer(t)
	configH := newTestConfigHandlers(server)
	rr := postConfigUpdate(t, configH, contracts.ConfigUpdateRequest{
		Network: &contracts.NetworkUpdate{PublicBaseURL: sptr("https://attacker.com")},
	})
	assertStatus(t, rr, http.StatusBadRequest)
	assertBodyContains(t, rr, "network.public_base_url")
}

func TestVendorLocked_RejectTLSCertPathWrite(t *testing.T) {
	server, _, _ := newTestServer(t)
	configH := newTestConfigHandlers(server)
	rr := postConfigUpdate(t, configH, contracts.ConfigUpdateRequest{
		Network: &contracts.NetworkUpdate{TLS: &contracts.TLSUpdate{CertPath: sptr("/etc/ssl/c")}},
	})
	assertStatus(t, rr, http.StatusBadRequest)
	assertBodyContains(t, rr, "network.tls.cert_path")
}

func TestVendorLocked_RejectTLSKeyPathWrite(t *testing.T) {
	server, _, _ := newTestServer(t)
	configH := newTestConfigHandlers(server)
	rr := postConfigUpdate(t, configH, contracts.ConfigUpdateRequest{
		Network: &contracts.NetworkUpdate{TLS: &contracts.TLSUpdate{KeyPath: sptr("/etc/ssl/k")}},
	})
	assertStatus(t, rr, http.StatusBadRequest)
	assertBodyContains(t, rr, "network.tls.key_path")
}

func TestVendorLocked_RejectAccessControlEnabledWrite(t *testing.T) {
	server, _, _ := newTestServer(t)
	configH := newTestConfigHandlers(server)
	rr := postConfigUpdate(t, configH, contracts.ConfigUpdateRequest{
		AccessControl: &contracts.AccessControlUpdate{Enabled: bptr(true)},
	})
	assertStatus(t, rr, http.StatusBadRequest)
	assertBodyContains(t, rr, "access_control.enabled")
}

func TestVendorLocked_RejectRemoteAccessEnabledWrite(t *testing.T) {
	server, _, _ := newTestServer(t)
	configH := newTestConfigHandlers(server)
	rr := postConfigUpdate(t, configH, contracts.ConfigUpdateRequest{
		RemoteAccess: &contracts.RemoteAccessUpdate{Enabled: bptr(true)},
	})
	assertStatus(t, rr, http.StatusBadRequest)
	assertBodyContains(t, rr, "remote_access.enabled")
}

// Round-trip tests: the dashboard form reposts the entire config including
// the values served by the locked getters. Those writes must be accepted as
// no-ops so saving an unrelated field (e.g. adding a repo) doesn't fail.

func TestVendorLocked_AcceptPublicBaseURLRoundTrip(t *testing.T) {
	server, _, _ := newTestServer(t)
	configH := newTestConfigHandlers(server)
	// Form sends back the locked-getter value verbatim.
	roundtrip := server.config.GetPublicBaseURL()
	rr := postConfigUpdate(t, configH, contracts.ConfigUpdateRequest{
		Network: &contracts.NetworkUpdate{PublicBaseURL: &roundtrip},
	})
	assertStatus(t, rr, http.StatusOK)
}

func TestVendorLocked_AcceptInertAccessControlFields(t *testing.T) {
	server, _, _ := newTestServer(t)
	configH := newTestConfigHandlers(server)
	// Provider and SessionTTLMinutes are inert when access_control.enabled
	// is locked false; their values should not block the save.
	rr := postConfigUpdate(t, configH, contracts.ConfigUpdateRequest{
		AccessControl: &contracts.AccessControlUpdate{
			Provider:          sptr("github"),
			SessionTTLMinutes: iptr(60),
		},
	})
	assertStatus(t, rr, http.StatusOK)
}

func TestVendorLocked_AcceptInertRemoteAccessFields(t *testing.T) {
	server, _, _ := newTestServer(t)
	configH := newTestConfigHandlers(server)
	// Timeout and Notify are inert when remote_access.enabled is locked false.
	rr := postConfigUpdate(t, configH, contracts.ConfigUpdateRequest{
		RemoteAccess: &contracts.RemoteAccessUpdate{
			TimeoutMinutes: iptr(120),
			Notify: &contracts.RemoteAccessNotifyUpdate{
				NtfyTopic: sptr("topic"),
				Command:   sptr("echo hi"),
			},
		},
	})
	assertStatus(t, rr, http.StatusOK)
}

func TestVendorLocked_RejectRemoteAccessSetPassword(t *testing.T) {
	server, _, _ := newTestServer(t)
	body := []byte(`{"password":"hunter2hunter2"}`)
	httpReq := httptest.NewRequest(http.MethodPost, "/api/remote-access/set-password", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.handleRemoteAccessSetPassword(rr, httpReq)
	assertStatus(t, rr, http.StatusServiceUnavailable)
}

func TestVendorLocked_ValidatorCoversEveryField(t *testing.T) {
	// For each exported field of NetworkUpdate, AccessControlUpdate,
	// RemoteAccessUpdate (and nested *TLSUpdate / *RemoteAccessNotifyUpdate),
	// build a ConfigUpdateRequest setting ONLY that field to a non-zero
	// value, run the validator, and assert it appears in the rejected
	// list OR is in vendorLockedAllowlist.

	targets := []struct {
		name string
		typ  reflect.Type
	}{
		{"Network", reflect.TypeOf(contracts.NetworkUpdate{})},
		{"AccessControl", reflect.TypeOf(contracts.AccessControlUpdate{})},
		{"RemoteAccess", reflect.TypeOf(contracts.RemoteAccessUpdate{})},
	}
	for _, tgt := range targets {
		for i := 0; i < tgt.typ.NumField(); i++ {
			f := tgt.typ.Field(i)
			if !f.IsExported() {
				continue
			}
			fqn := tgt.name + "." + f.Name
			if vendorLockedAllowlist[fqn] {
				continue
			}
			t.Run(fqn, func(t *testing.T) {
				server, _, _ := newTestServer(t)
				req := buildSingleFieldRequest(t, tgt.name, f.Name, f.Type)
				err := validateVendorLockedWrite(&req, server.config)
				if err == nil {
					t.Fatalf("field %s not rejected and not in allowlist", fqn)
				}
			})
		}
	}
}

// buildSingleFieldRequest constructs a ConfigUpdateRequest where only
// the named field on the named subtree is set to a non-zero / non-empty
// value. Walks reflection to handle pointer types and nested *XxxUpdate.
func buildSingleFieldRequest(t *testing.T, sub, field string, fieldType reflect.Type) contracts.ConfigUpdateRequest {
	t.Helper()
	var req contracts.ConfigUpdateRequest

	// Build a pointer to the subtree struct, set the named field, attach
	// to the request.
	var subPtr reflect.Value
	switch sub {
	case "Network":
		v := contracts.NetworkUpdate{}
		subPtr = reflect.ValueOf(&v)
		req.Network = &v
	case "AccessControl":
		v := contracts.AccessControlUpdate{}
		subPtr = reflect.ValueOf(&v)
		req.AccessControl = &v
	case "RemoteAccess":
		v := contracts.RemoteAccessUpdate{}
		subPtr = reflect.ValueOf(&v)
		req.RemoteAccess = &v
	default:
		t.Fatalf("unknown subtree: %s", sub)
	}

	// Set the field. Field is on subPtr.Elem().
	fieldVal := subPtr.Elem().FieldByName(field)
	if !fieldVal.CanSet() {
		t.Skipf("field %s.%s not settable via reflection", sub, field)
	}

	// Allocate a non-zero value of the field's type.
	nonZero := nonZeroValue(t, sub, field, fieldType)
	if !nonZero.IsValid() {
		t.Skipf("field %s.%s: no non-zero builder for type %v", sub, field, fieldType)
	}
	fieldVal.Set(nonZero)

	return req
}

// Note: this test invokes handleTLSValidate directly via httptest.NewRecorder
// rather than through the chi router. The route at server.go:811 is
// registered as GET-only (a pre-existing bug, out of scope), so a real POST
// would return 405 from chi BEFORE reaching the handler. Direct invocation
// bypasses chi routing entirely and exercises the vendorlocked early-return.
func TestVendorLocked_RejectTLSValidate(t *testing.T) {
	server, _, _ := newTestServer(t)
	body := []byte(`{"cert_path":"/tmp/c","key_path":"/tmp/k"}`)
	httpReq := httptest.NewRequest(http.MethodPost, "/api/tls/validate", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.handleTLSValidate(rr, httpReq)
	assertStatus(t, rr, http.StatusServiceUnavailable)
	assertBodyContains(t, rr, "TLS is not configurable in this build")
}

// nonZeroValue returns a non-zero reflect.Value of the given type.
// Returns invalid Value for unsupported types (caller should t.Skip).
// Special case: BindAddress gets "0.0.0.0" (a non-loopback string), so
// the validator's "not 127.0.0.1" check rejects it.
func nonZeroValue(t *testing.T, sub, field string, ft reflect.Type) reflect.Value {
	t.Helper()
	switch ft.Kind() {
	case reflect.Ptr:
		elemType := ft.Elem()
		switch elemType.Kind() {
		case reflect.String:
			s := "x"
			if sub == "Network" && field == "BindAddress" {
				s = "0.0.0.0"
			}
			return reflect.ValueOf(&s)
		case reflect.Bool:
			b := true
			return reflect.ValueOf(&b)
		case reflect.Int:
			i := 1
			return reflect.ValueOf(&i)
		case reflect.Struct:
			// For nested *XxxUpdate (e.g., *TLSUpdate, *RemoteAccessNotifyUpdate),
			// allocate the struct AND set every leaf field on it so the
			// parent appears non-empty AND every nested field gets a
			// non-zero value the validator can recognize.
			elemPtr := reflect.New(elemType)
			for i := 0; i < elemType.NumField(); i++ {
				nf := elemType.Field(i)
				if !nf.IsExported() {
					continue
				}
				inner := nonZeroValue(t, sub, field+"."+nf.Name, nf.Type)
				if inner.IsValid() {
					elemPtr.Elem().FieldByName(nf.Name).Set(inner)
				}
			}
			return elemPtr
		}
	}
	return reflect.Value{} // unsupported
}

func TestVendorLocked_AuthSecretsGet_ReturnsEmptyOK(t *testing.T) {
	// Reads return 200 with the "not configured" empty response so that
	// ConfigPage's initial load doesn't fail. Writes still return 503.
	server, _, _ := newTestServer(t)
	configH := newTestConfigHandlers(server)
	httpReq := httptest.NewRequest(http.MethodGet, "/api/auth/secrets", nil)
	rr := httptest.NewRecorder()
	configH.handleAuthSecretsGet(rr, httpReq)
	assertStatus(t, rr, http.StatusOK)
	assertBodyContains(t, rr, `"client_id":""`)
	assertBodyContains(t, rr, `"client_secret_set":false`)
}

func TestVendorLocked_RejectAuthSecretsUpdate(t *testing.T) {
	server, _, _ := newTestServer(t)
	configH := newTestConfigHandlers(server)
	body := []byte(`{"client_id":"x","client_secret":"y"}`)
	httpReq := httptest.NewRequest(http.MethodPost, "/api/auth/secrets", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	configH.handleAuthSecretsUpdate(rr, httpReq)
	assertStatus(t, rr, http.StatusServiceUnavailable)
}
