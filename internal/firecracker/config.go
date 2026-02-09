package firecracker

import (
	"strings"

	"github.com/alperreha/mergen-fire/internal/model"
	"github.com/alperreha/mergen-fire/internal/network"
)

const defaultBootArgs = "console=ttyS0 reboot=k panic=1 pci=off"

func RenderVMConfig(req model.CreateVMRequest, meta model.VMMetadata) model.VMConfig {
	bootArgs := strings.TrimSpace(req.BootArgs)
	if bootArgs == "" {
		bootArgs = defaultBootArgs
	}

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
