package model

import "time"

const (
	HookOnCreate = "onCreate"
	HookOnDelete = "onDelete"
	HookOnStart  = "onStart"
	HookOnStop   = "onStop"
)

type CreateVMRequest struct {
	RootFS    string                 `json:"rootfs"`
	Kernel    string                 `json:"kernel"`
	DataDisk  string                 `json:"dataDisk,omitempty"`
	VCPU      int                    `json:"vcpu"`
	MemMiB    int                    `json:"memMiB"`
	Ports     []PortBindingRequest   `json:"ports,omitempty"`
	Metadata  map[string]any         `json:"metadata,omitempty"`
	AutoStart bool                   `json:"autoStart,omitempty"`
	BootArgs  string                 `json:"bootArgs,omitempty"`
	ExtraEnv  map[string]string      `json:"extraEnv,omitempty"`
	Tags      map[string]string      `json:"tags,omitempty"`
	Hooks     map[string][]HookEntry `json:"hooks,omitempty"`
}

type PortBindingRequest struct {
	Guest    int    `json:"guest"`
	Host     int    `json:"host"`
	Protocol string `json:"protocol,omitempty"`
}

type PortBinding struct {
	Guest    int    `json:"guest"`
	Host     int    `json:"host"`
	Protocol string `json:"protocol"`
}

type VMPaths struct {
	ConfigDir    string `json:"configDir"`
	VMConfigPath string `json:"vmConfigPath"`
	MetaPath     string `json:"metaPath"`
	HooksPath    string `json:"hooksPath"`
	EnvPath      string `json:"envPath"`
	RunDir       string `json:"runDir"`
	SocketPath   string `json:"socketPath"`
	LockPath     string `json:"lockPath"`
	DataDir      string `json:"dataDir"`
	LogsDir      string `json:"logsDir"`
}

type VMMetadata struct {
	ID        string                 `json:"id"`
	CreatedAt time.Time              `json:"createdAt"`
	RootFS    string                 `json:"rootfs"`
	Kernel    string                 `json:"kernel"`
	DataDisk  string                 `json:"dataDisk,omitempty"`
	Ports     []PortBinding          `json:"ports"`
	GuestIP   string                 `json:"guestIP"`
	TapName   string                 `json:"tapName"`
	NetNS     string                 `json:"netns"`
	Metadata  map[string]any         `json:"metadata,omitempty"`
	Tags      map[string]string      `json:"tags,omitempty"`
	Paths     VMPaths                `json:"paths"`
	Hooks     map[string][]HookEntry `json:"hooks,omitempty"`
}

type HookEntry struct {
	Type      string            `json:"type"`
	URL       string            `json:"url,omitempty"`
	Cmd       []string          `json:"cmd,omitempty"`
	TimeoutMs int               `json:"timeoutMs,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Strict    bool              `json:"strict,omitempty"`
}

type HooksConfig struct {
	OnCreate []HookEntry `json:"onCreate,omitempty"`
	OnDelete []HookEntry `json:"onDelete,omitempty"`
	OnStart  []HookEntry `json:"onStart,omitempty"`
	OnStop   []HookEntry `json:"onStop,omitempty"`
}

type HookContext struct {
	ID         string         `json:"id"`
	HostPorts  []int          `json:"hostPorts"`
	GuestPorts []int          `json:"guestPorts"`
	GuestIP    string         `json:"guestIP"`
	CreatedAt  time.Time      `json:"createdAt"`
	Paths      VMPaths        `json:"paths"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type VMSummary struct {
	ID          string           `json:"id"`
	CreatedAt   time.Time        `json:"createdAt"`
	Systemd     SystemdState     `json:"systemd"`
	Firecracker FirecrackerState `json:"firecracker"`
	Network     NetworkState     `json:"network"`
	Paths       VMPaths          `json:"paths"`
	Metadata    map[string]any   `json:"metadata,omitempty"`
}

type SystemdState struct {
	Available   bool   `json:"available"`
	Unit        string `json:"unit"`
	Active      bool   `json:"active"`
	ActiveState string `json:"activeState,omitempty"`
	SubState    string `json:"subState,omitempty"`
	MainPID     int    `json:"mainPID,omitempty"`
}

type FirecrackerState struct {
	SocketPath    string `json:"socketPath"`
	SocketPresent bool   `json:"socketPresent"`
}

type NetworkState struct {
	GuestIP string        `json:"guestIP"`
	Ports   []PortBinding `json:"ports"`
	TapName string        `json:"tapName"`
	NetNS   string        `json:"netns"`
}

type VMConfig struct {
	BootSource        BootSource         `json:"boot-source"`
	Drives            []Drive            `json:"drives"`
	MachineConfig     MachineConfig      `json:"machine-config"`
	NetworkInterfaces []NetworkInterface `json:"network-interfaces"`
	Vsock             *Vsock             `json:"vsock,omitempty"`
}

type BootSource struct {
	KernelImagePath string `json:"kernel_image_path"`
	BootArgs        string `json:"boot_args"`
}

type Drive struct {
	DriveID      string `json:"drive_id"`
	PathOnHost   string `json:"path_on_host"`
	IsRootDevice bool   `json:"is_root_device"`
	IsReadOnly   bool   `json:"is_read_only"`
}

type MachineConfig struct {
	VCPUCount  int  `json:"vcpu_count"`
	MemSizeMiB int  `json:"mem_size_mib"`
	SMT        bool `json:"smt"`
}

type NetworkInterface struct {
	IfaceID     string `json:"iface_id"`
	HostDevName string `json:"host_dev_name"`
	GuestMAC    string `json:"guest_mac,omitempty"`
}

type Vsock struct {
	VsockID  string `json:"vsock_id"`
	GuestCID int    `json:"guest_cid"`
	UdsPath  string `json:"uds_path"`
}
