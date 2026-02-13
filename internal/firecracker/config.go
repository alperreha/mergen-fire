package firecracker

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/alperreha/mergen-fire/internal/model"
	"github.com/alperreha/mergen-fire/internal/network"
)

const defaultBootArgs = "console=ttyS0 reboot=k panic=1 pci=off"
const (
	defaultGuestMask   = "255.255.255.0"
	defaultGuestIfName = "eth0"
)

func RenderVMConfig(req model.CreateVMRequest, meta model.VMMetadata) model.VMConfig {
	bootArgs := resolvedBootArgs(req.BootArgs, meta.GuestIP)

	drives := []model.Drive{
		{
			DriveID:      "rootfs",
			PathOnHost:   req.RootFS,
			IsRootDevice: true,
			IsReadOnly:   false,
		},
	}

	if strings.TrimSpace(req.DataDisk) != "" {
		drives = append(drives, model.Drive{
			DriveID:      "data",
			PathOnHost:   req.DataDisk,
			IsRootDevice: false,
			IsReadOnly:   false,
		})
	}

	return model.VMConfig{
		BootSource: model.BootSource{
			KernelImagePath: req.Kernel,
			BootArgs:        bootArgs,
		},
		Drives: drives,
		MachineConfig: model.MachineConfig{
			VCPUCount:  req.VCPU,
			MemSizeMiB: req.MemMiB,
			SMT:        false,
		},
		NetworkInterfaces: []model.NetworkInterface{
			{
				IfaceID:     "eth0",
				HostDevName: meta.TapName,
				GuestMAC:    network.GuestMAC(meta.ID),
			},
		},
	}
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
