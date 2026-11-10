package consul

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type serviceRegistration struct {
	ID      string       `json:"ID"`
	Name    string       `json:"Name"`
	Address string       `json:"Address"`
	Port    int          `json:"Port"`
	Tags    []string     `json:"Tags,omitempty"`
	Check   serviceCheck `json:"Check"`
}

type serviceCheck struct {
	HTTP                           string `json:"HTTP"`
	Interval                       string `json:"Interval"`
	Timeout                        string `json:"Timeout"`
	DeregisterCriticalServiceAfter string `json:"DeregisterCriticalServiceAfter,omitempty"`
}

// Registrar handles self-registration and deregistration against a Consul Agent via HTTP.
type Registrar struct {
	consulAddr  string
	serviceID   string
	serviceName string
	serviceAddr string
	servicePort int
	client      *http.Client
}

// NewRegistrar builds a Consul registrar resolving the container hostname for multi-replica uniqueness.
func NewRegistrar(consulAddr, serviceName, serviceAddr, portStr string) (*Registrar, error) {
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("consul: invalid service port %q: %w", portStr, err)
	}

	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "extractor-local"
	}

	if serviceAddr == "" {
		serviceAddr = hostname
	}

	if !strings.HasPrefix(consulAddr, "http://") && !strings.HasPrefix(consulAddr, "https://") {
		consulAddr = "http://" + consulAddr
	}
	consulAddr = strings.TrimRight(consulAddr, "/")

	return &Registrar{
		consulAddr:  consulAddr,
		serviceID:   fmt.Sprintf("%s-%s", serviceName, hostname),
		serviceName: serviceName,
		serviceAddr: serviceAddr,
		servicePort: port,
		client:      &http.Client{Timeout: 5 * time.Second},
	}, nil
}

// ServiceID returns the unique instance identifier registered in Consul.
func (r *Registrar) ServiceID() string {
	return r.serviceID
}

// Register sends PUT /v1/agent/service/register with the JSON payload and health check definition.
func (r *Registrar) Register(ctx context.Context) error {
	payload := serviceRegistration{
		ID:      r.serviceID,
		Name:    r.serviceName,
		Address: r.serviceAddr,
		Port:    r.servicePort,
		Tags:    []string{"worker", "pdf", "extractor"},
		Check: serviceCheck{
			HTTP:                           fmt.Sprintf("http://%s:%d/ready", r.serviceAddr, r.servicePort),
			Interval:                       "10s",
			Timeout:                        "3s",
			DeregisterCriticalServiceAfter: "1m",
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("consul marshal: %w", err)
	}

	url := r.consulAddr + "/v1/agent/service/register"
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("consul request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("consul register: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("consul register returned unexpected status %d", resp.StatusCode)
	}

	return nil
}

// Deregister sends PUT /v1/agent/service/deregister/:service_id on graceful shutdown.
func (r *Registrar) Deregister(ctx context.Context) error {
	url := fmt.Sprintf("%s/v1/agent/service/deregister/%s", r.consulAddr, r.serviceID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, nil)
	if err != nil {
		return fmt.Errorf("consul deregister request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("consul deregister: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("consul deregister returned unexpected status %d", resp.StatusCode)
	}

	return nil
}
