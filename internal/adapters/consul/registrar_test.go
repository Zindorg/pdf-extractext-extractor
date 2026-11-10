package consul

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegistrar_RegisterAndDeregister_SendsExpectedJSON(t *testing.T) {
	var receivedReg serviceRegistration
	var deregisteredPath string

	mockConsul := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s; want PUT", r.Method)
		}

		switch {
		case r.URL.Path == "/v1/agent/service/register":
			if err := json.NewDecoder(r.Body).Decode(&receivedReg); err != nil {
				t.Fatalf("failed to decode JSON payload: %v", err)
			}
			w.WriteHeader(http.StatusOK)

		case strings.HasPrefix(r.URL.Path, "/v1/agent/service/deregister/"):
			deregisteredPath = r.URL.Path
			w.WriteHeader(http.StatusOK)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockConsul.Close()

	reg, err := NewRegistrar(mockConsul.URL, "extractor", "10.0.0.5", "8080")
	if err != nil {
		t.Fatalf("NewRegistrar failed: %v", err)
	}

	if err := reg.Register(context.Background()); err != nil {
		t.Fatalf("Register() failed: %v", err)
	}

	if receivedReg.Name != "extractor" {
		t.Errorf("Name = %q; want 'extractor'", receivedReg.Name)
	}
	if receivedReg.Address != "10.0.0.5" {
		t.Errorf("Address = %q; want '10.0.0.5'", receivedReg.Address)
	}
	if receivedReg.Port != 8080 {
		t.Errorf("Port = %d; want 8080", receivedReg.Port)
	}
	if receivedReg.Check.HTTP != "http://10.0.0.5:8080/ready" {
		t.Errorf("Check.HTTP = %q; want 'http://10.0.0.5:8080/ready'", receivedReg.Check.HTTP)
	}

	if err := reg.Deregister(context.Background()); err != nil {
		t.Fatalf("Deregister() failed: %v", err)
	}

	expectedDeregPath := "/v1/agent/service/deregister/" + reg.ServiceID()
	if deregisteredPath != expectedDeregPath {
		t.Errorf("deregisteredPath = %q; want %q", deregisteredPath, expectedDeregPath)
	}
}
