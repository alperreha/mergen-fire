package firecracker

import (
	"fmt"
	"net/netip"
	"path/filepath"
	"strings"

	"github.com/alperreha/mergen-fire/internal/model"
	"github.com/alperreha/mergen-fire/internal/network"
)

const defaultBootArgs = "console=ttyS0 reboot=k panic=1 pci=off random.trust_cpu=on random.trust_bootloader=on"
const (
	defaultGuestMask   = "255.255.255.0"
	defaultGuestIfName = "eth0"
)

func RenderVMConfig(req model.CreateVMRequest, meta model.VMMetadata) model.VMConfig {
	bootArgs := resolvedBootArgs(req.BootArgs, meta.GuestIP)

	drives := []*model.Drive{
		{
			DriveID:      model.StringPtr("rootfs"),
			PathOnHost:   model.StringPtr(req.RootFS),
			IsRootDevice: model.BoolPtr(true),
			IsReadOnly:   model.BoolPtr(false),
		},
	}

	if strings.TrimSpace(req.AgentDisk) != "" {
		drives = append(drives, &model.Drive{
			DriveID:      model.StringPtr("agent"),
			PathOnHost:   model.StringPtr(req.AgentDisk),
			IsRootDevice: model.BoolPtr(false),
			IsReadOnly:   model.BoolPtr(true),
		})
	}

	if strings.TrimSpace(req.PayloadDisk) != "" {
		drives = append(drives, &model.Drive{
			DriveID:      model.StringPtr("payload"),
			PathOnHost:   model.StringPtr(req.PayloadDisk),
			IsRootDevice: model.BoolPtr(false),
			IsReadOnly:   model.BoolPtr(false),
		})
	}

	if strings.TrimSpace(req.EnvDisk) != "" {
		drives = append(drives, &model.Drive{
			DriveID:      model.StringPtr("env"),
			PathOnHost:   model.StringPtr(req.EnvDisk),
			IsRootDevice: model.BoolPtr(false),
			IsReadOnly:   model.BoolPtr(true),
		})
	}
	if strings.TrimSpace(req.DataDisk) != "" {
		drives = append(drives, &model.Drive{
			DriveID:      model.StringPtr("data"),
			PathOnHost:   model.StringPtr(req.DataDisk),
			IsRootDevice: model.BoolPtr(false),
			IsReadOnly:   model.BoolPtr(false),
		})
	}

	cfg := model.VMConfig{
		BootSource: &model.BootSource{
			KernelImagePath: model.StringPtr(req.Kernel),
			BootArgs:        bootArgs,
		},
		Drives: drives,
		MachineConfig: &model.MachineConfig{
			VcpuCount:  model.Int64Ptr(int64(req.VCPU)),
			MemSizeMib: model.Int64Ptr(int64(req.MemMiB)),
			Smt:        model.BoolPtr(false),
		},
		NetworkInterfaces: []*model.NetworkInterface{
			{
				IfaceID:     model.StringPtr("eth0"),
				HostDevName: model.StringPtr(meta.TapName),
				GuestMac:    network.GuestMAC(meta.ID),
			},
		},
	}

	if vsock, ok := resolveVsock(req, meta); ok {
		cfg.Vsock = vsock
	}

	return cfg
}

func resolveVsock(req model.CreateVMRequest, meta model.VMMetadata) (*model.Vsock, bool) {
	enabled := req.VSockEnabled ||
		req.VSockGuestCID > 0 ||
		strings.TrimSpace(req.VSockUDSPath) != "" ||
		strings.TrimSpace(req.VSockID) != ""
	if !enabled {
		return nil, false
	}

	guestCID := req.VSockGuestCID
	if guestCID < 3 {
		guestCID = 3
	}

	udsPath := strings.TrimSpace(req.VSockUDSPath)
	if udsPath == "" {
		runDir := strings.TrimSpace(meta.Paths.RunDir)
		if runDir == "" {
			runDir = filepath.Join("/run/mergen", meta.ID)
		}
		udsPath = filepath.Join(runDir, "mergen.vsock")
	}

	vsockID := strings.TrimSpace(req.VSockID)
	if vsockID == "" {
		vsockID = "mergen"
	}

	return &model.Vsock{
		GuestCid: model.Int64Ptr(guestCID),
		UdsPath:  model.StringPtr(udsPath),
		VsockID:  vsockID,
	}, true
}

func resolvedBootArgs(requested, guestIP string) string {
	bootArgs := strings.TrimSpace(requested)
	if bootArgs == "" {
		bootArgs = defaultBootArgs
	}

	if !hasKernelArgWithPrefix(bootArgs, "ip=") {
		if kernelIPArg, ok := buildKernelIPArg(guestIP); ok {
			bootArgs += " " + kernelIPArg
		}
	}

	return strings.Join(strings.Fields(bootArgs), " ")
}

func hasKernelArgWithPrefix(bootArgs, prefix string) bool {
	for _, arg := range strings.Fields(bootArgs) {
		if strings.HasPrefix(arg, prefix) {
			return true
		}
	}
	return false
}

func buildKernelIPArg(guestIP string) (string, bool) {
	addr, err := netip.ParseAddr(strings.TrimSpace(guestIP))
	if err != nil || !addr.Is4() {
		return "", false
	}

	octets := addr.As4()
	gatewayLast := byte(1)
	if octets[3] == gatewayLast {
		gatewayLast = 2
	}
	gateway := fmt.Sprintf("%d.%d.%d.%d", octets[0], octets[1], octets[2], gatewayLast)
	return fmt.Sprintf("ip=%s::%s:%s::%s:off", addr.String(), gateway, defaultGuestMask, defaultGuestIfName), true
}
