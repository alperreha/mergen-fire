package model

import (
	"errors"
	"fmt"
	"strings"

	fcmodels "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
)

// VMConfig is backed by Firecracker's native FullVMConfiguration model.
type VMConfig = fcmodels.FullVMConfiguration

// Aliases for Firecracker native models used in vm.json.
type BootSource = fcmodels.BootSource
type Drive = fcmodels.Drive
type MachineConfig = fcmodels.MachineConfiguration
type NetworkInterface = fcmodels.NetworkInterface

func StringPtr(value string) *string {
	return &value
}

func BoolPtr(value bool) *bool {
	return &value
}

func Int64Ptr(value int64) *int64 {
	return &value
}

func StringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func BoolValue(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

func Int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func ValidateVMConfig(cfg VMConfig) error {
	if cfg.BootSource == nil {
		return errors.New("boot-source is required")
	}
	if strings.TrimSpace(StringValue(cfg.BootSource.KernelImagePath)) == "" {
		return errors.New("boot-source.kernel_image_path is empty")
	}

	if cfg.MachineConfig == nil {
		return errors.New("machine-config is required")
	}
	if Int64Value(cfg.MachineConfig.VcpuCount) <= 0 {
		return errors.New("machine-config.vcpu_count must be greater than zero")
	}
	if Int64Value(cfg.MachineConfig.MemSizeMib) <= 0 {
		return errors.New("machine-config.mem_size_mib must be greater than zero")
	}

	if len(cfg.Drives) == 0 {
		return errors.New("drives is empty")
	}
	rootDriveFound := false
	for _, drive := range cfg.Drives {
		if drive == nil {
			return errors.New("drives has nil entry")
		}
		driveID := strings.TrimSpace(StringValue(drive.DriveID))
		if driveID == "" {
			return errors.New("drive_id is empty")
		}
		pathOnHost := strings.TrimSpace(StringValue(drive.PathOnHost))
		if pathOnHost == "" {
			return fmt.Errorf("drive %s path_on_host is empty", driveID)
		}
		if BoolValue(drive.IsRootDevice) {
			rootDriveFound = true
		}
	}
	if !rootDriveFound {
		return errors.New("no root drive found")
	}

	for _, nic := range cfg.NetworkInterfaces {
		if nic == nil {
			return errors.New("network-interfaces has nil entry")
		}
		ifaceID := strings.TrimSpace(StringValue(nic.IfaceID))
		if ifaceID == "" {
			return errors.New("network-interfaces.iface_id is empty")
		}
		hostDevName := strings.TrimSpace(StringValue(nic.HostDevName))
		if hostDevName == "" {
			return fmt.Errorf("network interface %s host_dev_name is empty", ifaceID)
		}
	}

	return nil
}
