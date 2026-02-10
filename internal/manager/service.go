package manager

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/alperreha/mergen-fire/internal/firecracker"
	"github.com/alperreha/mergen-fire/internal/hooks"
	"github.com/alperreha/mergen-fire/internal/lock"
	"github.com/alperreha/mergen-fire/internal/model"
	"github.com/alperreha/mergen-fire/internal/network"
	"github.com/alperreha/mergen-fire/internal/store"
	"github.com/alperreha/mergen-fire/internal/systemd"
)

type Store interface {
	SaveVM(id string, cfg model.VMConfig, meta model.VMMetadata, hooks model.HooksConfig, env map[string]string) (model.VMPaths, error)
	Exists(id string) (bool, error)
	ReadMeta(id string) (model.VMMetadata, error)
	ReadVMConfig(id string) (model.VMConfig, error)
	ReadHooks(id string) (model.HooksConfig, error)
	ReadGlobalHooks() (model.HooksConfig, error)
	ListVMIDs() ([]string, error)
	ListMetas() ([]model.VMMetadata, error)
	DeleteVM(id string, retainData bool) error
	PathsFor(id string) model.VMPaths
}

type Service struct {
	store     Store
	systemd   systemd.Client
	hooks     *hooks.Runner
	allocator *network.Allocator
	logger    *slog.Logger
}

func NewService(store Store, systemdClient systemd.Client, hookRunner *hooks.Runner, allocator *network.Allocator, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		store:     store,
		systemd:   systemdClient,
		hooks:     hookRunner,
		allocator: allocator,
		logger:    logger,
	}
}

func (s *Service) CreateVM(ctx context.Context, req model.CreateVMRequest) (string, error) {
	s.logger.Debug(
		"create vm request received",
		"rootfs", req.RootFS,
		"kernel", req.Kernel,
		"vcpu", req.VCPU,
		"memMiB", req.MemMiB,
		"portRequests", len(req.Ports),
		"autoStart", req.AutoStart,
	)

	if err := validateCreate(req); err != nil {
		s.logger.Debug("create vm validation failed", "error", err)
		return "", fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	if err := validatePathExists(req.RootFS); err != nil {
		s.logger.Debug("create vm rootfs validation failed", "path", req.RootFS, "error", err)
		return "", fmt.Errorf("%w: rootfs %v", ErrInvalidRequest, err)
	}
	if err := validatePathExists(req.Kernel); err != nil {
		s.logger.Debug("create vm kernel validation failed", "path", req.Kernel, "error", err)
		return "", fmt.Errorf("%w: kernel %v", ErrInvalidRequest, err)
	}
	if strings.TrimSpace(req.DataDisk) != "" {
		if err := validatePathExists(req.DataDisk); err != nil {
			s.logger.Debug("create vm data disk validation failed", "path", req.DataDisk, "error", err)
			return "", fmt.Errorf("%w: dataDisk %v", ErrInvalidRequest, err)
		}
	}

	metas, err := s.store.ListMetas()
	if err != nil {
		return "", err
	}

	guestIP, ports, err := s.allocator.Allocate(metas, req.Ports)
	if err != nil {
		s.logger.Debug("resource allocation failed", "error", err)
		return "", fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	s.logger.Debug("resource allocation completed", "guestIP", guestIP, "allocatedPorts", len(ports))

	vmID, err := newUUIDv4()
	if err != nil {
		return "", err
	}

	meta := model.VMMetadata{
		ID:        vmID,
		CreatedAt: time.Now().UTC(),
		RootFS:    req.RootFS,
		Kernel:    req.Kernel,
		DataDisk:  req.DataDisk,
		Ports:     ports,
		GuestIP:   guestIP,
		TapName:   network.TapName(vmID),
		NetNS:     network.NetNSName(vmID),
		Metadata:  req.Metadata,
		Tags:      req.Tags,
		Hooks:     req.Hooks,
	}

	vmCfg := firecracker.RenderVMConfig(req, meta)
	hooksCfg := hooksFromMap(req.Hooks)
	paths := s.store.PathsFor(vmID)
	meta.Paths = paths
	env := s.baseEnv(meta, paths, req.ExtraEnv)
	if _, err := s.store.SaveVM(vmID, vmCfg, meta, hooksCfg, env); err != nil {
		s.logger.Error("failed to persist vm files", "vmID", vmID, "error", err)
		return "", err
	}
	s.logger.Debug("vm files persisted", "vmID", vmID, "configDir", paths.ConfigDir)

	s.triggerHooks(model.HookOnCreate, meta, nil)

	if req.AutoStart {
		s.logger.Debug("auto-start enabled, starting vm", "vmID", vmID)
		if err := s.StartVM(ctx, vmID); err != nil {
			return "", err
		}
	}

	s.logger.Info("vm created", "vmID", vmID, "guestIP", guestIP, "publishedPorts", len(ports))
	return vmID, nil
}

func (s *Service) StartVM(ctx context.Context, id string) error {
	s.logger.Debug("start vm requested", "vmID", id)
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: id is empty", ErrInvalidRequest)
	}
	exists, err := s.store.Exists(id)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}

	release, err := s.lockVM(id)
	if err != nil {
		return err
	}
	defer release()

	if err := s.systemd.Start(ctx, id); err != nil {
		if errors.Is(err, systemd.ErrUnavailable) || errors.Is(err, systemd.ErrUnitNotFound) {
			return fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		return err
	}

	meta, err := s.store.ReadMeta(id)
	if err == nil {
		s.triggerHooks(model.HookOnStart, meta, nil)
	}
	s.logger.Info("vm started", "vmID", id)
	return nil
}

func (s *Service) StopVM(ctx context.Context, id string) error {
	s.logger.Debug("stop vm requested", "vmID", id)
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: id is empty", ErrInvalidRequest)
	}
	exists, err := s.store.Exists(id)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}

	release, err := s.lockVM(id)
	if err != nil {
		return err
	}
	defer release()

	if err := s.systemd.Stop(ctx, id); err != nil {
		if errors.Is(err, systemd.ErrUnavailable) || errors.Is(err, systemd.ErrUnitNotFound) {
			return fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		return err
	}

	meta, err := s.store.ReadMeta(id)
	if err == nil {
		s.triggerHooks(model.HookOnStop, meta, nil)
	}
	s.logger.Info("vm stopped", "vmID", id)
	return nil
}

func (s *Service) DeleteVM(ctx context.Context, id string, retainData bool) error {
	s.logger.Debug("delete vm requested", "vmID", id, "retainData", retainData)
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: id is empty", ErrInvalidRequest)
	}
	exists, err := s.store.Exists(id)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}

	release, err := s.lockVM(id)
	if err != nil {
		return err
	}
	defer release()

	meta, err := s.store.ReadMeta(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	vmHooks, err := s.store.ReadHooks(id)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.logger.Warn("read vm hooks before delete failed", "vmID", id, "error", err)
	}

	if err := s.systemd.Stop(ctx, id); err != nil && !errors.Is(err, systemd.ErrUnavailable) {
		s.logger.Warn("stop unit before delete failed", "vmID", id, "error", err)
	}
	if err := s.systemd.Disable(ctx, id); err != nil && !errors.Is(err, systemd.ErrUnavailable) {
		s.logger.Warn("disable unit before delete failed", "vmID", id, "error", err)
	}

	if err := s.store.DeleteVM(id, retainData); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	s.triggerHooks(model.HookOnDelete, meta, &vmHooks)
	s.logger.Info("vm deleted", "vmID", id, "retainData", retainData)
	return nil
}

func (s *Service) GetVM(ctx context.Context, id string) (model.VMSummary, error) {
	s.logger.Debug("get vm requested", "vmID", id)
	if strings.TrimSpace(id) == "" {
		return model.VMSummary{}, fmt.Errorf("%w: id is empty", ErrInvalidRequest)
	}
	meta, err := s.store.ReadMeta(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return model.VMSummary{}, ErrNotFound
		}
		return model.VMSummary{}, err
	}

	systemdStatus, err := s.systemd.Status(ctx, id)
	if err != nil && !errors.Is(err, systemd.ErrUnavailable) {
		return model.VMSummary{}, err
	}

	socketPresent, err := firecracker.SocketPresent(meta.Paths.SocketPath)
	if err != nil {
		return model.VMSummary{}, err
	}
	s.logger.Debug("vm status collected", "vmID", id, "systemdActive", systemdStatus.Active, "socketPresent", socketPresent)

	return model.VMSummary{
		ID:        meta.ID,
		CreatedAt: meta.CreatedAt,
		Systemd: model.SystemdState{
			Available:   systemdStatus.Available,
			Unit:        systemdStatus.Unit,
			Active:      systemdStatus.Active,
			ActiveState: systemdStatus.ActiveState,
			SubState:    systemdStatus.SubState,
			MainPID:     systemdStatus.MainPID,
		},
		Firecracker: model.FirecrackerState{
			SocketPath:    meta.Paths.SocketPath,
			SocketPresent: socketPresent,
		},
		Network: model.NetworkState{
			GuestIP: meta.GuestIP,
			Ports:   meta.Ports,
			TapName: meta.TapName,
			NetNS:   meta.NetNS,
		},
		Paths:    meta.Paths,
		Metadata: meta.Metadata,
	}, nil
}

func (s *Service) ListVMs(ctx context.Context) ([]model.VMSummary, error) {
	s.logger.Debug("list vms requested")
	ids, err := s.store.ListVMIDs()
	if err != nil {
		return nil, err
	}

	result := make([]model.VMSummary, 0, len(ids))
	for _, id := range ids {
		vm, getErr := s.GetVM(ctx, id)
		if getErr != nil {
			if errors.Is(getErr, ErrNotFound) {
				continue
			}
			return nil, getErr
		}
		result = append(result, vm)
	}

	slices.SortFunc(result, func(a, b model.VMSummary) int {
		return b.CreatedAt.Compare(a.CreatedAt)
	})
	s.logger.Debug("list vms completed", "count", len(result))
	return result, nil
}

func (s *Service) baseEnv(meta model.VMMetadata, paths model.VMPaths, extra map[string]string) map[string]string {
	env := map[string]string{
		"MGN_VM_ID":       meta.ID,
		"MGN_CONFIG_DIR":  paths.ConfigDir,
		"MGN_VM_JSON":     paths.VMConfigPath,
		"MGN_META_JSON":   paths.MetaPath,
		"MGN_HOOKS_JSON":  paths.HooksPath,
		"MGN_RUN_DIR":     paths.RunDir,
		"MGN_SOCKET_PATH": paths.SocketPath,
		"MGN_TAP_NAME":    meta.TapName,
		"MGN_NETNS":       meta.NetNS,
		"MGN_GUEST_IP":    meta.GuestIP,
		"MGN_DATA_DIR":    paths.DataDir,
		"MGN_LOG_DIR":     paths.LogsDir,
	}

	for _, p := range meta.Ports {
		env[fmt.Sprintf("MGN_PUBLISH_%d", p.Guest)] = fmt.Sprintf("%d/%s", p.Host, p.Protocol)
	}
	for key, value := range extra {
		if strings.TrimSpace(key) == "" {
			continue
		}
		env[key] = value
	}
	return env
}

func (s *Service) triggerHooks(event string, meta model.VMMetadata, vmHooksOverride *model.HooksConfig) {
	if s.hooks == nil {
		s.logger.Debug("hook runner unavailable, skipping event", "vmID", meta.ID, "event", event)
		return
	}

	vmHooks := model.HooksConfig{}
	if vmHooksOverride != nil {
		vmHooks = *vmHooksOverride
	} else {
		readHooks, err := s.store.ReadHooks(meta.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			s.logger.Warn("read vm hooks failed", "vmID", meta.ID, "error", err)
		} else {
			vmHooks = readHooks
		}
	}

	globalHooks, err := s.store.ReadGlobalHooks()
	if err != nil {
		s.logger.Warn("read global hooks failed", "vmID", meta.ID, "error", err)
	}

	eventHooks := append(hooksForEvent(globalHooks, event), hooksForEvent(vmHooks, event)...)
	s.logger.Debug("triggering hooks", "vmID", meta.ID, "event", event, "hookCount", len(eventHooks))
	s.hooks.RunAsync(event, eventHooks, hookContext(meta))
}

func hookContext(meta model.VMMetadata) model.HookContext {
	hostPorts := make([]int, 0, len(meta.Ports))
	guestPorts := make([]int, 0, len(meta.Ports))
	for _, p := range meta.Ports {
		hostPorts = append(hostPorts, p.Host)
		guestPorts = append(guestPorts, p.Guest)
	}
	return model.HookContext{
		ID:         meta.ID,
		HostPorts:  hostPorts,
		GuestPorts: guestPorts,
		GuestIP:    meta.GuestIP,
		CreatedAt:  meta.CreatedAt,
		Paths:      meta.Paths,
		Metadata:   meta.Metadata,
	}
}

func hooksFromMap(hookMap map[string][]model.HookEntry) model.HooksConfig {
	if len(hookMap) == 0 {
		return model.HooksConfig{}
	}
	return model.HooksConfig{
		OnCreate: append([]model.HookEntry(nil), hookMap[model.HookOnCreate]...),
		OnDelete: append([]model.HookEntry(nil), hookMap[model.HookOnDelete]...),
		OnStart:  append([]model.HookEntry(nil), hookMap[model.HookOnStart]...),
		OnStop:   append([]model.HookEntry(nil), hookMap[model.HookOnStop]...),
	}
}

func hooksForEvent(cfg model.HooksConfig, event string) []model.HookEntry {
	switch event {
	case model.HookOnCreate:
		return cfg.OnCreate
	case model.HookOnDelete:
		return cfg.OnDelete
	case model.HookOnStart:
		return cfg.OnStart
	case model.HookOnStop:
		return cfg.OnStop
	default:
		return nil
	}
}

func (s *Service) lockVM(id string) (func(), error) {
	lockPath := s.store.PathsFor(id).LockPath
	s.logger.Debug("acquiring vm lock", "vmID", id, "lockPath", lockPath)
	lockHandle, err := lock.Acquire(lockPath)
	if err != nil {
		if errors.Is(err, lock.ErrAlreadyLocked) {
			s.logger.Debug("vm lock already held", "vmID", id, "lockPath", lockPath)
			return nil, ErrConflict
		}
		return nil, err
	}
	s.logger.Debug("vm lock acquired", "vmID", id, "lockPath", lockPath)
	return func() {
		if releaseErr := lockHandle.Release(); releaseErr != nil {
			s.logger.Warn("failed to release lock", "lockPath", lockPath, "error", releaseErr)
			return
		}
		s.logger.Debug("vm lock released", "vmID", id, "lockPath", lockPath)
	}, nil
}

func validateCreate(req model.CreateVMRequest) error {
	if strings.TrimSpace(req.RootFS) == "" {
		return errors.New("rootfs is required")
	}
	if strings.TrimSpace(req.Kernel) == "" {
		return errors.New("kernel is required")
	}
	if req.VCPU <= 0 {
		return errors.New("vcpu must be > 0")
	}
	if req.MemMiB < 128 {
		return errors.New("memMiB must be >= 128")
	}
	for _, p := range req.Ports {
		if p.Guest <= 0 || p.Guest > 65535 {
			return fmt.Errorf("invalid guest port: %d", p.Guest)
		}
		if p.Host < 0 || p.Host > 65535 {
			return fmt.Errorf("invalid host port: %d", p.Host)
		}
	}
	return nil
}

func validatePathExists(path string) error {
	stat, err := os.Stat(path)
	if err != nil {
		return err
	}
	if stat.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	return nil
}

func newUUIDv4() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		raw[0:4],
		raw[4:6],
		raw[6:8],
		raw[8:10],
		raw[10:16],
	), nil
}
