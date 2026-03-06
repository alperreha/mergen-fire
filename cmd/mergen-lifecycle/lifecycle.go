package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	lifecyclehooks "github.com/alperreha/mergen-fire/cmd/mergen-lifecycle/hooks"
	hookdelete "github.com/alperreha/mergen-fire/cmd/mergen-lifecycle/hooks/delete"
	hookpostdelete "github.com/alperreha/mergen-fire/cmd/mergen-lifecycle/hooks/postdelete"
	hookpoststart "github.com/alperreha/mergen-fire/cmd/mergen-lifecycle/hooks/poststart"
	hookpoststop "github.com/alperreha/mergen-fire/cmd/mergen-lifecycle/hooks/poststop"
	hookpredelete "github.com/alperreha/mergen-fire/cmd/mergen-lifecycle/hooks/predelete"
	hookprestart "github.com/alperreha/mergen-fire/cmd/mergen-lifecycle/hooks/prestart"
	hookprestop "github.com/alperreha/mergen-fire/cmd/mergen-lifecycle/hooks/prestop"
	"github.com/alperreha/mergen-fire/internal/model"
)

type lifecycleStage string

const (
	stagePreStart   lifecycleStage = "pre-start"
	stageStart      lifecycleStage = "start"
	stagePostStart  lifecycleStage = "post-start"
	stagePreStop    lifecycleStage = "pre-stop"
	stageStop       lifecycleStage = "stop"
	stagePostStop   lifecycleStage = "post-stop"
	stagePreDelete  lifecycleStage = "pre-delete"
	stageDelete     lifecycleStage = "delete"
	stagePostDelete lifecycleStage = "post-delete"
)

const (
	defaultHookTimeoutMs = 30000
)

type commandHook struct {
	Name      string   `json:"name,omitempty"`
	Cmd       []string `json:"cmd,omitempty"`
	Shell     string   `json:"shell,omitempty"`
	TimeoutMs int      `json:"timeoutMs,omitempty"`
	Strict    *bool    `json:"strict,omitempty"`
}

type lifecycleHooksConfig struct {
	PreStart   []commandHook `json:"preStart,omitempty"`
	Start      []commandHook `json:"start,omitempty"`
	PostStart  []commandHook `json:"postStart,omitempty"`
	PreStop    []commandHook `json:"preStop,omitempty"`
	Stop       []commandHook `json:"stop,omitempty"`
	PostStop   []commandHook `json:"postStop,omitempty"`
	PreDelete  []commandHook `json:"preDelete,omitempty"`
	Delete     []commandHook `json:"delete,omitempty"`
	PostDelete []commandHook `json:"postDelete,omitempty"`
}

var stageHookManager = newStageHookManager()

func newStageHookManager() *lifecyclehooks.Manager {
	manager := lifecyclehooks.NewManager()

	manager.Register(string(stagePreStart),
		lifecyclehooks.Definition{Name: "template", Strict: true, Handle: hookprestart.HandleTemplate},
		lifecyclehooks.Definition{Name: "create-network", Strict: true, Handle: hookprestart.HandleCreateNetwork},
	)
	manager.Register(string(stagePostStart),
		lifecyclehooks.Definition{Name: "template", Strict: true, Handle: hookpoststart.HandleTemplate},
	)
	manager.Register(string(stagePreStop),
		lifecyclehooks.Definition{Name: "template", Strict: true, Handle: hookprestop.HandleTemplate},
	)
	manager.Register(string(stagePostStop),
		lifecyclehooks.Definition{Name: "template", Strict: false, Handle: hookpoststop.HandleTemplate},
		lifecyclehooks.Definition{Name: "delete-network", Strict: false, Handle: hookpoststop.HandleDeleteNetwork},
	)
	manager.Register(string(stagePreDelete),
		lifecyclehooks.Definition{Name: "template", Strict: false, Handle: hookpredelete.HandleTemplate},
	)
	manager.Register(string(stageDelete),
		lifecyclehooks.Definition{Name: "template", Strict: false, Handle: hookdelete.HandleTemplate},
	)
	manager.Register(string(stagePostDelete),
		lifecyclehooks.Definition{Name: "template", Strict: false, Handle: hookpostdelete.HandleTemplate},
	)

	return manager
}

func runStageHooks(ctx context.Context, stage lifecycleStage, vmID string, logger *slog.Logger) error {
	req, err := buildHookRequest(stage, vmID, logger)
	if err != nil {
		return err
	}

	definitions := stageHookManager.Definitions(string(stage))

	cfgDefs, err := commandHookDefinitions(stage, vmID)
	if err != nil {
		return err
	}
	definitions = append(definitions, cfgDefs...)
	definitions = append(definitions, envHookDefinitions(stage)...)

	return stageHookManager.Run(ctx, req, definitions)
}

func buildHookRequest(stage lifecycleStage, vmID string, logger *slog.Logger) (lifecyclehooks.Request, error) {
	paths := resolvePaths(vmID)
	req := lifecyclehooks.Request{
		VMID:  vmID,
		Stage: string(stage),
		Paths: lifecyclehooks.Paths{
			VMDir:      paths.vmDir,
			RunDir:     paths.runDir,
			SocketPath: paths.socketPath,
			VMJSONPath: paths.vmJSONPath,
			MetaPath:   paths.metaPath,
		},
		Logger: logger,
	}

	content, err := os.ReadFile(paths.vmJSONPath)
	if err != nil {
		if os.IsNotExist(err) {
			return req, nil
		}
		if stageRequiresVMConfig(stage) {
			return req, fmt.Errorf("read vm config: %w", err)
		}
		if logger != nil {
			logger.Warn("vm config read failed for optional stage", "vmID", vmID, "stage", stage, "path", paths.vmJSONPath, "error", err)
		}
		return req, nil
	}

	var cfg model.VMConfig
	if err := json.Unmarshal(content, &cfg); err != nil {
		if stageRequiresVMConfig(stage) {
			return req, fmt.Errorf("parse vm config: %w", err)
		}
		if logger != nil {
			logger.Warn("vm config parse failed for optional stage", "vmID", vmID, "stage", stage, "path", paths.vmJSONPath, "error", err)
		}
		return req, nil
	}

	req.VMConfig = cfg
	req.VMConfigPresent = true
	return req, nil
}

func stageRequiresVMConfig(stage lifecycleStage) bool {
	switch stage {
	case stagePreStart, stageStart, stagePostStart:
		return true
	default:
		return false
	}
}

func commandHookDefinitions(stage lifecycleStage, vmID string) ([]lifecyclehooks.Definition, error) {
	hookConfig, err := loadLifecycleHooksConfig(vmID)
	if err != nil {
		return nil, err
	}

	hooks := hooksForStage(hookConfig, stage)
	definitions := make([]lifecyclehooks.Definition, 0, len(hooks))
	for _, hook := range hooks {
		definitions = append(definitions, definitionFromCommandHook(stage, hook))
	}
	return definitions, nil
}

func loadLifecycleHooksConfig(vmID string) (lifecycleHooksConfig, error) {
	paths := resolvePaths(vmID)
	hooksPath := getEnv("MGN_LIFECYCLE_HOOKS_JSON", filepath.Join(paths.vmDir, "lifecycle-hooks.json"))

	content, err := os.ReadFile(hooksPath)
	if err != nil {
		if os.IsNotExist(err) {
			return lifecycleHooksConfig{}, nil
		}
		return lifecycleHooksConfig{}, fmt.Errorf("read lifecycle hooks: %w", err)
	}

	var cfg lifecycleHooksConfig
	if err := json.Unmarshal(content, &cfg); err != nil {
		return lifecycleHooksConfig{}, fmt.Errorf("parse lifecycle hooks: %w", err)
	}
	return cfg, nil
}

func hooksForStage(cfg lifecycleHooksConfig, stage lifecycleStage) []commandHook {
	switch stage {
	case stagePreStart:
		return cfg.PreStart
	case stageStart:
		return cfg.Start
	case stagePostStart:
		return cfg.PostStart
	case stagePreStop:
		return cfg.PreStop
	case stageStop:
		return cfg.Stop
	case stagePostStop:
		return cfg.PostStop
	case stagePreDelete:
		return cfg.PreDelete
	case stageDelete:
		return cfg.Delete
	case stagePostDelete:
		return cfg.PostDelete
	default:
		return nil
	}
}

func definitionFromCommandHook(stage lifecycleStage, hook commandHook) lifecyclehooks.Definition {
	name := strings.TrimSpace(hook.Name)
	if name == "" {
		name = fmt.Sprintf("%s-hook", stage)
	}
	strict := true
	if hook.Strict != nil {
		strict = *hook.Strict
	}

	return lifecyclehooks.Definition{
		Name:   name,
		Strict: strict,
		Handle: func(ctx context.Context, req lifecyclehooks.Request) error {
			timeoutMs := hook.TimeoutMs
			if timeoutMs <= 0 {
				timeoutMs = defaultHookTimeoutMs
			}

			if len(hook.Cmd) > 0 {
				return runArgvHook(ctx, req.VMID, hook.Cmd, timeoutMs)
			}
			if strings.TrimSpace(hook.Shell) != "" {
				return runShellHook(ctx, req.VMID, hook.Shell, timeoutMs)
			}
			return nil
		},
	}
}

func envHookDefinitions(stage lifecycleStage) []lifecyclehooks.Definition {
	shellHooks := shellHooksFromEnv(stage)
	if len(shellHooks) == 0 {
		return nil
	}

	definitions := make([]lifecyclehooks.Definition, 0, len(shellHooks))
	for _, shellCommand := range shellHooks {
		command := shellCommand
		definitions = append(definitions, lifecyclehooks.Definition{
			Name:   "env-hook",
			Strict: true,
			Handle: func(ctx context.Context, req lifecyclehooks.Request) error {
				return runShellHook(ctx, req.VMID, command, defaultHookTimeoutMs)
			},
		})
	}
	return definitions
}

func shellHooksFromEnv(stage lifecycleStage) []string {
	key := envKeyForStage(stage)
	raw := strings.TrimSpace(getEnv(key, ""))
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ";")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return result
}

func envKeyForStage(stage lifecycleStage) string {
	switch stage {
	case stagePreStart:
		return "MGN_LIFECYCLE_PRE_START_HOOKS"
	case stageStart:
		return "MGN_LIFECYCLE_START_HOOKS"
	case stagePostStart:
		return "MGN_LIFECYCLE_POST_START_HOOKS"
	case stagePreStop:
		return "MGN_LIFECYCLE_PRE_STOP_HOOKS"
	case stageStop:
		return "MGN_LIFECYCLE_STOP_HOOKS"
	case stagePostStop:
		return "MGN_LIFECYCLE_POST_STOP_HOOKS"
	case stagePreDelete:
		return "MGN_LIFECYCLE_PRE_DELETE_HOOKS"
	case stageDelete:
		return "MGN_LIFECYCLE_DELETE_HOOKS"
	case stagePostDelete:
		return "MGN_LIFECYCLE_POST_DELETE_HOOKS"
	default:
		return ""
	}
}

func runArgvHook(ctx context.Context, vmID string, args []string, timeoutMs int) error {
	if len(args) == 0 {
		return nil
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	execArgs := make([]string, 0, len(args))
	for _, arg := range args {
		execArgs = append(execArgs, replaceHookTokens(arg, vmID))
	}
	cmd := exec.CommandContext(timeoutCtx, execArgs[0], execArgs[1:]...)
	cmd.Env = append(os.Environ(), "MGN_VM_ID="+vmID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", strings.Join(execArgs, " "), strings.TrimSpace(string(output)))
	}
	return nil
}

func runShellHook(ctx context.Context, vmID, shellCommand string, timeoutMs int) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	command := replaceHookTokens(shellCommand, vmID)
	cmd := exec.CommandContext(timeoutCtx, "sh", "-c", command)
	cmd.Env = append(os.Environ(), "MGN_VM_ID="+vmID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", command, strings.TrimSpace(string(output)))
	}
	return nil
}

func replaceHookTokens(input, vmID string) string {
	output := strings.ReplaceAll(input, "{{vm_id}}", vmID)
	output = strings.ReplaceAll(output, "${VM_ID}", vmID)
	return output
}
