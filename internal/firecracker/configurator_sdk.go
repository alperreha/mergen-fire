package firecracker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	fcclient "github.com/firecracker-microvm/firecracker-go-sdk/client"
	fcmodels "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	fcops "github.com/firecracker-microvm/firecracker-go-sdk/client/operations"
	"github.com/go-openapi/runtime"
	httptransport "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"

	"github.com/alperreha/mergen-fire/internal/model"
)

type SDKConfigurator struct {
	timeout time.Duration
	logger  *slog.Logger
}

func NewSDKConfigurator(timeout time.Duration) *SDKConfigurator {
	return &SDKConfigurator{
		timeout: timeout,
		logger:  slog.Default(),
	}
}

func (s *SDKConfigurator) WithLogger(logger *slog.Logger) *SDKConfigurator {
	if logger != nil {
		s.logger = logger
	}
	return s
}

func (s *SDKConfigurator) ConfigureAndStart(ctx context.Context, socketPath string, cfg model.VMConfig, opts ConfigureOptions) error {
	if s.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}

	opsClient := newOperationsClient(socketPath)
	s.logger.Debug("configuring firecracker via sdk", "socketPath", socketPath, "drives", len(cfg.Drives), "networkIfaces", len(cfg.NetworkInterfaces))

	if err := putMachineConfiguration(ctx, opsClient, cfg.MachineConfig); err != nil {
		return fmt.Errorf("machine-config: %w", err)
	}
	if err := putBootSource(ctx, opsClient, cfg.BootSource); err != nil {
		return fmt.Errorf("boot-source: %w", err)
	}
	if opts.EnableEntropyDevice {
		if err := putEntropyDevice(ctx, socketPath); err != nil {
			return fmt.Errorf("entropy-device: %w", err)
		}
		s.logger.Debug("entropy device configured", "socketPath", socketPath)
	}
	for _, drive := range cfg.Drives {
		driveName := "<nil>"
		if drive != nil {
			driveName = model.StringValue(drive.DriveID)
		}
		if err := putDrive(ctx, opsClient, drive); err != nil {
			return fmt.Errorf("drive %s: %w", driveName, err)
		}
	}
	for _, nic := range cfg.NetworkInterfaces {
		ifaceID := "<nil>"
		if nic != nil {
			ifaceID = model.StringValue(nic.IfaceID)
		}
		if err := putNetworkInterface(ctx, opsClient, nic); err != nil {
			return fmt.Errorf("network interface %s: %w", ifaceID, err)
		}
	}
	if cfg.Vsock != nil {
		if err := putVsock(ctx, opsClient, cfg.Vsock); err != nil {
			return fmt.Errorf("vsock: %w", err)
		}
	}
	if err := startInstance(ctx, opsClient); err != nil {
		return fmt.Errorf("instance start: %w", err)
	}

	s.logger.Debug("firecracker configuration and start action sent", "socketPath", socketPath)
	return nil
}

func newOperationsClient(socketPath string) fcops.ClientIface {
	httpClient := fcclient.NewHTTPClient(strfmt.NewFormats())
	httpClient.SetTransport(newUnixSocketTransport(socketPath))
	return httpClient.Operations
}

func newUnixSocketTransport(socketPath string) runtime.ClientTransport {
	socketTransport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}

	transport := httptransport.New(fcclient.DefaultHost, fcclient.DefaultBasePath, fcclient.DefaultSchemes)
	transport.Transport = socketTransport
	return transport
}

func putMachineConfiguration(ctx context.Context, client fcops.ClientIface, cfg *fcmodels.MachineConfiguration) error {
	params := fcops.NewPutMachineConfigurationParamsWithContext(ctx)
	params.SetBody(cfg)
	_, err := client.PutMachineConfiguration(params)
	return err
}

func putBootSource(ctx context.Context, client fcops.ClientIface, source *fcmodels.BootSource) error {
	params := fcops.NewPutGuestBootSourceParamsWithContext(ctx)
	params.SetBody(source)
	_, err := client.PutGuestBootSource(params)
	return err
}

func putDrive(ctx context.Context, client fcops.ClientIface, drive *fcmodels.Drive) error {
	if drive == nil {
		return fmt.Errorf("drive is nil")
	}
	params := fcops.NewPutGuestDriveByIDParamsWithContext(ctx)
	params.SetDriveID(model.StringValue(drive.DriveID))
	params.SetBody(drive)
	_, err := client.PutGuestDriveByID(params)
	return err
}

func putNetworkInterface(ctx context.Context, client fcops.ClientIface, nic *fcmodels.NetworkInterface) error {
	if nic == nil {
		return fmt.Errorf("network interface is nil")
	}
	params := fcops.NewPutGuestNetworkInterfaceByIDParamsWithContext(ctx)
	params.SetIfaceID(model.StringValue(nic.IfaceID))
	params.SetBody(nic)
	_, err := client.PutGuestNetworkInterfaceByID(params)
	return err
}

func putVsock(ctx context.Context, client fcops.ClientIface, vsock *fcmodels.Vsock) error {
	params := fcops.NewPutGuestVsockParamsWithContext(ctx)
	params.SetBody(vsock)
	_, err := client.PutGuestVsock(params)
	return err
}

func startInstance(ctx context.Context, client fcops.ClientIface) error {
	params := fcops.NewCreateSyncActionParamsWithContext(ctx)
	action := fcmodels.InstanceActionInfo{
		ActionType: stringPtr(fcmodels.InstanceActionInfoActionTypeInstanceStart),
	}
	params.SetInfo(&action)
	_, err := client.CreateSyncAction(params)
	return err
}

func putEntropyDevice(ctx context.Context, socketPath string) error {
	requestBody := bytes.NewBufferString("{}")
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, "http://localhost/entropy", requestBody)
	if err != nil {
		return fmt.Errorf("build entropy request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "unix", socketPath)
			},
		},
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("send entropy request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNoContent {
		bodyBytes, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		body := strings.TrimSpace(string(bodyBytes))
		if body == "" {
			return fmt.Errorf("unexpected status code %d", response.StatusCode)
		}
		return fmt.Errorf("unexpected status code %d: %s", response.StatusCode, body)
	}

	return nil
}

func stringPtr(value string) *string {
	return &value
}
