package firecracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/alperreha/mergen-fire/internal/model"
)

type RawConfigurator struct {
	client *http.Client
}

func NewRawConfigurator(timeout time.Duration) *RawConfigurator {
	return &RawConfigurator{
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (r *RawConfigurator) ConfigureAndStart(ctx context.Context, socketPath string, cfg model.VMConfig) error {
	if err := r.doJSON(ctx, socketPath, http.MethodPut, "/boot-source", cfg.BootSource); err != nil {
		return fmt.Errorf("boot-source: %w", err)
	}
	if err := r.doJSON(ctx, socketPath, http.MethodPut, "/machine-config", cfg.MachineConfig); err != nil {
		return fmt.Errorf("machine-config: %w", err)
	}
	for _, drive := range cfg.Drives {
		drivePath := path.Join("/drives", url.PathEscape(drive.DriveID))
		if err := r.doJSON(ctx, socketPath, http.MethodPut, drivePath, drive); err != nil {
			return fmt.Errorf("drive %s: %w", drive.DriveID, err)
		}
	}
	for _, nic := range cfg.NetworkInterfaces {
		nicPath := path.Join("/network-interfaces", url.PathEscape(nic.IfaceID))
		if err := r.doJSON(ctx, socketPath, http.MethodPut, nicPath, nic); err != nil {
			return fmt.Errorf("network interface %s: %w", nic.IfaceID, err)
		}
	}
	if cfg.Vsock != nil {
		if err := r.doJSON(ctx, socketPath, http.MethodPut, "/vsock", cfg.Vsock); err != nil {
			return fmt.Errorf("vsock: %w", err)
		}
	}

	return r.doJSON(ctx, socketPath, http.MethodPut, "/actions", map[string]string{
		"action_type": "InstanceStart",
	})
}

func (r *RawConfigurator) doJSON(ctx context.Context, socketPath, method, endpoint string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, method, "http://firecracker"+endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")

	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	defer transport.CloseIdleConnections()

	client := *r.client
	client.Transport = transport

	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("firecracker api status: %s", response.Status)
	}
	return nil
}
